package lintcheck

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	shHook = hookDir + "shell-lint-check.sh"
	shLib  = ".lighthouse/scripts/lib/shellcheck-bin.sh"
)

// shRepo is a wired shell repo; over replaces a file, or removes it with pyDelete.
func shRepo(t *testing.T, over map[string]string) string {
	t.Helper()
	files := map[string]string{
		shHook: "#!/bin/sh\nBLOCKING_RULES=\"SC2164,SC2046,SC2115,SC2155,SC2034,strict-style\"\n" +
			"NON_LINTER_RULES=\"strict-style\"\n",
		shLib:         "#!/usr/bin/env bash\n# Pinned.\nSHELLCHECK_PIN=0.11.0\n",
		wf + "ci.yml": pyWorkflow(".lighthouse/scripts/shellcheck-repo.sh"),
		"x.sh":        "#!/bin/sh\necho x\n",
	}
	for k, v := range over {
		if v == pyDelete {
			delete(files, k)
			continue
		}
		files[k] = v
	}
	return writeRepo(t, files)
}

func checkSh(t *testing.T, root string) ([]string, []string, error) {
	t.Helper()
	findings, wired, err := ShellChecker{}.Check(".", Env{Root: root})
	var lines []string
	for _, f := range findings {
		lines = append(lines, FormatFinding(f))
	}
	return lines, wired, err
}

// wantSh asserts the exact findings, and that shellcheck is wired only without findings.
func wantSh(t *testing.T, over map[string]string, want ...string) {
	t.Helper()
	got, wired, err := checkSh(t, shRepo(t, over))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got %q\nwant %q", got, want)
	}
	if len(want) == 0 && !reflect.DeepEqual(wired, []string{"shellcheck"}) {
		t.Fatalf("want shellcheck wired, got %v", wired)
	}
	if len(want) > 0 && len(wired) > 0 {
		t.Fatalf("wired with findings: %v", wired)
	}
}

func wantShUnchecked(t *testing.T, over map[string]string) {
	t.Helper()
	_, _, err := checkSh(t, shRepo(t, over))
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Fatalf("want UncheckedError, got %v", err)
	}
}

const curlShellcheck = "curl -sSL https://github.com/koalaman/shellcheck/releases/download/v%s/shellcheck-v%s.linux.x86_64.tar.xz | tar xJ"

func shellcheckDownload(v string) string { return strings.ReplaceAll(curlShellcheck, "%s", v) }

func TestShellNoShellcheckrcIsFine(t *testing.T) {
	wantSh(t, nil)
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow("bash .lighthouse/scripts/shellcheck-repo.sh")})
}

func TestShellrcWithoutFloorDisable(t *testing.T) {
	wantSh(t, map[string]string{".shellcheckrc": "# repo settings\nexternal-sources=true\ndisable=SC1091,SC2059 # sourced files\nenable=require-variable-braces\n"})
}

func TestShellrcDisablesFloorCode(t *testing.T) {
	wantSh(t, map[string]string{".shellcheckrc": "source-path=SCRIPTDIR\ndisable=SC1091,SC2164\ndisable = 2034\n"},
		".shellcheckrc:2: shellcheck — floor rule SC2164 disabled",
		".shellcheckrc:3: shellcheck — floor rule SC2034 disabled")
	// shellcheck's other rc name at the root counts too
	wantSh(t, map[string]string{"shellcheckrc": "disable=SC2046\n"},
		"shellcheckrc:1: shellcheck — floor rule SC2046 disabled")
	// a code disabled twice is reported once, at its first disable
	wantSh(t, map[string]string{".shellcheckrc": "disable=SC2164,2164\ndisable=SC2164\n"},
		".shellcheckrc:1: shellcheck — floor rule SC2164 disabled")
	// shellcheck reads the first rc file of a directory only
	wantSh(t, map[string]string{".shellcheckrc": "disable=SC1091\n", "shellcheckrc": "disable=SC2046\n"})
	// a commented line disables nothing
	wantSh(t, map[string]string{".shellcheckrc": "# disable=SC2164\n"})
}

