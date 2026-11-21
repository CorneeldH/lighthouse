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

const deptrySample = `[
 {"error":{"code":"DEP003","message":"m"},"module":"b","location":{"file":"src/m.py","line":1,"column":8}},
 {"error":{"code":"DEP001","message":"m"},"module":"numpy","location":{"file":"src/m.py","line":2,"column":8}},
 {"error":{"code":"DEP002","message":"m"},"module":"unused","location":{"file":"pyproject.toml","line":0,"column":0}},
 {"error":{"code":"DEP004","message":"m"},"module":"pytest","location":{"file":"src/t.py","line":3,"column":1}}
]`

func TestParseDeptry(t *testing.T) {
	got, err := parseDeptry([]byte(deptrySample))
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{File: "src/m.py", Line: 1, ID: "DEP003", Import: "b", Reason: deptryReasons["DEP003"]},
		{File: "src/m.py", Line: 2, ID: "DEP001", Import: "numpy", Reason: deptryReasons["DEP001"]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %+v, want %+v", got, want)
	}
	if _, err := parseDeptry([]byte("not json")); err == nil {
		t.Error("unreadable JSON must be an error")
	}
	if got, err := parseDeptry([]byte("[]")); err != nil || len(got) != 0 {
		t.Errorf("empty list: %v %v", got, err)
	}
}

type pyExec struct {
	calls   int
	argv    []string
	dir     string
	env     []string
	json    string // written to the --json-output path; "" writes nothing
	code    int
	err     error
	skipOut bool
}

func (p *pyExec) exec(dir string, argv []string, extraEnv []string) ([]byte, []byte, int, error) {
	p.calls++
	p.argv, p.dir, p.env = argv, dir, extraEnv
	if p.err != nil {
		return nil, nil, 0, p.err
	}
	for i, a := range argv {
		if a == "--json-output" && !p.skipOut {
			_ = os.WriteFile(argv[i+1], []byte(p.json), 0o644)
		}
	}
	return nil, []byte("stderr text"), p.code, nil
}

func pyEnv(root string, p *pyExec, stderr *bytes.Buffer) Env {
	return Env{
		Root:     root,
		Stderr:   stderr,
		Exec:     p.exec,
		LookPath: func(string) (string, error) { return "/bin/uv", nil },
	}
}

// pyRoot makes a repo with project svc, a src dir and an own .venv.
func pyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{"svc/src/m.py": "import b\n", "svc/.venv/pyvenv.cfg": ""})
	return root
}

func TestPythonCheckerFindingsAndInvocation(t *testing.T) {
	root := pyRoot(t)
	p := &pyExec{json: deptrySample, code: 1}
	c := PythonChecker{SourceDirs: []string{"src", "missing"}}
	got, err := c.Check(Project{Lang: "python", Dir: "svc"}, pyEnv(root, p, &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].File != "svc/src/m.py" || got[1].ID != "DEP001" {
		t.Fatalf("findings = %+v, want two, files mapped under svc", got)
	}
	wantPrefix := []string{"uv", "run", "--frozen", "--no-sync", "--with", "deptry==" + DeptryVersion, "deptry", "src", "--json-output"}
	if len(p.argv) != len(wantPrefix)+1 || !reflect.DeepEqual(p.argv[:len(wantPrefix)], wantPrefix) {
		t.Errorf("argv = %v, want %v and an output path (missing dir skipped)", p.argv, wantPrefix)
	}
	if p.dir != filepath.Join(root, "svc") {
		t.Errorf("dir = %q", p.dir)
	}
	venv := filepath.Join(root, "svc", ".venv")
	if !reflect.DeepEqual(p.env, []string{"VIRTUAL_ENV=" + venv, "UV_PROJECT_ENVIRONMENT=" + venv}) {
		t.Errorf("env = %v", p.env)
	}
}

func TestPythonCheckerCleanAndRootProject(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"src/m.py": "", ".venv/pyvenv.cfg": ""})
	p := &pyExec{json: "[]"}
	got, err := PythonChecker{SourceDirs: []string{"src"}}.Check(Project{Lang: "python", Dir: "."}, pyEnv(root, p, &bytes.Buffer{}))
	if err != nil || len(got) != 0 {
		t.Fatalf("findings=%v err=%v, want clean", got, err)
	}
	p = &pyExec{json: `[{"error":{"code":"DEP001"},"module":"x","location":{"file":"src/m.py","line":4}}]`, code: 1}
	got, _ = PythonChecker{SourceDirs: []string{"src"}}.Check(Project{Lang: "python", Dir: "."}, pyEnv(root, p, &bytes.Buffer{}))
	if len(got) != 1 || got[0].File != "src/m.py" {
		t.Errorf("root project file = %+v", got)
	}
}

