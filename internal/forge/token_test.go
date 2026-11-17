package forge

import (
	"errors"
	"slices"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestTokenGitHubOrder(t *testing.T) {
	tests := []struct {
		name string
		kind string
		env  map[string]string
		want string
	}{
		{"both set", "github", map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}, "gh"},
		{"only GITHUB_TOKEN", "github", map[string]string{"GITHUB_TOKEN": "github"}, "github"},
		{"empty kind is github", "", map[string]string{"GITHUB_TOKEN": "github"}, "github"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Token(tt.kind, "", envOf(tt.env))
			if err != nil || got != tt.want {
				t.Errorf("Token = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestTokenGitLab(t *testing.T) {
	got, err := Token("gitlab", "", envOf(map[string]string{"GITLAB_TOKEN": "gl", "GH_TOKEN": "gh"}))
	if err != nil || got != "gl" {
		t.Errorf("Token = %q, %v; want gl", got, err)
	}
	_, err = Token("gitlab", "", envOf(map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "x"}))
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("GH_TOKEN counted for gitlab: err = %v", err)
	}
}

func TestTokenEnvReplacesDefaults(t *testing.T) {
	_, err := Token("github", "WORK", envOf(map[string]string{"GH_TOKEN": "gh"}))
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v, want *TokenError", err)
	}
	if !slices.Equal(te.Vars, []string{"WORK"}) {
		t.Errorf("Vars = %v, want [WORK]", te.Vars)
	}
	got, err := Token("github", "WORK", envOf(map[string]string{"WORK": "w", "GH_TOKEN": "gh"}))
	if err != nil || got != "w" {
		t.Errorf("Token = %q, %v; want w", got, err)
	}
}

func TestTokenUnknownKind(t *testing.T) {
	_, err := Token("bitbucket", "", envOf(nil))
	if err == nil {
		t.Fatal("want an error")
	}
	if errors.As(err, new(*TokenError)) {
		t.Errorf("unknown kind gave a *TokenError: %v", err)
	}
}

func TestTokenErrorText(t *testing.T) {
	tests := []struct {
		kind, tokenEnv, want string
	}{
		{"github", "", "no GitHub token: set GH_TOKEN or GITHUB_TOKEN"},
		{"gitlab", "", "no GitLab token: set GITLAB_TOKEN"},
		{"gitlab", "WORK", "no GitLab token: set WORK"},
	}
	for _, tt := range tests {
		_, err := Token(tt.kind, tt.tokenEnv, envOf(nil))
		if err == nil || err.Error() != tt.want {
			t.Errorf("Token(%q,%q) error = %v, want %q", tt.kind, tt.tokenEnv, err, tt.want)
		}
		if !errors.As(err, new(*TokenError)) {
			t.Errorf("Token(%q,%q): not a *TokenError", tt.kind, tt.tokenEnv)
		}
	}
}

// Fails if TokenVar names another variable than the one Token reads, or loses its errors.
func TestTokenVarNamesTheVariableTokenReads(t *testing.T) {
	tests := []struct {
		kind, tokenEnv string
		env            map[string]string
		want           string
	}{
		{"github", "", map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}, "GH_TOKEN"},
		{"", "", map[string]string{"GITHUB_TOKEN": "github"}, "GITHUB_TOKEN"},
		{"gitlab", "", map[string]string{"GITLAB_TOKEN": "gl"}, "GITLAB_TOKEN"},
		{"github", "WORK", map[string]string{"WORK": "w", "GH_TOKEN": "gh"}, "WORK"},
	}
	for _, tt := range tests {
		got, err := TokenVar(tt.kind, tt.tokenEnv, envOf(tt.env))
		if err != nil || got != tt.want {
			t.Errorf("TokenVar(%q, %q) = %q, %v; want %q", tt.kind, tt.tokenEnv, got, err, tt.want)
		}
	}
	_, err := TokenVar("github", "WORK", envOf(map[string]string{"GH_TOKEN": "gh"}))
	var te *TokenError
	if !errors.As(err, &te) {
		t.Errorf("err = %v, want *TokenError", err)
	}
}

// Fails if a variable Token may read is missing from TokenVarNames: the agent of a scheduled run
// would keep that token.
func TestTokenVarNamesCoversEveryTokenVariable(t *testing.T) {
	if got, want := TokenVarNames(""), []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"}; !slices.Equal(got, want) {
		t.Errorf("TokenVarNames(\"\") = %q, want %q", got, want)
	}
	if got, want := TokenVarNames("WORK"), []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "WORK"}; !slices.Equal(got, want) {
		t.Errorf("TokenVarNames(WORK) = %q, want %q", got, want)
	}
	if got := TokenVarNames("GH_TOKEN"); len(got) != 3 {
		t.Errorf("TokenVarNames(GH_TOKEN) = %q, want each name once", got)
	}
	// Every variable TokenVar can name is in the list.
	for _, kind := range []string{"github", "gitlab"} {
		for _, v := range defaultTokenVars[kind] {
			name, err := TokenVar(kind, "", envOf(map[string]string{v: "x"}))
			if err != nil || !slices.Contains(TokenVarNames(""), name) {
				t.Errorf("TokenVar(%s) = %q, %v; not in TokenVarNames", kind, name, err)
			}
		}
	}
}
