//go:build canary

package canary

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	shellLintFile = "shell/hooks/shell-lint-check.sh"
	cleanSh       = "#!/usr/bin/env bash\necho \"hi\"\n"
	strictStyleID = "strict-style"
)

// floorCodes are the SC codes of BLOCKING_RULES: every ID except the ones the hook marks as
// no shellcheck code (NON_LINTER_RULES, the strict threshold).
func floorCodes(t *testing.T) []string {
	t.Helper()
	skip := nonLinterRules(t, shellLintFile)
	var codes []string
	for _, id := range blockingRules(t, shellLintFile) {
		if !slices.Contains(skip, id) {
			codes = append(codes, id)
		}
	}
	if len(codes) == 0 {
		t.Fatal("shell-lint-check has no floor codes")
	}
	return codes
}

func readShellFixture(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "shell-lint-check", id+".sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runShellLint writes source at rel in root and runs shell-lint-check on that edit, with the pinned
// shellcheck unless env names another SHELLCHECK_BIN.
func runShellLint(t *testing.T, root, rel, source string, env ...string) run {
	t.Helper()
	file := filepath.Join(root, rel)
	writeFile(t, file, source)
	payload, err := json.Marshal(map[string]any{
		"cwd": root, "hook_event_name": "PostToolUse", "tool_name": "Write",
		"tool_input": map[string]any{"file_path": file},
	})
	if err != nil {
		t.Fatal(err)
	}
	env = append([]string{"SHELLCHECK_BIN=" + shellcheckBin(t)}, env...)
	return runHook(t, root, "shell-lint-check.sh", string(payload), env...)
}

// runFloorScript runs the installed `shellcheck-repo.sh <args>` in dir.
func runFloorScript(t *testing.T, root, dir string, args []string, env ...string) run {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, ".lighthouse", "scripts", "shellcheck-repo.sh"), args...)
	cmd.Dir = dir
	cmd.Env = append(append(cleanEnv(lighthouseBin(t)), "SHELLCHECK_BIN="+shellcheckBin(t)), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("shellcheck-repo.sh did not run: %v", err)
		}
		code = ee.ExitCode()
	}
	return run{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

func TestShellLintCanary(t *testing.T) {
	for _, id := range floorCodes(t) {
		t.Run(id, func(t *testing.T) {
			root := installRepo(t, "core", "shell")
			if strings.Contains(root, id) {
				t.Fatalf("the reason ID %q is in the repository path %s", id, root)
			}
			requireCanary(t, runShellLint(t, root, "canary.sh", readShellFixture(t, id)), id, id+" (")
		})
	}
}

// The strict threshold: a style-only finding blocks in a strict directory, tagged with the
// threshold ID, and the same file outside one passes.
func TestShellLintStrictStyleCanary(t *testing.T) {
	if !slices.Contains(blockingRules(t, shellLintFile), strictStyleID) {
		t.Fatalf("BLOCKING_RULES lacks %s", strictStyleID)
	}
	root := installRepo(t, "core", "shell")
	source := readShellFixture(t, strictStyleID)
	r := runShellLint(t, root, filepath.Join(".claude", "hooks", "canary.sh"), source)
	requireCanary(t, r, strictStyleID, "[shell-lint-check/"+strictStyleID+"]")
	if !strings.Contains(r.Stderr, "SC2001 (") {
		t.Errorf("the strict block does not show the style finding SC2001:\n%s", r.Stderr)
	}
	if r := runShellLint(t, root, "canary.sh", source); r.Code != 0 {
		t.Fatalf("the style finding outside a strict dir: exit %d, want 0\nstderr: %s", r.Code, r.Stderr)
	}
}

// A warning in a strict directory blocks without the threshold ID: the threshold is not the reason.
func TestShellLintStrictDirWarningIsNotTheThreshold(t *testing.T) {
	root := installRepo(t, "core", "shell")
	r := runShellLint(t, root, filepath.Join(".claude", "hooks", "canary.sh"), readShellFixture(t, "SC2034"))
	requireCanary(t, r, "SC2034", "SC2034 (")
	if strings.Contains(r.Stderr, "[shell-lint-check/"+strictStyleID+"]") {
		t.Errorf("a warning is tagged with the style threshold:\n%s", r.Stderr)
	}
}

// hostileDir holds the script of the floor tests: a subdirectory, so an rc beside the script is
// another file than the one in the repository root.
const hostileDir = "sub"

// floorChannel switches one SC code off for the scripts of root, by one of the routes shellcheck
// reads; it returns the extra environment of the hook.
type floorChannel func(t *testing.T, root, code string) []string

var floorChannels = map[string]floorChannel{
	"rc beside the script": func(t *testing.T, root, code string) []string {
		writeFile(t, filepath.Join(root, hostileDir, ".shellcheckrc"), "disable="+code+"\n")
		return nil
	},
	"repo rc": func(t *testing.T, root, code string) []string {
		writeFile(t, filepath.Join(root, ".shellcheckrc"), "disable="+code+"\n")
		return nil
	},
	"rc above the repo": func(t *testing.T, root, code string) []string {
		writeFile(t, filepath.Join(filepath.Dir(root), ".shellcheckrc"), "disable="+code+"\n")
		return nil
	},
	"code range": func(t *testing.T, root, _ string) []string {
		writeFile(t, filepath.Join(root, ".shellcheckrc"), "disable=SC2000-SC3000\n")
		return nil
	},
	"SHELLCHECK_OPTS": func(_ *testing.T, _, code string) []string {
		return []string{"SHELLCHECK_OPTS=-e " + code}
	},
	"XDG rc": func(t *testing.T, root, code string) []string {
		dir := filepath.Join(filepath.Dir(root), "xdg")
		writeFile(t, filepath.Join(dir, "shellcheckrc"), "disable="+code+"\n")
		return []string{"XDG_CONFIG_HOME=" + dir}
	},
	"home rc": func(t *testing.T, root, code string) []string {
		dir := filepath.Join(filepath.Dir(root), "home")
		writeFile(t, filepath.Join(dir, ".shellcheckrc"), "disable="+code+"\n")
		return []string{"HOME=" + dir}
	},
}

// The floor: every route that switches a floor code off blocks with its ID on a clean script, so
// the hook does not rely on a finding that the configuration hides.
func TestShellFloorHostile(t *testing.T) {
	for name, channel := range floorChannels {
		for _, code := range floorCodes(t) {
			t.Run(name+"/"+code, func(t *testing.T) {
				root := installRepo(t, "core", "shell")
				env := channel(t, root, code)
				r := runShellLint(t, root, filepath.Join(hostileDir, "canary.sh"), cleanSh, env...)
				requireCanary(t, r, code, "[shell-lint-check/"+code+"]")
				if !strings.Contains(r.Stderr, "# shellcheck disable=") {
					t.Errorf("the block does not name an inline disable with a reason as the route for debt:\n%s", r.Stderr)
				}
			})
		}
	}
}

// The control of TestShellFloorHostile: without a hostile route the clean script passes, so the
// block there comes from the route.
func TestShellLintCleanScriptPasses(t *testing.T) {
	root := installRepo(t, "core", "shell")
	if r := runShellLint(t, root, "canary.sh", cleanSh); r.Code != 0 || r.Stdout != "" {
		t.Fatalf("a clean script: exit %d\nstdout: %s\nstderr: %s", r.Code, r.Stdout, r.Stderr)
	}
	// A severity in SHELLCHECK_OPTS gives way to the hook's own, in the lint run and the floor
	// check alike: it switches no floor code off.
	if r := runShellLint(t, root, "canary.sh", cleanSh, "SHELLCHECK_OPTS=-S error"); r.Code != 0 {
		t.Fatalf("a clean script under SHELLCHECK_OPTS=-S error: exit %d\nstderr: %s", r.Code, r.Stderr)
	}
}

// The floor is no ceiling: an optional check the adopter switches on keeps blocking (no --norc).
func TestShellFloorKeepsAdopterEnable(t *testing.T) {
	const upper = "#!/usr/bin/env bash\necho \"$UNSET_UPPER\"\n"
	root := installRepo(t, "core", "shell")
	if r := runShellLint(t, root, "canary.sh", upper); r.Code != 0 {
		t.Fatalf("without the rc the uppercase variable blocks: exit %d\n%s", r.Code, r.Stderr)
	}
	writeFile(t, filepath.Join(root, ".shellcheckrc"), "enable=check-unassigned-uppercase\n")
	requireCanary(t, runShellLint(t, root, "canary.sh", upper), "SC2154", "SC2154 (")
}

// The floor holds only codes that block outside the strict directories too.
func TestShellFloorCodesAreWarningOrError(t *testing.T) {
	bin := shellcheckBin(t)
	for _, code := range floorCodes(t) {
		t.Run(code, func(t *testing.T) {
			// A measurement of the code's own level: no SHELLCHECK_OPTS and no .shellcheckrc
			// of this machine may change it (--norc is forbidden in the hook, not here).
			cmd := exec.Command(bin, "--norc", "-f", "json", "-S", "style", filepath.Join("testdata", "shell-lint-check", code+".sh"))
			cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "SHELLCHECK_OPTS=") })
			out, err := cmd.Output()
			if ee, ok := err.(*exec.ExitError); err != nil && (!ok || ee.ExitCode() != 1) {
				t.Fatalf("shellcheck: %v", err)
			}
			var findings []struct {
				Code  int    `json:"code"`
				Level string `json:"level"`
			}
			if err := json.Unmarshal(out, &findings); err != nil {
				t.Fatalf("shellcheck -f json: %v\n%s", err, out)
			}
			found := false
			for _, f := range findings {
				if "SC"+strconv.Itoa(f.Code) != code {
					continue
				}
				found = true
				if f.Level != "warning" && f.Level != "error" {
					t.Errorf("%s has level %q, want warning or error", code, f.Level)
				}
			}
			if !found {
				t.Errorf("the fixture of %s does not report %s: %s", code, code, out)
			}
		})
	}
}

