package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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
)

// tickEnv is what tick and gather read from the machine: the environment, the path of
// host.toml, the GitHub API root, the clock, the forge workplace and the state directory.
// Only tests set ForgeURL, Now, Workplace and StateDir.
type tickEnv struct {
	Getenv   func(string) string
	HostToml string
	ForgeURL string           // "" is forge.DefaultBaseURL
	Now      func() time.Time // nil is time.Now
	// Workplace is the forge workplace of a repo; nil is a kennforge.Adapter from
	// [dispatch.forge], [dispatch.profiles] and the repo.
	Workplace func(repo repoconfig.Repo) workplace.Workplace
	StateDir  string // run instructions and carriers; "" is ${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/dispatch
	// Meter measures the usage windows; nil is a meter on usage.DefaultPaths and the [usage] table of HostToml.
	Meter *usage.Meter
	// MemInfo is the meminfo file of the memory brake; "" is /proc/meminfo.
	MemInfo string
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

// workplace returns the forge workplace of repo; stateDir is where its carriers go.
func (e tickEnv) workplace(cfg hostconfig.Dispatch, repo repoconfig.Repo, stateDir string) workplace.Workplace {
	if e.Workplace != nil {
		return e.Workplace(repo)
	}
	targets := make(map[string]string, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		targets[name] = p.TargetKey
	}
	return &kennforge.Adapter{
		Client: kennforge.New(cfg.Forge.URL, cfg.Forge.TokenFile),
		Kind:   repo.Kind, Repo: repo.Slug, MainCheckout: repo.Dir, StateDir: stateDir,
		Targets: targets, Profile: repo.Controller.Profile, Run: workplace.ExecRunner(),
	}
}

func newTickCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one dispatcher tick",
		Long: "Run one dispatcher tick over the repos in host.toml [dispatch]. Each dispatchable repo gets\n" +
			"a precheck and, when something changed, a full gather. Decide then runs once over the gathered\n" +
			"repos, and apply posts, claims and launches what it decided. A repo that cannot be dispatched or\n" +
			"gathered is skipped loudly, as is a failed action, and tick exits 1; a tick that is still running\n" +
			"makes this one exit 0.",
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
// dispatchable or whose gather fails; the others get gatherRepo, and the gathered ones
// decideAndApply. A busy lock is not an error. At least one skip or failed action returns
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
	skipped := false
	for _, path := range cfg.Repos {
		repo, err := repoconfig.Load(path)
		if err == nil {
			err = repoconfig.Dispatchable(repo)
		}
		var in *dispatch.RepoInput
		var fp string
		if err == nil {
			if login == nil {
				c := env.client(repo.Slug, token)
				login = sync.OnceValues(func() (string, error) { return tokenLogin(ctx, c) })
			}
			in, fp, err = gatherRepo(ctx, env, st.DB, cfg, facts, repo, token, login, errOut)
		}
		if err != nil {
			skipped = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
			continue
		}
		if in != nil {
			gathered = append(gathered, gatheredRepo{repo: repo, in: *in, fp: fp})
		}
	}
	failed, err := decideAndApply(ctx, env, st.DB, cfg, facts, token, gathered, errOut)
	if err != nil {
		return err
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
	actions := dispatch.Decide(dispatch.Input{Now: env.now(), Night: cfg.Night, Repos: inputs, Brake: &facts.Brake})
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
		if err := dispatch.SaveDeferred(ctx, db, g.repo.Slug, defers, env.now()); err != nil {
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
// `lighthouse run` share it.
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
		DB:        db,
		Poster:    func(slug string) dispatch.Poster { return env.client(slug, token) },
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
// A full gather writes one gathered line to errOut.
func gatherRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, facts hostFacts, repo repoconfig.Repo,
	token string, login func() (string, error), errOut io.Writer) (in *dispatch.RepoInput, fp string, err error) {
	c := env.client(repo.Slug, token)
	pre, err := prepare(ctx, db, c, repo)
	if err != nil {
		return nil, "", err
	}
	if pre.Stands, err = stands(ctx, env, cfg, repo, pre.OpenRuns, errOut); err != nil {
		return nil, "", err
	}
	pre.Markers, pre.PauseUntil = facts.Markers[repo.Slug], facts.PauseUntil
	fp = dispatch.Fingerprint(pre)
	prev, err := dispatch.LoadTickState(ctx, db, repo.Slug)
	if err != nil {
		return nil, "", err
	}
	now := env.now()
	due, reason := dispatch.Due(prev, fp, now, dispatch.Boundaries(cfg, pre.OpenRuns, pre.Markers, now))
	if !due {
		return nil, "", nil
	}
	ri, err := repoInput(ctx, db, cfg, c, pre, login, errOut, now)
	if err != nil {
		return nil, "", err
	}
	_, _ = fmt.Fprintf(errOut, "lighthouse: gathered %s: %d items (%s)\n", repo.Slug, len(ri.Snapshot.Items), reason)
	return &ri, fp, nil
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

// stands measures the session of each open run that has a workspace and a session key. A
// run whose sessions cannot be read, or whose session is gone, has no stand: not measurable.
// A read error also gets a stand line on errOut.
func stands(ctx context.Context, env tickEnv, cfg hostconfig.Dispatch, repo repoconfig.Repo,
	open []runs.OpenRun, errOut io.Writer) (map[int64]workplace.Stand, error) {
	out := map[int64]workplace.Stand{}
	var w workplace.Workplace
	for _, r := range open {
		if r.WorkspaceID == "" || r.WorkplaceSession == "" {
			continue
		}
		if w == nil {
			dir, err := env.stateDir()
			if err != nil {
				return nil, err
			}
			w = env.workplace(cfg, repo, dir)
		}
		sessions, err := w.Sessions(ctx, r.WorkspaceID)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "lighthouse: stand %s: %s\n", repo.Slug, oneLine(err))
			continue
		}
		for _, s := range sessions {
			if s.Key == r.WorkplaceSession {
				out[r.ID] = s.Stand()
				break
			}
		}
	}
	return out, nil
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
		DefaultBranch: pre.Repo.DefaultBranch, Installed: installed, History: history, Stands: pre.Stands, Markers: pre.Markers,
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
