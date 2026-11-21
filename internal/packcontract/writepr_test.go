package packcontract

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/cli"
	"github.com/CorneeldH/lighthouse/internal/evidence"
	"github.com/CorneeldH/lighthouse/internal/prbody"
	"gopkg.in/yaml.v3"
)

// write-pr: the counterpart of the hook pr-create-gate. These tests bind the skill text to the
// hook, to `lighthouse evidence` and `lighthouse pr-body-gate`, and to the allowlist constants in
// internal/evidence. The skill has one shared SKILL.md; variants still yields both.

const writePR = "core/skills/write-pr"

// writePRSection is the text from heading up to the next level-2 heading.
func writePRSection(t *testing.T, body, heading string) string {
	t.Helper()
	return section(t, body, heading, "\n## ")
}

// writePRFront is the parsed frontmatter of write-pr.
type writePRFront struct {
	Description string `yaml:"description"`
	Metadata    struct {
		Binding string `yaml:"binding"`
	} `yaml:"metadata"`
}

func parseWritePRFront(t *testing.T, body string) writePRFront {
	t.Helper()
	var front writePRFront
	if err := yaml.Unmarshal([]byte(frontmatter(t, body)), &front); err != nil {
		t.Fatal(err)
	}
	return front
}

// cliFlags fails when `lighthouse <path…>` is not a subcommand of the binary or lacks a flag.
func cliFlags(t *testing.T, path []string, flags ...string) {
	t.Helper()
	root := cli.NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	cmd, rest, err := root.Find(path)
	if err != nil || len(rest) > 0 || cmd == root {
		t.Fatalf("lighthouse %s is no subcommand (rest %v, err %v)", strings.Join(path, " "), rest, err)
	}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("lighthouse %s has no --%s", strings.Join(path, " "), f)
		}
	}
}

// exampleBlock is the example under "## Shape of the evidence block"; the example itself
// contains `## Related issues`, so it is cut up to the next real heading.
func exampleBlock(t *testing.T, body string) string {
	t.Helper()
	return section(t, body, "## Shape of the evidence block", "## Counterpart")
}

// Classification: fails when `binding: hard` leaves the metadata of write-pr, which check 8 binds
// to the pr-create-gate hook.
func TestWritePRBindingIsHard(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			if got := parseWritePRFront(t, body).Metadata.Binding; got != "hard" {
				t.Errorf("metadata.binding = %q, want hard", got)
			}
		})
	}
}

// Classification: fails when the description no longer parses up to its NOTE clause, e.g. an
// unquoted ` #N` that YAML reads as a comment.
func TestWritePRDescriptionSurvivesTheYAMLParser(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			desc := parseWritePRFront(t, body).Description
			if !strings.HasSuffix(desc, "repairing a red PR is lighthouse-fix-pr.") {
				t.Errorf("description does not end on the NOTE clause: %q", desc)
			}
		})
	}
}

// Classification: fails when the description stops naming the Refs form or starts offering
// `Closes #N`, which this skill never writes.
func TestWritePRDescriptionNamesRefsNotCloses(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			desc := parseWritePRFront(t, body).Description
			wantAll(t, desc, "`Refs #N — <reason>` with an Evidence table")
			wantNone(t, desc, "Closes #N")
		})
	}
}

var evidenceRunRe = regexp.MustCompile("lighthouse evidence run[^`\n]*")

// Classification: fails when a mention of `lighthouse evidence run` drops `--before-pr`, or the
// binary no longer has the flags the skill passes.
func TestWritePRRunsEvidenceBeforePR(t *testing.T) {
	cliFlags(t, []string{"evidence", "run"}, "issue", "body-file", "cwd", "before-pr")
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			runs := evidenceRunRe.FindAllString(body, -1)
			if len(runs) < 2 {
				t.Fatalf("%d mentions of lighthouse evidence run, want the V3 one and the command", len(runs))
			}
			for _, r := range runs {
				if !regexp.MustCompile(`--before-pr\b`).MatchString(r) {
					t.Errorf("%q runs without --before-pr", r)
				}
			}
		})
	}
}

// Classification: fails when the skill stops naming the hook by the path check 8 reads.
func TestWritePRNamesTheHook(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, writePRSection(t, body, "## Counterpart"),
				"`.claude/hooks/lighthouse/run.sh pr-create-gate` (PreToolUse on `Bash`) enforces the form")
		})
	}
}

// Classification: fails when a criterion that waits on the PR stops being `after merge`.
func TestWritePRAfterMergeCoversWhatWaitsOnThePR(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			row := lineStarting(t, writePRSection(t, body, "## Verdicts"), "| `after merge` |")
			wantAll(t, row, "can only happen after the merge", "a property that can only be measured once the PR exists")
		})
	}
}

