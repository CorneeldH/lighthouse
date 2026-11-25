package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

// fakeCodexPrompt is a `codex` that records the prompt (its last argument) and the value of
// --output-schema next to $CAPTURE, and answers `exec` with a clean result.
const fakeCodexPrompt = `for a; do last=$a; done
printf '%s' "$last" > "$CAPTURE.prompt"
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2" ;;
    --output-schema) schema="$2" ;;
  esac
  shift
done
printf '%s' "$schema" > "$CAPTURE.schema"
echo '{"review_markdown":"x","status":"Clean","findings":[]}' > "$out"
`

// cleanRoundEnv blanks what a controller run leaves in the environment.
func cleanRoundEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	t.Setenv("LIGHTHOUSE_TOML", "")
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pytest: test_plan_kind_sends_plan_prompt_schema_and_context, test_agents_skill_dir_reads_its_own_templates,
// test_plan_verify_sends_verifier_prompt_and_schema, test_spec_verify_sends_verifier_prompt_and_schema.
func TestRoundPicksPromptAndSchemaByKindVerifyAndSkillDir(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		verify     bool
		skillDir   string // empty: the default under the repo root
		contexts   func(repo, tmp string) []string
		wantPrompt func(repo, tmp string) string
		wantSchema string // relative to the repo root, or absolute when skillDir is
	}{
		{
			name: "plan review", kind: "plan",
			contexts:   func(string, string) []string { return []string{docsDir + "spec.md"} },
			wantPrompt: func(string, string) string { return "CLAUDE-PLAN-REVIEW docs/doc.md\n- docs/spec.md" },
			wantSchema: ".claude/skills/lighthouse-review-plan/findings.schema.json",
		},
		{
			name: "plan verification", kind: "plan", verify: true,
			contexts: func(_, tmp string) []string { return []string{filepath.Join(tmp, "round-2.fixes.diff")} },
			wantPrompt: func(_, tmp string) string {
				return "CLAUDE-PLAN-VERIFY docs/doc.md\n- " + filepath.Join(tmp, "round-2.fixes.diff")
			},
			wantSchema: ".claude/skills/lighthouse-review-plan/verification.schema.json",
		},
		{
			name: "spec verification", kind: "spec", verify: true,
			contexts: func(_, tmp string) []string { return []string{filepath.Join(tmp, "round-2.fixes.diff")} },
			wantPrompt: func(_, tmp string) string {
				return "CLAUDE-SPEC-VERIFY docs/doc.md\n- " + filepath.Join(tmp, "round-2.fixes.diff")
			},
			wantSchema: ".claude/skills/lighthouse-review-spec/verification.schema.json",
		},
		{
			name: "repo-relative skill dir", kind: "plan", skillDir: ".agents/skills/lighthouse-review-plan",
			contexts:   func(string, string) []string { return nil },
			wantPrompt: func(string, string) string { return "AGENTS-PLAN-REVIEW docs/doc.md\n(none)" },
			wantSchema: ".agents/skills/lighthouse-review-plan/findings.schema.json",
		},
		{
			name: "absolute skill dir", kind: "plan", skillDir: "@TMP@/abs-skill",
			contexts:   func(string, string) []string { return nil },
			wantPrompt: func(string, string) string { return "ABS-PLAN-REVIEW docs/doc.md\n(none)" },
			wantSchema: "@TMP@/abs-skill/findings.schema.json",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRound(t)
			cleanRoundEnv(t)
			repo := os.Getenv("LIGHTHOUSE_REPO_ROOT")
			tmp := t.TempDir()
			bin := t.TempDir()
			stubbin.RequirePOSIX(t, "fakeCodexPrompt reads its last argument and walks the options with shift")
			stubbin.MakeScript(t, bin, "codex", fakeCodexPrompt)
			stubbin.Prepend(t, bin)
			put(t, filepath.Join(repo, docsDir+"spec.md"), "# spec\n")
			put(t, filepath.Join(tmp, "round-2.fixes.diff"), "--- a\n+++ b\n")
			for _, kind := range []string{"spec", "plan"} {
				dir := filepath.Join(repo, ".claude/skills/lighthouse-review-"+kind)
				up := strings.ToUpper(kind)
				body := func(what string) string {
					return "---BEGIN PROMPT---\nCLAUDE-" + up + "-" + what + " {{DOC_PATH}}\n{{CONTEXT_PATHS}}\n---END PROMPT---\n"
				}
				put(t, filepath.Join(dir, "reviewer-prompt.md"), body("REVIEW"))
				put(t, filepath.Join(dir, "verifier-prompt.md"), body("VERIFY"))
				put(t, filepath.Join(dir, "findings.schema.json"), "{}")
				put(t, filepath.Join(dir, "verification.schema.json"), "{}")
			}
			skillDir := strings.ReplaceAll(c.skillDir, "@TMP@", tmp)
			if c.skillDir != "" {
				dir := skillDir
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(repo, dir)
				}
				marker := "AGENTS"
				if filepath.IsAbs(skillDir) {
					marker = "ABS"
				}
				put(t, filepath.Join(dir, "reviewer-prompt.md"),
					"---BEGIN PROMPT---\n"+marker+"-PLAN-REVIEW {{DOC_PATH}}\n{{CONTEXT_PATHS}}\n---END PROMPT---\n")
				put(t, filepath.Join(dir, "findings.schema.json"), "{}")
			}
			r.opts.Kind, r.opts.Verify, r.opts.SkillDir = c.kind, c.verify, skillDir
			r.opts.Contexts = c.contexts(repo, tmp)
			r.opts.ReadBudget = nil
			t.Setenv("LIGHTHOUSE_CODEX_BUDGET_CMD", writeSeam(t, `printf '%s\n' '{"decision": "codex", "reason": "ok"}'`))
			if code := r.run(); code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, r.stderr.String())
			}
			if got, want := string(readFile(t, r.capture+".prompt")), c.wantPrompt(repo, tmp); got != want {
				t.Errorf("prompt = %q, want %q", got, want)
			}
			want := strings.ReplaceAll(c.wantSchema, "@TMP@", tmp)
			if !filepath.IsAbs(want) {
				want = filepath.Join(repo, want)
			}
			if got := string(readFile(t, r.capture+".schema")); got != want {
				t.Errorf("--output-schema = %q, want %q", got, want)
			}
		})
	}
}

