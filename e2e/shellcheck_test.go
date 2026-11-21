//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
)

const (
	cleanSh = "#!/usr/bin/env bash\nset -euo pipefail\necho \"hallo\"\n"
	// SC2034: `x` is never used. Warning severity, so it blocks everywhere.
	brokenSh = "#!/usr/bin/env bash\nf() {\n  local x=1\n  echo \"hoi\"\n}\nf\n"
	// SC2001: style severity only, so it blocks only in the strict directories.
	styleOnlySh = "#!/usr/bin/env bash\nv=\"a b\"\necho \"$v\" | sed 's/a/c/'\n"

	shellUnchecked = "shell-lint-check: COULD NOT CHECK — "
	shellPackages  = "Packages: core, shell (found tools/ok.sh)\n"
)

// shellHookTools is what the hook needs on a PATH that holds nothing else.
var shellHookTools = []string{"env", "bash", "git", "dirname", "sed", "grep", "cut", "uname", "cat", "mktemp", "rm", "jq"}

// shellLib is the lib of the installed shell pack in repo.
func shellLib(repo string) string {
	return filepath.Join(repo, ".lighthouse", "scripts", "lib", "shellcheck-bin.sh")
}

// shellRepo is a repository with core and shell installed, and a clean script as trigger file.
func shellRepo(t *testing.T, bin string) string {
	t.Helper()
	return packRepo(t, bin, map[string]string{"tools/ok.sh": "#!/usr/bin/env bash\necho \"ok\"\n"}, shellPackages)
}

var pinned struct {
	once sync.Once
	path string
	err  string
}

// pinnedShellcheck is the pinned shellcheck, resolved once from this checkout (cache, PATH or
// download). No pin is a failure, never a skip: the gate tests would say nothing.
func pinnedShellcheck(t *testing.T) string {
	t.Helper()
	pinned.once.Do(func() {
		root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			pinned.err = fmt.Sprintf("git rev-parse: %v", err)
			return
		}
		checkout := strings.TrimSpace(string(root))
		lib := filepath.Join(checkout, "pack", "shell", "scripts", "lib", "shellcheck-bin.sh")
		cmd := exec.Command("bash", "-c", fmt.Sprintf(". %q\nresolve_shellcheck\n", lib))
		cmd.Dir = checkout
		cmd.Env = overrideEnv(cleanEnv(), "SHELLCHECK_BIN=")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil || stderr.Len() > 0 {
			pinned.err = fmt.Sprintf("resolve_shellcheck: %v\n%s", err, stderr.String())
			return
		}
		pinned.path = strings.TrimSpace(stdout.String())
	})
	if pinned.err != "" {
		t.Fatalf("the pinned shellcheck is not available: %s", pinned.err)
	}
	return pinned.path
}

