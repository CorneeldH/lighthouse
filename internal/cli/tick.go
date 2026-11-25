package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/usage"
	"github.com/CorneeldH/lighthouse/internal/workplace"
	"github.com/CorneeldH/lighthouse/internal/workplace/kennforge"
	"github.com/CorneeldH/lighthouse/internal/workplace/local"
)

// tickEnv is what tick and gather read from the machine: the environment, the path of
// host.toml, the GitHub API root, the clock, the forge workplace, the state directory, the
// usage meter, the meminfo file and the subprocess runner. Only tests set ForgeURL, Now,
// Workplace, StateDir, Meter, MemInfo and Run.
type tickEnv struct {
	Getenv   func(string) string
	HostToml string
	ForgeURL string           // the API root of GitHub repos; "" is forge.DefaultBaseURL
	Now      func() time.Time // nil is time.Now
	// Workplace is the workplace of a repo; nil, or a nil it returns, follows [dispatch]
	// workplace: "local" is a local.Adapter, any other value a kennforge.Adapter from
	// [dispatch.forge], [dispatch.profiles], [dispatch.nodes] and the repo.
	Workplace func(repo repoconfig.Repo, node, profile string) workplace.Workplace
	StateDir  string // run instructions, carriers and permission notes; "" is ${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/dispatch
	// Meter measures the usage windows; nil is a meter on usage.DefaultPaths and the [usage] table of HostToml.
	Meter *usage.Meter
	// MemInfo is the meminfo file of the memory brake; "" is /proc/meminfo.
	MemInfo string
	// Run runs git for the workplace and the sweep; nil is workplace.ExecRunner.
	Run workplace.Runner
	// DBPath is the database tick opened: the pusher gets it as --db. tick and run set it.
	DBPath string
	// Spawn starts the pusher process with the arguments and the environment; nil is spawnDetached.
	// Only tests set it.
	Spawn func(ctx context.Context, args, env []string) error
	// Jobs looks up a scheduled job by name; nil is scheduled.Lookup.
	Jobs func(name string) (scheduled.Job, bool)
	// GitBase is the base URL of the git remotes the branch of a scheduled run is pushed to;
	// "" is https://<the repository host, or github.com>.
	GitBase string

	// serves are the serve clients of the nodes for this tick or lighthouse run (withServes);
	// nil builds a client per workplace.
	serves *nodeServes
}

func (e tickEnv) runner() workplace.Runner {
	if e.Run == nil {
		return workplace.ExecRunner()
	}
	return e.Run
}

func (e tickEnv) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

// stateDir is StateDir, or lighthouse/dispatch under XDG_STATE_HOME, or under
// $HOME/.local/state when XDG_STATE_HOME is not an absolute path.
func (e tickEnv) stateDir() (string, error) {
	if e.StateDir != "" {
		return e.StateDir, nil
	}
	return collect.DispatchStateDir(e.Getenv)
}

// workplace returns the workplace of repo on node ("" is self) for profile; stateDir is where
// its carriers go. The local workplace has no nodes and no profile of its own. An adapter on a
// node gets the serve client of that node for this tick.
func (e tickEnv) workplace(cfg hostconfig.Dispatch, repo repoconfig.Repo, stateDir, node, profile string) workplace.Workplace {
	if e.Workplace != nil {
		if w := e.Workplace(repo, node, profile); w != nil {
			return w
		}
	}
	if cfg.Workplace == hostconfig.WorkplaceLocal {
		return &local.Adapter{
			RepoDir: repo.Dir, DefaultBranch: repo.DefaultBranch, Repo: repo.Slug,
			StateDir: stateDir, Run: e.runner(), Home: e.Getenv("HOME"),
		}
	}
	targets := make(map[string]string, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		targets[name] = p.TargetKey
	}
	return &kennforge.Adapter{
		Client: kennforge.New(cfg.Forge.URL, cfg.Forge.TokenFile),
		Kind:   repo.Kind, Repo: repo.Slug, StateDir: stateDir,
		Targets: targets, Profile: profile, Node: node, Run: e.runner(),
		Home: e.Getenv("HOME"), UnitPrefix: cfg.Forge.UnitPrefix, Serve: e.serve(cfg, node),
	}
}

// placeKey is one workplace of a tick: a repo, a node ("" is self) and a profile.
type placeKey struct{ repo, node, profile string }

// places returns the workplaces of repos, built lazily, one per (repo, node, profile): a run
// is measured, stopped and removed on the node and in the profile its row holds.
func (e tickEnv) places(cfg hostconfig.Dispatch, stateDir string) func(repo repoconfig.Repo, node, profile string) workplace.Workplace {
	built := map[placeKey]workplace.Workplace{}
	return func(repo repoconfig.Repo, node, profile string) workplace.Workplace {
		k := placeKey{repo.Slug, node, profile}
		w, ok := built[k]
		if !ok {
			w = e.workplace(cfg, repo, stateDir, node, profile)
			built[k] = w
		}
		return w
	}
}