// Classification: fails when the evidence checker or the body check no longer runs through the
// `lighthouse` binary, or a pinned subcommand disappears from it.
func TestWritePRVerifiesEvidenceThroughTheBinary(t *testing.T) {
	cliFlags(t, []string{"evidence", "check"})
	cliFlags(t, []string{"pr-body-gate"})
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body,
				"\nlighthouse evidence check '<Command cell>'\n",
				"\nlighthouse evidence run --issue <nr> --body-file <path> --cwd . --before-pr\n",
				"--body-file <path>\" | lighthouse pr-body-gate \"$PWD\"\n")
		})
	}
}

// allowlistTokens is every token in a code span of text, with `=` and backticks dropped, split on
// whitespace, `/` and `,`.
func allowlistTokens(text string) map[string]bool {
	tokens := map[string]bool{}
	for _, span := range codeSpanRe.FindAllString(text, -1) {
		cell := strings.NewReplacer("`", "", "=", "").Replace(span)
		for _, tok := range strings.FieldsFunc(cell, func(r rune) bool {
			return r == '/' || r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			tokens[tok] = true
		}
	}
	return tokens
}

// allowedCell is the "Allowed" cell of the table row that starts with prefix; with a non-empty
// command only the `<command>`: segment of that cell, up to the next `;`.
func allowedCell(t *testing.T, table, prefix, command string) string {
	t.Helper()
	cells := strings.Split(strings.Trim(strings.TrimSpace(lineStarting(t, table, prefix)), "|"), " | ")
	if len(cells) != 3 {
		t.Fatalf("row %s… has %d cells, want 3", prefix, len(cells))
	}
	if command == "" {
		return cells[1]
	}
	_, seg, ok := strings.Cut(cells[1], "`"+command+"`:")
	if !ok {
		t.Fatalf("row %s… has no `%s`: segment", prefix, command)
	}
	seg, _, _ = strings.Cut(seg, ";")
	return seg
}

// allowlistRows binds each allowlist constant to the row (and segment) of the skill table that
// must allow its flags.
var allowlistRows = []struct{ name, row, command string }{
	{"GIT_LOG_FLAGS", "| `git log`", ""},
	{"GIT_LOG_VAL", "| `git log`", ""},
	{"PYTEST_FLAGS", "| `uv run pytest`", ""},
	{"GH_API_VAL", "| `gh api <path>`", ""},
	{"GH_VIEW_FLAGS", "| `gh issue view`", ""},
	{"GH_VIEW_VAL", "| `gh issue view`", ""},
	{"LS_FLAGS", "| `test`, `ls`", "ls"},
	{"WC_FLAGS", "| `test`, `ls`", "wc"},
	{"CAT_FLAGS", "| `test`, `ls`", "cat"},
	{"GIT_GREP_VAL", "| `git grep`", ""},
}

// Classification: fails when the allowlist table drops a command, a refusal, a `lighthouse forge`
// read form, or a flag that evidence.Allowlist accepts from the Allowed cell of its own row.
func TestWritePRAllowlistTableCoversTheCode(t *testing.T) {
	subs := evidence.Allowlist["GIT_SUBS"]
	if len(subs) == 0 {
		t.Fatal("evidence.Allowlist[GIT_SUBS] is empty")
	}
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			table := section(t, body, "## Evidence allowlist", "```")
			wantAll(t, table, "`grep`", "`git grep`", "`git log`", "`uv run pytest`", "`gh api <path>`",
				"`gh api graphql`", "`gh pr view`", "`lighthouse forge api <path>`")
			// The read forms that forgeRead in internal/evidence accepts; every write form stays off.
			for _, form := range []string{"issue view", "issue list", "issue search", "issue blockers",
				"pr view", "milestone list", "milestone view", "label list"} {
				wantAll(t, table, "`lighthouse forge "+form+"`")
			}
			wantAll(t, table, "`--comments`", "`--field`", "`--state`", "`--label`", "`--milestone`", "`-X GET`")
			wantAll(t, table, "`graphql`", "`%`", "`..`", "`--web`", "`--output`", "`-H`", "`%G`")
			for _, r := range allowlistRows {
				flags := evidence.Allowlist[r.name]
				if len(flags) == 0 {
					t.Fatalf("evidence.Allowlist[%s] is empty", r.name)
				}
				tokens := allowlistTokens(allowedCell(t, table, r.row, r.command))
				for _, f := range flags {
					if !tokens[f] {
						t.Errorf("%s flag %s missing from the Allowed cell of %s…", r.name, f, r.row)
					}
				}
			}
			for _, sub := range subs {
				wantAll(t, table, "`git "+sub+"`")
			}
		})
	}
}

