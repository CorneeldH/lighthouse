package history

import "testing"

func writeRec(sess, path string) Record {
	return Record{File: "f.jsonl", Line: 2, Type: "assistant", SessionID: sess, WrittenPaths: []string{path}}
}

func TestWorkTracking_Commands(t *testing.T) {
	cases := []struct {
		cmd         string
		prs, issues int
	}{
		{"gh issue list", 0, 1},
		{"gh pr create --title x", 1, 0},
		{"gh pr view 5", 0, 0},
		{"lighthouse forge pr create", 1, 0},
		{"lighthouse forge issue view 3", 0, 1},
		{"glab mr create", 1, 0},
		{"glab issue list", 0, 1},
		{"git status", 0, 0},
	}
	for _, c := range cases {
		d := newWorkTracking()
		d.Add(bashRec("s", "b", "/r", c.cmd))
		if d.PRs() != c.prs || d.IssueCommands() != c.issues {
			t.Errorf("%q: prs %d issues %d", c.cmd, d.PRs(), d.IssueCommands())
		}
	}
}

func TestWorkTracking_PRCounting(t *testing.T) {
	d := newWorkTracking()
	d.Add(Record{SessionID: "s", Type: "pr-link", PRNumber: 7})
	d.Add(Record{SessionID: "t", Type: "pr-link", PRNumber: 7})
	if d.PRs() != 1 {
		t.Fatalf("same prNumber: %d", d.PRs())
	}
	d = newWorkTracking()
	d.Add(bashRec("s", "b", "/r", "gh pr create"))
	d.Add(Record{SessionID: "s", Type: "pr-link", PRNumber: 8})
	if d.PRs() != 1 {
		t.Fatalf("create then pr-link: %d", d.PRs())
	}
	d = newWorkTracking()
	d.Add(Record{SessionID: "s", Type: "pr-link", PRNumber: 8})
	d.Add(bashRec("s", "b", "/r", "gh pr create"))
	d.Add(bashRec("u", "b", "/r", "glab mr create"))
	if d.PRs() != 2 {
		t.Fatalf("link before create, other session create: %d", d.PRs())
	}
}

func TestWorkTracking_Finish(t *testing.T) {
	for _, p := range []string{"/r/TODO.md", "/r/docs/tasks/x.md", "/r/a.sqlite", "/r/b.db", "/r/memory/x.md", `C:\r\docs\tasks\x.md`} {
		d := newWorkTracking()
		d.Add(writeRec("s", p))
		f := d.Finish(Env{})
		if f.Key != "work_tracking" || f.Status != "found" || f.Measured["file_tasks"] != 1 || f.Proposal.Kind != "advice" || f.Proposal.Flag != "" {
			t.Errorf("%s: %+v", p, f)
		}
		if len(f.Sources) != 1 {
			t.Errorf("%s: sources %+v", p, f.Sources)
		}
	}
	d := newWorkTracking()
	d.Add(writeRec("s", "/r/main.go"))
	d.Add(writeRec("s", "/r/notes/todo.txt"))
	if f := d.Finish(Env{}); f.Status != "none" || f.Measured["file_tasks"] != 0 {
		t.Fatalf("%+v", f)
	}
	d = newWorkTracking()
	d.Add(bashRec("s", "b", "/r", "gh issue list"))
	d.Add(bashRec("s", "b", "/r", "gh pr create"))
	d.Add(writeRec("s", "/r/TODO.md"))
	f := d.Finish(Env{})
	if f.Status != "found" || f.Proposal.Kind != "knob" || f.Proposal.Flag != "--labels" {
		t.Fatalf("%+v", f)
	}
	if f.Measured["issue_commands"] != 1 || f.Measured["prs"] != 1 || f.Measured["file_tasks"] != 1 {
		t.Fatalf("%v", f.Measured)
	}
}
