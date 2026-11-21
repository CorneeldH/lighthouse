package packcontract

import (
	"regexp"
	"strings"
	"testing"
)

// close-issue: what lives only in skill text. Step 0 waits for the merge, B2a runs the Evidence
// tables of merged Refs PRs, entry C runs only the evidence runner on a fresh default branch and
// reopens fail-closed, the released issues are checked against the installed unblock lane, and the
// worktree cleanup follows `lighthouse worktree-audit` without a question. The comment filter and
// the Refs test of both jq filters are bound to prbody.RefsTargets in internal/prbody.

const closeIssue = "core/skills/close-issue"

// closeIssueSection is the text from heading up to the next level-2 heading.
func closeIssueSection(t *testing.T, body, heading string) string {
	t.Helper()
	return section(t, body, heading, "\n## ")
}

// closeIssueB2a is B2a up to the next level-3 heading.
func closeIssueB2a(t *testing.T, body string) string {
	t.Helper()
	return section(t, body, "### B2a.", "\n### ")
}

// Classification: fails when step 6 stops reading the waiting issues from the blocking list, or
// the fallback stops removing the blocked-issue label named through its config key.
func TestCloseIssueReportsReleasedIssues(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			s := section(t, body, "### 6. Released issues", "## Clean up the worktree")
			wantAll(t, s,
				`gh api "repos/$R/issues/<nr>/dependencies/blocking"`,
				`gh issue edit <m> --repo "$R" --remove-label <labels.blocked_issue>`)
			wantAll(t, flat(s), "loses the label `labels.blocked_issue` (`lighthouse config labels.blocked_issue`)")
		})
	}
}

var unblockLaneRe = regexp.MustCompile(`[\w-]*unblock-dependents\.yml`)

// Classification: fails when the skill looks for the unblock lane under another name than the
// installed lighthouse-unblock-dependents.yml, or stops recognising its reply comment.
func TestCloseIssueFindsTheInstalledUnblockLane(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body,
				"gh run list --workflow lighthouse-unblock-dependents.yml",
				"gh workflow run lighthouse-unblock-dependents.yml -f issue=<nr>",
				`(.body | startswith("Released lane after this close:"))`)
			names := unblockLaneRe.FindAllString(body, -1)
			if len(names) == 0 {
				t.Fatal("names no unblock-dependents.yml")
			}
			for _, n := range names {
				if n != "lighthouse-unblock-dependents.yml" {
					t.Errorf("names the lane as %q", n)
				}
			}
		})
	}
}

// Classification: fails when step 6 stops reading the bot from `ci.bot_login` or stops reporting
// a skipped lane.
func TestCloseIssueBotFromConfig(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			s := flat(closeIssueSection(t, body, "### 6. Released issues"))
			wantAll(t, s, "as the bot from `[ci]` (`lighthouse config ci.bot_login`)",
				"If `ci.bot_login` gives rc 1 or the workflow file is missing",
				"report `unblock lane: skipped — no bot in [ci]`")
		})
	}
}

const waitsForMerge = "waits for merge of #<pr>; run /lighthouse-close-issue <nr> again after the merge"

// Classification: fails when step 0 stops reporting the waits-for-merge line and stopping, or no
// longer comes before entry B.
func TestCloseIssueStepZeroWaitsForMerge(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, closeIssueSection(t, body, "## Step 0"), "report literally `"+waitsForMerge+"` and **stop**")
			wantOrder(t, body, "## Step 0", "## Entry B")
		})
	}
}

// Classification: fails when the second query of step 0 stops listing every open PR by head
// branch, or the gh calls stop running outside the sandbox.
func TestCloseIssueStepZeroStopsOnAnOpenRefsPR(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			s0 := closeIssueSection(t, body, "## Step 0")
			wantAll(t, s0,
				`gh pr list --repo "$R" --state open --limit 9999 --json number,headRefName,body`,
				`select((.headRefName | startswith("issue-<nr>-")) or`)
			wantAll(t, flat(s0), "Both `gh` calls run outside the sandbox (`dangerouslyDisableSandbox: true`).")
		})
	}
}

