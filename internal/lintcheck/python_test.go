package lintcheck

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const ruffHook = hookDir + "ruff-format-check.sh"

// pyDelete removes a default file from pyRepo.
const pyDelete = "\x00"

// pyWorkflow is a push workflow with one job "lint" whose steps run the given lines; step i sits on
// line 6+i.
func pyWorkflow(runs ...string) string {
	b := "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n"
	for _, r := range runs {
		b += "      - run: " + r + "\n"
	}
	return b
}

// pyRepo is a wired Python repo; over replaces a file, or removes it with pyDelete.
func pyRepo(t *testing.T, over map[string]string) string {
	t.Helper()
	files := map[string]string{
		ruffHook:                 "#!/bin/sh\nBLOCKING_RULES=\"E722,B006,F403,F811,B023,PT012,PGH003,PGH004\"\n",
		".ruff-version":          "0.9.0\n",
		"ruff.toml":              "[lint]\nselect = [\"B\", \"E722\", \"F\", \"PT\", \"PGH\"]\n",
		wf + "ci.yml":            pyWorkflow("uvx ruff@0.9.0 check"),
		"src/app.py":             "x = 1\n",
		".github/dependabot.yml": "version: 2\n",
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

func checkPy(t *testing.T, root string) ([]string, []string, error) {
	t.Helper()
	findings, wired, err := PythonChecker{}.Check(".", Env{Root: root})
	var lines []string
	for _, f := range findings {
		lines = append(lines, FormatFinding(f))
	}
	return lines, wired, err
}

// wantPy asserts the exact findings, and that ruff is wired only without findings.
func wantPy(t *testing.T, over map[string]string, want ...string) {
	t.Helper()
	got, wired, err := checkPy(t, pyRepo(t, over))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got %q\nwant %q", got, want)
	}
	if len(want) == 0 && !reflect.DeepEqual(wired, []string{"ruff"}) {
		t.Fatalf("want ruff wired, got %v", wired)
	}
	if len(want) > 0 && len(wired) > 0 {
		t.Fatalf("wired with findings: %v", wired)
	}
}

func wantPyUnchecked(t *testing.T, over map[string]string) {
	t.Helper()
	_, _, err := checkPy(t, pyRepo(t, over))
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Fatalf("want UncheckedError, got %v", err)
	}
}

func TestPythonWired(t *testing.T) {
	wantPy(t, nil)
}

func TestPythonNoCIStep(t *testing.T) {
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("make lint")},
		".github/workflows: ruff — no CI step runs ruff check")
	wantPy(t, map[string]string{wf + "ci.yml": pyDelete},
		".github/workflows: ruff — no CI step runs ruff check")
}

func TestPythonFloorCodeInIgnore(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"ALL\"]\nignore = [\"B006\"]\n"},
		"ruff.toml: ruff — floor rule B006 disabled")
}

func TestPythonFloorCodeInExtendIgnore(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"ALL\"]\nextend-ignore = [\"PGH\"]\n"},
		"ruff.toml: ruff — floor rule PGH003 disabled",
		"ruff.toml: ruff — floor rule PGH004 disabled")
}

func TestPythonFloorCodeNotSelected(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"B\", \"F\", \"PT\", \"PGH\"]\n"},
		"ruff.toml: ruff — floor rule E722 disabled")
}

func TestPythonMoreSpecificSelectWins(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"B\", \"E722\", \"F\", \"PGH\", \"PT012\"]\nignore = [\"PT\"]\n"})
	// the reverse: a more specific ignore beats a broader select
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"ALL\"]\nignore = [\"PT01\"]\n"},
		"ruff.toml: ruff — floor rule PT012 disabled")
	// the same specificity: ignore wins, as in ruff
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"ALL\", \"PT012\"]\nignore = [\"PT012\"]\n"},
		"ruff.toml: ruff — floor rule PT012 disabled")
}

func TestPythonSelectAll(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"ALL\"]\nignore = [\"D\", \"ANN\"]\n"})
	// ALL is the least specific selector: a linter prefix beats an ignored ALL
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"B\", \"E722\", \"F\", \"PT\", \"PGH\"]\nignore = [\"ALL\"]\n"})
}

func TestPythonExtendSelect(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"B\", \"F\"]\nextend-select = [\"E722\", \"PT\", \"PGH\"]\n"})
	// ruff's default selection covers F, but a default does not count
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nextend-select = [\"B\", \"E722\", \"PT\", \"PGH\"]\n"},
		"ruff.toml: ruff — floor rule F403 disabled",
		"ruff.toml: ruff — floor rule F811 disabled")
}

