package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/stubbin"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/testpath"
)

// fakeReviewCodex records that it ran and the value of --cd, and answers with a clean result.
const fakeReviewCodex = `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2" ;;
    --cd) cd_to="$2" ;;
  esac
  shift
done
printf '%s' "$cd_to" > "$CAPTURE"
echo '{"review_markdown":"x","status":"Clean","findings":[]}' > "$out"
`

type reviewEnv struct {
	repo, out, capture string
}

// reviewSandbox sets a repo with a spec skill and a document, a fake codex on a PATH of its own,
// a clean HOME and an environment without the controller's variables.
func reviewSandbox(t *testing.T) reviewEnv {
	t.Helper()
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	bin := filepath.Join(tmp, "bin")
	home := filepath.Join(tmp, "home")
	put := func(path, body string, mode os.FileMode) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(repo, "docs", "doc.md"), "# doc\n", 0o644)
	skill := filepath.Join(repo, ".claude", "skills", "lighthouse-review-spec")
	put(filepath.Join(skill, "reviewer-prompt.md"), "---BEGIN PROMPT---\nSpec: {{DOC_PATH}}\n---END PROMPT---\n", 0o644)
	put(filepath.Join(skill, "findings.schema.json"), "{}", 0o644)
	stubbin.RequirePOSIX(t, "argument loop with case on $1 and $CAPTURE from the environment")
	stubbin.MakeScript(t, bin, "codex", strings.TrimPrefix(fakeReviewCodex, "#!/bin/sh\n"))
	put(filepath.Join(home, ".codex", "auth.json"), "{}", 0o600)
	stubbin.Prepend(t, bin)
	testpath.SetHome(t, home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CODEX_SPECREVIEW_HOME", filepath.Join(tmp, "ch"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", repo)
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "")
	t.Setenv("LIGHTHOUSE_CODEX_BUDGET_CMD", "")
	t.Setenv("CAPTURE", filepath.Join(tmp, "capture"))
	return reviewEnv{repo: repo, out: filepath.Join(tmp, "out", "round-1.json"), capture: filepath.Join(tmp, "capture")}
}

func runReviewRun(t *testing.T, session int, args ...string) codexRun {
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newReviewRunCommand(readOK(codexReading(session, 1))))
	root.SetArgs(append([]string{"review-run"}, args...))
	code := execute(root, &errOut)
	return codexRun{out.String(), errOut.String(), code}
}

func writeReviewToml(t *testing.T, dir string) {
	t.Helper()
	body := "[project]\nlanguage = \"en\"\n\n[codex_usage]\nreview_pct_session = 40\n"
	if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// pytest: test_production_path_reads_the_budget_without_a_seam, test_production_path_below_the_threshold_runs_codex
func TestReviewRunBudgetUsesTheThresholdsOfTheRepoConfig(t *testing.T) {
	cases := []struct {
		name    string
		session int
		code    int
		stderr  string
		budget  string
		runs    bool
	}{
		{"above the session threshold", 50, 6, "CODEX_BUDGET: fallback — 5h 50% ≥ 40%\n",
			`{"decision":"fallback","reason":"5h 50% ≥ 40%","window":"codex-5h","reset_at":"2099-01-01T05:00:00Z",` +
				`"session_pct":50,"week_pct":1,"source":"app-server","lower_bound":false,"thresholds":{"session":40,"week":95},"reset_credits":null}` + "\n", false},
		{"below the session threshold", 10, 0, "",
			`{"decision":"codex","reason":"ok: 5h 10%, week 1%","window":null,"reset_at":null,` +
				`"session_pct":10,"week_pct":1,"source":"app-server","lower_bound":false,"thresholds":{"session":40,"week":95},"reset_credits":null}` + "\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := reviewSandbox(t)
			writeReviewToml(t, env.repo)
			got := runReviewRun(t, c.session, "--kind", "spec", "docs/doc.md", "1", env.out)
			if got.code != c.code || got.stderr != c.stderr {
				t.Fatalf("code = %d, stderr = %q; want %d, %q", got.code, got.stderr, c.code, c.stderr)
			}
			if _, err := os.Stat(env.capture); (err == nil) != c.runs {
				t.Errorf("codex exec called = %v, want %v", err == nil, c.runs)
			}
			budget, err := os.ReadFile(env.out + ".budget.json")
			if err != nil {
				t.Fatal(err)
			}
			if string(budget) != c.budget {
				t.Errorf("budget.json = %s, want %s", budget, c.budget)
			}
		})
	}
}

// pytest: test_repo_root_from_lighthouse_repo_root
func TestReviewRunTakesTheRepoFromLighthouseRepoRoot(t *testing.T) {
	env := reviewSandbox(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(filepath.Join(other, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"docs/doc.md", ".claude/skills/lighthouse-review-spec/reviewer-prompt.md", ".claude/skills/lighthouse-review-spec/findings.schema.json"} {
		data, err := os.ReadFile(filepath.Join(env.repo, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(other, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeReviewToml(t, other) // the repo of the cwd has none
	t.Setenv("LIGHTHOUSE_REPO_ROOT", other)
	t.Chdir(env.repo)
	if got := runReviewRun(t, 50, "docs/doc.md", "1", env.out); got.code != 6 {
		t.Fatalf("code = %d, stderr = %q; want 6 from the thresholds of %s", got.code, got.stderr, other)
	}
	if got := runReviewRun(t, 10, "docs/doc.md", "1", env.out); got.code != 0 {
		t.Fatalf("code = %d, stderr = %q", got.code, got.stderr)
	}
	cd, err := os.ReadFile(env.capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(cd) != other {
		t.Errorf("--cd = %q, want %q", cd, other)
	}
}

// pytest: test_runner_messages_are_english
func TestReviewRunUsageErrorsExitTwo(t *testing.T) {
	reviewSandbox(t)
	got := runReviewRun(t, 10, "--kind", "spec")
	if got.code != 2 || !strings.HasPrefix(got.stderr, "usage: lighthouse review-run") {
		t.Errorf("no arguments: code = %d, stderr = %q", got.code, got.stderr)
	}
	got = runReviewRun(t, 10, "--bogus")
	if got.code != 2 || got.stdout != "" {
		t.Errorf("unknown flag: code = %d, stdout = %q, stderr = %q", got.code, got.stdout, got.stderr)
	}
}
