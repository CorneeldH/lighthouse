package kpi

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runFixPR(opts Options) (int, string, string) {
	var out, errOut bytes.Buffer
	rc := RunFixPR(opts, &out, &errOut)
	return rc, out.String(), errOut.String()
}

// Classification: fails when fix-pr starts reading the worktree sibling directories, which
// only start-issue does.
func TestFixPRReadsNoWorktreeSiblings(t *testing.T) {
	parent := t.TempDir()
	line := `{"type":"user","message":{"content":"fix 610"}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/x"}}]}}` + "\n"
	for _, d := range []string{"main", "main--claude-worktrees-x"} {
		if err := os.Mkdir(filepath.Join(parent, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(parent, "main--claude-worktrees-x", "s.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := runFixPR(Options{Metric: "leadin", Dir: filepath.Join(parent, "main")})
	want := "{\n  \"leadin\": {\n    \"n\": 0,\n    \"without_mutation\": 0\n  }\n}\n"
	if rc != 0 || out != want {
		t.Fatalf("rc %d stderr %q\n%s\nwant\n%s", rc, errOut, out, want)
	}
}

// Classification: fails when the metric check, the directory check or the row order of the
// fix-pr table breaks.
func TestFixPRTableAndErrors(t *testing.T) {
	rc, out, _ := runFixPR(Options{Dir: t.TempDir()})
	if rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	last := -1
	for _, name := range FixPRMetrics {
		i := strings.Index(out, fmt.Sprintf("%q: {", name))
		if i <= last {
			t.Errorf("row %q out of order in\n%s", name, out)
		}
		last = i
	}
	rc, _, errOut := runFixPR(Options{Metric: "paths", Dir: t.TempDir()})
	if rc != 2 || errOut != "measure: unknown metric \"paths\" (choose from leadin, diagnose, mistrigger, scope)\n" {
		t.Errorf("rc %d stderr %q", rc, errOut)
	}
	rc, _, errOut = runFixPR(Options{Dir: "./gone/"})
	if rc != 2 || errOut != "transcript directory does not exist: gone\n" {
		t.Errorf("rc %d stderr %q", rc, errOut)
	}
}

// Classification: fails when the scout budget in code and the persona drift apart.
func TestScoutBudgetMatchesPersona(t *testing.T) {
	data, err := os.ReadFile("../../pack/core/agents/issue-impact-scout.md")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("at most %d searches", ScoutToolCallBudget)
	if !strings.Contains(strings.ToLower(string(data)), want) {
		t.Errorf("persona does not say %q", want)
	}
}

// Classification: fails when the persona is renamed and the Agent calls stop being counted as scouts.
func TestScoutNameMatchesPersona(t *testing.T) {
	data, err := os.ReadFile("../../pack/core/agents/issue-impact-scout.md")
	if err != nil {
		t.Fatal(err)
	}
	front, _, _ := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---")
	if !strings.Contains("\n"+front+"\n", "\nname: "+scoutAgent+"\n") {
		t.Errorf("persona frontmatter does not say name: %s", scoutAgent)
	}
}

// Classification: fails when the diagnosis block of the skill gains a mutating command, or
// the block is moved out of its section.
func TestFixPRDiagnosisBlockIsReadOnly(t *testing.T) {
	for _, variant := range []string{"claude", "agents"} {
		t.Run(variant, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../pack/core/skills/fix-pr", variant, "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			_, section, ok := strings.Cut(text, "## 1. Diagnosis in one block")
			if !ok {
				t.Fatal("no diagnosis section")
			}
			section, _, _ = strings.Cut(section, "\n## ")
			blocks := strings.Split(section, "```bash\n")
			if len(blocks) != 2 {
				t.Fatalf("%d bash blocks in the diagnosis section, want 1", len(blocks)-1)
			}
			block, _, _ := strings.Cut(blocks[1], "```")
			if m := mutatingRe.FindString(block); m != "" {
				t.Errorf("diagnosis block mutates: %q", m)
			}
		})
	}
}
