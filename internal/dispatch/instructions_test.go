package dispatch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteInstructions(t *testing.T) {
	run, err := os.ReadFile(filepath.Join("instructions", "run.md"))
	if err != nil {
		t.Fatal(err)
	}
	appendix, err := os.ReadFile(filepath.Join("instructions", "round_limit_question.md"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		atRoundLimit string
		want         string
	}{
		{"stop", string(run)},
		{"", string(run)},
		{"vraag", string(run) + string(appendix)},
	}
	for _, tc := range cases {
		t.Run("at round limit "+tc.atRoundLimit, func(t *testing.T) {
			state := t.TempDir()
			path, err := WriteInstructions(state, freshSession, tc.atRoundLimit)
			if err != nil {
				t.Fatalf("WriteInstructions: %v", err)
			}
			if want := filepath.Join(state, "runs", freshSession, "instructions.md"); path != want {
				t.Errorf("path = %q, want %q", path, want)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("content differs from instructions/*.md:\n%s", got)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
			}
			dir, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if dir.Mode().Perm() != 0o700 {
				t.Errorf("dir mode = %v, want 0700", dir.Mode().Perm())
			}
		})
	}
}

func TestWriteInstructionsTightensAnExistingFile(t *testing.T) {
	state := t.TempDir()
	dir := filepath.Join(state, "runs", freshSession)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("old, longer than nothing"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := WriteInstructions(state, freshSession, "stop")
	if err != nil {
		t.Fatalf("WriteInstructions: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	if d, err := os.Stat(dir); err != nil || d.Mode().Perm() != 0o700 {
		t.Errorf("dir = %v, %v; want mode 0700", d, err)
	}
	run, err := os.ReadFile(filepath.Join("instructions", "run.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(run) {
		t.Errorf("content = %q, want run.md", got)
	}
}

func TestWriteInstructionsRejectsSessionPath(t *testing.T) {
	for _, session := range []string{"", ".", "..", "../x", "a/b"} {
		if _, err := WriteInstructions(t.TempDir(), session, "stop"); err == nil {
			t.Errorf("WriteInstructions(%q): want an error", session)
		}
	}
}

// run.md is the product here: these lines are what a run must read to act right.
func TestRunInstructionsPinnedText(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("instructions", "run.md"))
	if err != nil {
		t.Fatal(err)
	}
	// run.md wraps lines; compare on single spaces.
	text := strings.Join(strings.Fields(string(raw)), " ")
	for _, want := range []string{
		// spec §4 rule 5: the close check measures what landed, not the worktree branch.
		"First run `git fetch origin` and measure the acceptance criteria against `origin/<default branch>`, not against the worktree branch.",
		// a served repo cannot read lighthouse's docs; the binary describes its own flags.
		"`lighthouse ask --help`",
		// the hint covers every refusal before the post, not only a wrong shape.
		"When it says `nothing posted`, nothing is on the issue: fix what the message names (the shape of the call, `LIGHTHOUSE_CONTROLLER_RUN`, `lighthouse.toml` or the GitHub token) and run it again.",
		// #204: ask renders only its fixed parts in the project language; the run writes the rest.
		"Write the text you pass to `lighthouse ask` (`--task`, `--where`, `--what`, `--question`, `--option`) in the `[project] language` of `lighthouse.toml`, also when these instructions and their example are English.",
		// the flag names the run uses must be the flags of `lighthouse ask`.
		"--issue", "--phase", "--task", "--where", "--what", "--question", "--option", "--recommended",
		"--command", "--dry-run",
		// #203: a handoff after the question becomes the last turn, and the answer resumes nothing.
		"stop after it, without `/lighthouse-write-handoff`",
		"End every phase with `/lighthouse-write-handoff`, except a phase that ends with `lighthouse ask`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("run.md lacks %q", want)
		}
	}
}

// Every `/lighthouse-<name>` run.md tells a run to start must be a skill of pack/core: a renamed
// skill left behind here is a command the run cannot start.
func TestRunInstructionsNameExistingSkills(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("instructions", "run.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "Before push + PR: `/lighthouse-write-pr <nr>`") {
		t.Error("run.md does not send a run through /lighthouse-write-pr before push + PR")
	}
	names := regexp.MustCompile("/lighthouse-([a-z0-9-]+)").FindAllStringSubmatch(text, -1)
	if len(names) == 0 {
		t.Fatal("run.md names no /lighthouse- skill")
	}
	for _, m := range names {
		if _, err := os.Stat(filepath.Join("..", "..", "pack", "core", "skills", m[1], "SKILL.md")); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join("..", "..", "pack", "core", "skills", m[1], "claude", "SKILL.md")); err != nil {
			t.Errorf("run.md names /lighthouse-%s, which is no skill in pack/core/skills", m[1])
		}
	}
	// The dispatcher starts fix-pr only on a red PR (#119): run.md carries no roborev rule.
	if strings.Contains(text, "roborev") {
		t.Error("run.md still carries a roborev rule")
	}
}
