package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// directionRepo is a git repo in a temp dir with the given files, the cwd, and no env leaking in.
func directionRepo(t *testing.T, files map[string]string) {
	t.Helper()
	checkInstructionsEnv(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "-C", repo, "init", "-q")
	init.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	headings, err := os.ReadFile("../../pack/core/headings.toml")
	if err != nil {
		t.Fatal(err)
	}
	files[".lighthouse/headings.toml"] = string(headings)
	for p, c := range files {
		full := filepath.Join(repo, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(repo)
}

func runDirectionCheck(t *testing.T) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	root := NewRoot(strings.NewReader(""), &o, &e)
	root.SetArgs([]string{"direction", "check"})
	code = execute(root, &e)
	return code, o.String(), e.String()
}

const directionTOML = "[project]\nlanguage = \"en\"\n\n[paths]\nstrategy = \"s.md\"\ntenets = \"t.md\"\nareas = \"areas\"\n"

const directionLabelsTOML = directionTOML + "labels = \"labels.md\"\n\n[labels]\narea_prefix = \"2-area:\"\n"

const directionStrategy = "# S\n\n## Diagnosis\n\nx\n\n## For whom\n\nx\n\n## Guiding policy\n\n- x\n\n## Deliberately not\n\n- **S1** x\n\n## Roadmap\n\nx\n"

const directionTenets = "# T\n\n## A. One over two\n\n*Evidence*: [a](s.md), [b](s.md)\n"

const directionArea = "# A\n\n## Crux\n\nx\n\n## Guiding policy\n\n- x\n\n## Deliberately not\n\n"

// Classification: cli-output. Fails when a repo without direction keys stops being silent with
// exit 0, or a broken area heading stops giving exit 1 with path:line:, or a missing root or an
// unparsable lighthouse.toml stops giving exit 2.
func TestDirectionCheckCommand(t *testing.T) {
	t.Run("no keys is silent", func(t *testing.T) {
		directionRepo(t, map[string]string{"lighthouse.toml": "[project]\nlanguage = \"nl\"\n"})
		if code, out, errOut := runDirectionCheck(t); code != 0 || out != "" || errOut != "" {
			t.Errorf("code %d out %q err %q, want 0 and silence", code, out, errOut)
		}
	})
	t.Run("clean repo", func(t *testing.T) {
		directionRepo(t, map[string]string{"lighthouse.toml": directionTOML, "s.md": directionStrategy,
			"t.md": directionTenets, "areas/.keep": ""})
		if code, out, errOut := runDirectionCheck(t); code != 0 || out != "" || errOut != "" {
			t.Errorf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("broken area heading", func(t *testing.T) {
		directionRepo(t, map[string]string{"lighthouse.toml": directionLabelsTOML, "labels.md": "| Label | Kleur | Betekenis |\n|---|---|---|\n| `2-area:core` | `4C1D95` | core |\n", "s.md": directionStrategy,
			"t.md": directionTenets, "areas/core.md": strings.Replace(directionArea, "## Crux", "## Kern", 1)})
		code, out, _ := runDirectionCheck(t)
		if code != 1 || !strings.HasPrefix(out, "areas/core.md:3: ") {
			t.Errorf("code %d out %q, want 1 and areas/core.md:3:", code, out)
		}
	})
	t.Run("no repo root", func(t *testing.T) {
		checkInstructionsEnv(t)
		plain := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
		t.Chdir(plain)
		code, out, errOut := runDirectionCheck(t)
		if code != 2 || out != "" || !strings.Contains(errOut, "COULD NOT CHECK") {
			t.Errorf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("broken lighthouse.toml", func(t *testing.T) {
		directionRepo(t, map[string]string{"lighthouse.toml": "[paths\n"})
		code, out, errOut := runDirectionCheck(t)
		if code != 2 || out != "" || !strings.Contains(errOut, "COULD NOT CHECK") {
			t.Errorf("code %d out %q err %q", code, out, errOut)
		}
	})
}
