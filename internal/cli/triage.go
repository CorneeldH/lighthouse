package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	tri "github.com/CorneeldH/lighthouse/internal/triage"
)

// judgeFunc runs one judgment in dir with prompt on stdin and cfg, and returns claude's stdout.
type judgeFunc func(ctx context.Context, dir, prompt string, cfg judgeConfig) ([]byte, error)

// brake reports whether the round may go on; reason says why not ("usage: not measured").
type brake interface {
	Check() (reason string, open bool)
}

// triageEnv is what a triage round reads from the machine: that of tick, the judgment, the
// brake and the plugin list the skill catalog reads.
type triageEnv struct {
	tickEnv               // Getenv, HostToml, ForgeURL, Now
	Judge       judgeFunc // nil is execJudge
	Brake       brake     // required
	PluginsFile string    // "" is $HOME/.claude/plugins/installed_plugins.json
	// SettingsFile is the user's settings file the judgment takes env and modelSettings from;
	// "" is judgeSettingsPath(Getenv).
	SettingsFile string
}

// issueRef is one --issue: an issue of a repo, by slug and number.
type issueRef struct {
	Repo   string
	Number int
}

// triageOpts are the flags of a round.
type triageOpts struct {
	DryRun bool
	Issues []issueRef // only with DryRun
}

// parseIssueRef reads "<owner>/<name>#<number>".
func parseIssueRef(s string) (issueRef, error) {
	repo, num, hash := strings.Cut(s, "#")
	owner, name, slash := strings.Cut(repo, "/")
	n, err := strconv.Atoi(num)
	if !hash || !slash || owner == "" || name == "" || err != nil || n <= 0 {
		return issueRef{}, fmt.Errorf("--issue %q is not <owner>/<name>#<number>", s)
	}
	return issueRef{Repo: repo, Number: n}, nil
}

// triage runs one round over the repos of host.toml [dispatch] that have [controller.triage]:
// per repo it gathers the candidates, judges each, rechecks it and applies the outcome. Its
// errors are *exitError: 1 when the round was skipped (no repo, the brake at the start, a held
// triage.lock), 2 for any failure. A failure on one repo or issue is a line on errOut, and the
// round goes on; it then ends with 2.
func triage(ctx context.Context, env triageEnv, opts triageOpts, dbPath string, out, errOut io.Writer) error {
	if len(opts.Issues) > 0 && !opts.DryRun {
		return &exitError{code: 2, err: errors.New("--issue requires --dry-run")}
	}
	if env.Brake == nil {
		return &exitError{code: 2, err: errors.New("triage: no brake")}
	}
	lock, err := triageLock(env.Getenv)
	if errors.Is(err, flock.ErrHeld) {
		_, _ = fmt.Fprintln(errOut, "lighthouse: previous triage still running")
		return &exitError{code: 1}
	}
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	defer func() { _ = lock.Close() }()

	token := githubToken(env.Getenv)
	if token == "" {
		return &exitError{code: 2, err: errors.New("triage: no GitHub token: set GH_TOKEN or GITHUB_TOKEN")}
	}
	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	failed := false
	type enabled struct {
		path string
		repo repoconfig.Repo
	}
	var repos []enabled
	for _, path := range cfg.Repos {
		repo, err := repoconfig.Load(path)
		if err != nil {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
			continue
		}
		if repo.Triage == nil {
			continue
		}
		if len(opts.Issues) > 0 && !slices.ContainsFunc(opts.Issues, func(r issueRef) bool { return r.Repo == repo.Slug }) {
			continue
		}
		repos = append(repos, enabled{path, repo})
	}
	for _, ref := range opts.Issues {
		if !slices.ContainsFunc(repos, func(e enabled) bool { return e.repo.Slug == ref.Repo }) {
			failed = true
			_, _ = fmt.Fprintf(errOut, "lighthouse: triage %s#%d: no repo %s with [controller.triage] in [dispatch] repos\n", ref.Repo, ref.Number, ref.Repo)
		}
	}
	if len(repos) == 0 {
		if failed {
			return &exitError{code: 2}
		}
		_, _ = fmt.Fprintln(errOut, "lighthouse: no repo with [controller.triage]")
		return &exitError{code: 1}
	}
	if reason, open := env.Brake.Check(); !open {
		_, _ = fmt.Fprintf(errOut, "lighthouse: triage skipped: %s\n", reason)
		return &exitError{code: 1}
	}

	if dbPath == "" {
		if dbPath, err = store.DefaultPath(); err != nil {
			return &exitError{code: 2, err: err}
		}
	}
	st, err := store.OpenShared(ctx, dbPath)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	defer func() { _ = st.Close() }()

	r := &round{env: env, opts: opts, cfg: cfg, token: token, db: st, out: out, errOut: errOut}
	if r.env.Judge == nil {
		r.env.Judge = execJudge
	}
	for _, e := range repos {
		if r.repo(ctx, e.path, e.repo) {
			failed = true
		}
		if r.stopped {
			break
		}
	}
	if failed {
		return &exitError{code: 2}
	}
	return nil
}

