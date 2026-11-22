package packcontract

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// write-issue: what lives only in skill text. The relation comes before the blocked-issue label,
// step 0 searches for duplicates before the template, the label taxonomy comes from the file
// under `[paths] labels` and the `[labels]` keys, and the invariant check of step 6 runs as
// written against `lighthouse config`. The variants differ only in the "Generated with" line.

const writeIssue = "core/skills/write-issue"

// Classification: fails when 3e sets the blocked-issue label before the relation, stops verifying
// from the list instead of the summary, loses the **unknown** outcome or the `created_at`
// recovery rule, or drops a verdict of the invariant table.
func TestWriteIssueSetsRelationBeforeLabel(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			s := section(t, body, "#### 3e. Set a block", "#### 3f.")
			wantOrder(t, s, "lighthouse forge issue block <nr> --by <blocker-nr>", `--add-label "$BLOCKED"`)
			f := flat(s)
			wantAll(t, f,
				"The relation comes **before** the label, so that a half failure never leaves a label without a relation.",
				"**Verify with the list, not from the summary**",
				"`created_at` after the noted start time",
				"**unknown** + the search call, stop before every mutation. Never create again blindly",
				"Only when relation and winning blocking label stand there together, report success.")
			for _, verdict := range []string{"holds", "lapsed", "unlinked", "label missing"} {
				wantAll(t, s, "**"+verdict+"**")
			}
		})
	}
}

// Classification: fails when 3e names the blocked-issue label literally instead of through its
// config key, a blocking key disappears, or the rank table of 3c changes its order.
func TestWriteIssueBlockingLabelsComeFromConfigInRankOrder(t *testing.T) {
	keys := []string{"labels.needs_shaping", "labels.needs_decision", "labels.blocked_issue",
		"labels.blocked_external", "labels.needs_hands", "labels.needs_testing"}
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(section(t, body, "#### 3e. Set a block", "### 4.")),
				`BLOCKED=$(lighthouse config labels.blocked_issue) || exit 2 lighthouse forge issue edit <nr> --add-label "$BLOCKED"`)
			rank := section(t, body, "#### 3c.", "#### 3d.")
			var rows []string
			for i, key := range keys {
				rows = append(rows, "| "+string(rune('1'+i))+" | `"+key+"` |")
			}
			wantOrder(t, rank, rows...)
		})
	}
}

// Classification: fails when step 0 stops searching issues (open and closed, bounded) and the
// default branch before the template, loses the report line for no hit or a failed fetch, the
// exit rule of git grep, or a choice for a covering hit.
func TestWriteIssueSearchesDuplicatesBeforeTheTemplate(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			step0 := section(t, body, "### 0. Does this exist already?", "### 1. Choose the template")
			wantAll(t, step0,
				`lighthouse forge issue search '<key term>' | jq -r '.[:5][] | "#\(.number) \(.state) \(.title)"'`,
				"B=$(lighthouse config forge.default_branch) || exit 2",
				`git fetch origin "$B" || echo 'FETCH FAILED: origin/'"$B"' may be stale'`,
				`git grep -l '<key term>' "origin/$B" -- .`)
			f := flat(step0)
			wantAll(t, f,
				"only exit ≥ 2 (e.g. 128) is an error",
				"report exactly one line `duplicate? no — searched on <terms>`",
				"If `FETCH FAILED` is in the output",
				"report `duplicate? not measurable — fetch failed` instead of `no`",
				"**do not create**", "**add to** the existing issue", "**create anyway**")
		})
	}
}

// existingIssueRowRe matches a row of the "An existing issue" table: step, then its verdict.
func existingIssueRowRe(step, verdict string) string {
	return `\| ` + regexp.QuoteMeta(step) + ` [^|]*\| ` + verdict
}

// Classification: fails when the existing-issue table stops naming which steps drop and which
// hold, or the edit stops going through `--body-file`, or step 4 stops being named as never run.
func TestWriteIssueExistingIssueHasItsOwnPath(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			s := section(t, body, "## An existing issue", "## A defect in an external tool")
			f := flat(s)
			for _, step := range []string{"0.", "1.", "2.", "4."} {
				wantRe(t, f, existingIssueRowRe(step, "drops"))
			}
			for _, step := range []string{"3c.", "3e."} {
				wantRe(t, f, existingIssueRowRe(step, "holds"))
			}
			wantAll(t, s, "lighthouse forge issue edit <nr> --title '<title>' --body-file <file>")
			wantAll(t, f, "| 4. Create the issue | drops: never `lighthouse forge issue create` |",
				"never the body itself as a shell argument")
		})
	}
}