// place is the workplace.Place of one repo.
func (e tickEnv) place(cfg hostconfig.Dispatch, repo repoconfig.Repo, stateDir string) workplace.Place {
	places := e.places(cfg, stateDir)
	return func(node, profile string) workplace.Workplace { return places(repo, node, profile) }
}

func newTickCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one dispatcher tick",
		Long: "Run one dispatcher tick over the repos in host.toml [dispatch]. Each dispatchable repo gets\n" +
			"a precheck and, when something changed, a full gather. Decide then runs once over the gathered\n" +
			"repos, and apply posts, claims and launches what it decided. Every tick then removes the forge\n" +
			"workspaces of issues that left Controller, when nothing is lost by it. A repo that cannot be\n" +
			"dispatched or gathered is skipped loudly, as is a failed action or cleanup, and tick exits 1; a\n" +
			"tick that is still running makes this one exit 0.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env := tickEnv{Getenv: os.Getenv, HostToml: hostconfig.Path()}
			return tick(cmd.Context(), env, dbPath, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	return cmd
}

// tickLockPath is $XDG_RUNTIME_DIR/lighthouse/tick.lock, with its directory made.
func tickLockPath(getenv func(string) string) (string, error) {
	runtime := getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", errors.New("tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock")
	}
	if !filepath.IsAbs(runtime) {
		return "", errors.New("tick: XDG_RUNTIME_DIR is not an absolute path: cannot take tick.lock")
	}
	dir := filepath.Join(runtime, "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("tick: %w", err)
	}
	return filepath.Join(dir, "tick.lock"), nil
}

// tick takes tick.lock, loads the repos of host.toml, opens the forge of each with its own token
// (openForges) and skips, loudly, each one that does not load, shares its slug, is not
// dispatchable, has no forge or whose gather fails; the others get gatherRepo, the gathered ones
// planSchedules (their due schedules, gated) and decideAndApply, every repo whose precheck passed saveRunViews, also when its full gather
// failed, and every repo that was not skipped sweepRepos, due or not. The run views follow
// apply, so a run this tick starts or closes shows at once. A busy lock is
// not an error. At least one skip, failed action, unsaved run view or failed cleanup returns
// errSilent: the reasons are already on errOut.
func tick(ctx context.Context, env tickEnv, dbPath string, errOut io.Writer) error {
	lockPath, err := tickLockPath(env.Getenv)
	if err != nil {
		return err
	}
	lock, err := flock.TryLock(lockPath)
	if errors.Is(err, flock.ErrHeld) {
		_, _ = fmt.Fprintln(errOut, "lighthouse: previous tick still running")
		return nil
	}
	if err != nil {
		return fmt.Errorf("tick: %w", err)
	}
	defer func() { _ = lock.Close() }()

	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return err
	}
	loaded := repoconfig.LoadAll(cfg.Repos)
	forges, err := openForges(ctx, env, loaded)
	if err != nil {
		return fmt.Errorf("tick: %w", err)
	}
	if len(cfg.Repos) == 0 {
		if env.HostToml == "" {
			_, _ = fmt.Fprintln(errOut, "lighthouse: no host.toml: no repos to check")
		} else {
			_, _ = fmt.Fprintf(errOut, "lighthouse: no repos in [dispatch] of %s\n", env.HostToml)
		}
	}
	if dbPath == "" {
		if dbPath, err = store.DefaultPath(); err != nil {
			return err
		}
	}
	env.DBPath = dbPath
	st, err := store.OpenShared(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	env = env.withServes(cfg, errOut)
	facts, err := readHostFacts(ctx, env, st.DB, cfg, errOut)
	if err != nil {
		return fmt.Errorf("tick: the brake cannot check: %w", err)
	}
	pushUsage(ctx, env, errOut)

	// One GET /user per tick for each kind, host, base URL and token (forgeOpener), and none
	// when no repo is due.
	var gathered []gatheredRepo
	var checked, viewed []checkedRepo
	ticked := map[string]forge.Forge{}
	skipped := false
	for i, l := range loaded {
		path, repo := l.Path, l.Repo
		of := forges[i]
		var in *dispatch.RepoInput
		var fp string
		var pre *dispatch.Pre
		err := of.err
		if err == nil {
			ticked[repo.Slug] = of.rf.Forge
			in, fp, pre, err = gatherRepo(ctx, env, st.DB, cfg, facts, repo, of.rf, errOut)
		}
		if pre != nil {
			viewed = append(viewed, checkedRepo{repo: repo, pre: *pre})
		}
		if err != nil {
			skipped = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
			continue
		}
		checked = append(checked, checkedRepo{repo: repo, pre: *pre})
		if in != nil {
			gathered = append(gathered, gatheredRepo{repo: repo, in: *in, fp: fp})
		}
	}
	planFailed := planSchedules(ctx, env, st.DB, cfg, ticked, gathered, errOut)
	failed, err := decideAndApply(ctx, env, st.DB, cfg, facts, ticked, gathered, errOut)
	if err != nil {
		return err
	}
	failed = failed || planFailed
	if saveRunViews(ctx, env, st.DB, viewed, errOut) {
		failed = true
	}
	// The transcripts of runs go first: the sweeps remove the workspaces and mirrors they come from.
	// A failed collection does not hold the sweeps back (see collectRuns).
	if collectRuns(ctx, env, st.DB, cfg, loadedRepos(loaded), errOut) {
		failed = true
	}
	// A failed step never holds the sweeps back either: it only turns the tick red.
	tel, err := hostconfig.LoadTelemetry(env.HostToml)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: version change: %s\n", oneLine(err))
		failed = true
	} else if proposeVersionChanges(ctx, env, st.DB, tel, loadedRepos(loaded), ticked, errOut) {
		failed = true
	}
	if sweepRepos(ctx, env, st.DB, cfg, ticked, checked, errOut) {
		failed = true
	}
	if sweepNodes(ctx, env, st.DB, errOut) {
		failed = true
	}
	if skipped || failed {
		return errSilent
	}
	return nil
}

