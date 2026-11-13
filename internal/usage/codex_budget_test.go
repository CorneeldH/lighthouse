package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ports of tests/pack/test_codex_usage.py (decide_budget, parse_app_server, read_usage,
// cmd_budget). Where a pytest case is already covered by codex_test.go or
// codex_rollout_test.go, the existing Go test says so.

var budgetNow = time.Date(2026, 9, 28, 19, 25, 46, 0, time.UTC) // NOW_SPEC

const (
	resetS = "2026-09-28T23:00:00Z" // after budgetNow
	resetW = "2026-10-02T08:00:00Z"
)

// budgetReading is the pytest helper `reading`: a reading whose resets lie after budgetNow.
func budgetReading(session, week int, lowerBound bool, resetSession, resetWeek string) *CodexState {
	source := "app-server"
	if lowerBound {
		source = "rollout-log"
	}
	return &CodexState{
		Source:      source,
		GeneratedAt: "2026-09-28T19:25:46Z",
		LowerBound:  lowerBound,
		Session:     CodexWindow{Pct: session, ResetAt: resetSession},
		Week:        CodexWindow{Pct: week, ResetAt: resetWeek},
	}
}

func rd(session, week int) *CodexState { return budgetReading(session, week, false, resetS, resetW) }

func decide(r *CodexState) BudgetOutcome { return DecideBudget(r, 90, 95, budgetNow) }

func str(s string) *string { return &s }

// pytest: test_budget_thresholds
func TestBudgetThresholds(t *testing.T) {
	cases := []struct {
		session, week int
		decision      string
		window        *string
		resetAt       *string
	}{
		{10, 10, "codex", nil, nil},
		{89, 94, "codex", nil, nil},
		{90, 10, "fallback", str("codex-5h"), str(resetS)},
		{10, 95, "fallback", str("codex-week"), str(resetW)},
		{99, 99, "fallback", str("codex-week"), str(resetW)}, // week before 5h
	}
	for _, tc := range cases {
		got := decide(rd(tc.session, tc.week))
		want := BudgetOutcome{
			Decision: tc.decision, Reason: got.Reason, Window: tc.window, ResetAt: tc.resetAt,
			SessionPct: &tc.session, WeekPct: &tc.week, Source: str("app-server"),
			LowerBound: new(bool), Thresholds: &BudgetThresholds{Session: 90, Week: 95},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%d/%d: got %+v, want %+v", tc.session, tc.week, got, want)
		}
	}
}

// pytest: test_budget_reason_fallback, test_budget_reason_codex
func TestBudgetReason(t *testing.T) {
	for _, tc := range []struct {
		r    *CodexState
		want string
	}{
		{rd(92, 41), "5h 92% ≥ 90%"},
		{rd(10, 96), "week 96% ≥ 95%"},
		{rd(10, 20), "ok: 5h 10%, week 20%"},
	} {
		if got := decide(tc.r).Reason; got != tc.want {
			t.Errorf("reason = %q, want %q", got, tc.want)
		}
	}
}

// pytest: test_budget_none_is_not_measurable
func TestBudgetNilIsNotMeasurable(t *testing.T) {
	want := BudgetOutcome{Decision: "not_measurable", Reason: "not measurable", Thresholds: &BudgetThresholds{Session: 90, Week: 95}}
	if got := decide(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// pytest: test_budget_log_reading_above_threshold_counts
func TestBudgetLogReadingAboveThresholdCounts(t *testing.T) {
	if got := decide(budgetReading(92, 10, true, resetS, resetW)).Decision; got != "fallback" {
		t.Errorf("decision = %q, want fallback", got)
	}
}

// pytest: test_budget_log_reading_below_thresholds_not_measurable
func TestBudgetLogReadingBelowThresholdsNotMeasurable(t *testing.T) {
	got := decide(budgetReading(10, 10, true, resetS, resetW))
	if got.Decision != "not_measurable" || got.Window != nil || got.ResetAt != nil ||
		got.Reason != "not measurable: log reading below the thresholds is no proof" {
		t.Errorf("got %+v", got)
	}
}

// pytest: test_budget_log_reading_expired_not_measurable
func TestBudgetLogReadingExpiredNotMeasurable(t *testing.T) {
	past := "2026-09-28T19:00:00Z"
	for name, r := range map[string]*CodexState{
		"session": budgetReading(92, 10, true, past, resetW),
		"week":    budgetReading(92, 10, true, resetS, past),
	} {
		got := decide(r)
		if got.Decision != "not_measurable" || got.Reason != "not measurable: log reading expired" {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}

// pytest: test_budget_app_server_window_still_rolling
func TestBudgetAppServerWindowStillRolling(t *testing.T) {
	for _, stamp := range []string{"2026-09-28T19:00:00Z", "2026-09-28T19:25:46Z"} { // past, exactly now
		for name, r := range map[string]*CodexState{
			"session": budgetReading(92, 10, false, stamp, resetW),
			"week":    budgetReading(10, 96, false, resetS, stamp),
		} {
			got := decide(r)
			want := "not measurable: window still rolling (reset " + stamp + " has passed)"
			if got.Decision != "not_measurable" || got.Window != nil || got.ResetAt != nil || got.Reason != want {
				t.Errorf("%s %s: got %+v, want reason %q", name, stamp, got, want)
			}
		}
	}
}

// Mutation guard for the order: a week hit that is still rolling must not be hidden by a 5h
// hit that is not (the week is judged first, and its stale reset makes the reading unusable).
func TestBudgetWeekIsJudgedBeforeSession(t *testing.T) {
	got := decide(budgetReading(99, 99, false, resetS, "2026-09-28T19:00:00Z"))
	if got.Decision != "not_measurable" || !strings.Contains(got.Reason, "window still rolling") {
		t.Errorf("got %+v, want not_measurable", got)
	}
}

// pytest: NINE_KEYS (the JSON is a contract: the review skills read these keys through lighthouse review-run).
func TestBudgetOutcomeJSONKeyOrder(t *testing.T) {
	data, err := json.Marshal(decide(rd(92, 41)))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"decision":"fallback","reason":"5h 92% ≥ 90%","window":"codex-5h","reset_at":"2026-09-28T23:00:00Z",` +
		`"session_pct":92,"week_pct":41,"source":"app-server","lower_bound":false,"thresholds":{"session":90,"week":95}}`
	if string(data) != want {
		t.Errorf("json = %s\nwant   %s", data, want)
	}
	data, err = json.Marshal(decide(nil))
	if err != nil {
		t.Fatal(err)
	}
	want = `{"decision":"not_measurable","reason":"not measurable","window":null,"reset_at":null,"session_pct":null,` +
		`"week_pct":null,"source":null,"lower_bound":null,"thresholds":{"session":90,"week":95}}`
	if string(data) != want {
		t.Errorf("json = %s\nwant   %s", data, want)
	}
}

func fixtureResult(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	return []byte(fakeFixture(t, mutate))
}

// pytest: test_output_form (and the key order of the reading JSON)
func TestParseAppServerOutputForm(t *testing.T) {
	got, creditsErr, err := parseAppServer(fixtureResult(t, nil), budgetNow)
	if err != nil || creditsErr != nil {
		t.Fatal(err, creditsErr)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"source":"app-server","generated_at":"2026-09-28T19:25:46Z","age_s":0,"source_file":null,"lower_bound":false,` +
		`"session":{"pct":55,"reset_at":"2026-09-28T23:23:29Z"},"week":{"pct":66,"reset_at":"2026-10-03T17:17:59Z"},` +
		`"reset_credits":{"available":2,"expires":["2026-10-04T23:53:00Z","2026-10-22T20:18:18Z"]}}`
	if string(data) != want {
		t.Errorf("json = %s\nwant   %s", data, want)
	}
}

// pytest: test_no_account_id_in_output
func TestParseAppServerNoAccountIDInOutput(t *testing.T) {
	res := fixtureResult(t, func(r map[string]any) { r["accountId"] = "acc-geheim" })
	got, _, err := parseAppServer(res, budgetNow)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "acc-geheim") {
		t.Errorf("account id in output: %s", data)
	}
}

// pytest: test_missing_window_not_measurable (TestCodexAppServerRejects/nowindow covers secondary only)
func TestParseAppServerMissingWindowNotMeasurable(t *testing.T) {
	for _, window := range []string{"primary", "secondary"} {
		res := fixtureResult(t, func(r map[string]any) {
			everyLimit(r, func(l map[string]any) { l[window] = nil })
		})
		if _, _, err := parseAppServer(res, budgetNow); err == nil || !strings.Contains(err.Error(), "window "+window+" missing") {
			t.Errorf("%s: err = %v", window, err)
		}
	}
}

// pytest: test_without_reset_credits_is_none
func TestParseAppServerWithoutResetCreditsIsNull(t *testing.T) {
	res := fixtureResult(t, func(r map[string]any) { delete(r, "rateLimitResetCredits") })
	got, creditsErr, err := parseAppServer(res, budgetNow)
	if err != nil || creditsErr != nil {
		t.Fatal(err, creditsErr)
	}
	data, _ := json.Marshal(got)
	if !strings.Contains(string(data), `"reset_credits":null`) {
		t.Errorf("json = %s, want reset_credits null", data)
	}
}

// pytest: test_only_available_credits_count_for_expires
func TestParseAppServerOnlyAvailableCreditsCountForExpires(t *testing.T) {
	res := fixtureResult(t, func(r map[string]any) {
		credits := r["rateLimitResetCredits"].(map[string]any)["credits"].([]any)
		credits[0].(map[string]any)["status"] = "used"
	})
	got, _, err := parseAppServer(res, budgetNow)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"available":2,"expires":["2026-10-22T20:18:18Z"]}`
	if string(got.ResetCredits) != want {
		t.Errorf("reset_credits = %s, want %s", got.ResetCredits, want)
	}
}

// pytest: _reset_credits raises NotMeasurable("reset credits unreadable"); here the shared
// parser returns it apart from a valid reading, so `usage codex` can ignore it.
func TestParseAppServerUnreadableResetCreditsIsReturnedApart(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"no availableCount": func(r map[string]any) { delete(r["rateLimitResetCredits"].(map[string]any), "availableCount") },
		"credits is null":   func(r map[string]any) { r["rateLimitResetCredits"].(map[string]any)["credits"] = nil },
		"bad expiresAt": func(r map[string]any) {
			r["rateLimitResetCredits"].(map[string]any)["credits"].([]any)[0].(map[string]any)["expiresAt"] = "morgen"
		},
	} {
		got, creditsErr, err := parseAppServer(fixtureResult(t, mutate), budgetNow)
		if err != nil {
			t.Fatalf("%s: err = %v, want a valid reading", name, err)
		}
		if creditsErr == nil || !strings.HasPrefix(creditsErr.Error(), "reset credits unreadable: ") {
			t.Errorf("%s: creditsErr = %v", name, creditsErr)
		}
		if got.Session.Pct != 55 || len(got.ResetCredits) != 0 {
			t.Errorf("%s: got %+v, want a reading with null reset_credits", name, got)
		}
	}
}

// usage codex and serve: an unreadable rateLimitResetCredits does not fail the home.
func TestCodexReadIgnoresUnreadableResetCredits(t *testing.T) {
	r, _ := fakeReader(t, "badcredits", "h")
	got, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if got.Source != "app-server" || got.Session.Pct != 55 || !strings.Contains(string(data), `"reset_credits":null`) {
		t.Errorf("got %s", data)
	}
}

// codex-usage read and budget (pytest: _reset_credits raises NotMeasurable inside the home
// loop of read_usage): the unreadable credits fail that home, the next home delivers.
func TestCodexReadStrictFailsThatHomeOnly(t *testing.T) {
	r, logPath := fakeReaderModes(t, map[string]string{"a": "badcredits", "b": "ok"}, "a", "b")
	got, err := r.ReadStrict(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantFixtureState()) {
		t.Errorf("got %+v, want %+v", got, wantFixtureState())
	}
	raw, _ := os.ReadFile(logPath)
	if !strings.Contains(string(raw), "home=a") || !strings.Contains(string(raw), "home=b") {
		t.Errorf("log = %q, want both homes tried", raw)
	}
}

// pytest: test_all_fail_reason_names_both_sources, for the credits reason.
func TestCodexReadStrictAllHomesUnreadableCredits(t *testing.T) {
	r, _ := fakeReader(t, "badcredits", "a", "b")
	_, err := r.ReadStrict(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"app-server: a: reset credits unreadable: ", "; b: reset credits unreadable: ", "; rollout-log: no usable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// pytest: test_both_homes_fail_fall_back_to_log, with the credits reason as the failure.
func TestCodexReadStrictFallsBackToRollout(t *testing.T) {
	home := t.TempDir()
	path := writeRollout(t, home, "rollout-x.jsonl", rolloutFixture(t))
	r, _ := fakeReader(t, "badcredits", home)
	got, err := r.ReadStrict(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantRolloutState(path)) {
		t.Errorf("got %+v, want %+v", got, wantRolloutState(path))
	}
}

func writeConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(path, []byte("[project]\nlanguage = \"en\"\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
}

// readOf returns a read function for ReadBudget that records the timeout it was given.
func readOf(state CodexState, err error, timeout *time.Duration) func(context.Context, time.Duration) (CodexState, error) {
	return func(_ context.Context, d time.Duration) (CodexState, error) {
		if timeout != nil {
			*timeout = d
		}
		return state, err
	}
}

// A reset far in the future, because ReadBudget decides at the real time.
const (
	farS = "2099-01-01T05:00:00Z"
	farW = "2099-01-05T00:00:00Z"
)

func farState(session, week int) CodexState {
	r := budgetReading(session, week, false, farS, farW)
	return *r
}

// pytest: TestBudgetCli.test_exit_and_nine_keys_without_marker
func TestReadBudgetExitCodeAndKeys(t *testing.T) {
	writeConfig(t, "")
	for _, tc := range []struct {
		state    CodexState
		err      error
		code     int
		decision string
	}{
		{farState(92, 41), nil, 6, "fallback"},
		{farState(10, 10), nil, 0, "codex"},
		{CodexState{}, errors.New("gone"), 0, "not_measurable"},
	} {
		out, code, stderr := ReadBudget(context.Background(), readOf(tc.state, tc.err, nil))
		if code != tc.code || out.Decision != tc.decision || stderr != "" {
			t.Errorf("%s: code = %d, outcome = %+v, stderr = %q", tc.decision, code, out, stderr)
		}
	}
}

// pytest: test_fallback_carries_window_and_thresholds
func TestReadBudgetFallbackCarriesWindowAndThresholds(t *testing.T) {
	writeConfig(t, "")
	out, code, _ := ReadBudget(context.Background(), readOf(farState(92, 41), nil, nil))
	want := BudgetOutcome{
		Decision: "fallback", Reason: "5h 92% ≥ 90%", Window: str("codex-5h"), ResetAt: str(farS),
		SessionPct: pctp(92), WeekPct: pctp(41), Source: str("app-server"), LowerBound: new(bool),
		Thresholds: &BudgetThresholds{Session: 90, Week: 95},
	}
	if code != 6 || !reflect.DeepEqual(out, want) {
		t.Errorf("code = %d, got %+v, want %+v", code, out, want)
	}
}

func pctp(n int) *int { return &n }

// pytest: test_not_measurable_names_the_reason
func TestReadBudgetNotMeasurableNamesTheReason(t *testing.T) {
	writeConfig(t, "")
	out, code, _ := ReadBudget(context.Background(), readOf(CodexState{}, errors.New("gone"), nil))
	if code != 0 || out.Reason != "not measurable: gone" || out.Thresholds == nil || *out.Thresholds != (BudgetThresholds{Session: 90, Week: 95}) {
		t.Errorf("code = %d, got %+v", code, out)
	}
}

// pytest: test_uses_the_configured_thresholds, test_read_passes_configured_timeout
func TestReadBudgetUsesTheConfiguredValues(t *testing.T) {
	writeConfig(t, "[codex_usage]\nreview_pct_session = 50\nreview_pct_week = 60\napp_server_timeout_seconds = 7\n")
	var timeout time.Duration
	out, code, _ := ReadBudget(context.Background(), readOf(farState(55, 10), nil, &timeout))
	if code != 6 || out.Reason != "5h 55% ≥ 50%" || *out.Thresholds != (BudgetThresholds{Session: 50, Week: 60}) {
		t.Errorf("code = %d, got %+v", code, out)
	}
	if timeout != 7*time.Second {
		t.Errorf("timeout = %v, want 7s", timeout)
	}
}

// pytest: test_broken_config_is_said_on_stderr
func TestReadBudgetBrokenConfigIsSaidOnStderr(t *testing.T) {
	writeConfig(t, "[codex_usage]\nreview_pct_week = \"x\"\n")
	called := false
	read := func(context.Context, time.Duration) (CodexState, error) { called = true; return CodexState{}, nil }
	out, code, stderr := ReadBudget(context.Background(), read)
	if code != 0 || out.Decision != "not_measurable" || !strings.HasPrefix(stderr, "codex-usage: config: ") || !strings.HasSuffix(stderr, "\n") {
		t.Errorf("code = %d, outcome = %+v, stderr = %q", code, out, stderr)
	}
	if called {
		t.Error("read was called with a broken config")
	}
}

// pytest: test_config_unreadable
func TestReadBudgetConfigUnreadable(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone.toml")
	t.Setenv("LIGHTHOUSE_TOML", gone)
	out, code, stderr := ReadBudget(context.Background(), readOf(CodexState{}, nil, nil))
	data, _ := json.Marshal(out)
	if code != 0 || !bytes.Contains(data, []byte(`"thresholds":null`)) || out.Decision != "not_measurable" ||
		!strings.HasPrefix(out.Reason, "not measurable: config: ") || !strings.Contains(out.Reason, gone) ||
		stderr != "codex-usage: "+strings.TrimPrefix(out.Reason, "not measurable: ")+"\n" {
		t.Errorf("code = %d, outcome = %+v, stderr = %q", code, out, stderr)
	}
}
