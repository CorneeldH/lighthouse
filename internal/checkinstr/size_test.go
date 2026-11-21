package checkinstr

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func words(n int) string { return strings.Repeat("woord ", n) }

func ceilingTOML(n int) string {
	return "[project]\ninstructions_word_ceiling = " + strconv.Itoa(n) + "\n"
}

// Check 10: the word ceiling over CLAUDE.md and the AGENTS.md it imports.
// Class: cli-output. At risk: an instruction file over its ceiling reading as fine, or the reverse.
func TestSizeCheck(t *testing.T) {
	cases := []struct {
		name  string
		files tree
		toml  string
		want  verdict
	}{
		{"above the ceiling", tree{"CLAUDE.md": words(50)}, ceilingTOML(10),
			verdict{code: 1, lines: []string{"FAIL  size: CLAUDE.md counts 50 words, ceiling is 10"}}},
		{"at the ceiling", tree{"CLAUDE.md": words(10)}, ceilingTOML(10),
			verdict{code: 0, lines: []string{"ok    CLAUDE.md counts 10 words, within the ceiling of 10"}}},
		{"one above the ceiling", tree{"CLAUDE.md": words(11)}, ceilingTOML(10),
			verdict{code: 1, lines: []string{"FAIL  size: CLAUDE.md counts 11 words, ceiling is 10"}}},
		{"below the ceiling", tree{"CLAUDE.md": words(5)}, ceilingTOML(10),
			verdict{code: 0, lines: []string{"ok    CLAUDE.md counts 5 words, within the ceiling of 10"}}},
		{"the ceiling comes from the config", tree{"CLAUDE.md": words(8)}, ceilingTOML(7),
			verdict{code: 1, lines: []string{"FAIL  size: CLAUDE.md counts 8 words, ceiling is 7"}}},
		{"the default ceiling is 3000", tree{"CLAUDE.md": words(5)}, "",
			verdict{code: 0, lines: []string{"ok    CLAUDE.md counts 5 words, within the ceiling of 3000"}}},
		{"AGENTS.md counts along", tree{"CLAUDE.md": words(4), "AGENTS.md": words(6)}, ceilingTOML(10),
			verdict{code: 0, lines: []string{"ok    CLAUDE.md + AGENTS.md counts 10 words, within the ceiling of 10"}}},
		{"AGENTS.md pushes the sum over the ceiling", tree{"CLAUDE.md": "@AGENTS.md\n", "AGENTS.md": words(10)}, ceilingTOML(10),
			verdict{code: 1, lines: []string{"FAIL  size: CLAUDE.md + AGENTS.md counts 11 words, ceiling is 10"}}},
		{"no CLAUDE.md is SKIPPED", tree{}, "",
			verdict{code: 0, lines: []string{"SKIPPED  size: '{root}/CLAUDE.md' does not exist, so there is nothing to count here"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, tc.files, runOptions{toml: tc.toml}), tc.want)
		})
	}
}

// Check 10 on a file it cannot read: SKIPPED and never an ok on the words it could count.
// Class: cli-output. At risk: an unreadable instruction file passing as within the ceiling.
func TestSizeUnreadableFileIsSkipped(t *testing.T) {
	cases := []struct {
		name, unreadable string
		want             verdict
	}{
		{"CLAUDE.md", "CLAUDE.md", verdict{code: 0,
			lines:  []string{"SKIPPED  size: '{root}/CLAUDE.md' is not readable, so there is nothing to count here"},
			absent: []string{"ok    CLAUDE.md counts 5 words, within the ceiling of 3000", "ok    CLAUDE.md + AGENTS.md counts 10 words, within the ceiling of 3000"}}},
		{"AGENTS.md", "AGENTS.md", verdict{code: 0,
			lines:  []string{"SKIPPED  size: '{root}/AGENTS.md' is not readable, so there is nothing to count here"},
			absent: []string{"ok    CLAUDE.md counts 5 words, within the ceiling of 3000", "ok    CLAUDE.md + AGENTS.md counts 10 words, within the ceiling of 3000"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Both files exist; check() writes them, then the one under test is closed, so the
			// run has to be assembled by hand.
			base := t.TempDir()
			root := filepath.Join(base, "repo")
			writeFile(t, filepath.Join(root, "CLAUDE.md"), words(5))
			writeFile(t, filepath.Join(root, "AGENTS.md"), words(5))
			closed := filepath.Join(root, tc.unreadable)
			if err := os.Chmod(closed, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(closed, 0o644) })
			if _, err := os.ReadFile(closed); err == nil {
				t.Skip("readable although mode 000 (running as root)")
			}
			r := runIn(t, root, base, filepath.Join(base, "personal"), runOptions{})
			assertVerdict(t, r, tc.want)
		})
	}
}
