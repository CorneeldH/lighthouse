package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/pusher"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// pushEnv is what push-workflows reads from the machine. Only tests set ForgeURL, Workplace and
// Run.
type pushEnv struct {
	Getenv   func(string) string
	ForgeURL string // the API root of GitHub repos; "" is forge.DefaultBaseURL
	// Workplace is the workplace of the repo; nil is the one tick builds from [dispatch].
	Workplace func(repo repoconfig.Repo, node, profile string) workplace.Workplace
	// Run runs the push for the parsed flags; nil is pushWorkflows.
	Run func(ctx context.Context, env pushEnv, o pushOptions) error
}

// pushOptions are the flags of push-workflows. Tick resolves every path in its own environment
// and passes it, because the pusher's cleaned environment lacks XDG_STATE_HOME and
// LIGHTHOUSE_CONTROLLER_STATE: the pusher never resolves a default of its own.
type pushOptions struct {
	repo, worktree, permit                      string
	issue                                       int
	runID                                       int64
	db, pauseFile, cloneDir, stateDir, hostToml string
}

func newPushWorkflowsCommand() *cobra.Command {
	return pushWorkflowsCommand(pushEnv{Getenv: os.Getenv})
}

func pushWorkflowsCommand(env pushEnv) *cobra.Command {
	var o pushOptions
	cmd := &cobra.Command{
		Use:   "push-workflows",
		Short: "Push a run's workflow changes after the floor, the policy and the review (tick starts it)",
		Long: "Push the branch of a controller run whose range touches .github/workflows/. Tick starts it for a\n" +
			"handoff `next: push-workflows`, or for the answer push to the pusher's question. The pusher\n" +
			"reads the run's worktree as untrusted input and checks the SHA in its own clone: the floor\n" +
			"(branch, clean, fast-forward, own commits, roborev, pause), the policy and actionlint and\n" +
			"zizmor, and for a pre-merge change the security review. Only then does it read the workflow\n" +
			"token and push exactly that SHA. A red step posts a question and closes the push row.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.check(); err != nil {
				return err
			}
			run := env.Run
			if run == nil {
				run = pushWorkflows
			}
			return run(cmd.Context(), env, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.repo, "repo", "", "forge.repo of the repository (owner/name)")
	f.IntVar(&o.issue, "issue", 0, "issue number")
	f.StringVar(&o.worktree, "worktree", "", "worktree of the run, as the handoff names it")
	f.Int64Var(&o.runID, "run-id", 0, "id of the push row in runs")
	f.StringVar(&o.db, "db", "", "path of the SQLite database tick uses")
	f.StringVar(&o.pauseFile, "pause-file", "", "path of the pause flag tick reads")
	f.StringVar(&o.cloneDir, "clone-dir", "", "path of the pusher's own bare clone of the repository")
	f.StringVar(&o.stateDir, "state-dir", "", "tick's state directory")
	f.StringVar(&o.hostToml, "host-toml", "", "path of the host.toml tick read")
	f.StringVar(&o.permit, "permit", "", "SHA the user permitted with the answer push: the review is skipped for it")
	return cmd
}

