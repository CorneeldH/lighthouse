//go:build canary

package canary

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	psLintFile   = "powershell/hooks/powershell-lint-check.sh"
	psLintHook   = "powershell-lint-check.sh"
	psCouldNot   = "powershell-lint-check: COULD NOT CHECK"
	psCleanPs1   = "function Get-Answer {\n    param([int]$Base)\n    return $Base + 1\n}\n"
	psInvokeExpr = "PSAvoidUsingInvokeExpression"
)

// pssaVersion is the PSScriptAnalyzer version CI pins, from LIGHTHOUSE_PSSA_VERSION. cleanEnv drops
// every LIGHTHOUSE_* variable, so the tests pass it to the hook themselves. Under CI an empty value
// fails: the pin would go untested.
func pssaVersion(t *testing.T) string {
	t.Helper()
	v := os.Getenv("LIGHTHOUSE_PSSA_VERSION")
	if v == "" && os.Getenv("CI") != "" {
		t.Fatal("LIGHTHOUSE_PSSA_VERSION is empty under CI: the pinned PSScriptAnalyzer version goes untested")
	}
	return v
}

// pssaEnv is the hook environment that forwards the pinned version, if there is one.
func pssaEnv(t *testing.T) []string {
	t.Helper()
	if v := pssaVersion(t); v != "" {
		return []string{"LIGHTHOUSE_PSSA_VERSION=" + v}
	}
	return nil
}

// requirePwsh fails unless pwsh is on PATH and loads PSScriptAnalyzer with an empty HOME, as runHook
// gives the hook: a canary that cannot run says nothing, so it fails instead of skipping.
func requirePwsh(t *testing.T) {
	t.Helper()
	bin, err := exec.LookPath("pwsh")
	if err != nil {
		t.Fatalf("no pwsh on PATH for the powershell canaries: %v", err)
	}
	cmd := exec.Command(bin, "-NoProfile", "-NonInteractive", "-Command", "Import-Module PSScriptAnalyzer -ErrorAction Stop")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pwsh cannot load PSScriptAnalyzer with an empty HOME (install it for AllUsers): %v\n%s", err, out)
	}
}

