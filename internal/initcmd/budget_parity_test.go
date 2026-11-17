package initcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/checkinstr"
	"github.com/CorneeldH/lighthouse/internal/wordcount"
)

// Red when the init count and check 10 stop counting the same files with the same counter,
// for example when wordcount.Count falls back to strings.Fields.
func TestBudgetMatchesCheck10(t *testing.T) {
	claude := []byte("@AGENTS.md\nClaude rest — met\ttab\r\n")
	// U+0085 is the character where the counters differ: wc -w counts a\u0085b as 1 word,
	// strings.Fields as 2. GNU wc here treats NBSP as a separator, so NBSP does not tell them apart.
	agents := []byte("<!-- lighthouse:core:start -->\nblok  één\n<!-- lighthouse:core:end -->\n\n" +
		"<!-- lighthouse:go:start -->\nblok twee — a\u0085b\n<!-- lighthouse:go:end -->\n\n" +
		"<!-- lighthouse:py:start -->\n  blok drie  \n<!-- lighthouse:py:end -->\n")

	root := t.TempDir()
	for rel, b := range map[string][]byte{"CLAUDE.md": claude, "AGENTS.md": agents} {
		if err := os.WriteFile(filepath.Join(root, rel), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wantLabel, want, err := checkinstr.InstructionWords(root)
	if err != nil {
		t.Fatal(err)
	}
	if wantLabel != "CLAUDE.md + AGENTS.md" {
		t.Fatalf("check 10 label = %q, want %q", wantLabel, "CLAUDE.md + AGENTS.md")
	}

	plan := map[string]*planned{
		"CLAUDE.md": {old: claude, new: claude, existed: true},
		"AGENTS.md": {old: agents, new: agents, existed: true},
	}
	label, got, err := instructionWords(plan, true, wordcount.Count)
	if err != nil {
		t.Fatal(err)
	}
	if label != "CLAUDE.md + AGENTS.md" {
		t.Errorf("label = %q, want %q", label, "CLAUDE.md + AGENTS.md")
	}
	if got != want {
		t.Errorf("init counts %d words, check 10 counts %d", got, want)
	}
}
