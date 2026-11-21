package packcontract

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/cli"
	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/review"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// review-spec and review-plan: what lives only in skill text. Round 1 is a review, every later
// round only verifies the fixes; exit 6 falls back in Claude Code and stops in the agents
// variant; round 1 runs paired (Codex and Opus, decided blind) and Opus verifies, in Claude Code
// only; the round limit records the open points and carries on; the controller run applies
// only if present. The prompts and schemas are one file per skill (TestCorePack); the
// reviewer prompt itself is TestBuildPromptByteIdentical in internal/review.

// reviewSkills maps each review skill to its directory under packRoot.
var reviewSkills = map[string]string{
	"review-spec": "core/skills/review-spec",
	"review-plan": "core/skills/review-plan",
}

// reviewVariant is the SKILL.md of one variant of a review skill.
func reviewVariant(t *testing.T, name, variant string) string {
	t.Helper()
	return variants(t, reviewSkills[name])[variant]
}

// forEachReviewVariant runs fn per skill and variant as subtest "<skill>/<variant>".
func forEachReviewVariant(t *testing.T, fn func(t *testing.T, name, variant, body string)) {
	t.Helper()
	for _, name := range []string{"review-spec", "review-plan"} {
		for variant, body := range variants(t, reviewSkills[name]) {
			t.Run(name+"/"+variant, func(t *testing.T) { fn(t, name, variant, body) })
		}
	}
}

// Classification: fails when a review skill drops a verification rule — a round n ≥ 2 only
// after an accepted fix, MINORs that never start a round, the third round only when a fix of
// round 2 does not hold, the check that every accepted ID is verified — or `--verify` leaves
// the runner.
func TestReviewVerifiesInsteadOfRereviewing(t *testing.T) {
	cliFlags(t, []string{"review-run"}, "verify")
	perSkill := map[string][]string{
		"review-spec": {
			"**Round n ≥ 2 runs only if round n-1 had a BLOCKER/MAJOR with `accept`.** `defer` does not count. No fixes, no round.",
			"**Only round 1 is a review.** From round 2 on the reviewer only verifies whether the fixes of the previous round hold — no new search for problems;",
			"**A third round only if a fix from round 2 does not hold** — the same finding two rounds in a row is a structural problem, no round-4 candidate.",
			"Every accepted ID is in `verified` or in `verifies` of a finding (accepted = `accept`). If one is missing, run *that* verification round again (same `n`, same `.fixes.diff`, `round-<n>.json` overwritten).",
		},
		"review-plan": {
			"**Round n ≥ 2 runs only if round n-1 had a BLOCKER/MAJOR with `accept` or `spec`.** No fixes, no round.",
			"It does not search for problems again; it only verifies whether the fixes of the previous round hold (see *Workflow*, step 5).",
			"| round 3 reached with open BLOCKERs/MAJORs. A third round only if a fix from round 2 does not hold |",
			"Every accepted ID is in `verified` or in `verifies` of a finding (accepted = `accept` or `spec`). If one is missing, run *that* verification round again (same `n`, same `.fixes.diff`, `round-<n>.json` overwritten).",
		},
	}
	forEachReviewVariant(t, func(t *testing.T, name, _, body string) {
		f := flat(body)
		wantAll(t, f, perSkill[name]...)
		wantAll(t, f,
			"MINORs never start a round.",
			"`git diff --no-index` exits 1 when there is a difference; that is no error.",
			`| \(.verifies // "") |`,
			`jq -r '.verified // empty | "VERIFIED: " + (if length == 0 then "none" else join(", ") end)'`)
	})
}

