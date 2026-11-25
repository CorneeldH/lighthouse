package roborev

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

func fallbackBudget() (int, string) { return 6, `{"decision":"fallback","reason":"5h 93%"}` }

func noAlarm() (usage.Alarm, bool) { return usage.Alarm{}, false }

func hitAlarm() (usage.Alarm, bool) { return usage.Alarm{Kind: "venster", Pct: 92}, true }

// withClaude puts a fake claude in the bin directory of the repo.
func withClaude(r *pushGateRepo) {
	r.t.Helper()
	bin := filepath.Dir(mustLookPath(r.t, "roborev"))
	stubbin.Make(r.t, bin, "claude", stubbin.Spec{})
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// strictPath sets PATH to bin plus the directory of git. A claude in that directory skips the test.
func strictPath(t *testing.T, bin string) {
	t.Helper()
	gitDir := filepath.Dir(mustLookPath(t, "git"))
	if _, err := os.Stat(filepath.Join(gitDir, "claude")); err == nil {
		t.Skip("claude lives next to git")
	}
	stubbin.Only(t, bin, "git")
}

func binOf(t *testing.T) string { return filepath.Dir(mustLookPath(t, "roborev")) }

func TestReviewAgentCodex(t *testing.T) {
	newPushGateRepo(t)
	agent, reason := reviewAgent(codexBudget, noAlarm)
	if agent != "codex" || reason != "" {
		t.Fatalf("got (%q, %q)", agent, reason)
	}
}

func TestReviewAgentFallbackIsClaude(t *testing.T) {
	withClaude(newPushGateRepo(t))
	agent, reason := reviewAgent(fallbackBudget, noAlarm)
	if agent != "claude-code" || reason != "" {
		t.Fatalf("got (%q, %q)", agent, reason)
	}
}

func TestReviewAgentFallbackWithoutClaude(t *testing.T) {
	newPushGateRepo(t)
	strictPath(t, binOf(t))
	agent, reason := reviewAgent(fallbackBudget, noAlarm)
	if want := "Codex-budget: fallback - 5h 93%; geen claude op PATH"; agent != "" || reason != want {
		t.Fatalf("got (%q, %q), want reason %q", agent, reason, want)
	}
}

func TestReviewAgentFallbackWithClaudeAlarm(t *testing.T) {
	withClaude(newPushGateRepo(t))
	agent, reason := reviewAgent(fallbackBudget, hitAlarm)
	if want := "Codex-budget: fallback - 5h 93%; Claude-alarm: venster 92%"; agent != "" || reason != want {
		t.Fatalf("got (%q, %q), want reason %q", agent, reason, want)
	}
}

func TestReviewAgentKeepsUnreadableAsSkip(t *testing.T) {
	withClaude(newPushGateRepo(t))
	rows := []struct {
		code int
		out  string
		want string
	}{
		{0, `{"decision":"not_measurable","reason":"x"}`, "Codex-budget: not_measurable - x"},
		{1, "", "Codex-budget: onleesbaar - exit 1"},
		{6, `{"decision":"codex","reason":"x"}`, "Codex-budget: codex - x"},
		{6, `{"reason":"x"}`, "Codex-budget: onleesbaar - exit 6"},
	}
	for _, c := range rows {
		budget := func() (int, string) { return c.code, c.out }
		t.Run(c.want, func(t *testing.T) {
			if agent, reason := reviewAgent(budget, noAlarm); agent != "" || reason != c.want {
				t.Fatalf("no alarm: got (%q, %q), want reason %q", agent, reason, c.want)
			}
			want := c.want + "; Claude-alarm: venster 92%"
			if agent, reason := reviewAgent(budget, hitAlarm); agent != "" || reason != want {
				t.Fatalf("alarm: got (%q, %q), want reason %q", agent, reason, want)
			}
		})
	}
}

func TestGatePushFallbackRunsClaude(t *testing.T) {
	r := newPushGateRepo(t)
	withClaude(r)
	out, err := GatePush(r.root, fallbackBudget, noAlarm)
	if err != nil {
		t.Fatal(err)
	}
	if v := gateField(t, out, "verdict"); v != "run" {
		t.Fatalf("verdict %v", v)
	}
	if a := gateField(t, out, "agent"); a != "claude-code" {
		t.Fatalf("agent %v", a)
	}
	if entries, _ := filepath.Glob(filepath.Join(r.stateDir(), "*.json")); len(entries) != 0 {
		t.Fatalf("state files written: %v", entries)
	}
}

func TestGatePushCodexRunCarriesAgent(t *testing.T) {
	r := newPushGateRepo(t)
	out, err := GatePush(r.root, codexBudget, noAlarm)
	if err != nil {
		t.Fatal(err)
	}
	if a := gateField(t, out, "agent"); a != "codex" {
		t.Fatalf("agent %v", a)
	}
	var keys []string
	for _, k := range []string{"verdict", "range", "lines", "agent", "state", "raw"} {
		gateField(t, out, k)
		keys = append(keys, k)
	}
	if got := fmt.Sprint(out.Keys()); got != fmt.Sprint(keys) {
		t.Fatalf("key order %s, want %v", got, keys)
	}
}

func TestGateFallbackWithoutCodexStaysSkip(t *testing.T) {
	r := newPushGateRepo(t)
	bin := binOf(t)
	if err := os.Remove(filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	withClaude(r)
	strictPath(t, bin)
	out, err := GatePush(r.root, fallbackBudget, noAlarm)
	if err != nil {
		t.Fatal(err)
	}
	if v := gateField(t, out, "verdict"); v != "skip" {
		t.Fatalf("verdict %v", v)
	}
	if got := gateField(t, out, "reason"); got != "geen roborev of codex op PATH" {
		t.Fatalf("reason %v", got)
	}
}

const agentLogLine = "2026/11/22 14:31:33 ci review: ref=a..b agents=[claude-code] types=[default] reasoning=thorough\n"

func agentRaw(sha string, logged bool) string {
	raw := "## roborev: Review Passed (`" + sha + "`)\n\nNo issues found.\n"
	if logged {
		raw = agentLogLine + raw
	}
	return raw
}

// recordPush records the review of HEAD the way the restart path does and returns head.
func recordPush(t *testing.T, r *pushGateRepo, logged bool) string {
	t.Helper()
	head := r.git(r.root, "rev-parse", "HEAD")
	rawPath := filepath.Join(r.stateDir(), "raw.md")
	r.writeAbs(rawPath, agentRaw(head, logged))
	state, _ := pushPathsOf(t, r, head)
	rng := "aaaaaaa..bbbbbbb"
	if _, err := Record(r.root, RecordOptions{Sha: &head, Decisions: []any{}, Out: state, Raw: rawPath, Range: &rng}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	return head
}

func pushPathsOf(t *testing.T, r *pushGateRepo, head string) (string, string) {
	t.Helper()
	state, raw, err := PushPaths(r.root, head)
	if err != nil {
		t.Fatal(err)
	}
	return state, raw
}

func TestRecordTakesAgentFromRaw(t *testing.T) {
	r := newPushGateRepo(t)
	head := recordPush(t, r, true)
	state, _ := pushPathsOf(t, r, head)
	text, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), `"agent": "claude-code"`) {
		t.Errorf("record lacks the agent: %s", text)
	}
}

func TestBodyLinePushNamesAgent(t *testing.T) {
	r := newPushGateRepo(t)
	head := recordPush(t, r, true)
	got, err := BodyLinePush(r.root)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<!-- roborev-lokaal: " + head + " agent: claude-code -->"; got != want {
		t.Errorf("BodyLinePush = %q, want %q", got, want)
	}
}

