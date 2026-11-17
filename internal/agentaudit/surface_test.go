package agentaudit

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sk(source, name, kind string, nameChars, descChars int) Skill {
	return Skill{Source: source, Name: name, Kind: kind, Path: source + "/" + name, Harness: "claude", NameChars: nameChars, DescChars: descChars}
}

func TestMeasurePerSource(t *testing.T) {
	inv := &Inventory{Skills: []Skill{
		sk("project", "a", "skill", 5, 95),    // 100 chars
		sk("lighthouse", "b", "skill", 5, 96), // 101 chars
		sk("lighthouse", "c", "agent", 4, 0),  // + 4 = 105
		sk("gebruiker", "d", "skill", 5, 95),  // 100 chars, same tokens as project
	}}
	got := Measure(inv, 10, 1024).PerSource
	want := []SourceSurface{
		{Source: "lighthouse", Count: 2, Chars: 105, Tokens: 27},
		{Source: "gebruiker", Count: 1, Chars: 100, Tokens: 25},
		{Source: "project", Count: 1, Chars: 100, Tokens: 25},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PerSource\n got %+v\nwant %+v", got, want)
	}
}

func TestMeasureTokensRoundUp(t *testing.T) {
	for chars, want := range map[int]int{0: 0, 1: 1, 4: 1, 100: 25, 101: 26} {
		got := Measure(&Inventory{Skills: []Skill{sk("p", "a", "skill", 0, chars)}}, 1, 1024).PerSource
		if got[0].Tokens != want {
			t.Errorf("%d chars: want %d tokens, got %d", chars, want, got[0].Tokens)
		}
	}
}

func TestMeasureTop(t *testing.T) {
	inv := &Inventory{Skills: []Skill{
		sk("p", "small", "skill", 5, 10),
		sk("p", "big", "skill", 5, 500),
		sk("q", "mid", "agent", 5, 100),
	}}
	got := Measure(inv, 2, 1024).Top
	if len(got) != 2 || got[0].Name != "big" || got[1].Name != "mid" {
		t.Errorf("Top = %+v", got)
	}
	if n := len(Measure(inv, 0, 1024).Top); n != 0 {
		t.Errorf("top 0 gave %d", n)
	}
	if n := len(Measure(inv, 99, 1024).Top); n != 3 {
		t.Errorf("top 99 gave %d, want 3", n)
	}
}

func TestMeasureTooLong(t *testing.T) {
	inv := &Inventory{Skills: []Skill{
		sk("p", "exact", "skill", 5, 1024),
		sk("p", "over", "skill", 4, 1025),
	}}
	got := Measure(inv, 5, 1024).TooLong
	if len(got) != 1 || got[0].Name != "over" {
		t.Errorf("TooLong = %+v", got)
	}
}

func TestMeasureCommandsAndAgentsCount(t *testing.T) {
	inv := &Inventory{Skills: []Skill{
		sk("p", "s", "skill", 1, 1), sk("p", "c", "command", 1, 1), sk("p", "a", "agent", 1, 1),
	}}
	if got := Measure(inv, 5, 1024).PerSource[0].Count; got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
}

func TestMeasureCodexSkillsNotCounted(t *testing.T) {
	codex := sk("project", "x", "skill", 5, 2000)
	codex.Harness = "codex"
	inv := &Inventory{Skills: []Skill{sk("project", "a", "skill", 5, 5), codex}}
	s := Measure(inv, 5, 1024)
	if len(s.PerSource) != 1 || s.PerSource[0].Count != 1 || s.PerSource[0].Chars != 10 {
		t.Errorf("PerSource = %+v", s.PerSource)
	}
	if len(s.Top) != 1 || len(s.TooLong) != 0 {
		t.Errorf("Top %+v, TooLong %+v", s.Top, s.TooLong)
	}
	if got := Measure(&Inventory{Skills: []Skill{codex}}, 5, 1024).PerSource; len(got) != 0 {
		t.Errorf("only a codex skill: PerSource = %+v", got)
	}
}

func TestMeasureCollectedTree(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/skills/a/SKILL.md": "---\nname: aa\ndescription: " + strings.Repeat("x", 10) + "\n---\n",
		".agents/skills/b/SKILL.md": "---\nname: bb\ndescription: " + strings.Repeat("y", 2000) + "\n---\n",
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	s := Measure(inv, 5, 1024)
	if len(s.PerSource) != 1 || s.PerSource[0].Count != 1 || s.PerSource[0].Chars != 12 || len(s.TooLong) != 0 {
		t.Errorf("surface = %+v", s)
	}
}

func TestMeasureNoFrontmatter(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/skills/bare/SKILL.md":  "# just text, no frontmatter\n",
		".claude/skills/empty/SKILL.md": "---\nname: emp\ndescription:\n---\n",
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	s := Measure(inv, 5, 1024)
	if len(s.PerSource) != 1 || s.PerSource[0].Count != 2 {
		t.Fatalf("surface = %+v", s)
	}
	// "bare" falls back to the folder name, "emp" is its frontmatter name; both 0 desc chars.
	if s.PerSource[0].Chars != len("bare")+len("emp") {
		t.Errorf("Chars = %d, want %d", s.PerSource[0].Chars, len("bare")+len("emp"))
	}
}
