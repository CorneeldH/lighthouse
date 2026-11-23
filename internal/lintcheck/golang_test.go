package lintcheck

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const goHook = hookDir + "go-lint-gate.sh"

// goActionWorkflow is a push workflow whose job "lint" runs the golangci-lint action on version;
// the action sits on line 6.
func goActionWorkflow(version string) string {
	return "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: golangci/golangci-lint-action@v9\n        with:\n          version: " + version + "\n"
}

const goStandardCfg = "version: \"2\"\nlinters:\n  default: standard\n"

// goRepo is a wired Go repo; over replaces a file, or removes it with pyDelete.
func goRepo(t *testing.T, over map[string]string) string {
	t.Helper()
	files := map[string]string{
		goHook: "#!/bin/sh\nBLOCKING_RULES=\"errcheck,govet,ineffassign,staticcheck,unused,typecheck\"\n" +
			"NON_LINTER_RULES=\"typecheck\"\n",
		".golangci-version": "v2.15.0\n",
		".golangci.yml":     goStandardCfg,
		wf + "ci.yml":       goActionWorkflow("v2.15.0"),
		"go.mod":            "module example.com/x\n",
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

func checkGo(t *testing.T, root string) ([]string, []string, error) {
	t.Helper()
	findings, wired, err := GoChecker{}.Check(".", Env{Root: root})
	var lines []string
	for _, f := range findings {
		lines = append(lines, FormatFinding(f))
	}
	return lines, wired, err
}

// wantGo asserts the exact findings, and that golangci-lint is wired only without findings.
func wantGo(t *testing.T, over map[string]string, want ...string) {
	t.Helper()
	got, wired, err := checkGo(t, goRepo(t, over))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got %q\nwant %q", got, want)
	}
	if len(want) == 0 && !reflect.DeepEqual(wired, []string{"golangci-lint"}) {
		t.Fatalf("want golangci-lint wired, got %v", wired)
	}
	if len(want) > 0 && len(wired) > 0 {
		t.Fatalf("wired with findings: %v", wired)
	}
}

func wantGoUnchecked(t *testing.T, over map[string]string) {
	t.Helper()
	_, _, err := checkGo(t, goRepo(t, over))
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Fatalf("want UncheckedError, got %v", err)
	}
}

func TestGoWired(t *testing.T) {
	wantGo(t, nil)
	// a version given as a YAML number is version 2 too
	wantGo(t, map[string]string{".golangci.yml": "version: 2\nlinters:\n  default: all\n  disable: [lll, wsl]\n"})
}

func TestGoNoCIStep(t *testing.T) {
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow("make lint")},
		".github/workflows: golangci-lint — no CI step runs golangci-lint")
	wantGo(t, map[string]string{wf + "ci.yml": pyDelete},
		".github/workflows: golangci-lint — no CI step runs golangci-lint")
}

func TestGoCIVersionMismatch(t *testing.T) {
	wantGo(t, map[string]string{wf + "ci.yml": goActionWorkflow("v2.14.0")},
		".github/workflows/ci.yml:6: golangci-lint — CI version v2.14.0, pin v2.15.0")
	// a go install in an earlier step of the job gives the version
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0", "golangci-lint run ./...")},
		".github/workflows/ci.yml:6: golangci-lint — CI version v2.14.0, pin v2.15.0")
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0", "golangci-lint run ./...")})
	// the install script takes the version as its last argument
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b /usr/local/bin v2.14.0",
		"golangci-lint run")},
		".github/workflows/ci.yml:6: golangci-lint — CI version v2.14.0, pin v2.15.0")
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b /usr/local/bin v2.15.0",
		"golangci-lint run")})
	// a version in echo is no install
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 && echo golangci-lint@v2.14.0",
		"golangci-lint run")})
	// a step that never runs installs nothing
	wantGo(t, map[string]string{wf + "ci.yml": "on: [push]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0\n" +
		"      - if: false\n        run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0\n" +
		"      - run: golangci-lint run\n"})
	// the pin without its v prefix matches
	wantGo(t, map[string]string{".golangci-version": "2.15.0\n"})
}

