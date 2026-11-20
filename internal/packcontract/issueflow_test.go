package packcontract

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The issue skills: route-issue, start-issue and write-handoff. What lives only in skill text
// and no runtime enforces: gate 0 reads the blocker from the relation, start-issue reports the
// lines `lighthouse measure-start-issue-kpi` counts and runs the scout at 4b/4c, and the handoff
// writes the tokens `lighthouse protocol` reads.

const (
	routeIssue   = "core/skills/route-issue"
	startIssue   = "core/skills/start-issue"
	writeHandoff = "core/skills/write-handoff"
)

// wantAll fails for every phrase text does not contain.
func wantAll(t *testing.T, text string, phrases ...string) {
	t.Helper()
	for _, p := range phrases {
		if !strings.Contains(text, p) {
			t.Errorf("missing %q", p)
		}
	}
}

// wantNone fails for every phrase text contains.
func wantNone(t *testing.T, text string, phrases ...string) {
	t.Helper()
	for _, p := range phrases {
		if strings.Contains(text, p) {
			t.Errorf("contains %q", p)
		}
	}
}

// wantRe fails for every pattern that matches nowhere in text.
func wantRe(t *testing.T, text string, patterns ...string) {
	t.Helper()
	for _, p := range patterns {
		if !regexp.MustCompile(p).MatchString(text) {
			t.Errorf("no match for %s", p)
		}
	}
}

// wantOrder fails when a piece is missing or the pieces do not appear in this order.
func wantOrder(t *testing.T, text string, pieces ...string) {
	t.Helper()
	last := -1
	for _, p := range pieces {
		i := strings.Index(text, p)
		if i < 0 {
			t.Errorf("missing %q", p)
			return
		}
		if i < last {
			t.Errorf("%q comes before the piece preceding it", p)
		}
		last = i
	}
}

// lineStarting returns the first line that starts with prefix (after leading spaces).
func lineStarting(t *testing.T, text, prefix string) string {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), prefix) {
			return line
		}
	}
	t.Fatalf("no line starts with %q", prefix)
	return ""
}

// --- route-issue -------------------------------------------------------------------

// Classification: fails when gate 0 stops reading the blocker from `lighthouse forge issue
// blockers` or stops pointing at the per-forge blockers section.
func TestRouteIssueGate0ReadsTheRelation(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			gate0 := section(t, body, "### Gate 0", "### Gate 1")
			wantAll(t, gate0, "lighthouse forge issue blockers <nr>")
			wantRe(t, gate0, `\.\./lighthouse-forge/<kind>\.md §blockers\b`)
		})
	}
}

// Classification: fails when gate 0 lets an issue with an open blocker but no blocked label slip
// through, or drops the verdicts "label missing" and "unknown".
func TestRouteIssueGate0StopsWithoutBlockedLabel(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			gate0 := section(t, body, "### Gate 0", "### Gate 1")
			wantAll(t, gate0, "open blocker in the `issue blockers` list", "**label missing**", "**unknown**")
		})
	}
}

// Classification: fails when gate 0 loses the controller-run exception, stops treating
// labels.blocked_external as an unconditional stop, or lets the router post a handoff.
func TestRouteIssueGate0RoutesOnInAControllerRun(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			gate0 := section(t, body, "### Gate 0", "### Gate 1")
			wantAll(t, gate0, "**Controller run**", "`labels.blocked_external` always stops")
			wantNone(t, gate0, "/lighthouse-write-handoff")
		})
	}
}

// Classification: fails when a router line (label row, hard-stop paragraph, non-negotiable)
// contradicts the controller-run exception of gate 0, or extends it to labels.blocked_external.
func TestRouteIssueInvariantsKnowTheControllerRun(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			for _, prefix := range []string{"| `labels.blocked_issue`", "- ✅ `labels.blocked_*`"} {
				if !strings.Contains(lineStarting(t, body, prefix), "controller run") {
					t.Errorf("line %q… does not name the controller run", prefix)
				}
			}
			hardStop := section(t, body, "Only the two blocked labels", "## Non-negotiable")
			wantAll(t, hardStop, "controller run")
			if strings.Contains(lineStarting(t, body, "| `labels.blocked_external`"), "controller run") {
				t.Error("labels.blocked_external row names the controller run; it stays an unconditional stop")
			}
		})
	}
}

