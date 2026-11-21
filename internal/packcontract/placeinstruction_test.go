package packcontract

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/checkinstr"
	"github.com/CorneeldH/lighthouse/internal/config"
)

// place-instruction: the skill decides on which layer a new instruction belongs and validates
// with `lighthouse check-instructions`. That command carries only the generic checks; the skill
// names no project-own check (6, 9, 12, 13). The skill prefix and the word ceiling come from
// `[project]`, the bounding clause it prescribes passes check 4, and the skill stays called by
// name only. `python3` in the skill is TestRequiresNamesTheSkillPerTool.

const placeInstruction = "core/skills/place-instruction"

// placeInstructionStep is the text of "## Step <n> " up to the next level-2 heading.
func placeInstructionStep(t *testing.T, body string, n int) string {
	t.Helper()
	return section(t, body, "\n## Step "+strconv.Itoa(n)+" ", "\n## ")
}

// checkinstrNumbers are the check numbers internal/checkinstr implements: one `// Check <n>:`
// comment heads each check function, and the package carries no other numbering.
func checkinstrNumbers(t *testing.T) []int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "checkinstr", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	re := regexp.MustCompile(`(?m)^// Check (\d+):`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			n, _ := strconv.Atoi(m[1])
			nums = append(nums, n)
		}
	}
	if len(nums) == 0 {
		t.Fatal("no `// Check <n>:` in internal/checkinstr")
	}
	return nums
}

// Classification: fails when the skill names a check `lighthouse check-instructions` does not
// carry, such as a project-own check 6, 9, 12 or 13.
func TestPlaceInstructionNamesOnlyChecksTheCommandCarries(t *testing.T) {
	have := checkinstrNumbers(t)
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			named := regexp.MustCompile(`\b[Cc]heck (\d+)`).FindAllStringSubmatch(body, -1)
			if len(named) == 0 {
				t.Fatal("the skill names no check")
			}
			for _, m := range named {
				n, _ := strconv.Atoi(m[1])
				if !slices.Contains(have, n) {
					t.Errorf("names check %d; check-instructions carries %v", n, have)
				}
			}
		})
	}
}

// Classification: fails when step 7 stops validating with `lighthouse check-instructions`, or
// the binary loses the subcommand.
func TestPlaceInstructionValidatesWithTheLighthouseCommand(t *testing.T) {
	cliFlags(t, []string{"check-instructions"})
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, placeInstructionStep(t, body, 7), "## Step 7 — Validate\n\n```bash\nlighthouse check-instructions\n```")
		})
	}
}

// Classification: fails when the skill prefix or the word ceiling stops coming from
// `lighthouse config`, or the key is one config does not know.
func TestPlaceInstructionPrefixAndCeilingFromConfig(t *testing.T) {
	for _, key := range []string{"project.skill_prefix", "project.instructions_word_ceiling"} {
		if !config.IsKnownKey(key) {
			t.Errorf("config does not know %s", key)
		}
	}
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(placeInstructionStep(t, body, 6)),
				"- **The project's skill prefix**, always: `lighthouse config project.skill_prefix` (rc 1: no prefix set — choose one and set it in `[project]`).")
			wantAll(t, flat(section(t, body, "## KPIs", "## Non-negotiable")),
				"| Words in `CLAUDE.md` + `AGENTS.md` | ≤ the ceiling (`lighthouse config project.instructions_word_ceiling`, default 3000) |")
		})
	}
}

// overlapRepo is a repo with two installed skills whose descriptions share seven trigger words
// (check 4 trips at five); the second ends in clause. It returns the output of check-instructions
// on it.
func overlapRepo(t *testing.T, clause string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "lighthouse.toml"), "[project]\nlanguage = \"en\"\n")
	shared := "Audits worktrees branches sessions releases milestones of the repository."
	descs := map[string]string{
		"lighthouse-alpha-task": shared,
		"lighthouse-beta-task":  shared + " " + clause,
	}
	for name, desc := range descs {
		text := "---\nname: " + name + "\ndescription: '" + desc + "'\n---\n\n# " + name + "\n"
		for _, base := range []string{".claude/skills", ".agents/skills"} {
			writeFile(t, filepath.Join(root, base, name, "SKILL.md"), text)
		}
	}
	t.Setenv("LIGHTHOUSE_TOML", "")
	var out, errOut bytes.Buffer
	rc := checkinstr.Run(root, filepath.Join(root, "no-personal-skills"), &out, &errOut)
	if rc == 2 || errOut.Len() > 0 {
		t.Fatalf("check-instructions could not check: rc %d, stderr %q", rc, errOut.String())
	}
	return out.String()
}