// budgetFields are the JSON keys of the budget decision the runner writes to <OUT>.budget.json.
func budgetFields(t *testing.T) map[string]bool {
	t.Helper()
	data, err := json.Marshal(usage.BudgetOutcome{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for k := range fields {
		keys[k] = true
	}
	return keys
}

// Classification: fails when exit 6 (the Codex budget exhausted before the round) no longer
// sends the Claude variant to the Opus fallback without a question, or no longer stops the
// agents variant before the round; or when the runner changes the exit code or the budget
// fields the skills read.
func TestReviewExit6FallsBackInClaudeAndStopsInAgents(t *testing.T) {
	exit6 := "exit " + strconv.Itoa(review.ExitBudgetEmpty)
	fields := budgetFields(t)
	for _, f := range []string{"reason", "reset_at"} {
		if !fields[f] {
			t.Errorf("the budget decision has no field %q", f)
		}
	}
	claude := map[string][]string{
		"review-spec": {
			"**Codex budget exhausted beforehand = " + exit6 + ", fallback at once.**",
			"Round 1 then runs Opus-only (see *Fallback without Codex*): no question to the user, no waiting. Report one line with `reason` and `reset_at` from `<OUT>.budget.json`.",
			"the budget exhausted beforehand (" + exit6 + "), or a Codex limit in the middle of the round (exit 5 + `try again at`). For the last two do not ask, act at once.",
		},
		"review-plan": {
			"the budget exhausted beforehand (" + exit6 + "), or a Codex limit in the middle of the round (exit 5 + `try again at`). Round 1 then runs Opus-only:",
			"- **Exit 6** you recognise by `CODEX_BUDGET: fallback — <reason>` on stderr and the decision in `<ATTEMPT>/round-1.codex.json.budget.json`. **Do not ask and do not wait.** Report one line with `reason` and `reset_at` from that file.",
			"| Codex " + exit6 + " (budget exhausted beforehand) | `mv <ATTEMPT>/round-1.fallback.json <ATTEMPT>/round-1.json` | `fallback --reviewer-model <reported model>` |",
		},
	}
	agents := map[string]string{
		"review-spec": "Report `reason` and `reset_at` from `<OUT>.budget.json` and stop **before** the round; the outcome is `aborted`, as with the limit.",
		"review-plan": "Report `reason` and `reset_at` from `<OUT>.budget.json` (a paired round 1 does not exist here: outside Claude Code there is no pair) and stop **before** the round; the convergence outcome is `aborted`, as with the limit.",
	}
	forEachReviewVariant(t, func(t *testing.T, name, variant, body string) {
		f := flat(body)
		if variant == "agents" {
			wantAll(t, f, agents[name])
			return
		}
		wantAll(t, f, claude[name]...)
		wantAll(t, f,
			"Only round 1 runs on Codex, so only round 1 has a fallback. Three triggers:",
			`"before the round (`+exit6+`)" or "in the middle of the round (exit 5)".`)
	})
}

// Classification: fails when the Claude variant stops running the retry itself as one inline
// background Codex call outside the sandbox, starts reading its log, retries exit 6, or the
// spec skill loses the mapping of the plan table's retry onto its own step 2.
func TestReviewRetryIsInlineCodex(t *testing.T) {
	cliFlags(t, []string{"review-run"}, "kind", "context")
	commands := map[string]string{
		"review-spec": `lighthouse review-run --kind spec <spec> 1 <ATTEMPT>/round-1.json none [max] > <ATTEMPT>/round-1.retry.log 2>&1; echo "exit=$?"`,
		"review-plan": `lighthouse review-run --kind plan <plan> 1 <ATTEMPT>/round-1.json none [max] --context <spec path> [--context <spec path 2>] > <ATTEMPT>/round-1.retry.log 2>&1; echo "exit=$?"`,
	}
	const phrase = "run the retry from step 5 once"
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			body := reviewVariant(t, name, "claude")
			f := flat(body)
			if n := strings.Count(f, command); n != 1 {
				t.Fatalf("the retry command occurs %d times, want 1", n)
			}
			before, _, _ := strings.Cut(f, command)
			i := strings.LastIndex(before, "**Only round 1 is a review**")
			if i < 0 {
				t.Fatal("no **Only round 1 is a review** before the retry command")
			}
			wantAll(t, before[i:], "`run_in_background: true`", "`dangerouslyDisableSandbox: true`")
			wantAll(t, f, "Read only the exit code, never `round-1.retry.log`.")
			switch name {
			case "review-plan":
				wantAll(t, f,
					"Never retry the retry: exit 6 is never retried, nor is any other exit.",
					"| none; "+phrase+" |")
			case "review-spec":
				wantAll(t, f, `Where that table says "`+phrase+`", that is the retry in step 2 here.`)
			}
		})
	}
}