// Classification: fails when route-issue changes the gate line the KPI counts, loses a gate, its
// "never calls the route" rule, or starts calling the scout or setting up a worktree.
func TestRouteIssueIsReadOnlyAndReportsOneLine(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body,
				"#<nr> heading:gate_line <n>[<a|b|c>]: <route> — <reason>",
				"### Gate 0", "### Gate 1", "### Gate 2", "### Gate 3", "### Gate 4",
				"never calls the route")
			wantNone(t, body, "issue-impact-scout", "using-git-worktrees")
		})
	}
}

// Classification: fails when the route-issue description no longer parses as YAML up to its
// NOTE clause, e.g. an unquoted ` #610` that YAML reads as a comment.
func TestRouteIssueDescriptionSurvivesTheYAMLParser(t *testing.T) {
	const note = "NOTE — actually starting on an issue (worktree, board, execution) is lighthouse-start-issue."
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			var front struct {
				Description string `yaml:"description"`
			}
			if err := yaml.Unmarshal([]byte(frontmatter(t, body)), &front); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(front.Description, note) {
				t.Errorf("description does not end on the NOTE clause: %q", front.Description)
			}
		})
	}
}

var labelRowRe = regexp.MustCompile("(?m)^\\| `labels\\.")

// Classification: fails when gate 4 stops sending an open fog question from the milestone to c,
// or the blocking-label table gains or loses a label row.
func TestRouteIssueSendsAnOpenFogQuestionTo4c(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			gate4 := section(t, body, "### Gate 4", "## A blocking label")
			wantAll(t, gate4, "heading:milestone_not_yet_decided", `touches open question "`, "→ #<nr>")
			labels := section(t, body, "## A blocking label", "## Non-negotiable")
			if n := len(labelRowRe.FindAllString(labels, -1)); n != 6 {
				t.Errorf("%d label rows, want 6", n)
			}
		})
	}
}

// Classification: fails when route-issue stops reading one of the six labels from config, or
// names a literal label (`3-blocked`, `3-needs…`).
func TestRouteIssueReadsLabelsFromConfig(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			for _, key := range []string{"blocked_issue", "blocked_external", "needs_shaping", "needs_decision", "needs_hands", "needs_testing"} {
				wantRe(t, body, `lighthouse config labels\.`+key+`\b`)
			}
			wantNone(t, body, "3-blocked", "3-needs")
		})
	}
}

// Classification: fails when route-issue stops naming `lighthouse measure-start-issue-kpi` as
// the reader of its gate line.
func TestRouteIssueNamesTheSubcommand(t *testing.T) {
	for variant, body := range variants(t, routeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "`lighthouse measure-start-issue-kpi` counts the gate line")
		})
	}
}

// --- start-issue -------------------------------------------------------------------

// bashBlock returns the first ```bash block after heading.
func bashBlock(t *testing.T, body, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, heading)
	if !ok {
		t.Fatalf("no %q in the text", heading)
	}
	return section(t, rest, "```bash", "```")
}

// Classification: fails when step 4 stops holding a blocked execution at gates 1, 2 and 4a with
// a `next: /lighthouse-start-issue` handoff, or forgets that `stack:`/`blocker:` accounts for it.
func TestStartIssueHoldsABlockedExecution(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step4 := section(t, body, "### 4.", "#### Scout")
			wantAll(t, step4, "next: /lighthouse-start-issue", "`stack:` or `blocker:`",
				"Gate `1`, `2` or `4a`: do not execute.")
		})
	}
}

// Classification: fails when step 4 stops counting the old `stapel:`/`blokkade:` keys of a
// running issue's started comment as the same as `stack:`.
func TestStartIssueReadsLegacyStack(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step4 := section(t, body, "### 4.", "#### Scout")
			wantAll(t, step4, "`stack:`", "`stapel:`", "`blokkade:`", "heading:kind_started")
		})
	}
}