// loadedRepos are the repos of loaded that loaded without an error.
func loadedRepos(loaded []repoconfig.Loaded) []repoconfig.Repo {
	var out []repoconfig.Repo
	for _, l := range loaded {
		if l.Err == nil {
			out = append(out, l.Repo)
		}
	}
	return out
}

// openedForge is the forge of one loaded repo, or why it has none.
type openedForge struct {
	rf  repoForge
	err error
}

// openForges opens, without output, the forge of each repo in loaded that loaded and is
// dispatchable; the others carry their load, duplicate-slug or Dispatchable error. When no repo
// gets a forge it returns the first token error. When no repo needed a forge, a GitHub token
// must still be set, as before forges were per repo. Its GitHub clients live one tick, so they
// remember each enrichment of an open pull request (forge.GitHub.EnrichMemo).
func openForges(ctx context.Context, env tickEnv, loaded []repoconfig.Loaded) ([]openedForge, error) {
	o := newForgeOpener(env.Getenv, env.ForgeURL)
	o.enrichMemo = true
	out := make([]openedForge, len(loaded))
	var tally openTally
	for i, l := range loaded {
		err := l.Err
		if err == nil {
			err = repoconfig.Dispatchable(l.Repo)
		}
		if err != nil {
			out[i].err = err
			continue
		}
		out[i].rf, out[i].err = o.forRepo(ctx, l.Repo)
		tally.add(out[i].err)
	}
	if err := tally.err(env.Getenv); err != nil {
		return nil, err
	}
	return out, nil
}

// oneLine is the text of err on one line.
func oneLine(err error) string {
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ")
}

// gatheredRepo is a repo whose full gather ran in this tick, with its precheck fingerprint.
// hold keeps its tick state from being saved: its planner failed, so the next tick must gather
// again.
type gatheredRepo struct {
	repo repoconfig.Repo
	in   dispatch.RepoInput
	fp   string
	hold bool
}

// checkedRepo is a repo whose precheck passed in this tick, with that precheck.
type checkedRepo struct {
	repo repoconfig.Repo
	pre  dispatch.Pre
}

// saveRunViews replaces the run view of each repo with its open and parked runs as they stand
// after apply (the markers of this host and of the nodes read again), with the titles and measured stands of its precheck. Each repo whose view was not
// saved gets a save run view line on errOut; failed reports one.
func saveRunViews(ctx context.Context, env tickEnv, db *sql.DB, repos []checkedRepo, errOut io.Writer) (failed bool) {
	if len(repos) == 0 {
		return false
	}
	var parked map[string][]runs.Row
	var all []park.Marker
	stateDir, err := env.stateDir()
	var markerDirs []string
	if err == nil {
		markerDirs, err = park.HostMarkerDirs(env.Getenv, stateDir)
	}
	if err == nil {
		all, err = park.ReadMarkersIn(markerDirs)
	}
	if err == nil {
		onNodes, _, _ := env.nodeMarkers(ctx) // a node that does not read shows no parked run
		_, parked, err = controllerMarkers(ctx, db, append(all, onNodes...))
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: save run view: %s\n", oneLine(err))
		return true
	}
	for _, cr := range repos {
		if err := saveRunView(ctx, env, db, cr, parked[cr.repo.Slug]); err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: save run view %s: %s\n", cr.repo.Slug, oneLine(err))
		}
	}
	return failed
}

// saveRunView saves the run view of one repo; see saveRunViews.
func saveRunView(ctx context.Context, env tickEnv, db *sql.DB, cr checkedRepo, parked []runs.Row) error {
	open, err := runs.OpenBriefs(ctx, db, cr.repo.Slug)
	if err != nil {
		return err
	}
	ids := make([]int64, len(parked))
	for i, r := range parked {
		ids[i] = r.ID
	}
	briefs, err := runs.BriefsByID(ctx, db, ids)
	if err != nil {
		return err
	}
	return dispatch.SaveRunView(ctx, db, cr.repo.Slug, dispatch.RunViews(cr.pre, open, briefs), env.now())
}