func readPsFixture(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "powershell-lint-check", id+".ps1"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// psPayload is a PostToolUse Write payload for file.
func psPayload(t *testing.T, root, file string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"cwd": root, "hook_event_name": "PostToolUse", "tool_name": "Write",
		"tool_input": map[string]any{"file_path": file},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

// runPsLint writes source at rel in root and runs powershell-lint-check on that edit.
func runPsLint(t *testing.T, root, rel, source string, env ...string) run {
	t.Helper()
	file := filepath.Join(root, rel)
	writeFile(t, file, source)
	return runHook(t, root, psLintHook, psPayload(t, root, file), env...)
}

// requireCouldNotCheck demands exit 2 with a COULD NOT CHECK first line that holds want, and no
// rule tag: a failure is never a finding.
func requireCouldNotCheck(t *testing.T, r run, want string) {
	t.Helper()
	first, _, _ := strings.Cut(r.Stderr, "\n")
	if r.Code != 2 || !strings.HasPrefix(first, psCouldNot+" — ") || !strings.Contains(r.Stderr, want) || strings.Contains(r.Stderr, "[powershell-lint-check/") {
		t.Fatalf("exit %d, want 2 with a %q first line naming %q and no rule tag\nstdout: %s\nstderr: %s", r.Code, psCouldNot, want, r.Stdout, r.Stderr)
	}
}

// stubPwsh is a directory with a pwsh that writes msg to stderr and exits with rc, nothing on stdout.
func stubPwsh(t *testing.T, msg, rc string) string {
	t.Helper()
	dir, err := os.MkdirTemp(workDir, "pwsh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	script := "#!/bin/sh\necho '" + msg + "' >&2\nexit " + rc + "\n"
	if err := os.WriteFile(filepath.Join(dir, "pwsh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPowershellLintCanary(t *testing.T) {
	requirePwsh(t)
	ids := blockingRules(t, psLintFile)
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			root := installRepo(t, "core", "powershell")
			if strings.Contains(root, id) {
				t.Fatalf("the reason ID %q is in the repository path %s", id, root)
			}
			r := runPsLint(t, root, "canary.ps1", readPsFixture(t, id), pssaEnv(t)...)
			requireCanary(t, r, id, "[powershell-lint-check/"+id+"]")
			// One fixture per rule: it breaks no other blocking rule.
			for _, other := range ids {
				if other != id && strings.Contains(r.Stderr, "[powershell-lint-check/"+other+"]") {
					t.Errorf("the fixture of %s also breaks %s:\n%s", id, other, r.Stderr)
				}
			}
		})
	}
}

func TestPowershellLintBijection(t *testing.T) {
	problem, err := bijectionProblem("powershell-lint-check", blockingRules(t, psLintFile))
	if err != nil {
		t.Fatal(err)
	}
	if problem != "" {
		t.Error(problem)
	}
}

// The pin: with the pinned version the bad fixture blocks on its rule, so that version loaded; with
// a version that does not exist the same fixture is COULD NOT CHECK, so the version is enforced.
func TestPowershellLintVersionPin(t *testing.T) {
	requirePwsh(t)
	root := installRepo(t, "core", "powershell")
	source := readPsFixture(t, psInvokeExpr)
	if v := pssaVersion(t); v != "" {
		r := runPsLint(t, root, "canary.ps1", source, "LIGHTHOUSE_PSSA_VERSION="+v)
		requireCanary(t, r, psInvokeExpr, "[powershell-lint-check/"+psInvokeExpr+"]")
	} else {
		t.Log("LIGHTHOUSE_PSSA_VERSION is unset outside CI: only the enforcement half of the pin runs")
	}
	r := runPsLint(t, root, "canary.ps1", source, "LIGHTHOUSE_PSSA_VERSION=0.0.1")
	requireCouldNotCheck(t, r, "0.0.1")
}

// What must pass: a clean script, a script with only a non-blocking finding (Write-Host), and a
// payload that names no file.
func TestPowershellLintPasses(t *testing.T) {
	requirePwsh(t)
	root := installRepo(t, "core", "powershell")
	for name, source := range map[string]string{
		"clean":      psCleanPs1,
		"write-host": "Write-Host \"hi\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if r := runPsLint(t, root, "canary.ps1", source, pssaEnv(t)...); r.Code != 0 {
				t.Fatalf("exit %d, want 0\nstdout: %s\nstderr: %s", r.Code, r.Stdout, r.Stderr)
			}
		})
	}
	t.Run("psm1", func(t *testing.T) {
		requireCanary(t, runPsLint(t, root, "canary.psm1", readPsFixture(t, psInvokeExpr), pssaEnv(t)...), psInvokeExpr, "[powershell-lint-check/"+psInvokeExpr+"]")
	})
}

// No file in the payload, or a file deleted between the edit and the hook: nothing to check. These
// never reach pwsh, so they run without it.
func TestPowershellLintNothingToCheck(t *testing.T) {
	root := installRepo(t, "core", "powershell")
	noPath := `{"tool_name":"Write","tool_input":{"content":"x.ps1"}}`
	if r := runHook(t, root, psLintHook, noPath); r.Code != 0 {
		t.Fatalf("a payload without file_path: exit %d, want 0\nstderr: %s", r.Code, r.Stderr)
	}
	gone := filepath.Join(root, "gone.ps1")
	if r := runHook(t, root, psLintHook, psPayload(t, root, gone)); r.Code != 0 {
		t.Fatalf("a deleted file: exit %d, want 0\nstderr: %s", r.Code, r.Stderr)
	}
}