// writeSeam writes an executable `sh` stub and returns its path, for LIGHTHOUSE_CODEX_BUDGET_CMD.
func writeSeam(t *testing.T, body string) string {
	t.Helper()
	stubbin.RequirePOSIX(t, "the budget seam is a sh script")
	return stubbin.MakeScript(t, t.TempDir(), "budget-stub", body+"\n")
}

// pytest: test_missing_context_path_is_a_usage_error
func TestMissingContextPathIsAUsageError(t *testing.T) {
	cases := []struct {
		name    string
		context string // @REPO@ stands for the repo root
		want    string
	}{
		{"repo-relative", docsDir + "does-not-exist.md", "context path not found under the repo root: @REPO@/docs/does-not-exist.md\n"},
		{"absolute", "@REPO@/gone.diff", "context path not found: @REPO@/gone.diff\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRound(t)
			cleanRoundEnv(t)
			repo := os.Getenv("LIGHTHOUSE_REPO_ROOT")
			put(t, filepath.Join(repo, ".claude/skills/lighthouse-review-plan/reviewer-prompt.md"),
				"---BEGIN PROMPT---\n{{DOC_PATH}} {{CONTEXT_PATHS}}\n---END PROMPT---\n")
			put(t, filepath.Join(repo, ".claude/skills/lighthouse-review-plan/findings.schema.json"), "{}")
			r.opts.Kind = "plan"
			r.opts.Contexts = []string{strings.ReplaceAll(c.context, "@REPO@", repo)}
			if code := r.run(); code != ExitUsage {
				t.Fatalf("exit %d, want 2; stderr: %s", code, r.stderr.String())
			}
			if got, want := r.stderr.String(), strings.ReplaceAll(c.want, "@REPO@", repo); got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
			if _, err := os.Stat(r.capture); err == nil {
				t.Error("codex exec was called")
			}
		})
	}
}

