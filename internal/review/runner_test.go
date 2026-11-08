package review

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

// fakeCodex is a `codex` that records its argv and answers `exec` with a clean result.
const fakeCodex = `#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE"
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; fi
  shift
done
echo '{"review_markdown":"x","status":"Clean","findings":[{"severity":"MAJOR"}]}' > "$out"
`

type round struct {
	opts    Options
	capture string
	stdout  bytes.Buffer
	stderr  bytes.Buffer
}

// newRound sets a repo with a document, a fake codex on the PATH and a clean HOME.
func newRound(t *testing.T) *round {
	t.Helper()
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	bin := filepath.Join(tmp, "bin")
	home := filepath.Join(tmp, "home")
	for _, d := range []string{filepath.Join(repo, "docs"), bin, filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "docs", "doc.md"), "# doc\n", 0o644)
	write(filepath.Join(home, ".codex", "auth.json"), "{}", 0o600)
	write(filepath.Join(bin, "codex"), fakeCodex, 0o755)
	skill := filepath.Join(repo, ".claude", "skills", "lighthouse-review-spec")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(skill, "reviewer-prompt.md"), "---BEGIN PROMPT---\nSpec: {{DOC_PATH}} round {{ROUND}}\n---END PROMPT---\n", 0o644)
	write(filepath.Join(skill, "findings.schema.json"), "{}", 0o644)

	r := &round{capture: filepath.Join(tmp, "capture.txt")}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("CODEX_SPECREVIEW_HOME", filepath.Join(tmp, "ch"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", repo)
	t.Setenv("LIGHTHOUSE_CODEX_BUDGET_CMD", "")
	t.Setenv("CAPTURE", r.capture)
	r.opts = Options{
		Kind: "spec", Doc: "docs/doc.md", Round: "1", Out: filepath.Join(tmp, "out", "round-1.json"),
		Stdout: &r.stdout, Stderr: &r.stderr,
	}
	return r
}

func (r *round) run() int { return RunRound(context.Background(), r.opts) }

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFile(t, path), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBudgetPanicIsNotMeasurable(t *testing.T) {
	r := newRound(t)
	r.opts.ReadBudget = func(context.Context) (usage.BudgetOutcome, int, string) { panic("boem") }
	if code := r.run(); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, r.stderr.String())
	}
	if _, err := os.Stat(r.capture); err != nil {
		t.Error("codex exec was not called after a panicking budget check")
	}
	data := readFile(t, r.opts.Out+".budget.json")
	want := `{"decision": "not_measurable", "reason": "budget check failed: exit 1", "window": null, "reset_at": null, ` +
		`"session_pct": null, "week_pct": null, "source": null, "lower_bound": null, "thresholds": null}` + "\n"
	if string(data) != want {
		t.Errorf("budget.json = %q, want %q", data, want)
	}
}

func TestBudgetFallbackStopsBeforeCodex(t *testing.T) {
	r := newRound(t)
	r.opts.ReadBudget = func(context.Context) (usage.BudgetOutcome, int, string) {
		return usage.BudgetOutcome{Decision: "fallback", Reason: "5h 92% ≥ 90%"}, 6, ""
	}
	if code := r.run(); code != ExitBudgetEmpty {
		t.Fatalf("exit %d, want 6", code)
	}
	if got := r.stderr.String(); got != "CODEX_BUDGET: fallback — 5h 92% ≥ 90%\n" {
		t.Errorf("stderr = %q", got)
	}
	if _, err := os.Stat(r.capture); err == nil {
		t.Error("codex exec was called")
	}
	if readJSON(t, r.opts.Out+".budget.json")["decision"] != "fallback" {
		t.Error("budget.json lacks decision fallback")
	}
}