// sweepRepos runs workplace.Sweep for each repo: the workspaces of issues the controller no
// longer holds go, when nothing is lost by it (workplace.Cleanup), and the workspace of a
// scheduled run without an open row goes forcibly. Each removed
// workspace gets a removed line on errOut, each repo whose sweep failed a cleanup line; a
// workspace it keeps gets no line: it is in Workspaces to clean up. A repo without a
// workspace to sweep builds no workplace. failed reports a cleanup line.
func sweepRepos(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, forges map[string]forge.Forge,
	repos []checkedRepo, errOut io.Writer) (failed bool) {
	for _, sr := range repos {
		slug := sr.repo.Slug
		if msg := cloneProblem(ctx, env, cfg, sr.repo); msg != "" {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: cleanup %s: %s\n", slug, msg)
		}
		removed, err := sweepOne(ctx, env, db, cfg, forges[slug], sr)
		for _, l := range removed {
			of := fmt.Sprintf("%s#%d", slug, l.Issue)
			if l.Schedule != "" {
				of = slug + " scheduled:" + l.Schedule
			}
			_, _ = fmt.Fprintf(errOut, "lighthouse: removed workspace %s of %s\n", l.WorkspaceID, of)
		}
		if err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: cleanup %s: %s\n", slug, oneLine(err))
		}
	}
	return failed
}

// sweepNodes removes the transcript mirrors and tick-side carriers of node runs that are no
// longer open (sweepNodeLeftovers); a failure gets a cleanup line on errOut, and failed reports it.
func sweepNodes(ctx context.Context, env tickEnv, db *sql.DB, errOut io.Writer) (failed bool) {
	dir, err := env.stateDir()
	if err == nil {
		err = sweepNodeLeftovers(ctx, db, dir)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: cleanup node leftovers: %s\n", oneLine(err))
		return true
	}
	return false
}

// sweepOne sweeps one repo, whose forge is f; see sweepRepos.
func sweepOne(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, f forge.Forge,
	sr checkedRepo) ([]workplace.Leftover, error) {
	refs, err := runs.Workspaces(ctx, db, sr.repo.Slug)
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	dir, err := env.stateDir()
	if err != nil {
		return nil, err
	}
	in := workplace.SweepInput{
		Repo:         sr.repo.Slug,
		InController: dispatch.InController(sr.pre),
		MergedHead: func(ctx context.Context, branch string) (string, error) {
			prs, err := f.BranchPRs(ctx, branch)
			if err != nil {
				return "", err
			}
			return dispatch.MergedHead(prs), nil
		},
	}
	return workplace.Sweep(ctx, db, env.place(cfg, sr.repo, dir), env.runner(), in, env.now())
}

// decideAndApply decides once over the gathered repos and applies the actions. Each deferral
// gets a deferred line on errOut, each repo with a failed action an apply line. It saves the
// fingerprint of a repo only when none of its actions was deferred or failed and its planner did
// not fail (gatheredRepo.hold), so the next tick tries again (spec §3 step 2), and replaces the deferred rows of each gathered repo with the
// deferrals of Decide. forges holds the forge of each gathered repo. failed reports an apply
// line or a state that was not saved.
func decideAndApply(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, facts hostFacts, forges map[string]forge.Forge,
	gathered []gatheredRepo, errOut io.Writer) (failed bool, err error) {
	if len(gathered) == 0 {
		return false, nil
	}
	inputs := make([]dispatch.RepoInput, len(gathered))
	repos := make(map[string]repoconfig.Repo, len(gathered))
	for i, g := range gathered {
		inputs[i] = g.in
		repos[g.repo.Slug] = g.repo
	}
	actions := dispatch.Decide(decideInput(env, cfg, inputs, &facts.Brake))
	if saveIdle(ctx, env, db, gathered, actions, errOut) {
		failed = true
	}
	// An idle mark has no effect: Apply does not know the kind.
	actions = slices.DeleteFunc(actions, func(a dispatch.Action) bool { return a.Kind == dispatch.KindIdle })
	res, err := applyActions(ctx, env, db, cfg, facts.MarkerNode, forges, repos, actions, errOut)
	if err != nil {
		return false, fmt.Errorf("tick: %w", err)
	}
	for _, g := range gathered {
		// What Decide deferred plus what apply found out, such as a busy workplace; a key in both
		// is one row.
		defers := append(repoDefers(actions, g.repo.Slug), repoDefers(res.DeferredActions, g.repo.Slug)...)
		titles := make(map[int]string, len(g.in.Snapshot.Items))
		for _, it := range g.in.Snapshot.Items {
			titles[it.Issue] = it.Title
		}
		if err := dispatch.SaveDeferred(ctx, db, g.repo.Slug, defers, titles, env.now()); err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: save deferred %s: %s\n", g.repo.Slug, oneLine(err))
		}
	}
	for _, d := range res.Deferred {
		_, _ = fmt.Fprintf(errOut, "lighthouse: deferred %s\n", d)
	}
	for _, g := range gathered {
		slug := g.repo.Slug
		if err := res.Failed[slug]; err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: apply %s: %s\n", slug, oneLine(err))
			continue
		}
		// A deferral of an issue ("<slug>#<n>: ") or of a scheduled run ("<slug> scheduled:...").
		if g.hold || slices.ContainsFunc(res.Deferred, func(d string) bool {
			return strings.HasPrefix(d, slug+"#") || strings.HasPrefix(d, slug+" ")
		}) {
			continue
		}
		state := dispatch.TickState{Fingerprint: g.fp, FullAt: g.in.Snapshot.GatheredAt}
		if err := dispatch.SaveTickState(ctx, db, slug, state, env.now()); err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: save tick state %s: %s\n", slug, oneLine(err))
		}
	}
	return failed, nil
}