const notMeasurableTail = `"window": null, "reset_at": null, "session_pct": null, "week_pct": null, ` +
	`"source": null, "lower_bound": null, "thresholds": null, "reset_credits": null}` + "\n"

// pytest: test_budget_fallback_stops_before_codex (the seam command), test_broken_budget_check_is_not_measurable_and_codex_runs,
// test_budget_codex_writes_record_and_runs.
func TestBudgetSeamAnswer(t *testing.T) {
	cases := []struct {
		name       string
		stub       string
		code       int
		stderr     string
		budget     string
		codexCalls bool
	}{
		{"codex", `printf '%s\n' '{"decision": "codex", "reason": "below the thresholds"}'`,
			0, "", `{"decision": "codex", "reason": "below the thresholds"}` + "\n", true},
		{"fallback", `printf '%s\n' '{"decision": "fallback", "reason": "5h 92% ≥ 90%"}'; exit 6`,
			6, "CODEX_BUDGET: fallback — 5h 92% ≥ 90%\n", `{"decision": "fallback", "reason": "5h 92% ≥ 90%"}` + "\n", false},
		{"no json, exit 6", `echo 'no json'; exit 6`,
			0, "", `{"decision": "not_measurable", "reason": "budget check failed: exit 6", ` + notMeasurableTail, true},
		{"crash, exit 1", `echo boem >&2; exit 1`,
			0, "boem\n", `{"decision": "not_measurable", "reason": "budget check failed: exit 1", ` + notMeasurableTail, true},
		{"a list, exit 0", `echo '[]'`,
			0, "", `{"decision": "not_measurable", "reason": "budget check failed: exit 0", ` + notMeasurableTail, true},
		{"a list, exit 6", `echo '[]'; exit 6`,
			0, "", `{"decision": "not_measurable", "reason": "budget check failed: exit 6", ` + notMeasurableTail, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRound(t)
			cleanRoundEnv(t)
			t.Setenv("LIGHTHOUSE_CODEX_BUDGET_CMD", writeSeam(t, c.stub))
			if code := r.run(); code != c.code {
				t.Fatalf("exit %d, want %d; stderr: %s", code, c.code, r.stderr.String())
			}
			if got := r.stderr.String(); got != c.stderr {
				t.Errorf("stderr = %q, want %q", got, c.stderr)
			}
			if got := string(readFile(t, r.opts.Out+".budget.json")); got != c.budget {
				t.Errorf("budget.json = %q, want %q", got, c.budget)
			}
			_, err := os.Stat(r.capture)
			if called := err == nil; called != c.codexCalls {
				t.Errorf("codex exec called = %v, want %v", called, c.codexCalls)
			}
		})
	}
}

// pytest: test_paired_round_one_without_python3 (the review-run step: the rest of the chain has
// its own tests, TestStartReservesAnAttemptWithTheReviewedCommit and TestPairFixtureCases)
func TestRoundRunsOnAPathWithOnlyCodex(t *testing.T) {
	r := newRound(t)
	cleanRoundEnv(t)
	bin := t.TempDir()
	stubbin.RequirePOSIX(t, "fakeCodex records argv with printf and shifts positional parameters")
	stubbin.MakeScript(t, bin, "codex", fakeCodex)
	stubbin.Alone(t, bin)
	t.Setenv("LIGHTHOUSE_CODEX_BUDGET_CMD", writeSeam(t, `printf '%s\n' '{"decision": "codex", "reason": "ok"}'`))
	if code := r.run(); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, r.stderr.String())
	}
	if got, want := r.stderr.String(), ""; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}
