package usage

import (
	"context"
	"sync"
	"time"
)

// StaleAfter is the age beyond which a measurement counts as stale.
const StaleAfter = 900 * time.Second

const (
	codexReuseOK   = 300 * time.Second // how long a successful Codex reading is reused
	codexReuseFail = 60 * time.Second  // how long a failed one is reused
)

// States of a window in GET /api/usage.
const (
	StateOK            = "ok"
	StateStale         = "stale"
	StateNotMeasurable = "not_measurable"
)

// WindowReport is one window of one source.
type WindowReport struct {
	State      string `json:"state" enum:"ok,stale,not_measurable"`
	Pct        *int   `json:"pct"`      // null when not_measurable
	ResetAt    string `json:"reset_at"` // "" without a reset
	AgeS       *int   `json:"age_s"`    // null when not_measurable
	LowerBound bool   `json:"lower_bound"`
	Reason     string `json:"reason,omitempty"`
}

// SourceReport holds the two windows of one source.
type SourceReport struct {
	Session WindowReport `json:"session"`
	Week    WindowReport `json:"week"`
}

// Report is the body of GET /api/usage.
type Report struct {
	Claude SourceReport `json:"claude"`
	Codex  SourceReport `json:"codex"`
}

// Reporter combines the Claude cache and the Codex measurement. It writes nothing.
type Reporter struct {
	Cache     string // Claude cache path; "" counts as no cache
	ReadCodex func(context.Context) (CodexState, error)
	Now       func() time.Time

	mu      sync.Mutex // held during a Codex measurement: one app-server at a time
	have    bool
	codex   CodexState
	codexEr error
	codexAt time.Time // end of the measurement
}

// Report builds the report at Now().
func (r *Reporter) Report(ctx context.Context) Report {
	return Report{Claude: r.claude(), Codex: r.codexReport(ctx)}
}

func (r *Reporter) claude() SourceReport {
	s, at, ok := ReadState(r.Cache)
	if r.Cache == "" || !ok {
		return notMeasurable("no Claude usage cache")
	}
	age := ageSince(r.Now(), at)
	return SourceReport{
		Session: windowReport(s.Session.Pct, s.Session.ResetAt, age, false),
		Week:    windowReport(s.Week.Pct, s.Week.ResetAt, age, false),
	}
}

func (r *Reporter) codexReport(ctx context.Context) SourceReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.have || r.Now().Sub(r.codexAt) > r.reuse() {
		// The measurement outlives a cancelled request, so the remembered
		// reading is never a half-measured one.
		r.codex, r.codexEr = r.ReadCodex(context.WithoutCancel(ctx))
		r.codexAt = r.Now()
		r.have = true
	}
	if r.codexEr != nil {
		return notMeasurable(r.codexEr.Error())
	}
	at, err := time.Parse(timeLayout, r.codex.GeneratedAt)
	if err != nil {
		return notMeasurable("unreadable Codex timestamp: " + err.Error())
	}
	measured := at.Add(-time.Duration(r.codex.AgeS) * time.Second)
	age := ageSince(r.Now(), measured)
	return SourceReport{
		Session: windowReport(r.codex.Session.Pct, r.codex.Session.ResetAt, age, r.codex.LowerBound),
		Week:    windowReport(r.codex.Week.Pct, r.codex.Week.ResetAt, age, r.codex.LowerBound),
	}
}

// reuse is how long the remembered Codex outcome stays valid.
func (r *Reporter) reuse() time.Duration {
	if r.codexEr != nil {
		return codexReuseFail
	}
	return codexReuseOK
}

// ageSince is the age in whole seconds; a measurement from the future (clock
// skew) is 0 seconds old.
func ageSince(now, measured time.Time) int {
	d := now.Sub(measured)
	if d < 0 {
		return 0
	}
	return int(d / time.Second)
}

func windowReport(pct int, resetAt string, age int, lowerBound bool) WindowReport {
	state := StateOK
	if time.Duration(age)*time.Second > StaleAfter {
		state = StateStale
	}
	return WindowReport{State: state, Pct: &pct, ResetAt: resetAt, AgeS: &age, LowerBound: lowerBound}
}

func notMeasurable(reason string) SourceReport {
	w := WindowReport{State: StateNotMeasurable, Reason: reason}
	return SourceReport{Session: w, Week: w}
}