func TestPythonTopLevelSelect(t *testing.T) {
	// the deprecated top-level form still selects; [lint] wins over it
	wantPy(t, map[string]string{"ruff.toml": "select = [\"ALL\"]\n"})
	wantPy(t, map[string]string{"ruff.toml": "select = [\"ALL\"]\n[lint]\nselect = [\"B\"]\n"},
		"ruff.toml: ruff — floor rule E722 disabled",
		"ruff.toml: ruff — floor rule F403 disabled",
		"ruff.toml: ruff — floor rule F811 disabled",
		"ruff.toml: ruff — floor rule PT012 disabled",
		"ruff.toml: ruff — floor rule PGH003 disabled",
		"ruff.toml: ruff — floor rule PGH004 disabled")
}

func TestPythonPerFileIgnoresAreScope(t *testing.T) {
	// a per-file ignore is path scope, which does not count
	wantPy(t, map[string]string{"ruff.toml": "[lint]\nselect = [\"ALL\"]\n[lint.per-file-ignores]\n\"tests/*\" = [\"PT012\"]\n[lint.extend-per-file-ignores]\n\"**\" = [\"B006\"]\n"})
}

func TestPythonPyprojectToolRuffLint(t *testing.T) {
	wantPy(t, map[string]string{
		"ruff.toml":      pyDelete,
		"pyproject.toml": "[project]\nname = \"x\"\n[tool.ruff.lint]\nselect = [\"ALL\"]\n",
	})
	wantPy(t, map[string]string{
		"ruff.toml":      pyDelete,
		"pyproject.toml": "[project]\nname = \"x\"\n[tool.ruff.lint]\nselect = [\"ALL\"]\nignore = [\"E722\"]\n",
	}, "pyproject.toml: ruff — floor rule E722 disabled")
	// a pyproject.toml without [tool.ruff] is no ruff config
	wantPy(t, map[string]string{"ruff.toml": pyDelete, "pyproject.toml": "[project]\nname = \"x\"\n"},
		"ruff.toml: ruff — config missing")
}

func TestPythonDotRuffTomlWinsOverRuffToml(t *testing.T) {
	wantPy(t, map[string]string{".ruff.toml": "[lint]\nselect = [\"B\"]\n"},
		".ruff.toml: ruff — floor rule E722 disabled",
		".ruff.toml: ruff — floor rule F403 disabled",
		".ruff.toml: ruff — floor rule F811 disabled",
		".ruff.toml: ruff — floor rule PT012 disabled",
		".ruff.toml: ruff — floor rule PGH003 disabled",
		".ruff.toml: ruff — floor rule PGH004 disabled")
}

func TestPythonExtendFollowed(t *testing.T) {
	// the base selects; the child only ignores what is not floor
	wantPy(t, map[string]string{
		"ruff.toml":        "extend = \"config/base.toml\"\n[lint]\nignore = [\"D\"]\n",
		"config/base.toml": "[lint]\nselect = [\"ALL\"]\n",
	})
	// the child ignores a floor code the base selects
	wantPy(t, map[string]string{
		"ruff.toml":        "extend = \"config/base.toml\"\n[lint]\nignore = [\"B006\"]\n",
		"config/base.toml": "[lint]\nselect = [\"ALL\"]\n",
	}, "ruff.toml: ruff — floor rule B006 disabled")
	// a child select replaces the base selection
	wantPy(t, map[string]string{
		"ruff.toml":        "extend = \"config/base.toml\"\n[lint]\nselect = [\"B\", \"E722\", \"F\", \"PT\"]\n",
		"config/base.toml": "[lint]\nselect = [\"ALL\"]\n",
	}, "ruff.toml: ruff — floor rule PGH003 disabled", "ruff.toml: ruff — floor rule PGH004 disabled")
	// a relative extend inside the base resolves from the base's directory, into a pyproject
	wantPy(t, map[string]string{
		"ruff.toml":             "extend = \"config/base.toml\"\n",
		"config/base.toml":      "extend = \"../shared/pyproject.toml\"\n",
		"shared/pyproject.toml": "[tool.ruff]\nselect = [\"ALL\"]\n",
	})
}

func TestPythonExtendCycleTerminates(t *testing.T) {
	wantPyUnchecked(t, map[string]string{
		"ruff.toml": "extend = \"a.toml\"\n",
		"a.toml":    "extend = \"ruff.toml\"\n[lint]\nselect = [\"ALL\"]\n",
	})
	wantPyUnchecked(t, map[string]string{"ruff.toml": "extend = \"ruff.toml\"\n"})
}

