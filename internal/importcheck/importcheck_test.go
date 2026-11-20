package importcheck

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeChecker func(p Project, env Env) ([]Finding, error)

func (f fakeChecker) Check(p Project, env Env) ([]Finding, error) { return f(p, env) }

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

func run(t *testing.T, o Options) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(o, Env{Root: o.Root, Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func TestRunExitCodes(t *testing.T) {
	finding := Finding{File: "a.py", Line: 1, ID: "DEP003", Import: "numpy", Reason: "transitive"}
	tests := []struct {
		name       string
		result     func() ([]Finding, error)
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"finding", func() ([]Finding, error) { return []Finding{finding}, nil }, 1,
			"a.py:1: DEP003 numpy — transitive\n", ""},
		{"unchecked", func() ([]Finding, error) { return nil, UncheckedError("no uv") }, 3,
			"", "COULD NOT CHECK — no uv\n"},
		{"clean", func() ([]Finding, error) { return nil, nil }, 0, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := gitRepo(t, map[string]string{"pyproject.toml": "", "a.py": ""})
			c := fakeChecker(func(Project, Env) ([]Finding, error) { return tt.result() })
			code, stdout, stderr := run(t, Options{Root: root, Langs: []Lang{"python"}, Checkers: map[Lang]Checker{"python": c}})
			if code != tt.wantCode || stdout != tt.wantStdout || stderr != tt.wantStderr {
				t.Errorf("got %d %q %q, want %d %q %q", code, stdout, stderr, tt.wantCode, tt.wantStdout, tt.wantStderr)
			}
		})
	}
}

func TestRunFindingsWinOverUnchecked(t *testing.T) {
	root := gitRepo(t, map[string]string{"pyproject.toml": "", "go.mod": "", "a.py": ""})
	py := fakeChecker(func(Project, Env) ([]Finding, error) {
		return []Finding{{File: "a.py", Line: 2, ID: "X", Import: "m", Reason: "r"}}, nil
	})
	goc := fakeChecker(func(Project, Env) ([]Finding, error) { return nil, UncheckedError("no go") })
	code, stdout, stderr := run(t, Options{Root: root, Langs: []Lang{"python", "go"},
		Checkers: map[Lang]Checker{"python": py, "go": goc}})
	if code != 1 || stdout != "a.py:2: X m — r\n" || stderr != "COULD NOT CHECK — no go\n" {
		t.Errorf("got %d %q %q", code, stdout, stderr)
	}
}

func TestRunLangWithoutCheckerIsUnchecked(t *testing.T) {
	root := gitRepo(t, map[string]string{"go.mod": ""})
	code, stdout, stderr := run(t, Options{Root: root, Langs: []Lang{"go"}})
	if code != 3 || stdout != "" || !strings.HasPrefix(stderr, "COULD NOT CHECK — ") {
		t.Errorf("got %d %q %q", code, stdout, stderr)
	}
}

func TestRunScopesToFiles(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"pyproject.toml": "", "uv.lock": "", "a.py": "", "b.py": "",
		"web/pyproject.toml": "", "web/x.ts": "",
	})
	var ran []string
	c := fakeChecker(func(p Project, _ Env) ([]Finding, error) {
		ran = append(ran, p.Dir)
		pre := ""
		if p.Dir != "." {
			pre = p.Dir + "/"
		}
		return []Finding{
			{File: pre + "b.py", Line: 1, ID: "I", Import: "b", Reason: "r"},
			{File: pre + "a.py", Line: 1, ID: "I", Import: "a", Reason: "r"},
		}, nil
	})
	cs := map[Lang]Checker{"python": c}
	tests := []struct {
		name    string
		files   []string
		want    string
		wantRan string
	}{
		{"one file", []string{"a.py"}, "a.py:1: I a — r\n", "."},
		{"manifest reports everything", []string{"uv.lock"}, "a.py:1: I a — r\nb.py:1: I b — r\n", "."},
		{"only the touched project", []string{"web/x.ts"}, "", "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran = nil
			_, stdout, _ := run(t, Options{Root: root, Langs: []Lang{"python"}, Files: tt.files, Checkers: cs})
			if stdout != tt.want {
				t.Errorf("stdout %q, want %q", stdout, tt.want)
			}
			if got := strings.Join(ran, ","); got != tt.wantRan {
				t.Errorf("ran %q, want %q", got, tt.wantRan)
			}
		})
	}
}