// Classification: fails when the pytest row stops reading the test directory from config, or
// names a literal test directory.
func TestWritePRPytestDirComesFromConfig(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			row := lineStarting(t, writePRSection(t, body, "## Evidence allowlist"), "| `uv run pytest` |")
			wantAll(t, row, "only paths under the test directory `lighthouse config test_gate.evidence_test_dir`")
			wantNone(t, body, "tests/unit")
		})
	}
}

// useConfig points the config at a lighthouse.toml with language en and forge.repo
// Acme/widgets, and the headings at pack/core/headings.toml.
func useConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	toml := "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	headings, err := filepath.Abs(filepath.Join(packRoot, "core", "headings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
	t.Setenv("LIGHTHOUSE_HEADINGS", headings)
}

// Classification: fails when row 1 of the example, copied as is, no longer holds against the
// installed PR template (its command, its Result or the template drifted).
func TestWritePRExampleRowHoldsAgainstTheTemplate(t *testing.T) {
	useConfig(t)
	template := readPack(t, "core/templates/pull_request_template.md")
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			block := exampleBlock(t, body)
			_, table, ok := strings.Cut(block, "|---|---|---|\n")
			if !ok {
				t.Fatal("no table in the example block")
			}
			row, _, _ := strings.Cut(table, "\n")
			if !strings.HasPrefix(row, "| 1. ") {
				t.Fatalf("first example row %q does not start with criterion 1", row)
			}
			repo := t.TempDir()
			if err := os.MkdirAll(filepath.Join(repo, ".github"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".github", "pull_request_template.md"), []byte(template), 0o644); err != nil {
				t.Fatal(err)
			}
			bodyFile := filepath.Join(t.TempDir(), "body.md")
			text := "Refs #1091 — close-issue runs the rows\n\n### Evidence #1091\n\n" +
				"| AC | Command | Result |\n|---|---|---|\n" + row + "\n"
			if err := os.WriteFile(bodyFile, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if rc := evidence.Run(1091, bodyFile, repo, false, &out); rc != 0 {
				t.Fatalf("evidence run exit %d:\n%s", rc, out.String())
			}
			wantAll(t, out.String(), "| holds |")
		})
	}
}

// Classification: fails when V3 stops always writing `Refs #N — <reason>`, stops handing the
// rows to close-issue, or loses the row rules (only proven criteria, numbered, no table when
// every criterion is open).
func TestWritePRV3AlwaysWritesRefs(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			v3 := flat(writePRSection(t, body, "## V3."))
			wantAll(t, v3,
				"The form is always `Refs #N — <reason>`, never `Closes`",
				"The reason says that `/lighthouse-close-issue` runs the rows after the merge and closes the issue.",
				"one row per AC that is `demonstrably done`",
				"the AC cell starts with the number of the criterion in the issue",
				"Every criterion open: no table.")
		})
	}
}

// Classification: fails when the example block stops being a Refs body that pr-create-gate lets
// through, or the gate starts asking the forge for the criteria count of a Refs body.
func TestWritePRExampleBlockIsAValidRefsBody(t *testing.T) {
	useConfig(t)
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			block := exampleBlock(t, body)
			wantAll(t, block, "Refs #1091 — ")
			wantNone(t, block, "Closes #1091")
			project := t.TempDir()
			if err := os.WriteFile(filepath.Join(project, "body.md"), []byte(block), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLAUDE_PROJECT_DIR", project)
			var forgeCalls []string
			env := prbody.Env{
				GH: func(_ context.Context, argv ...string) (string, int, error) {
					forgeCalls = append(forgeCalls, "gh "+strings.Join(argv, " "))
					return "", 1, nil
				},
				Issue: func(_ context.Context, _ prbody.Repo, _ int) (string, error) {
					forgeCalls = append(forgeCalls, "issue")
					return "", errors.New("the gate must not read the issue of a Refs body")
				},
			}
			var stdout, stderr bytes.Buffer
			rc := prbody.Main(context.Background(), project,
				strings.NewReader("gh pr create --repo Acme/widgets -F body.md"), &stdout, &stderr, env)
			if rc != 0 || stdout.String() != "pr-create-gate: body checked — Refs #1091\n" {
				t.Errorf("gate: exit %d, stdout %q, stderr %q", rc, stdout.String(), stderr.String())
			}
			if len(forgeCalls) > 0 {
				t.Errorf("the gate asked the forge: %v", forgeCalls)
			}
		})
	}
}

