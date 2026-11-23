package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Classification: these tests fail when `lint check` maps a usage error, a missing wiring, an
// empty manifest or an uninstalled --lang to the wrong exit code, prints a line twice, loses
// --dir, or reads the CI files of the wrong forge. They observe the exit code, stdout and stderr
// of the whole command on temp repos.

const (
	lintShHook = ".claude/hooks/lighthouse/shell-lint-check.sh"
	lintShLib  = ".lighthouse/scripts/lib/shellcheck-bin.sh"
	lintShStep = ".lighthouse/scripts/shellcheck-repo.sh"
)

const lintWorkflow = "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + lintShStep + "\n"

const lintGitLab = "lint:\n  script:\n    - " + lintShStep + "\n"

// lintShellFiles is a wired shell repo with the given manifest packages.
func lintShellFiles(packages string) map[string]string {
	return map[string]string{
		".lighthouse/manifest.toml": "version = \"dev\"\npackages = [" + packages + "]\n",
		lintShHook: "#!/bin/sh\nBLOCKING_RULES=\"SC2164,SC2046,SC2115,SC2155,SC2034,strict-style\"\n" +
			"NON_LINTER_RULES=\"strict-style\"\n",
		lintShLib:                  "#!/usr/bin/env bash\nSHELLCHECK_PIN=0.11.0\n",
		".github/workflows/ci.yml": lintWorkflow,
		"x.sh":                     "#!/bin/sh\necho x\n",
	}
}

func runLint(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs(append([]string{"lint"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func TestLintCheckWiredRepoExit0(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"core", "shell"`))
	code, out, errOut := runLint(t, "check")
	if code != 0 || out != "shellcheck: wired\n" || errOut != "" {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestLintCheckMissingExit1WithLines(t *testing.T) {
	files := lintShellFiles(`"shell"`)
	delete(files, ".github/workflows/ci.yml")
	newImportsRepo(t, "", files)
	code, out, errOut := runLint(t, "check")
	if code != 1 || !strings.Contains(out, "shellcheck — ") || strings.Count(out, "\n") != 1 || errOut != "" {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestLintCheckUnknownLangExit3(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"shell"`))
	code, out, errOut := runLint(t, "check", "--lang", "ruby")
	if code != 3 || out != "" || !strings.HasPrefix(errOut, "lint check: COULD NOT CHECK — ") || !strings.Contains(errOut, `unknown --lang "ruby"`) {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestLintCheckLangNotInstalledExit3(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"shell"`))
	code, out, errOut := runLint(t, "check", "--lang", "go")
	if code != 3 || out != "" || !strings.Contains(errOut, "lint check: COULD NOT CHECK — ") || !strings.Contains(errOut, "go") {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	// Without a manifest nothing is installed either.
	newImportsRepo(t, "", map[string]string{"x.sh": "echo\n"})
	if code, _, _ := runLint(t, "check", "--lang", "shell"); code != 3 {
		t.Errorf("no manifest: code=%d", code)
	}
}

func TestLintCheckLangSelectsOne(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"shell", "go"`))
	code, out, _ := runLint(t, "check", "--lang", "shell")
	if code != 0 || out != "shellcheck: wired\n" {
		t.Errorf("code=%d stdout=%q", code, out)
	}
}

func TestLintCheckUnknownSubcommandExit3(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"shell"`))
	for _, args := range [][]string{{}, {"bogus"}} {
		code, out, errOut := runLint(t, args...)
		if code != 3 || out != "" || !strings.Contains(errOut, "lint check: COULD NOT CHECK — unknown subcommand") {
			t.Errorf("lint %q: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

func TestLintCheckUnexpectedArgExit3(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"shell"`))
	code, out, errOut := runLint(t, "check", "extra")
	if code != 3 || out != "" || !strings.Contains(errOut, `unexpected argument "extra"`) {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestLintCheckFlagErrorExit3(t *testing.T) {
	newImportsRepo(t, "", lintShellFiles(`"shell"`))
	for _, args := range [][]string{{"check", "--bogus"}, {"check", "--lang"}, {"--bogus"}} {
		code, out, errOut := runLint(t, args...)
		if code != 3 || out != "" || !strings.HasPrefix(errOut, "lint check: COULD NOT CHECK — ") {
			t.Errorf("%q: code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

func TestLintCheckEmptyManifestExit0Silent(t *testing.T) {
	newImportsRepo(t, "", map[string]string{".lighthouse/manifest.toml": "version = \"dev\"\npackages = [\"core\"]\n"})
	code, out, errOut := runLint(t, "check")
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestLintCheckDirFlagNodeRoot(t *testing.T) {
	newImportsRepo(t, "", map[string]string{
		".lighthouse/manifest.toml": "version = \"dev\"\npackages = [\"node\"]\n",
		"web/package.json":          `{"name":"web","scripts":{"lint":"eslint ."},"devDependencies":{"eslint":"10.13.0"}}` + "\n",
		"web/eslint.config.js":      "import js from '@eslint/js'\nexport default [{ extends: [js.configs.recommended] }]\n",
		"web/src/app.js":            "export const x = 1\n",
		"other/package.json":        `{"name":"other"}` + "\n",
		".github/workflows/ci.yml": "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        working-directory: web\n" +
			"    steps:\n      - run: npm ci\n      - run: npm run lint\n",
	})
	if code, out, _ := runLint(t, "check"); code != 1 || !strings.Contains(out, "other/") {
		t.Errorf("without --dir: code=%d stdout=%q", code, out)
	}
	code, out, errOut := runLint(t, "check", "--dir", "web")
	if code != 0 || out != "eslint: wired\n" || errOut != "" {
		t.Errorf("--dir web: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestLintCheckForgeKindFromConfig(t *testing.T) {
	files := lintShellFiles(`"shell"`)
	delete(files, ".github/workflows/ci.yml")
	files[".gitlab-ci.yml"] = lintGitLab
	newImportsRepo(t, "[forge]\nkind = \"gitlab\"\n", files)
	if code, out, errOut := runLint(t, "check"); code != 0 || out != "shellcheck: wired\n" || errOut != "" {
		t.Errorf("gitlab: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	// Without forge.kind the repo is a GitHub one and has no workflow.
	newImportsRepo(t, "", files)
	if code, _, _ := runLint(t, "check"); code != 1 {
		t.Errorf("github default: code=%d", code)
	}
}