func TestPythonExtendMissingIsUnchecked(t *testing.T) {
	wantPyUnchecked(t, map[string]string{"ruff.toml": "extend = \"nowhere.toml\"\n"})
}

func TestPythonInvalidTomlIsUnchecked(t *testing.T) {
	wantPyUnchecked(t, map[string]string{"ruff.toml": "[lint\nselect = \n"})
}

func TestPythonPinMissing(t *testing.T) {
	wantPy(t, map[string]string{".ruff-version": pyDelete}, ".ruff-version: ruff — pin missing")
	wantPy(t, map[string]string{".ruff-version": "  \n"}, ".ruff-version: ruff — pin missing")
}

func TestPythonCIVersionMismatch(t *testing.T) {
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("echo start", "uvx ruff@0.8.0 check")},
		".github/workflows/ci.yml:7: ruff — CI version 0.8.0, pin 0.9.0")
	// the install in an earlier step of the job gives the version
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("pip install ruff==0.8.0", "ruff check src/")},
		".github/workflows/ci.yml:6: ruff — CI version 0.8.0, pin 0.9.0")
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("pip install ruff==0.9.0", "ruff check src/")})
	// a version in echo is no install
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("pip install ruff==0.9.0 && echo ruff==0.8.0", "ruff check")})
}

func TestPythonCIVersionFromAction(t *testing.T) {
	act := "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: astral-sh/ruff-action@v3\n        with:\n          version: %s\n"
	wantPy(t, map[string]string{wf + "ci.yml": strings.Replace(act, "%s", "0.9.0", 1)})
	wantPy(t, map[string]string{wf + "ci.yml": strings.Replace(act, "%s", "0.8.0", 1)},
		".github/workflows/ci.yml:6: ruff — CI version 0.8.0, pin 0.9.0")
}

func TestPythonActionWithoutVersionIsNotPinned(t *testing.T) {
	base := "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: astral-sh/ruff-action@v3\n"
	for _, with := range []string{"", "        with:\n          version: \"\"\n", "        with:\n          version: latest\n"} {
		wantPy(t, map[string]string{wf + "ci.yml": base + with}, ".github/workflows/ci.yml:6: ruff — CI version not pinned")
	}
	// the version step itself, should a call without a version input ever be judged valid
	for _, v := range []string{"", "latest", " "} {
		c := Call{Step: Step{File: wf + "ci.yml", Job: "lint", Line: 6, Uses: "astral-sh/ruff-action@v3", With: map[string]string{"version": v}}, Linter: "ruff", Verdict: Valid}
		f, ok := ruffVersionFinding([]Step{c.Step}, []Call{c}, "0.9.0")
		if ok || FormatFinding(f) != ".github/workflows/ci.yml:6: ruff — CI version not pinned" {
			t.Fatalf("version %q: got %q, %v", v, FormatFinding(f), ok)
		}
	}
}

func TestPythonCIVersionFromEnv(t *testing.T) {
	body := "on: [push]\nenv:\n  RUFF_VERSION: %s\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: pip install \"ruff==${RUFF_VERSION}\"\n      - run: ruff check\n"
	wantPy(t, map[string]string{wf + "ci.yml": strings.Replace(body, "%s", "0.9.0", 1)})
	wantPy(t, map[string]string{wf + "ci.yml": strings.Replace(body, "%s", "0.8.0", 1)},
		".github/workflows/ci.yml:8: ruff — CI version 0.8.0, pin 0.9.0")
}

func TestPythonPinWithVPrefixMatches(t *testing.T) {
	wantPy(t, map[string]string{".ruff-version": "v0.9.0\n"})
}

func TestPythonUnpinnedCIInstall(t *testing.T) {
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("pip install ruff", "ruff check")},
		".github/workflows/ci.yml:7: ruff — CI version not pinned")
}

func TestPythonVersionFromCat(t *testing.T) {
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow(`uvx ruff@"$(cat .ruff-version)" check`)})
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow(`pip install "ruff==$(cat .ruff-version)"`, "ruff check")})
}

func TestPythonConfigMissing(t *testing.T) {
	wantPy(t, map[string]string{"ruff.toml": pyDelete}, "ruff.toml: ruff — config missing")
}