// saveIdle saves per gathered repo the idle counts of this evaluation: an issue Decide marked
// KindIdle gets the count it came in with plus one, every other issue loses its count, so only
// evaluations in a row count. failed reports a count that was not saved.
func saveIdle(ctx context.Context, env tickEnv, db *sql.DB, gathered []gatheredRepo, actions []dispatch.Action,
	errOut io.Writer) (failed bool) {
	for _, g := range gathered {
		counts := map[int]int{}
		for _, a := range actions {
			if a.Kind == dispatch.KindIdle && a.Repo == g.repo.Slug {
				counts[a.Issue] = g.in.IdleTicks[a.Issue] + 1
			}
		}
		if err := dispatch.SaveIdle(ctx, db, g.repo.Slug, counts, env.now()); err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: save idle counts %s: %s\n", g.repo.Slug, oneLine(err))
		}
	}
	return failed
}

// applyActions applies actions to repos, each with its forge in forges and its workplace; apply
// logs to errOut with the "lighthouse: " of every tick line. Without actions it applies nothing.
// tick and `lighthouse run` share it. Every repo in repos has a board: Dispatchable guarantees it.
// markerNode is hostFacts.MarkerNode: a marker of a session in it is removed on that node.
func applyActions(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, markerNode map[string]string,
	forges map[string]forge.Forge, repos map[string]repoconfig.Repo, actions []dispatch.Action, errOut io.Writer) (dispatch.Result, error) {
	if len(actions) == 0 {
		return dispatch.Result{}, nil
	}
	dir, err := env.stateDir()
	if err != nil {
		return dispatch.Result{}, err
	}
	markerDirs, err := park.HostMarkerDirs(env.Getenv, dir)
	if err != nil {
		return dispatch.Result{}, err
	}
	places := env.places(cfg, dir)
	// Apply's joined error is dropped on purpose: the same errors are in res.Failed, per
	// repo: the callers write their apply lines from it, and tick decides which fingerprints
	// it saves.
	res, _ := dispatch.Apply(ctx, dispatch.Env{
		DB:           db,
		Forge:        func(slug string) forge.Forge { return forges[slug] },
		Workplace:    func(slug, node, profile string) workplace.Workplace { return places(repos[slug], node, profile) },
		MCPConfig:    func(profile string) string { return cfg.Profiles[profile].MCPConfig },
		ScheduledJob: scheduledJob(env, db, cfg, forges, repos),
		Remote:       scheduledRemote(env, dir, repos),
		RoundLimit:   func(slug string) string { return repos[slug].Controller.AtRoundLimit },
		Labels:       func(slug string) repoconfig.Labels { return repos[slug].Labels },
		StateDir:     dir,
		MarkerDir:    markerDirs[0],
		RemoveMarker: func(session, key string) error {
			return env.removeMarker(ctx, cfg, markerDirs, markerNode[session], session, key)
		},
		MaxWait:        cfg.MaxWait,
		Now:            env.now,
		NewSession:     runs.NewSessionID,
		Log:            prefixed{errOut},
		Getenv:         env.Getenv,
		TokenEnv:       func(slug string) string { return tokenEnvName(repos[slug]) },
		TokenEnvs:      tokenEnvs(cfg),
		StartPusher:    env.startPusher(dir),
		ClassifyAnswer: env.lazyClassifier(),
	}, actions)
	return res, nil
}

// removeMarker removes the resume marker of session while its key is key: on node (DELETE
// markers through serve), or in markerDirs when node is "".
func (e tickEnv) removeMarker(ctx context.Context, cfg hostconfig.Dispatch, markerDirs []string, node, session, key string) error {
	if node == "" {
		return park.RemoveMarkerIn(markerDirs, session, key)
	}
	c := e.serve(cfg, node)
	if c == nil {
		return fmt.Errorf("node %s is not in host.toml: its marker stays", node)
	}
	return c.DeleteMarker(ctx, session, key)
}

// prefixed writes each line apply logs with the "lighthouse: " of every tick line: apply
// writes one line per Write.
type prefixed struct{ w io.Writer }

func (p prefixed) Write(b []byte) (int, error) {
	if _, err := io.WriteString(p.w, "lighthouse: "); err != nil {
		return 0, err
	}
	return p.w.Write(b)
}