func TestShellrcDisableAll(t *testing.T) {
	wantSh(t, map[string]string{".shellcheckrc": "disable=all\n"},
		".shellcheckrc:1: shellcheck — floor rule SC2164 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2046 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2115 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2155 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2034 disabled")
}

func TestShellrcDisableRange(t *testing.T) {
	wantSh(t, map[string]string{".shellcheckrc": "disable=SC2100-SC2120\n"},
		".shellcheckrc:1: shellcheck — floor rule SC2115 disabled")
	wantSh(t, map[string]string{".shellcheckrc": "disable=SC2000-SC2999\n"},
		".shellcheckrc:1: shellcheck — floor rule SC2164 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2046 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2115 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2155 disabled",
		".shellcheckrc:1: shellcheck — floor rule SC2034 disabled")
	// a range next to the floor disables none of it
	wantSh(t, map[string]string{".shellcheckrc": "disable=SC1000-SC1999,SC2165-SC2200\n"})
}

func TestShellrcUnreadableValueIsUnchecked(t *testing.T) {
	wantShUnchecked(t, map[string]string{".shellcheckrc": "disable=SC2200-SC2100\n"})
	wantShUnchecked(t, map[string]string{".shellcheckrc": "disable=SCxyz\n"})
}

func TestShellOptsEnvNarrows(t *testing.T) {
	head := "on: [push]\n%wjobs:\n  lint:\n    runs-on: ubuntu-latest\n%j    steps:\n      - run: .lighthouse/scripts/shellcheck-repo.sh\n%s"
	opts := "  SHELLCHECK_OPTS: -e SC2164\n"
	for name, body := range map[string]string{
		"workflow": strings.NewReplacer("%w", "env:\n"+opts, "%j", "", "%s", "").Replace(head),
		"job":      strings.NewReplacer("%w", "", "%j", "    env:\n    "+opts, "%s", "").Replace(head),
		"step":     strings.NewReplacer("%w", "", "%j", "", "%s", "        env:\n        "+opts).Replace(head),
	} {
		got, _, err := checkSh(t, shRepo(t, map[string]string{wf + "ci.yml": body}))
		if err != nil || len(got) != 1 || !strings.Contains(got[0], "shellcheck — CI flag -e turns off floor rule SC2164 (SHELLCHECK_OPTS)") {
			t.Fatalf("%s: got %q, %v", name, got, err)
		}
	}
	// an option that leaves the floor alone keeps it wired
	wantSh(t, map[string]string{wf + "ci.yml": "on: [push]\nenv:\n  SHELLCHECK_OPTS: -e SC1091\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: .lighthouse/scripts/shellcheck-repo.sh\n"})
}

func TestShellNoCIStep(t *testing.T) {
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow("make lint")},
		".github/workflows: shellcheck — no CI step runs shellcheck")
	wantSh(t, map[string]string{wf + "ci.yml": pyDelete},
		".github/workflows: shellcheck — no CI step runs shellcheck")
}

func TestShellPinFromInstalledLib(t *testing.T) {
	// the pin comes from the installed lib: a download of that version is wired
	wantSh(t, map[string]string{
		shLib:         "SHELLCHECK_PIN=0.10.0\n",
		wf + "ci.yml": pyWorkflow(shellcheckDownload("0.10.0"), "shellcheck x.sh"),
	})
	wantShUnchecked(t, map[string]string{shLib: pyDelete})
	wantShUnchecked(t, map[string]string{shLib: "#!/usr/bin/env bash\necho no pin\n"})
}

