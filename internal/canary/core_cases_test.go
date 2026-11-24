package canary

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
	"github.com/CorneeldH/lighthouse/internal/hook"
)

// controllerRun is a uuid-shaped LIGHTHOUSE_CONTROLLER_RUN: workflow-push-gate does nothing outside one.
const controllerRun = "LIGHTHOUSE_CONTROLLER_RUN=123e4567-e89b-42d3-a456-426614174000"

// commandCase is a canary of a Bash-command hook: testdata/<hook>/<id>.cmd is the command.
type commandCase struct {
	hook, script string
	setup        func(t *testing.T, root string) (env []string)
}

// addOrigin gives root a bare remote next to it as origin, with main pushed to it.
func addOrigin(t *testing.T, root string) {
	t.Helper()
	parent := filepath.Dir(root)
	fixturesetup.Build(t, parent, []map[string]any{
		{"bare": "origin.git"},
		{"origin": "repo", "url": filepath.Join(parent, "origin.git")},
	})
	fixturesetup.Git(t, root, "push", "-q", "origin", "main")
}

// writeConfig replaces lighthouse.toml of root with text: the canary needs only the keys it names.
func writeConfig(t *testing.T, root, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(text+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dropBlocking removes name from .claude/hooks/lighthouse/blocking of root.
func dropBlocking(t *testing.T, root, name string) {
	t.Helper()
	path := filepath.Join(root, ".claude", "hooks", "lighthouse", "blocking")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	kept := slices.DeleteFunc(lines, func(l string) bool { return strings.TrimSpace(l) == name })
	if len(kept) == len(lines) {
		t.Fatalf("%s is not in %s", name, path)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// extraPacks are the packs a command case installs next to core: the import check of
// commit-test-gate checks only the languages of the installed packs.
var extraPacks = map[string][]string{"undeclared-import": {"go"}}

var commandCases = map[string]commandCase{
	"commit-to-main": {"main-commit-gate", "run.sh main-commit-gate", func(*testing.T, string) []string { return nil }},
	"push-to-main": {"main-commit-gate", "run.sh main-commit-gate", func(t *testing.T, root string) []string {
		addOrigin(t, root)
		return nil
	}},
	// commit-test-gate refuses --no-verify only in a repo whose pack predates no-verify-check:
	// drop that line from the blocking list, as an older pack has it.
	"no-verify": {"commit-test-gate", "run.sh commit-test-gate pre", func(t *testing.T, root string) []string {
		dropBlocking(t, root, "no-verify-check")
		return nil
	}},
	"memory-write": {"memory-gate", "run.sh memory-gate", func(*testing.T, string) []string { return nil }},
	"flag":         {"no-verify-check", "run.sh no-verify-check", func(*testing.T, string) []string { return nil }},
	"tests-red": {"commit-test-gate", "run.sh commit-test-gate pre", func(t *testing.T, root string) []string {
		writeConfig(t, root, "[test_gate]\ncommand = \"false\"")
		return nil
	}},
	"undeclared-import": {"commit-test-gate", "run.sh commit-test-gate pre", func(t *testing.T, root string) []string {
		writeConfig(t, root, "[test_gate]\ncommand = \"true\"")
		// An import of a module go.mod does not require; the replace resolves it offline.
		fixturesetup.Build(t, root, []map[string]any{
			{"file": "go.mod", "text": "module example.test/app\n\ngo 1.23\n\nreplace example.test/dep => ./dep\n"},
			{"file": "dep/go.mod", "text": "module example.test/dep\n\ngo 1.23\n"},
			{"file": "dep/dep.go", "text": "package dep\n\nfunc F() {}\n"},
			{"file": "app.go", "text": "package app\n\nimport \"example.test/dep\"\n\nfunc G() { dep.F() }\n"},
		})
		fixturesetup.Git(t, root, "add", "go.mod", "dep", "app.go")
		return []string{"GOPROXY=off", "GOFLAGS="}
	}},
	"pr-body": {"pr-create-gate", "run.sh pr-create-gate", func(t *testing.T, root string) []string {
		writeConfig(t, root, "[forge]\nkind = \"github\"\nrepo = \"example/canary\"")
		return nil
	}},
	"workflow-path": {"workflow-push-gate", "run.sh workflow-push-gate", func(t *testing.T, root string) []string {
		addOrigin(t, root)
		fixturesetup.Git(t, root, "switch", "-q", "-c", "topic")
		fixturesetup.Build(t, root, []map[string]any{
			{"file": ".github/workflows/ci.yml", "text": "on: push\n"},
			{"commit": ".", "message": "add workflow"},
		})
		return []string{controllerRun}
	}},
}

// fileCase is a canary of a file hook: testdata/<hook>/<id>.<ext> is the file, written as
// canary.<ext> into a repository whose path does not hold the ID.
type fileCase struct {
	hook, script string
	setup        func(t *testing.T, root string)
}

var fileCases = map[string]fileCase{
	"dead":     {"md-link-check", "run.sh md-link-check", nil},
	"absolute": {"md-link-check", "run.sh md-link-check", nil},
	"non-english": {"code-language-check", "run.sh code-language-check", func(t *testing.T, root string) {
		writeConfig(t, root, "[project]\ncode_language = \"en\"\ncode_language_paths = [\"**\"]")
	}},
}

// Every declared reason of a core hook has a canary case, and every case a declared reason.
func TestEveryCoreReasonHasACanaryCase(t *testing.T) {
	var cases, declared []string
	for id, c := range commandCases {
		cases = append(cases, c.hook+"/"+id)
	}
	for id, c := range fileCases {
		cases = append(cases, c.hook+"/"+id)
	}
	for _, name := range hook.DeclaredHooks() {
		if name == "bash-write-check" {
			continue
		}
		ids, ok := hook.Reasons(name)
		if !ok {
			t.Fatalf("%s is declared but has no reasons entry", name)
		}
		for _, id := range ids {
			declared = append(declared, name+"/"+id)
		}
	}
	slices.Sort(cases)
	slices.Sort(declared)
	if !slices.Equal(cases, declared) {
		t.Errorf("canary cases %v, want one per declared reason %v", cases, declared)
	}
}
