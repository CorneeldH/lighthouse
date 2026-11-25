package checkinstr

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const mirrorTOML = "[paths]\nmirror_exceptions = \"mirror-exceptions.txt\"\n"

// harnessExceptions is the recorded form of the difference of the `paia-harnas` pair below.
const harnessExceptions = "=== paia-harnas/SKILL.md\n< Draai in Claude Code.\n> Draai in Codex.\n"

func harnessPair(claudeBody, agentsBody string) tree {
	return tree{
		".claude/skills/paia-harnas/SKILL.md": skillText("paia-harnas", "Doet iets.", nil, claudeBody),
		".agents/skills/paia-harnas/SKILL.md": skillText("paia-harnas", "Doet iets.", nil, agentsBody),
	}
}

// Check 11 (mirror content) against the exceptions file: green, one-sided drift, drift next to a
// recorded exception, a shifted-but-equal edit, a stale exception, and the hint that names the file.
// Class: cli-output. At risk: whether a mirror difference is called recorded or new.
func TestMirrorContentAgainstExceptions(t *testing.T) {
	sidedBodies := tree{
		".claude/skills/paia-scheef/SKILL.md": skillText("paia-scheef", "Doet iets.", nil, "# Nieuwe regel\n"),
		".agents/skills/paia-scheef/SKILL.md": skillText("paia-scheef", "Doet iets.", nil, "# Oude regel\n"),
	}
	const failLine = "FAIL  mirror-content differs from mirror-exceptions.txt (- recorded, + now; < .claude, > .agents):"
	cases := []struct {
		name  string
		files tree
		want  verdict
	}{
		{
			"equal mirror",
			emptySkillRoots().with(mirrored("paia-gelijk", skillText("paia-gelijk", "Doet iets.", nil, "# x\n"))),
			verdict{code: 0, lines: []string{"ok    mirror-content matches, apart from the recorded exceptions"}},
		},
		{
			"one-sided change",
			emptySkillRoots().with(sidedBodies),
			verdict{code: 1, lines: []string{
				failLine,
				"      +=== paia-scheef/SKILL.md",
				"      +< # Nieuwe regel",
				"      +> # Oude regel",
				"      lighthouse check-instructions --print-mirror-exceptions > mirror-exceptions.txt",
			}},
		},
		{
			"recorded exception is green",
			emptySkillRoots().with(harnessPair("Draai in Claude Code.\n", "Draai in Codex.\n"),
				tree{"mirror-exceptions.txt": harnessExceptions}),
			verdict{code: 0, lines: []string{"ok    mirror-content matches, apart from the recorded exceptions"}},
		},
		{
			"new drift next to a recorded exception",
			emptySkillRoots().with(harnessPair("Draai in Claude Code.\nAlleen links.\n", "Draai in Codex.\n"),
				tree{"mirror-exceptions.txt": harnessExceptions}),
			verdict{code: 1, lines: []string{failLine, "      +< Alleen links."}},
		},
		{
			"the same edit on both sides stays green",
			// Right after the closing `---`: a line above it would take the frontmatter away.
			emptySkillRoots().with(
				tree{
					".claude/skills/paia-harnas/SKILL.md": strings.Replace(skillText("paia-harnas", "Doet iets.", nil, "Draai in Claude Code.\n"), "\n---\n", "\n---\nBovenaan erbij.\n", 1),
					".agents/skills/paia-harnas/SKILL.md": strings.Replace(skillText("paia-harnas", "Doet iets.", nil, "Draai in Codex.\n"), "\n---\n", "\n---\nBovenaan erbij.\n", 1),
					"mirror-exceptions.txt":               harnessExceptions,
				}),
			verdict{code: 0, lines: []string{"ok    mirror-content matches, apart from the recorded exceptions"}},
		},
		{
			"a recorded exception that no longer exists",
			emptySkillRoots().with(harnessPair("Draai in Claude Code.\n", "Draai in Claude Code.\n"),
				tree{"mirror-exceptions.txt": harnessExceptions}),
			verdict{code: 1, lines: []string{failLine, "      -=== paia-harnas/SKILL.md", "      -< Draai in Claude Code.", "      -> Draai in Codex."}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertVerdict(t, check(t, tc.files, runOptions{toml: mirrorTOML}), tc.want)
		})
	}
}

