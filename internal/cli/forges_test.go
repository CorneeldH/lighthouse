package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// spyOpener is an opener whose open records each Config and returns an inert GitHub client.
func spyOpener(vars map[string]string, githubURL string) (*forgeOpener, *[]forge.Config) {
	o := newForgeOpener(func(k string) string { return vars[k] }, githubURL)
	var got []forge.Config
	o.open = func(cfg forge.Config) (forge.Forge, error) {
		got = append(got, cfg)
		return forge.New(cfg.Repo, cfg.Token), nil
	}
	return o, &got
}

func TestOpenerOverrideOnlyForGitHub(t *testing.T) {
	o, got := spyOpener(map[string]string{"GH_TOKEN": "gh-secret", "GITLAB_TOKEN": "gl-secret"}, "http://override")
	board := &repoconfig.Board{ProjectID: "PVT_x"}
	ctx := context.Background()
	if _, err := o.forRepo(ctx, repoconfig.Repo{Slug: "g/l", Kind: "gitlab", Host: "gitlab.example", Board: board}); err != nil {
		t.Fatalf("gitlab forRepo = %v", err)
	}
	if _, err := o.forRepo(ctx, repoconfig.Repo{Slug: "o/r", Kind: "github", Board: board}); err != nil {
		t.Fatalf("github forRepo = %v", err)
	}
	if len(*got) != 2 {
		t.Fatalf("open calls = %d, want 2", len(*got))
	}
	gl, gh := (*got)[0], (*got)[1]
	if gl.Kind != "gitlab" || gl.BaseURL != "" || gl.Repo != "g/l" || gl.Host != "gitlab.example" || gl.Token != "gl-secret" || gl.Board != board {
		t.Errorf("gitlab Config = %+v, want kind gitlab, no BaseURL, repo g/l, host gitlab.example, the GitLab token and its board", gl)
	}
	if gh.Kind != "github" || gh.BaseURL != "http://override" || gh.Repo != "o/r" || gh.Token != "gh-secret" || gh.Board != board {
		t.Errorf("github Config = %+v, want kind github, BaseURL http://override, repo o/r, the GitHub token and its board", gh)
	}
	if gl.HTTP == nil || gl.HTTP != gh.HTTP {
		t.Errorf("HTTP clients %p and %p, want one shared client", gl.HTTP, gh.HTTP)
	}
}

func TestOpenerTokenPerKind(t *testing.T) {
	for name, tc := range map[string]struct {
		repo    repoconfig.Repo
		ghToken func() string
		want    string // the token open gets; "" is a *forge.TokenError
		wantErr string
	}{
		"github":                      {repo: repoconfig.Repo{Slug: "o/r", Kind: "github"}, want: "gh-secret"},
		"gitlab":                      {repo: repoconfig.Repo{Slug: "g/l", Kind: "gitlab"}, want: "gl-secret"},
		"gitlab token_env unset":      {repo: repoconfig.Repo{Slug: "g/l", Kind: "gitlab", TokenEnv: "WORK"}, wantErr: "no GitLab token: set WORK"},
		"github token_env unset, ask": {repo: repoconfig.Repo{Slug: "o/r", Kind: "github", TokenEnv: "WORK"}, ghToken: func() string { return "asked" }, want: "asked"},
		"gitlab token_env unset, ask": {repo: repoconfig.Repo{Slug: "g/l", Kind: "gitlab", TokenEnv: "WORK"}, ghToken: func() string { return "asked" }, wantErr: "no GitLab token: set WORK"},
		"github none, ask empty":      {repo: repoconfig.Repo{Slug: "o/r", Kind: "github", TokenEnv: "WORK"}, ghToken: func() string { return "" }, wantErr: "no GitHub token: set WORK"},
	} {
		t.Run(name, func(t *testing.T) {
			o, got := spyOpener(map[string]string{"GH_TOKEN": "gh-secret", "GITLAB_TOKEN": "gl-secret"}, "")
			o.ghToken = tc.ghToken
			_, err := o.forRepo(context.Background(), tc.repo)
			if tc.wantErr != "" {
				var te *forge.TokenError
				if !errors.As(err, &te) || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want *forge.TokenError %q", err, tc.wantErr)
				}
				for _, secret := range []string{"gh-secret", "gl-secret", "asked"} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("err %q contains a token", err)
					}
				}
				if len(*got) != 0 {
					t.Errorf("open called %d times after a token error", len(*got))
				}
				return
			}
			if err != nil {
				t.Fatalf("forRepo = %v", err)
			}
			if len(*got) != 1 || (*got)[0].Token != tc.want {
				t.Errorf("open got %+v, want one Config with token %q", *got, tc.want)
			}
		})
	}
}

