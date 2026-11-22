package roborev

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/usage"
)

func fallbackBudget() (int, string) { return 6, `{"decision":"fallback","reason":"5h 93%"}` }

func noAlarm() (usage.Alarm, bool) { return usage.Alarm{}, false }

func hitAlarm() (usage.Alarm, bool) { return usage.Alarm{Kind: "venster", Pct: 92}, true }

// withClaude puts a fake claude in the bin directory of the repo.
func withClaude(r *pushGateRepo) {
	r.t.Helper()
	bin := filepath.Dir(mustLookPath(r.t, "roborev"))
	r.writeAbs(filepath.Join(bin, "claude"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		r.t.Fatal(err)
	}
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
	t.Setenv("PATH", bin+string(os.PathListSeparator)+gitDir)
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