// Classification: fails when the spec gate stops sorting what it takes over into the three
// piles, stops marking a small decision in the spec, lets the user's pile grow past four
// questions, or the Claude variant stops asking through the controller only if present.
func TestReviewSpecGateSortsThreePiles(t *testing.T) {
	ask := map[string]string{
		"claude": "In Claude Code one `AskUserQuestion`; in a controller run (if present) a numbered list as the last text (*Controller run*).",
		"agents": "Here: one message with a numbered list.",
	}
	for variant, body := range variants(t, reviewSkills["review-spec"]) {
		t.Run(variant, func(t *testing.T) {
			f := flat(body)
			wantAll(t, f,
				"- **Already answered** by spec or repo: the answer goes into the spec.",
				"- **Small**: a reasonable builder decides it without anything else being built. Decide it yourself and mark it in the spec with `*(own choice after spec review, <ID>)*`.",
				"- **The user's**: the answer changes what gets built. Only this pile becomes a question, bundled in one message: at most four, the most important first, each with a recommendation.",
				"You decide MINORs yourself (`minor_yes`/`minor_no`); in the sidecar each MINOR gets its own line.",
				ask[variant])
		})
	}
}

// Classification: fails when the Claude variant stops running round 1 as a blind pair (a clean
// work tree, a bare DONE line from Opus, no peeking before `merge`, blind decisions, `merge`,
// the reported model as `--shadow-model`), or a command or flag it names leaves the binary.
func TestReviewClaudeRunsThePairInRoundOne(t *testing.T) {
	cliFlags(t, []string{"review-pair", "blind"})
	cliFlags(t, []string{"review-pair", "merge"})
	cliFlags(t, []string{"plan-review-record", "start"}, "kind")
	cliFlags(t, []string{"plan-review-record", "write"}, "shadow-model", "reviewer-model", "review-mode")
	perSkill := map[string][]string{
		"review-spec": {
			"- `git status --porcelain` is empty before the dispatch. Otherwise `blind` refuses.",
			"- Do not read `pair.json`, the raw files or `round-1.codex.log` before `merge`.",
			"The reported ID goes unchanged as `--shadow-model` into the record (step 5).",
			"lighthouse plan-review-record start --kind spec <spec>",
			"- `decision`: `accept`, `reject`, `defer`, `accepted_open` (only at `round_limit`, set by `accept-open` in step 5, never by hand), `minor_yes` or `minor_no`.",
			"- Paired round 1 (there is a `round-1.sources.json`): `--review-mode codex --shadow-model <model Opus reported>`.",
		},
		"review-plan": {
			"First check that `git status --porcelain` is empty.",
			"Do not read `pair.json`, the raw files or `round-1.codex.log`.",
			"So take the reported ID over as `--shadow-model` (step 9) and never fill it in yourself.",
			"| paired (there is a `round-1.sources.json`) | `--review-mode codex --shadow-model <model Opus reported in round 1>` |",
		},
	}
	for name, pins := range perSkill {
		t.Run(name, func(t *testing.T) {
			f := flat(reviewVariant(t, name, "claude"))
			wantAll(t, f, pins...)
			wantAll(t, f,
				"to return only `DONE <path> model=<exact model ID from your own system prompt>`: no findings, no count.",
				"`lighthouse review-pair blind <ATTEMPT>`",
				"- Write `<ATTEMPT>/round-1.blind-decisions.json` instead of `round-1.decisions.json`, with the blind IDs (`A-f<i>`, `B-f<i>`).",
				"- Then run `lighthouse review-pair merge <ATTEMPT>` (`dangerouslyDisableSandbox: true`).",
				"lighthouse plan-review-record write <ATTEMPT>")
		})
	}
}