// triageLock takes triage.lock in $XDG_RUNTIME_DIR/lighthouse, beside tick.lock.
func triageLock(getenv func(string) string) (*os.File, error) {
	runtime := getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return nil, errors.New("triage: XDG_RUNTIME_DIR is not set: cannot take triage.lock")
	}
	if !filepath.IsAbs(runtime) {
		return nil, errors.New("triage: XDG_RUNTIME_DIR is not an absolute path: cannot take triage.lock")
	}
	dir := filepath.Join(runtime, "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("triage: %w", err)
	}
	return flock.TryLock(filepath.Join(dir, "triage.lock"))
}

// round is one triage round over the enabled repos.
type round struct {
	env         triageEnv
	opts        triageOpts
	cfg         hostconfig.Dispatch
	token       string
	db          *store.Store
	out, errOut io.Writer
	login       func() (string, error) // GET /user, once per round
	stopped     bool                   // the brake closed: no repo goes on
}

// counts are the numbers of the summary line of one repo.
type counts struct {
	candidates, judgments, forwarded, design, other, invalid, work, skipped, errors int
}

// repo triages one repo and writes its summary line; failed reports a skipped repo or an
// issue that failed. Every judgment of the repo runs in one export of origin/<default_branch>
// with one judge config; the export is removed when repo returns, also after an error or the
// brake.
func (r *round) repo(ctx context.Context, path string, repo repoconfig.Repo) (failed bool) {
	skip := func(err error) bool {
		_, _ = fmt.Fprintf(r.errOut, "lighthouse: skip %s: %s\n", path, oneLine(err))
		return true
	}
	g, err := r.prepare(ctx, path, repo)
	if err != nil {
		return skip(err)
	}
	if g.judge, err = loadJudgeConfig(r.settingsFile()); err != nil {
		return skip(err)
	}
	ref := "origin/" + repo.DefaultBranch
	export, remove, err := exportTree(ctx, repo.Dir, ref)
	if err != nil {
		return skip(err)
	}
	defer remove()
	if err := tri.CheckRouteSkill(export); err != nil {
		return skip(fmt.Errorf("%s: %w", ref, err))
	}
	catalog, warning, err := tri.Catalog(export, repo.Dir, r.pluginsFile())
	if err != nil {
		return skip(err)
	}
	g.export = export
	if warning != "" {
		_, _ = fmt.Fprintf(r.errOut, "lighthouse: warning: %s: %s\n", repo.Slug, warning)
	}
	var n counts
	cands := r.candidates(ctx, g, &n)
	tri.Sort(cands)
	jc := tri.Context{Language: repo.Language, Labels: repo.Labels, Catalog: catalog}
	for _, f := range cands {
		if n.forwarded >= repo.Triage.MaxForwarded || !r.issue(ctx, g, f, jc, &n) {
			break
		}
	}
	_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s: candidates %d, judgments %d, forwarded %d, design %d, other %d, invalid %d, running work %d, skipped %d, errors %d\n",
		repo.Slug, n.candidates, n.judgments, n.forwarded, n.design, n.other, n.invalid, n.work, n.skipped, n.errors)
	return n.errors > 0
}

func (r *round) pluginsFile() string {
	if r.env.PluginsFile != "" {
		return r.env.PluginsFile
	}
	return filepath.Join(r.env.Getenv("HOME"), ".claude", "plugins", "installed_plugins.json")
}

func (r *round) settingsFile() string {
	if r.env.SettingsFile != "" {
		return r.env.SettingsFile
	}
	return judgeSettingsPath(r.env.Getenv)
}

