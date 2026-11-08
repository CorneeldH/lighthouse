package gate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../tests/pack/fixtures/gate_node", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatal(err)
	}
}

func TestNodeTouchesFixtures(t *testing.T) {
	var cases []struct {
		Name  string
		Files []string
		Dir   string
		Also  []string
		Want  bool
	}
	readFixture(t, "touches.json", &cases)
	if len(cases) != 7 {
		t.Fatalf("fixture has %d cases, want 7", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := NodeTouches(c.Files, c.Dir, c.Also); got != c.Want {
				t.Errorf("NodeTouches = %v, want %v", got, c.Want)
			}
		})
	}
}

func TestNodeUsageFixtures(t *testing.T) {
	var cases []struct {
		Name string
		Argv []string
	}
	readFixture(t, "usage.json", &cases)
	if len(cases) != 16 {
		t.Fatalf("fixture has %d cases, want 16", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			rc, _, errOut := runNode(t, nodeProject(t), "", c.Argv...)
			want := "gate-node: COULD NOT CHECK — usage: lighthouse gate node [--dir DIR] " +
				"[--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT...\n"
			if rc != 3 || errOut != want {
				t.Errorf("rc=%d stderr=%q", rc, errOut)
			}
		})
	}
}

const fakeNpm = `#!/bin/sh
echo "$PWD $*" >> "$NPM_LOG"
script="$3"
if [ "$script" = gen ]; then
  if [ -n "$GEN_CONTENT" ]; then printf '%s' "$GEN_CONTENT" > src/api/client.ts; fi
  if [ -n "$GEN_FILE" ]; then printf '%s' "$GEN_FILE" > src/api.ts; fi
  if [ -n "$GEN_NEW" ]; then printf '%s' "$GEN_NEW" > src/api/new.ts; fi
fi
eval "exit \${NPM_EXIT_$script:-0}"
`

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// nodeProject is a root with web/ (package.json, node_modules, src/api) and a PATH that holds a
// fake npm, which logs its call to the file in NPM_LOG, and a fake node.
func nodeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	web := filepath.Join(root, "web")
	writeFile(t, filepath.Join(web, "node_modules", ".keep"), "", 0o644)
	writeFile(t, filepath.Join(web, "package.json"),
		`{"scripts": {"gen": "x", "lint": "x", "typecheck": "x", "test": "x"}}`, 0o644)
	writeFile(t, filepath.Join(web, "src", "api.ts"), "v1", 0o644)
	writeFile(t, filepath.Join(web, "src", "api", "client.ts"), "v1", 0o644)
	bin := filepath.Join(root, "bin")
	writeFile(t, filepath.Join(bin, "npm"), fakeNpm, 0o755)
	writeFile(t, filepath.Join(bin, "node"), "#!/bin/sh\nexit 0\n", 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NPM_LOG", filepath.Join(root, "npm.log"))
	for _, name := range []string{"GEN_CONTENT", "GEN_FILE", "GEN_NEW", "NPM_EXIT_lint", "NPM_EXIT_gen"} {
		t.Setenv(name, "")
	}
	return root
}

func runNode(t *testing.T, root, files string, args ...string) (rc int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	rc = Node(args, Env{Dir: root, FilesPath: files, Stdout: &out, Stderr: &errOut})
	return rc, out.String(), errOut.String()
}

