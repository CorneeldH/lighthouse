//go:build canary

package canary

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// workDir holds the binary and every temporary repository of this test binary. TestMain makes and
// removes it, not t.TempDir(): that one vanishes after the first test and carries the test name,
// which would put a reason ID in the path of a fixture.
var workDir string

var (
	buildOnce sync.Once
	builtBin  string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "canary-")
	if err != nil {
		panic(err)
	}
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		panic(err)
	}
	workDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// lighthouseBin builds cmd/lighthouse once per test binary.
func lighthouseBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		bin := filepath.Join(workDir, "bin", "lighthouse")
		cmd := exec.Command("go", "build", "-o", bin, "github.com/CorneeldH/lighthouse/cmd/lighthouse")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, out)
		}
		builtBin = bin
	})
	if builtBin == "" {
		t.Fatal("the lighthouse binary was not built")
	}
	return builtBin
}

var (
	shellcheckOnce sync.Once
	shellcheckPath string
	shellcheckErr  string
)

// shellcheckBin is the pinned shellcheck, resolved once with the lib of pack/shell from this
// checkout (its tool cache, a pinned copy on PATH, or the pinned download). A missing pin fails the
// test, never skips it: a canary that did not run says nothing. Hooks get it as SHELLCHECK_BIN.
func shellcheckBin(t *testing.T) string {
	t.Helper()
	shellcheckOnce.Do(func() {
		lib, err := filepath.Abs(packFile("shell/scripts/lib/shellcheck-bin.sh"))
		if err != nil {
			shellcheckErr = err.Error()
			return
		}
		cmd := exec.Command("bash", "-c", `. "$1" && resolve_shellcheck`, "resolve", lib)
		cmd.Dir = filepath.Dir(lib)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err = cmd.Run()
		bin := strings.TrimSpace(stdout.String())
		switch {
		case err != nil || bin == "":
			shellcheckErr = "resolve_shellcheck failed (" + fmt.Sprint(err) + "): " + stderr.String()
		case stderr.Len() > 0:
			// A notice means a shellcheck other than the pin: the canaries would measure that one.
			shellcheckErr = "resolve_shellcheck found no pinned shellcheck: " + stderr.String()
		default:
			shellcheckPath = bin
		}
	})
	if shellcheckPath == "" {
		t.Fatalf("no pinned shellcheck for the shell canaries: %s", shellcheckErr)
	}
	return shellcheckPath
}

// cleanEnv is the process environment without LIGHTHOUSE_* and with the directory of bin first on PATH.
func cleanEnv(bin string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(key, "LIGHTHOUSE_") && key != "PATH" && key != "SHELLCHECK_BIN" {
			env = append(env, kv)
		}
	}
	return append(env, "PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// installRepo is a temporary git repository on main with `lighthouse init --yes --pack <p>...`.
func installRepo(t *testing.T, packs ...string) string {
	t.Helper()
	bin := lighthouseBin(t)
	parent, err := os.MkdirTemp(workDir, "r")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	root := filepath.Join(parent, "repo")
	fixturesetup.Build(t, parent, []map[string]any{{"repo": "repo", "branch": "main"}})
	args := []string{"init", "--yes"}
	for _, p := range packs {
		args = append(args, "--pack", p)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = root
	cmd.Env = cleanEnv(bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lighthouse %v: %v\n%s", args, err, out)
	}
	return root
}

type run struct {
	Code           int
	Stdout, Stderr string
}

// runHook runs <root>/.claude/hooks/lighthouse/<script> (the script name and its arguments, for
// example "run.sh md-link-check") with payload on stdin.
func runHook(t *testing.T, root, script, payload string, env ...string) run {
	t.Helper()
	bin := lighthouseBin(t)
	fields := strings.Fields(script)
	cmd := exec.Command(filepath.Join(root, ".claude", "hooks", "lighthouse", fields[0]), fields[1:]...)
	cmd.Dir = root
	cmd.Env = append(cleanEnv(bin), env...)
	cmd.Stdin = strings.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s did not run: %v", script, err)
		}
		code = ee.ExitCode()
	}
	return run{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

// requireCanary demands the block (exit 2) of the reason id, with marker on stderr.
func requireCanary(t *testing.T, r run, id string, marker string) {
	t.Helper()
	if r.Code != 2 || !strings.Contains(r.Stderr, marker) {
		t.Fatalf("canary %s: exit %d, want 2 with %q on stderr\nstdout: %s\nstderr: %s", id, r.Code, marker, r.Stdout, r.Stderr)
	}
}
