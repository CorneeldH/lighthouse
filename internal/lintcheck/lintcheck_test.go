package lintcheck

import (
	"bytes"
	"strings"
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
		{"findings only", map[Lang]Checker{"python": fakeChecker{findings: []Finding{finding}}}, []Lang{"python"}, 1, "a.py:3: ruff — rule E722 missing", ""},
		{"findings and unchecked", map[Lang]Checker{
			"python": fakeChecker{findings: []Finding{finding}},
			"go":     fakeChecker{err: UncheckedError("no golangci-lint")},
		}, []Lang{"python", "go"}, 1, "a.py:3: ruff — rule E722 missing", "lint check: COULD NOT CHECK — no golangci-lint"},
		{"unchecked only", map[Lang]Checker{"go": fakeChecker{err: UncheckedError("boom")}}, []Lang{"go"}, 3, "", "lint check: COULD NOT CHECK — boom"},
		{"clean", map[Lang]Checker{"python": fakeChecker{wired: []string{"ruff"}}}, []Lang{"python"}, 0, "ruff: wired", ""},
		{"no checker", nil, []Lang{"shell"}, 3, "", "lint check: COULD NOT CHECK"},
	}
	for _, c := range cases {
		rc, out, er := run(t, c.languages, c.cs)
		if rc != c.rc || !strings.Contains(out, c.out) || !strings.Contains(er, c.er) {
			t.Errorf("%s: rc=%d out=%q err=%q", c.name, rc, out, er)
		}
	}
}

func TestFormatFindingWithoutLine(t *testing.T) {
	if got := FormatFinding(Finding{File: ".github/workflows/ci.yml", Linter: "ruff", Msg: "step not recognised"}); got != ".github/workflows/ci.yml: ruff — step not recognised" {
		t.Fatal(got)
	}
}