// Classification: fails when V2b stops running in the build session, or a write-pr run started
// with `/lighthouse-write-pr <nr>` starts reviewing at `run` or `classify` instead of recording a
// skip (also in the verdict table itself).
func TestWritePRV2bRunsInTheBuildSession(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			v2b := flat(writePRSection(t, body, "## V2b."))
			wantAll(t, v2b,
				"In a controller run the **build session** reviews",
				"the write-pr run never reviews",
				"If the gate says `run` or `classify` there, the build skipped V2b",
				`lighthouse roborev-gate record --skip "build session ran no review"`)
			_, table, ok := strings.Cut(v2b, "| `verdict` | Do |")
			if !ok {
				t.Fatal("no verdict table in V2b")
			}
			wantAll(t, table,
				"In a run started with `/lighthouse-write-pr <nr>`, `run` or `classify` do not mean step 1 or 2",
				`lighthouse roborev-gate record --skip "build session ran no review"`)
		})
	}
}

// Classification: fails when V5 stops handing off to close-issue after the merge, loses its
// controller-run signals, or comes before V4.
func TestWritePRV5HandsOffToCloseIssue(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			v5 := flat(writePRSection(t, body, "## V5."))
			wantAll(t, v5,
				"`/lighthouse-write-handoff` with as first step `/lighthouse-close-issue <nr>`, to run after the merge",
				"(the prompt ends in `(issue #<nr>, controller-run)` or `LIGHTHOUSE_CONTROLLER_RUN` is in the environment)",
				"posts `next: /lighthouse-close-issue <nr>`",
				"No PR opened: no handoff to close-issue.",
				"with `No issue:` V5 is skipped")
			wantOrder(t, body, "## V4.", "## V5.")
		})
	}
}

// Classification: fails when V4 drops `--title` or `--body-file` from the create command, stops
// writing the body in a separate Write step, or loses the open-review-points call.
func TestWritePRV4GivesATitleAndWritesTheBodySeparately(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			v4 := flat(writePRSection(t, body, "## V4."))
			wantAll(t, v4,
				"lighthouse forge pr create --title '<title>' --body-file <path>",
				"in a separate step (the Write tool), not in the same Bash call as `lighthouse forge pr create`",
				"lighthouse plan-review-record pr-open-points <path>...")
		})
	}
}

// Classification: fails when the DoD makes Claude Code run the full suite by hand instead of
// leaving it to CI, or stops sending the other harnesses to the configured test command.
func TestWritePRDoDLeavesTheSuiteToCI(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			dod := writePRSection(t, body, "## DoD")
			wantAll(t, flat(dod),
				"`.claude/hooks/lighthouse/run.sh commit-test-gate`",
				"proves only the files of each commit, not the suite",
				"write `suite: CI` in `## Verification`, and do not run the full suite by hand")
			wantOrder(t, dod, "Outside Claude Code", "lighthouse config test_gate.command")
		})
	}
}

// Classification: fails when the gh view row stops demanding a number, so gh falls back on the
// branch of the cwd.
func TestWritePRGhViewDemandsANumber(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			row := lineStarting(t, writePRSection(t, body, "## Evidence allowlist"), "| `gh issue view`, `gh pr view` |")
			wantAll(t, row, "exactly one number or github.com URL")
		})
	}
}

// Classification: fails when the git grep row stops refusing a value option of grep that git
// grep lacks (GREP_VAL minus GIT_GREP_VAL in evidence.Allowlist).
func TestWritePRGitGrepRowNamesWhatEvidenceRefuses(t *testing.T) {
	gitGrep := map[string]bool{}
	for _, o := range evidence.Allowlist["GIT_GREP_VAL"] {
		gitGrep[o] = true
	}
	var missing []string
	for _, o := range evidence.Allowlist["GREP_VAL"] {
		if !gitGrep[o] {
			missing = append(missing, o)
		}
	}
	if len(missing) == 0 {
		t.Fatal("GREP_VAL minus GIT_GREP_VAL is empty")
	}
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			row := lineStarting(t, body, "| `git grep` |")
			cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), " | ")
			if len(cells) != 3 {
				t.Fatalf("git grep row has %d cells, want 3: %q", len(cells), row)
			}
			for _, o := range missing {
				wantAll(t, cells[2], "`"+o+"`")
			}
		})
	}
}