// Check 11 and `--print-mirror-exceptions` when git cannot compare: SKIPPED, never "equal", and
// exit 4 for the print.
// Class: cli-output. At risk: an unreadable file or directory reading as "no difference".
func TestMirrorContentUnreadable(t *testing.T) {
	cases := []struct {
		name string
		dir  bool
	}{{"unreadable file", false}, {"unreadable skill directory", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dir && runtime.GOOS == "windows" {
				t.Skip("chmod 000 does not close a directory on NTFS; see host-assumptions.md")
			}
			files := emptySkillRoots().with(mirrored("paia-dicht", skillText("paia-dicht", "Doet iets.", nil, "# x\n")))
			base := t.TempDir()
			root := filepath.Join(base, "repo")
			for path, content := range files {
				if !strings.HasSuffix(path, "/") {
					writeFile(t, filepath.Join(root, path), content)
				}
			}
			closed := []string{filepath.Join(root, ".claude/skills/paia-dicht/SKILL.md")}
			if tc.dir {
				closed = []string{filepath.Join(root, ".claude/skills/paia-dicht"), filepath.Join(root, ".agents/skills/paia-dicht")}
			}
			for _, path := range closed {
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
			}
			if _, err := os.ReadFile(closed[0]); err == nil {
				t.Skip("readable although mode 000 (running as root)")
			}
			toml := filepath.Join(base, "lighthouse.toml")
			writeFile(t, toml, mirrorTOML)
			t.Setenv("LIGHTHOUSE_TOML", toml)
			t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")

			var out, errOut bytes.Buffer
			code := Run(root, filepath.Join(base, "none"), &out, &errOut)
			if code != 0 {
				t.Errorf("Run exit %d, want 0\n%s", code, out.String())
			}
			if want := "SKIPPED  mirror-content: diff could not compare a skill pair, so the content was not measured\n"; !strings.Contains(out.String(), want) {
				t.Errorf("no %q in:\n%s", want, out.String())
			}
			if strings.Contains(out.String(), "ok    mirror-content") {
				t.Errorf("unreadable pair counted as equal:\n%s", out.String())
			}
			if tc.dir {
				return // PrintMirrorExceptions for a directory is covered by TestMirrorDifferencesNamesUnreadableDir
			}
			out.Reset()
			errOut.Reset()
			if code := PrintMirrorExceptions(root, &out, &errOut); code != 4 {
				t.Errorf("PrintMirrorExceptions exit %d, want 4", code)
			}
			if want := "SKIPPED  diff could not compare a skill pair\n"; !strings.HasSuffix(errOut.String(), want) {
				t.Errorf("stderr %q does not end in %q", errOut.String(), want)
			}
		})
	}
}

// With an empty PATH check 11 cannot run git and check 10 cannot run wc: both say SKIPPED, and
// the run still exits 0.
// Class: cli-output. At risk: a missing tool reading as "measured, no finding".
func TestMissingGitAndWcAreSkipped(t *testing.T) {
	files := emptySkillRoots().with(
		mirrored("paia-dicht", skillText("paia-dicht", "Doet iets.", nil, "# x\n")),
		tree{"CLAUDE.md": "een twee drie\n"})
	r := check(t, files, runOptions{toml: mirrorTOML, env: map[string]string{"PATH": t.TempDir()}})
	assertVerdict(t, r, verdict{code: 0, lines: []string{
		"SKIPPED  mirror-content: diff could not compare a skill pair, so the content was not measured",
		"SKIPPED  size: 'wc' is missing on this machine, so CLAUDE.md cannot be counted here",
	}, absent: []string{"ok    mirror-content matches, apart from the recorded exceptions"}})
}