// Classification: fails when step 4 stops calling lighthouse-route-issue for the route.
func TestStartIssueDelegatesStep4(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step4 := section(t, body, "### 4.", "#### Scout")
			wantAll(t, step4, "Call `lighthouse-route-issue` with this number")
		})
	}
}

// Classification: fails when the already-done check loses its one-call recipe, one of its three
// report lines, its stop and choice, or starts closing the issue itself.
func TestStartIssueAsksAlreadyDoneBeforeTheWorktree(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step2 := section(t, body, "### 2.", "### 3.")
			recipe := bashBlock(t, body, "**Already done?**")
			wantRe(t, recipe, `lighthouse config forge\.default_branch\b`, `lighthouse config paths\.search_dirs\b`)
			wantAll(t, recipe, "FETCH FAILED")
			wantAll(t, step2,
				"already done? no — searched for ",
				"does not cover the AC",
				"carry on anyway",
				"Controller run (if present)",
				"already done? likely — ",
				"demonstrably covers",
				"one Bash call",
				"/lighthouse-close-issue <nr>",
				"already done? not measurable — fetch failed")
			wantNone(t, step2, "gh issue close", "wontfix")
		})
	}
}

// Classification: fails when start-issue stops reading the DoD, no-gos and open questions from
// the milestone, stops reporting `fog:`, or stops passing the open questions to the scout.
func TestStartIssueReadsFogFromTheMilestone(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step2 := section(t, body, "### 2.", "### 3.")
			wantAll(t, step2, "### heading:milestone_not_yet_decided", "heading:milestone_dod",
				"heading:milestone_no_gos", "fog: ")
			scout := section(t, body, "#### Scout", "Report the trigger")
			wantAll(t, scout, "heading:milestone_not_yet_decided")
		})
	}
}

// Classification: fails when start-issue stops reading the axis prefixes from config, or names
// a literal prefix (`1-loc`, `2-area`).
func TestStartIssueReadsAxesFromConfig(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step2 := section(t, body, "### 2.", "### 3.")
			wantRe(t, step2, `lighthouse config labels\.location_prefix\b`, `lighthouse config labels\.area_prefix\b`)
			wantNone(t, body, "1-loc", "2-area")
		})
	}
}

// Classification: fails when the board step stops calling `lighthouse board-start`, or stops
// treating a missing [forge.board] (rc 1, `board: skipped`) as no stop.
func TestStartIssueSkipsBoardWithoutConfig(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			step3 := section(t, body, "### 3.", "### 4.")
			wantRe(t, step3, `lighthouse board-start <nr>`, `lighthouse config forge\.board\.project_number\b`)
			wantAll(t, step3, "rc 1", "board: skipped", "is not a stop")
		})
	}
}

// Classification: fails when start-issue drops the rule that the plan review is skipped only on
// a decision of the user, or the three-way choice after a committed plan.
func TestStartIssueForbidsSkippingWithoutADecision(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body,
				"skip the plan review without a decision of the user",
				"That lets the user choose: plan review, controller (SDD, if present) or interactive (`/superpowers:executing-plans`)")
		})
	}
}

// Classification: fails when start-issue stops naming the project spec and plan paths from config.
func TestStartIssueNamesTheProjectPaths(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantRe(t, body, `lighthouse config paths\.specs\b`, `lighthouse config paths\.plans\b`)
		})
	}
}

// Classification: fails when the KPI table loses one of the six `lighthouse
// measure-start-issue-kpi` subcommands.
func TestStartIssueKeepsTheMeasurements(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			for _, sub := range []string{"leadin", "worktree-first", "mistrigger", "paths", "gate", "scout"} {
				wantRe(t, body, "`lighthouse measure-start-issue-kpi "+regexp.QuoteMeta(sub)+"`")
			}
		})
	}
}

// Classification: fails when start-issue stops calling the scout by its subagent type.
func TestStartIssueMentionsTheScout(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "subagent_type: lighthouse-issue-impact-scout")
		})
	}
}