// prepare checks the conditions of a repo (steps 1 and 2) and reads its lists (steps 3 and 4).
// Under --dry-run only [controller], the usage guard and answer_permissions are a warning
// line (decision 7): an error in [controller.triage], the board or its To do option still skips.
func (r *round) prepare(ctx context.Context, path string, repo repoconfig.Repo) (*repoGather, error) {
	if repo.TriageErr != nil {
		return nil, repo.TriageErr
	}
	if err := repoconfig.Triageable(repo); err != nil {
		if !r.opts.DryRun {
			return nil, err
		}
		if err := boardReady(repo); err != nil {
			return nil, err
		}
		_, _ = fmt.Fprintf(r.errOut, "lighthouse: warning: %s: %s\n", path, oneLine(err))
	}
	c := r.env.client(repo.Slug, r.token)
	if r.login == nil {
		r.login = sync.OnceValues(func() (string, error) { return tokenLogin(ctx, c) })
	}
	login, err := r.login()
	if err != nil {
		return nil, err
	}
	g := &repoGather{repo: repo, c: c, db: r.db.DB, login: login,
		trust: dispatch.NewTrust(c, r.cfg.AnswerPermissions), now: r.env.now()}
	if repo.Triage.WaitForNight {
		ok, err := g.trust.Trusted(ctx, login)
		if err != nil {
			return nil, err
		}
		if !ok {
			err := fmt.Errorf("wait_for_night: the token login %s has no role in [dispatch] answer_permissions, so the tick would not read the night handoff", login)
			if !r.opts.DryRun {
				return nil, err
			}
			_, _ = fmt.Fprintf(r.errOut, "lighthouse: warning: %s: %s\n", path, err)
		}
	}
	if err := readRepo(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// boardReady is what --dry-run still demands: a board the candidate filter can read, with the
// To do option.
func boardReady(repo repoconfig.Repo) error {
	b := repo.Board
	switch {
	case b == nil:
		return errors.New("missing [forge.board]")
	case b.ProjectID == "":
		return errors.New("forge.board.project_id is empty")
	case b.StatusFieldID == "":
		return errors.New("forge.board.status_field_id is empty")
	case b.StatusOptions["To do"] == "":
		return errors.New("missing forge.board.status_options.To do")
	}
	return nil
}

// candidates gathers the facts of the issues that pass the candidate filter, or of the
// --issue issues of the repo. An issue the cheap rules (state, milestone, status) already
// reject is not read further. Running work and gather failures get their line.
func (r *round) candidates(ctx context.Context, g *repoGather, n *counts) []tri.Facts {
	slug := g.repo.Slug
	var issues []forge.Issue
	if len(r.opts.Issues) > 0 {
		for _, ref := range r.opts.Issues {
			if ref.Repo != slug {
				continue
			}
			is, err := g.c.Issue(ctx, ref.Number)
			if err != nil {
				r.issueError(slug, ref.Number, "gather", err, n)
				continue
			}
			issues = append(issues, is)
		}
	} else {
		for _, is := range g.open {
			pre := tri.Facts{State: is.State, Milestone: is.Milestone}
			if it, ok := g.board[is.Number]; ok {
				pre.Status, _ = g.boardFields(it)
			}
			if s := tri.Candidate(pre, *g.repo.Triage, g.repo.Labels); s == tri.SkipMilestone || s == tri.SkipStatus {
				continue
			}
			issues = append(issues, is)
		}
	}
	var out []tri.Facts
	item := func(n int) (forge.BoardItem, bool, error) { return g.boardItem(ctx, n) }
	work := func() (workLists, error) { return g.work, nil }
	for _, is := range issues {
		f, err := g.gatherFacts(ctx, is, item, work, true)
		if err != nil {
			r.issueError(slug, is.Number, "gather", err, n)
			continue
		}
		if len(r.opts.Issues) == 0 {
			switch tri.Candidate(f, *g.repo.Triage, g.repo.Labels) {
			case tri.SkipNone:
			case tri.SkipWork:
				n.work++
				_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s#%d: running work: %s\n", slug, f.Number, strings.Join(f.Work, ", "))
				continue
			case tri.SkipArchived:
				n.skipped++
				_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s#%d: archived board item, skipped\n", slug, f.Number)
				continue
			default:
				continue
			}
		}
		n.candidates++
		out = append(out, f)
	}
	return out
}

func (r *round) issueError(slug string, number int, step string, err error, n *counts) {
	n.errors++
	_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s#%d: %s: %s\n", slug, number, step, oneLine(err))
}

// issue judges candidate f and applies the outcome. It returns false when the brake closed: then
// the whole round stops. Once max_judgments is spent it judges no more, but returns true, so an
// invariant issue later in the list still gets its comment.
func (r *round) issue(ctx context.Context, g *repoGather, f tri.Facts, jc tri.Context, n *counts) bool {
	repo, slug := g.repo, g.repo.Slug
	l := repo.Labels
	devs := tri.Invariant(f.Labels, l)
	var g0 *tri.Gate0
	if fixed, ok := tri.FixedGate0(f, l); ok {
		g0 = &fixed
	}
	var j *tri.Judgment
	if len(devs) == 0 {
		// Decision 1: a broken invariant costs no judgment, so only a judgment meets the limit and the brake.
		if n.judgments >= repo.Triage.MaxJudgments {
			return true
		}
		if reason, open := r.env.Brake.Check(); !open {
			_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage stopped: %s\n", reason)
			r.stopped = true
			return false
		}
		jc.Gate0 = g0
		n.judgments++
		envelope, err := r.env.Judge(ctx, g.export, tri.Prompt(f, jc), g.judge)
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			// The spec counts a timeout as an invalid judgment.
			n.invalid++
			_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s#%d: invalid judgment: %s\n", slug, f.Number, oneLine(err))
			return true
		}
		if err != nil {
			r.issueError(slug, f.Number, "judge", err, n)
			return true
		}
		valid, err := tri.Validate(envelope, f, jc)
		if err != nil {
			n.invalid++
			_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s#%d: invalid judgment: %s\n", slug, f.Number,
				strings.TrimPrefix(oneLine(err), "invalid judgment: "))
			return true
		}
		j = &valid
	}
	o := tri.Choose(f, devs, g0, j, l, tri.Target{Language: repo.Language, Worktree: repo.Dir,
		DefaultBranch: repo.DefaultBranch, WaitForNight: repo.Triage.WaitForNight})
	if r.opts.DryRun {
		if j == nil {
			_, _ = fmt.Fprintf(r.out, "%s #%d invariant\n", slug, f.Number)
		} else {
			_, _ = fmt.Fprintf(r.out, "%s %s\n", slug, j.RouteLine)
		}
		n.count(o.Row)
		return true
	}
	if step, err := r.apply(ctx, g, f, o); err != nil {
		if step == "" {
			n.skipped++
			_, _ = fmt.Fprintf(r.errOut, "lighthouse: triage %s#%d: changed since gather, skipped\n", slug, f.Number)
		} else {
			r.issueError(slug, f.Number, step, err, n)
		}
		return true
	}
	n.count(o.Row)
	return true
}