// Classification: fails when the waits-for-merge stop also fires for a closed issue, or the
// fall-through row to entry A or B changes.
func TestCloseIssueStepZeroStopIsScopedToAnOpenIssue(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			s0 := closeIssueSection(t, body, "## Step 0")
			wantAll(t, s0,
				"| issue `state` `OPEN` and (a node with `state` `OPEN` **or** a number from the second query) |",
				"| issue `state` `CLOSED` | **entry C** |",
				"| otherwise (issue `OPEN`, no open `Closes` PR and no open PR from the second query) | entry A or B |")
		})
	}
}

// Classification: fails when B2a stops listing merged PRs with an Evidence table newest first,
// stops merging only the latest row per criterion, stops running the file through the runner on a
// fresh default branch, or loses a row of its outcome-to-verdict table.
func TestCloseIssueEntryBRunsTheEvidenceOfMergedRefsPRs(t *testing.T) {
	cliFlags(t, []string{"evidence", "run"}, "issue", "body-file", "cwd")
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			b2a := closeIssueB2a(t, body)
			wantOrder(t, b2a, "R=$(lighthouse config forge.repo) || exit 2", `gh pr list --repo "$R" --state merged`)
			if n := strings.Count(b2a, "mktemp -d"); n != 1 {
				t.Errorf("B2a makes %d scratch directories, want 1", n)
			}
			wantAll(t, b2a,
				`gh pr list --repo "$R" --state merged --limit 9999 --json number,headRefName,body,mergedAt`,
				`select((.headRefName | startswith("issue-<nr>-")) or`,
				`select($b | test("(?m)^### Evidence #<nr>[ \t\r]*$"))] | sort_by(.mergedAt) | reverse`,
				"B=$(lighthouse config forge.default_branch) &&",
				`git worktree add --detach "<scratch>/after-merge-<nr>" "origin/$B" &&`,
				`{ lighthouse evidence run --issue <nr> --body-file "<scratch>/evidence.md" --cwd "<scratch>/after-merge-<nr>"`,
				"| `heading:evidence_holds` | `demonstrably done`;",
				"| `heading:evidence_fails` or `heading:evidence_invalid` | `not` |",
				"| `heading:evidence_unmeasurable` | not measurable from here |")
			wantAll(t, flat(b2a),
				"the line `### Evidence #<nr>`, the head `| AC | Command | Result |`",
				"per criterion only the row of the latest merged PR",
				"The criterion is the number the AC cell starts with; a cell without a number is a criterion of its own.")
		})
	}
}

// Classification: fails when B2a moves out of its place between B2 and B3.
func TestCloseIssueB2aSitsBetweenB2AndB3(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, body, "### B2.", "### B2a.", "### B3.")
		})
	}
}

// Classification: fails when the per-PR body fetch of B2a stops running outside the sandbox.
func TestCloseIssueB2aBodyFetchRunsOutsideTheSandbox(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueB2a(t, body)),
				"Per PR fetch the body, outside the sandbox (`dangerouslyDisableSandbox: true`): `gh pr view <pr> --json body --jq .body > <scratch>/body-<pr>.md`")
		})
	}
}

// Classification: fails when B2a stops forbidding body text in a command line.
func TestCloseIssueB2aNeverPastesBodyTextIntoACommand(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueB2a(t, body)),
				"Body text is free text: never paste it into a command line, it travels only through `--body-file`.")
		})
	}
}

// Classification: fails when controller mode (B7) stops running B2a.
func TestCloseIssueB7IncludesB2a(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueSection(t, body, "### B7.")), "Step 0, B1–B4 and B2a hold unchanged.")
		})
	}
}