// Classification: fails when the agents variant stops saying the paired round 1 is Claude Code
// only, starts naming `review-pair blind`, stops starting the attempt with the kind of the review
// (`start` defaults to plan), or stops recording with its own skill directory.
func TestReviewAgentsPairIsClaudeCodeOnly(t *testing.T) {
	cliFlags(t, []string{"plan-review-record", "start"}, "kind")
	cliFlags(t, []string{"plan-review-record", "write"}, "skill-dir")
	start := map[string]string{
		"review-spec": "\nlighthouse plan-review-record start --kind spec <spec>\n```",
		"review-plan": "\nlighthouse plan-review-record start <plan>\n```",
	}
	record := map[string]string{
		"review-spec": "lighthouse plan-review-record write <ATTEMPT> \\ --outcome <converged|round_limit|aborted> --review-mode codex [--level max] \\ --skill-dir .agents/skills/lighthouse-review-spec",
		"review-plan": "lighthouse plan-review-record write <ATTEMPT> \\ --outcome <converged|round_limit|aborted> --review-mode codex \\ --spec <spec path> [--spec <spec path 2>] [--level max] \\ --skill-dir .agents/skills/lighthouse-review-plan",
	}
	for name, cmd := range record {
		t.Run(name, func(t *testing.T) {
			body := reviewVariant(t, name, "agents")
			f := flat(body)
			wantAll(t, f,
				"**Paired round 1: Claude Code only.** There Codex and an Opus subagent run side by side in round 1 by default, and Opus verifies rounds 2 and 3. Here that subagent does not exist; every round runs through Codex, as below.",
				cmd)
			wantAll(t, body, start[name])
			wantNone(t, body, "lighthouse review-pair blind")
		})
	}
}

// Classification: fails when the Claude variant stops pairing Codex with Opus in round 1 or
// stops dispatching Opus (model "opus", the verifier prompt from round 2 on) for the
// verification rounds.
func TestReviewClaudePairsAndVerifiesOnOpus(t *testing.T) {
	for _, name := range []string{"review-spec", "review-plan"} {
		t.Run(name, func(t *testing.T) {
			body := reviewVariant(t, name, "claude")
			wantAll(t, body, "\n#### Round 1: Codex and Opus\n", "\n## Opus dispatch\n")
			wantAll(t, flat(body),
				"Dispatch a `general-purpose` subagent with `model: \"opus\"` and the text between `---BEGIN PROMPT---` and `---END PROMPT---` from `reviewer-prompt.md` (round 1) or `verifier-prompt.md` (round ≥ 2),",
				"Always Opus, also when Codex still has budget: Codex is spared for round 1.")
		})
	}
}

// Classification: fails when the agents variant gains the controller run, the paired round 1
// or the Opus dispatch, the Claude variant loses one of them, or `codex_same_family` moves
// between the variants or leaves the review modes `write` accepts.
func TestReviewVariantsKeepTheirDifference(t *testing.T) {
	cliFlags(t, []string{"plan-review-record", "write"}, "review-mode")
	for _, name := range []string{"review-spec", "review-plan"} {
		t.Run(name, func(t *testing.T) {
			claude, agents := reviewVariant(t, name, "claude"), reviewVariant(t, name, "agents")
			for _, only := range []string{"## Controller run", "Round 1: Codex and Opus", "## Opus dispatch"} {
				if !strings.Contains(claude, only) {
					t.Errorf("claude lacks %q", only)
				}
				if strings.Contains(agents, only) {
					t.Errorf("agents has %q", only)
				}
			}
			wantAll(t, agents, "`--review-mode codex_same_family`")
			wantNone(t, claude, "codex_same_family")
		})
	}
	// `write` accepts codex_same_family: the usage line names it.
	if !strings.Contains(reviewModeUsage(t), "codex_same_family") {
		t.Error("plan-review-record write does not name codex_same_family as a review mode")
	}
}

// reviewModeUsage is the usage text of `plan-review-record write --review-mode`.
func reviewModeUsage(t *testing.T) string {
	t.Helper()
	root := cli.NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	cmd, _, err := root.Find([]string{"plan-review-record", "write"})
	if err != nil {
		t.Fatal(err)
	}
	f := cmd.Flags().Lookup("review-mode")
	if f == nil {
		t.Fatal("no --review-mode")
	}
	return f.Usage
}