// Classification: fails when a move between blocking labels stops being one edit, comes before
// the relation, or stops closing with the final check of 3e step 5.
func TestWriteIssueLapsedBlockPath(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			s := section(t, body, "## An existing issue", "## A defect in an external tool")
			wantAll(t, s, `lighthouse forge issue edit <nr> --remove-label "$OLD" --add-label "$NEW"`)
			wantOrder(t, flat(s), "If it is `labels.blocked_issue`, set the relation first with 3e steps 2–3.",
				`--add-label "$NEW"`)
			wantAll(t, flat(s),
				"Close with 3e step 5 as the final check, also when no relation is involved",
				"Report what stands, never success from the command alone.")
		})
	}
}

// Classification: fails when the pitch loses its not-yet-decided heading (after the no-gos), the
// DoD heading, the outflow to an issue of its own, or the rule that the fog has no timebox.
func TestWriteIssuePitchHasSectionNotYetDecided(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			pitch := section(t, body, "### Pitch (Shape Up)", "## Title Standards")
			wantOrder(t, pitch, "### heading:milestone_dod", "### heading:milestone_no_gos",
				"### heading:milestone_not_yet_decided")
			wantAll(t, flat(pitch),
				"it becomes an issue of its own in this milestone through `/lighthouse-write-issue`, and the bullet becomes `→ #<nr>`",
				"the fog gets no timebox of its own")
		})
	}
}

// Classification: fails when the Task template loses its acceptance-criteria heading key.
func TestWriteIssueTaskTemplateCarriesTheAcceptanceCriteriaHeading(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, section(t, body, "### Task", "### Pitch (Shape Up)"), "### heading:acceptance_criteria")
		})
	}
}

// Classification: fails when the taxonomy stops coming from `paths.labels` with its fallback, the
// axes stop coming from their prefix keys, or a literal label name or source-project name appears.
func TestWriteIssueTaxonomyFromPaths(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(section(t, body, "### 3. Set the labels", "#### 3a.")),
				"lighthouse config paths.labels",
				"rc 1 (not set): only the `[labels]` keys and the GitHub default labels")
			wantAll(t, section(t, body, "#### 3a.", "#### 3b."), "`lighthouse config labels.location_prefix`. rc 0:")
			wantAll(t, section(t, body, "#### 3b.", "#### 3c."), "`lighthouse config labels.area_prefix`. rc 0:")
			wantNone(t, body, "intake", "nexus", "1-loc", "2-area", "3-blocked", "3-needs", "Corneel")
		})
	}
}

// Classification: fails when the skill stops pointing at the board section of the forge
// reference, setting the status through `lighthouse forge`, or reporting a skipped board.
func TestWriteIssueBoardFromConfig(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "lighthouse config forge.kind", `lighthouse forge issue status <nr> "<status>"`)
			wantAll(t, flat(body),
				"What the board is per forge (a GitHub Projects v2 project, GitLab's work item status) is `../lighthouse-forge/<kind>.md §board`.",
				"report `board: skipped — no [forge.board] configured` and set nothing")
		})
	}
}

// Classification: fails when the variants differ in any line but the "Generated with" line.
func TestWriteIssueVariantsDifferOnlyInTheGeneratedLine(t *testing.T) {
	v := variants(t, writeIssue)
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
		`- Never include "Generated with Claude Code" in issues unless directly relevant`,
		`- Never include "Generated with Codex" in issues unless directly relevant`,
	}
	if strings.Join(diff, "\n") != strings.Join(want, "\n") {
		t.Errorf("differing lines (claude, agents pairs) = %q, want %q", diff, want)
	}
}

// Classification: fails when Dutch, a source issue number, a host name or a lighthouse-repo path
// enters the skill.
func TestWriteIssueIsEnglishAndDetached(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, strings.ToLower(body), dutchWords...)
			if m := issueNumberRe.FindString(body); m != "" {
				t.Errorf("names source issue %s", m)
			}
			wantNone(t, body, "Beelink", "developer/")
		})
	}
}

// Classification: fails when the milestone title stops reading its prefix from
// `project.milestone_prefix`, loses the no-prefix report, the form rule, the size rule, the DoD
// hard rule or the gatekeeper rule.
func TestWriteIssueMilestonePrefixFromConfig(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			f := flat(section(t, body, "## Milestones", "## The board"))
			wantAll(t, f,
				"**Naming**: `<project.milestone_prefix> <name>`",
				"`MP=$(lighthouse config project.milestone_prefix)`; rc 1 means no prefix",
				"the report says `milestone prefix: none — no [project] milestone_prefix set`",
				"is project content and lives in the file under `[paths] labels`",
				"the plan of a milestone splits it into at least about five issues",
				"*a milestone without DoD text in its description does not exist.*",
				"The description is at the same time the **gatekeeper**.")
		})
	}
}

