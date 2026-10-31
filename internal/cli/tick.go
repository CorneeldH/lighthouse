package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// tickEnv is what tick and gather read from the machine: the environment, the path of
// host.toml, the GitHub API root and the clock. Only tests set ForgeURL and Now.
type tickEnv struct {
	Getenv   func(string) string
	HostToml string
	ForgeURL string           // "" is forge.DefaultBaseURL
	Now      func() time.Time // nil is time.Now
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

func newTickCommand() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one dispatcher tick",
		Long: "Run one dispatcher tick over the repos in host.toml [dispatch]. Each dispatchable repo gets\n" +
			"a precheck and, when something changed, a full gather. A repo that cannot be dispatched or\n" +
			"gathered is skipped loudly and tick exits 1; a tick that is still running makes this one exit 0.",
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
// dispatchable or whose gather fails; the others get gatherRepo. A busy lock is not an error.
// At least one skip returns errSilent: the reasons are already on errOut.
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

	// One GET /user per tick, and none when no repo is due. /user does not depend on the
	// repo, so the client of the first dispatchable repo serves every repo.
	var login func() (string, error)
	skipped := false
	for _, path := range cfg.Repos {
		repo, err := repoconfig.Load(path)
		if err == nil {
			err = repoconfig.Dispatchable(repo)
		}
		if err == nil {
			if login == nil {
				c := env.client(repo.Slug, token)
				login = sync.OnceValues(func() (string, error) { return tokenLogin(ctx, c) })
			}
			err = gatherRepo(ctx, env, st.DB, cfg, repo, token, login, errOut)
		}
		if err != nil {
			skipped = true
			reason := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ")
			_, _ = fmt.Fprintf(errOut, "lighthouse: skip %s: %s\n", path, reason)
		}
	}
	if skipped {
		return errSilent
	}
	return nil
}

// gatherRepo runs the precheck for repo and, when it is due, the full gather. A repo that is
// not due writes nothing. A full gather saves the fingerprint and its time and writes one
// gathered line to errOut; an error saves nothing. Until #114 decides on the snapshot, every
// full gather saves the state.
func gatherRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, repo repoconfig.Repo,
	token string, login func() (string, error), errOut io.Writer) error {
	open, err := runs.Open(ctx, db, repo.Slug)
	if err != nil {
		return err
	}
	c := env.client(repo.Slug, token)
	pre, err := dispatch.Prepare(ctx, c, repo, open)
	if err != nil {
		return err
	}
	fp := dispatch.Fingerprint(pre)
	prev, err := dispatch.LoadTickState(ctx, db, repo.Slug)
	if err != nil {
		return err
	}
	now := env.now()
	due, reason := dispatch.Due(prev, fp, now, dispatch.Boundaries(cfg, open, now))
	if !due {
		return nil
	}
	who, err := login()
	if err != nil {
		return err
	}
	snap, err := dispatch.Complete(ctx, c, pre, dispatch.NewTrust(c, cfg.AnswerPermissions), who, errOut, now)
	if err != nil {
		return err
	}
	if err := dispatch.SaveTickState(ctx, db, repo.Slug, dispatch.TickState{Fingerprint: fp, FullAt: now}, now); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(errOut, "lighthouse: gathered %s: %d items (%s)\n", repo.Slug, len(snap.Items), reason)
	return nil
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