// The probe of every floor code fires in a clean environment: no false floor block.
func TestShellFloorProbeFiresForEveryCode(t *testing.T) {
	root := installRepo(t, "core", "shell")
	r := runFloorScript(t, root, root, []string{"--check-floor", strings.Join(floorCodes(t), ",")})
	if r.Code != 0 || r.Stdout != "" {
		t.Fatalf("--check-floor in a clean repository: exit %d\nstdout: %s\nstderr: %s", r.Code, r.Stdout, r.Stderr)
	}
}

// --check-floor names each code that is off, one per line, and exits 1.
func TestShellFloorProbeListsTheMissingCodes(t *testing.T) {
	root := installRepo(t, "core", "shell")
	r := runFloorScript(t, root, root, []string{"--check-floor", "SC2164,SC2046,SC2034"}, "SHELLCHECK_OPTS=-e SC2164 -e SC2034")
	if r.Code != 1 || r.Stdout != "SC2164\nSC2034\n" {
		t.Fatalf("exit %d, stdout %q; want 1 and SC2164, SC2034\nstderr: %s", r.Code, r.Stdout, r.Stderr)
	}
}

// fakeShellcheck is an executable that answers every call with rc and nothing on stdout.
func fakeShellcheck(t *testing.T, rc string) string {
	t.Helper()
	dir, err := os.MkdirTemp(workDir, "fake")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := filepath.Join(dir, "shellcheck")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho boom >&2\nexit "+rc+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// The failure branches of --check-floor: none of them is a pass or a floor finding.
func TestShellFloorProbeCouldNotCheck(t *testing.T) {
	root := installRepo(t, "core", "shell")
	cases := []struct {
		name string
		args []string
		env  []string
		code int
		want string
	}{
		{"shellcheck missing", []string{"--check-floor", "SC2164"}, []string{"SHELLCHECK_BIN=/nowhere/shellcheck"}, 4, "is not executable"},
		{"shellcheck fails", []string{"--check-floor", "SC2164"}, []string{"SHELLCHECK_BIN=" + fakeShellcheck(t, "3")}, 4, "rc=3"},
		{"unknown code", []string{"--check-floor", "SC9999"}, nil, 2, "SC9999"},
		{"no codes", []string{"--check-floor"}, nil, 2, "usage"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runFloorScript(t, root, root, c.args, c.env...)
			if r.Code != c.code || !strings.Contains(r.Stderr, c.want) || r.Stdout != "" {
				t.Fatalf("exit %d, want %d with %q on stderr and no stdout\nstdout: %s\nstderr: %s", r.Code, c.code, c.want, r.Stdout, r.Stderr)
			}
		})
	}
}