func npmLog(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "npm.log"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestNodeSkipsWhenNothingUnderDir(t *testing.T) {
	root := nodeProject(t)
	rc, out, _ := runNode(t, root, filesList(t, root, "pack/go/x.go"), "--dir", "web", "--", "lint")
	if rc != 0 || out != "gate-node: skipped — the commit touches nothing under web\n" {
		t.Errorf("rc=%d out=%q", rc, out)
	}
	if len(npmLog(t, root)) != 0 {
		t.Errorf("npm ran: %q", npmLog(t, root))
	}
}

func TestNodeSkipLineForDotDir(t *testing.T) {
	root := nodeProject(t)
	files := filepath.Join(root, "empty.txt")
	writeFile(t, files, "", 0o644)
	rc, out, _ := runNode(t, root, files, "--dir", ".", "--", "lint")
	if rc != 0 || out != "gate-node: skipped — the commit touches nothing\n" {
		t.Errorf("rc=%d out=%q", rc, out)
	}
}

func TestNodeSkipsBeforeLookingAtNodeModules(t *testing.T) {
	root := nodeProject(t)
	if err := os.RemoveAll(filepath.Join(root, "web", "node_modules")); err != nil {
		t.Fatal(err)
	}
	rc, out, _ := runNode(t, root, filesList(t, root, "pack/go/x.go"), "--dir", "web", "--", "lint")
	if rc != 0 || !strings.Contains(out, "gate-node: skipped") {
		t.Errorf("rc=%d out=%q", rc, out)
	}
}

func TestNodeRunsDriftThenScriptsInDir(t *testing.T) {
	root := nodeProject(t)
	rc, out, errOut := runNode(t, root, filesList(t, root, "web/src/a.ts"),
		"--dir", "web", "--drift", "gen=src/api", "--", "lint", "typecheck", "test")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errOut)
	}
	web := filepath.Join(root, "web")
	want := []string{web + " run --silent gen", web + " run --silent lint",
		web + " run --silent typecheck", web + " run --silent test"}
	if got := npmLog(t, root); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("npm calls = %q, want %q", got, want)
	}
	wantOut := "gate-node: web — npm run gen\ngate-node: web — npm run lint\n" +
		"gate-node: web — npm run typecheck\ngate-node: web — npm run test\n" +
		"gate-node: passed — gen, lint, typecheck, test\n"
	if out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

func TestNodeAlsoPathTouches(t *testing.T) {
	root := nodeProject(t)
	for _, also := range []string{"lighthouse.toml", "./lighthouse.toml"} {
		_ = os.Remove(filepath.Join(root, "npm.log"))
		rc, _, _ := runNode(t, root, filesList(t, root, "lighthouse.toml"),
			"--dir", "web", "--also", also, "--", "lint")
		if rc != 0 || len(npmLog(t, root)) != 1 {
			t.Errorf("also %q: rc=%d log=%q", also, rc, npmLog(t, root))
		}
	}
}

func TestNodeNormalisesDir(t *testing.T) {
	root := nodeProject(t)
	rc, _, _ := runNode(t, root, filesList(t, root, "web/a.ts"), "--dir", "./web", "--", "lint")
	if rc != 0 || len(npmLog(t, root)) != 1 {
		t.Errorf("rc=%d log=%q", rc, npmLog(t, root))
	}
}

func TestNodePathWithSpaceTouches(t *testing.T) {
	root := nodeProject(t)
	rc, _, _ := runNode(t, root, filesList(t, root, "web/src/a b.ts"), "--dir", "web", "--", "lint")
	if rc != 0 || len(npmLog(t, root)) != 1 {
		t.Errorf("rc=%d log=%q", rc, npmLog(t, root))
	}
}

func TestNodeStopsAtFirstFailure(t *testing.T) {
	root := nodeProject(t)
	t.Setenv("NPM_EXIT_lint", "7")
	rc, _, _ := runNode(t, root, filesList(t, root, "web/a.ts"), "--dir", "web", "--", "lint", "typecheck")
	if rc != 7 {
		t.Errorf("rc = %d, want 7", rc)
	}
	for _, line := range npmLog(t, root) {
		if strings.Contains(line, "typecheck") {
			t.Errorf("typecheck ran: %q", npmLog(t, root))
		}
	}
}

func TestNodeWithoutFilesRunsEverything(t *testing.T) {
	root := nodeProject(t)
	rc, _, _ := runNode(t, root, "", "--dir", "web", "--", "lint", "typecheck")
	if rc != 0 || len(npmLog(t, root)) != 2 {
		t.Errorf("rc=%d log=%q", rc, npmLog(t, root))
	}
}

