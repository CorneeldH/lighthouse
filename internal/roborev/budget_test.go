package roborev

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

func budgetEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	toml := filepath.Join(dir, "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[project]\nlanguage = \"nl\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex-home"))
	t.Setenv("PATH", dir)
}

func TestDefaultBudgetUnreadableCodexIsNotMeasurable(t *testing.T) {
	budgetEnv(t)
	code, body := DefaultBudget()()
	var got struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if code != 0 || got.Decision != "not_measurable" {
		t.Errorf("code %d, decision %q; want 0, not_measurable", code, got.Decision)
	}
}

func TestDefaultBudgetFallbackIsExitSix(t *testing.T) {
	budgetEnv(t)
	read := func(context.Context, time.Duration) (usage.CodexState, error) {
		state := usage.CodexState{Source: "app-server"}
		state.Session = usage.CodexWindow{Pct: 99, ResetAt: "2999-01-01T00:00:00Z"}
		return state, nil
	}
	code, body := budgetFrom(read)()
	var got struct{ Decision string }
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if code != 6 || got.Decision != "fallback" {
		t.Errorf("code %d, decision %q; want 6, fallback", code, got.Decision)
	}
}

// A panic in the in-process check is an unreadable budget, as a crashing subprocess was.
func TestDefaultBudgetPanicIsUnreadable(t *testing.T) {
	budgetEnv(t)
	read := func(context.Context, time.Duration) (usage.CodexState, error) { panic(errors.New("boom")) }
	if code, body := budgetFrom(read)(); code != 1 || body != "" {
		t.Errorf("code %d, body %q; want 1 and empty", code, body)
	}
}