func TestPythonFloorFromHook(t *testing.T) {
	// the default config covers the eight codes; a hook with an extra code makes it fail
	wantPy(t, map[string]string{ruffHook: "BLOCKING_RULES=\"E722,B006,F403,F811,B023,PT012,PGH003,PGH004,S101\"\n"},
		"ruff.toml: ruff — floor rule S101 disabled")
	wantPyUnchecked(t, map[string]string{ruffHook: pyDelete})
	wantPyUnchecked(t, map[string]string{ruffHook: "#!/bin/sh\necho no rules\n"})
}

func TestPythonInvalidWorkflowIsUnchecked(t *testing.T) {
	wantPyUnchecked(t, map[string]string{wf + "ci.yml": "on: [push\njobs: {\n"})
}

func TestPythonConfigFlagLaxFile(t *testing.T) {
	wantPy(t, map[string]string{
		"ci-lax.toml": "[lint]\nselect = [\"ALL\"]\nignore = [\"B006\"]\n",
		wf + "ci.yml": pyWorkflow("uvx ruff@0.9.0 check --config ci-lax.toml"),
	}, ".github/workflows/ci.yml:6: ruff — CI config ci-lax.toml misses the floor")
	// a strict file is fine, also resolved from the working directory of a cd
	wantPy(t, map[string]string{
		"ci/strict.toml": "extend = \"../ruff.toml\"\n",
		wf + "ci.yml":    pyWorkflow("cd ci && uvx ruff@0.9.0 check --config strict.toml"),
	})
	// a missing file misses the floor
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("uvx ruff@0.9.0 check --config gone.toml")},
		".github/workflows/ci.yml:6: ruff — CI config gone.toml misses the floor")
}

func TestPythonInstallOtherJob(t *testing.T) {
	body := "on: [push]\njobs:\n  setup:\n    runs-on: ubuntu-latest\n    steps:\n      - run: pip install ruff==0.9.0\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ruff check\n"
	wantPy(t, map[string]string{wf + "ci.yml": body}, ".github/workflows/ci.yml:10: ruff — CI version not pinned")
}

func TestPythonInstallAfterCall(t *testing.T) {
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("ruff check", "pip install ruff==0.9.0")},
		".github/workflows/ci.yml:6: ruff — CI version not pinned")
}

func TestPythonCIVersionOnlyBeforeTheCall(t *testing.T) {
	// the install after the call in the same step does not set the version the call runs
	wantPy(t, map[string]string{wf + "ci.yml": pyWorkflow("pip install ruff==0.8.0 && ruff check && pip install ruff==0.9.0")},
		".github/workflows/ci.yml:6: ruff — CI version 0.8.0, pin 0.9.0")
}

func TestPythonGitLab(t *testing.T) {
	root := pyRepo(t, map[string]string{
		wf + "ci.yml":    pyDelete,
		".gitlab-ci.yml": "lint:\n  script:\n    - pip install ruff==0.9.0\n    - ruff check\n",
	})
	findings, wired, err := PythonChecker{}.Check(".", Env{Root: root, ForgeKind: "gitlab"})
	if err != nil || len(findings) != 0 || !reflect.DeepEqual(wired, []string{"ruff"}) {
		t.Fatalf("got %v %v %v", findings, wired, err)
	}
	root = pyRepo(t, map[string]string{wf + "ci.yml": pyDelete, ".gitlab-ci.yml": "build:\n  script:\n    - make\n"})
	findings, _, err = PythonChecker{}.Check(".", Env{Root: root, ForgeKind: "gitlab"})
	if err != nil || len(findings) != 1 || FormatFinding(findings[0]) != ".gitlab-ci.yml: ruff — no CI step runs ruff check" {
		t.Fatalf("got %v %v", findings, err)
	}
}

func TestPythonAllFindingsTogether(t *testing.T) {
	wantPy(t, map[string]string{".ruff-version": pyDelete, "ruff.toml": pyDelete, wf + "ci.yml": pyDelete},
		".ruff-version: ruff — pin missing",
		"ruff.toml: ruff — config missing",
		".github/workflows: ruff — no CI step runs ruff check")
}

func TestRuffEffective(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"ruff.toml": "[lint]\nselect = [\"PT012\"]\nignore = [\"PT\"]\n",
	})
	for code, want := range map[string]bool{"PT012": true, "PT011": false, "B006": false} {
		got, err := ruffEffective([]string{".ruff.toml", "ruff.toml"}, root, code)
		if err != nil || got != want {
			t.Fatalf("%s: got %v, %v; want %v", code, got, err, want)
		}
	}
	got, err := ruffEffective([]string{"none.toml"}, root, "B006")
	if err != nil || got {
		t.Fatalf("no config: got %v, %v", got, err)
	}
}