func TestNodeDrift(t *testing.T) {
	cases := []struct {
		name, env, value, path string
		wantRC                 int
		wantLine               string
	}{
		{"changed content", "GEN_CONTENT", "v2", "src/api", 1,
			"gate-node: web/src/api was stale; npm run gen regenerated it — commit the result\n"},
		{"new file", "GEN_NEW", "x", "src/api", 1,
			"gate-node: web/src/api was stale; npm run gen regenerated it — commit the result\n"},
		{"single file", "GEN_FILE", "v2", "src/api.ts", 1,
			"gate-node: web/src/api.ts was stale; npm run gen regenerated it — commit the result\n"},
		{"up to date", "GEN_CONTENT", "v1", "src/api", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := nodeProject(t)
			t.Setenv(c.env, c.value)
			rc, out, _ := runNode(t, root, filesList(t, root, "web/a.ts"),
				"--dir", "web", "--drift", "gen="+c.path, "--", "lint")
			if rc != c.wantRC {
				t.Errorf("rc = %d, want %d", rc, c.wantRC)
			}
			if c.wantLine != "" {
				if !strings.Contains(out, c.wantLine) {
					t.Errorf("stdout = %q, want line %q", out, c.wantLine)
				}
				for _, line := range npmLog(t, root) {
					if strings.Contains(line, "lint") {
						t.Errorf("lint ran after stale drift")
					}
				}
			}
		})
	}
}

func TestNodeDriftGenFailureReturnsItsCode(t *testing.T) {
	root := nodeProject(t)
	t.Setenv("NPM_EXIT_gen", "4")
	rc, _, _ := runNode(t, root, filesList(t, root, "web/a.ts"),
		"--dir", "web", "--drift", "gen=src/api", "--", "lint")
	if rc != 4 {
		t.Errorf("rc = %d, want 4", rc)
	}
	for _, line := range npmLog(t, root) {
		if strings.Contains(line, "lint") {
			t.Errorf("lint ran after failed gen")
		}
	}
}

func TestNodeUnchecked(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, root string)
		files   string
		args    []string
		wantErr string
	}{
		{"node_modules missing", func(t *testing.T, root string) {
			_ = os.RemoveAll(filepath.Join(root, "web", "node_modules"))
		}, "web/a.ts", []string{"--dir", "web", "--", "lint"},
			"gate-node: COULD NOT CHECK — web/node_modules missing; fix: cd web && npm ci\n"},
		{"npm not on PATH", func(t *testing.T, root string) {
			only := filepath.Join(root, "only-node")
			writeFile(t, filepath.Join(only, "node"), "#!/bin/sh\nexit 0\n", 0o755)
			t.Setenv("PATH", only)
		}, "web/a.ts", []string{"--dir", "web", "--", "lint"},
			"gate-node: COULD NOT CHECK — npm not on PATH\n"},
		{"unknown script", func(*testing.T, string) {}, "web/a.ts",
			[]string{"--dir", "web", "--", "lint", "nope"},
			"gate-node: COULD NOT CHECK — no script nope in web/package.json\n"},
		{"unknown drift script", func(*testing.T, string) {}, "web/a.ts",
			[]string{"--dir", "web", "--drift", "nope=src/api", "--", "lint"},
			"gate-node: COULD NOT CHECK — no script nope in web/package.json\n"},
		{"invalid package.json", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "web", "package.json"), "{", 0o644)
		}, "web/a.ts", []string{"--dir", "web", "--", "lint"}, "cannot read web/package.json: "},
		{"scripts not an object", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "web", "package.json"), `{"scripts": []}`, 0o644)
		}, "web/a.ts", []string{"--dir", "web", "--", "lint"},
			"gate-node: COULD NOT CHECK — web/package.json has no scripts object\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := nodeProject(t)
			c.setup(t, root)
			rc, _, errOut := runNode(t, root, filesList(t, root, c.files), c.args...)
			if rc != 3 || !strings.Contains(errOut, c.wantErr) {
				t.Errorf("rc=%d stderr=%q, want it to contain %q", rc, errOut, c.wantErr)
			}
			if len(npmLog(t, root)) != 0 {
				t.Errorf("npm ran: %q", npmLog(t, root))
			}
		})
	}
}

func TestNodeUnreadableListIsUnchecked(t *testing.T) {
	root := nodeProject(t)
	rc, _, errOut := runNode(t, root, filepath.Join(root, "missing.txt"), "--dir", "web", "--", "lint")
	if rc != 3 || !strings.HasPrefix(errOut, "gate-node: COULD NOT CHECK — cannot read the changed files: ") {
		t.Errorf("rc=%d stderr=%q", rc, errOut)
	}
}
