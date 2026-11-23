package lintcheck

import (
	"bytes"
	"testing"
)

type fakeChecker struct {
	findings []Finding
	wired    []string
	err      error
}

func (f fakeChecker) Check(string, Env) ([]Finding, []string, error) {
	return f.findings, f.wired, f.err
}

func run(t *testing.T, languages []Lang, cs map[Lang]Checker) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := writeRepo(t, map[string]string{"x": ""})
	rc := Run(Options{Root: root, Langs: languages, Checkers: cs}, Env{Stdout: &out, Stderr: &errOut})
	return rc, out.String(), errOut.String()
}

func TestRunExitCodes(t *testing.T) {
	finding := Finding{File: "a.py", Line: 3, Linter: "ruff", Msg: "rule E722 missing"}
	cases := []struct {
		name      string
		cs        map[Lang]Checker
		languages []Lang
		rc        int
		out, er   string
	}{
		{"findings only", map[Lang]Checker{"python": fakeChecker{findings: []Finding{finding}}}, []Lang{"python"}, 1, "a.py:3: ruff — rule E722 missing\n", ""},
		{"findings and unchecked", map[Lang]Checker{
			"python": fakeChecker{findings: []Finding{finding}},
			"go":     fakeChecker{err: UncheckedError("no golangci-lint")},
		}, []Lang{"python", "go"}, 1, "a.py:3: ruff — rule E722 missing\n", "lint check: COULD NOT CHECK — no golangci-lint\n"},
		{"unchecked only", map[Lang]Checker{"go": fakeChecker{err: UncheckedError("boom")}}, []Lang{"go"}, 3, "", "lint check: COULD NOT CHECK — boom\n"},
		{"clean", map[Lang]Checker{"python": fakeChecker{wired: []string{"ruff"}}}, []Lang{"python"}, 0, "ruff: wired\n", ""},
		{"no checker", nil, []Lang{"shell"}, 3, "", "lint check: COULD NOT CHECK — no checker for shell (.)\n"},
	}
	for _, c := range cases {
		rc, out, er := run(t, c.languages, c.cs)
		if rc != c.rc || out != c.out || er != c.er {
			t.Errorf("%s: rc=%d out=%q err=%q, want rc=%d out=%q err=%q", c.name, rc, out, er, c.rc, c.out, c.er)
		}
	}
}

// dirChecker answers per directory.
type dirChecker map[string]fakeChecker

func (d dirChecker) Check(dir string, env Env) ([]Finding, []string, error) {
	return d[dir].Check(dir, env)
}

// Classification: cli-output. Fails when a run over several Node roots prints `eslint: wired`
// without naming the root, or prints it for a root that also has a finding for eslint.
func TestRunMultiRootNamesTheDir(t *testing.T) {
	var out, errOut bytes.Buffer
	root := writeRepo(t, map[string]string{"x": ""})
	miss := Finding{File: ".github/workflows", Linter: "eslint", Msg: "no CI step runs eslint in app"}
	cs := map[Lang]Checker{"node": dirChecker{
		"app": {findings: []Finding{miss}, wired: []string{"eslint"}},
		"web": {wired: []string{"eslint"}},
	}}
	rc := Run(Options{Root: root, Langs: []Lang{"node"}, Dirs: []string{"app", "web"}, Checkers: cs}, Env{Stdout: &out, Stderr: &errOut})
	want := ".github/workflows: eslint — no CI step runs eslint in app\neslint: wired (web)\n"
	if rc != 1 || out.String() != want || errOut.Len() != 0 {
		t.Fatalf("rc=%d out=%q err=%q, want out %q", rc, out.String(), errOut.String(), want)
	}
}

func TestFormatFindingWithoutLine(t *testing.T) {
	if got := FormatFinding(Finding{File: ".github/workflows/ci.yml", Linter: "ruff", Msg: "step not recognised"}); got != ".github/workflows/ci.yml: ruff — step not recognised" {
		t.Fatal(got)
	}
}
