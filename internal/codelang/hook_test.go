package codelang

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
)

// Classification: this is a hook/gate change; the likely regression surface is the exit code,
// stderr and stdout of `lighthouse hook code-language-check` for one payload.

// isolate keeps the environment of the run out of the hook: no session toml, no nested call,
// a private state dir, and git never looks above dir.
func isolate(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("LIGHTHOUSE_UNDER_BASH_WRITE_CHECK", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
}

// gitRepo is repoWith in a git repository, with symlinks resolved so paths compare as the
// hook prints them.
func gitRepo(t *testing.T, extra string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(repoWith(t, extra))
	if err != nil {
		t.Fatal(err)
	}
	isolate(t, root)
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return root
}

// runPayload runs the hook on raw with a runtime root that has no lighthouse.toml.
func runPayload(t *testing.T, raw string) hook.Result {
	t.Helper()
	return runHook(context.Background(), t.TempDir(), "", hook.Payload{Raw: []byte(raw)})
}

// hookOn runs the hook for a write to path.
func hookOn(t *testing.T, path string) hook.Result {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"tool_name": "Edit", "tool_input": map[string]any{"file_path": path}})
	if err != nil {
		t.Fatal(err)
	}
	return runPayload(t, string(raw))
}

func emptyResult() hook.Result { return hook.Result{} }

// cannotCheckBlock is the block of cannot_check() in code-language-check.sh.
func cannotCheckBlock(reason, detail string) string {
	s := "code-language-check: COULD NOT CHECK — " + reason + "\n\n" +
		"This hook checks that code is written in English.\n" +
		"Nothing was checked; that is not a green light.\n"
	if detail != "" {
		s += "\n" + detail + "\n"
	}
	return s
}

