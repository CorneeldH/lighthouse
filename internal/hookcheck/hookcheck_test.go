package hookcheck

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/deps"
	"github.com/CorneeldH/lighthouse/internal/initcmd"
)

// gitIn runs git in dir and fails the test when it fails.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newFixture is a fresh repository on main with one commit, after a real `lighthouse init --yes`
// of the core package plus packNames, at manifest version version.
func newFixture(t *testing.T, version string, packNames ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on the PATH")
	}
	dir := t.TempDir()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "config", "user.email", "check@example.invalid")
	gitIn(t, root, "config", "user.name", "check")
	gitIn(t, root, "config", "commit.gpgsign", "false")
	writeFile(t, root, "README.md", "x\n")
	gitIn(t, root, "add", "README.md")
	gitIn(t, root, "commit", "-q", "-m", "init")
	var out, errOut bytes.Buffer
	code := initcmd.Run(initcmd.Options{
		Dir:      root,
		Yes:      true,
		Version:  version,
		AddPacks: packNames,
		Level:    "strict", // a first init picks light; the smoke test wants every hook registered
		Deps: deps.Checker{
			LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
			Run:      func(string, ...string) error { return nil },
		},
		HostTOML:    filepath.Join(t.TempDir(), "host.toml"),
		TailscaleIP: func() (string, error) { return "", errors.New("no tailscale in tests") },
		Out:         &out,
		Err:         &errOut,
	})
	if code != 0 {
		t.Fatalf("init: exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	return root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withoutSmoke turns step 3 off for the tests of steps 1, 2 and 2b, so their exit code says
// what those steps found and nothing else.
func withoutSmoke(t *testing.T) {
	t.Helper()
	saved := smokeStep
	smokeStep = func(Env, []registration, string, *report) {}
	t.Cleanup(func() { smokeStep = saved })
}

// testEnv is the default environment of root with an empty home of its own.
func testEnv(t *testing.T, root string) Env {
	env := DefaultEnv(root)
	env.Home = t.TempDir()
	return env
}

func run(env Env) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code = Run(env, &o, &e)
	return code, o.String(), e.String()
}

// windowsEnv fakes a Windows host: env holds the variables, files the paths that exist, git the
// path of git.exe on the PATH ("" when it is not there).
func windowsEnv(env map[string]string, files []string, git string) Env {
	exists := map[string]bool{}
	for _, f := range files {
		exists[f] = true
	}
	return Env{
		GOOS:   "windows",
		Getenv: func(k string) string { return env[k] },
		LookPath: func(name string) (string, error) {
			if name == "git.exe" && git != "" {
				return git, nil
			}
			return "", exec.ErrNotFound
		},
		Exists: func(p string) bool { return exists[p] },
	}
}

func TestFindGitBash(t *testing.T) {
	const (
		envVar = "CLAUDE_CODE_GIT_BASH_PATH"
		std64  = `C:\Program Files\Git\bin\bash.exe`
		std32  = `C:\Program Files (x86)\Git\bin\bash.exe`
	)
	cases := []struct {
		name    string
		env     map[string]string
		files   []string
		git     string
		want    string
		wantErr string // a substring of the error; "" means no error
	}{
		{name: "variable set and present", env: map[string]string{envVar: `D:\Tools\Git\bin\bash.exe`},
			files: []string{`D:\Tools\Git\bin\bash.exe`, std64}, want: `D:\Tools\Git\bin\bash.exe`},
		{name: "variable set to sh.exe", env: map[string]string{envVar: `D:\Tools\sh.exe`},
			files: []string{`D:\Tools\sh.exe`}, want: `D:\Tools\sh.exe`},
		{name: "variable set but absent, nothing else", env: map[string]string{envVar: `D:\Gone\bash.exe`},
			wantErr: `D:\Gone\bash.exe`},
		{name: "variable set but absent falls back to detection", env: map[string]string{envVar: `D:\Gone\bash.exe`},
			files: []string{std64}, want: std64},
		{name: "variable naming no bash or sh is ignored", env: map[string]string{envVar: `D:\Tools\zsh.exe`},
			files: []string{`D:\Tools\zsh.exe`, std64}, want: std64},
		{name: "via git.exe in cmd", git: `E:\Apps\Git\cmd\git.exe`,
			files: []string{`E:\Apps\Git\bin\bash.exe`, std64}, want: `E:\Apps\Git\bin\bash.exe`},
		{name: "via git.exe in mingw64", git: `E:\Apps\Git\mingw64\bin\git.exe`,
			files: []string{`E:\Apps\Git\bin\bash.exe`}, want: `E:\Apps\Git\bin\bash.exe`},
		{name: "git.exe without bash.exe beside it", git: `E:\Odd\git.exe`,
			files: []string{std32}, want: std32},
		{name: "standard path", files: []string{std64}, want: std64},
		{name: "standard x86 path", files: []string{std32}, want: std32},
		{name: "nowhere", wantErr: "Git Bash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FindGitBash(windowsEnv(c.env, c.files, c.git))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("FindGitBash = %q, %v; want an error naming %q", got, err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("FindGitBash = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestRunGitBashLineOnlyOnWindows(t *testing.T) {
	withoutSmoke(t)
	root := newFixture(t, "v1.0.0")

	code, out, _ := run(testEnv(t, root))
	if code != 0 || strings.Contains(out, "Git Bash") {
		t.Fatalf("linux: exit %d, want 0 without a Git Bash line:\n%s", code, out)
	}

	env := testEnv(t, root)
	fake := windowsEnv(nil, []string{`C:\Program Files\Git\bin\bash.exe`}, "")
	env.GOOS, env.Getenv, env.LookPath, env.Exists = fake.GOOS, fake.Getenv, fake.LookPath, fake.Exists
	code, out, _ = run(env)
	if code != 0 || !strings.Contains(out, "ok    Git Bash: C:\\Program Files\\Git\\bin\\bash.exe") {
		t.Fatalf("windows with Git Bash: exit %d, want 0 and the ok line with the path:\n%s", code, out)
	}

	fake = windowsEnv(map[string]string{"CLAUDE_CODE_GIT_BASH_PATH": `D:\Gone\bash.exe`}, []string{`C:\Program Files\Git\bin\bash.exe`}, "")
	env.Getenv, env.LookPath, env.Exists = fake.Getenv, fake.LookPath, fake.Exists
	code, out, _ = run(env)
	want := "ok    Git Bash: C:\\Program Files\\Git\\bin\\bash.exe (CLAUDE_CODE_GIT_BASH_PATH=D:\\Gone\\bash.exe is no bash or sh that exists, so Claude Code ignores it)"
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("windows with a stale variable: exit %d, want 0 and %q:\n%s", code, want, out)
	}

	fake = windowsEnv(nil, nil, "")
	env.Getenv, env.LookPath, env.Exists = fake.Getenv, fake.LookPath, fake.Exists
	code, out, _ = run(env)
	if code != 1 || !strings.Contains(out, "FAIL  Git Bash not found") || !strings.Contains(out, "CLAUDE_CODE_GIT_BASH_PATH") {
		t.Fatalf("windows without Git Bash: exit %d, want 1 and a FAIL with the remedy:\n%s", code, out)
	}
}

func TestRunCRLFIsFail(t *testing.T) {
	withoutSmoke(t)
	cases := []struct {
		name, rel string
	}{
		{"blocking list", ".claude/hooks/lighthouse/blocking"},
		{"run.sh", ".claude/hooks/lighthouse/run.sh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newFixture(t, "v1.0.0")
			if code, out, _ := run(testEnv(t, root)); code != 0 {
				t.Fatalf("fresh fixture: exit %d, want 0:\n%s", code, out)
			}
			body := readFile(t, root, c.rel)
			writeFile(t, root, c.rel, strings.ReplaceAll(body, "\n", "\r\n"))
			code, out, _ := run(testEnv(t, root))
			want := "FAIL  " + c.rel + " has CRLF line endings"
			if code != 1 || !strings.Contains(out, want) || !strings.Contains(out, "lighthouse init --upgrade") {
				t.Fatalf("exit %d, want 1 and %q with the remedy:\n%s", code, want, out)
			}
		})
	}
}

func TestRunUnregisteredHookIsFail(t *testing.T) {
	withoutSmoke(t)
	root := newFixture(t, "v1.0.0")
	settings := readFile(t, root, ".claude/settings.json")
	cut := strings.Replace(settings, "run.sh main-commit-gate", "run.sh main-commit-gate-renamed", 1)
	if cut == settings {
		t.Fatal("fixture has no main-commit-gate registration")
	}
	writeFile(t, root, ".claude/settings.json", cut)

	code, out, _ := run(testEnv(t, root))
	if code != 1 || !strings.Contains(out, "FAIL  hook not registered") ||
		!strings.Contains(out, "run.sh main-commit-gate") || !strings.Contains(out, "lighthouse init --upgrade") {
		t.Fatalf("exit %d, want 1 and a FAIL naming main-commit-gate with the remedy:\n%s", code, out)
	}
}

func TestRunDisableAllHooksIsFail(t *testing.T) {
	withoutSmoke(t)
	cases := []struct {
		name  string
		write func(t *testing.T, root, home string) string // returns the path the FAIL must name
	}{
		{"project settings", func(t *testing.T, root, _ string) string {
			s := readFile(t, root, ".claude/settings.json")
			writeFile(t, root, ".claude/settings.json", strings.Replace(s, "{", "{\n  \"disableAllHooks\": true,", 1))
			return filepath.Join(root, ".claude", "settings.json")
		}},
		{"local settings", func(t *testing.T, root, _ string) string {
			writeFile(t, root, ".claude/settings.local.json", `{"disableAllHooks": true}`)
			return filepath.Join(root, ".claude", "settings.local.json")
		}},
		{"user settings", func(t *testing.T, _, home string) string {
			writeFile(t, home, ".claude/settings.json", `{"disableAllHooks": true}`)
			return filepath.Join(home, ".claude", "settings.json")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newFixture(t, "v1.0.0")
			env := testEnv(t, root)
			path := c.write(t, root, env.Home)
			code, out, _ := run(env)
			want := "FAIL  disableAllHooks is true in " + path
			if code != 1 || !strings.Contains(out, want) {
				t.Fatalf("exit %d, want 1 and %q:\n%s", code, want, out)
			}
		})
	}

	t.Run("false is fine", func(t *testing.T) {
		root := newFixture(t, "v1.0.0")
		env := testEnv(t, root)
		writeFile(t, env.Home, ".claude/settings.json", `{"disableAllHooks": false}`)
		if code, out, _ := run(env); code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
	})
}

func TestRunCouldNotCheck(t *testing.T) {
	withoutSmoke(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{"no settings.json", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, ".claude", "settings.json")); err != nil {
				t.Fatal(err)
			}
		}, ".claude/settings.json"},
		{"settings.json is not JSON", func(t *testing.T, root string) {
			writeFile(t, root, ".claude/settings.json", "{ not json")
		}, ".claude/settings.json"},
		{"hooks of the wrong shape", func(t *testing.T, root string) {
			writeFile(t, root, ".claude/settings.json", `{"hooks": []}`)
		}, `"hooks" is not an object`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newFixture(t, "v1.0.0")
			c.setup(t, root)
			code, out, errOut := run(testEnv(t, root))
			if code != 2 || !strings.HasPrefix(errOut, "check-hooks: COULD NOT CHECK — ") || !strings.Contains(errOut, c.want) || out != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q; want 2, no stdout and COULD NOT CHECK naming %q", code, out, errOut, c.want)
			}
		})
	}

	t.Run("root is no directory", func(t *testing.T) {
		env := testEnv(t, filepath.Join(t.TempDir(), "absent"))
		code, _, errOut := run(env)
		if code != 2 || !strings.HasPrefix(errOut, "check-hooks: COULD NOT CHECK — ") {
			t.Fatalf("exit %d, stderr %q; want 2 and COULD NOT CHECK", code, errOut)
		}
	})
}