// Classification: fails when entry C stops measuring only through the runner, in a throwaway
// worktree on the default branch read from config.
func TestCloseIssueEntryCRunsOnlyTheRunner(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, flat(c), "Free command text from the body **never** runs. Only the runner runs rows, and only rows on the allowlist.")
			wantAll(t, c,
				"B=$(lighthouse config forge.default_branch) &&",
				`{ lighthouse evidence run --issue <nr> --body-file "$scratch/body.md" --cwd "$scratch/after-merge-<nr>"`)
		})
	}
}

// Classification: fails when a reopen stops going issue first, board second, the board failure
// stops being reported loudly, or the reopen outcomes lose their heading keys.
func TestCloseIssueEntryCReopensFailClosed(t *testing.T) {
	// board-start parses its arguments raw (`--reopen <nr>`): only the subcommand is checked.
	cliFlags(t, []string{"board-start"})
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, c, "**Exit `1` or `2` with recognised output — reopen, fail-closed.** First the issue, then the board:")
			wantOrder(t, c, `gh issue reopen <nr> --repo "$R"`, "lighthouse board-start --reopen <nr>")
			wantAll(t, flat(c),
				"report loudly `issue reopened, board not updated` with the hand command",
				"At `2` the comment (`run.txt`) starts with `heading:evidence_closes_without_block`",
				"at `heading:evidence_invalid` rows the table says `Evidence outside the allowlist`",
				`--body "after-merge check: $(lighthouse headings evidence_holds)"`)
		})
	}
}

// Classification: fails when a skipped board stops being no error, or its paragraph starts
// reporting it as a board failure.
func TestCloseIssueBoardSkipIsNoError(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, flat(c), "`board: skipped` (no `[forge.board]`, rc 0) is no error: report the line as it is and go on.")
			hits := 0
			for _, p := range paragraphSplit.Split(c, -1) {
				if strings.Contains(p, "board: skipped") {
					hits++
					wantNone(t, p, "issue reopened, board not updated")
				}
			}
			if hits == 0 {
				t.Error("no paragraph names `board: skipped`")
			}
		})
	}
}

// Classification: fails when entry C stops treating exit 3, an unknown exit, unrecognised output
// or a missing rc line as not measurable, reported loudly without a reopen.
func TestCloseIssueEntryCReportsAnUnmeasurableRunLoudly(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueSection(t, body, "## Entry C")),
				"**Exit `3`, an unknown exit code, an unrecognised output or no `rc=` line.** Not measurable is no passed check and no proved failure.",
				"**no** `gh issue reopen`, no board mutation, and report loudly to the user `COULD NOT CHECK` with the full content of `run.txt` and `run.err`.")
		})
	}
}

// Classification: fails when entry C starts closing an issue itself.
func TestCloseIssueEntryCNeverClosesItself(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, flat(c), "Entry C closes nothing itself")
			wantNone(t, c, "gh issue close", "forge issue close")
		})
	}
}

// Classification: fails when entry C pastes runner output into a quoted command line instead of
// going through the run.txt file.
func TestCloseIssueEntryCDoesNotPasteRunnerOutputInAQuotedLine(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, c, `gh issue reopen <nr> --repo "$R" --comment "$(cat <scratch>/run.txt)"`)
			wantAll(t, flat(c), "So never paste runner output or body text into a quoted command line")
		})
	}
}

// Classification: fails when entry C catches the exit code after the worktree remove, or stops
// writing stderr to a file of its own.
func TestCloseIssueEntryCCatchesTheExitCodeBeforeTheWorktreeRemove(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, c,
				`> "$scratch/run.txt" 2> "$scratch/run.err"; rc=$?; echo "rc=$rc"; git worktree remove --force "$scratch/after-merge-<nr>"; }`)
			wantAll(t, flat(c), "The exit code is caught in `rc` **before** the `git worktree remove`")
		})
	}
}

