package packcontract

import (
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/kpi"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/roborev"
	"gopkg.in/yaml.v3"
)

// fix-pr: what lives only in skill text. The description triggers and bounds the skill, the
// diagnosis block reads the forge through `lighthouse forge`, every known gate has a local
// command, the worktree step uses the head branch, the KPI table names the binary's metrics, and
// section 6 processes a roborev review with the marker the renderer writes and the dispatcher
// reads. That the diagnosis block is one read-only block is TestFixPRDiagnosisBlockIsReadOnly
// (internal/kpi); that the pre-push paragraph says "if present" is TestOptionalStepsGuarded.

const fixPR = "core/skills/fix-pr"

// fixPRDescription is the description from the frontmatter, parsed as YAML.
func fixPRDescription(t *testing.T, body string) string {
	t.Helper()
	var front struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter(t, body)), &front); err != nil {
		t.Fatal(err)
	}
	if front.Description == "" {
		t.Fatal("no description")
	}
	return front.Description
}

// fixPRDiagnosis is the one bash block under the diagnosis heading.
func fixPRDiagnosis(t *testing.T, body string) string {
	t.Helper()
	s := section(t, body, "## 1. Diagnosis in one block", "\n## ")
	if n := strings.Count(s, "```bash\n"); n != 1 {
		t.Fatalf("%d bash blocks under the diagnosis heading, want 1", n)
	}
	return section(t, s, "```bash\n", "```")
}

// fixPRSection6 is section 6 up to the non-negotiable list.
func fixPRSection6(t *testing.T, body string) string {
	t.Helper()
	return section(t, body, "## 6. Process a roborev review (only if present)", "## Non-negotiable")
}

// Classification: fails when the description loses one of the measured trigger phrases (fix pr,
// conflicts, red).
func TestFixPRDescriptionCarriesTheMeasuredTriggers(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, fixPRDescription(t, body),
				"is red or has merge conflicts",
				`"fix pr 596"`,
				`"612 has conflicts, fix it"`,
				`"PR 585 is red"`)
		})
	}
}

// Classification: fails when the NOTE boundary of the description stops naming
// lighthouse-close-issue or caveman-review as the skill for what lies past green.
func TestFixPRDescriptionCarriesANegativeBoundary(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, fixPRDescription(t, body),
				"NOTE — this skill stops as soon as the PR is green (and a roborev review is processed):",
				"closing the issue is lighthouse-close-issue",
				"a review of the diff on content is caveman-review")
		})
	}
}

// Classification: fails when the diagnosis stops reading the PR, its comments, its checks or the
// failed log through `lighthouse forge` (or the binary drops one of those flags), calls gh, swallows
// the checks exit with `|| true`, or loses an `|| echo` branch for an empty result.
func TestFixPRDiagnosisRunsOnLighthouseForge(t *testing.T) {
	cliFlags(t, []string{"forge", "pr", "view"}, "comments")
	cliFlags(t, []string{"forge", "pr", "checks"}, "failed-log")
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			block := fixPRDiagnosis(t, body)
			wantAll(t, block,
				`PR=$(lighthouse forge pr view "$N" --comments 2>&1)`,
				`CK=$(lighthouse forge pr checks "$N") || { echo "checks: forge error (see above)"; CK='[]'; }`,
				`lighthouse forge pr checks "$N" --failed-log \`,
				`|| echo "no checks"`,
				`echo "no fail check — red is then a conflict, a pending check or a run that did not start"`)
			wantNone(t, block, "gh ", "|| true")
		})
	}
}

// fixPRGates is the gate table in order: the label in the first cell and the local command.
var fixPRGates = []struct{ label, command string }{
	{"Tests", "`lighthouse config test_gate.command` prints it; run it with `bash -c`"},
	{"Quality ratchet (ruff)", "`test -e .lighthouse/scripts/quality/ratchet.py && uv run python .lighthouse/scripts/quality/ratchet.py ruff`"},
	{"Expo lockfiles", "`/lighthouse-refresh-expo-lockfiles`"},
	{"Any other step", "the command of that step in `.github/workflows/`"},
}

// fixPRGateRows are the body rows of the gate table in section 3.
func fixPRGateRows(t *testing.T, body string) []string {
	t.Helper()
	var rows []string
	s := section(t, body, "## 3. The gates, and how to reproduce them locally", "\n## ")
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			rows = append(rows, line)
		}
	}
	if len(rows) < 2 {
		t.Fatal("no gate table under the heading")
	}
	return rows[2:]
}

// Classification: fails when a known gate leaves the table, the order changes, or a row loses
// the command that reproduces it locally.
func TestFixPREveryKnownGateHasALocalCommand(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			rows := fixPRGateRows(t, body)
			if len(rows) != len(fixPRGates) {
				t.Fatalf("%d gate rows, want %d: %q", len(rows), len(fixPRGates), rows)
			}
			for i, g := range fixPRGates {
				cells := strings.Split(rows[i], "|")
				if strings.TrimSpace(cells[1]) != g.label {
					t.Errorf("row %d first cell %q, want %q", i, cells[1], g.label)
				}
				wantAll(t, rows[i], g.command)
			}
		})
	}
}

// Classification: fails when the test row stops reading `[test_gate] command`, the ratchet or
// Expo row loses its "only if present" guard, or a literal `tests/unit` enters the skill.
func TestFixPRUsesTheTestGateCommand(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			rows := fixPRGateRows(t, body)
			if len(rows) < 3 {
				t.Fatalf("%d gate rows, want at least 3", len(rows))
			}
			wantAll(t, rows[0], "`lighthouse config test_gate.command` prints it")
			wantAll(t, rows[1], "pack/python, only if present", "`test -e .lighthouse/scripts/quality/ratchet.py &&")
			wantAll(t, rows[2], "pack/expo, only if present")
			wantNone(t, body, "tests/unit")
		})
	}
}

