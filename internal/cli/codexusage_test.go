package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

// Ports of the cmd_read / cmd_budget / main cases of tests/pack/test_codex_usage.py.

type codexRun struct {
	stdout, stderr string
	code           int
}

func runCodexUsage(t *testing.T, read func(context.Context, time.Duration) (usage.CodexState, error), args ...string) codexRun {
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newCodexUsageCommand(read))
	root.SetArgs(append([]string{"codex-usage"}, args...))
	code := execute(root, &errOut)
	return codexRun{out.String(), errOut.String(), code}
}

func writeCodexConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte("[project]\nlanguage = \"en\"\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
}

func codexReading(session, week int) usage.CodexState {
	return usage.CodexState{
		Source: "app-server", GeneratedAt: "2099-01-01T00:00:00Z",
		Session: usage.CodexWindow{Pct: session, ResetAt: "2099-01-01T05:00:00Z"},
		Week:    usage.CodexWindow{Pct: week, ResetAt: "2099-01-05T00:00:00Z"},
	}
}

func readOK(s usage.CodexState) func(context.Context, time.Duration) (usage.CodexState, error) {
	return func(context.Context, time.Duration) (usage.CodexState, error) { return s, nil }
}

func readFails(msg string) func(context.Context, time.Duration) (usage.CodexState, error) {
	return func(context.Context, time.Duration) (usage.CodexState, error) {
		return usage.CodexState{}, errors.New(msg)
	}
}

// pytest: test_read_print_json
func TestCodexUsageReadPrintsIndentedJSON(t *testing.T) {
	writeCodexConfig(t, "")
	got := runCodexUsage(t, readOK(codexReading(55, 66)), "read")
	if got.code != 0 || got.stderr != "" {
		t.Fatalf("code = %d, stderr = %q", got.code, got.stderr)
	}
	if !strings.HasPrefix(got.stdout, "{\n  \"source\": \"app-server\",\n") {
		t.Errorf("stdout is not indented by 2: %q", got.stdout)
	}
	var state usage.CodexState
	if err := json.Unmarshal([]byte(got.stdout), &state); err != nil || state.Session.Pct != 55 || state.Source != "app-server" {
		t.Errorf("stdout = %q (%v)", got.stdout, err)
	}
}

// pytest: test_read_not_measurable
func TestCodexUsageReadNotMeasurable(t *testing.T) {
	writeCodexConfig(t, "")
	got := runCodexUsage(t, readFails("x"), "read")
	if got != (codexRun{"", "not measurable: x\n", 1}) {
		t.Errorf("got %+v", got)
	}
}

// pytest: test_read_config_load_error_is_not_measurable_exit_1
func TestCodexUsageReadConfigErrorIsNotMeasurable(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone.toml")
	t.Setenv("LIGHTHOUSE_TOML", gone)
	got := runCodexUsage(t, readOK(codexReading(1, 1)), "read")
	if got.code != 1 || got.stdout != "" || !strings.HasPrefix(got.stderr, "not measurable: config: ") || !strings.Contains(got.stderr, gone) {
		t.Errorf("got %+v", got)
	}
}

// pytest: test_read_passes_configured_timeout
func TestCodexUsageReadPassesConfiguredTimeout(t *testing.T) {
	writeCodexConfig(t, "[codex_usage]\napp_server_timeout_seconds = 7\n")
	var seen time.Duration
	read := func(_ context.Context, d time.Duration) (usage.CodexState, error) {
		seen = d
		return codexReading(1, 1), nil
	}
	if got := runCodexUsage(t, read, "read"); got.code != 0 || seen != 7*time.Second {
		t.Errorf("code = %d, timeout = %v", got.code, seen)
	}
}

// pytest: TestBudgetCli.test_exit_and_nine_keys_without_marker
func TestCodexUsageBudgetOneLineAndExitCode(t *testing.T) {
	writeCodexConfig(t, "")
	for _, tc := range []struct {
		read     func(context.Context, time.Duration) (usage.CodexState, error)
		code     int
		decision string
	}{
		{readOK(codexReading(92, 41)), 6, "fallback"},
		{readOK(codexReading(10, 10)), 0, "codex"},
		{readFails("gone"), 0, "not_measurable"},
	} {
		got := runCodexUsage(t, tc.read, "budget")
		if got.code != tc.code || got.stderr != "" || strings.Count(got.stdout, "\n") != 1 || !strings.HasSuffix(got.stdout, "}\n") {
			t.Errorf("%s: %+v", tc.decision, got)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(got.stdout), &data); err != nil || data["decision"] != tc.decision {
			t.Errorf("%s: stdout = %q (%v)", tc.decision, got.stdout, err)
		}
	}
}

// pytest: test_fallback_carries_window_and_thresholds (raw UTF-8, ensure_ascii=False)
func TestCodexUsageBudgetPrintsRawUTF8(t *testing.T) {
	writeCodexConfig(t, "")
	got := runCodexUsage(t, readOK(codexReading(92, 41)), "budget")
	if !strings.Contains(got.stdout, `"reason":"5h 92% ≥ 90%"`) {
		t.Errorf("stdout = %q", got.stdout)
	}
}

// pytest: test_broken_config_is_said_on_stderr
func TestCodexUsageBudgetBrokenConfigIsSaidOnStderr(t *testing.T) {
	writeCodexConfig(t, "[codex_usage]\nreview_pct_week = \"x\"\n")
	got := runCodexUsage(t, readOK(codexReading(10, 10)), "budget")
	if got.code != 0 || !strings.HasPrefix(got.stderr, "codex-usage: config: ") || !strings.Contains(got.stdout, `"decision":"not_measurable"`) {
		t.Errorf("got %+v", got)
	}
}

// pytest: test_main_requires_a_subcommand, test_gate_is_not_a_subcommand (argparse: exit 2)
func TestCodexUsageUsageErrorsExitTwo(t *testing.T) {
	for name, args := range map[string][]string{
		"no subcommand":   {},
		"gate":            {"gate"},
		"extra argument":  {"budget", "now"},
		"unknown flag":    {"budget", "--x"},
		"read extra args": {"read", "x"},
	} {
		got := runCodexUsage(t, readFails("unused"), args...)
		if got.code != 2 || got.stdout != "" || got.stderr == "" {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestUsageErrWrapsAsExitTwo(t *testing.T) {
	var ee *exitError
	err := usageErr(errors.New("bad"))
	if !errors.As(err, &ee) || ee.code != 2 || err.Error() != "bad" {
		t.Errorf("err = %#v", err)
	}
}

// The command is registered under the script's name with dashes.
func TestCodexUsageIsRegistered(t *testing.T) {
	root := NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if cmd, _, err := root.Find([]string{"codex-usage", "budget"}); err != nil || cmd.Name() != "budget" {
		t.Errorf("Find = %v, %v", cmd, err)
	}
}