// Classification: fails when the runner's outcomes are named literally instead of through
// heading keys, or the could-not-check exit loses its marker.
func TestWritePROutcomesAreHeadings(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			allow := flat(writePRSection(t, body, "## Evidence allowlist"))
			wantAll(t, allow,
				"a table `heading:evidence_table_head`. Every row must be `heading:evidence_holds`",
				"| 0 | all `heading:evidence_holds` |",
				"| 3 | `COULD NOT CHECK`")
		})
	}
}

// Classification: fails when the changelog step stops being opt-in, stops checking what the
// branch already adds, loses a type, the file name rule, English, or its place before the PR.
func TestWritePRChangelogFragmentIsOptInAndEnglish(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			s := flat(writePRSection(t, body, "## Changelog fragment"))
			wantAll(t, s,
				"## Changelog fragment (if present)",
				".d/.lighthouse` in the repository root. Otherwise skip this section.",
				"--diff-filter=A origin/<default branch>...HEAD --",
				"writes nothing twice",
				"write one before `lighthouse forge pr create`, commit it on the branch and push",
				".d/<branch>.md`, a `/` in the branch name becoming `-`",
				"take the first free `-<n>` from 2",
				"- `added`:", "- `changed`:", "- `fixed`:", "- `removed`:", "- `internal`:",
				"Write the text in English for a user")
		})
	}
}

// Classification: fails when the roborev step moves out of its place between V2 and the
// verdicts.
func TestWritePRRoborevStepSitsBetweenV2AndTheVerdicts(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, body, "## V2.", "## V2b. Roborev review", "## Verdicts")
		})
	}
}

// Classification: fails when V2b loses a command, the foreground timeout, the sandbox escape, the
// fix-commit line, the push of fix commits, or the rule for an unexpected exit; or when it starts
// posting a review comment itself or runs the review in the background.
func TestWritePRRoborevStepCarriesTheContract(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			raw := writePRSection(t, body, "## V2b.")
			s := flat(raw)
			wantAll(t, s,
				"git fetch origin <default branch>",
				"lighthouse roborev-gate gate",
				"timeout 540 roborev ci review --agent codex --ref <range> > <raw> 2>&1",
				"In the foreground, with Bash timeout `600000`",
				"(`dangerouslyDisableSandbox: true`)",
				"follow `superpowers:receiving-code-review`",
				"the line `roborev: <path>:<line> (<severity>)`",
				"lighthouse roborev-gate classify",
				"lighthouse roborev-gate fixed",
				"lighthouse roborev-gate record --done <sha> --decisions <file> --range <range>",
				"This step also runs with `No issue:`",
				"git ls-remote --exit-code origin <branch>",
				"git push origin HEAD",
				"Any other non-zero exit of `lighthouse roborev-gate` than exit 3 of `gate` (step 0) and exit 2 of `record` (step 3)")
			wantNone(t, raw, "--comment", "run_in_background")
		})
	}
}

// Classification: fails when V4 stops adding the roborev body line or stops rendering and posting
// the roborev comment through lighthouse.
func TestWritePRV4WritesTheMarkerAndPostsTheComment(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			v4 := flat(writePRSection(t, body, "## V4."))
			wantAll(t, v4,
				"add the output of `lighthouse roborev-gate body-line`",
				"lighthouse roborev-comment render --pr <nr> --bron lokaal --state <state> --raw <raw> > <file>",
				"lighthouse forge pr comment <nr> --body-file <file>")
		})
	}
}

// Classification: fails when the documents step stops reading the taxonomy from config, stops
// reading the labels of every linked issue, loses its decision rule, its skip paths or its place
// between V3 and the changelog fragment.
func TestWritePRDocumentsOfTheLinkedIssues(t *testing.T) {
	for variant, body := range variants(t, writePR) {
		t.Run(variant, func(t *testing.T) {
			s := flat(writePRSection(t, body, "## Documents of the linked issues"))
			wantAll(t, s,
				"Read the taxonomy file with `lighthouse config paths.labels`.",
				"rc 1 (not set), or a path to a file that does not exist: there is no taxonomy file; skip this section, with no note in the body.",
				"rc 2: the configuration is broken; stop and report it, no PR.",
				"For every `Closes #N` and every `Refs #N` of the body, read the labels: `lighthouse forge issue view N --field labels`.",
				"does the branch deliver what the issue changes?",
				"- With `Closes #N`: yes.",
				"- With `Refs #N`: judge from the diff and the AC of the issue.",
				"commit it and push it before `lighthouse forge pr create`. No: do nothing, and leave no note in the body.",
				"A re-run writes nothing twice")
			wantOrder(t, body, "## V3. Choose the form", "## Documents of the linked issues", "## Changelog fragment")
		})
	}
}
