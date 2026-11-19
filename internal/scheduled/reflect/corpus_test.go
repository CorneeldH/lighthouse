package reflect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Classification: pure logic plus files. These tests fail if system text reaches the corpus as a
// user prompt, one bad line costs the rest of a session, the corpus outgrows its budget, or one
// kind of session pushes the other out.

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func userLine(text string) string {
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%q}}`, text)
}

func TestExtractKeepsRealPromptsOnly(t *testing.T) {
	p := writeLines(t,
		userLine("fix the login bug"),
		userLine("<command-name>/clear</command-name>"),
		userLine("<system-reminder>hi</system-reminder>"),
		userLine("[Request interrupted by user]"),
		userLine("This session is being continued from a previous conversation"),
		userLine("Analysis: the thing"),
		userLine("**bold** system"),
		userLine("   "),
		`{"type":"user","isMeta":true,"message":{"content":"meta text"}}`,
		`{"type":"assistant","message":{"content":"not a user line"}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"block prompt"},{"type":"tool_result","content":"x"}]}}`,
	)
	got := extractPrompts(p, 400, 1<<20)
	if len(got) != 2 || got[0] != "fix the login bug" || got[1] != "block prompt" {
		t.Errorf("prompts = %q, want the two real ones", got)
	}
}

func TestExtractSurvivesACorruptLineAndAHugeLine(t *testing.T) {
	huge := userLine(strings.Repeat("x", 3000))
	p := writeLines(t, userLine("before"), `{"type":"user","message":{"content":"cut o`, "\xff\xfe not json", huge, userLine("after"))
	got := extractPrompts(p, 400, 2000) // a line of 3000 bytes is above the budget of 2000
	if len(got) != 2 || got[0] != "before" || got[1] != "after" {
		t.Errorf("prompts = %q, want before and after: the bad line and the huge line are skipped", got)
	}
}

func TestExtractTruncatesToMaxPromptRunes(t *testing.T) {
	got := extractPrompts(writeLines(t, userLine(strings.Repeat("é", 50))), 10, 1<<20)
	if len(got) != 1 || len([]rune(got[0])) != 10 {
		t.Errorf("prompt = %q, want 10 runes", got)
	}
}

func sessions(kind string, n int, prompt string) []corpusSession {
	var out []corpusSession // newest first, as buildCorpus takes them
	for i := n - 1; i >= 0; i-- {
		out = append(out, corpusSession{Date: fmt.Sprintf("2026-11-%02d", i+1), ID: fmt.Sprintf("%s%04d", kind, i), Prompts: []string{fmt.Sprintf("%s %d %s", kind, i, prompt)}})
	}
	return out
}

func TestCorpusHasTwoSectionsAndStaysInBudget(t *testing.T) {
	inter := sessions("i", 40, strings.Repeat("p", 200))
	ctrl := sessions("c", 40, strings.Repeat("q", 200))
	for _, max := range []int{3000, 6000, 20000} {
		b := buildCorpus(inter, ctrl, max)
		if len(b) > max {
			t.Errorf("max %d: corpus is %d bytes", max, len(b))
		}
		s := string(b)
		ii, ci := strings.Index(s, "## Interactief"), strings.Index(s, "## Controller")
		if ii < 0 || ci < ii {
			t.Fatalf("max %d: sections missing or out of order:\n%s", max, s[:min(len(s), 200)])
		}
		// Each side gets about half: neither crowds the other out.
		if !strings.Contains(s[ii:ci], "i 39 ") || !strings.Contains(s[ci:], "c 39 ") {
			t.Errorf("max %d: the newest session of one kind is missing", max)
		}
	}
}

func TestCorpusGivesUnusedBudgetToTheOtherKind(t *testing.T) {
	inter := sessions("i", 2, "short")
	ctrl := sessions("c", 60, strings.Repeat("q", 200))
	s := string(buildCorpus(inter, ctrl, 9000))
	if strings.Count(s, "- c ") <= 15 { // half of 9000 would hold about 15 of them
		t.Errorf("controller got %d prompts, want more than its half: interactive used little", strings.Count(s, "- c "))
	}
	s = string(buildCorpus(nil, ctrl, 9000))
	if !strings.Contains(s, "## Interactief") || !strings.Contains(s, "## Controller") {
		t.Error("an empty section must still be there")
	}
}

func TestCorpusMergesIdenticalControllerPromptsWithACount(t *testing.T) {
	var ctrl []corpusSession
	for i := range 5 {
		ctrl = append(ctrl, corpusSession{Date: "2026-11-01", ID: fmt.Sprintf("c%d", i), Prompts: []string{"/lighthouse-start-issue 1", fmt.Sprintf("unique %d", i)}})
	}
	inter := []corpusSession{{Date: "2026-11-01", ID: "i1", Prompts: []string{"/lighthouse-start-issue 1", "/lighthouse-start-issue 1"}}}
	s := string(buildCorpus(inter, ctrl, 100000))
	c := s[strings.Index(s, "## Controller"):]
	if strings.Count(c, "/lighthouse-start-issue 1") != 1 || !strings.Contains(c, "/lighthouse-start-issue 1 ×5") {
		t.Errorf("controller section:\n%s\nwant the repeated prompt once with ×5", c)
	}
	if !strings.Contains(c, "unique 4") {
		t.Error("a unique prompt must stay under its session")
	}
	if i := s[:strings.Index(s, "## Controller")]; strings.Count(i, "/lighthouse-start-issue 1") != 2 || strings.Contains(i, "×") {
		t.Errorf("interactive section is not merged:\n%s", i)
	}
}