// check refuses a missing flag or a relative path: a default would be resolved in the pusher's
// own environment, which is not tick's.
func (o pushOptions) check() error {
	var missing []string
	for _, f := range []struct{ name, value string }{
		{"repo", o.repo}, {"worktree", o.worktree}, {"db", o.db}, {"pause-file", o.pauseFile},
		{"clone-dir", o.cloneDir}, {"state-dir", o.stateDir}, {"host-toml", o.hostToml},
	} {
		if f.value == "" {
			missing = append(missing, "--"+f.name)
		} else if f.name != "repo" && !filepath.IsAbs(f.value) {
			return fmt.Errorf("push-workflows: --%s %q is not an absolute path", f.name, f.value)
		}
	}
	if o.issue <= 0 {
		missing = append(missing, "--issue")
	}
	if o.runID <= 0 {
		missing = append(missing, "--run-id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("push-workflows: missing %s: tick passes every one of them", strings.Join(missing, ", "))
	}
	return nil
}

// pushWorkflowsArgs is the command line tick starts the pusher with.
func pushWorkflowsArgs(o pushOptions) []string {
	args := []string{"push-workflows",
		"--repo", o.repo, "--issue", strconv.Itoa(o.issue), "--worktree", o.worktree,
		"--run-id", strconv.FormatInt(o.runID, 10), "--db", o.db, "--pause-file", o.pauseFile,
		"--clone-dir", o.cloneDir, "--state-dir", o.stateDir, "--host-toml", o.hostToml}
	if o.permit != "" {
		args = append(args, "--permit", o.permit)
	}
	return args
}

// pusherPaths are the database, the pause flag and the pusher's clone of slug, resolved the way
// tick and its brake resolve them: dbPath (tick's --db) or the default database, park.PauseFile,
// and lighthouse/push/<owner>/<name>.git under the state home.
func pusherPaths(getenv func(string) string, dbPath, slug string) (db, pause, clone string, err error) {
	db = dbPath
	if db == "" {
		if db, err = store.DefaultPath(); err != nil {
			return "", "", "", err
		}
	}
	if pause, err = park.PauseFile(getenv); err != nil {
		return "", "", "", err
	}
	base := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", "", "", errors.New("neither XDG_STATE_HOME nor HOME is an absolute path: no place for the pusher's clone")
		}
		base = filepath.Join(home, ".local", "state")
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || owner == ".." || name == ".." {
		return "", "", "", fmt.Errorf("forge.repo %q is not owner/name", slug)
	}
	return db, pause, filepath.Join(base, "lighthouse", "push", owner, name+".git"), nil
}

// pushGetenv reads only PATH, HOME, XDG_RUNTIME_DIR and the forge token variables: the pusher
// takes everything else from its flags.
func pushGetenv(getenv func(string) string) func(string) string {
	return func(k string) string {
		switch k {
		case "PATH", "HOME", "XDG_RUNTIME_DIR", "GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN":
			return getenv(k)
		}
		return ""
	}
}

// pushWorkflows loads the repo of o.repo from host.toml, opens its forge, the database and its
// workplace, and runs the pusher. Without [dispatch.workflow_push], or on a forge other than
// github.com, the pusher posts its question with the reason.
func pushWorkflows(ctx context.Context, env pushEnv, o pushOptions) error {
	cfg, err := hostconfig.LoadDispatch(o.hostToml)
	if err != nil {
		return err
	}
	repo, err := dispatchRepo(cfg, o.repo)
	if err != nil {
		return err
	}
	base := pushGetenv(env.Getenv)
	getenv := func(k string) string {
		if repo.TokenEnv != "" && k == repo.TokenEnv {
			return env.Getenv(k)
		}
		return base(k)
	}
	rf, err := newForgeOpener(getenv, env.ForgeURL).forRepo(ctx, repo)
	if err != nil {
		return fmt.Errorf("push-workflows: %w", err)
	}
	token, err := forge.Token(repo.Kind, repo.TokenEnv, getenv)
	if err != nil {
		return fmt.Errorf("push-workflows: %w", err)
	}
	st, err := store.OpenShared(ctx, o.db)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	te := tickEnv{Getenv: getenv, HostToml: o.hostToml, StateDir: o.stateDir, Workplace: env.Workplace}
	req := pusher.Request{
		Repo: repo.Slug, Default: repo.DefaultBranch, Worktree: o.worktree, Issue: o.issue, RunID: o.runID,
		StateDir: o.stateDir, CloneDir: o.cloneDir, PauseFile: o.pauseFile, ForgeToken: token, Permit: o.permit,
		Config: cfg.WorkflowPush, Language: repo.Language,
		Guard: []string{cfg.Forge.TokenFile, o.stateDir, o.cloneDir},
	}
	if req.URL, err = pusher.RemoteURL(repo); err != nil {
		req.Off = err.Error()
	}
	return pusher.Run(ctx, pusher.Deps{
		DB: st.DB, Forge: rf.Forge, Workplace: te.workplace(cfg, repo, o.stateDir, "", ""),
		Now: time.Now, ReadToken: pusher.ReadTokenFile,
	}, req)
}

// dispatchRepo is the repo of [dispatch] repos whose forge.repo is slug.
func dispatchRepo(cfg hostconfig.Dispatch, slug string) (repoconfig.Repo, error) {
	for _, l := range repoconfig.LoadAll(cfg.Repos) {
		if l.Err == nil && l.Repo.Slug == slug {
			return l.Repo, nil
		}
	}
	return repoconfig.Repo{}, fmt.Errorf("push-workflows: no repo %s in [dispatch] repos", slug)
}