// gatherRepo runs the precheck for repo and, when it is due, the full gather; in is nil when
// the repo is not due. fp is the fingerprint of the precheck: tick saves it after apply.
// pre is the precheck, also when the repo is not due or its full gather fails: tick builds the
// run view and the sweep from it after apply; it is nil only when the precheck failed. A full
// gather writes one gathered line to errOut. rf is the forge of repo.
func gatherRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, facts hostFacts, repo repoconfig.Repo,
	rf repoForge, errOut io.Writer) (in *dispatch.RepoInput, fp string, _ *dispatch.Pre, err error) {
	pre, err := measuredPre(ctx, env, db, cfg, rf, repo, errOut)
	if err != nil {
		return nil, "", nil, err
	}
	pre.Markers, pre.PauseUntil, pre.Unreadable = facts.Markers[repo.Slug], facts.PauseUntil, facts.Unreadable
	pre.MarkersIncomplete = facts.MarkersIncomplete
	fp = dispatch.Fingerprint(pre)
	prev, err := dispatch.LoadTickState(ctx, db, repo.Slug)
	if err != nil {
		return nil, "", &pre, err
	}
	now := env.now()
	boundaries := append(dispatch.Boundaries(cfg, pre.OpenRuns, pre.Runs, pre.Markers, now),
		dispatch.ScheduleBoundaries(repo.Schedules, now)...)
	due, reason := dispatch.Due(prev, fp, now, boundaries)
	if !due {
		return nil, "", &pre, nil
	}
	ri, err := repoInput(ctx, db, cfg, rf.Forge, pre, rf.who, errOut, now)
	if err != nil {
		return nil, "", &pre, err
	}
	if ri.IdleTicks, err = dispatch.LoadIdle(ctx, db, repo.Slug); err != nil {
		return nil, "", &pre, err
	}
	_, _ = fmt.Fprintf(errOut, "lighthouse: gathered %s: %d items (%s)\n", repo.Slug, len(ri.Snapshot.Items), reason)
	return &ri, fp, &pre, nil
}

// prepare is the board half of a gather: the open runs, the branch of each issue's last run,
// and the board with the PRs of those branches. The To do marks of the last precheck spare the
// comment reads of unchanged To do items (dispatch.ToDoProbe); with save, prepare saves the
// marks when they changed, so the next precheck reads none on an unchanged board.
func prepare(ctx context.Context, db *sql.DB, cfg hostconfig.Dispatch, rf repoForge, repo repoconfig.Repo,
	save bool, now time.Time) (dispatch.Pre, error) {
	open, err := runs.Open(ctx, db, repo.Slug)
	if err != nil {
		return dispatch.Pre{}, err
	}
	history, err := runs.History(ctx, db, repo.Slug)
	if err != nil {
		return dispatch.Pre{}, err
	}
	state, err := dispatch.LoadTickState(ctx, db, repo.Slug)
	if err != nil {
		return dispatch.Pre{}, err
	}
	probe := dispatch.ToDoProbe{Known: state.ToDo, TokenLogin: rf.who, Trust: dispatch.NewTrust(rf.Forge, cfg.AnswerPermissions)}
	pre, err := dispatch.Prepare(ctx, rf.Forge, repo, open, dispatch.IssueBranches(history), probe)
	if err != nil {
		return dispatch.Pre{}, err
	}
	if save && !dispatch.SameToDo(state.ToDo, pre.ToDo) {
		if err := dispatch.SaveToDo(ctx, db, repo.Slug, pre.ToDo, now); err != nil {
			return dispatch.Pre{}, err
		}
	}
	return pre, nil
}

// measuredPre is the precheck half of a gather that tick and `lighthouse run` share: it
// adopts the sessions of uncertain starts, reads the board half (prepare), and measures the
// open runs. Adoption comes first, so the open runs prepare reads already hold a session that
// showed up in this tick.
func measuredPre(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, rf repoForge,
	repo repoconfig.Repo, errOut io.Writer) (dispatch.Pre, error) {
	if err := adopt(ctx, env, db, cfg, repo, errOut); err != nil {
		return dispatch.Pre{}, err
	}
	pre, err := prepare(ctx, db, cfg, rf, repo, true, env.now())
	if err != nil {
		return dispatch.Pre{}, err
	}
	if pre.Stands, pre.Runs, err = measure(ctx, env, cfg, repo, pre.OpenRuns, errOut); err != nil {
		return dispatch.Pre{}, err
	}
	return pre, nil
}

// adopt runs workplace.AdoptSessions for repo when one of its open runs has a workspace but no
// session key; otherwise it builds no workplace. A run it cannot look up keeps no key and gets
// a measure line on errOut: the tick goes on without it.
func adopt(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, repo repoconfig.Repo, errOut io.Writer) error {
	open, err := runs.Open(ctx, db, repo.Slug)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(open, func(r runs.OpenRun) bool { return r.WorkspaceID != "" && r.WorkplaceSession == "" }) {
		return nil
	}
	dir, err := env.stateDir()
	if err != nil {
		return err
	}
	if err := workplace.AdoptSessions(ctx, db, env.place(cfg, repo, dir), repo.Slug); err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: measure %s: %s\n", repo.Slug, oneLine(err))
	}
	return nil
}