func TestBodyLineWithoutAgentKeepsOldMarker(t *testing.T) {
	t.Run("BodyLinePush", func(t *testing.T) {
		r := newPushGateRepo(t)
		head := recordPush(t, r, false)
		got, err := BodyLinePush(r.root)
		if err != nil {
			t.Fatal(err)
		}
		if want := "<!-- roborev-lokaal: " + head + " -->"; got != want {
			t.Errorf("BodyLinePush = %q, want %q", got, want)
		}
	})
	t.Run("covering record", func(t *testing.T) {
		r := newPushGateRepo(t)
		old := r.git(r.root, "rev-parse", "HEAD")
		r.commitLines("fix2.go", 2)
		fix := r.git(r.root, "rev-parse", "HEAD")
		r.writeAbs(filepath.Join(r.stateDir(), "push-"+old+".json"), doneRecord(old, fix[:10]))
		got, err := BodyLinePush(r.root)
		if err != nil {
			t.Fatal(err)
		}
		if want := "<!-- roborev-lokaal: " + old + " -->"; got != want {
			t.Errorf("BodyLinePush = %q, want %q", got, want)
		}
	})
	t.Run("BodyLine", func(t *testing.T) {
		r := newPushGateRepo(t)
		head := r.git(r.root, "rev-parse", "HEAD")
		state, _, err := StatePaths(r.root)
		if err != nil {
			t.Fatal(err)
		}
		r.writeAbs(state, doneRecord(head))
		got, err := BodyLine(r.root)
		if err != nil {
			t.Fatal(err)
		}
		if want := "<!-- roborev-lokaal: " + head + " -->"; got != want {
			t.Errorf("BodyLine = %q, want %q", got, want)
		}
	})
}

func TestBodyLinePushCoveringNamesAgent(t *testing.T) {
	r := newPushGateRepo(t)
	old := r.git(r.root, "rev-parse", "HEAD")
	r.commitLines("fix2.go", 2)
	fix := r.git(r.root, "rev-parse", "HEAD")
	rec := strings.Replace(doneRecord(old, fix[:10]), `"uitkomst": "done"`, `"uitkomst": "done", "agent": "claude-code"`, 1)
	r.writeAbs(filepath.Join(r.stateDir(), "push-"+old+".json"), rec)
	got, err := BodyLinePush(r.root)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<!-- roborev-lokaal: " + old + " agent: claude-code -->"; got != want {
		t.Errorf("BodyLinePush = %q, want %q", got, want)
	}
}