// Classification: fails when the round limit stops recording the open points with `accept-open`
// and carrying on without a question, the skill misreads the "nothing to accept" message the
// binary prints, or drops the ACCEPT-OPEN FAILED path.
func TestReviewRoundLimitRecordsAndCarriesOn(t *testing.T) {
	cliFlags(t, []string{"plan-review-record", "accept-open"}, "doc")
	doc := map[string]string{"review-spec": "spec", "review-plan": "plan"}
	forEachReviewVariant(t, func(t *testing.T, name, _, body string) {
		d := doc[name]
		wantAll(t, flat(body),
			"1. `lighthouse plan-review-record accept-open <ATTEMPT> --doc <"+d+">`",
			"puts the block `heading:open_after_review` at the bottom of the "+d+"; an existing block is replaced, never doubled.",
			"Exit 2 with on stderr `"+review.ErrNoOpenPoints.Error()+"` = nothing is open: the outcome is then `converged`, not `round_limit`.",
			"Exit 4 (`ACCEPT-OPEN FAILED: …`): report the message literally, repair the cause (path, sidecar) and run again.",
			"If it still fails, put the open points in the handoff and go on to the next phase; never edit the sidecar or the document by hand, never a question.")
	})
	for variant, body := range variants(t, reviewSkills["review-plan"]) {
		t.Run("review-plan/"+variant+"/spec-in-round-3", func(t *testing.T) {
			wantAll(t, flat(body),
				"Exception: in round 3 a `spec` decision does not stop the chain; the point goes through `accept-open` like any other open point |")
		})
	}
}

// Classification: fails when the PR template loses the `## Open review points` heading under
// which `plan-review-record pr-open-points` writes the open points, or that command changes the
// heading.
func TestReviewPRTemplateKnowsTheOpenReviewPointsHeading(t *testing.T) {
	dir := t.TempDir()
	toml := filepath.Join(dir, "lighthouse.toml")
	writeFile(t, toml, "[project]\nlanguage = \"en\"\n")
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(packRoot, "core", "headings.toml"))
	block, err := review.RenderOpen(3, []review.Point{{FindingID: "r3-f1", Severity: "MAJOR", Section: "S", Problem: "P"}}, "en")
	if err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "spec.md")
	writeFile(t, doc, "# Spec\n\n"+block)
	out, err := review.PROpenPoints([]string{doc}, dir)
	if err != nil {
		t.Fatal(err)
	}
	heading, _, _ := strings.Cut(out, "\n")
	if !strings.HasPrefix(heading, "## ") {
		t.Fatalf("pr-open-points starts with %q, not a heading", heading)
	}
	template := readPack(t, "core/templates/pull_request_template.md")
	if !strings.Contains(template, "\n"+heading+"\n") {
		t.Errorf("the PR template has no heading line %q", heading)
	}
}