// measure measures each open run that has a workspace, on the node and in the profile of the run:
// the stand of its session (only with a session key) and its RunState. A run it cannot measure has neither, and gets a measure line
// on errOut; the other runs are still measured. The error is only for a missing state
// directory.
func measure(ctx context.Context, env tickEnv, cfg hostconfig.Dispatch, repo repoconfig.Repo,
	open []runs.OpenRun, errOut io.Writer) (map[int64]workplace.Stand, map[int64]dispatch.RunState, error) {
	stands := map[int64]workplace.Stand{}
	states := map[int64]dispatch.RunState{}
	var place workplace.Place
	var dir string
	for _, r := range open {
		if r.WorkspaceID == "" {
			continue
		}
		if place == nil {
			var err error
			if dir, err = env.stateDir(); err != nil {
				return nil, nil, err
			}
			place = env.place(cfg, repo, dir)
		}
		m := runProbe{w: place(r.Node, r.Profile), stateDir: dir}
		stand, measured, state, err := m.measure(ctx, r)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: measure %s: run %d: %s\n", repo.Slug, r.ID, oneLine(err))
			continue
		}
		if measured {
			stands[r.ID] = stand
		}
		states[r.ID] = state
	}
	return stands, states, nil
}

// runProbe measures one open run in its workplace and against the state directory
// (permission notes). The resume markers of the usage guard are not its: tick reads them once
// per tick (readHostFacts) and Decide gets them in RepoInput.Markers.
type runProbe struct {
	w        workplace.Workplace
	stateDir string
}

// measure reads the sessions of the run's workspace, its transcript when the workplace has
// one and its permission note (permission). A workspace the workplace no
// longer knows is Gone, not an error. measured says whether stand holds the stand of the
// session whose key the run recorded.
func (m *runProbe) measure(ctx context.Context, r runs.OpenRun) (stand workplace.Stand, measured bool, s dispatch.RunState, err error) {
	sessions, err := m.w.Sessions(ctx, r.WorkspaceID)
	switch {
	case errors.Is(err, workplace.ErrGone):
		s.Gone = true
	case err != nil:
		return 0, false, dispatch.RunState{}, err
	}
	if r.WorkplaceSession != "" {
		for _, ses := range sessions {
			if ses.Key == r.WorkplaceSession {
				stand, measured, s.SessionAt = ses.Stand(), true, ses.UpdatedAt
				break
			}
		}
	}
	if !s.Gone {
		if s.Transcript, s.TranscriptUnmeasurable, err = m.transcript(ctx, r); err != nil {
			return 0, false, dispatch.RunState{}, err
		}
	}
	if s.Permission, err = m.permission(ctx, r); err != nil {
		return 0, false, dispatch.RunState{}, err
	}
	if s.PostFailed, err = m.postFailed(ctx, r); err != nil {
		return 0, false, dispatch.RunState{}, err
	}
	return stand, measured, s, nil
}

// postFailed is the post-failed note of the run, nil without one. It reads like permission: on a
// node through serve (workplace.PostFailedReader), otherwise from the state directory. Content
// that does not parse is a note with Unreadable set, never an error: the controller must still see
// that a post failed. A serve that does not answer is an error.
func (m *runProbe) postFailed(ctx context.Context, r runs.OpenRun) (*dispatch.PostFailed, error) {
	pr, ok := m.w.(workplace.PostFailedReader)
	if r.Node == "" || !ok {
		return dispatch.ReadPostFailed(m.stateDir, r.SessionID)
	}
	data, mod, err := pr.PostFailed(ctx, r.SessionID)
	if err != nil || data == nil {
		return nil, err
	}
	return dispatch.ParsePostFailed(data, mod), nil
}

// permission is the permission note of the run, nil without one. A run on a node whose
// workplace reads notes there (workplace.PermissionReader) gets it through serve; any other run
// reads the state directory. Every kenn-forge adapter is a PermissionReader, also on self, where
// it is not measurable: hence the node first. A serve that does not answer is an error: the run
// is not measurable this tick.
func (m *runProbe) permission(ctx context.Context, r runs.OpenRun) (*dispatch.Permission, error) {
	pr, ok := m.w.(workplace.PermissionReader)
	if r.Node == "" || !ok {
		return dispatch.ReadPermission(m.stateDir, r.SessionID)
	}
	data, err := pr.Permission(ctx, r.SessionID)
	if err != nil || data == nil {
		return nil, err
	}
	return dispatch.ParsePermission(data)
}

