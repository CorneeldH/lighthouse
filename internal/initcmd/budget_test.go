package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/wordcount"
)

func ceiling(n string) func(*Options) {
	return stubLookup(map[string]string{"project.instructions_word_ceiling": n})
}

func wc(t *testing.T, root, rel string) int {
	t.Helper()
	n, err := wordcount.Count([]byte(read(t, root, rel)))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

const askLine = "Apply these changes? [y/N] "

// Red when the count reads the disk before the run (the new total then misses the
// planned CLAUDE.md) or the line is printed after the question.
func TestBudgetCountsPlannedFiles(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "y\n", ceiling("3000"))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
	a := wc(t, root, "AGENTS.md")
	want := fmt.Sprintf("instruction blocks: +%d words; CLAUDE.md + AGENTS.md becomes %d, ceiling 3000\n", a, 1+a)
	i, j := strings.Index(r.out, want), strings.Index(r.out, askLine)
	if i < 0 || j < 0 || i > j {
		t.Fatalf("want %q before the question in:\n%s", want, r.out)
	}
}

// Red when an overrun returns an error or stops the write.
func TestBudgetWarnsOverCeilingAndStillWrites(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, ceiling("5")))
	if r.code != 0 || !exists(root, "AGENTS.md") {
		t.Fatalf("rc %d, AGENTS.md written %v\n%s", r.code, exists(root, "AGENTS.md"), r.err)
	}
	total := wc(t, root, "AGENTS.md") + wc(t, root, "CLAUDE.md")
	want := fmt.Sprintf("warning: CLAUDE.md + AGENTS.md is %d words over the ceiling of 5; trim it or raise [project] instructions_word_ceiling", total-5)
	if !strings.Contains(r.err, want) {
		t.Fatalf("want %q in stderr:\n%s", want, r.err)
	}
}

// Red when the line is printed without a change to CLAUDE.md or AGENTS.md.
func TestBudgetSilentWhenInstructionsUnchanged(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	changed := func(o *Options) {
		p := testPacks()
		p["core/lib/a.py"] = file("x = 2\n")
		o.Packs = p
	}
	r := run(t, root, "", all(upgrade, yes, ceiling("3000"), changed))
	if r.code != 0 || read(t, root, ".lighthouse/lib/lighthouse_lib/a.py") != "# lighthouse: core@dev\nx = 2\n" {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	if strings.Contains(r.out+r.err, "instruction blocks:") {
		t.Fatalf("budget line without an instruction change:\n%s%s", r.out, r.err)
	}
	r = run(t, root, "", all(upgrade, yes, ceiling("3000"), changed))
	if r.out != "No changes.\n" {
		t.Fatalf("out = %q", r.out)
	}
}

// Red when a lookup error is skipped silently.
func TestBudgetNotMeasurableWithoutPython(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "y\n", func(o *Options) {
		o.Lookup = func(string) (string, bool, error) { return "", false, errNoPython }
	})
	if r.code != 0 || !exists(root, "AGENTS.md") {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
	if !strings.Contains(r.err, "instruction blocks: budget not measurable: python3 is not available") {
		t.Fatalf("stderr:\n%s", r.err)
	}
}

// Red when a missing CLAUDE.md is counted as 0 words instead of reported.
func TestBudgetNotMeasurableWithoutClaudeMD(t *testing.T) {
	root := newRepo(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("eigen regel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(t, root, "", all(yes, ceiling("3000")))
	if !strings.Contains(r.err, "budget not measurable: CLAUDE.md does not exist") {
		t.Fatalf("stderr:\n%s", r.err)
	}
}

// Red when ErrNoWC is swallowed or replaced by another reason.
func TestBudgetNotMeasurableWithoutWC(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, ceiling("3000"), func(o *Options) {
		o.CountWords = func([]byte) (int, error) { return 0, wordcount.ErrNoWC }
	}))
	if !strings.Contains(r.err, "budget not measurable: 'wc' is missing") {
		t.Fatalf("stderr:\n%s", r.err)
	}
}

// Red when the ceiling is parsed leniently or its error is dropped.
func TestBudgetCeilingNotANumber(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes, ceiling("veel")))
	if !strings.Contains(r.err, `budget not measurable: project.instructions_word_ceiling is "veel", not a number`) {
		t.Fatalf("stderr:\n%s", r.err)
	}
}

// Red when the delta is the words after without subtraction, "before" comes from new
// instead of old, the sign is missing, CLAUDE.md is left out of the total, or nothing is
// reported on --upgrade.
func TestBudgetUpgradeReportsBlockDelta(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	for rel, extra := range map[string]string{"AGENTS.md": "\nproject regel een twee drie\n", "CLAUDE.md": "Claude eigen tekst\n"} {
		f, err := os.OpenFile(filepath.Join(root, rel), os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(extra); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	before := wc(t, root, "AGENTS.md")
	r := run(t, root, "y\n", all(upgrade, ceiling("3000"), func(o *Options) {
		p := testPacks()
		p["core/instructions/AGENTS.md"] = file("## core\n")
		o.Packs = p
	}))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s%s", r.code, r.out, r.err)
	}
	total := wc(t, root, "CLAUDE.md") + wc(t, root, "AGENTS.md")
	if total == before-2 {
		t.Fatalf("fixture cannot tell CLAUDE.md in the total: %d", total)
	}
	want := fmt.Sprintf("instruction blocks: -2 words; CLAUDE.md + AGENTS.md becomes %d, ceiling 3000\n", total)
	i, j := strings.Index(r.out, want), strings.Index(r.out, askLine)
	if i < 0 || j < 0 || i > j {
		t.Fatalf("want %q before the question in:\n%s", want, r.out)
	}
}

// Red when reportBudget returns early on an unchanged plan before looking at an
// unreadable CLAUDE.md, or is only called in the branch with changes.
func TestBudgetUnreadableClaudeMDWithoutChanges(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "CLAUDE.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := run(t, root, "", all(upgrade, ceiling("3000")))
	if r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("rc %d, out %q\n%s", r.code, r.out, r.err)
	}
	if n := strings.Count(r.err, "instruction blocks: budget not measurable: CLAUDE.md is not readable:"); n != 1 {
		t.Fatalf("line printed %d times:\n%s", n, r.err)
	}
}

// Red when buildPlan returns the read error (rc 1, nothing written) or skips silently.
func TestBudgetNotMeasurableWithUnreadableClaudeMD(t *testing.T) {
	root := newRepo(t)
	if err := os.Mkdir(filepath.Join(root, "CLAUDE.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := run(t, root, "y\n", ceiling("3000"))
	if r.code != 0 {
		t.Fatalf("rc %d\n%s", r.code, r.err)
	}
	if !strings.Contains(r.err, "instruction blocks: budget not measurable: CLAUDE.md is not readable:") {
		t.Fatalf("stderr:\n%s", r.err)
	}
	info, err := os.Stat(filepath.Join(root, "CLAUDE.md"))
	if !exists(root, "AGENTS.md") || err != nil || !info.IsDir() {
		t.Fatalf("AGENTS.md written %v, CLAUDE.md dir %v (%v)", exists(root, "AGENTS.md"), err == nil && info.IsDir(), err)
	}
}