// Classification: fails when C3 stops telling a real outcome (table head, closes-without-block
// heading) from a broken runner (traceback, argparse).
func TestCloseIssueEntryCReopensOnlyOnARecognisedOutcome(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c3 := section(t, body, "### C3", "\n## ")
			wantAll(t, c3,
				"| `1` | the table head `heading:evidence_table_head` is in it |",
				"| `2` | starts with `heading:evidence_closes_without_block` |",
				"| everything else | `1` without table head (traceback), `2` without `heading:evidence_closes_without_block` (argparse), an unknown exit code | the runner itself is broken | see below |")
		})
	}
}

// Classification: fails when step 0 stops reading the closer of the last ClosedEvent, or C1 stops
// taking only a PR closer as the closing PR.
func TestCloseIssueEntryCMeasuresThePRThatActuallyClosedTheIssue(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			step0 := section(t, body, "## Step 0", "## Three entries")
			wantAll(t, step0, "timelineItems(itemTypes:[CLOSED_EVENT],last:1){nodes{... on ClosedEvent{closer{__typename ... on PullRequest{number}}}}}")
			c1 := section(t, body, "### C1", "### C2")
			wantAll(t, flat(c1),
				"What counts is what **actually** closed the issue: the `closer` of the last `ClosedEvent`",
				"| `__typename` `PullRequest`, `number` `<pr>` | `<pr>` is the closing PR: on to C2 |",
				"report `closed without a closing PR — no after-merge check`")
		})
	}
}

// Classification: fails when entry C stops making a fresh scratch directory before the runner,
// stops chaining the worktree add, or stops treating a missing rc line as COULD NOT CHECK.
func TestCloseIssueEntryCScratchIsFreshAndChained(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantOrder(t, c, `scratch=$(mktemp -d) && echo "scratch=$scratch" &&`,
				`git worktree add --detach "$scratch/after-merge-<nr>" "origin/$B" &&`, "lighthouse evidence run")
			wantAll(t, flat(c), "If there is no `rc=` line in the output, the runner did not run: that is `COULD NOT CHECK`")
		})
	}
}

// Classification: fails when exit 4 stops writing its comment to a file and posting it with
// `--body-file`.
func TestCloseIssueEntryCExit4WritesACommentFile(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			c := closeIssueSection(t, body, "## Entry C")
			wantAll(t, c, "cat <scratch>/run.txt; } > <scratch>/comment.md",
				`gh issue comment <nr> --repo "$R" --body-file <scratch>/comment.md`)
		})
	}
}

// Classification: fails when the cleanup stops reading its verdict from `lighthouse
// worktree-audit --issue`, or loses the yes or no token.
func TestCloseIssueCleanupReadsTheAuditToken(t *testing.T) {
	cliFlags(t, []string{"worktree-audit"}, "issue")
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			cleanup := closeIssueSection(t, body, "## Clean up the worktree")
			wantAll(t, cleanup, "lighthouse worktree-audit --issue <nr>; echo \"exit=$?\"",
				"| `cleanup: yes` |", "| `cleanup: no — <reason>` |")
			wantAll(t, flat(cleanup), "Only exit `0` plus, in a branch's block, a line `cleanup: yes` can be cleaned up.")
		})
	}
}

// Classification: fails when the cleanup starts asking the user a question.
func TestCloseIssueCleanupAsksNoQuestion(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			cleanup := closeIssueSection(t, body, "## Clean up the worktree")
			wantAll(t, flat(cleanup), "No question: the verdict is the safety check", "There is no question, in any mode.")
			wantNone(t, cleanup, "AskUserQuestion")
		})
	}
}