// transcript is the transcript of the run's session, or nil when it is not measurable: a
// workplace without transcripts, no transcript directory on this host, no transcript file yet
// (an uncertain start whose agent never ran), or a transcript without a timestamp.
// A node that did not answer (workplace.ErrUnanswered) is an error: the run is not measured
// this tick, so a nil transcript never makes it silent. unmeasurable is true when the workplace
// should hold a transcript but cannot show it (workplace.ErrNotMeasurable): Decide then never
// reads the nil as "no work after the Stop" (#514).
func (m *runProbe) transcript(ctx context.Context, r runs.OpenRun) (t *workplace.Transcript, unmeasurable bool, err error) {
	tr, ok := m.w.(workplace.Transcripts)
	if !ok {
		return nil, false, nil
	}
	got, err := tr.Transcript(ctx, r.WorkspaceID, r.SessionID)
	if errors.Is(err, workplace.ErrUnanswered) {
		return nil, false, err // the node did not answer: the run is not measurable, never silent
	}
	if errors.Is(err, workplace.ErrNotMeasurable) {
		return nil, true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &got, false, nil
}

// decideInput is the Input of one decide over repos, with the limits of host.toml [dispatch],
// the node of each profile, and brake; a nil brake is no brake.
func decideInput(env tickEnv, cfg hostconfig.Dispatch, repos []dispatch.RepoInput, brake *dispatch.Brake) dispatch.Input {
	return dispatch.Input{Now: env.now(), Night: cfg.Night, Repos: repos, Brake: brake,
		MaxWait: dispatch.Duration(cfg.MaxWait), MaxRuntime: dispatch.Duration(cfg.MaxRuntime),
		ProfileNodes: profileNodes(cfg)}
}

// profileNodes maps each profile that names a node to that node's node_id; nil when none
// does. The local workplace has no nodes: every run of it is on this host.
func profileNodes(cfg hostconfig.Dispatch) map[string]string {
	if cfg.Workplace == hostconfig.WorkplaceLocal {
		return nil
	}
	var out map[string]string
	for name := range cfg.Profiles {
		if id := cfg.NodeID(name); id != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[name] = id
		}
	}
	return out
}

// repoInput completes the gather of pre and returns what Decide knows of the repo. It reads
// the history only after the comments: `lighthouse run` takes no tick.lock and writes its
// run row before its Started, so a Started in the snapshot always has its row in the history
// and the orphan rule never hands back a fresh run.
func repoInput(ctx context.Context, db *sql.DB, cfg hostconfig.Dispatch, f forge.Forge, pre dispatch.Pre,
	login func() (string, error), errOut io.Writer, now time.Time) (dispatch.RepoInput, error) {
	who, err := login()
	if err != nil {
		return dispatch.RepoInput{}, err
	}
	snap, err := dispatch.Complete(ctx, f, pre, dispatch.NewTrust(f, cfg.AnswerPermissions), who, errOut, now)
	if err != nil {
		return dispatch.RepoInput{}, err
	}
	history, err := runs.History(ctx, db, pre.Repo.Slug)
	if err != nil {
		return dispatch.RepoInput{}, err
	}
	installed, err := dispatch.Installed(pre.Repo.Dir)
	if err != nil {
		return dispatch.RepoInput{}, err
	}
	return dispatch.RepoInput{
		Snapshot: snap, Controller: *pre.Repo.Controller, Language: pre.Repo.Language,
		LocationPrefix: pre.Repo.Labels.LocationPrefix, Labels: pre.Repo.Labels,
		DefaultBranch: pre.Repo.DefaultBranch, Installed: installed, History: history, Stands: pre.Stands, Runs: pre.Runs,
		Markers: pre.Markers, Unreadable: pre.Unreadable, MarkersIncomplete: pre.MarkersIncomplete,
		WorkflowPush: cfg.WorkflowPush != nil, PushTimeout: dispatch.PushTimeout(cfg),
	}, nil
}

// tokenEnvs are the token_env names of every repo in host.toml, gathered this tick or not: the
// agent of a scheduled run must not inherit any of them.
func tokenEnvs(cfg hostconfig.Dispatch) []string {
	var out []string
	for _, l := range repoconfig.LoadAll(cfg.Repos) {
		name := l.Repo.TokenEnv
		if l.Err != nil { // a repo that does not load still has a token the agent must not see
			name = repoconfig.LenientTokenEnv(l.Path)
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// cloneProblem is the one-line report, with its repair, of a kenn-forge clone of repo that is not
// bare (kennforge.CheckClone, #478); "" when the clone is sound, when there is nothing to check
// (another workplace, no HOME, no worktree to find the clone from) or when the check itself
// fails: a failing check must not hide the sweep, so it is reported as a problem of its own.
func cloneProblem(ctx context.Context, env tickEnv, cfg hostconfig.Dispatch, repo repoconfig.Repo) string {
	home := env.Getenv("HOME")
	if env.Workplace != nil || cfg.Workplace != hostconfig.WorkplaceKennForge || home == "" {
		return ""
	}
	msg, err := kennforge.CheckClone(ctx, env.runner(), home, repo.Kind, repo.Host, repo.Slug)
	if err != nil {
		return "could not check the kenn-forge clone: " + oneLine(err)
	}
	return msg
}