// Classification: fails when the roborev part of the diagnosis stops depending on
// `.roborev.toml`, or section 6 loses its "only if present" heading.
func TestFixPRRoborevOnlyIfPresent(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, fixPRDiagnosis(t, body),
				"if test -e .roborev.toml; then",
				`echo "roborev: skipped — no .roborev.toml"`)
			wantAll(t, body, "\n## 6. Process a roborev review (only if present)\n")
		})
	}
}

// Classification: fails when section 2 stops creating the worktree on the head branch from
// origin, or stops ruling out EnterWorktree (a new branch from the default branch).
func TestFixPRWorktreeStepUsesTheHeadBranch(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			s2 := section(t, body, "## 2. Work in the worktree of the head branch", "\n## ")
			wantAll(t, s2, `git fetch origin "$B" && git worktree add "$WT" --track -b "$B" "origin/$B"`)
			wantAll(t, flat(s2), "`EnterWorktree` and `superpowers:using-git-worktrees` make a **new** branch from `origin/<default branch>` and are therefore wrong here.")
			wantAll(t, body, "- ✅ `git worktree add <path> --track -b <b> origin/<b>` · ❌ `EnterWorktree` (makes a new branch from the default branch)")
		})
	}
}

// Classification: fails when the KPI table stops naming a metric of `lighthouse
// measure-fix-pr-kpi`, names one the binary does not have, or the subcommand disappears.
func TestFixPRKPITableNamesTheMeasuringCommand(t *testing.T) {
	cliFlags(t, []string{"measure-fix-pr-kpi"})
	if len(kpi.FixPRMetrics) == 0 {
		t.Fatal("kpi.FixPRMetrics is empty")
	}
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			table := section(t, body, "## KPIs", "\n## ")
			for _, m := range kpi.FixPRMetrics {
				wantAll(t, table, "| `lighthouse measure-fix-pr-kpi "+m+"` |")
			}
			if n := strings.Count(table, "lighthouse measure-fix-pr-kpi "); n != len(kpi.FixPRMetrics) {
				t.Errorf("KPI table names the command %d times, want %d", n, len(kpi.FixPRMetrics))
			}
		})
	}
}

// Classification: fails when the description stops naming the second entry, a green PR with an
// unprocessed roborev review.
func TestFixPRDescriptionNamesTheRoborevEntry(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, fixPRDescription(t, body),
				"or when a green PR carries an unprocessed roborev review comment (findings without a `roborev-verwerkt` marker)")
		})
	}
}

// Classification: fails when the diagnosis stops selecting the roborev comment by its
// `roborev-pr-comment` tag, stops printing the whole body as one JSON line, or stops showing the
// `roborev-verwerkt` marker lines.
func TestFixPRDiagnosisShowsTheFullRoborevBody(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, fixPRDiagnosis(t, body),
				`(.body | contains("roborev-pr-comment"))) | .body | @json'`,
				`RB=$(printf '%s\n' "$RB" | tail -1 | jq -r '. // empty' 2>/dev/null)`,
				`select(contains("<!-- roborev-verwerkt:"))`)
		})
	}
}