// Classification: fails when B7 stops running the cleanup before its close comment, or a
// headless run stops putting the cleanup lines into the close comment.
func TestCloseIssueCleanupRunsHeadlessToo(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueSection(t, body, "### B7.")),
				"Before that close comment, run *Clean up the worktree*; its result per branch goes along as evidence lines in the close comment.")
			wantAll(t, flat(closeIssueSection(t, body, "## Clean up the worktree")),
				"Headless (B7, controller run, if present) the same lines go as evidence lines into the close comment.")
		})
	}
}

// Classification: fails when the cleanup loses its forbidden line.
func TestCloseIssueCleanupKeepsTheForbiddenLines(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, closeIssueSection(t, body, "## Clean up the worktree"),
				"- ❌ `--force`, `git worktree unlock` or `git push origin --delete` — also not when git refuses")
		})
	}
}

// Classification: fails when a git refusal stops the cleanup of the remaining branches.
func TestCloseIssueCleanupGoesOnAfterARefusal(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueSection(t, body, "## Clean up the worktree")),
				"report the error line, skip the rest of that branch's lines, and go on with the next branch.")
		})
	}
}

// Classification: fails when the cleanup stops proving per branch, from the main checkout, that
// worktree and branch are gone.
func TestCloseIssueCleanupShowsEvidencePerBranch(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, closeIssueSection(t, body, "## Clean up the worktree"),
				"\ngit -C <main checkout> worktree list\ngit -C <main checkout> branch --list <branch>\n")
		})
	}
}

// Classification: fails when the cleanup may remove the worktree that holds the own cwd.
func TestCloseIssueCleanupNeverRemovesOwnCwd(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueSection(t, body, "## Clean up the worktree")),
				"The worktree that holds your own cwd is never cleaned up")
		})
	}
}

// Classification: fails when the commit URL stops being built from `forge.repo`.
func TestCloseIssueCommitURLsFromForgeRepo(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body,
				"# Repo from lighthouse.toml, never from the git remote of the cwd\nR=$(lighthouse config forge.repo) || exit 2",
				`printf 'Completed in commit: https://github.com/%s/commit/%s\n\n' "$R"`,
				"Completed in commit: https://github.com/<forge.repo>/commit/<hash>")
		})
	}
}

// Classification: fails when the release step stops being conditional on scripts/release.py.
func TestCloseIssueReleaseFlowOnlyIfPresent(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			release := section(t, body, "### Step 4.", "### Step 5.")
			wantAll(t, lineStarting(t, release, "### Step 4."), "(only if present)")
			wantAll(t, flat(release),
				"(`test -e scripts/release.py`); otherwise report `release: skipped — no scripts/release.py` and go on with step 5.")
		})
	}
}

// Classification: fails when the variants differ in any line but the instruction file of the DoD.
func TestCloseIssueVariantsDifferOnlyInTheInstructionFile(t *testing.T) {
	v := variants(t, closeIssue)
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
		"- [ ] Documentation updated (README, CLAUDE.md, etc.)",
		"- [ ] Documentation updated (README, AGENTS.md, etc.)",
	}
	if strings.Join(diff, "\n") != strings.Join(want, "\n") {
		t.Errorf("differing lines (claude, agents pairs) = %q, want %q", diff, want)
	}
}

// Classification: fails when closing a milestone stops copying the title and appending " - DONE"
// in the same PATCH as the close.
func TestCloseIssueMilestoneCloseKeepsTheTitleForm(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			step := closeIssueSection(t, body, "### Step 3. Close the milestone")
			wantAll(t, step, "  -f state=closed -f title='<title> - DONE'",
				`# close + rename (copy the title exactly, then " - DONE" after it)`)
		})
	}
}

// Classification: fails when step 5 stops taking the area from the part of the milestone title
// after `project.milestone_prefix`.
func TestCloseIssueAreaOfAMilestoneAfterThePrefix(t *testing.T) {
	for variant, body := range variants(t, closeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(closeIssueSection(t, body, "### Step 5. Update the near goal of the area")),
				"with `<area>` the part before `:` after `project.milestone_prefix` in the milestone title")
		})
	}
}
