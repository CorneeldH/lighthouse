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
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
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
	ForgeURL string           // "" is forge.DefaultBaseURL
	Now      func() time.Time // nil is time.Now
	// Workplace is the workplace of a repo; nil follows [dispatch] workplace: "local" is a
	// local.Adapter, any other value a kennforge.Adapter from [dispatch.forge],
	// [dispatch.profiles] and the repo.
	Workplace func(repo repoconfig.Repo) workplace.Workplace
	StateDir  string // run instructions, carriers and permission notes; "" is ${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/dispatch
	// Meter measures the usage windows; nil is a meter on usage.DefaultPaths and the [usage] table of HostToml.
	Meter *usage.Meter
	// MemInfo is the meminfo file of the memory brake; "" is /proc/meminfo.
	MemInfo string
	// Run runs git for the workplace and the sweep; nil is workplace.ExecRunner.
	Run workplace.Runner
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

// client returns a GitHub client for repo at ForgeURL.
func (e tickEnv) client(repo, token string) *forge.Client {
	c := forge.New(repo, token)
	if e.ForgeURL != "" {
		c.BaseURL = e.ForgeURL
	}
	return c
}

// stateDir is StateDir, or lighthouse/dispatch under XDG_STATE_HOME, or under
// $HOME/.local/state when XDG_STATE_HOME is not an absolute path.
func (e tickEnv) stateDir() (string, error) {
	if e.StateDir != "" {
		return e.StateDir, nil
	}
	base := e.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := e.Getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("neither XDG_STATE_HOME nor HOME is an absolute path: no place for the run instructions")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "lighthouse", "dispatch"), nil
}

// workplace returns the workplace of repo; stateDir is where its carriers go.
func (e tickEnv) workplace(cfg hostconfig.Dispatch, repo repoconfig.Repo, stateDir string) workplace.Workplace {
	if e.Workplace != nil {
		return e.Workplace(repo)
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
		Targets: targets, Profile: repo.Controller.Profile, Run: e.runner(),
		Home: e.Getenv("HOME"), UnitPrefix: cfg.Forge.UnitPrefix,
	}
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

// tick takes tick.lock, loads the repos of host.toml and skips, loudly, each one that is not
// dispatchable or whose gather fails; the others get gatherRepo, the gathered ones
// decideAndApply, and every repo whose precheck passed saveRunViews and sweepRepos, due or not.
// The run views follow apply, so a run this tick starts or closes shows at once. A busy lock is
// not an error. At least one skip, failed action, unsaved run view or failed cleanup returns
// errSilent: the reasons are already on errOut.
func tick(ctx context.Context, env tickEnv, dbPath string, errOut io.Writer) error {
	runtime := env.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return errors.New("tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock")
	}
	if !filepath.IsAbs(runtime) {
		return errors.New("tick: XDG_RUNTIME_DIR is not an absolute path: cannot take tick.lock")
	}
	dir := filepath.Join(runtime, "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tick: %w", err)
	}
	lock, err := flock.TryLock(filepath.Join(dir, "tick.lock"))
	if errors.Is(err, flock.ErrHeld) {
		_, _ = fmt.Fprintln(errOut, "lighthouse: previous tick still running")
		return nil
	}
	if err != nil {
		return fmt.Errorf("tick: %w", err)
	}
	defer func() { _ = lock.Close() }()

	token := githubToken(env.Getenv)
	if token == "" {
		return errors.New("tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN")
	}
	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return err
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
	st, err := store.OpenShared(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	facts, err := readHostFacts(ctx, env, st.DB, cfg)
	if err != nil {
		return fmt.Errorf("tick: the brake cannot check: %w", err)
	}

	// One GET /user per tick, and none when no repo is due. /user does not depend on the
	// repo, so the client of the first dispatchable repo serves every repo.
	var login func() (string, error)
	var gathered []gatheredRepo
	var checked []checkedRepo
	skipped := false
	for _, path := range cfg.Repos {
		repo, err := repoconfig.Load(path)
		if err == nil {
			err = repoconfig.Dispatchable(repo)
		}
		var in *dispatch.RepoInput
		var fp string
		var pre dispatch.Pre
		if err == nil {
			if login == nil {
				c := env.client(repo.Slug, token)
				login = sync.OnceValues(func() (string, error) { return tokenLogin(ctx, c) })
			}
			in, fp, pre, err = gatherRepo(ctx, env, st.DB, cfg, facts, repo, token, login, errOut)
		}
		if err != nil {
			skipped = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
			continue
		}
		checked = append(checked, checkedRepo{repo: repo, pre: pre})
		if in != nil {
			gathered = append(gathered, gatheredRepo{repo: repo, in: *in, fp: fp})
		}
	}
	failed, err := decideAndApply(ctx, env, st.DB, cfg, facts, token, gathered, errOut)
	if err != nil {
		return err
	}
	if saveRunViews(ctx, env, st.DB, checked, errOut) {
		failed = true
	}
	if sweepRepos(ctx, env, st.DB, cfg, token, checked, errOut) {
		failed = true
	}
	if skipped || failed {
		return errSilent
	}
	return nil
}

