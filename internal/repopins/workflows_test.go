package repopins

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// gateGoFlags returns the flags of the Go part of [test_gate] command in lighthouse.toml
// (`lighthouse gate go -- go test ...`).
func gateGoFlags(t *testing.T) []string {
	t.Helper()
	toml := read(t, "lighthouse.toml")
	m := regexp.MustCompile(`lighthouse gate go --\s+(go test[^&]*?)\s*(?:&&|")`).FindStringSubmatch(toml)
	if m == nil {
		t.Fatal("no `lighthouse gate go -- go test ...` in [test_gate] command of lighthouse.toml")
	}
	var flags []string
	for _, tok := range strings.Fields(m[1]) {
		if strings.HasPrefix(tok, "-") {
			flags = append(flags, tok)
		}
	}
	if len(flags) == 0 {
		t.Fatalf("the Go part of the gate has no flags to pin: %q", m[1])
	}
	return flags
}

// goTestCommands returns every `go test` run of a workflow, over all its jobs.
func goTestCommands(t *testing.T, workflow string) []string {
	t.Helper()
	var out []string
	for _, body := range jobs(read(t, ".github", "workflows", workflow)) {
		for _, run := range runBlocks(body) {
			if strings.HasPrefix(run, "go test ") {
				out = append(out, run)
			}
		}
	}
	return out
}

func assertGateFlags(t *testing.T, workflow string) {
	t.Helper()
	flags := gateGoFlags(t)
	runs := goTestCommands(t, workflow)
	if len(runs) != 2 {
		t.Fatalf("%s has %d `go test` runs, want 2 (unit and e2e): %q", workflow, len(runs), runs)
	}
	for _, run := range runs {
		words := strings.Fields(run)
		for _, flag := range flags {
			found := false
			for _, w := range words {
				found = found || w == flag
			}
			if !found {
				t.Errorf("%s: %q lacks the gate flag %s", workflow, run, flag)
			}
		}
	}
}

func hasLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// A drift between the pin and the lint action would show only as a different lint result on CI.
func TestGolangciPinMatchesCI(t *testing.T) {
	pin := strings.TrimSpace(read(t, ".golangci-version"))
	ci := read(t, ".github", "workflows", "ci.yml")
	m := regexp.MustCompile(`(?s)uses: golangci/golangci-lint-action@\S+.*?\n\s+with:\n\s+version: (\S+)`).FindStringSubmatch(ci)
	if m == nil {
		t.Fatal("no golangci-lint-action step with a version in ci.yml")
	}
	if m[1] != pin {
		t.Errorf("ci.yml lints with %s, .golangci-version pins %s", m[1], pin)
	}
}

// A `go test` step without a flag of the gate runs a weaker test than the commit gate does.
func TestEveryCIGoTestHasGateFlags(t *testing.T) {
	assertGateFlags(t, "ci.yml")
}

func TestEveryMacOSGoTestHasGateFlags(t *testing.T) {
	assertGateFlags(t, "macos.yml")
}

func TestCIBuildsWithoutCGO(t *testing.T) {
	runs := runBlocks(read(t, ".github", "workflows", "ci.yml"))
	for _, run := range runs {
		if run == "CGO_ENABLED=0 go build ./..." {
			return
		}
	}
	t.Errorf("ci.yml has no `CGO_ENABLED=0 go build ./...` step, got runs %q", runs)
}

// A platform-only break would otherwise show first at the release.
func TestCICrossBuildsReleaseTargets(t *testing.T) {
	runs := runBlocks(read(t, ".github", "workflows", "ci.yml"))
	for _, target := range []string{"GOOS=windows GOARCH=amd64", "GOOS=darwin GOARCH=arm64"} {
		want := target + " CGO_ENABLED=0 go build ./..."
		found := false
		for _, run := range runs {
			found = found || run == want
		}
		if !found {
			t.Errorf("ci.yml has no %q step", want)
		}
	}
}

func TestPagesPublishesTheSiteFolder(t *testing.T) {
	if !hasLine(read(t, ".github", "workflows", "pages.yml"), "path: docs/site") {
		t.Error("pages.yml does not publish `path: docs/site`")
	}
}

func TestPagesRunsOnSiteChangesOnMain(t *testing.T) {
	text := read(t, ".github", "workflows", "pages.yml")
	for _, want := range []string{"branches: [main]", `- "docs/site/**"`, "workflow_dispatch:"} {
		if !hasLine(text, want) {
			t.Errorf("pages.yml lacks the line %q", want)
		}
	}
}

func TestPagesPermissions(t *testing.T) {
	text := read(t, ".github", "workflows", "pages.yml")
	for _, want := range []string{"pages: write", "id-token: write", "contents: read"} {
		if !hasLine(text, want) {
			t.Errorf("pages.yml lacks the permission %q", want)
		}
	}
}

// A tag can move; only a full commit SHA, with its tag as a comment, pins an action.
func TestEveryWorkflowPinsActionsBySHA(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflow files found: %v", err)
	}
	uses := regexp.MustCompile(`^\s*(?:- )?uses:\s*(\S+)`)
	pinned := regexp.MustCompile(`@[0-9a-f]{40} # v\d+(\.\d+)*\s*$`)
	for _, file := range files {
		rel := filepath.Base(file)
		for _, line := range strings.Split(read(t, ".github", "workflows", rel), "\n") {
			m := uses.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "./.github/workflows/") {
				continue
			}
			if !pinned.MatchString(line) {
				t.Errorf("%s: action not pinned by SHA with a version comment: %s", rel, strings.TrimSpace(line))
			}
		}
	}
}
