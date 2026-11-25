package transcripts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBelongsToRepo_EmptySlugMatchesNothing(t *testing.T) {
	roots := []string{"/r"}
	for _, segs := range [][]string{nil, {""}, {"o", ""}} {
		if BelongsToRepo("/elders/x", roots, segs, Options{}) {
			t.Errorf("segs %q: foreign cwd matched", segs)
		}
		if !BelongsToRepo("/r/sub", roots, segs, Options{}) {
			t.Errorf("segs %q: cwd under root did not match", segs)
		}
	}
}

func TestBelongsToRepo_FoldCase(t *testing.T) {
	roots := []string{`D:\Work\repo`}
	cwd := `d:\work\repo\wt`
	if !BelongsToRepo(cwd, roots, nil, Options{FoldCase: true}) {
		t.Error("FoldCase: want match")
	}
	if BelongsToRepo(cwd, roots, nil, Options{}) {
		t.Error("no FoldCase: want no match")
	}
}

func TestBelongsToRepo_WindowsCwdOnLinux(t *testing.T) {
	fold := Options{FoldCase: true}
	if !BelongsToRepo(`C:\Users\u\o\r`, nil, []string{"o", "r"}, fold) {
		t.Error("slug rule on a Windows cwd: want match")
	}
	if !BelongsToRepo(`C:\Users\u\repo\x`, []string{"/mnt/c/Users/u/repo"}, nil, fold) {
		t.Error("root rule on a Windows cwd: want match")
	}
	if BelongsToRepo(`C:\Users\u\other\x`, []string{"/mnt/c/Users/u/repo"}, []string{"o", "r"}, fold) {
		t.Error("foreign Windows cwd matched")
	}
}

func TestFind_SubagentsBelongToParent(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-r")
	if err := os.MkdirAll(filepath.Join(dir, "abc", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "abc.jsonl"), `{"cwd":"/r/wt"}`+"\n")
	write(filepath.Join(dir, "abc", "subagents", "a.jsonl"), "{}\n")
	got, err := Find(projects, []string{"/r"}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "abc" || len(got[0].Subs) != 1 {
		t.Fatalf("got %+v, want one session abc with one sub", got)
	}
}