// overrideEnv is base with the KEY=value entries of extra replacing the same keys.
func overrideEnv(base []string, extra ...string) []string {
	drop := map[string]bool{}
	for _, kv := range extra {
		key, _, _ := strings.Cut(kv, "=")
		drop[key] = true
	}
	var env []string
	for _, kv := range base {
		if key, _, _ := strings.Cut(kv, "="); !drop[key] {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

// pathWith is a PATH value with dir first and the PATH of the test process after it.
func pathWith(dir string) string {
	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// writeExe writes an executable script dir/name.
func writeExe(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeShellcheck is a script that reports version and exits 0 on everything else.
func fakeShellcheck(version string) string {
	return "#!/usr/bin/env bash\n" +
		"if [ \"${1:-}\" = \"--version\" ]; then\n" +
		"  echo \"ShellCheck - shell script analysis tool\"\n" +
		"  echo \"version: " + version + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
}

const failingCurl = "#!/usr/bin/env bash\nexit 1\n"

// crashingShellcheck is a shellcheck that fails with rc and a message on stderr.
func crashingShellcheck(t *testing.T, rc int) string {
	t.Helper()
	return writeExe(t, t.TempDir(), fmt.Sprintf("shellcheck-rc%d", rc), fmt.Sprintf("#!/usr/bin/env bash\necho boom >&2\nexit %d\n", rc))
}

// tomlFile writes a lighthouse.toml with body in a temporary directory and returns its path.
func tomlFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(p, []byte("[project]\nlanguage = \"en\"\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// pinVersion is SHELLCHECK_PIN as the lib in repo says it.
func pinVersion(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(shellLib(repo))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^SHELLCHECK_PIN=(\S+)`).FindSubmatch(data)
	if m == nil {
		t.Fatal("SHELLCHECK_PIN not found in the lib")
	}
	return string(m[1])
}

// runIn runs the command with args in dir under env and returns rc, stdout and stderr.
func runIn(t *testing.T, dir string, env []string, name string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	if err != nil {
		t.Fatalf("%s did not run: %v", name, err)
	}
	return 0, stdout.String(), stderr.String()
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func TestShellHooksRegistered(t *testing.T) {
	bin := buildLighthouse(t)
	repo := shellRepo(t, bin)
	command, timeout := registeredHook(t, repo, "PostToolUse", "Edit|Write", "shell-lint-check.sh")
	if want := `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/shell-lint-check.sh`; command != want {
		t.Errorf("command %q, want %q", command, want)
	}
	if timeout != 60 {
		t.Errorf("timeout %d, want 60", timeout)
	}
}

func TestShellLintCheck(t *testing.T) {
	needTool(t, "jq")
	bin := buildLighthouse(t)
	repo := shellRepo(t, bin)
	command, _ := registeredHook(t, repo, "PostToolUse", "Edit|Write", "shell-lint-check.sh")

	// edit writes text at rel in dir and returns its path.
	edit := func(dir, rel, text string) string {
		writeFile(t, dir, rel, text)
		return filepath.Join(dir, rel)
	}
	// hook runs the registered command for an edit of path; SHELLCHECK_BIN is empty unless env sets it.
	hook := func(path string, env ...string) (int, string, string) {
		t.Helper()
		payload := map[string]any{"tool_input": map[string]string{"file_path": path}}
		return runPackHook(t, bin, repo, command, payload, append([]string{"SHELLCHECK_BIN="}, env...))
	}
	wantUnchecked := func(t *testing.T, code int, stdout, stderr string) {
		t.Helper()
		if code != 2 {
			t.Fatalf("rc %d, want 2\n%s%s", code, stdout, stderr)
		}
		if !strings.HasPrefix(firstLine(stderr), shellUnchecked) {
			t.Fatalf("first line %q does not start with %q", firstLine(stderr), shellUnchecked)
		}
	}

	t.Run("passes_clean_script", func(t *testing.T) {
		code, stdout, stderr := hook(edit(repo, "clean.sh", cleanSh), "SHELLCHECK_BIN="+pinnedShellcheck(t))
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("blocks_a_deliberately_broken_script", func(t *testing.T) {
		code, stdout, stderr := hook(edit(repo, "broken.sh", brokenSh), "SHELLCHECK_BIN="+pinnedShellcheck(t))
		if code != 2 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "SC2034") {
			t.Fatalf("no SC2034 in:\n%s", stderr)
		}
		if want := "shell-lint-check: fix this in the code"; !strings.HasPrefix(firstLine(stderr), want) {
			t.Fatalf("first line %q does not start with %q", firstLine(stderr), want)
		}
		if got := bashwrites.Classify(code, []byte(stderr)); got != "finding" {
			t.Fatalf("classify: %q, want finding", got)
		}
	})

	t.Run("ignores_non_shell", func(t *testing.T) {
		code, stdout, stderr := hook(edit(repo, "notes.md", "# hoi\n"))
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("lets_style_pass_outside_the_strict_dirs", func(t *testing.T) {
		code, stdout, stderr := hook(edit(repo, "styleonly.sh", styleOnlySh), "SHELLCHECK_BIN="+pinnedShellcheck(t))
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("strict_default_covers_lighthouse_hooks", func(t *testing.T) {
		path := edit(repo, ".claude/hooks/lighthouse/styleonly.sh", styleOnlySh)
		code, stdout, stderr := hook(path, "SHELLCHECK_BIN="+pinnedShellcheck(t))
		if code != 2 || !strings.Contains(stderr, "SC2001") {
			t.Fatalf("rc %d, want 2 with SC2001\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("strict_dirs_from_config", func(t *testing.T) {
		env := []string{"SHELLCHECK_BIN=" + pinnedShellcheck(t), "LIGHTHOUSE_TOML=" + tomlFile(t, "[shell]\nstrict_dirs = [\"ops\"]\n")}
		code, stdout, stderr := hook(edit(repo, "ops/styleonly.sh", styleOnlySh), env...)
		if code != 2 || !strings.Contains(stderr, "SC2001") {
			t.Fatalf("inside: rc %d, want 2 with SC2001\n%s%s", code, stdout, stderr)
		}
		code, stdout, stderr = hook(edit(repo, ".claude/hooks/styleonly.sh", styleOnlySh), env...)
		if code != 0 {
			t.Fatalf("outside: rc %d, want 0\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("strict_dirs_does_not_match_a_prefix", func(t *testing.T) {
		env := []string{"SHELLCHECK_BIN=" + pinnedShellcheck(t), "LIGHTHOUSE_TOML=" + tomlFile(t, "[shell]\nstrict_dirs = [\"ops\"]\n")}
		code, stdout, stderr := hook(edit(repo, "opsx/styleonly.sh", styleOnlySh), env...)
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("broken_config_is_unchecked", func(t *testing.T) {
		toml := tomlFile(t, "[shell]\nstrict_dirs = \"ops\"\n")
		code, stdout, stderr := hook(edit(repo, "clean.sh", cleanSh), "LIGHTHOUSE_TOML="+toml)
		wantUnchecked(t, code, stdout, stderr)
	})

	t.Run("fails_loud_when_the_tool_is_missing", func(t *testing.T) {
		code, stdout, stderr := hook(edit(repo, "clean.sh", cleanSh), "SHELLCHECK_BIN=/nowhere/shellcheck")
		wantUnchecked(t, code, stdout, stderr)
		if got := bashwrites.Classify(code, []byte(stderr)); got != "unchecked" {
			t.Fatalf("classify: %q, want unchecked", got)
		}
	})

	t.Run("unchecked_first_line", func(t *testing.T) {
		// A lighthouse root with core only: no shellcheck-repo.sh under it.
		coreOnly := packRepo(t, bin, nil, "Packages: core\n")
		code, stdout, stderr := hook(edit(repo, "clean.sh", cleanSh), "LIGHTHOUSE_REPO_ROOT="+coreOnly)
		wantUnchecked(t, code, stdout, stderr)
		if !strings.Contains(stderr, ".lighthouse/scripts/shellcheck-repo.sh") {
			t.Fatalf("no script name in:\n%s", stderr)
		}
		if got := bashwrites.Classify(code, []byte(stderr)); got != "unchecked" {
			t.Fatalf("classify: %q, want unchecked", got)
		}
	})

	t.Run("script_found_in_the_file_root_without_env", func(t *testing.T) {
		// The script of the project directory fails, the one in the repository of the edited file works: without
		// LIGHTHOUSE_REPO_ROOT the root is that of the edited file, as for an edit in a worktree.
		other := shellRepo(t, bin)
		bare := shellRepo(t, bin)
		writeExe(t, filepath.Join(bare, ".lighthouse", "scripts"), "shellcheck-repo.sh", "#!/bin/bash\nexit 4\n")
		bareCommand, _ := registeredHook(t, bare, "PostToolUse", "Edit|Write", "shell-lint-check.sh")
		payload := map[string]any{"tool_input": map[string]string{"file_path": edit(other, "broken.sh", brokenSh)}}
		code, stdout, stderr := runPackHook(t, bin, bare, bareCommand, payload, []string{"SHELLCHECK_BIN=" + pinnedShellcheck(t)})
		if code != 2 || !strings.Contains(stderr, "SC2034") {
			t.Fatalf("rc %d, want 2 with SC2034\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("divergent_version_notice_reaches_the_session", func(t *testing.T) {
		dir := toolDir(t, bin, nil, "", map[string]string{"shellcheck": fakeShellcheck("0.9.0"), "curl": failingCurl})
		code, stdout, stderr := hook(edit(repo, "clean.sh", cleanSh), pathWith(dir))
		if code != 0 {
			t.Fatalf("rc %d\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, `"systemMessage":"shell-lint-check: `) || !strings.Contains(stdout, "differs from the pin") {
			t.Fatalf("no systemMessage about the pin in stdout:\n%s", stdout)
		}
	})

	t.Run("without_jq_could_not_check", func(t *testing.T) {
		dir := toolDir(t, bin, shellHookTools, "jq", nil)
		code, stdout, stderr := hook(edit(repo, "broken.sh", brokenSh), "PATH="+dir, "SHELLCHECK_BIN="+pinnedShellcheck(t))
		wantUnchecked(t, code, stdout, stderr)
		if !strings.Contains(stderr, "jq is not present") {
			t.Fatalf("no jq reason in:\n%s", stderr)
		}
	})

	t.Run("without_jq_a_non_shell_edit_passes", func(t *testing.T) {
		dir := toolDir(t, bin, shellHookTools, "jq", nil)
		code, stdout, stderr := hook(edit(repo, "notes.md", "# notes\n"), "PATH="+dir)
		if code != 0 || stderr != "" {
			t.Fatalf("rc %d, stderr %q\n%s", code, stderr, stdout)
		}
	})

	t.Run("without_lighthouse_could_not_check", func(t *testing.T) {
		dir := toolDir(t, bin, shellHookTools, "lighthouse", nil)
		code, stdout, stderr := hook(edit(repo, "broken.sh", brokenSh), "PATH="+dir, "SHELLCHECK_BIN="+pinnedShellcheck(t))
		wantUnchecked(t, code, stdout, stderr)
		if !strings.Contains(stderr, "lighthouse not on PATH") {
			t.Fatalf("no lighthouse reason in:\n%s", stderr)
		}
	})

	t.Run("shellcheck_failure_is_unchecked", func(t *testing.T) {
		code, stdout, stderr := hook(edit(repo, "clean.sh", cleanSh), "SHELLCHECK_BIN="+crashingShellcheck(t, 3))
		wantUnchecked(t, code, stdout, stderr)
		if got := bashwrites.Classify(code, []byte(stderr)); got != "unchecked" {
			t.Fatalf("classify: %q, want unchecked", got)
		}
	})

	t.Run("bash_write_through_gate", func(t *testing.T) {
		// A Bash write of a broken .sh is a finding via `bash-write-check post`, not
		// `no shell gate installed`.
		preCmd := eventHookCommand(t, repo, "PreToolUse", "bash-write-check pre")
		postCmd := eventHookCommand(t, repo, "PostToolUse", "bash-write-check post")
		xdg := t.TempDir()
		env := []string{"XDG_RUNTIME_DIR=" + xdg, "SHELLCHECK_BIN=" + pinnedShellcheck(t)}
		bashCmd := "cat > x.sh <<'EOF'\n" + brokenSh + "EOF\n"
		payload := func(event string) map[string]any {
			return map[string]any{
				"session_id": "sess-shell", "tool_use_id": "toolu_shell", "hook_event_name": event,
				"tool_name": "Bash", "cwd": repo, "tool_input": map[string]string{"command": bashCmd},
			}
		}
		if code, stdout, stderr := runPackHook(t, bin, repo, preCmd, payload("PreToolUse"), env); code != 0 {
			t.Fatalf("pre: rc %d\n%s%s", code, stdout, stderr)
		}
		if code, _, stderr := runIn(t, repo, hookEnv(bin), "bash", "-c", bashCmd); code != 0 {
			t.Fatalf("the Bash call: rc %d\n%s", code, stderr)
		}
		code, stdout, stderr := runPackHook(t, bin, repo, postCmd, payload("PostToolUse"), env)
		if code != 2 {
			t.Fatalf("post: rc %d, want 2\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "SC2034") {
			t.Fatalf("no SC2034 in:\n%s", stderr)
		}
		if strings.Contains(stderr+stdout, "no shell gate installed") {
			t.Fatalf("the shell gate was not found:\n%s%s", stdout, stderr)
		}
	})
}

func TestShellcheckRepo(t *testing.T) {
	bin := buildLighthouse(t)
	repo := shellRepo(t, bin)
	script := filepath.Join(repo, ".lighthouse", "scripts", "shellcheck-repo.sh")
	// runScript runs shellcheck-repo.sh in repo; SHELLCHECK_BIN is empty unless env sets it.
	runScript := func(env []string, args ...string) (int, string, string) {
		t.Helper()
		full := overrideEnv(append(hookEnv(bin), gitEnv...), append([]string{"SHELLCHECK_BIN="}, env...)...)
		return runIn(t, repo, full, "bash", append([]string{script}, args...)...)
	}

	t.Run("strict_set_from_config", func(t *testing.T) {
		// Without files the script checks `shell.strict_dirs`, at style, new files included.
		env := []string{"SHELLCHECK_BIN=" + pinnedShellcheck(t), "LIGHTHOUSE_TOML=" + tomlFile(t, "[shell]\nstrict_dirs = [\"ops\"]\n")}
		writeFile(t, repo, "ops/clean.sh", cleanSh)
		writeFile(t, repo, "elsewhere/styleonly.sh", styleOnlySh)
		if code, stdout, stderr := runScript(env); code != 0 {
			t.Fatalf("rc %d, want 0\n%s%s", code, stdout, stderr)
		}
		writeFile(t, repo, "ops/sub/styleonly.sh", styleOnlySh)
		code, stdout, stderr := runScript(env)
		if code != 1 || !strings.Contains(stdout, "SC2001") {
			t.Fatalf("rc %d, want 1 with SC2001 on stdout\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("empty_strict_set_is_not_green", func(t *testing.T) {
		env := []string{"SHELLCHECK_BIN=" + pinnedShellcheck(t), "LIGHTHOUSE_TOML=" + tomlFile(t, "[shell]\nstrict_dirs = []\n")}
		code, stdout, stderr := runScript(env)
		if code != 2 || !strings.Contains(stderr, "this is not green") {
			t.Fatalf("rc %d, want 2 with \"this is not green\"\n%s%s", code, stdout, stderr)
		}
	})

	t.Run("unchecked_is_rc_4", func(t *testing.T) {
		writeFile(t, repo, "clean.sh", cleanSh)
		code, stdout, stderr := runScript([]string{"SHELLCHECK_BIN=/nowhere/shellcheck"}, "--", filepath.Join(repo, "clean.sh"))
		if code != 4 || !strings.Contains(stderr, "is not executable") {
			t.Fatalf("rc %d, want 4 with \"is not executable\"\n%s%s", code, stdout, stderr)
		}
	})

	// shellcheck's own rc 2-4 (a file it could not read, bad options) or a crash is "not
	// checked", not "findings".
	for _, rc := range []int{2, 3, 4, 126} {
		t.Run(fmt.Sprintf("shellcheck_failure_rc_%d_is_rc_4", rc), func(t *testing.T) {
			writeFile(t, repo, "clean.sh", cleanSh)
			code, stdout, stderr := runScript([]string{"SHELLCHECK_BIN=" + crashingShellcheck(t, rc)}, "--", filepath.Join(repo, "clean.sh"))
			if code != 4 {
				t.Fatalf("rc %d, want 4\n%s%s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, fmt.Sprintf("shellcheck exited with rc=%d", rc)) || !strings.Contains(stderr, "boom") {
				t.Fatalf("stderr lacks the rc and the message:\n%s", stderr)
			}
		})
	}
}

func TestShellcheckBin(t *testing.T) {
	bin := buildLighthouse(t)
	installed := shellRepo(t, bin)
	lib := shellLib(installed)
	pin := pinVersion(t, installed)
	// resolve runs fn from the lib in dir, with stubs first on the PATH and no SHELLCHECK_BIN.
	resolve := func(dir, stubs, fn string) (int, string, string) {
		t.Helper()
		env := overrideEnv(append(cleanEnv(), gitEnv...), pathWith(stubs), "SHELLCHECK_BIN=")
		return runIn(t, dir, env, "bash", "-c", fmt.Sprintf(". %q\n%s\n", lib, fn))
	}

	// Fails when the pinned download needs GNU sha256sum, which macOS lacks, or has no Mac
	// release (#90).
	for machine, sha := range map[string]string{
		"arm64":  "56affdd8de5527894dca6dc3d7e0a99a873b0f004d7aabc30ae407d3f48b0a79",
		"x86_64": "3c89db4edcab7cf1c27bff178882e0f6f27f7afdf54e859fa041fca10febe4c6",
	} {
		t.Run("download_verifies_with_shasum_when_sha256sum_is_missing_"+machine, func(t *testing.T) {
			work := t.TempDir()
			payload := filepath.Join(work, "payload", "shellcheck-v"+pin)
			if err := os.MkdirAll(payload, 0o755); err != nil {
				t.Fatal(err)
			}
			writeExe(t, payload, "shellcheck", fakeShellcheck(pin))
			archive := filepath.Join(work, "release.tar.xz")
			needTool(t, "tar")
			needTool(t, "xz")
			if out, err := exec.Command("tar", "-cJf", archive, "-C", filepath.Dir(payload), "shellcheck-v"+pin).CombinedOutput(); err != nil {
				t.Fatalf("tar: %v\n%s", err, out)
			}
			shasumLog := filepath.Join(work, "shasum.log")
			stubs := map[string]string{
				"uname": "#!/bin/bash\ncase \"$1\" in -s) echo Darwin ;; -m) echo " + machine + " ;; esac\n",
				"curl": "#!/bin/bash\nout=\"\"\nwhile [ $# -gt 0 ]; do [ \"$1\" = -o ] && out=\"$2\"; shift; done\n" +
					fmt.Sprintf("cp %q \"$out\"\n", archive),
				// BSD shasum: refuses to run without `-a 256`. The stub returns the pin hash.
				"shasum": fmt.Sprintf("#!/bin/bash\nprintf \"%%s\\n\" \"$*\" >> %q\n", shasumLog) +
					"[ \"$1\" = -a ] && [ \"$2\" = 256 ] || { echo \"shasum: needs -a 256\" >&2; exit 1; }\n" +
					"echo \"" + sha + "  $3\"\n",
			}
			// The real tooling the download needs, but no sha256sum.
			dir := toolDir(t, bin, []string{"bash", "git", "tar", "xz", "mktemp", "cut", "rm", "mkdir", "chmod", "dirname", "cp"}, "", stubs)
			toolsDir := filepath.Join(work, "tools")
			code, _, stderr := runIn(t, work, []string{"PATH=" + dir}, filepath.Join(dir, "bash"), "-c",
				fmt.Sprintf(". %q\n_sc_download %q\n", lib, toolsDir))
			if strings.Contains(stderr, "sha256 mismatch") || strings.Contains(stderr, "sha256sum: command not found") {
				t.Fatalf("verification failed:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("rc %d\n%s", code, stderr)
			}
			if info, err := os.Stat(filepath.Join(toolsDir, "shellcheck")); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("no shellcheck in %s: %v", toolsDir, err)
			}
			logged, err := os.ReadFile(shasumLog)
			if err != nil {
				t.Fatal(err)
			}
			if f := strings.Fields(string(logged)); len(f) < 2 || f[0] != "-a" || f[1] != "256" {
				t.Fatalf("shasum called with %q, want -a 256", logged)
			}
		})
	}

	t.Run("cache_dir_is_lighthouse_tools", func(t *testing.T) {
		// One cache per clone, shared by its worktrees: `<git-common-dir>/lighthouse-tools`.
		repo := freshRepo(t)
		wt := filepath.Join(t.TempDir(), "wt")
		git(t, repo, "worktree", "add", "-q", wt)
		for _, d := range []string{filepath.Join(repo, "sub", "deep"), filepath.Join(wt, "sub")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		want, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
		if err != nil {
			t.Fatal(err)
		}
		want = filepath.Join(want, "lighthouse-tools")
		// From a subdirectory too: there git prints the common dir relative to the cwd.
		for _, where := range []string{repo, filepath.Join(repo, "sub", "deep"), wt, filepath.Join(wt, "sub")} {
			code, stdout, stderr := resolve(where, t.TempDir(), "_sc_tool_dir")
			if code != 0 {
				t.Fatalf("%s: rc %d\n%s", where, code, stderr)
			}
			// The directory need not exist yet: resolve its parent, which does.
			parent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(strings.TrimSpace(stdout))))
			if err != nil {
				t.Fatal(err)
			}
			got := filepath.Join(parent, filepath.Base(strings.TrimSpace(stdout)))
			if got != want {
				t.Fatalf("%s: cache dir %q, want %q", where, got, want)
			}
		}
	})

	t.Run("resolve_prefers_the_pinned_cache_over_a_divergent_path_version", func(t *testing.T) {
		// The strict set is clean UNDER THE PIN. A divergent PATH version must never push the
		// pin aside: the same tree would be red on one runner and green on the other.
		repo := freshRepo(t)
		cache := filepath.Join(repo, ".git", "lighthouse-tools")
		if err := os.MkdirAll(cache, 0o755); err != nil {
			t.Fatal(err)
		}
		writeExe(t, cache, "shellcheck", fakeShellcheck(pin))
		stubs := t.TempDir()
		writeExe(t, stubs, "shellcheck", fakeShellcheck("0.9.0"))
		// No network: without the cache the divergent PATH version would be the answer.
		writeExe(t, stubs, "curl", failingCurl)
		code, stdout, stderr := resolve(repo, stubs, "resolve_shellcheck")
		if code != 0 {
			t.Fatalf("rc %d\n%s", code, stderr)
		}
		if got := strings.TrimSpace(stdout); got != filepath.Join(cache, "shellcheck") {
			t.Fatalf("resolved %q, want the cache", got)
		}
	})

	t.Run("resolve_uses_a_pinned_path_version_without_downloading", func(t *testing.T) {
		repo := freshRepo(t)
		stubs := t.TempDir()
		writeExe(t, stubs, "shellcheck", fakeShellcheck(pin))
		code, stdout, stderr := resolve(repo, stubs, "resolve_shellcheck")
		if code != 0 || stderr != "" {
			t.Fatalf("rc %d, stderr %q", code, stderr)
		}
		if got := strings.TrimSpace(stdout); got != filepath.Join(stubs, "shellcheck") {
			t.Fatalf("resolved %q, want the PATH version", got)
		}
	})

	t.Run("resolve_falls_back_to_a_divergent_path_version_when_the_pin_is_unreachable", func(t *testing.T) {
		// Offline with only an old shellcheck: looking with the wrong version beats not looking,
		// but with a notice. `curl` is a stub that fails: unit tests do not go on the network.
		repo := freshRepo(t)
		stubs := t.TempDir()
		writeExe(t, stubs, "shellcheck", fakeShellcheck("0.9.0"))
		writeExe(t, stubs, "curl", failingCurl)
		code, stdout, stderr := resolve(repo, stubs, "resolve_shellcheck")
		if code != 0 {
			t.Fatalf("rc %d\n%s", code, stderr)
		}
		if got := strings.TrimSpace(stdout); got != filepath.Join(stubs, "shellcheck") {
			t.Fatalf("resolved %q, want the PATH version", got)
		}
		if !strings.Contains(stderr, "differs from the pin") {
			t.Fatalf("no notice in:\n%s", stderr)
		}
	})
}