func TestRoundRunsCodexAndSummarises(t *testing.T) {
	r := newRound(t)
	r.opts.Level = "max"
	r.opts.ReadBudget = func(context.Context) (usage.BudgetOutcome, int, string) {
		return usage.BudgetOutcome{Decision: "codex", Reason: "ok"}, 0, ""
	}
	if code := r.run(); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, r.stderr.String())
	}
	argv := strings.Split(strings.TrimSuffix(string(readFile(t, r.capture)), "\n"), "\n")
	repo := os.Getenv("LIGHTHOUSE_REPO_ROOT")
	want := []string{
		"exec", "--sandbox", "read-only", "-c", "model_reasoning_effort=max", "--cd", repo,
		"--output-schema", filepath.Join(repo, ".claude/skills/lighthouse-review-spec/findings.schema.json"),
		"-o", r.opts.Out, "--json", "Spec: docs/doc.md round 1",
	}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	wantLine := "ok | reviewer: gpt-5.6-sol | status: Clean | 1 findings (0B/1M/0m) | raw: " + r.opts.Out + "\n"
	if got := r.stdout.String(); got != wantLine {
		t.Errorf("stdout = %q, want %q", got, wantLine)
	}
	if got := string(readFile(t, filepath.Join(os.Getenv("CODEX_SPECREVIEW_HOME"), "config.toml"))); got != defaultConfigToml {
		t.Errorf("config.toml = %q", got)
	}
}

func TestMissingDocumentIsAUsageErrorAfterTheCodexCheck(t *testing.T) {
	r := newRound(t)
	r.opts.Doc = "docs/missing.md"
	if code := r.run(); code != ExitUsage {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(r.stderr.String(), "document not found under the repo root") {
		t.Errorf("stderr = %q", r.stderr.String())
	}
	t.Setenv("PATH", t.TempDir())
	r.stderr.Reset()
	if code := r.run(); code != ExitNoCodex {
		t.Fatalf("exit %d, want 3", code)
	}
	if got := r.stderr.String(); got != "CODEX_MISSING: no codex in PATH\n" {
		t.Errorf("stderr = %q", got)
	}
}

func TestTail(t *testing.T) {
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, string(rune('a'+i-1)))
	}
	text := strings.Join(lines, "\n") + "\n"
	got := string(tail([]byte(text), 15))
	if want := strings.Join(lines[5:], "\n") + "\n"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if got := string(tail([]byte("a\nb"), 15)); got != "a\nb" {
		t.Errorf("short tail = %q", got)
	}
}

func TestRoundFailureExits(t *testing.T) {
	budgetOK := func(context.Context) (usage.BudgetOutcome, int, string) {
		return usage.BudgetOutcome{Decision: "codex", Reason: "ok"}, 0, ""
	}
	cases := []struct {
		name   string
		setup  func(t *testing.T, r *round)
		code   int
		stderr string
	}{
		{"not logged in", func(t *testing.T, _ *round) {
			if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".codex", "auth.json")); err != nil {
				t.Fatal(err)
			}
		}, ExitNoLogin, "CODEX_UNAUTHENTICATED: ~/.codex/auth.json missing\n"},
		{"codex fails", func(t *testing.T, _ *round) {
			bin := t.TempDir()
			script := "#!/bin/sh\necho boom >&2\nexit 7\n"
			if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		}, ExitRunFailed, "boom\n"},
		{"incomplete result", func(t *testing.T, _ *round) {
			bin := t.TempDir()
			script := "#!/bin/sh\nwhile [ \"$1\" != -o ]; do shift; done\necho '{\"status\":\"x\"}' > \"$2\"\n"
			if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		}, ExitFailed, "incomplete result, missing ['findings', 'review_markdown']\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRound(t)
			r.opts.ReadBudget = budgetOK
			c.setup(t, r)
			if code := r.run(); code != c.code {
				t.Fatalf("exit %d, want %d; stderr: %s", code, c.code, r.stderr.String())
			}
			if !strings.Contains(r.stderr.String(), c.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", r.stderr.String(), c.stderr)
			}
		})
	}
}