func TestGoCIVersionFromPinFileAndEnv(t *testing.T) {
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		`go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(cat .golangci-version)"`, "golangci-lint run")})
	body := "on: [push]\nenv:\n  GOLANGCI_VERSION: %s\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: go install \"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}\"\n      - run: golangci-lint run\n"
	wantGo(t, map[string]string{wf + "ci.yml": strings.Replace(body, "%s", "v2.15.0", 1)})
	wantGo(t, map[string]string{wf + "ci.yml": strings.Replace(body, "%s", "v2.14.0", 1)},
		".github/workflows/ci.yml:8: golangci-lint — CI version v2.14.0, pin v2.15.0")
}

func TestGoActionWithoutVersion(t *testing.T) {
	wantGo(t, map[string]string{wf + "ci.yml": goActionWorkflow("latest")},
		".github/workflows/ci.yml:6: golangci-lint — CI version not pinned")
}

func TestGoPinMissing(t *testing.T) {
	wantGo(t, map[string]string{".golangci-version": pyDelete}, ".golangci-version: golangci-lint — pin missing")
}

func TestGoConfigWithoutDefault(t *testing.T) {
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\n"},
		".golangci.yml: golangci-lint — linters.default missing")
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  enable: [errcheck, govet]\n"},
		".golangci.yml: golangci-lint — linters.default missing")
	// without a default, an enable list of the whole floor makes it explicit
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  enable: [errcheck, govet, ineffassign, staticcheck, unused]\n"})
}

func TestGoConfigMissingWithOtherwiseCompleteSetup(t *testing.T) {
	root := goRepo(t, map[string]string{".golangci.yml": pyDelete})
	got, _, err := checkGo(t, root)
	want := []string{".golangci.yml: golangci-lint — config missing"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v", got, err)
	}
	if code := Run(Options{Root: root, Langs: []Lang{"go"}, Checkers: map[Lang]Checker{"go": GoChecker{}}}, Env{}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestGoV1ConfigNotVersion2(t *testing.T) {
	wantGo(t, map[string]string{".golangci.yml": "linters:\n  enable-all: true\n"},
		".golangci.yml: golangci-lint — config not version 2")
	wantGo(t, map[string]string{".golangci.yml": "version: \"1\"\nlinters:\n  default: standard\n"},
		".golangci.yml: golangci-lint — config not version 2")
}

func TestGoFloorLinterInDisable(t *testing.T) {
	wantGo(t, map[string]string{".golangci.yml": goStandardCfg + "  disable: [errcheck, unused, lll]\n"},
		".golangci.yml: golangci-lint — floor rule errcheck disabled",
		".golangci.yml: golangci-lint — floor rule unused disabled")
	// disable wins over enable
	wantGo(t, map[string]string{".golangci.yml": goStandardCfg + "  enable: [govet]\n  disable: [govet]\n"},
		".golangci.yml: golangci-lint — floor rule govet disabled")
}

func TestGoDefaultAllAndEnable(t *testing.T) {
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  default: all\n"})
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  default: none\n  enable: [errcheck, govet, ineffassign, staticcheck, unused]\n"})
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  default: none\n  enable: [errcheck, govet]\n"},
		".golangci.yml: golangci-lint — floor rule ineffassign disabled",
		".golangci.yml: golangci-lint — floor rule staticcheck disabled",
		".golangci.yml: golangci-lint — floor rule unused disabled")
	// fast is no default that counts
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  default: fast\n  enable: [errcheck, govet, ineffassign, staticcheck]\n"},
		".golangci.yml: golangci-lint — floor rule unused disabled")
	// standard covers its own set only: an extra floor linter needs enable
	extra := map[string]string{goHook: "BLOCKING_RULES=\"errcheck,govet,ineffassign,staticcheck,unused,gosec\"\n"}
	wantGo(t, extra, ".golangci.yml: golangci-lint — floor rule gosec disabled")
	extra[".golangci.yml"] = goStandardCfg + "  enable: [gosec]\n"
	wantGo(t, extra)
}

func TestGoExclusionsAreScope(t *testing.T) {
	wantGo(t, map[string]string{".golangci.yml": goStandardCfg +
		"  exclusions:\n    paths: [third_party]\n    rules:\n      - path: _test\\.go\n        linters: [errcheck]\n"})
}