// The hook says COULD NOT CHECK, naming the floor check, when that check could not look.
func TestShellLintFloorCouldNotCheck(t *testing.T) {
	root := installRepo(t, "core", "shell")
	for name, bin := range map[string]string{
		"shellcheck missing": "/nowhere/shellcheck",
		"shellcheck fails":   fakeShellcheck(t, "3"),
	} {
		t.Run(name, func(t *testing.T) {
			r := runShellLint(t, root, "canary.sh", cleanSh, "SHELLCHECK_BIN="+bin)
			first, _, _ := strings.Cut(r.Stderr, "\n")
			if r.Code != 2 || !strings.HasPrefix(first, "shell-lint-check: COULD NOT CHECK — ") || !strings.Contains(first, "--check-floor") {
				t.Fatalf("exit %d, want 2 with a COULD NOT CHECK first line about --check-floor\nstderr: %s", r.Code, r.Stderr)
			}
		})
	}
}

// A shellcheck that sees nothing proves no floor code: the hook blocks on each of them.
func TestShellLintBlindShellcheckBlocksOnTheFloor(t *testing.T) {
	root := installRepo(t, "core", "shell")
	r := runShellLint(t, root, "canary.sh", cleanSh, "SHELLCHECK_BIN="+fakeShellcheck(t, "0"))
	for _, code := range floorCodes(t) {
		requireCanary(t, r, code, "[shell-lint-check/"+code+"]")
	}
}

// The shellcheck of the floor check is found from the hook's directory, as the lint run's is: a
// script outside any git repository is checked with the pinned shellcheck of the hook's tool
// cache, not reported as COULD NOT CHECK.
func TestShellLintFileOutsideGitIsChecked(t *testing.T) {
	root := installRepo(t, "core", "shell")
	cache := filepath.Join(root, ".git", "lighthouse-tools")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shellcheckBin(t), filepath.Join(cache, "shellcheck")); err != nil {
		t.Fatal(err)
	}
	outside, err := os.MkdirTemp(workDir, "o")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	file := filepath.Join(outside, "canary.sh")
	writeFile(t, file, readShellFixture(t, "SC2034"))
	payload, err := json.Marshal(map[string]any{"cwd": root, "tool_input": map[string]any{"file_path": file}})
	if err != nil {
		t.Fatal(err)
	}
	requireCanary(t, runHook(t, root, "shell-lint-check.sh", string(payload)), "SC2034", "SC2034 (")
}

func TestShellLintBijection(t *testing.T) {
	assertBijection(t, "shell-lint-check", blockingRules(t, shellLintFile))
}