func TestHookPayload(t *testing.T) {
	isolate(t, t.TempDir())
	for _, c := range []struct {
		name, raw string
		want      hook.Result
	}{
		{"not JSON", "{", hook.Result{Code: 2, Stderr: cannotCheckBlock("the payload is not valid JSON.", "")}},
		{"not an object", "[1]", hook.Result{Code: 2, Stderr: cannotCheckBlock("the payload is not valid JSON.", "")}},
		{"tool_input not an object", `{"tool_input": "x"}`, hook.Result{Code: 2, Stderr: cannotCheckBlock("the payload is not valid JSON.", "")}},
		{"empty stdin", "", hook.Result{}},
		{"no tool_input", `{}`, hook.Result{}},
		{"no file_path", `{"tool_input": {}}`, hook.Result{}},
		{"empty file_path", `{"tool_input": {"file_path": ""}}`, hook.Result{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := runPayload(t, c.raw); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// The path is made canonical as `realpath -m` does: a symlinked directory resolves, and a file
// that does not exist is no error but nothing to check.
func TestHookCanonicalPath(t *testing.T) {
	root := gitRepo(t, "")
	writeFile(t, filepath.Join(root, "real", "a.py"), []byte(dutchComment))
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"cwd": root, "tool_input": map[string]any{"file_path": "link/a.py"}})
	res := runPayload(t, string(raw))
	want := "code-language-check: " + filepath.Join(root, "real", "a.py") + " is not English\n"
	if res.Code != 2 || len(res.Stderr) < len(want) || res.Stderr[:len(want)] != want {
		t.Errorf("symlinked directory: %+v, want 2 and a report on the real path", res)
	}
	if got := hookOn(t, filepath.Join(root, "link", "missing.py")); !reflect.DeepEqual(got, emptyResult()) {
		t.Errorf("missing file in an existing directory: %+v, want nothing", got)
	}
	if got := hookOn(t, filepath.Join(root, "gone", "deeper", "missing.py")); !reflect.DeepEqual(got, emptyResult()) {
		t.Errorf("missing directory: %+v, want nothing", got)
	}
}

// A relative path without a cwd in the payload resolves against the working directory.
func TestHookRelativePathFromWorkingDirectory(t *testing.T) {
	root := gitRepo(t, "")
	writeFile(t, filepath.Join(root, "a.py"), []byte(dutchComment))
	t.Chdir(root)
	res := runPayload(t, `{"tool_input": {"file_path": "a.py"}}`)
	if want := "code-language-check: " + filepath.Join(root, "a.py") + " is not English\n"; res.Code != 2 ||
		len(res.Stderr) < len(want) || res.Stderr[:len(want)] != want {
		t.Errorf("%+v, want 2 and a report on %s", res, filepath.Join(root, "a.py"))
	}
}

func TestHookNotAGitRepo(t *testing.T) {
	dir, err := filepath.EvalSymlinks(repoWith(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	isolate(t, dir)
	path := filepath.Join(dir, "a.py")
	writeFile(t, path, []byte(dutchComment))
	if got := hookOn(t, path); !reflect.DeepEqual(got, emptyResult()) {
		t.Errorf("%+v, want nothing", got)
	}
}

// fakeGit puts a git first on PATH that prints out and exits with code.
func fakeGit(t *testing.T, out string, code int) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' '" + out + "'\nexit " + strconv.Itoa(code) + "\n"
	writeFile(t, filepath.Join(bin, "git"), []byte(script))
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestHookGitFailures(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolate(t, dir)
	path := filepath.Join(dir, "a.py")
	writeFile(t, path, []byte(dutchComment))
	for _, c := range []struct {
		name, out string
		code      int
		want      hook.Result
	}{
		{"broken", "fatal: something broke\n", 1,
			hook.Result{Code: 2, Stderr: cannotCheckBlock("git could not read '"+dir+"'.", "fatal: something broke")}},
		{"not a repository", "fatal: not a git repository (or any of the parent directories): .git\n", 1, hook.Result{}},
		{"empty root", "\n", 0, hook.Result{Code: 2, Stderr: cannotCheckBlock("git gave no repository root for '"+dir+"'.", "")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeGit(t, c.out, c.code)
			if got := hookOn(t, path); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
	t.Run("no git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		want := hook.Result{Code: 2, Stderr: cannotCheckBlock("missing tooling: git", "")}
		if got := hookOn(t, path); !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}

// A checker error is one line, not the block of the hook level.
func TestHookCheckerCannotCheck(t *testing.T) {
	root := gitRepo(t, "code_language_exempt = 3\n")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte("x = 1\n"))
	res := hookOn(t, path)
	if res.Code != 2 || res.Text != "" || res.Output != nil ||
		!strings.HasPrefix(res.Stderr, "code-language-check: COULD NOT CHECK — ") || strings.Count(res.Stderr, "\n") != 1 {
		t.Errorf("%+v, want 2 and a COULD NOT CHECK line", res)
	}
}

// Only a notice: JSON for the user and the agent; under bash-write-check plain text instead.
func TestHookNoticeWithoutFindings(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "http500")
	root := gitRepo(t, "")
	t.Setenv("LIGHTHOUSE_TOML", sessionTOML(t, t.TempDir(), llmTable(fake.url)))
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(doubtLine))
	notice := "code-language-check: layer 2 did not answer (m1: HTTP 500); 1 doubtful units not checked:\n" +
		path + ":1: name: legacy_processed_lijst_weg"
	want := hook.Result{Output: map[string]any{
		"systemMessage": notice,
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PostToolUse", "additionalContext": notice,
		},
	}}
	if got := hookOn(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestHookNoticeWithFindings(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "http500")
	root := gitRepo(t, "")
	t.Setenv("LIGHTHOUSE_TOML", sessionTOML(t, t.TempDir(), llmTable(fake.url)))
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	want := hook.Result{Code: 2, Stderr: "code-language-check: " + path + " is not English\n" +
		path + ":1: name: lees_bestand [layer 1]\n" +
		`The whole file must be English. A line that is deliberately not English gets "lighthouse: allow-language".` + "\n\n" +
		"code-language-check: layer 2 did not answer (m1: HTTP 500); 1 doubtful units not checked:\n" +
		path + ":2: name: legacy_processed_lijst_weg\n"}
	if got := hookOn(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
}
