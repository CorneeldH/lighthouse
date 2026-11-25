package collect

import "testing"

func TestCountPlanTasks(t *testing.T) {
	for _, c := range []struct {
		name, md string
		want     int
	}{
		{"six headings", "# Plan\n## Task 1: a\n### Task 2\n## Task 3\n### Task 4: x\n## Task 5\n### Task 6\n", 6},
		{"in backtick fence", "## Task 1\n```md\n### Task 2\n## Task 3\n```\n## Task 4\n", 2},
		{"in tilde fence", "~~~\n## Task 1\n~~~\n## Task 2\n", 1},
		{"longer fence is not closed by a shorter one", "````\n```\n## Task 1\n```\n````\n## Task 2\n", 1},
		{"dutch and crlf", "## Taak 1\r\n### Taak 2\r\ntext\r\n", 2},
		{"no headings", "# Plan\nTask 1 in prose\n#### Task 2\n## Tasks\n## Task x\n", 0},
		{"empty", "", 0},
	} {
		if got := CountPlanTasks(c.md); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}
