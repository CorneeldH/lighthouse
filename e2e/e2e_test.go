//go:build e2e

// Package e2e proves the DoD of M0 (a) on a fresh repository: `lighthouse init --yes`
// installs main-commit-guard, the command registered in .claude/settings.json blocks a
// commit on main, and `lighthouse init --upgrade` right after gives an empty diff. For
// M0 (c) it proves the same for pack/python: in a repository with a pyproject.toml the
// registered pre-commit-tests blocks a commit with a red test.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var gitEnv = []string{
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
}

// cleanEnv is the process environment without the lighthouse overrides, so the guard
// finds its lib the way it does in a real repository.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LIGHTHOUSE_") {
			env = append(env, kv)
		}
	}
	return env
}

func buildLighthouse(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lighthouse")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/CorneeldH/lighthouse/cmd/lighthouse")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(cleanEnv(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func freshRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-qm", "init")
	return repo
}

func run(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	// host.toml next to the repo, in the test's own temporary directory: an e2e run
	// never touches ~/.config, also on a host where `tailscale ip` works.
	cmd.Env = append(cleanEnv(), "LIGHTHOUSE_HOST_TOML="+filepath.Join(filepath.Dir(dir), "host.toml"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("lighthouse %v: %v\n%s\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// guardCommand reads the registered command from the merged settings, not the script path.
func guardCommand(t *testing.T, repo string) string {
	t.Helper()
	return bashHookCommand(t, repo, "main-commit-guard")
}

// bashHookCommand reads the one command under PreToolUse/Bash whose text names name.
func bashHookCommand(t *testing.T, repo, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, g := range s.Hooks["PreToolUse"] {
		if g.Matcher != "Bash" {
			continue
		}
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, name) {
				found = append(found, h.Command)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s registrations under PreToolUse/Bash: %v", name, found)
	}
	return found[0]
}

// runHook runs command the way Claude Code does: through a shell, with
// CLAUDE_PROJECT_DIR set, and the PreToolUse payload on stdin.
func runHook(t *testing.T, repo, command, bashCmd string) (int, string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"cwd":        repo,
		"tool_input": map[string]string{"command": bashCmd},
	})
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = repo
	cmd.Env = append(cleanEnv(), "CLAUDE_PROJECT_DIR="+repo)
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stderr.String()
	}
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	return 0, stderr.String()
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		sum := sha256.Sum256(data)
		snap[p] = info.Mode().String() + " " + hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestFreshRepoBlocksCommitOnMain(t *testing.T) {
	bin := buildLighthouse(t)
	repo := freshRepo(t)
	run(t, repo, bin, "init", "--yes")

	cmd := guardCommand(t, repo)
	code, stderr := runHook(t, repo, cmd, "git commit -m x")
	if code != 2 || !strings.Contains(stderr, "Main-commit guard: blocked") || strings.Contains(stderr, "COULD NOT CHECK") {
		t.Fatalf("main: rc %d\n%s", code, stderr)
	}

	git(t, repo, "checkout", "-q", "-b", "issue-1-x")
	if code, stderr := runHook(t, repo, cmd, "git commit -m x"); code != 0 {
		t.Fatalf("feature branch: rc %d\n%s", code, stderr)
	}
	git(t, repo, "checkout", "-q", "main")

	before := snapshot(t, repo)
	out := run(t, repo, bin, "init", "--upgrade")
	if out != "No changes.\n" || !maps.Equal(before, snapshot(t, repo)) {
		t.Fatalf("upgrade not empty:\n%s", out)
	}
}

// runGate runs the registered pre-commit-tests like runHook, with host.toml next to the
// repo (never ~/.config, whose gate_slice would wrap the run in systemd-run), and gives
// stdout too: a passed gate reports there.
func runGate(t *testing.T, repo, command string) (int, string, string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"cwd":        repo,
		"tool_input": map[string]string{"command": "git commit -m x"},
	})
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = repo
	cmd.Env = append(cleanEnv(), "CLAUDE_PROJECT_DIR="+repo,
		"LIGHTHOUSE_HOST_TOML="+filepath.Join(filepath.Dir(repo), "host.toml"))
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	return 0, stdout.String(), stderr.String()
}

func writeFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func needUV(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("uv"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("uv not on PATH; CI must install it before the e2e tests")
		}
		t.Skip("uv not on PATH (local run): the default [test_gate] command of pack/python is `uv run pytest -q`")
	}
}

// pythonRepo is a fresh repository with pyproject.toml, after `init --yes`, on a
// feature branch.
func pythonRepo(t *testing.T, bin, pyproject string) string {
	t.Helper()
	repo := freshRepo(t)
	writeFile(t, repo, "pyproject.toml", pyproject)
	git(t, repo, "add", "pyproject.toml")
	git(t, repo, "commit", "-qm", "pyproject")
	if out := run(t, repo, bin, "init", "--yes"); !strings.HasPrefix(out, "Packages: core, python (found pyproject.toml)\n") {
		t.Fatalf("init:\n%s", out)
	}
	git(t, repo, "checkout", "-q", "-b", "issue-1-x")
	return repo
}

const pyprojectHead = "[project]\nname = \"e2e\"\nversion = \"0\"\nrequires-python = \">=3.9\"\n"

func TestRedTestBlocksCommit(t *testing.T) {
	needUV(t)
	bin := buildLighthouse(t)
	repo := pythonRepo(t, bin, pyprojectHead+"\n[dependency-groups]\ndev = [\"pytest\"]\n")
	writeFile(t, repo, "tests/test_x.py", "def test_x():\n    assert 1 == 2\n")
	cmd := bashHookCommand(t, repo, "pre-commit-tests")

	code, _, stderr := runGate(t, repo, cmd)
	if code != 2 || !strings.Contains(stderr, "pre-commit-tests: tests failed") {
		t.Fatalf("red test: rc %d\n%s", code, stderr)
	}

	// Another size than the red file: a rewrite in the same second with the same size
	// would leave the stale .pyc of the red run valid.
	writeFile(t, repo, "tests/test_x.py", "def test_x():\n    assert 1 + 1 == 2\n")
	if code, stdout, stderr := runGate(t, repo, cmd); code != 0 {
		t.Fatalf("green test: rc %d\n%s\n%s", code, stdout, stderr)
	}
}

func TestDefaultCommandWithoutPytest(t *testing.T) {
	needUV(t)
	bin := buildLighthouse(t)
	repo := pythonRepo(t, bin, pyprojectHead)
	cmd := bashHookCommand(t, repo, "pre-commit-tests")

	code, _, stderr := runGate(t, repo, cmd)
	if code != 2 || !strings.Contains(stderr, "[test_gate] command") || !strings.Contains(stderr, "lighthouse.toml") || !strings.Contains(stderr, `command = ""`) {
		t.Fatalf("without pytest: rc %d\n%s", code, stderr)
	}

	toml, err := os.ReadFile(filepath.Join(repo, "lighthouse.toml"))
	if err != nil {
		t.Fatal(err)
	}
	off := strings.Replace(string(toml), `command = "uv run pytest -q"`, `command = ""`, 1)
	writeFile(t, repo, "lighthouse.toml", off)
	code, stdout, stderr := runGate(t, repo, cmd)
	if code != 0 || !strings.Contains(stdout, "no test command configured, nothing was tested") {
		t.Fatalf("command = \"\": rc %d\n%s\n%s", code, stdout, stderr)
	}
}
