package initcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/wordcount"
)

var check10Line = regexp.MustCompile(`CLAUDE\.md \+ AGENTS\.md counts (\d+) words`)

// Red when the init count and check 10 stop counting the same files with the same counter,
// for example when wordcount.Count falls back to strings.Fields.
func TestBudgetMatchesCheck10(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is missing")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is missing")
	}
	script, err := filepath.Abs("../../.lighthouse/scripts/check-instructions.sh")
	if err != nil {
		t.Fatal(err)
	}

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
	tmp := t.TempDir()
	toml := filepath.Join(tmp, "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[project]\ninstructions_word_ceiling = 100000\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bash, script)
	cmd.Env = append(os.Environ(),
		"LIGHTHOUSE_REPO_ROOT="+root,
		"LIGHTHOUSE_TOML="+toml,
		"CHECK_INSTRUCTIONS_PERSONAL="+filepath.Join(tmp, "personal"),
	)
	out, _ := cmd.CombinedOutput() // other checks may fail on this bare repo; only the check-10 line counts
	m := check10Line.FindSubmatch(out)
	if m == nil {
		t.Fatalf("no check 10 line in script output:\n%s", out)
	}
	want, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
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