// Classification: fails when one of the three scout report lines that `lighthouse
// measure-start-issue-kpi scout` counts changes.
func TestStartIssueCarriesTheThreeReportLines(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "Scout: lighthouse-issue-impact-scout", "heading:scout_skipped — ", "Scout: failed — ")
		})
	}
}

// Classification: fails when the scout is no longer bound to gates 4b and 4c, or gate 4a is no
// longer named as a gate that never calls it.
func TestStartIssueBindsTheScoutToGate4bAnd4c(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			scout := section(t, body, "#### Scout", "## Non-negotiable")
			wantAll(t, scout, "Gates `4b` and `4c` hand on", "Gates `0`, `1`, `2` and `4a` never call it")
		})
	}
}

// Classification: fails when skipping the scout no longer demands a concrete reason ("looks
// simple" is not a valid reason).
func TestStartIssueEscapeHatchDemandsAConcreteReason(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, strings.ToLower(body), "looks simple", "not a valid reason")
		})
	}
}

// Classification: fails when a failed scout no longer lets the route go on.
func TestStartIssueFailurePathIsFailOpen(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "The route continues with the normal context exploration.")
		})
	}
}

// Classification: fails when the fixed handover reference for the IMPACT PACK changes.
func TestStartIssueHandoverHasOneDemand(t *testing.T) {
	for variant, body := range variants(t, startIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "IMPACT PACK: in context, do not repeat")
		})
	}
}

// Classification: fails when the Agents variant stops saying the scout persona is missing there
// (and not to simulate it), or the Claude variant gains that section.
func TestStartIssueAgentsVariantNamesTheMissingPersona(t *testing.T) {
	v := variants(t, startIssue)
	wantAll(t, v["agents"], "##### Outside Claude Code", "**not available**", "**not simulate**")
	wantNone(t, v["claude"], "##### Outside Claude Code")
}

// --- write-handoff -----------------------------------------------------------------

// Classification: fails when the blocked restart of start-issue no longer comes from the issue's
// own worktree, where `/lighthouse-start-issue` otherwise starts from main.
func TestHandoffBlockedRestartFromItsOwnWorktree(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			step3 := section(t, body, "3. **`next:`", "4. **`when:`**")
			wantAll(t, step3, "main checkout and `branch:` `main`", "— blocker #<x> open",
				"own worktree and branch of the issue")
		})
	}
}

// Classification: fails when the rule for where a handoff belongs leaves the skill or moves
// below the routing table.
func TestHandoffCarriesTheRule(t *testing.T) {
	const rule = "A handoff belongs where the next step needs a clean context: the previous step used up a " +
		"lot of context, or the next step must judge independently. A handoff also belongs where " +
		"the chain waits for an external event, such as red CI or a merge."
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, flat(body), rule, "4. **Decide the first step")
		})
	}
}

// Classification: fails when a finished build with a green last commit no longer routes to
// `/lighthouse-write-pr` with `offer`, in either routing table.
func TestHandoffRoutesAFinishedBuildToWritePR(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			step4 := section(t, body, "4. **Decide the first step", "5. **Name the pitfalls")
			var rows []string
			for line := range strings.SplitSeq(step4, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "| build done") {
					rows = append(rows, strings.TrimSpace(line))
				}
			}
			if len(rows) != 1 {
				t.Fatalf("%d build-done rows in step 4, want 1", len(rows))
			}
			if !strings.HasPrefix(rows[0], "| build done, last commit green, no PR yet | `/lighthouse-write-pr <nr>` |") ||
				!strings.HasSuffix(rows[0], "| `offer` |") {
				t.Errorf("build-done row: %q", rows[0])
			}
			step3 := section(t, body, "3. **`next:` is exactly one command", "4. **`when:`**")
			wantAll(t, step3, "| build done, last commit green | `/lighthouse-write-pr <nr>` |")
		})
	}
}

// Classification: fails when the build-done row stops demanding V2b and the push before the
// handoff is posted or offered.
func TestHandoffBuildRowPushesAfterV2bFirst(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			step4 := flat(section(t, body, "4. **Decide the first step", "5. **Name the pitfalls"))
			wantAll(t, step4, "step V2b of `/lighthouse-write-pr` and `git push -u origin HEAD`",
				"before you post or offer")
		})
	}
}

