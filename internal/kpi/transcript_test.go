package kpi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionFilesOrder(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "P")
	write(t, filepath.Join(p, "b.jsonl"), "")
	write(t, p+"--claude-worktrees-x/a.jsonl", "")
	write(t, p+"-ander/c.jsonl", "")
	write(t, filepath.Join(p, "skip.txt"), "")
	got, err := sessionFiles(p, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{p + "/b.jsonl", p + "--claude-worktrees-x/a.jsonl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with siblings = %v, want %v", got, want)
	}
	got, _ = sessionFiles(p, false)
	if !reflect.DeepEqual(got, want[:1]) {
		t.Errorf("without siblings = %v, want %v", got, want[:1])
	}
	if got, err := sessionFiles(filepath.Join(root, "gone"), true); err != nil || len(got) != 0 {
		t.Errorf("missing dir = %v, %v", got, err)
	}
}

func rec(typ string, extra map[string]any) map[string]any {
	r := map[string]any{"type": typ}
	for k, v := range extra {
		r[k] = v
	}
	return r
}

func msg(content any) map[string]any { return map[string]any{"content": content} }

func TestIterTurns(t *testing.T) {
	prompt := func(text string, extra map[string]any) map[string]any {
		r := rec("user", map[string]any{"message": msg(text)})
		for k, v := range extra {
			r[k] = v
		}
		return r
	}
	toolResult := rec("user", map[string]any{"message": msg([]any{map[string]any{"type": "tool_result"}})})
	asst := rec("assistant", nil)
	records := []map[string]any{
		asst, // before the first prompt: dropped
		prompt("one", nil),
		toolResult,
		prompt("side", map[string]any{"isSidechain": 1.0}),
		prompt("meta", map[string]any{"isMeta": "x"}),
		prompt("internal", map[string]any{"userType": "internal"}),
		prompt("   ", nil),
		prompt("two", map[string]any{"userType": "external"}),
		prompt("three", map[string]any{"userType": nil, "isMeta": false}),
	}
	turns := iterTurns(records)
	var prompts []string
	for _, tr := range turns {
		prompts = append(prompts, tr.Prompt)
	}
	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(prompts, want) {
		t.Fatalf("prompts = %v, want %v", prompts, want)
	}
	if len(turns[0].Records) != 5 {
		t.Errorf("turn one holds %d records, want 5", len(turns[0].Records))
	}
}

func TestReadSessionSkipsBrokenLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, path, `{"a":1}`+"\n\n{broken\n[1,2]\n"+`{"b":"x`+"\xff"+`y"}`+"\r\n"+`{"c":"p`+" "+`q"}`+"\u0085")
	got, err := readSession(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"a": 1.0}, {"b": "x�y"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("records = %#v, want %#v", got, want)
	}
	if _, err := readSession(path + ".none"); err == nil {
		t.Error("missing file: want an error")
	}
}

func TestAfter(t *testing.T) {
	since := "2026-08-24"
	for _, tt := range []struct {
		name  string
		rec   map[string]any
		since *string
		want  bool
	}{
		{"nil since", map[string]any{"timestamp": "2020-01-01"}, nil, true},
		{"no timestamp", map[string]any{}, &since, true},
		{"non-string timestamp", map[string]any{"timestamp": 5.0}, &since, true},
		{"before", map[string]any{"timestamp": "2026-08-23T23:59Z"}, &since, false},
		{"same day", map[string]any{"timestamp": "2026-08-24T00:00Z"}, &since, true},
	} {
		if got := after(tt.rec, tt.since); got != tt.want {
			t.Errorf("%s: after = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestToolUsesAndSkills(t *testing.T) {
	block := func(name string, in any) any {
		return map[string]any{"type": "tool_use", "name": name, "id": "i1", "input": in}
	}
	a := rec("assistant", map[string]any{"message": msg([]any{
		block("Skill", map[string]any{"skill": "s1"}),
		block("Bash", "oops"),
		map[string]any{"type": "text", "text": "x"},
		"junk",
	})})
	uses := toolUses(a)
	if len(uses) != 2 || uses[0].ID != "i1" || len(uses[1].Input) != 0 {
		t.Fatalf("toolUses = %#v", uses)
	}
	side := rec("assistant", map[string]any{"isSidechain": true, "message": a["message"]})
	if len(toolUses(side)) != 0 || len(toolUses(rec("user", map[string]any{"message": a["message"]}))) != 0 {
		t.Error("sidechain and non-assistant records have no tool uses")
	}
	if got := loadedSkills(Turn{Records: []map[string]any{a, side}}); !reflect.DeepEqual(got, []string{"s1"}) {
		t.Errorf("loadedSkills = %v", got)
	}
	if len(contentBlocks(rec("user", nil))) != 0 {
		t.Error("no message: no blocks")
	}
}

func TestTruthy(t *testing.T) {
	for v, want := range map[string]bool{"nil": false, "false": false, "zero": false, "empty": false, "list": true, "obj": true, "str": true, "num": true, "true": true} {
		vals := map[string]any{"nil": nil, "false": false, "zero": 0.0, "empty": "", "list": []any{1.0}, "obj": map[string]any{"a": 1.0}, "str": "x", "num": 2.0, "true": true}
		if got := truthy(vals[v]); got != want {
			t.Errorf("truthy(%s) = %v, want %v", v, got, want)
		}
	}
	if truthy([]any{}) || truthy(map[string]any{}) {
		t.Error("empty containers are falsy")
	}
}