func TestOpenerSharesLogin(t *testing.T) {
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	o := newForgeOpener(func(k string) string {
		return map[string]string{"GH_TOKEN": "tok", "OTHER": "tok2"}[k]
	}, srv.URL())
	ctx := context.Background()
	for i, repo := range []repoconfig.Repo{
		{Slug: "o/r", Kind: "github"},
		{Slug: "o/s", Kind: "github"},
		{Slug: "o/t", Kind: "github", TokenEnv: "OTHER"},
	} {
		rf, err := o.forRepo(ctx, repo)
		if err != nil {
			t.Fatalf("forRepo(%s) = %v", repo.Slug, err)
		}
		if who, err := rf.who(); err != nil || who != "lh-bot" {
			t.Fatalf("who(%s) = %q, %v", repo.Slug, who, err)
		}
		if want := []int{1, 1, 2}[i]; srv.Count("/user") != want {
			t.Errorf("after %s: /user requests = %d, want %d (one per token)", repo.Slug, srv.Count("/user"), want)
		}
	}
}

// A login that failed because the ctx of its forRepo call was cancelled is not kept: a later
// forRepo with the same token, on a live ctx, asks GET /user again. The opener is shared across
// the tick and triage calls, so one cancelled call must not fail every later login.
func TestOpenerLoginForgetsCancelledCtx(t *testing.T) {
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	o := newForgeOpener(func(k string) string { return map[string]string{"GH_TOKEN": "tok"}[k] }, srv.URL())
	repo := repoconfig.Repo{Slug: "o/r", Kind: "github"}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	rf, err := o.forRepo(cancelled, repo)
	if err != nil {
		t.Fatalf("forRepo = %v", err)
	}
	if _, err := rf.who(); !errors.Is(err, context.Canceled) {
		t.Fatalf("who on a cancelled ctx = %v, want context.Canceled", err)
	}

	rf, err = o.forRepo(context.Background(), repo)
	if err != nil {
		t.Fatalf("forRepo = %v", err)
	}
	if who, err := rf.who(); err != nil || who != "lh-bot" {
		t.Fatalf("who on a live ctx = %q, %v; want lh-bot", who, err)
	}
	if n := srv.Count("/user"); n != 1 {
		t.Errorf("/user requests = %d, want 1: the cancelled one never reached the server", n)
	}
}

// A login that failed on a live ctx is kept: the tick asks GET /user once per token, also when
// it fails.
func TestOpenerLoginKeepsFailure(t *testing.T) {
	srv := forgetest.New(t)
	srv.Login = ""
	o := newForgeOpener(func(k string) string { return map[string]string{"GH_TOKEN": "tok"}[k] }, srv.URL())
	for _, slug := range []string{"o/r", "o/s"} {
		rf, err := o.forRepo(context.Background(), repoconfig.Repo{Slug: slug, Kind: "github"})
		if err != nil {
			t.Fatalf("forRepo(%s) = %v", slug, err)
		}
		if _, err := rf.who(); err == nil {
			t.Fatalf("who(%s) = nil error, want the empty-login error", slug)
		}
	}
	if n := srv.Count("/user"); n != 1 {
		t.Errorf("/user requests = %d, want 1", n)
	}
}

func TestForgeOpenerPassesCache(t *testing.T) {
	ctx := context.Background()
	gh := repoconfig.Repo{Slug: "o/r"}
	t.Run("with a state dir", func(t *testing.T) {
		o, got := spyOpener(map[string]string{"GH_TOKEN": "t", "XDG_STATE_HOME": t.TempDir()}, "")
		if _, err := o.forRepo(ctx, gh); err != nil {
			t.Fatal(err)
		}
		if (*got)[0].Cache == nil || (*got)[0].CacheLog == nil {
			t.Errorf("github Config Cache/CacheLog = %v/%v, want both set", (*got)[0].Cache, (*got)[0].CacheLog)
		}
	})
	t.Run("without a state dir", func(t *testing.T) {
		o, got := spyOpener(map[string]string{"GH_TOKEN": "t"}, "")
		if _, err := o.forRepo(ctx, gh); err != nil {
			t.Fatal(err)
		}
		if (*got)[0].Cache != nil {
			t.Errorf("Cache = %v, want nil", (*got)[0].Cache)
		}
	})
	t.Run("gitlab gets none", func(t *testing.T) {
		o, got := spyOpener(map[string]string{"GITLAB_TOKEN": "t", "XDG_STATE_HOME": t.TempDir()}, "")
		if _, err := o.forRepo(ctx, repoconfig.Repo{Slug: "g/l", Kind: "gitlab", Host: "gitlab.example"}); err != nil {
			t.Fatal(err)
		}
		if (*got)[0].Cache != nil {
			t.Errorf("gitlab Cache = %v, want nil", (*got)[0].Cache)
		}
	})
}

func TestForgeOpenerOpensCacheLazily(t *testing.T) {
	state := t.TempDir()
	o, _ := spyOpener(map[string]string{"GH_TOKEN": "t", "XDG_STATE_HOME": state}, "")
	list := func() int {
		es, err := os.ReadDir(state)
		if err != nil {
			t.Fatal(err)
		}
		return len(es)
	}
	if n := list(); n != 0 {
		t.Fatalf("newForgeOpener created %d entries under the state dir", n)
	}
	if _, err := o.forRepo(context.Background(), repoconfig.Repo{Slug: "o/r"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "lighthouse", "forge-cache.db")); err != nil {
		t.Errorf("first github forRepo left no cache file: %v", err)
	}
}