// Classification: fails when the PR-open row stops saying the close check starts only after the
// merge of the Refs PR, not from the handoff.
func TestHandoffPROpenRowNamesTheCloseCheck(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			step3 := section(t, body, "3. **`next:` is exactly one command", "4. **`when:`**")
			row := lineStarting(t, step3, "| PR open")
			wantAll(t, row, "the controller does not start it from the handoff", "after the merge of the `Refs` PR")
		})
	}
}

// planSeam are the sentences of the plan seam that steer a choice, an order or a stop. The
// anchor, the question, the advice and the interactive block are pinned in order by
// TestHandoffPlanSeamBlocksBeforeTheQuestion.
var planSeam = []string{
	"no `AskUserQuestion`",
	"[<file name>](<absolute path>)",
	"end the turn",
	"A controller run does not ask this",
	"skipped on a decision of the user",
	"`/lighthouse-review-spec` on that spec | `default` |",
	"`/superpowers:writing-plans` on that spec | `no` |",
	"`/lighthouse-review-plan` on that plan | `choice` |",
	"`/superpowers:executing-plans` | `offer` |",
	"`/lighthouse-fix-pr <pr>` | `offer` |",
	"At `choice` you print two blocks in step 6 before the question: first *review*, then *interactive*.",
	"then ask the question only with *review* or *interactive*, without *controller*",
	"At `no` you do not read the board status; if the issue is on `Controller` anyway, it stays there until the user moves it.",
	"If the user wants the plan review itself through the controller: `next: /lighthouse-review-plan <plan>` and steps 5 and 6.",
	"When in doubt: **review**.",
	"This also holds when the issue is already on `Controller`",
}

// Classification: fails when the plan seam loses a sentence that steers the choice in chat text:
// the controller column values, the no-dialog rule, the stop, the advice default.
func TestHandoffPlanSeamAsksInChatText(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(body), planSeam...)
		})
	}
}

// Classification: fails when the plan seam no longer prints the review and interactive blocks
// before the plan link, the advice and the question.
func TestHandoffPlanSeamBlocksBeforeTheQuestion(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			para := section(t, body, "**Exception: a plan, not reviewed.**", "A controller run does not ask this")
			wantOrder(t, para,
				"`First step: /lighthouse-review-plan <plan>`",
				"`First step: /superpowers:executing-plans <plan>`",
				"Plan is in",
				"My advice:",
				"Review, controller or interactive?")
		})
	}
}

// Classification: fails when the plan-seam exception moves below the rule that posts at once on
// `Controller`.
func TestHandoffPlanSeamExceptionBeforeControllerPost(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, body, "**Exception: a plan, not reviewed.**",
				"Only at `offer` (never at `default` above): if it is already on `Controller`, post at once")
		})
	}
}

// Classification: fails when step 3 stops posting a spec or plan anchor with `lighthouse
// post-artefact` outside the sandbox, before the handoff comment, without blocking on an error.
func TestHandoffPostArtefact(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(body),
				"lighthouse post-artefact <nr> <absolute path>",
				"with `dangerouslyDisableSandbox: true`:",
				"also in controller mode (if present) before the `🤖 heading:kind_handoff`",
				"An error does not block the handoff",
				"artefact: <the line from lighthouse post-artefact>")
		})
	}
}

// Classification: fails when a controller run may post a handoff after its own `lighthouse
// ask` question, which would make the handoff the last turn and the answer resume nothing.
func TestHandoffPostsNothingAfterAQuestion(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(body), "except when this run posted a question with `lighthouse ask`")
		})
	}
}

// Classification: fails when `lighthouse post-artefact` leaves workflow step 3.
func TestHandoffPostArtefactIsInStep3(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, body, "3. **Decide the anchor.**", "lighthouse post-artefact <nr>", "4. **Decide the first step")
		})
	}
}