// Classification: fails when the controller-run section of the Claude variant loses its
// presence guard, its recognition or the rule set it applies, moves below the workflow, or the
// controller table stops carrying the round-limit key the section names.
func TestReviewControllerRunSectionIsGuarded(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "lighthouse.toml")
	writeFile(t, cfg, "[forge]\nrepo = \"o/r\"\n\n[controller]\nbij_rondegrens = \"probe\"\n")
	repo, err := repoconfig.LoadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Controller == nil || repo.Controller.AtRoundLimit != "probe" {
		t.Fatalf("[controller] bij_rondegrens is not read: %+v", repo.Controller)
	}
	const heading = "## Controller run (only if present)"
	perSkill := map[string][]string{
		"review-spec": {
			"5. At `converged` or `round_limit`: `/lighthouse-write-handoff` with `next: /superpowers:writing-plans <absolute spec path>`.",
			"6. At `aborted`: first the record (`--outcome aborted`, step 5), then a `🤖 heading:kind_question` with the open points and two options.",
			"*On to writing-plans with the open points*: handoff `next: /superpowers:writing-plans <absolute spec path>`, open points under the handoff's note field. No second `write`, no `accept-open`.",
			"*Continue interactively*: handoff `next: /superpowers:executing-plans <absolute spec path>`",
			"*Another round* is no option: that clashes with the round limit, and you never reuse an old attempt.",
			"3. Step 3: you decide the table and the piles yourself, also every MINOR, and report the choices.",
			"7. Background tasks: the paragraph **Controller run (if present):** in `lighthouse-review-plan`, *Round 1: Codex and Opus*.",
			"1. No `codex`: *Fallback without Codex* at once, without a question (exit 5 and 6 do that already). 2.",
		},
		"review-plan": {
			"*Adjust the spec*: the resumed run adjusts the spec according to the answer, commits, and posts the handoff `next: /lighthouse-review-spec <absolute spec path>`",
			"*Carry on with the point open*: the review goes on and the point goes into the handoff to SDD as a note.",
			"In the sidecar the point gets `\"decision\": \"reject\"` with `\"reason\": \"open on request (heading:kind_question)\"`:",
			"The run never changes the spec materially by itself.",
			"*On to SDD with the open points* (handoff `next: /superpowers:subagent-driven-development <absolute plan path>`, open points as a note, no second `write`, no `accept-open`)",
			"a fix with which the plan departs from the spec, a `spec` decision that is more than a clarification, and doubt whether something contradicts the spec.",
			"No option *another round*.",
			"1. No `codex`: *Fallback without Codex* at once, without a question. 2.",
		},
	}
	for name, pins := range perSkill {
		t.Run(name, func(t *testing.T) {
			body := reviewVariant(t, name, "claude")
			wantOrder(t, body, "\n"+heading+"\n", "\n## Workflow\n")
			s := flat(section(t, body, "\n"+heading+"\n", "\n## "))
			wantAll(t, s, pins...)
			wantAll(t, s,
				"It is present when `lighthouse.toml` has a `[controller]` table (`grep -q '^\\[controller\\]' lighthouse.toml`); otherwise this section is skipped.",
				"Recognition: the prompt ends on `(issue #<nr>, controller-run)`, or `LIGHTHOUSE_CONTROLLER_RUN` is set in the environment;",
				"`🤖 heading:kind_question`",
				"`controller.bij_rondegrens` in `lighthouse.toml`")
		})
	}
	t.Run("review-plan/round-1-wait-rule", func(t *testing.T) {
		body := reviewVariant(t, "review-plan", "claude")
		round1 := section(t, body, "#### Round 1: Codex and Opus", "### 6. Gate")
		wantAll(t, flat(round1),
			"**Controller run (if present):** end no answer while a reviewer runs, also not the verifier from round 2 on or the retry; wait for every notification.")
	})
	t.Run("review-spec/next-window", func(t *testing.T) {
		wantAll(t, flat(reviewVariant(t, "review-spec", "claude")),
			"Interactively, `superpowers:writing-plans` runs only in the next window (a fixed seam); for a controller run (if present) see *Controller run*.")
	})
	for _, name := range []string{"review-spec", "review-plan"} {
		t.Run(name+"/agents", func(t *testing.T) {
			wantAll(t, flat(reviewVariant(t, name, "agents")),
				"Controller runs exist only in Claude Code (the controller, if present, starts `claude -p`); here *report and stop* holds headless too.")
		})
	}
}

// Classification: fails when a review skill names the source repo, its host, paths or scripts,
// or a source issue number, or stops naming the always-on host by its `host.toml`.
func TestReviewIsDetachedFromTheSourceRepo(t *testing.T) {
	host := map[string]string{
		"review-spec": "| 3 | not on the always-on host (`host.toml`): no record | report loudly that this review falls outside the measurement, on to closing |",
		"review-plan": "| 3 | not on the always-on host (`host.toml`): no record | report loudly that this chain falls outside the measurement, on to step 10 |",
	}
	forEachReviewVariant(t, func(t *testing.T, name, _, body string) {
		wantNone(t, body, "Beelink", "experiments/", "scripts/controller_launch.sh", "developer/tenets.md")
		if m := regexp.MustCompile(`\(#\d+\)|#\d{3,}`).FindString(body); m != "" {
			t.Errorf("names issue number %s", m)
		}
		wantAll(t, body, host[name])
	})
}