func TestPythonCheckerUnchecked(t *testing.T) {
	tests := []struct {
		name    string
		noUV    bool
		noVenv  bool
		p       pyExec
		want    string
		wantRun int
	}{
		{"no uv", true, false, pyExec{}, "uv not found", 0},
		{"no venv", false, true, pyExec{}, "no .venv in svc or the main checkout; run uv sync first.", 0},
		{"unreadable json", false, false, pyExec{json: "garbage", code: 1}, "cannot read", 1},
		{"no json written", false, false, pyExec{skipOut: true, code: 2}, "exit 2", 1},
		{"deptry exit 2 with json", false, false, pyExec{json: "[]", code: 2}, "exit 2", 1},
		{"does not start", false, false, pyExec{err: errors.New("exec failed")}, "exec failed", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := pyRoot(t)
			if tt.noVenv {
				_ = os.RemoveAll(filepath.Join(root, "svc", ".venv"))
			}
			env := pyEnv(root, &tt.p, &bytes.Buffer{})
			if tt.noUV {
				env.LookPath = func(string) (string, error) { return "", errors.New("not found") }
			}
			got, err := PythonChecker{SourceDirs: []string{"src"}}.Check(Project{Lang: "python", Dir: "svc"}, env)
			var u UncheckedError
			if !errors.As(err, &u) {
				t.Fatalf("err = %v (findings %v), want UncheckedError", err, got)
			}
			if !strings.Contains(string(u), tt.want) || !strings.Contains(string(u), "svc") {
				t.Errorf("reason %q lacks %q or the project dir", u, tt.want)
			}
			if tt.p.calls != tt.wantRun {
				t.Errorf("exec calls = %d, want %d", tt.p.calls, tt.wantRun)
			}
		})
	}
}

func TestSourceDirsRules(t *testing.T) {
	root := pyRoot(t)
	project := Project{Lang: "python", Dir: "svc"}

	p := &pyExec{}
	var stderr bytes.Buffer
	got, err := PythonChecker{SourceDirs: []string{}}.Check(project, pyEnv(root, p, &stderr))
	if got != nil || err != nil || p.calls != 0 || stderr.Len() != 0 {
		t.Errorf("empty SourceDirs: findings=%v err=%v calls=%d stderr=%q, want silent and no run", got, err, p.calls, stderr.String())
	}

	p = &pyExec{}
	stderr.Reset()
	got, err = PythonChecker{SourceDirs: []string{"nope", "gone"}}.Check(project, pyEnv(root, p, &stderr))
	if got != nil || err != nil || p.calls != 0 {
		t.Errorf("no existing dir: findings=%v err=%v calls=%d, want none and no run", got, err, p.calls)
	}
	if !strings.Contains(stderr.String(), "not checked") || !strings.Contains(stderr.String(), "svc") {
		t.Errorf("stderr %q must say not checked for svc", stderr.String())
	}
}

func TestFindVenvFallsBackToMainCheckout(t *testing.T) {
	proj, main := t.TempDir(), t.TempDir()
	if v, ok := findVenv(proj, main); ok {
		t.Errorf("no venv anywhere: got %q", v)
	}
	writeTree(t, main, map[string]string{".venv/pyvenv.cfg": ""})
	if v, ok := findVenv(proj, main); !ok || v != filepath.Join(main, ".venv") {
		t.Errorf("fallback = %q %v", v, ok)
	}
	if v, ok := findVenv(proj, ""); ok {
		t.Errorf("without MainCheckout only the own venv counts, got %q", v)
	}
	writeTree(t, proj, map[string]string{".venv/pyvenv.cfg": ""})
	if v, ok := findVenv(proj, main); !ok || v != filepath.Join(proj, ".venv") {
		t.Errorf("own venv must win, got %q %v", v, ok)
	}
}
