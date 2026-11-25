//go:build canary

package canary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

const (
	goLintGateFile = "go/hooks/go-lint-gate.sh"
	gofmtCheckFile = "go/hooks/gofmt-check.sh"
	// goModule is the smallest module that lints clean under every linter of the floor.
	goModule = "module example.com/m\n\ngo 1.26\n"
	cleanGo  = "package m\n\nfunc F() {}\n"
)

// goRepo is an installed repository with the go pack, a root module and a golangci-lint v2
// configuration with the given section body (indented under `linters:`).
func goRepo(t *testing.T, section string) string {
	t.Helper()
	root := installRepo(t, "core", "go")
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", goModule)
	write(".golangci.yml", "version: \"2\"\nlinters:\n"+section)
	return root
}

const stdSet = "  default: standard\n"

// stopPayload is a Stop payload for a turn that edited file in root.
func stopPayload(t *testing.T, root, file string) string {
	t.Helper()
	entries := []map[string]any{
		{"type": "user", "timestamp": "2026-10-25T10:00:00.000Z", "cwd": root,
			"message": map[string]any{"role": "user", "content": "go"}},
		{"type": "assistant", "timestamp": "2026-10-25T10:00:01.000Z", "cwd": root,
			"message": map[string]any{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "toolu_1", "name": "Edit", "input": map[string]any{"file_path": file}},
			}}},
	}
	var transcript strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		transcript.Write(line)
		transcript.WriteByte('\n')
	}
	path := filepath.Join(filepath.Dir(root), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(transcript.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"session_id": "s1", "transcript_path": path, "cwd": root, "hook_event_name": "Stop", "stop_hook_active": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

// runGoLintGate writes source as canary.go and runs go-lint-gate in the repository; the usage alarm
// is read from an empty cache, never from the machine's own.
func runGoLintGate(t *testing.T, root, source string, env ...string) run {
	t.Helper()
	file := filepath.Join(root, "canary.go")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	env = append(env, "XDG_CACHE_HOME="+filepath.Join(filepath.Dir(root), "xdg-cache"))
	return runHook(t, root, "go-lint-gate.sh", stopPayload(t, root, file), env...)
}

func readFixture(t *testing.T, hookName, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", hookName, id+".go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGoLintGateCanary(t *testing.T) {
	for _, id := range blockingRules(t, goLintGateFile) {
		t.Run(id, func(t *testing.T) {
			root := goRepo(t, stdSet)
			if strings.Contains(root, id) {
				t.Fatalf("the reason ID %q is in the repository path %s", id, root)
			}
			r := runGoLintGate(t, root, readFixture(t, "go-lint-gate", id))
			requireCanary(t, r, id, "("+id+")")
		})
	}
}

// The floor: a configuration that switches a linter of the list off blocks on a clean module,
// so the hook does not rely on the finding for it.
func TestGoLintGateFloorHostileConfig(t *testing.T) {
	skipRules := nonLinterRules(t, goLintGateFile)
	var floor []string
	for _, id := range blockingRules(t, goLintGateFile) {
		if !slices.Contains(skipRules, id) {
			floor = append(floor, id)
		}
	}
	for _, id := range floor {
		// Two ways to leave the linter off: switched off by name, and never switched on
		// (`default: none` with only the other linters of the floor enabled).
		var others strings.Builder
		for _, other := range floor {
			if other != id {
				others.WriteString("    - " + other + "\n")
			}
		}
		configs := map[string]string{
			"disabled":     stdSet + "  disable:\n    - " + id + "\n",
			"default-none": "  default: none\n  enable:\n" + others.String(),
		}
		for name, section := range configs {
			t.Run(id+"/"+name, func(t *testing.T) {
				root := goRepo(t, section)
				r := runGoLintGate(t, root, cleanGo)
				requireCanary(t, r, id, "[go-lint-gate/"+id+"]")
				if !strings.Contains(r.Stderr, "issues.new-from-rev") {
					t.Errorf("the block does not name issues.new-from-rev:\n%s", r.Stderr)
				}
			})
		}
	}
}

// The floor is no ceiling: a configuration with more linters keeps all of them.
func TestGoLintGateFloorKeepsStricterAdopterConfig(t *testing.T) {
	const singleCase = "package m\n\nfunc F(x int) {\n\tswitch x {\n\tcase 1:\n\t\tprintln(\"a\")\n\t}\n}\n"
	if r := runGoLintGate(t, goRepo(t, stdSet), singleCase); r.Code != 0 {
		t.Fatalf("the standard set blocks the gocritic fixture: exit %d\n%s", r.Code, r.Stderr)
	}
	root := goRepo(t, stdSet+"  enable:\n    - gocritic\n")
	requireCanary(t, runGoLintGate(t, root, singleCase), "gocritic", "(gocritic)")
}

func TestGoLintGateCleanModulePasses(t *testing.T) {
	if r := runGoLintGate(t, goRepo(t, stdSet), cleanGo); r.Code != 0 || r.Stdout != "" {
		t.Fatalf("a clean module: exit %d\nstdout: %s\nstderr: %s", r.Code, r.Stdout, r.Stderr)
	}
}

// fakeGolangciV1 puts a golangci-lint that says it is v1.64.8 first on the PATH; its `run` prints
// runOut and exits runRC.
func fakeGolangciV1(t *testing.T, runOut string, runRC int) string {
	t.Helper()
	stubbin.RequirePOSIX(t, "case on $1 and the FAKE_RUN_* variables")
	dir, err := os.MkdirTemp(workDir, "fake-v1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	script := "case \"$1\" in\n" +
		"  version) echo 1.64.8 ;;\n" +
		"  config) exit 0 ;;\n" +
		"  run) printf '%s\\n' \"$FAKE_RUN_OUT\"; exit \"$FAKE_RUN_RC\" ;;\n" +
		"esac\n"
	stubbin.MakeScript(t, dir, "golangci-lint", script)
	return stubbin.PathValue(t, dir+string(os.PathListSeparator)+filepath.Dir(lighthouseBin(t)))
}

func TestGoLintGateV1IsUnchecked(t *testing.T) {
	const floorNote = "COULD NOT CHECK the floor"
	t.Run("without finding", func(t *testing.T) {
		root := goRepo(t, stdSet)
		path := fakeGolangciV1(t, "", 0)
		r := runGoLintGate(t, root, cleanGo, "PATH="+path, "FAKE_RUN_OUT=", "FAKE_RUN_RC=0")
		if r.Code != 0 || !strings.Contains(r.Stdout, floorNote) {
			t.Fatalf("exit %d, want 0 with %q in the system message\nstdout: %s\nstderr: %s", r.Code, floorNote, r.Stdout, r.Stderr)
		}
	})
	t.Run("with finding", func(t *testing.T) {
		root := goRepo(t, stdSet)
		path := fakeGolangciV1(t, "canary.go:6:11: Error return value is not checked (errcheck)", 1)
		r := runGoLintGate(t, root, cleanGo, "PATH="+path,
			"FAKE_RUN_OUT=canary.go:6:11: Error return value is not checked (errcheck)", "FAKE_RUN_RC=1")
		requireCanary(t, r, "errcheck", "(errcheck)")
		if !strings.Contains(r.Stderr, floorNote) {
			t.Fatalf("the block lacks %q:\n%s", floorNote, r.Stderr)
		}
	})
}

func TestGofmtParseErrorCanary(t *testing.T) {
	for _, id := range blockingRules(t, gofmtCheckFile) {
		t.Run(id, func(t *testing.T) {
			root := installRepo(t, "core", "go")
			data, err := os.ReadFile(filepath.Join("testdata", "gofmt-check", id+".go"))
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "canary.go")
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]any{
				"cwd": root, "hook_event_name": "PostToolUse", "tool_name": "Write",
				"tool_input": map[string]any{"file_path": file},
			})
			if err != nil {
				t.Fatal(err)
			}
			r := runHook(t, root, "gofmt-check.sh", string(payload))
			requireCanary(t, r, id, "[gofmt-check/"+id+"]")
		})
	}
}
