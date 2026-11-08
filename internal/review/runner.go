package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// Exit codes of a round. 1 is an unbuildable prompt or an incomplete result.
const (
	ExitOK          = 0
	ExitFailed      = 1
	ExitUsage       = 2
	ExitNoCodex     = 3
	ExitNoLogin     = 4
	ExitRunFailed   = 5
	ExitBudgetEmpty = 6
)

const defaultConfigToml = "model = \"gpt-5.6-sol\"\nmodel_reasoning_effort = \"high\"\n"

// Options is one round. The messages go to Stderr, the `ok` line to Stdout.
type Options struct {
	Kind       string // "spec" or "plan"
	Verify     bool
	SkillDir   string // empty: .claude/skills/lighthouse-review-<Kind> under the repo root
	Prompt     string // empty: reviewer-prompt.md or verifier-prompt.md in SkillDir
	Schema     string // empty: findings.schema.json or verification.schema.json in SkillDir
	Contexts   []string
	Doc        string
	Round      string
	Out        string
	Prior      string // empty counts as "none"
	Level      string
	Stdout     io.Writer
	Stderr     io.Writer
	ReadBudget func(context.Context) (usage.BudgetOutcome, int, string) // the in-process budget check
}

// RunRound runs one review round and returns its exit code. Every message is already printed.
func RunRound(ctx context.Context, o Options) int {
	if o.Prior == "" {
		o.Prior = "none"
	}
	fail := func(code int, format string, args ...any) int {
		_, _ = fmt.Fprintf(o.Stderr, format+"\n", args...)
		return code
	}

	repo, err := config.RepoRoot()
	if err != nil {
		return fail(ExitUsage, "%v", err)
	}
	skillDir := o.SkillDir
	if skillDir == "" {
		skillDir = ".claude/skills/lighthouse-review-" + o.Kind
	}
	if !filepath.IsAbs(skillDir) {
		skillDir = filepath.Join(repo, skillDir)
	}
	promptFile, schemaFile := o.Prompt, o.Schema
	if o.Verify {
		promptFile = orDefault(promptFile, filepath.Join(skillDir, "verifier-prompt.md"))
		schemaFile = orDefault(schemaFile, filepath.Join(skillDir, "verification.schema.json"))
	} else {
		promptFile = orDefault(promptFile, filepath.Join(skillDir, "reviewer-prompt.md"))
		schemaFile = orDefault(schemaFile, filepath.Join(skillDir, "findings.schema.json"))
	}

	if o.Doc == "" || o.Round == "" || o.Out == "" {
		return fail(ExitUsage, "usage: lighthouse review-run [--kind spec|plan] [--verify] [--skill-dir DIR] "+
			"[--prompt F] [--schema F] [--context PATH]... <DOC> <ROUND> <OUT> [PRIOR] [LEVEL]")
	}
	if !isFile(promptFile) {
		return fail(ExitUsage, "prompt template not found: %s", promptFile)
	}
	if !isFile(schemaFile) {
		return fail(ExitUsage, "schema not found: %s", schemaFile)
	}

	if _, err := exec.LookPath("codex"); err != nil {
		return fail(ExitNoCodex, "CODEX_MISSING: no codex in PATH")
	}

	if !isFile(filepath.Join(repo, o.Doc)) {
		return fail(ExitUsage, "document not found under the repo root: %s/%s", repo, o.Doc)
	}
	for _, c := range o.Contexts {
		if c == "" {
			continue
		}
		if filepath.IsAbs(c) {
			if !isFile(c) {
				return fail(ExitUsage, "context path not found: %s", c)
			}
		} else if !isFile(filepath.Join(repo, c)) {
			return fail(ExitUsage, "context path not found under the repo root: %s/%s", repo, c)
		}
	}

	// A clean CODEX_HOME: the reviewer inherits no marketplaces, plugins, MCP servers, hooks
	// or agents. They cost ~8k tokens and undermine the clean context this review rests on.
	home, _ := os.UserHomeDir()
	ch := os.Getenv("CODEX_SPECREVIEW_HOME")
	if ch == "" {
		ch = filepath.Join(home, ".codex-specreview")
	}
	if err := os.MkdirAll(ch, 0o755); err != nil {
		return fail(ExitFailed, "%v", err)
	}
	auth := filepath.Join(home, ".codex", "auth.json")
	if !isFile(auth) {
		return fail(ExitNoLogin, "CODEX_UNAUTHENTICATED: ~/.codex/auth.json missing")
	}
	if err := copyFile(auth, filepath.Join(ch, "auth.json")); err != nil { // takes a refreshed token along
		return fail(ExitFailed, "%v", err)
	}
	cfgPath := filepath.Join(ch, "config.toml")
	if info, err := os.Stat(cfgPath); err != nil || info.Size() == 0 {
		if err := os.WriteFile(cfgPath, []byte(defaultConfigToml), 0o644); err != nil {
			return fail(ExitFailed, "%v", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(o.Out), 0o755); err != nil {
		return fail(ExitFailed, "%v", err)
	}

	if code := checkBudget(ctx, o); code == ExitBudgetEmpty {
		return ExitBudgetEmpty
	} else if code < 0 {
		return ExitFailed
	}

	template, err := os.ReadFile(promptFile)
	if err != nil {
		return fail(ExitFailed, "%v", err)
	}
	prompt, err := BuildPrompt(template, o.Doc, repo, o.Round, o.Prior, o.Contexts)
	if err != nil {
		return fail(ExitFailed, "%v", err)
	}

	code, err := runCodex(ctx, o, ch, repo, schemaFile, prompt)
	if err != nil {
		return fail(ExitFailed, "%v", err)
	}
	if info, statErr := os.Stat(o.Out); code != 0 || statErr != nil || info.Size() == 0 {
		_, _ = fmt.Fprintf(o.Stderr, "CODEX_FAILED (exit %d). Last lines of %s.err:\n", code, o.Out)
		if data, err := os.ReadFile(o.Out + ".err"); err == nil {
			_, _ = o.Stderr.Write(tail(data, 15))
		}
		return ExitRunFailed
	}

	line, err := summary(o.Out, cfgPath)
	if err != nil {
		return fail(ExitFailed, "%v", err)
	}
	_, _ = fmt.Fprintln(o.Stdout, line)
	return ExitOK
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}

// checkBudget runs the budget check before Codex and writes `<OUT>.budget.json`. It returns 6
// when the budget is exhausted beforehand (after printing the CODEX_BUDGET line), else 0. A
// check that fails, panics or gives no JSON object counts as not measurable: the round goes on.
func checkBudget(ctx context.Context, o Options) int {
	budgetPath := o.Out + ".budget.json"
	code, data := runBudget(ctx, o)
	if err := os.WriteFile(budgetPath, data, 0o644); err != nil {
		_, _ = fmt.Fprintf(o.Stderr, "%v\n", err)
		return -1
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err == nil && doc != nil && (code == 0 || code == 6) {
		if code == 6 {
			reason, _ := doc["reason"].(string)
			_, _ = fmt.Fprintf(o.Stderr, "CODEX_BUDGET: fallback — %s\n", reason)
		}
		return code
	}
	// The fields of the budget answer, in its JSON order.
	synth := fmt.Sprintf(`{"decision": "not_measurable", "reason": "budget check failed: exit %d", `+
		`"window": null, "reset_at": null, "session_pct": null, "week_pct": null, `+
		`"source": null, "lower_bound": null, "thresholds": null}`+"\n", code)
	if err := os.WriteFile(budgetPath, []byte(synth), 0o644); err != nil {
		_, _ = fmt.Fprintf(o.Stderr, "%v\n", err)
		return -1
	}
	return 0
}

// runBudget returns the exit code and the stdout of the budget check: the seam command from
// LIGHTHOUSE_CODEX_BUDGET_CMD (split on spaces), else the in-process check.
func runBudget(ctx context.Context, o Options) (code int, stdout []byte) {
	if seam := os.Getenv("LIGHTHOUSE_CODEX_BUDGET_CMD"); seam != "" {
		argv := strings.Fields(seam)
		if len(argv) == 0 {
			return 127, nil
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, o.Stderr
		return exitCode(cmd.Run()), buf.Bytes()
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(o.Stderr, "budget check failed: %v\n", r)
			code, stdout = 1, nil
		}
	}()
	if o.ReadBudget == nil {
		return 1, nil
	}
	outcome, code, stderr := o.ReadBudget(ctx)
	if stderr != "" {
		_, _ = io.WriteString(o.Stderr, stderr)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(outcome); err != nil {
		return 1, nil
	}
	return code, buf.Bytes()
}

// exitCode is the exit status of a finished command: 127 when it did not start, as bash says.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 127
}

func runCodex(ctx context.Context, o Options, codexHome, repo, schema, prompt string) (int, error) {
	args := []string{"exec", "--sandbox", "read-only"}
	if o.Level != "" {
		args = append(args, "-c", "model_reasoning_effort="+o.Level)
	}
	args = append(args, "--cd", repo, "--output-schema", schema, "-o", o.Out, "--json", prompt)

	events, err := os.Create(o.Out + ".events")
	if err != nil {
		return 0, err
	}
	errFile, err := os.Create(o.Out + ".err")
	if err != nil {
		return 0, errors.Join(err, events.Close())
	}

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+codexHome)
	cmd.Stdout, cmd.Stderr = events, errFile // stdin stays empty: /dev/null
	code := exitCode(cmd.Run())
	return code, errors.Join(events.Close(), errFile.Close())
}

// tail is the last n lines of data, as `tail -n`.
func tail(data []byte, n int) []byte {
	end := len(data)
	if end > 0 && data[end-1] == '\n' {
		end--
	}
	start := end
	for seen := 0; start > 0; start-- {
		if data[start-1] == '\n' {
			if seen++; seen == n {
				break
			}
		}
	}
	return data[start:]
}

// summary checks the result and builds the `ok | reviewer: …` line.
func summary(out, cfgPath string) (string, error) {
	data, err := os.ReadFile(out)
	if err != nil {
		return "", err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("the result is not a JSON object: %w", err)
	}
	var missing []string
	for _, k := range []string{"review_markdown", "status", "findings"} {
		if _, ok := doc[k]; !ok {
			missing = append(missing, "'"+k+"'")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return "", fmt.Errorf("incomplete result, missing [%s]", strings.Join(missing, ", "))
	}

	var findings []struct {
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(doc["findings"], &findings); err != nil {
		return "", fmt.Errorf("findings: %w", err)
	}
	var status string
	if err := json.Unmarshal(doc["status"], &status); err != nil {
		status = strings.TrimSpace(string(doc["status"]))
	}
	count := map[string]int{}
	for _, f := range findings {
		count[f.Severity]++
	}

	model := "unknown"
	if cfg, err := os.ReadFile(cfgPath); err == nil {
		for _, line := range strings.Split(string(cfg), "\n") {
			if strings.HasPrefix(line, "model =") {
				if parts := strings.Split(line, `"`); len(parts) > 1 {
					model = parts[1]
				}
				break
			}
		}
	}
	return fmt.Sprintf("ok | reviewer: %s | status: %s | %d findings (%dB/%dM/%dm) | raw: %s",
		model, status, len(findings), count["BLOCKER"], count["MAJOR"], count["MINOR"], out), nil
}
