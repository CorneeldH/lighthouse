package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/prbody"
)

// labelCreateForge records CreateLabel; any other method panics on the nil embedded interface.
type labelCreateForge struct {
	forge.Forge
	name, color string
	err         error
}

func (l *labelCreateForge) CreateLabel(_ context.Context, name, color string) error {
	l.name, l.color = name, color
	return l.err
}

// Classification: behaviour. Fails when the command changes the name (case, colon) or hands the
// forge a colour with a "#", which GitLab's forge adds itself.
func TestForgeLabelCreate(t *testing.T) {
	f := &labelCreateForge{}
	code, out, errOut := runForge(t, f, "label", "create", "1-location:Local", "--color", "0E8A16")
	if code != 0 || errOut != "" || f.name != "1-location:Local" || f.color != "0E8A16" {
		t.Fatalf("code %d, err %q, created %q %q", code, errOut, f.name, f.color)
	}
	if !strings.Contains(out, "1-location:Local") {
		t.Errorf("out %q does not name the label", out)
	}
}

func TestForgeLabelCreateAcceptsAHash(t *testing.T) {
	f := &labelCreateForge{}
	if code, _, errOut := runForge(t, f, "label", "create", "x", "--color", "#0e8a16"); code != 0 || f.color != "0e8a16" {
		t.Fatalf("code %d, err %q, colour %q", code, errOut, f.color)
	}
}

func TestForgeLabelCreateUsage(t *testing.T) {
	for _, args := range [][]string{
		{"label", "create"},
		{"label", "create", "x"},
		{"label", "create", "x", "--color", "red"},
		{"label", "create", "x", "--color", "12345"},
		{"label", "create", "a", "b", "--color", "0E8A16"},
	} {
		f := &labelCreateForge{}
		code, _, errOut := runForge(t, f, args...)
		if code != forgeExitUsage || errOut == "" || f.name != "" {
			t.Errorf("%v: code %d, err %q, created %q", args, code, errOut, f.name)
		}
	}
}

func TestForgeLabelCreateForgeError(t *testing.T) {
	f := &labelCreateForge{err: errors.New("POST /labels: 403")}
	code, _, errOut := runForge(t, f, "label", "create", "x", "--color", "0E8A16")
	if code != forgeExitForge || !strings.Contains(errOut, "403") {
		t.Fatalf("code %d, err %q", code, errOut)
	}
}

func labelsToml(t *testing.T, text string) {
	t.Helper()
	postArtefactToml(t, text)
}

// Only a loopback LIGHTHOUSE_GITHUB_API_URL is followed: the token goes with every request.
func TestLabelForgeFollowsOnlyALoopbackAPIURL(t *testing.T) {
	labelsToml(t, "[forge]\nrepo = \"o/r\"\n")
	t.Setenv("GH_TOKEN", "dummy")
	for _, tc := range []struct{ name, url, want string }{
		{"loopback", "http://127.0.0.1:9", "http://127.0.0.1:9"},
		{"elsewhere", "https://evil.example.com/api", "https://api.github.com"},
		{"unset", "", "https://api.github.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(prbody.GitHubAPIURLEnv, tc.url)
			f := defaultLabelForge()("")
			if gh, ok := f.(*forge.GitHub); !ok || gh.BaseURL != tc.want {
				t.Errorf("forge = %#v, want a GitHub with BaseURL %q", f, tc.want)
			}
		})
	}
}

// Without GH_TOKEN the token of `gh auth token` is used; without that too, or without a
// forge.repo or a readable configuration, the forge is nil and init falls back to printing.
func TestLabelForgeFallsBackToGhAuthTokenThenNil(t *testing.T) {
	labelsToml(t, "[forge]\nrepo = \"o/r\"\n")
	none := func(string) string { return "" }

	o := newForgeOpener(none, "")
	o.ghToken = func() string { return "from-gh" }
	if gh, ok := labelForgeWith(o)("").(*forge.GitHub); !ok || gh.Token != "from-gh" {
		t.Errorf("want a GitHub with the token of gh auth token")
	}

	if f := labelForgeWith(newForgeOpener(none, ""))(""); f != nil {
		t.Errorf("forge = %#v without any token, want nil", f)
	}

	labelsToml(t, "[project]\nlanguage = \"en\"\n")
	o = newForgeOpener(func(string) string { return "t" }, "")
	if f := labelForgeWith(o)(""); f != nil {
		t.Errorf("forge = %#v without forge.repo, want nil", f)
	}

	t.Setenv("LIGHTHOUSE_TOML", filepath.Join(t.TempDir(), "missing.toml"))
	if f := labelForgeWith(o)(""); f != nil {
		t.Errorf("forge = %#v without a configuration, want nil", f)
	}
}