// Classification: fails when a review skill writes the spec or plan directory literally
// instead of reading it from `lighthouse config`.
func TestReviewDirectoriesFromConfig(t *testing.T) {
	for _, key := range []string{"paths.specs", "paths.plans"} {
		if !config.IsKnownKey(key) {
			t.Errorf("config does not know %s", key)
		}
	}
	for variant, body := range variants(t, reviewSkills["review-spec"]) {
		t.Run("review-spec/"+variant, func(t *testing.T) {
			wantAll(t, flat(body),
				"No argument → the newest file in the spec directory (`lighthouse config paths.specs`), and confirm which one you take.")
			wantNone(t, body, "developer/planning/specs/")
		})
	}
	for variant, body := range variants(t, reviewSkills["review-plan"]) {
		t.Run("review-plan/"+variant, func(t *testing.T) {
			wantAll(t, flat(body), "A fixed seam: plan committed in the plan directory (`lighthouse config paths.plans`) → this skill →")
			wantNone(t, body, "developer/planning/plans/")
		})
	}
}

// Classification: fails when pack/core/permissions.json stops allowing exactly the review
// runner and the agent audit, or either subcommand leaves the binary: without the rule an
// unattended background round waits on an approval prompt.
func TestReviewPermissionsCoverTheRunner(t *testing.T) {
	cliFlags(t, []string{"review-run"})
	cliFlags(t, []string{"agent-audit"})
	var perms struct {
		Allow []string `json:"allow"`
	}
	if err := json.Unmarshal([]byte(readPack(t, "core/permissions.json")), &perms); err != nil {
		t.Fatal(err)
	}
	want := []string{"Bash(lighthouse review-run:*)", "Bash(lighthouse agent-audit:*)"}
	if !reflect.DeepEqual(perms.Allow, want) {
		t.Errorf("allow = %q, want %q", perms.Allow, want)
	}
}

// Classification: fails when a Dutch sentence enters a review skill or its shared prompts.
func TestReviewIsEnglish(t *testing.T) {
	forEachReviewVariant(t, func(t *testing.T, _, _, body string) {
		wantNone(t, strings.ToLower(body), dutchWords...)
	})
	for name, dir := range reviewSkills {
		for _, f := range []string{"reviewer-prompt.md", "verifier-prompt.md"} {
			t.Run(name+"/"+f, func(t *testing.T) {
				wantNone(t, strings.ToLower(readPack(t, filepath.Join(dir, f))), dutchWords...)
			})
		}
	}
}

// reviewRunCall matches one `lighthouse review-run` call up to the end of its code span, its
// redirect or its line.
var reviewRunCall = regexp.MustCompile("lighthouse review-run ([^`>;\n]*(?:\\\\\n[^`>;\n]*)*)")

// Classification: fails when a review skill calls the runner without its kind, without
// `--verify` for the verification round, the agents variant without its own skill directory,
// or with a flag `lighthouse review-run` does not have.
func TestReviewSkillCallsTheRunnerWithKindAndVerify(t *testing.T) {
	kind := map[string]string{"review-spec": "spec", "review-plan": "plan"}
	forEachReviewVariant(t, func(t *testing.T, name, variant, body string) {
		k := kind[name]
		f := flat(body)
		if variant == "claude" {
			wantAll(t, f, "lighthouse review-run --kind "+k+" ", "lighthouse review-run --kind "+k+" --verify ")
		} else {
			dir := "--skill-dir .agents/skills/lighthouse-review-" + k
			wantAll(t, f, "lighthouse review-run --kind "+k+" "+dir, "lighthouse review-run --kind "+k+" --verify "+dir)
		}
		calls := reviewRunCall.FindAllStringSubmatch(body, -1)
		if len(calls) == 0 {
			t.Fatal("no lighthouse review-run call")
		}
		var flags []string
		for _, c := range calls {
			for _, m := range regexp.MustCompile(`(?:^|\s)--([a-z][a-z-]*)`).FindAllStringSubmatch(c[1], -1) {
				if m[1] != "help" { // cobra adds --help itself
					flags = append(flags, m[1])
				}
			}
		}
		if len(flags) == 0 {
			t.Fatal("no flag on any lighthouse review-run call")
		}
		cliFlags(t, []string{"review-run"}, flags...)
	})
}