// errChanged is the recheck finding a change: apply then writes nothing.
var errChanged = errors.New("changed since gather")

// apply rechecks issue f and writes outcome o: the comment, the handoff, and for a forwarded
// issue the board item and the Controller status, in that order (spec "Volgorde per issue").
// step names the step that failed; it is "" for errChanged.
func (r *round) apply(ctx context.Context, g *repoGather, f tri.Facts, o tri.Outcome) (step string, err error) {
	after, nodeID, err := g.recheck(ctx, f.Number)
	if err != nil {
		return "gather", err
	}
	if tri.Changed(f, after) {
		return "", errChanged
	}
	if _, err := g.c.CreateComment(ctx, f.Number, o.Comment); err != nil {
		return "comment", err
	}
	if o.Handoff != "" {
		if _, err := g.c.CreateComment(ctx, f.Number, o.Handoff); err != nil {
			return "handoff", err
		}
	}
	if o.Row != tri.RowForward {
		return "", nil
	}
	b := g.repo.Board
	item := after.ItemID
	if item == "" {
		if item, err = g.c.AddToBoard(ctx, b.ProjectID, nodeID); err != nil {
			return "board", err
		}
	}
	if err := g.c.SetStatus(ctx, b.ProjectID, item, b.StatusFieldID, b.StatusOptions["Controller"]); err != nil {
		return "status", err
	}
	return "", nil
}

// count adds an applied outcome to the summary.
func (n *counts) count(row tri.Row) {
	switch row {
	case tri.RowForward:
		n.forwarded++
	case tri.RowDesign:
		n.design++
	default:
		n.other++
	}
}

// newTriageCommand is `lighthouse triage`. env builds the environment at the start of the run;
// its error is exit code 2. A panic in the round is exit code 2 too, with a line naming it, and
// so is a flag or argument error: the host unit counts 1 as success, so a typo must not pass.
func newTriageCommand(env func() (triageEnv, error)) *cobra.Command {
	var (
		opts   triageOpts
		issues []string
		dbPath string
	)
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Judge the open issues of the repos with [controller.triage]",
		Long: "Judge the open issues of the repos in host.toml [dispatch] repos that have\n" +
			"[controller.triage]: per candidate one claude judgment, then the comment, the marker and,\n" +
			"for a clear recommendation, the move to Controller. The pause flag and the usage window\n" +
			"close the round like they close a tick. Exit code 0: the round ran. 1: skipped (no repo,\n" +
			"the brake, a round still running). 2: a failure.",
		Example: "  lighthouse triage --dry-run --issue CorneeldH/lighthouse#114",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &exitError{code: 2, err: err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = &exitError{code: 2, err: fmt.Errorf("triage panicked: %v", r)}
				}
			}()
			for _, s := range issues {
				ref, perr := parseIssueRef(s)
				if perr != nil {
					return &exitError{code: 2, err: perr}
				}
				opts.Issues = append(opts.Issues, ref)
			}
			e, eerr := env()
			if eerr != nil {
				return &exitError{code: 2, err: eerr}
			}
			return triage(cmd.Context(), e, opts, dbPath, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{code: 2, err: err}
	})
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "judge, but write nothing; the route lines go to stdout")
	cmd.Flags().StringArrayVar(&issues, "issue", nil, "judge only this issue, <owner>/<name>#<number> (repeatable, needs --dry-run)")
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	return cmd
}