// Classification: fails when the pause form loses its `.handoff.md` path argument, the atomic
// write, the chat copy, or a field the resume needs (uncommitted work, resume moment, job ID).
func TestHandoffPauseForm(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(body),
				"## Pause form: /lighthouse-write-handoff <path>",
				"an absolute path that ends in `.handoff.md`",
				"`mkdir -p` the directory",
				"then `mv` it onto the path",
				"outside the sandbox",
				"also in the chat",
				"what is uncommitted",
				"the resume moment",
				"job ID",
				"Without a path the skill works as described below.")
		})
	}
}

// Classification: fails when the pause form stops skipping the side effects of a phase
// transition (post-artefact, controller mode, the question, /clear), also in a controller run.
func TestHandoffPauseFormSkipsPhaseSideEffects(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			pause := flat(section(t, body, "## Pause form", "## Workflow"))
			wantAll(t, pause, "lighthouse post-artefact", "Controller mode", "no question to the user", "/clear",
				"LIGHTHOUSE_CONTROLLER_RUN")
		})
	}
}

// Classification: fails when the pause form stops pushing the branch (never the default branch,
// not blocking on failure) before it writes the block.
func TestHandoffPauseFormPushesTheBranch(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			pause := flat(section(t, body, "## Pause form", "## Workflow"))
			wantAll(t, pause, "never the default branch", "A failed push does not block the pause", "`Watch out:`")
			wantOrder(t, pause, "git push -u origin HEAD", "Write the block to the path")
		})
	}
}

// Classification: fails when controller mode stops pushing before the handoff comment, also
// when post-artefact posted nothing.
func TestHandoffControllerModePushesBeforeThePost(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			mode := flat(section(t, body, "## Controller mode", "## Output format"))
			wantAll(t, mode, "also when `lighthouse post-artefact` posted nothing", "never the default branch")
			wantOrder(t, mode, "git push -u origin HEAD", "lighthouse forge issue comment <nr>")
		})
	}
}

// Classification: fails when the native lane (`lane: native`, started on Sonnet, offered as
// *Yes, native*) leaves the skill.
func TestHandoffKnowsLaneNative(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "lane: native", "the controller starts it on Sonnet", "Yes, native")
		})
	}
}

// Classification: fails when the posted block stops carrying the tokens `lighthouse protocol`
// reads (`anchor:`, `next:`, `when: now`) under the kind_handoff heading.
func TestHandoffWritesProtocolTokens(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			post := section(t, body, "5. **Post**", "6. **Status")
			wantAll(t, post, "anchor: <anchor>", "next: <command> <argument>", "when: now",
				"lighthouse headings kind_handoff")
			wantAll(t, body, "`when:`** is `now`, unless the user chooses or asks for `night`")
		})
	}
}

// Classification: fails when board status and the comment stop going through `lighthouse forge`,
// the post is no longer chained to the write of a defined scratch file, or the host check and
// the controller-run environment variable leave the skill.
func TestHandoffBoardAndHostFromConfig(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body,
				"lighthouse forge issue status <nr>`",
				"lighthouse forge issue status <nr> Controller",
				"lighthouse forge issue comment <nr> --body-file <scratch>/handoff.md",
				"} > <scratch>/handoff.md && lighthouse forge issue comment",
				"lighthouse config host-check",
				"the always-on host (`host.toml`)",
				"LIGHTHOUSE_CONTROLLER_RUN")
			wantAll(t, flat(body), "`<scratch>` is the session's scratchpad directory, otherwise a `mktemp -d`")
		})
	}
}

// Classification: fails when the block may be printed before an `AskUserQuestion`, whose dialog
// hides it, or the offer stops asking before it prints.
func TestHandoffPrintsTheBlockAfterTheQuestion(t *testing.T) {
	for variant, body := range variants(t, writeHandoff) {
		t.Run(variant, func(t *testing.T) {
			step6 := flat(section(t, body, "6. **Print the block", "7. **Read your own block back"))
			wantAll(t, step6, "never before an `AskUserQuestion`", "in your last message of the turn")
			offer := flat(section(t, body, "Only at `offer`", "3. **`next:` is exactly one command"))
			wantAll(t, offer, "Ask before you print the block")
		})
	}
}
