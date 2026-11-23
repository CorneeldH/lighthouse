package cli

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forgecache"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// forgeOpener opens the forge of each repo with that repo's token, and shares the http client
// and the token login of forges with the same kind, host, base URL and token.
type forgeOpener struct {
	getenv    func(string) string
	githubURL string                                  // override for GitHub repos only; "" is the default
	gitlabURL string                                  // API base URL for GitLab repos; only tests set it, "" is host or gitlab.com
	ghToken   func() string                           // last resort for a GitHub token (ask); nil is none
	open      func(forge.Config) (forge.Forge, error) // forge.Open; tests replace it
	http      *http.Client
	cache     *forgecache.Lazy // the GitHub ETag cache, opened on the first GitHub forRepo
	// enrichMemo sets forge.GitHub.EnrichMemo on each GitHub client it opens. Only openForges
	// sets it: its clients live one tick; serve, the pusher and postartefact read fresh.
	enrichMemo bool
	mu         sync.Mutex // guards logins
	logins     map[loginKey]*tokenLoginOnce
}

// loginKey is what the token login of a forge depends on.
type loginKey struct{ kind, host, baseURL, token string }

// repoForge is the forge of one repo with the login of its token.
type repoForge struct {
	forge.Forge
	who func() (string, error) // tokenLogin, once per loginKey
}

// newForgeOpener returns an opener that reads tokens through getenv and sends GitHub requests
// to githubURL ("" is the default API root).
func newForgeOpener(getenv func(string) string, githubURL string) *forgeOpener {
	return &forgeOpener{
		getenv:    getenv,
		githubURL: githubURL,
		open:      forge.Open,
		http:      &http.Client{Timeout: 30 * time.Second},
		cache:     forgecache.NewLazy(getenv, os.Stderr),
		logins:    map[loginKey]*tokenLoginOnce{},
	}
}

// forRepo opens the forge of repo. A token that is not set is a *forge.TokenError, also after
// ghToken for a GitHub repo gave nothing. The login of the forge is asked once per loginKey,
// on the first call of who, with ctx; see tokenLoginOnce for a cancelled ctx.
func (o *forgeOpener) forRepo(ctx context.Context, repo repoconfig.Repo) (repoForge, error) {
	token, err := forge.Token(repo.Kind, repo.TokenEnv, o.getenv)
	var te *forge.TokenError
	if errors.As(err, &te) && te.Kind == "github" && o.ghToken != nil {
		if t := o.ghToken(); t != "" {
			token, err = t, nil
		}
	}
	if err != nil {
		return repoForge{}, err
	}
	cfg := forge.Config{Kind: repo.Kind, Repo: repo.Slug, Host: repo.Host, Token: token,
		Board: repo.Board, HTTP: o.http}
	switch cfg.Kind {
	case "", "github":
		cfg.BaseURL = o.githubURL
		cfg.Cache, cfg.CacheLog = o.cache.Store(), os.Stderr
	case "gitlab":
		cfg.BaseURL = o.gitlabURL
	}
	f, err := o.open(cfg)
	if err != nil {
		return repoForge{}, err
	}
	if gh, ok := f.(*forge.GitHub); ok && o.enrichMemo {
		gh.EnrichMemo = true
	}
	key := loginKey{kind: f.Kind(), host: cfg.Host, baseURL: cfg.BaseURL, token: token}
	o.mu.Lock()
	l, ok := o.logins[key]
	if !ok {
		l = &tokenLoginOnce{}
		o.logins[key] = l
	}
	o.mu.Unlock()
	return repoForge{Forge: f, who: func() (string, error) { return l.get(ctx, f) }}, nil
}

// tokenLoginOnce is the token login of one loginKey, asked once and then kept, a failure too.
// Only a failure while the ctx of the asker is done is not kept: it says nothing of the token,
// and an opener that outlives one request (serve) must not keep it for the next.
type tokenLoginOnce struct {
	mu    sync.Mutex
	done  bool
	login string
	err   error
}

func (l *tokenLoginOnce) get(ctx context.Context, f forge.Forge) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return l.login, l.err
	}
	login, err := tokenLogin(ctx, f)
	if err != nil && ctx.Err() != nil {
		return "", err
	}
	l.done, l.login, l.err = true, login, err
	return login, err
}

// tokenLogin returns the login of the token. A 🚀 counts only from this login, so an empty
// login is an error rather than a gather in which no 🚀 counts.
func tokenLogin(ctx context.Context, f forge.Forge) (string, error) {
	login, err := f.Login(ctx)
	if err != nil {
		return "", err
	}
	if login == "" {
		return "", errors.New("GET /user: token has no login")
	}
	return login, nil
}

// openTally counts what a command opened, to decide whether it can go on: when no repo got a
// forge it fails on the first token error, and when no repo needed a forge, a GitHub token
// must still be set, as before forges were per repo.
type openTally struct {
	tried, opened bool
	tokenErr      *forge.TokenError
}

// add counts one forRepo outcome.
func (t *openTally) add(err error) {
	t.tried = true
	var te *forge.TokenError
	switch {
	case err == nil:
		t.opened = true
	case t.tokenErr == nil && errors.As(err, &te):
		t.tokenErr = te
	}
}

// err is why the command cannot go on, or nil.
func (t openTally) err(getenv func(string) string) error {
	switch {
	case t.opened:
	case t.tokenErr != nil:
		return t.tokenErr
	case !t.tried:
		if _, err := forge.Token("github", "", getenv); err != nil {
			return err
		}
	}
	return nil
}
