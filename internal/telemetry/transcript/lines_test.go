package transcript

import (
	"fmt"
	"testing"
)

func toolUse(id, name, path string) string {
	return fmt.Sprintf(`[{"type":"tool_use","id":%q,"name":%q,"input":{"file_path":%q}}]`, id, name, path)
}

func commandArgs(name, args string) string {
	return fmt.Sprintf("%q", "<command-name>/"+name+"</command-name>\n<command-args>"+args+"</command-args>")
}

func issuesOf(r Reading) []int {
	var out []int
	for _, l := range r.Lines() {
		out = append(out, l.Issue)
	}
	return out
}

func TestLinesIssueFromArguments(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", `"gitBranch":"issue-433-x",`, skillText("/x/superpowers/1.0/skills/foo", "body", "433")),
	))
	ls := r.Lines()
	if len(ls) != 1 || ls[0].Issue != 433 || ls[0].Branch != "issue-433-x" {
		t.Fatalf("Lines = %+v, want issue 433 on issue-433-x", ls)
	}
}

func TestLinesIssueFromCommandArgs(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", commandArgs("lighthouse-start-issue", "433 extra")),
	))
	if got := issuesOf(r); len(got) != 1 || got[0] != 433 {
		t.Fatalf("issues = %v, want [433]", got)
	}
}

func TestLinesIgnoreFreeTextHash(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", `"please look at #433 now"`),
	))
	if got := issuesOf(r); len(got) != 1 || got[0] != 0 {
		t.Fatalf("issues = %v, want [0]", got)
	}
}

func TestLinesIssueNotAtStartIgnored(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("user", "2026-01-01T10:00:00Z", "", skillText("/x/skills/foo", "body", "see 433")),
		line("user", "2026-01-01T10:01:00Z", "", commandArgs("foo", "see 433")),
	))
	for _, n := range issuesOf(r) {
		if n != 0 {
			t.Fatalf("issues = %v, want all 0", issuesOf(r))
		}
	}
}

func TestQuestionsPairToolResult(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("assistant", "2026-01-01T10:00:00Z", "", toolUse("q1", "AskUserQuestion", "")),
		line("user", "2026-01-01T10:05:00Z", "", `[{"type":"tool_result","tool_use_id":"q1","content":"yes"}]`),
	))
	qs := r.Questions()
	if len(qs) != 1 || qs[0].ToolUseID != "q1" || !qs[0].Asked.Equal(ts("2026-01-01T10:00:00Z")) ||
		qs[0].Answered == nil || !qs[0].Answered.Equal(ts("2026-01-01T10:05:00Z")) {
		t.Fatalf("Questions = %+v", qs)
	}
}

func TestQuestionUnanswered(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("assistant", "2026-01-01T10:00:00Z", "", toolUse("q1", "AskUserQuestion", "")),
		line("user", "2026-01-01T10:05:00Z", "", `[{"type":"tool_result","tool_use_id":"other","content":"x"}]`),
	))
	qs := r.Questions()
	if len(qs) != 1 || qs[0].Answered != nil {
		t.Fatalf("Questions = %+v, want one unanswered", qs)
	}
}

func TestWrittenFilesChronological(t *testing.T) {
	r := mustParse(t, f("s.jsonl",
		line("assistant", "2026-01-01T10:02:00Z", "", toolUse("t2", "Edit", "/b")),
		line("assistant", "2026-01-01T10:00:00Z", "", toolUse("t1", "Write", "/a")),
		line("assistant", "2026-01-01T10:01:00Z", "", toolUse("t3", "Read", "/c")),
		line("assistant", "2026-01-01T10:03:00Z", "", toolUse("t4", "Write", "/a")),
	))
	want := []WrittenFile{
		{"/a", ts("2026-01-01T10:00:00Z")},
		{"/b", ts("2026-01-01T10:02:00Z")},
		{"/a", ts("2026-01-01T10:03:00Z")},
	}
	got := r.WrittenFiles()
	if len(got) != len(want) {
		t.Fatalf("WrittenFiles = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Path != want[i].Path || !got[i].At.Equal(want[i].At) {
			t.Fatalf("WrittenFiles = %+v, want %+v", got, want)
		}
	}
}