func TestRunSkipsProjectsOutsideFiles(t *testing.T) {
	root := gitRepo(t, map[string]string{"api/go.mod": "", "web/go.mod": "", "web/x.go": ""})
	c := fakeChecker(func(p Project, _ Env) ([]Finding, error) {
		if p.Dir == "api" {
			panic("checker ran for an untouched project")
		}
		return nil, nil
	})
	code, _, _ := run(t, Options{Root: root, Langs: []Lang{"go"}, Files: []string{"web/x.go"}, Checkers: map[Lang]Checker{"go": c}})
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
}

func TestFormatFinding(t *testing.T) {
	got := FormatFinding(Finding{"src/a.py", 3, "DEP003", "numpy", "imported but only a transitive dependency"})
	want := "src/a.py:3: DEP003 numpy — imported but only a transitive dependency"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunDefaultLangsFromManifest(t *testing.T) {
	write := func(pk string) string {
		return gitRepo(t, map[string]string{
			".lighthouse/manifest.toml": "version = \"dev\"\npackages = " + pk + "\n",
			"package.json":              "{}", "go.mod": "",
		})
	}
	var ran []Lang
	mk := func(l Lang) Checker {
		return fakeChecker(func(Project, Env) ([]Finding, error) { ran = append(ran, l); return nil, nil })
	}
	cs := map[Lang]Checker{"node": mk("node"), "go": mk("go"), "python": mk("python")}

	code, _, _ := run(t, Options{Root: write(`["core","expo"]`), Checkers: cs})
	if code != 0 || len(ran) != 1 || ran[0] != "node" {
		t.Errorf("expo: exit %d ran %v, want node only", code, ran)
	}
	ran = nil
	code, out, errb := run(t, Options{Root: write(`["core","unknown"]`), Checkers: cs})
	if code != 0 || len(ran) != 0 || out != "" || errb != "" {
		t.Errorf("core: exit %d ran %v out %q err %q", code, ran, out, errb)
	}
}

func writeUntracked(t *testing.T, root, name string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A change to the package.json of a workspace child widens the scope of its node project: the
// child is no project of its own, so its manifest must count for the root. A nested project that
// is no workspace keeps its own package.json.
func TestRunWorkspaceChildManifestWidensScope(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"package.json":                `{"workspaces": ["packages/*"]}`,
		"packages/child/package.json": `{}`, "packages/child/index.js": "",
		"tools/package.json": `{}`, "tools/run.js": "",
	})
	var ran []string
	c := fakeChecker(func(p Project, _ Env) ([]Finding, error) {
		ran = append(ran, p.Dir)
		if p.Dir == "tools" {
			return []Finding{{File: "tools/run.js", Line: 1, ID: "unlisted", Import: "x", Reason: "r"}}, nil
		}
		return []Finding{{File: "packages/child/index.js", Line: 1, ID: "unlisted", Import: "lodash", Reason: "r"}}, nil
	})
	cs := map[Lang]Checker{"node": c}
	tests := []struct {
		name, files, want, wantRan string
	}{
		{"child manifest", "packages/child/package.json", "packages/child/index.js:1: unlisted lodash — r\n", "."},
		{"nested project manifest stays its own", "tools/package.json", "tools/run.js:1: unlisted x — r\n", "tools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran = nil
			_, stdout, _ := run(t, Options{Root: root, Langs: []Lang{"node"}, Files: []string{tt.files}, Checkers: cs})
			if got := strings.Join(ran, ","); got != tt.wantRan {
				t.Errorf("ran %q, want %q", got, tt.wantRan)
			}
			if stdout != tt.want {
				t.Errorf("stdout %q, want %q", stdout, tt.want)
			}
		})
	}
}
