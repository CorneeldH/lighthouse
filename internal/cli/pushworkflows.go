package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// merge runs the merge pusher of PR pr on head sha instead of a push.
	merge bool
	pr    int
	sha   string
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
			"token and push exactly that SHA. A red step posts a question and closes the push row.\n\n" +
			"With --merge --pr <n> --sha <head> it merges that PR with the workflow token instead, only\n" +
			"when the SHA it pushed last on the PR's branch is an ancestor of the head and nothing under\n" +
			".github/workflows/ changed after it. Tick starts it for a green PR the pusher pushed to.",
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
	f.BoolVar(&o.merge, "merge", false, "merge the PR --pr on head --sha with the workflow token instead of pushing")
	f.IntVar(&o.pr, "pr", 0, "with --merge: the PR to merge")
	f.StringVar(&o.sha, "sha", "", "with --merge: the head SHA the PR must still have")
	return cmd
}

// check refuses a missing flag or a relative path: a default would be resolved in the pusher's
// own environment, which is not tick's. A push needs --worktree; a merge needs --pr and a full
// --sha instead, and takes no worktree and no permit.
func (o pushOptions) check() error {
	if err := o.checkMode(); err != nil {
		return err
	}
	var missing []string
	paths := []struct{ name, value string }{
		{"repo", o.repo}, {"db", o.db}, {"pause-file", o.pauseFile},
		{"clone-dir", o.cloneDir}, {"state-dir", o.stateDir}, {"host-toml", o.hostToml},
	}
	if !o.merge {
		paths = append(paths, struct{ name, value string }{"worktree", o.worktree})
	}
	for _, f := range paths {
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

// checkMode refuses flags of the other mode: --pr or --sha without --merge, and --worktree,
// --permit, a missing --pr or a SHA that is not a full one with it.
func (o pushOptions) checkMode() error {
	switch {
	case !o.merge && (o.pr != 0 || o.sha != ""):
		return errors.New("push-workflows: --pr and --sha go only with --merge")
	case !o.merge:
		return nil
	case o.worktree != "" || o.permit != "":
		return errors.New("push-workflows: --merge takes no --worktree and no --permit")
	case o.pr <= 0:
		return errors.New("push-workflows: --merge needs --pr")
	case !fullSHA.MatchString(o.sha):
		return fmt.Errorf("push-workflows: --sha %q is not a full SHA", o.sha)
	}
	return nil
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// pushWorkflowsArgs is the command line tick starts the pusher with.
func pushWorkflowsArgs(o pushOptions) []string {
	args := []string{"push-workflows",
		"--repo", o.repo, "--issue", strconv.Itoa(o.issue),
		"--run-id", strconv.FormatInt(o.runID, 10), "--db", o.db, "--pause-file", o.pauseFile,
		"--clone-dir", o.cloneDir, "--state-dir", o.stateDir, "--host-toml", o.hostToml}
	if o.worktree != "" {
		args = append(args, "--worktree", o.worktree)
	}
	if o.permit != "" {
		args = append(args, "--permit", o.permit)
	}
	if o.merge {
		args = append(args, "--merge")
	}
	if o.pr != 0 {
		args = append(args, "--pr", strconv.Itoa(o.pr))
	}
	if o.sha != "" {
		args = append(args, "--sha", o.sha)
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

// tokenEnvName is the variable that holds the forge token of repo: [forge] token_env, else
// GH_TOKEN for GitHub and GITLAB_TOKEN for GitLab.
func tokenEnvName(repo repoconfig.Repo) string {
	if repo.TokenEnv != "" {
		return repo.TokenEnv
	}
	if repo.Kind == "gitlab" {
		return "GITLAB_TOKEN"
	}
	return "GH_TOKEN"
}

// pushGetenv reads only PATH, HOME, XDG_RUNTIME_DIR and the forge token of repo under its own
// name: [forge] token_env, else GH_TOKEN for GitHub and GITLAB_TOKEN for GitLab. The pusher
// takes everything else from its flags.
func pushGetenv(getenv func(string) string, repo repoconfig.Repo) func(string) string {
	token := tokenEnvName(repo)
	return func(k string) string {
		switch k {
		case "PATH", "HOME", "XDG_RUNTIME_DIR", token:
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
	getenv := pushGetenv(env.Getenv, repo)
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
	deps := pusher.Deps{DB: st.DB, Forge: rf.Forge, Now: time.Now, ReadToken: pusher.ReadTokenFile}
	if o.merge {
		deps.Merger = mergeClient(repo, env.ForgeURL)
		return pusher.Merge(ctx, deps, req, o.pr, o.sha)
	}
	deps.Workplace = te.workplace(cfg, repo, o.stateDir, "", "")
	return pusher.Run(ctx, deps, req)
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

// mergeClient builds the merge pusher's own GitHub client of repo with the workflow token it is
// given: apiURL "" is forge.DefaultBaseURL.
func mergeClient(repo repoconfig.Repo, apiURL string) func(token string) pusher.Merger {
	return func(token string) pusher.Merger {
		c := forge.New(repo.Slug, token)
		if apiURL != "" {
			c.BaseURL = strings.TrimRight(apiURL, "/")
		}
		return c
	}
}