// Classification: fails when the marker in section 6 differs from the first line the renderer
// writes, or the section stops sending each finding through receiving-code-review, or stops
// forbidding a re-review, `roborev fix` and `roborev refine`.
func TestFixPRRoborevSectionCarriesTheContract(t *testing.T) {
	state := orderedjson.NewObject()
	state.Set("sha", "<sha>")
	state.Set("bevindingen", []any{})
	rendered, err := roborev.Render(state, "", 1, "ci")
	if err != nil {
		t.Fatal(err)
	}
	marker, _, _ := strings.Cut(rendered, "\n")
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(fixPRSection6(t, body)),
				"The renderer puts the marker `"+marker+"` at the top",
				"Follow `superpowers:receiving-code-review` per finding: fix and commit, or refute with a reason.",
				"Request no new review (no close and reopen) and no `roborev fix` or `roborev refine`.")
		})
	}
}

// Classification: fails when the variants differ in anything but the three sandbox lines, or the
// claude variant loses `dangerouslyDisableSandbox: true` in the diagnosis intro.
func TestFixPRVariantsDifferOnlyInTheSandboxWording(t *testing.T) {
	v := variants(t, fixPR)
	claude, agents := strings.Split(v["claude"], "\n"), strings.Split(v["agents"], "\n")
	if len(claude) != len(agents) {
		t.Fatalf("claude has %d lines, agents %d", len(claude), len(agents))
	}
	var diff []string
	for i := range claude {
		if claude[i] != agents[i] {
			diff = append(diff, claude[i], agents[i])
		}
	}
	want := []string{
		"One Bash call, read-only, `dangerouslyDisableSandbox: true`. Replace `<nr>`.",
		"One Bash call, read-only, outside the sandbox. Replace `<nr>`.",
		"   The forge calls here run with `dangerouslyDisableSandbox: true`, like the diagnosis:",
		"   The forge calls here run outside the sandbox, like the diagnosis:",
		"- ✅ `lighthouse forge` with `dangerouslyDisableSandbox: true` · ❌ without — Go's TLS is out then",
		"- ✅ `lighthouse forge` outside the sandbox · ❌ without — Go's TLS is out then",
	}
	if strings.Join(diff, "\n") != strings.Join(want, "\n") {
		t.Errorf("differing lines (claude, agents pairs) = %q, want %q", diff, want)
	}
}

// Classification: fails when section 6 stops saving the CI comment, recording the decisions with
// the raw review, or rendering the shared log with `--bron ci`, or the binary drops a flag it uses.
func TestFixPRRoborevSectionRendersTheSharedLog(t *testing.T) {
	cliFlags(t, []string{"roborev-gate", "classify"})
	cliFlags(t, []string{"roborev-gate", "record"}, "done", "decisions", "out", "raw")
	cliFlags(t, []string{"roborev-comment", "render"}, "pr", "bron", "state", "raw")
	cliFlags(t, []string{"forge", "pr", "comment"}, "body-file")
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, fixPRSection6(t, body),
				"lighthouse roborev-gate classify <scratch>/roborev-ci.md",
				"lighthouse roborev-gate record --done <sha> --decisions <scratch>/decisions.json --out <scratch>/roborev.json --raw <scratch>/roborev-ci.md",
				"lighthouse roborev-comment render --pr <nr> --bron ci --state <scratch>/roborev.json --raw <scratch>/roborev-ci.md > <scratch>/comment.md",
				"lighthouse forge pr comment <nr> --body-file <scratch>/comment.md")
		})
	}
}

// Classification: fails when section 6 stops telling the agent to post nothing and stop when
// `classify` skips a review it cannot read.
func TestFixPRRoborevSectionStopsOnAnUnreadableCIReview(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(fixPRSection6(t, body)),
				"If `classify` prints `verdict` `skip` (for instance `review faalde: bevindingen onleesbaar`), there is no `sha` to record: render and post nothing, name the reason in your answer and stop this section.")
		})
	}
}

// Classification: fails when the skill stops leaving the merge to the tick, stops naming the four
// causes the dispatcher starts it on, or stops saying the dispatcher reads the marker.
func TestFixPRDispatcherContract(t *testing.T) {
	for variant, body := range variants(t, fixPR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(section(t, body, "## Domain", "## 1.")),
				"starts this skill after a finished run not only on a red PR but also on a conflict, an unprocessed roborev review and a merge that failed.",
				"The tick merges a green PR of its own; this skill never merges.",
				"and the merge itself — the tick does that after a finished run, never this skill.")
			wantAll(t, flat(fixPRSection6(t, body)),
				"Under a controller run the dispatcher reads the marker: a review without one keeps the PR from merging and starts this skill, at most three times per PR.")
		})
	}
}