func TestGoFloorExcludesTypecheck(t *testing.T) {
	// typecheck is in BLOCKING_RULES but also in NON_LINTER_RULES: no config or CI flag needs it
	wantGo(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  default: none\n  enable: [errcheck, govet, ineffassign, staticcheck, unused]\n"})
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0",
		"golangci-lint run --enable-only errcheck,govet,ineffassign,staticcheck,unused")})
	// without NON_LINTER_RULES typecheck is a floor linter the config must turn on
	wantGo(t, map[string]string{goHook: "BLOCKING_RULES=\"errcheck,govet,ineffassign,staticcheck,unused,typecheck\"\n"},
		".golangci.yml: golangci-lint — floor rule typecheck disabled")
}

func TestGoConfigAsTomlJson(t *testing.T) {
	wantGo(t, map[string]string{".golangci.yml": pyDelete, ".golangci.toml": "version = \"2\"\n[linters]\ndefault = \"standard\"\n"})
	wantGo(t, map[string]string{".golangci.yml": pyDelete, ".golangci.json": `{"version": "2", "linters": {"default": "standard"}}`})
	wantGo(t, map[string]string{".golangci.yml": pyDelete, ".golangci.yaml": goStandardCfg})
	wantGo(t, map[string]string{".golangci.yml": pyDelete, ".golangci.toml": "version = \"2\"\n[linters]\ndefault = \"standard\"\ndisable = [\"govet\"]\n"},
		".golangci.toml: golangci-lint — floor rule govet disabled")
	wantGo(t, map[string]string{".golangci.yml": pyDelete, ".golangci.json": `{"linters": {"default": "standard"}}`},
		".golangci.json: golangci-lint — config not version 2")
	// golangci-lint's own order: .yml before .toml
	wantGo(t, map[string]string{".golangci.toml": "version = \"2\"\n"})
}

func TestGoInvalidConfigIsUnchecked(t *testing.T) {
	wantGoUnchecked(t, map[string]string{".golangci.yml": "version: [\n"})
	wantGoUnchecked(t, map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  enable: errcheck\n"})
	wantGoUnchecked(t, map[string]string{goHook: pyDelete})
}

func TestGoConfigFlagLaxFile(t *testing.T) {
	install := "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"
	wantGo(t, map[string]string{
		".golangci-ci.yml": "version: \"2\"\nlinters:\n  default: none\n  enable: [govet, ineffassign, staticcheck, unused]\n",
		wf + "ci.yml":      pyWorkflow(install, "golangci-lint run --config .golangci-ci.yml"),
	}, ".github/workflows/ci.yml:7: golangci-lint — CI config .golangci-ci.yml misses the floor")
	// a strict file is fine, also resolved from the working directory of a cd
	wantGo(t, map[string]string{
		"ci/strict.toml": "version = \"2\"\n[linters]\ndefault = \"all\"\n",
		wf + "ci.yml":    pyWorkflow(install, "cd ci && golangci-lint run -c strict.toml"),
	})
	// a missing file, a v1 file and one without default miss the floor
	for _, cfg := range []string{"", "linters:\n  enable-all: true\n", "version: \"2\"\n"} {
		over := map[string]string{wf + "ci.yml": pyWorkflow(install, "golangci-lint run --config=lax.yml")}
		if cfg != "" {
			over["lax.yml"] = cfg
		}
		wantGo(t, over, ".github/workflows/ci.yml:7: golangci-lint — CI config lax.yml misses the floor")
	}
}

func TestGoInstallOtherJob(t *testing.T) {
	body := "on: [push]\njobs:\n  setup:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0\n" +
		"  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: golangci-lint run\n"
	wantGo(t, map[string]string{wf + "ci.yml": body}, ".github/workflows/ci.yml:10: golangci-lint — CI version not pinned")
}

func TestGoInstallAfterCall(t *testing.T) {
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow("golangci-lint run",
		"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0")},
		".github/workflows/ci.yml:6: golangci-lint — CI version not pinned")
}

func TestGoCIVersionOnlyBeforeTheCall(t *testing.T) {
	wantGo(t, map[string]string{wf + "ci.yml": pyWorkflow(
		"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 && golangci-lint run && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0")},
		".github/workflows/ci.yml:6: golangci-lint — CI version v2.14.0, pin v2.15.0")
}

func TestGoAllFindingsTogether(t *testing.T) {
	wantGo(t, map[string]string{".golangci-version": pyDelete, ".golangci.yml": pyDelete, wf + "ci.yml": pyDelete},
		".golangci-version: golangci-lint — pin missing",
		".golangci.yml: golangci-lint — config missing",
		".github/workflows: golangci-lint — no CI step runs golangci-lint")
}
