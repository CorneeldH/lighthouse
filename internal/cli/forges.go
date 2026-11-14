package cli

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// forgeOpener opens the forge of each repo with that repo's token, and shares the http client
// and the token login of forges with the same kind, host, base URL and token.
type forgeOpener struct {
	getenv    func(string) string
	githubURL string                                  // override for GitHub repos only; "" is the default
	ghToken   func() string                           // last resort for a GitHub token (ask); nil is none
	open      func(forge.Config) (forge.Forge, error) // forge.Open; tests replace it
	http      *http.Client
	logins    map[loginKey]func() (string, error)
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
		logins:    map[loginKey]func() (string, error){},
	}
}

// forRepo opens the forge of repo. A token that is not set is a *forge.TokenError, also after
// ghToken for a GitHub repo gave nothing. The login of the forge is asked once per loginKey,
// on the first call of who.
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
	if cfg.Kind == "" || cfg.Kind == "github" {
		cfg.BaseURL = o.githubURL
	}
	f, err := o.open(cfg)
	if err != nil {
		return repoForge{}, err
	}
	key := loginKey{kind: f.Kind(), host: cfg.Host, baseURL: cfg.BaseURL, token: token}
	who, ok := o.logins[key]
	if !ok {
		who = sync.OnceValues(func() (string, error) { return tokenLogin(ctx, f) })
		o.logins[key] = who
	}
	return repoForge{Forge: f, who: who}, nil
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