// oneLine is the text of err on one line.
func oneLine(err error) string {
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ")
}

// gatheredRepo is a repo whose full gather ran in this tick, with its precheck fingerprint.
type gatheredRepo struct {
	repo repoconfig.Repo
	in   dispatch.RepoInput
	fp   string
}

// checkedRepo is a repo whose precheck passed in this tick, with that precheck.
type checkedRepo struct {
	repo repoconfig.Repo
	pre  dispatch.Pre
}

// saveRunViews replaces the run view of each repo with its open and parked runs as they stand
// after apply, with the titles and measured stands of its precheck. Each repo whose view was not
// saved gets a save run view line on errOut; failed reports one.
func saveRunViews(ctx context.Context, env tickEnv, db *sql.DB, repos []checkedRepo, errOut io.Writer) (failed bool) {
	if len(repos) == 0 {
		return false
	}
	markerDir, err := park.MarkerDir(env.Getenv)
	var parked map[string][]runs.Row
	if err == nil {
		_, parked, err = controllerMarkers(ctx, db, markerDir)
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
// longer holds go, when nothing is lost by it (workplace.Cleanup). Each removed
// workspace gets a removed line on errOut, each repo whose sweep failed a cleanup line; a
// workspace it keeps gets no line: it is in Workspaces to clean up. A repo without a
// workspace to sweep builds no workplace. failed reports a cleanup line.
func sweepRepos(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, token string,
	repos []checkedRepo, errOut io.Writer) (failed bool) {
	for _, sr := range repos {
		slug := sr.repo.Slug
		removed, err := sweepOne(ctx, env, db, cfg, token, sr)
		for _, l := range removed {
			_, _ = fmt.Fprintf(errOut, "lighthouse: removed workspace %s of %s#%d\n", l.WorkspaceID, slug, l.Issue)
		}
		if err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: cleanup %s: %s\n", slug, oneLine(err))
		}
	}
	return failed
}

// sweepOne sweeps one repo; see sweepRepos.
func sweepOne(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, token string,
	sr checkedRepo) ([]workplace.Leftover, error) {
	refs, err := runs.Workspaces(ctx, db, sr.repo.Slug)
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	dir, err := env.stateDir()
	if err != nil {
		return nil, err
	}
	c := env.client(sr.repo.Slug, token)
	in := workplace.SweepInput{
		Repo:         sr.repo.Slug,
		InController: dispatch.InController(sr.pre.Statuses),
		MergedHead: func(ctx context.Context, branch string) (string, error) {
			prs, err := c.BranchPRs(ctx, branch)
			if err != nil {
				return "", err
			}
			return dispatch.MergedHead(prs), nil
		},
	}
	return workplace.Sweep(ctx, db, env.workplace(cfg, sr.repo, dir), env.runner(), in, env.now())
}

// decideAndApply decides once over the gathered repos and applies the actions. Each deferral
// gets a deferred line on errOut, each repo with a failed action an apply line. It saves the
// fingerprint of a repo only when none of its actions was deferred or failed, so the next tick
// tries again (spec §3 step 2), and replaces the deferred rows of each gathered repo with the
// deferrals of Decide. failed reports an apply line or a state that was not saved.
func decideAndApply(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, facts hostFacts, token string,
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
	res, err := applyActions(ctx, env, db, cfg, token, repos, actions, errOut)
	if err != nil {
		return false, fmt.Errorf("tick: %w", err)
	}
	for _, g := range gathered {
		var defers []dispatch.Action
		for _, a := range actions {
			if a.Kind == dispatch.KindDefer && a.Repo == g.repo.Slug {
				defers = append(defers, a)
			}
		}
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
		if slices.ContainsFunc(res.Deferred, func(d string) bool { return strings.HasPrefix(d, slug+"#") }) {
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

// applyActions applies actions to repos, each with its forge workplace; apply logs to errOut
// with the "lighthouse: " of every tick line. Without actions it applies nothing. tick and
// `lighthouse run` share it. Every repo in repos has a board: Dispatchable guarantees it.
func applyActions(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, token string,
	repos map[string]repoconfig.Repo, actions []dispatch.Action, errOut io.Writer) (dispatch.Result, error) {
	if len(actions) == 0 {
		return dispatch.Result{}, nil
	}
	dir, err := env.stateDir()
	if err != nil {
		return dispatch.Result{}, err
	}
	markerDir, err := park.MarkerDir(env.Getenv)
	if err != nil {
		return dispatch.Result{}, err
	}
	places := make(map[string]workplace.Workplace, len(repos))
	for slug, repo := range repos {
		places[slug] = env.workplace(cfg, repo, dir)
	}
	// Apply's joined error is dropped on purpose: the same errors are in res.Failed, per
	// repo: the callers write their apply lines from it, and tick decides which fingerprints
	// it saves.
	res, _ := dispatch.Apply(ctx, dispatch.Env{
		DB:     db,
		Poster: func(slug string) dispatch.Poster { return env.client(slug, token) },
		Board: func(slug string) dispatch.Board {
			return dispatch.NewBoard(env.client(slug, token), *repos[slug].Board)
		},
		Workplace: func(slug string) workplace.Workplace { return places[slug] },
		Profile: func(slug string) (string, string) {
			name := repos[slug].Controller.Profile
			return name, cfg.Profiles[name].MCPConfig
		},
		RoundLimit: func(slug string) string { return repos[slug].Controller.AtRoundLimit },
		StateDir:   dir,
		MarkerDir:  markerDir,
		MaxWait:    cfg.MaxWait,
		Now:        env.now,
		NewSession: runs.NewSessionID,
		Log:        prefixed{errOut},
	}, actions)
	return res, nil
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
// pre is the precheck, also when the repo is not due: tick builds the run view and the sweep
// from it after apply. A full gather writes one gathered line to errOut.
func gatherRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, facts hostFacts, repo repoconfig.Repo,
	token string, login func() (string, error), errOut io.Writer) (in *dispatch.RepoInput, fp string, pre dispatch.Pre, err error) {
	c := env.client(repo.Slug, token)
	pre, err = measuredPre(ctx, env, db, cfg, c, repo, errOut)
	if err != nil {
		return nil, "", dispatch.Pre{}, err
	}
	pre.Markers, pre.PauseUntil = facts.Markers[repo.Slug], facts.PauseUntil
	fp = dispatch.Fingerprint(pre)
	prev, err := dispatch.LoadTickState(ctx, db, repo.Slug)
	if err != nil {
		return nil, "", dispatch.Pre{}, err
	}
	now := env.now()
	due, reason := dispatch.Due(prev, fp, now, dispatch.Boundaries(cfg, pre.OpenRuns, pre.Runs, pre.Markers, now))
	if !due {
		return nil, "", pre, nil
	}
	ri, err := repoInput(ctx, db, cfg, c, pre, login, errOut, now)
	if err != nil {
		return nil, "", dispatch.Pre{}, err
	}
	_, _ = fmt.Fprintf(errOut, "lighthouse: gathered %s: %d items (%s)\n", repo.Slug, len(ri.Snapshot.Items), reason)
	return &ri, fp, pre, nil
}

// prepare is the board half of a gather: the open runs, the branch of each issue's last run,
// and the board with the PRs of those branches.
func prepare(ctx context.Context, db *sql.DB, c *forge.Client, repo repoconfig.Repo) (dispatch.Pre, error) {
	open, err := runs.Open(ctx, db, repo.Slug)
	if err != nil {
		return dispatch.Pre{}, err
	}
	history, err := runs.History(ctx, db, repo.Slug)
	if err != nil {
		return dispatch.Pre{}, err
	}
	branches := map[int]string{}
	for _, r := range history { // ordered by id: the last run of an issue wins
		branches[r.Issue] = r.Branch
	}
	return dispatch.Prepare(ctx, c, repo, open, branches)
}

// measuredPre is the precheck half of a gather that tick and `lighthouse run` share: it
// adopts the sessions of uncertain starts, reads the board half (prepare), and measures the
// open runs. Adoption comes first, so the open runs prepare reads already hold a session that
// showed up in this tick.
func measuredPre(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, c *forge.Client,
	repo repoconfig.Repo, errOut io.Writer) (dispatch.Pre, error) {
	if err := adopt(ctx, env, db, cfg, repo, errOut); err != nil {
		return dispatch.Pre{}, err
	}
	pre, err := prepare(ctx, db, c, repo)
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
	if err := workplace.AdoptSessions(ctx, db, env.workplace(cfg, repo, dir), repo.Slug); err != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: measure %s: %s\n", repo.Slug, oneLine(err))
	}
	return nil
}

// measure measures each open run that has a workspace: the stand of its session (only with a
// session key) and its RunState. A run it cannot measure has neither, and gets a measure line
// on errOut; the other runs are still measured. The error is only for a missing state
// directory.
func measure(ctx context.Context, env tickEnv, cfg hostconfig.Dispatch, repo repoconfig.Repo,
	open []runs.OpenRun, errOut io.Writer) (map[int64]workplace.Stand, map[int64]dispatch.RunState, error) {
	stands := map[int64]workplace.Stand{}
	states := map[int64]dispatch.RunState{}
	var m *runProbe
	for _, r := range open {
		if r.WorkspaceID == "" {
			continue
		}
		if m == nil {
			dir, err := env.stateDir()
			if err != nil {
				return nil, nil, err
			}
			m = &runProbe{w: env.workplace(cfg, repo, dir), stateDir: dir}
		}
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
// one on this host and its permission note. A workspace the workplace no
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
		if s.Transcript, err = m.transcript(ctx, r); err != nil {
			return 0, false, dispatch.RunState{}, err
		}
	}
	if s.Permission, err = dispatch.ReadPermission(m.stateDir, r.SessionID); err != nil {
		return 0, false, dispatch.RunState{}, err
	}
	return stand, measured, s, nil
}

// transcript is the transcript of the run's session, or nil when it is not measurable: a
// workplace without transcripts, no transcript directory on this host, no transcript file yet
// (an uncertain start whose agent never ran), or a transcript without a timestamp.
func (m *runProbe) transcript(ctx context.Context, r runs.OpenRun) (*workplace.Transcript, error) {
	tr, ok := m.w.(workplace.Transcripts)
	if !ok {
		return nil, nil
	}
	t, err := tr.Transcript(ctx, r.WorkspaceID, r.SessionID)
	if errors.Is(err, workplace.ErrNotMeasurable) || errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// decideInput is the Input of one decide over repos, with the limits of host.toml [dispatch]
// and brake; a nil brake is no brake.
func decideInput(env tickEnv, cfg hostconfig.Dispatch, repos []dispatch.RepoInput, brake *dispatch.Brake) dispatch.Input {
	return dispatch.Input{Now: env.now(), Night: cfg.Night, Repos: repos, Brake: brake,
		MaxWait: dispatch.Duration(cfg.MaxWait), MaxRuntime: dispatch.Duration(cfg.MaxRuntime)}
}

// repoInput completes the gather of pre and returns what Decide knows of the repo. It reads
// the history only after the comments: `lighthouse run` takes no tick.lock and writes its
// run row before its Started, so a Started in the snapshot always has its row in the history
// and the orphan rule never hands back a fresh run.
func repoInput(ctx context.Context, db *sql.DB, cfg hostconfig.Dispatch, c *forge.Client, pre dispatch.Pre,
	login func() (string, error), errOut io.Writer, now time.Time) (dispatch.RepoInput, error) {
	who, err := login()
	if err != nil {
		return dispatch.RepoInput{}, err
	}
	snap, err := dispatch.Complete(ctx, c, pre, dispatch.NewTrust(c, cfg.AnswerPermissions), who, errOut, now)
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
		DefaultBranch: pre.Repo.DefaultBranch, Installed: installed, History: history, Stands: pre.Stands, Runs: pre.Runs,
		Markers: pre.Markers,
	}, nil
}

// tokenLogin returns the login of the token. A 🚀 counts only from this login, so an empty
// login is an error rather than a gather in which no 🚀 counts.
func tokenLogin(ctx context.Context, c *forge.Client) (string, error) {
	login, err := c.Login(ctx)
	if err != nil {
		return "", err
	}
	if login == "" {
		return "", errors.New("GET /user: token has no login")
	}
	return login, nil
}
