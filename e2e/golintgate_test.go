//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	lintUnchecked  = "go-lint-gate: COULD NOT CHECK — "
	lintNotChecked = "go-lint-gate: not checked — no go.mod in the repository root"
	lintFinding    = "a.go:5:22: Error return value of `os.Remove` is not checked (errcheck)"
	goStub         = `#!/bin/bash
echo "go $*" >> "$STUB_LOG"
exit 0
`
	// With STUB_FAIL_IN set, `run` finds STUB_RUN_OUT only in that directory.
	lintStub = `#!/bin/bash
echo "$* @ $PWD" >> "$STUB_LOG"
case "$1" in
  version) echo "${STUB_VERSION:-2.15.0}" ;;
  linters)
    if [ -n "$STUB_LINTERS_RC" ]; then exit "$STUB_LINTERS_RC"; fi
    DEFAULT='{"Enabled":[{"name":"errcheck"},{"name":"govet"},{"name":"ineffassign"},{"name":"staticcheck"},{"name":"unused"}]}'
    echo "${STUB_LINTERS_OUT:-$DEFAULT}" ;;
  config) exit ${STUB_VERIFY_RC:-0} ;;
  run)
    if [ -n "$STUB_FAIL_IN" ]; then
      [ "$PWD" = "$STUB_FAIL_IN" ] || exit 0
      echo "$STUB_RUN_OUT"; exit 1
    fi
    echo "$STUB_RUN_OUT"; exit ${STUB_RUN_RC:-0} ;;
esac
`
)

// lintTools are the tools the hook needs; lighthouse is the binary built from this checkout.
var lintTools = []string{"bash", "jq", "git", "cat", "tr", "head", "sed", "lighthouse", "mktemp", "rm"}

