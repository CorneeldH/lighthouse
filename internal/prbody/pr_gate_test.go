//go:build !windows

package prbody

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gateNoIssue   = "No issue: a valid reason here"
	gateRefs      = "Refs #5 — a valid reason here"
	gateRefsShort = "Refs #5 — short"
	gateTable     = "\n\n### Evidence #5\n\n| AC | Command | Result |\n|---|---|---|\n| 1. one | `grep -F a f` | a |\n"
	gateClosesBad = "Closes #5"
)

// acCountOne is an acCount stub that gives one AC for every issue.
func acCountOne(int) (int, bool, string) { return 1, true, "" }

func TestCheckBodyPRGate(t *testing.T) {
	tests := []struct {
		name         string
		gate         string
		body         string
		wantBlockers int
		wantContains []string
		wantAbsent   []string
	}{
		{"optional allows No issue", "optional", gateNoIssue, 0, nil, nil},
		{"empty allows No issue", "", gateNoIssue, 0, nil, nil},
		{"optional allows Refs without table", "optional", gateRefs, 0, nil, nil},
		{"empty allows Refs without table", "", gateRefs, 0, nil, nil},
		{"optional blocks Closes without Evidence", "optional", gateClosesBad, 1, []string{"Closes #5"}, nil},

		{"required blocks No issue", "required", gateNoIssue, 1,
			[]string{"pr_gate.issue = required", "`No issue: <reason>` is not allowed"}, nil},
		{"required allows Refs without table", "required", gateRefs, 0, nil, nil},
		{"required: a too short Refs reason is a form error", "required", gateRefsShort, 1,
			[]string{"pr_gate.issue = required:"}, []string{"No issue: <reason>"}},
		{"required allows No issue with a valid Refs", "required", gateNoIssue + "\n\n" + gateRefs, 0, nil, nil},
		{"required allows Closes with Evidence", "required", "Closes #5" + gateTable, 0, nil, nil},
		{"required blocks Closes without Evidence", "required", gateClosesBad, 1, []string{"Closes #5"}, nil},

		{"evidence blocks Refs without table", "evidence", gateRefs, 1,
			[]string{"pr_gate.issue = evidence", "### Evidence #5"}, nil},
		{"evidence allows Refs with table", "evidence", gateRefs + gateTable, 0, nil, nil},
		{"evidence blocks No issue", "evidence", gateNoIssue, 1,
			[]string{"pr_gate.issue = evidence", "`Refs #N — <reason>` with its `### Evidence #N` table"}, nil},
		{"evidence: a body with no valid form lists the combined Refs form", "evidence", gateRefsShort, 1,
			[]string{"pr_gate.issue = evidence", "`Refs #N — <reason>` with its `### Evidence #N` table"}, []string{"No issue"}},
		{"evidence: an empty body lists the combined Refs form", "evidence", "", 1,
			[]string{"pr_gate.issue = evidence", "`Refs #N — <reason>` with its `### Evidence #N` table"}, []string{"No issue"}},
		{"evidence allows Closes with Evidence", "evidence", "Closes #5" + gateTable, 0, nil, nil},
		{"evidence blocks Closes without Evidence", "evidence", gateClosesBad, 1, []string{"Closes #5"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := Repo{Kind: "github", Slug: "o/r", PRGate: tt.gate}
			v := CheckBody(tt.body, repo, acCountOne)
			if len(v.Blockers) != tt.wantBlockers {
				t.Fatalf("blockers = %q, want %d", v.Blockers, tt.wantBlockers)
			}
			all := strings.Join(v.Blockers, "\n")
			for _, want := range tt.wantContains {
				if !strings.Contains(all, want) {
					t.Errorf("blockers %q lack %q", all, want)
				}
			}
			for _, bad := range tt.wantAbsent {
				if strings.Contains(all, bad) {
					t.Errorf("blockers %q contain %q", all, bad)
				}
			}
		})
	}
}

// writeGateConfig is a lighthouse.toml with forge.repo and the [pr_gate] issue value (none for "").
func writeGateConfig(t *testing.T, issue string) {
	t.Helper()
	dir := t.TempDir()
	writeFixtureConfig(t, dir, "Acme/widgets")
	if issue == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "lighthouse.toml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString("\n[pr_gate]\nissue = \"" + issue + "\"\n"); err != nil {
		t.Fatal(err)
	}
}

func TestMainPRGate(t *testing.T) {
	cases := []struct {
		name       string
		gate       string
		body       string
		wantRC     int
		wantStderr []string
		absent     []string
	}{
		{"required blocks No issue", "required", gateNoIssue, 2,
			[]string{"pr_gate.issue = required", "`Refs #N — <reason>`"}, []string{"No issue"}},
		{"evidence blocks No issue", "evidence", gateNoIssue, 2,
			[]string{"pr_gate.issue = evidence", "`Refs #N — <reason>` with its `### Evidence #N` table"}, []string{"No issue"}},
		{"evidence blocks Refs without table", "evidence", gateRefs, 2,
			[]string{"pr_gate.issue = evidence"}, nil},
		{"evidence allows Refs with table", "evidence", gateRefs + gateTable, 0, nil, nil},
		{"optional allows No issue", "", gateNoIssue, 0, nil, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			writeGateConfig(t, tt.gate)
			_, project := newProject(t)
			writeFile(t, filepath.Join(project, "body.md"), tt.body)
			got := runMain(t, project, "gh pr create --repo Acme/widgets --body-file body.md", project)
			if got.rc != tt.wantRC {
				t.Fatalf("rc = %d, want %d (stderr %q)", got.rc, tt.wantRC, got.stderr)
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(got.stderr, want) {
					t.Errorf("stderr %q lacks %q", got.stderr, want)
				}
			}
			_, footer, _ := strings.Cut(got.stderr, "Forms:")
			for _, bad := range tt.absent {
				if strings.Contains(footer, bad) {
					t.Errorf("footer %q contains %q", footer, bad)
				}
			}
		})
	}
}
