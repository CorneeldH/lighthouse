package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/importcheck"
)

// Classification: these tests fail when `imports check` maps a flag error, an unknown language,
// the config switch or a path outside the root to the wrong exit code, when --lang or --files do
// not reach the run, or when the python checker loses the main-checkout .venv fallback. They
// observe the exit code, stdout and stderr of the whole command and the projects a fake checker
// is asked to check.

type importsRepo struct {
	root string
}

// newImportsRepo makes a git repo with the files, a lighthouse.toml with toml, and points the
// config reader at it.
func newImportsRepo(t *testing.T, toml string, files map[string]string) importsRepo {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files["lighthouse.toml"] = toml
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
	t.Setenv("LIGHTHOUSE_TOML", filepath.Join(root, "lighthouse.toml"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	return importsRepo{root: root}
}

func runImports(t *testing.T, tune func(*importcheck.Env, map[importcheck.Lang]importcheck.Checker), args ...string) (code int, stdout, stderr string) {
	t.Helper()
	importsTune = tune
	t.Cleanup(func() { importsTune = nil })
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs(append([]string{"imports"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func TestImportsCheckFlagErrorsAreExit3(t *testing.T) {
	newImportsRepo(t, "", map[string]string{})
	for _, args := range [][]string{
		{"check", "--bogus"},
		{"check", "--lang", "ruby"},
		{"check", "--lang"},
		{"check", "extra"},
	} {
		code, out, errOut := runImports(t, nil, args...)
		if code != 3 || out != "" || !strings.HasPrefix(errOut, "imports: COULD NOT CHECK — ") {
			t.Errorf("%q: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

func TestImportsCheckUnknownLangNamesIt(t *testing.T) {
	newImportsRepo(t, "", map[string]string{})
	_, _, errOut := runImports(t, nil, "check", "--lang", "ruby")
	if !strings.Contains(errOut, `unknown --lang "ruby"`) {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestImportsWithoutCheckIsNotAGate(t *testing.T) {
	newImportsRepo(t, "", map[string]string{})
	for _, args := range [][]string{{}, {"bogus"}} {
		code, _, errOut := runImports(t, nil, args...)
		if code != 3 || !strings.Contains(errOut, "COULD NOT CHECK — unknown subcommand") {
			t.Errorf("imports %q: code=%d stderr=%q", args, code, errOut)
		}
	}
}

func TestImportsCheckDisabledByConfig(t *testing.T) {
	newImportsRepo(t, "[imports]\ncheck = false\n", map[string]string{"go.mod": "module x\n"})
	called := false
	tune := func(_ *importcheck.Env, cs map[importcheck.Lang]importcheck.Checker) {
		cs["go"] = recordingChecker{func(importcheck.Project) { called = true }}
	}
	code, out, errOut := runImports(t, tune, "check", "--lang", "go")
	if code != 0 || out != "" || errOut != "" || called {
		t.Errorf("code=%d stdout=%q stderr=%q called=%v", code, out, errOut, called)
	}
}

type recordingChecker struct{ on func(importcheck.Project) }

func (r recordingChecker) Check(p importcheck.Project, _ importcheck.Env) ([]importcheck.Finding, error) {
	r.on(p)
	return nil, nil
}

func TestImportsCheckPassesFlagsThrough(t *testing.T) {
	repo := newImportsRepo(t, "", map[string]string{
		"a/go.mod": "module a\n", "a/x.go": "package a\n",
		"b/go.mod": "module b\n", "b/y.go": "package b\n",
		"p/pyproject.toml": "[project]\nname='p'\n",
	})
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"lang only", []string{"--lang", "go"}, []string{"a", "b"}},
		{"files relative", []string{"--lang", "go", "--files", "a/x.go"}, []string{"a"}},
		{"files absolute", []string{"--lang", "go", "--files", filepath.Join(repo.root, "b", "y.go")}, []string{"b"}},
		{"files backslash", []string{"--lang", "go", "--files", `b\y.go`}, []string{"b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var got []string
			tune := func(_ *importcheck.Env, cs map[importcheck.Lang]importcheck.Checker) {
				cs["go"] = recordingChecker{func(p importcheck.Project) { mu.Lock(); got = append(got, p.Dir); mu.Unlock() }}
				cs["python"] = recordingChecker{func(p importcheck.Project) { t.Errorf("python checked %s despite --lang go", p.Dir) }}
			}
			code, _, errOut := runImports(t, tune, append([]string{"check"}, tt.args...)...)
			if code != 0 || strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("code=%d projects=%v want %v stderr=%q", code, got, tt.want, errOut)
			}
		})
	}
}

func TestImportsCheckFileOutsideRootIsUnchecked(t *testing.T) {
	newImportsRepo(t, "", map[string]string{"go.mod": "module x\n"})
	outside := filepath.Join(t.TempDir(), "z.go")
	for _, f := range []string{outside, "../z.go"} {
		code, out, errOut := runImports(t, nil, "check", "--lang", "go", "--files", f)
		if code != 3 || out != "" || !strings.Contains(errOut, "COULD NOT CHECK — --files "+f+" is outside the repository root") {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", f, code, out, errOut)
		}
	}
}

func TestImportsCheckFindingIsExit1(t *testing.T) {
	newImportsRepo(t, "", map[string]string{"go.mod": "module x\n"})
	tune := func(_ *importcheck.Env, cs map[importcheck.Lang]importcheck.Checker) {
		cs["go"] = findingChecker{}
	}
	code, out, _ := runImports(t, tune, "check", "--lang", "go")
	if code != 1 || out != "x.go:3: GO1 foo — undeclared\n" {
		t.Errorf("code=%d stdout=%q", code, out)
	}
}

type findingChecker struct{}

func (findingChecker) Check(importcheck.Project, importcheck.Env) ([]importcheck.Finding, error) {
	return []importcheck.Finding{{File: "x.go", Line: 3, ID: "GO1", Import: "foo", Reason: "undeclared"}}, nil
}

func TestImportsCheckPythonFallsBackToMainCheckoutVenv(t *testing.T) {
	newImportsRepo(t, "[python]\nsource_dirs = [\"src\"]\n", map[string]string{
		"pyproject.toml": "[project]\nname='p'\n", "src/m.py": "import os\n",
	})
	main := t.TempDir()
	if err := os.MkdirAll(filepath.Join(main, ".venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	tune := func(env *importcheck.Env, _ map[importcheck.Lang]importcheck.Checker) {
		env.LookPath = func(string) (string, error) { return "/bin/uv", nil }
		env.Exec = func(_ string, argv []string, _ []string) ([]byte, []byte, int, error) {
			calls++
			for i, a := range argv {
				if a == "--json-output" {
					_ = os.WriteFile(argv[i+1], []byte("[]"), 0o644)
				}
			}
			return nil, nil, 0, nil
		}
	}

	t.Setenv("CLAUDE_PROJECT_DIR", main)
	code, out, errOut := runImports(t, tune, "check", "--lang", "python")
	if code != 0 || calls != 1 || out != "" || errOut != "" {
		t.Errorf("with fallback: code=%d calls=%d stdout=%q stderr=%q", code, calls, out, errOut)
	}

	t.Setenv("CLAUDE_PROJECT_DIR", "")
	code, _, errOut = runImports(t, tune, "check", "--lang", "python")
	if code != 3 || !strings.Contains(errOut, "no .venv") || calls != 1 {
		t.Errorf("without fallback: code=%d calls=%d stderr=%q", code, calls, errOut)
	}
}
