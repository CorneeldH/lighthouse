package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

type stubReleases struct {
	rels []forge.Release
	err  error
}

func (s stubReleases) Releases(context.Context, string) ([]forge.Release, error) {
	return s.rels, s.err
}

// pinsRepo is a git repository with a lighthouse.toml holding one tool pin, as the cwd.
func pinsRepo(t *testing.T, toml string) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

const oneToolPin = "[pins.mytool]\nkind = \"tool\"\nrepo = \"o/mytool\"\nversion = \"0.5.1\"\n"

func runPins(t *testing.T, env func(string) pins.Env, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.ResetCommands()
	root.AddCommand(newPinsCommand(env))
	root.SetArgs(append([]string{"pins"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func toolEnv(version string, rels ...forge.Release) func(string) pins.Env {
	return func(root string) pins.Env {
		return pins.Env{
			Root:             root,
			InstalledPlugins: filepath.Join(root, "none.json"),
			Releases:         stubReleases{rels: rels},
			Run: func(context.Context, string, ...string) (string, error) {
				return "mytool " + version + "\n", nil
			},
		}
	}
}

// Fails when behind/ahead exits non-zero, or an error exits zero.
func TestPinsCheckCommand_exitCode(t *testing.T) {
	rel := func(tag string) forge.Release { return forge.Release{Tag: tag} }
	tests := []struct {
		name     string
		toml     string
		env      func(string) pins.Env
		args     []string
		wantCode int
		wantOut  string
	}{
		{"behind upstream and ahead locally", oneToolPin, toolEnv("0.9.0", rel("v0.6.0"), rel("v0.5.1")), nil, 0,
			"mytool upstream behind"},
		{"upstream error", oneToolPin, toolEnv("0.5.1"), []string{"--upstream"}, 1, "mytool upstream error"},
		{"local error only", oneToolPin, toolEnv("junk"), []string{"--local"}, 1, "mytool lokaal error"},
		{"local ahead is no failure", oneToolPin, toolEnv("0.9.0"), []string{"--local"}, 0, "mytool lokaal ahead"},
		{"no pins", "[project]\nlanguage = \"nl\"\n", toolEnv("0.5.1"), nil, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinsRepo(t, tt.toml)
			code, out, errOut := runPins(t, tt.env, append([]string{"check"}, tt.args...)...)
			if code != tt.wantCode {
				t.Errorf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tt.wantCode, out, errOut)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout %q lacks %q", out, tt.wantOut)
			}
		})
	}
}

// Fails when --json does not print the machine form on stdout or when one flag still runs both.
func TestPinsCheckCommand_jsonAndFlags(t *testing.T) {
	pinsRepo(t, oneToolPin)
	code, out, _ := runPins(t, toolEnv("0.5.1", forge.Release{Tag: "v0.5.1"}), "check", "--json", "--local")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, `"local"`) || strings.Contains(out, `"upstream"`) {
		t.Errorf("--local --json = %s, want only the local measurement", out)
	}
}
