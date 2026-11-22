package importcheck

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// nodeSample is the real shape of `knip --reporter json`, with a category besides unlisted.
const nodeSample = `{"issues":[
 {"file":"src/index.js","unlisted":[{"name":"lodash","line":1,"col":15,"pos":14},{"name":"left-pad","line":2,"col":19,"pos":42}]},
 {"file":"src/other.js","unlisted":[],"exports":[{"name":"unusedExport","line":3,"col":1,"pos":9}]},
 {"file":"package.json","dependencies":[{"name":"unused","line":5,"col":3,"pos":50}]},
 {"file":"src/noline.js","unlisted":[{"name":"zod"}]}
]}`

func TestParseKnipUnlisted(t *testing.T) { // lighthouse: allow-language — name of the tool
	got, err := parseKnip([]byte(nodeSample))
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{File: "src/index.js", Line: 1, ID: "unlisted", Import: "lodash", Reason: knipReason},
		{File: "src/index.js", Line: 2, ID: "unlisted", Import: "left-pad", Reason: knipReason},
		{File: "src/noline.js", Line: 1, ID: "unlisted", Import: "zod", Reason: knipReason},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %+v, want %+v", got, want)
	}
	for _, bad := range []string{"not json", "", "{}", `{"issues":null}`} {
		if _, err := parseKnip([]byte(bad)); err == nil {
			t.Errorf("parseKnip(%q) must be an error", bad)
		}
	}
	if got, err := parseKnip([]byte(`{"issues":[]}`)); err != nil || len(got) != 0 {
		t.Errorf("empty issues: %v %v", got, err)
	}
}

type nodeExec struct {
	calls  int
	argv   []string
	dir    string
	stdout string
	code   int
	err    error
}

func (n *nodeExec) exec(dir string, argv []string, _ []string) ([]byte, []byte, int, error) {
	n.calls++
	n.argv, n.dir = argv, dir
	if n.err != nil {
		return nil, nil, 0, n.err
	}
	return []byte(n.stdout), []byte("stderr text"), n.code, nil
}

func nodeEnv(root string, n *nodeExec, noNpx bool) Env {
	return Env{
		Root:   root,
		Stderr: &bytes.Buffer{},
		Exec:   n.exec,
		LookPath: func(string) (string, error) {
			if noNpx {
				return "", errors.New("not found")
			}
			return "/bin/npx", nil
		},
	}
}

func nodeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{"web/package.json": "{}", "web/node_modules/.keep": ""})
	return root
}

func TestNodeCheckerFindingsAndInvocation(t *testing.T) {
	root := nodeRoot(t)
	n := &nodeExec{stdout: nodeSample, code: 1}
	got, err := NodeChecker{}.Check(Project{Lang: "node", Dir: "web"}, nodeEnv(root, n, false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].File != "web/src/index.js" || got[2].File != "web/src/noline.js" {
		t.Fatalf("findings = %+v, want three, files mapped under web", got)
	}
	wantArgv := []string{"npx", "--yes", "knip@" + KnipVersion, "--include", "unlisted", "--reporter", "json"}
	if !reflect.DeepEqual(n.argv, wantArgv) {
		t.Errorf("argv = %v, want %v", n.argv, wantArgv)
	}
	if n.dir != filepath.Join(root, "web") {
		t.Errorf("dir = %q", n.dir)
	}
}

func TestNodeCheckerRootProjectFile(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"node_modules/.keep": ""})
	n := &nodeExec{stdout: `{"issues":[{"file":"src/a.js","unlisted":[{"name":"x","line":4}]}]}`, code: 1}
	got, err := NodeChecker{}.Check(Project{Lang: "node", Dir: "."}, nodeEnv(root, n, false))
	if err != nil || len(got) != 1 || got[0].File != "src/a.js" || got[0].Line != 4 {
		t.Errorf("findings=%+v err=%v, want src/a.js:4", got, err)
	}
}

func TestNodeCheckerUnchecked(t *testing.T) {
	tests := []struct {
		name       string
		noNpx      bool
		noModules  bool
		n          nodeExec
		want       string
		wantRunned int
	}{
		{"no node_modules", false, true, nodeExec{}, "node_modules missing in web", 0},
		{"no npx", true, false, nodeExec{}, "npx not found on PATH, needed in web", 0},
		{"unreadable json", false, false, nodeExec{stdout: "garbage", code: 1}, "cannot read the knip output of web", 1},
		{"empty output", false, false, nodeExec{code: 0}, "cannot read the knip output of web", 1},
		{"exit 2 with json", false, false, nodeExec{stdout: `{"issues":[]}`, code: 2}, "knip failed in web: exit 2", 1},
		{"does not start", false, false, nodeExec{err: errors.New("exec failed")}, "exec failed", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := nodeRoot(t)
			if tt.noModules {
				if err := os.RemoveAll(filepath.Join(root, "web", "node_modules")); err != nil {
					t.Fatal(err)
				}
			}
			n := tt.n
			got, err := NodeChecker{}.Check(Project{Lang: "node", Dir: "web"}, nodeEnv(root, &n, tt.noNpx))
			var u UncheckedError
			if !errors.As(err, &u) || !strings.Contains(err.Error(), tt.want) || got != nil {
				t.Fatalf("got %v, %v; want UncheckedError containing %q", got, err, tt.want)
			}
			if n.calls != tt.wantRunned {
				t.Errorf("exec calls = %d, want %d (nothing is installed or run without node_modules)", n.calls, tt.wantRunned)
			}
		})
	}
}

func TestNodeCheckerReadsJSONNotExitCode(t *testing.T) {
	root := nodeRoot(t)
	n := &nodeExec{stdout: `{"issues":[]}`, code: 1}
	got, err := NodeChecker{}.Check(Project{Lang: "node", Dir: "web"}, nodeEnv(root, n, false))
	if err != nil || len(got) != 0 {
		t.Errorf("exit 1 with valid JSON and no findings: %v %v, want clean", got, err)
	}
	n = &nodeExec{stdout: "garbage", code: 1}
	_, err = NodeChecker{}.Check(Project{Lang: "node", Dir: "web"}, nodeEnv(root, n, false))
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Errorf("exit 1 with garbage: %v, want UncheckedError", err)
	}
}
