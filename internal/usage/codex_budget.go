package usage

import (
	"context"
	"fmt"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// BudgetThresholds are the review thresholds in percent, as `budget` printed them.
type BudgetThresholds struct {
	Session int `json:"session"`
	Week    int `json:"week"`
}

// BudgetOutcome is the decision of `codex-usage budget`. The field order is the key order of
// the JSON, and nil is null. Window and ResetAt are only filled for "fallback".
type BudgetOutcome struct {
	Decision   string            `json:"decision"` // "codex", "fallback" or "not_measurable"
	Reason     string            `json:"reason"`
	Window     *string           `json:"window"`
	ResetAt    *string           `json:"reset_at"`
	SessionPct *int              `json:"session_pct"`
	WeekPct    *int              `json:"week_pct"`
	Source     *string           `json:"source"`
	LowerBound *bool             `json:"lower_bound"`
	Thresholds *BudgetThresholds `json:"thresholds"`
}

// DecideBudget decides before a review round whether Codex still fits. First hit wins.
//
// The week goes before the 5-hour window. A rollout-log reading is a lower bound: above a
// threshold it counts, below it proves nothing, and an expired reset makes it unusable. A
// reset_at in the past in the window that hits its threshold is an outdated reading: not
// measurable, no fallback. A nil reading is not measurable.
func DecideBudget(r *CodexState, review5h, reviewWeek int, now time.Time) BudgetOutcome {
	thresholds := &BudgetThresholds{Session: review5h, Week: reviewWeek}
	if r == nil {
		return BudgetOutcome{Decision: "not_measurable", Reason: "not measurable", Thresholds: thresholds}
	}
	outcome := func(decision, reason string) BudgetOutcome {
		session, week, source, lowerBound := r.Session.Pct, r.Week.Pct, r.Source, r.LowerBound
		return BudgetOutcome{
			Decision: decision, Reason: reason,
			SessionPct: &session, WeekPct: &week, Source: &source, LowerBound: &lowerBound,
			Thresholds: thresholds,
		}
	}
	if r.LowerBound && (passed(r.Session.ResetAt, now) || passed(r.Week.ResetAt, now)) {
		return outcome("not_measurable", "not measurable: log reading expired")
	}
	for _, w := range []struct {
		window    CodexWindow
		name      string
		label     string
		threshold int
	}{
		{r.Week, "codex-week", "week", reviewWeek},
		{r.Session, "codex-5h", "5h", review5h},
	} {
		if w.window.Pct < w.threshold {
			continue
		}
		if passed(w.window.ResetAt, now) {
			return outcome("not_measurable",
				fmt.Sprintf("not measurable: window still rolling (reset %s has passed)", w.window.ResetAt))
		}
		out := outcome("fallback", fmt.Sprintf("%s %d%% ≥ %d%%", w.label, w.window.Pct, w.threshold))
		out.Window, out.ResetAt = &w.name, &w.window.ResetAt
		return out
	}
	if r.LowerBound {
		return outcome("not_measurable", "not measurable: log reading below the thresholds is no proof")
	}
	return outcome("codex", fmt.Sprintf("ok: 5h %d%%, week %d%%", r.Session.Pct, r.Week.Pct))
}

// passed reports whether the reset time has come. An unreadable time counts as passed: the
// reading is then not usable.
func passed(resetAt string, now time.Time) bool {
	t, err := time.Parse(timeLayout, resetAt)
	return err != nil || !t.After(now)
}

// BudgetConfig holds the [codex_usage] keys that `codex-usage` uses.
type BudgetConfig struct {
	Timeout       time.Duration // app_server_timeout_seconds, per home
	ReviewSession int           // review_pct_session
	ReviewWeek    int           // review_pct_week
}

// LoadBudgetConfig reads [codex_usage] from lighthouse.toml, with the defaults of the config
// reader. The error says what is wrong with the file or a key.
func LoadBudgetConfig() (BudgetConfig, error) {
	path, err := config.Path()
	if err != nil {
		return BudgetConfig{}, err
	}
	c, err := config.Load(path)
	if err != nil {
		return BudgetConfig{}, err
	}
	var n [3]int
	for i, name := range []string{"app_server_timeout_seconds", "review_pct_session", "review_pct_week"} {
		v, err := config.Lookup(c, "codex_usage."+name)
		if err != nil {
			return BudgetConfig{}, err
		}
		got, ok := v.(int64)
		if !ok {
			return BudgetConfig{}, fmt.Errorf("codex_usage.%s: not an integer", name)
		}
		n[i] = int(got)
	}
	return BudgetConfig{Timeout: time.Duration(n[0]) * time.Second, ReviewSession: n[1], ReviewWeek: n[2]}, nil
}

// ReadBudget reads the config and the reading, and decides. read gets the time per home. The
// exit code is 6 for "fallback", else 0; stderr is a line for the caller to print ("" for none).
// A broken config gives not_measurable, a stderr line and no read.
func ReadBudget(ctx context.Context, read func(context.Context, time.Duration) (CodexState, error)) (outcome BudgetOutcome, code int, stderr string) {
	cfg, err := LoadBudgetConfig()
	if err != nil {
		// The 0/6 contract stays, but a broken config must not be only in the JSON.
		return BudgetOutcome{Decision: "not_measurable", Reason: fmt.Sprintf("not measurable: config: %v", err)},
			0, fmt.Sprintf("codex-usage: config: %v\n", err)
	}
	now := time.Now()
	var reading *CodexState
	reason := ""
	if state, err := read(ctx, cfg.Timeout); err != nil {
		reason = "not measurable: " + err.Error()
	} else {
		reading = &state
	}
	outcome = DecideBudget(reading, cfg.ReviewSession, cfg.ReviewWeek, now)
	if reason != "" {
		outcome.Reason = reason
	}
	if outcome.Decision == "fallback" {
		code = 6
	}
	return outcome, code, ""
}