// Review focus: a path with spaces and parentheses reaches pwsh whole, and a BOM with CRLF line ends
// still blocks.
func TestPowershellLintHostileFiles(t *testing.T) {
	requirePwsh(t)
	root := installRepo(t, "core", "powershell")
	bad := readPsFixture(t, psInvokeExpr)
	t.Run("spaces and parentheses", func(t *testing.T) {
		r := runPsLint(t, root, filepath.Join("A B", "x (1) [2].ps1"), bad, pssaEnv(t)...)
		requireCanary(t, r, psInvokeExpr, "[powershell-lint-check/"+psInvokeExpr+"]")
		if !strings.Contains(r.Stderr, "x (1) [2].ps1") {
			t.Errorf("the block does not name the whole path:\n%s", r.Stderr)
		}
	})
	t.Run("BOM and CRLF", func(t *testing.T) {
		source := "\ufeff" + strings.ReplaceAll(psCleanPs1+bad, "\n", "\r\n")
		r := runPsLint(t, root, "bom.ps1", source, pssaEnv(t)...)
		requireCanary(t, r, psInvokeExpr, "[powershell-lint-check/"+psInvokeExpr+"]")
	})
}

// toolsOnlyPath is a directory with links to exactly the tools the hook needs besides pwsh.
func toolsOnlyPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(workDir, "path")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for _, tool := range []string{"bash", "cat", "jq", "git", "dirname", "mktemp", "rm"} {
		bin, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s is not on PATH: %v", tool, err)
		}
		if err := os.Symlink(bin, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The failure branches: none of them passes or poses as a finding. They run with a stub or without
// pwsh, so they need no real one.
func TestPowershellLintCouldNotCheck(t *testing.T) {
	root := installRepo(t, "core", "powershell")
	bad := readPsFixture(t, psInvokeExpr)
	path := os.Getenv("PATH")
	t.Run("no pwsh on PATH", func(t *testing.T) {
		r := runPsLint(t, root, "canary.ps1", bad, "PATH="+toolsOnlyPath(t))
		requireCouldNotCheck(t, r, "pwsh is not on PATH")
	})
	t.Run("module missing", func(t *testing.T) {
		// Emptying PSModulePath does not hide the module (PowerShell 7 adds its own paths back):
		// the stub answers as the helper does without the module.
		dir := stubPwsh(t, "psscriptanalyzer-repo.ps1: the PSScriptAnalyzer module cannot be loaded", "3")
		r := runPsLint(t, root, "canary.ps1", bad, "PATH="+dir+string(os.PathListSeparator)+path)
		requireCouldNotCheck(t, r, "PSScriptAnalyzer module cannot be loaded")
	})
	t.Run("pwsh fails without output", func(t *testing.T) {
		dir := stubPwsh(t, "boom", "1")
		r := runPsLint(t, root, "canary.ps1", bad, "PATH="+dir+string(os.PathListSeparator)+path)
		requireCouldNotCheck(t, r, "rc=1")
	})
	t.Run("rc 10 without a finding", func(t *testing.T) {
		dir := stubPwsh(t, "no lines", "10")
		r := runPsLint(t, root, "canary.ps1", bad, "PATH="+dir+string(os.PathListSeparator)+path)
		requireCouldNotCheck(t, r, "rc=10")
	})
	t.Run("helper missing", func(t *testing.T) {
		if err := os.Remove(filepath.Join(root, ".lighthouse", "scripts", "psscriptanalyzer-repo.ps1")); err != nil {
			t.Fatal(err)
		}
		r := runPsLint(t, root, "canary.ps1", bad)
		requireCouldNotCheck(t, r, "psscriptanalyzer-repo.ps1")
	})
}

// The helper itself: its exit codes are the hook's contract (0 clean, 10 findings, 3 module, 4 rest).
func TestPowershellHelperExitCodes(t *testing.T) {
	requirePwsh(t)
	script, err := filepath.Abs(packFile("powershell/scripts/psscriptanalyzer-repo.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.ps1")
	writeFile(t, clean, psCleanPs1)
	rules := strings.Join(blockingRules(t, psLintFile), ",")
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"clean", []string{"-Path", clean, "-Rules", rules}, 0},
		{"module version missing", []string{"-Path", clean, "-Rules", rules, "-Version", "0.0.1"}, 3},
		{"file missing", []string{"-Path", filepath.Join(dir, "nope.ps1"), "-Rules", rules}, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("pwsh", append([]string{"-NoProfile", "-NonInteractive", "-File", script}, c.args...)...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != c.code || stdout.Len() != 0 {
				t.Fatalf("exit %d, want %d and no stdout\nstdout: %s\nstderr: %s", code, c.code, stdout.String(), stderr.String())
			}
		})
	}
}