func TestGoLintGate(t *testing.T) {
	skipOnWindows(t, "the pack hooks run through runPackHook's tool dir, which breaks bash -c quoting on Windows (#559); and lighthouse init per subtest takes ~5 s there")
	bin := buildLighthouse(t)

	// newRepo is a repository with the go pack installed, and a go.mod unless goMod is false;
	// pin, when set, is the content of .golangci-version.
	newRepo := func(t *testing.T, goMod bool, pin string) (repo, command string) {
		t.Helper()
		repo = packRepo(t, bin, map[string]string{"go.mod": "module example.com/m\n\ngo 1.26\n"}, "Packages: core, go (found go.mod)\n")
		command, _ = registeredHook(t, repo, "Stop", "", "go-lint-gate.sh")
		if !goMod {
			if err := os.Remove(filepath.Join(repo, "go.mod")); err != nil {
				t.Fatal(err)
			}
		}
		if pin != "" {
			writeFile(t, repo, ".golangci-version", pin)
		}
		return repo, command
	}
	// stubEnv is an environment whose go and golangci-lint are stubs that log to STUB_LOG, the
	// alarm directory XDG_CACHE_HOME in a temporary directory (never the real alarm of the
	// machine) and extra (KEY=value) on top.
	stubEnv := func(t *testing.T, extra ...string) (env []string, log, xdg string) {
		t.Helper()
		dir := toolDir(t, bin, lintTools, "", map[string]string{"go": goStub, "golangci-lint": lintStub})
		log = filepath.Join(t.TempDir(), "stub.log")
		xdg = t.TempDir()
		env = append([]string{
			"PATH=" + dir + string(os.PathListSeparator) + filepath.Dir(bin) + string(os.PathListSeparator) + os.Getenv("PATH"),
			"STUB_LOG=" + log,
			"XDG_CACHE_HOME=" + xdg,
		}, extra...)
		return env, log, xdg
	}
	// bareEnv is an environment whose PATH is dir alone.
	bareEnv := func(t *testing.T, dir string) []string {
		t.Helper()
		return []string{"PATH=" + dir, "STUB_LOG=" + filepath.Join(t.TempDir(), "l"), "XDG_CACHE_HOME=" + t.TempDir()}
	}
	// editTurn is the payload of a turn that edited a file in edited, from a session whose cwd
	// is root; edited is a path.
	editTurn := func(t *testing.T, root, edited string) map[string]any {
		t.Helper()
		transcript := writeTranscript(t, filepath.Join(t.TempDir(), "transcript.jsonl"),
			promptEntry(root, promptTS),
			toolEntry("Edit", map[string]any{"file_path": edited}, root, toolTS),
			resultEntry(root, resultTS))
		return stopPayload(root, transcript, false)
	}
	// cleanTurn is the payload of a turn that started now and edited only edited in root.
	cleanTurn := func(t *testing.T, root, edited string) map[string]any {
		t.Helper()
		pastThisSecond()
		transcript := writeTranscript(t, filepath.Join(t.TempDir(), "transcript.jsonl"),
			promptEntry(root, stamp(time.Now())),
			toolEntry("Edit", map[string]any{"file_path": filepath.Join(root, edited)}, root, toolTS),
			resultEntry(root, resultTS))
		return stopPayload(root, transcript, false)
	}
	// worktree commits the repo and adds a worktree of it.
	worktree := func(t *testing.T, root string) string {
		t.Helper()
		git(t, root, "add", "-A")
		git(t, root, "commit", "-qm", "init")
		wt := filepath.Join(root, ".claude", "worktrees", "wt")
		git(t, root, "worktree", "add", "-q", wt, "-b", "wt")
		return wt
	}
	// turn is the payload of a turn that edited a Go file in root.
	turn := func(t *testing.T, root string) map[string]any {
		return editTurn(t, root, filepath.Join(root, "a.go"))
	}
	// message is the systemMessage of a hook that exited 0.
	message := func(t *testing.T, code int, stdout, stderr string) string {
		t.Helper()
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
		var out struct {
			SystemMessage string `json:"systemMessage"`
		}
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("stdout %q is no JSON: %v", stdout, err)
		}
		return out.SystemMessage
	}
	silent := func(t *testing.T, code int, stdout, stderr string) {
		t.Helper()
		if code != 0 || stdout != "" {
			t.Fatalf("want silent rc 0, got rc %d\n%s%s", code, stdout, stderr)
		}
	}
	readLog := func(log string) string {
		data, err := os.ReadFile(log)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(data)
	}
	runs := func(log string) []string {
		var out []string
		for _, line := range strings.Split(readLog(log), "\n") {
			if strings.HasPrefix(line, "run ") {
				out = append(out, line)
			}
		}
		return out
	}
	equalLines := func(t *testing.T, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("lint runs:\n%q\nwant:\n%q", got, want)
		}
	}
	// alarm writes line as the usage alarm that the gate reads through XDG_CACHE_HOME=xdg.
	alarm := func(t *testing.T, xdg, local string, delta time.Duration) {
		t.Helper()
		at := time.Now().UTC().Add(delta).Format("2006-01-02T15:04:05Z")
		writeFile(t, xdg, "lighthouse/usage-window.alarm", "venster 92 "+local+" "+at+"\n")
	}
	skipMessage := func(root string) string {
		return "go-lint-gate: not checked — the turn touched no Go path in " + root + "."
	}

	t.Run("clean_passes", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, _ := stubEnv(t)
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		silent(t, code, stdout, stderr)
		if got := readLog(log); !strings.Contains(got, "run ./...") || strings.Contains(got, "vet") {
			t.Fatalf("log:\n%s", got)
		}
	})

	t.Run("lint_failure_could_not_check", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, _, _ := stubEnv(t, "STUB_RUN_RC=3")
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		if got := message(t, code, stdout, stderr); !strings.HasPrefix(got, lintUnchecked) {
			t.Fatalf("message %q", got)
		}
	})

	t.Run("invalid_config_could_not_check", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, _ := stubEnv(t, "STUB_VERIFY_RC=3")
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		if got := message(t, code, stdout, stderr); !strings.HasPrefix(got, lintUnchecked) {
			t.Fatalf("message %q", got)
		}
		if got := readLog(log); strings.Contains(got, "run ./...") {
			t.Fatalf("linted with an invalid config:\n%s", got)
		}
	})

	for name, extra := range map[string]string{
		"fails":     "STUB_LINTERS_RC=3",
		"empty-set": "STUB_LINTERS_OUT={}",
		"not-json":  "STUB_LINTERS_OUT=not json",
	} {
		t.Run("unreadable_linter_list_says_the_floor_is_unchecked/"+name, func(t *testing.T) {
			repo, command := newRepo(t, true, "")
			env, log, _ := stubEnv(t, extra)
			code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
			if got := message(t, code, stdout, stderr); !strings.Contains(got, "COULD NOT CHECK the floor") {
				t.Fatalf("message %q", got)
			}
			if got := readLog(log); !strings.Contains(got, "run ./...") {
				t.Fatalf("the lint run was skipped:\n%s", got)
			}
		})
	}

	t.Run("linter_off_blocks_on_the_floor", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		out := `{"Enabled":[{"name":"govet"},{"name":"ineffassign"},{"name":"staticcheck"},{"name":"unused"}]}`
		env, log, _ := stubEnv(t, "STUB_LINTERS_OUT="+out)
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		if code != 2 {
			t.Fatalf("rc %d, want 2\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "[go-lint-gate/errcheck]") {
			t.Errorf("stderr lacks the floor reason:\n%s", stderr)
		}
		if got := readLog(log); strings.Contains(got, "run ./...") {
			t.Errorf("linted past a floor block:\n%s", got)
		}
	})

	t.Run("no_config_file_lints_with_defaults", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, _ := stubEnv(t, "STUB_VERIFY_RC=6")
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		silent(t, code, stdout, stderr)
		if got := readLog(log); !strings.Contains(got, "run ./...") {
			t.Fatalf("log:\n%s", got)
		}
	})

	t.Run("pin_matches_with_v_and_newline", func(t *testing.T) {
		repo, command := newRepo(t, true, "v2.15.0\n")
		env, log, _ := stubEnv(t, "STUB_VERSION=2.15.0")
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		silent(t, code, stdout, stderr)
		if got := readLog(log); !strings.Contains(got, "run ./...") {
			t.Fatalf("log:\n%s", got)
		}
	})

	t.Run("pin_mismatch_could_not_check", func(t *testing.T) {
		repo, command := newRepo(t, true, "v2.15.0")
		env, log, _ := stubEnv(t, "STUB_VERSION=2.11.4")
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		got := message(t, code, stdout, stderr)
		if !strings.HasPrefix(got, lintUnchecked) || !strings.Contains(got, "2.11.4") || !strings.Contains(got, "2.15.0") {
			t.Fatalf("message %q", got)
		}
		if l := readLog(log); strings.Contains(l, "run ./...") {
			t.Fatalf("linted with the wrong version:\n%s", l)
		}
	})

	t.Run("no_pin_runs_unpinned", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, _ := stubEnv(t, "STUB_VERSION=2.99.9")
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		silent(t, code, stdout, stderr)
		if got := readLog(log); !strings.Contains(got, "run ./...") {
			t.Fatalf("log:\n%s", got)
		}
	})

	t.Run("no_go_mod_reports_not_checked", func(t *testing.T) {
		repo, command := newRepo(t, false, "")
		env, log, _ := stubEnv(t)
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		if got := message(t, code, stdout, stderr); !strings.HasPrefix(got, lintNotChecked) {
			t.Fatalf("message %q", got)
		}
		if got := readLog(log); got != "" {
			t.Fatalf("a tool ran without a go.mod:\n%s", got)
		}
	})

	t.Run("no_go_mod_without_golangci_lint_reports_not_checked", func(t *testing.T) {
		repo, command := newRepo(t, false, "")
		dir := toolDir(t, bin, lintTools, "", map[string]string{"go": goStub})
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), bareEnv(t, dir))
		if got := message(t, code, stdout, stderr); !strings.HasPrefix(got, lintNotChecked) {
			t.Fatalf("message %q", got)
		}
	})

	t.Run("stop_hook_active_passes", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, _ := stubEnv(t, "STUB_RUN_RC=1")
		code, stdout, stderr := runPackHook(t, bin, repo, command, map[string]any{"stop_hook_active": true, "cwd": repo}, env)
		silent(t, code, stdout, stderr)
		if got := readLog(log); got != "" {
			t.Fatalf("a tool ran on the second Stop:\n%s", got)
		}
	})

	for _, tool := range []string{"go", "golangci-lint", "jq", "git", "lighthouse"} {
		t.Run("missing_tool_could_not_check/"+tool, func(t *testing.T) {
			repo, command := newRepo(t, true, "")
			stubs := map[string]string{"go": goStub, "golangci-lint": lintStub}
			delete(stubs, tool)
			dir := toolDir(t, bin, lintTools, tool, stubs)
			code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), bareEnv(t, dir))
			got := message(t, code, stdout, stderr)
			if !strings.HasPrefix(got, lintUnchecked) || !strings.Contains(got, tool) {
				t.Fatalf("message %q", got)
			}
		})
	}

	t.Run("a_finding_in_the_edited_worktree_blocks_with_the_cwd_on_main", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		wt := worktree(t, repo)
		env, log, _ := stubEnv(t, "STUB_FAIL_IN="+wt, "STUB_RUN_OUT="+lintFinding)
		code, stdout, stderr := runPackHook(t, bin, repo, command, editTurn(t, repo, filepath.Join(wt, "a.go")), env)
		if code != 2 {
			t.Fatalf("rc %d, want 2\n%s%s", code, stdout, stderr)
		}
		if want := "go-lint-gate: golangci-lint finds problems in " + wt + "."; strings.Split(stderr, "\n")[0] != want {
			t.Errorf("first line of stderr, want %q:\n%s", want, stderr)
		}
		if !strings.Contains(stderr, lintFinding) {
			t.Errorf("stderr lacks the finding:\n%s", stderr)
		}
		equalLines(t, runs(log), "run ./... @ "+wt)
	})

	t.Run("a_finding_in_an_untouched_worktree_does_not_block", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		wt := worktree(t, repo)
		env, log, _ := stubEnv(t, "STUB_FAIL_IN="+wt, "STUB_RUN_OUT="+lintFinding)
		code, stdout, stderr := runPackHook(t, bin, repo, command, editTurn(t, repo, filepath.Join(repo, "a.go")), env)
		silent(t, code, stdout, stderr)
		equalLines(t, runs(log), "run ./... @ "+repo)
	})

	t.Run("every_checkout_of_the_turn_is_linted", func(t *testing.T) {
		// A Bash call from main into the worktree names both; the finding in the second blocks.
		repo, command := newRepo(t, true, "")
		wt := worktree(t, repo)
		env, log, _ := stubEnv(t, "STUB_FAIL_IN="+wt, "STUB_RUN_OUT="+lintFinding)
		writeFile(t, repo, "b.go", "package m\n")
		writeFile(t, wt, "a.go", "package m\n")
		transcript := writeTranscript(t, filepath.Join(t.TempDir(), "transcript.jsonl"),
			promptEntry(repo, promptTS),
			toolEntry("Bash", map[string]any{"command": "git -C " + wt + " commit -qm x"}, repo, toolTS))
		code, stdout, stderr := runPackHook(t, bin, repo, command, stopPayload(repo, transcript, false), env)
		if code != 2 {
			t.Fatalf("rc %d, want 2\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, lintFinding) {
			t.Errorf("stderr lacks the finding:\n%s", stderr)
		}
		equalLines(t, runs(log), "run ./... @ "+repo, "run ./... @ "+wt)
	})

	t.Run("without_a_transcript_could_not_check", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, _ := stubEnv(t, "STUB_RUN_RC=1", "STUB_RUN_OUT="+lintFinding)
		code, stdout, stderr := runPackHook(t, bin, repo, command, map[string]any{"stop_hook_active": false, "cwd": repo}, env)
		got := message(t, code, stdout, stderr)
		if !strings.HasPrefix(got, lintUnchecked) || !strings.Contains(got, "transcript_path") {
			t.Fatalf("message %q", got)
		}
		equalLines(t, runs(log))
	})

	t.Run("valid_usage_alarm_skips_the_lint", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, xdg := stubEnv(t, "STUB_RUN_OUT="+lintFinding, "STUB_RUN_RC=1")
		alarm(t, xdg, "14_30", time.Hour)
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		want := "go-lint-gate: not checked — the usage alarm is up until 14 30; the usage gate wraps up this session."
		if got := message(t, code, stdout, stderr); got != want {
			t.Fatalf("message %q, want %q", got, want)
		}
		equalLines(t, runs(log))
	})

	t.Run("without_lighthouse_a_valid_alarm_could_not_check", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, log, xdg := stubEnv(t, "STUB_RUN_OUT="+lintFinding, "STUB_RUN_RC=1")
		alarm(t, xdg, "14:30", time.Hour)
		dir := toolDir(t, bin, lintTools, "lighthouse", map[string]string{"go": goStub, "golangci-lint": lintStub})
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+pathWithoutLighthouse(t))
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		want := lintUnchecked + "lighthouse is missing. Nothing was linted; that is not a green light."
		if got := message(t, code, stdout, stderr); got != want {
			t.Fatalf("message %q, want %q", got, want)
		}
		equalLines(t, runs(log))
	})

	t.Run("alarm_is_read_with_lighthouse_usage_alarm", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, _, _ := stubEnv(t, "STUB_RUN_OUT="+lintFinding, "STUB_RUN_RC=1")
		fake := toolDir(t, bin, nil, "", map[string]string{"lighthouse": lighthouseStub(bin,
			`[ "$*" = "usage alarm" ] || exit 1`+"\n"+"echo 'venster 92 9_15 2099-01-01T00:00:00Z'\n")})
		env = append(env, "PATH="+fake+string(os.PathListSeparator)+strings.TrimPrefix(env[0], "PATH="))
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		want := "go-lint-gate: not checked — the usage alarm is up until 9 15; the usage gate wraps up this session."
		if got := message(t, code, stdout, stderr); got != want {
			t.Fatalf("message %q, want %q", got, want)
		}
	})

	t.Run("help_text_from_an_old_lighthouse_is_no_alarm", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, _, _ := stubEnv(t, "STUB_RUN_OUT="+lintFinding, "STUB_RUN_RC=1")
		old := toolDir(t, bin, nil, "", map[string]string{"lighthouse": lighthouseStub(bin,
			"echo 'Usage:'\necho '  lighthouse usage [command]'\n")})
		env = append(env, "PATH="+old+string(os.PathListSeparator)+strings.TrimPrefix(env[0], "PATH="))
		code, stdout, stderr := runPackHook(t, bin, repo, command, turn(t, repo), env)
		if code != 2 || !strings.Contains(stderr, lintFinding) {
			t.Fatalf("rc %d, want 2 with the finding\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("stop_hook_active_with_alarm_is_silent", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		env, _, xdg := stubEnv(t, "STUB_RUN_OUT="+lintFinding, "STUB_RUN_RC=1")
		alarm(t, xdg, "14:30", time.Hour)
		transcript := writeTranscript(t, filepath.Join(t.TempDir(), "transcript.jsonl"), promptEntry(repo, promptTS))
		code, stdout, stderr := runPackHook(t, bin, repo, command, stopPayload(repo, transcript, true), env)
		silent(t, code, stdout, stderr)
	})

	t.Run("a_turn_without_go_paths_skips_and_says_so", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		worktree(t, repo)
		env, log, _ := stubEnv(t)
		code, stdout, stderr := runPackHook(t, bin, repo, command, cleanTurn(t, repo, "README.md"), env)
		if got := message(t, code, stdout, stderr); got != skipMessage(repo) {
			t.Fatalf("message %q, want %q", got, skipMessage(repo))
		}
		equalLines(t, runs(log))
	})

	for _, rel := range []string{"internal/x/a.go", "go.mod", "go.sum", ".golangci.yml", ".golangci-version"} {
		t.Run("a_go_path_lints/"+rel, func(t *testing.T) {
			repo, command := newRepo(t, true, "")
			worktree(t, repo)
			env, log, _ := stubEnv(t)
			code, stdout, stderr := runPackHook(t, bin, repo, command, cleanTurn(t, repo, rel), env)
			if code != 0 {
				t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
			}
			equalLines(t, runs(log), "run ./... @ "+repo)
		})
	}

	t.Run("a_skipped_root_needs_no_go_tooling", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		worktree(t, repo)
		dir := toolDir(t, bin, lintTools, "", nil)
		code, stdout, stderr := runPackHook(t, bin, repo, command, cleanTurn(t, repo, "README.md"), bareEnv(t, dir))
		got := message(t, code, stdout, stderr)
		if !strings.Contains(got, "not checked — the turn touched no Go path") || strings.Contains(got, "COULD NOT CHECK") {
			t.Fatalf("message %q", got)
		}
	})

	t.Run("an_untouched_root_is_named_next_to_a_finding", func(t *testing.T) {
		repo, command := newRepo(t, true, "")
		wt := worktree(t, repo)
		env, log, _ := stubEnv(t, "STUB_FAIL_IN="+wt, "STUB_RUN_OUT="+lintFinding)
		pastThisSecond()
		transcript := writeTranscript(t, filepath.Join(t.TempDir(), "transcript.jsonl"),
			promptEntry(repo, stamp(time.Now())),
			toolEntry("Edit", map[string]any{"file_path": filepath.Join(wt, "a.go")}, repo, toolTS),
			toolEntry("Bash", map[string]any{"command": "ls"}, repo, toolTS),
			resultEntry(repo, resultTS))
		code, stdout, stderr := runPackHook(t, bin, repo, command, stopPayload(repo, transcript, false), env)
		if code != 2 {
			t.Fatalf("rc %d, want 2\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, lintFinding) || !strings.Contains(stderr, skipMessage(repo)) {
			t.Errorf("stderr lacks the finding or the skip message:\n%s", stderr)
		}
		equalLines(t, runs(log), "run ./... @ "+wt)
	})
}

// pastThisSecond waits until the next whole second: the Stop gate compares the prompt of the
// turn with commits at second precision, so a commit of the setup in the same second would
// count as part of the turn.
func pastThisSecond() {
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Second).Add(1020 * time.Millisecond)))
}