// Classification: fails when the form the skill prescribes for the bounding clause is one check 4
// of `lighthouse check-instructions` does not accept.
func TestPlaceInstructionBoundingClausePassesCheck4(t *testing.T) {
	unbounded := overlapRepo(t, "")
	if !strings.Contains(unbounded, "FAIL  overlap without a bounding clause: 'lighthouse-alpha-task' and 'lighthouse-beta-task'") {
		t.Fatalf("the two descriptions do not trip check 4 without a clause:\n%s", unbounded)
	}
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			m := regexp.MustCompile("Form: `([^`]+)`").FindStringSubmatch(body)
			if m == nil {
				t.Fatal("no Form: for the bounding clause")
			}
			clause := strings.NewReplacer("X", "the cleanup", "Y", "lighthouse-alpha-task").Replace(m[1])
			if clause == m[1] {
				t.Fatalf("the form %q has no X or Y to fill in", m[1])
			}
			out := overlapRepo(t, clause)
			if !strings.Contains(out, "ok    no unbounded overlap between descriptions\n") {
				t.Errorf("check 4 rejects the prescribed form %q:\n%s", clause, out)
			}
		})
	}
}

// Classification: fails when the skill names the source repo, its host, paths or vocabulary, or
// a source issue number.
func TestPlaceInstructionIsDetachedFromTheSourceRepo(t *testing.T) {
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, body, "Beelink", "tests/unit/", "developer/planning/specs/", "Milestone #58", "applicatie", "vault")
			if m := issueNumberRe.FindString(body); m != "" {
				t.Errorf("names issue number %s", m)
			}
		})
	}
}

// Classification: fails when the skill becomes model-invoked, or `disable-model-invocation`
// moves under `metadata:`, where Claude Code does not read it.
func TestPlaceInstructionIsCalledByNameOnly(t *testing.T) {
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, "\n"+frontmatter(t, body)+"\n", "\ndisable-model-invocation: true\n")
		})
	}
}

// Classification: fails when a Dutch sentence enters the skill.
func TestPlaceInstructionIsEnglish(t *testing.T) {
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, strings.ToLower(body), dutchWords...)
		})
	}
}

// Classification: fails when a name shape leaves the table of step 6, the check-14 rule for
// lighthouse hooks changes, or step 5 stops deriving the kind of unit from the name.
func TestPlaceInstructionNamingConventionPerForm(t *testing.T) {
	for variant, body := range variants(t, placeInstruction) {
		t.Run(variant, func(t *testing.T) {
			s6 := placeInstructionStep(t, body, 6)
			for _, row := range []string{
				"| workflow skill (the session *does* the task) | verb + object: `write-issue`, `review-spec` |",
				"| standard (quality bar plus our preference and style, loaded next to another task) | `<topic>-standard`: `go-standard`, `test-standard` |",
				"| hook, gate (registered on `PreToolUse`, `Stop` or `SubagentStop` and exits 2: the action does not get through) | `<object>-gate`: `main-commit-gate` |",
				"| hook, check (reports or warns afterwards, with or without exit 2: the action has happened) | `<object>-check`: `md-link-check` |",
				"| agent | `<object>-<occupation>`: `issue-impact-scout` |",
			} {
				if !strings.Contains(s6, "\n  "+row) {
					t.Errorf("no name-shape row %s", row)
				}
			}
			wantAll(t, flat(s6),
				"Check 14 reads only the lighthouse hooks in `.claude/settings.json` (commands under `.claude/hooks/lighthouse/`): `-gate` or `-check` at the end, a gate on a blocking event, a check not on `Stop` or `SubagentStop`.")
			wantAll(t, flat(placeInstructionStep(t, body, 5)),
				"`type` stays away in particular: the name carries the kind of the unit (see the name shapes in step 6), so a field would only repeat it.")
		})
	}
}