// Classification: fails when step 6, run as written against the real `lighthouse config` and a
// forge page, stops reporting an issue with two blocking labels or starts reporting a clean one.
func TestWriteIssueInvariantCheckRunsAgainstTheForge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the step is a POSIX shell block")
	}
	bash := needTool(t, "bash")
	bin := t.TempDir()
	for _, tool := range []string{"jq", "sh", "cat"} {
		path := needTool(t, tool)
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	// The forge is the boundary: `lighthouse forge issue list` answers from a page in the JSON
	// shape of the wrapper and refuses any other argv the way the real command refuses an unknown
	// flag. Every other call runs this test binary as lighthouse (TestMain).
	stub := "#!/bin/sh\nif [ \"$1\" = forge ]; then\n" +
		"  [ \"$*\" = \"forge issue list --state open\" ] || { echo \"unknown flag: $*\" >&2; exit 2; }\n" +
		"  exec cat \"$FORGE_PAGE\"\nfi\nexec \"$REAL_LIGHTHOUSE\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "lighthouse"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	toml := "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n\n" +
		"[labels]\nlocation_prefix = \"loc-\"\narea_prefix = \"area-\"\n"
	if err := os.WriteFile(filepath.Join(repo, "lighthouse.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(repo, "page.json")
	pageJSON := `[{"number": 1, "labels": ["loc-a", "area-b", "needs-shaping", "needs-decision"]},` +
		` {"number": 2, "labels": ["loc-a", "area-b"]}]`
	if err := os.WriteFile(page, []byte(pageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			block := bashBlock(t, section(t, body, "### 6. Check the invariants", "## Milestones"), "### 6.")
			script := strings.TrimPrefix(block, "```bash\n")
			cmd := exec.Command(bash, "-c", script)
			cmd.Dir = repo
			cmd.Env = []string{
				"PATH=" + bin,
				"HOME=" + repo,
				"FORGE_PAGE=" + page,
				"REAL_LIGHTHOUSE=" + self,
				"LIGHTHOUSE_REPO_ROOT=" + repo,
				"LIGHTHOUSE_HOST_TOML=" + filepath.Join(repo, "host.toml"),
				envActAsLighthouse + "=1",
			}
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("step 6: %v\n%s", err, stderr.String())
			}
			var lines []string
			for line := range strings.SplitSeq(string(out), "\n") {
				if line != "" {
					lines = append(lines, line)
				}
			}
			if want := `{"n":1,"loc":1,"area":1,"blk":2}`; strings.Join(lines, "\n") != want {
				t.Errorf("step 6 printed %q, want only %s\nstderr: %s", lines, want, stderr.String())
			}
		})
	}
}

// Classification: fails when the milestone create block stops reading its own prefix under MP,
// stops reporting a missing one, or stops putting it in front of the title.
func TestWriteIssueMilestoneCreateReadsItsOwnPrefix(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			block := bashBlock(t, body, "Creating goes through `lighthouse forge milestone create`")
			wantAll(t, block,
				"MP=$(lighthouse config project.milestone_prefix); rc=$?",
				`[ "$rc" -eq 1 ] && echo "milestone prefix: none — no [project] milestone_prefix set"`,
				`lighthouse forge milestone create --title "${MP:+$MP }<name>" --description-file <file>`)
		})
	}
}

// Classification: fails when 3f stops reading the document of a free label, stops gating the
// label on the `When` column, loses its skip path or its place before step 4, or when the
// existing-issue row or the create command of step 4 stops carrying the free label.
func TestWriteIssueFreeLabelsWithADocument(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			step := flat(section(t, body, "#### 3f. Free labels with a document", "### 4. Create the issue"))
			wantAll(t, step,
				"the columns `When` and `Document` (the `Document` column names a file)",
				"open that document to see what already exists, then test the issue against the `When` column",
				"set the label only when the issue matches the row's `When` column, not merely because the issue touches something the document describes",
				"This step runs before step 4; the label goes into the `--label` of step 4.",
				"No taxonomy file, or no such row: skip this step.")
			_, existing, ok := strings.Cut(body, "## An existing issue")
			if !ok {
				t.Fatal("no ## An existing issue")
			}
			wantAll(t, flat(existing),
				"| 3f. Free labels with a document | only when the change makes the issue match a `When` column; "+
					"then `lighthouse forge issue edit <nr> --add-label <label>`, since step 4 drops |")
			create := section(t, body, "### 4. Create the issue", "### 5. Report the result")
			wantAll(t, flat(create), "One `--label` for each axis the project has (3a, 3b) plus every free label from 3f.")
			wantAll(t, create, "--label '<location label>' --label '<area label>' [--label '<free label>']")
		})
	}
}

// Classification: fails when write-issue loses the gate-4 judgement (design depth at 4c means
// `labels.needs_shaping`), or names `On hold` as a status again. Nothing writes `On hold` any more:
// parking is `To do` plus `labels.blocked_issue`.
func TestWriteIssueSkillSetsNeedsShapingAtGate4c(t *testing.T) {
	for variant, body := range variants(t, writeIssue) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "gate 4", "4c", "`labels.needs_shaping`")
			wantNone(t, body, "\"On hold\"", "Status `On hold`")
			wantAll(t, body, "`To do`", "`labels.blocked_issue`")
		})
	}
}
