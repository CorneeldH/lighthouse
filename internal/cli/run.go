package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"sync"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
)

func newRunCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "run <owner/name>#<issue>",
		Short: "Start or resume one issue now",
		Long: "Start or resume one issue now, as the next tick would. The repo must be in host.toml\n" +
			"[dispatch] repos. run takes no tick.lock and runs no precheck: it gathers the repo, decides,\n" +
			"and applies only the close, defer, start or resume of that issue. A Returned, a Done or a 🚀\n" +
			"stays with the tick. It ends in one line: started, deferred with its reason, not accepted\n" +
			"(the workplace refused the launch), or nothing to start, each with exit 0.",
		Example: "  lighthouse run CorneeldH/lighthouse#114",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := tickEnv{Getenv: os.Getenv, HostToml: hostconfig.Path()}
			return runIssue(cmd.Context(), env, args[0], dbPath, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "path of the SQLite database (default: the lighthouse data directory)")
	return cmd
}

// runTarget is <owner/name>#<issue>, with an issue number from 1.
var runTarget = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#([1-9][0-9]*)$`)

// runKinds are the actions run applies for its issue. A close comes along: an open run that
// closes in this decide would otherwise make the claim of the start or resume fail.
var runKinds = map[dispatch.Kind]bool{
	dispatch.KindClose: true, dispatch.KindDefer: true, dispatch.KindStart: true, dispatch.KindResume: true,
}

// runIssue gathers the repo of target, decides, and applies the runKinds actions of its issue.
// It writes one result line to errOut: started, deferred or nothing to start. A failed action
// gets an apply line and returns errSilent.
func runIssue(ctx context.Context, env tickEnv, target, dbPath string, errOut io.Writer) error {
	m := runTarget.FindStringSubmatch(target)
	if m == nil {
		return fmt.Errorf("run: %s is not <owner/name>#<issue>", target)
	}
	slug := m[1]
	issue, err := strconv.Atoi(m[2])
	if err != nil {
		return fmt.Errorf("run: %s is not <owner/name>#<issue>", target)
	}
	token := githubToken(env.Getenv)
	if token == "" {
		return errors.New("run: no GitHub token: set GH_TOKEN or GITHUB_TOKEN")
	}
	cfg, err := hostconfig.LoadDispatch(env.HostToml)
	if err != nil {
		return err
	}
	repo, err := findRepo(cfg, slug, env.HostToml)
	if err != nil {
		return fmt.Errorf("run: %w", err)
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

	c := env.client(repo.Slug, token)
	pre, err := prepare(ctx, st.DB, c, repo)
	if err != nil {
		return err
	}
	if pre.Stands, err = stands(ctx, env, cfg, repo, pre.OpenRuns, errOut); err != nil {
		return err
	}
	login := sync.OnceValues(func() (string, error) { return tokenLogin(ctx, c) })
	in, err := repoInput(ctx, st.DB, cfg, c, pre, login, errOut, env.now())
	if err != nil {
		return err
	}
	var actions []dispatch.Action
	for _, a := range dispatch.Decide(dispatch.Input{Now: env.now(), Night: cfg.Night, Repos: []dispatch.RepoInput{in}}) {
		if a.Issue == issue && runKinds[a.Kind] {
			actions = append(actions, a)
		}
	}
	res, err := applyActions(ctx, env, st.DB, cfg, token, map[string]repoconfig.Repo{repo.Slug: repo}, actions, errOut)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return runResult(res, repo.Slug, issue, errOut)
}

// findRepo returns the repo of [dispatch] repos with slug; it must be dispatchable. A repo
// that does not load is skipped: it may be another one. hostToml names host.toml in the error.
func findRepo(cfg hostconfig.Dispatch, slug, hostToml string) (repoconfig.Repo, error) {
	for _, path := range cfg.Repos {
		repo, err := repoconfig.Load(path)
		if err != nil || repo.Slug != slug {
			continue
		}
		if err := repoconfig.Dispatchable(repo); err != nil {
			return repoconfig.Repo{}, fmt.Errorf("%s: %w", path, err)
		}
		return repo, nil
	}
	return repoconfig.Repo{}, fmt.Errorf("%s is not in [dispatch] repos of %s", slug, hostToml)
}

// runResult writes what apply did for slug#issue to errOut: each deferral, an apply line for
// a failed action, and then started, not accepted (the workplace refused the launch) or, when
// nothing started, deferred or failed, nothing to start. A failed action returns errSilent.
func runResult(res dispatch.Result, slug string, issue int, errOut io.Writer) error {
	for _, d := range res.Deferred {
		_, _ = fmt.Fprintf(errOut, "lighthouse: deferred %s\n", d)
	}
	failed := res.Failed[slug]
	if failed != nil {
		_, _ = fmt.Fprintf(errOut, "lighthouse: apply %s: %s\n", slug, oneLine(failed))
	}
	target := fmt.Sprintf("%s#%d", slug, issue)
	switch {
	case res.Started > 0:
		_, _ = fmt.Fprintf(errOut, "lighthouse: started %s\n", target)
	case slices.Contains(res.NotAccepted, target):
		_, _ = fmt.Fprintf(errOut, "lighthouse: not accepted %s\n", target)
	case len(res.Deferred) == 0 && failed == nil:
		_, _ = fmt.Fprintf(errOut, "lighthouse: nothing to start for %s\n", target)
	}
	if failed != nil {
		return errSilent
	}
	return nil
}