func TestShellCIInstallVersionMismatch(t *testing.T) {
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow(shellcheckDownload("0.10.0"), "shellcheck x.sh")},
		".github/workflows/ci.yml:6: shellcheck — CI version 0.10.0, pin 0.11.0")
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow(shellcheckDownload("0.11.0"), "shellcheck x.sh")})
	// an apt install on a version
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow("sudo apt-get install -y shellcheck=0.9.0-1", "shellcheck x.sh")},
		".github/workflows/ci.yml:6: shellcheck — CI version 0.9.0, pin 0.11.0")
	// the nearest install wins, and a version in echo is no install
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow(shellcheckDownload("0.10.0"), shellcheckDownload("0.11.0")+" && echo shellcheck-v0.9.0", "shellcheck x.sh")})
	// a step that never runs installs nothing
	wantSh(t, map[string]string{wf + "ci.yml": "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: " + shellcheckDownload("0.11.0") + "\n      - if: false\n        run: " + shellcheckDownload("0.9.0") + "\n      - run: shellcheck x.sh\n"})
}

func TestShellUnpinnedAptInstall(t *testing.T) {
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow("sudo apt-get install -y shellcheck", "shellcheck x.sh")},
		".github/workflows/ci.yml:7: shellcheck — CI version not pinned")
}

func TestShellFloorExcludesStrictStyle(t *testing.T) {
	// strict-style is in BLOCKING_RULES but also in NON_LINTER_RULES: an include of the SC codes
	// keeps the floor
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow(shellcheckDownload("0.11.0"),
		"shellcheck -i SC2164,SC2046,SC2115,SC2155,SC2034 x.sh")})
	wantSh(t, map[string]string{".shellcheckrc": "disable=all\n",
		shHook: "BLOCKING_RULES=\"strict-style\"\nNON_LINTER_RULES=\"strict-style\"\n"})
	wantShUnchecked(t, map[string]string{shHook: pyDelete})
}

func TestShellRcfileFlag(t *testing.T) {
	wantSh(t, map[string]string{
		"ci/lax.rc":   "disable=SC2155\n",
		wf + "ci.yml": pyWorkflow(".lighthouse/scripts/shellcheck-repo.sh --rcfile ci/lax.rc"),
	}, ".github/workflows/ci.yml:6: shellcheck — CI rc file ci/lax.rc turns off the floor")
	// a strict file is fine, resolved from the working directory of a cd
	wantSh(t, map[string]string{
		"ci/strict.rc": "disable=SC1091\n",
		wf + "ci.yml":  pyWorkflow("cd ci && ../.lighthouse/scripts/shellcheck-repo.sh --rcfile=strict.rc"),
	})
	// a missing file
	wantSh(t, map[string]string{wf + "ci.yml": pyWorkflow(".lighthouse/scripts/shellcheck-repo.sh --rcfile gone.rc")},
		".github/workflows/ci.yml:6: shellcheck — CI rc file gone.rc turns off the floor")
}

func TestShellInstallOtherJob(t *testing.T) {
	body := "on: [push]\njobs:\n  setup:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + shellcheckDownload("0.11.0") +
		"\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: shellcheck x.sh\n"
	wantSh(t, map[string]string{wf + "ci.yml": body}, ".github/workflows/ci.yml:10: shellcheck — CI version not pinned")
}

func TestShellAllFindingsTogether(t *testing.T) {
	wantSh(t, map[string]string{".shellcheckrc": "disable=SC2034\n", wf + "ci.yml": pyDelete},
		".shellcheckrc:1: shellcheck — floor rule SC2034 disabled",
		".github/workflows: shellcheck — no CI step runs shellcheck")
}

// Every floor code of the shipped hook has a severity, so --severity judges it by its own level.
func TestShellcheckFloorSeverityCoversHook(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "pack", "shell", "hooks", "shell-lint-check.sh"))
	if err != nil {
		t.Fatal(err)
	}
	floor, err := Floor(writeRepo(t, map[string]string{shHook: string(data)}), "shell-lint-check.sh")
	if err != nil || len(floor) == 0 {
		t.Fatalf("floor %v, %v", floor, err)
	}
	for _, code := range floor {
		if _, ok := shellcheckFloorSeverity[code]; !ok {
			t.Errorf("floor code %s has no severity", code)
		}
	}
}
