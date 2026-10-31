package kennforge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestUnitName(t *testing.T) {
	cases := []struct{ profile, path, want string }{
		{"software", "/w/issue-7-x", "paia-forge-software-issue-7-x"},
		{"software", "/w/a.b", "paia-forge-software-a-b"},
		{"infra", "/w/é", "paia-forge-infra---"},
		{"software", "/w/a_b-C9", "paia-forge-software-a_b-C9"},
	}
	for _, c := range cases {
		if got := UnitName(c.profile, c.path); got != c.want {
			t.Errorf("UnitName(%q, %q) = %q, want %q", c.profile, c.path, got, c.want)
		}
	}
}

// step is one scripted subprocess call: the exact command expected and its answer.
type step struct {
	cmd  string
	out  string
	code int
	err  error
}

// scriptRunner replays steps in order and fails the test on any other call.
func scriptRunner(t *testing.T, steps []step, calls *[]string) func(context.Context, time.Duration, string, ...string) (string, int, error) {
	t.Helper()
	return func(_ context.Context, _ time.Duration, name string, args ...string) (string, int, error) {
		got := strings.Join(append([]string{name}, args...), " ")
		n := len(*calls)
		*calls = append(*calls, got)
		if n >= len(steps) {
			t.Errorf("unexpected extra call %q", got)
			return "", 0, fmt.Errorf("unexpected call")
		}
		if steps[n].cmd != got {
			t.Errorf("call %d = %q, want %q", n, got, steps[n].cmd)
		}
		return steps[n].out, steps[n].code, steps[n].err
	}
}

func stopAdapter(t *testing.T, profile string, steps []step) (*Adapter, *fakeForge, *[]string) {
	t.Helper()
	f, a := newFakeForge(t)
	f.path = "/w/issue-7-x"
	a.Profile = profile
	a.MainCheckout = "/repo/main"
	calls := &[]string{}
	a.Run = scriptRunner(t, steps, calls)
	return a, f, calls
}

const unit = "paia-forge-software-issue-7-x"

func TestStopUnitInactive(t *testing.T) {
	a, _, calls := stopAdapter(t, "infra", []step{
		{cmd: "systemctl --user stop paia-forge-infra-issue-7-x", code: 5},
		{cmd: "systemctl --user is-active paia-forge-infra-issue-7-x", out: "inactive\n", code: 3},
	})
	if err := a.Stop(context.Background(), "ws1", []string{testSessionID}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Errorf("calls = %v, want only stop and is-active", *calls)
	}
}

func TestStopUnitFailedCountsAsGone(t *testing.T) {
	a, _, _ := stopAdapter(t, "infra", []step{
		{cmd: "systemctl --user stop paia-forge-infra-issue-7-x"},
		{cmd: "systemctl --user is-active paia-forge-infra-issue-7-x", out: "failed\n", code: 3},
	})
	if err := a.Stop(context.Background(), "ws1", nil); err != nil {
		t.Fatal(err)
	}
}

func TestStopUnitStillActive(t *testing.T) {
	a, _, _ := stopAdapter(t, "infra", []step{
		{cmd: "systemctl --user stop paia-forge-infra-issue-7-x"},
		{cmd: "systemctl --user is-active paia-forge-infra-issue-7-x", out: "active\n"},
	})
	err := a.Stop(context.Background(), "ws1", nil)
	if err == nil || !strings.Contains(err.Error(), "paia-forge-infra-issue-7-x") {
		t.Errorf("err = %v, want one naming the unit", err)
	}
}

func TestStopIsActiveUnmeasurable(t *testing.T) {
	a, _, _ := stopAdapter(t, "infra", []step{
		{cmd: "systemctl --user stop paia-forge-infra-issue-7-x"},
		{cmd: "systemctl --user is-active paia-forge-infra-issue-7-x", code: 4},
	})
	if err := a.Stop(context.Background(), "ws1", nil); err == nil {
		t.Error("err = nil, want an error for an unmeasurable is-active")
	}
}

func softwareHead() []step {
	return []step{
		{cmd: "systemctl --user stop " + unit},
		{cmd: "systemctl --user is-active " + unit, out: "inactive\n", code: 3},
		{cmd: "podman ps --filter label=devcontainer.local_folder=/repo/main --format {{.ID}}", out: "c1\n"},
	}
}

func TestStopSoftwarePkill(t *testing.T) {
	steps := append(softwareHead(),
		step{cmd: "podman exec c1 pgrep -f " + testSessionID, out: "42\n"},
		step{cmd: "podman exec c1 pkill -f " + testSessionID},
		step{cmd: "podman exec c1 pgrep -f " + testSessionID, code: 1},
	)
	a, _, calls := stopAdapter(t, "software", steps)
	if err := a.Stop(context.Background(), "ws1", []string{testSessionID}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != len(steps) {
		t.Errorf("calls = %d, want %d", len(*calls), len(steps))
	}
}

func TestStopSoftwareNoSessionsSkipsPodman(t *testing.T) {
	a, _, calls := stopAdapter(t, "software", softwareHead()[:2])
	if err := a.Stop(context.Background(), "ws1", nil); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Errorf("calls = %v", *calls)
	}
}

func TestStopSoftwareSurvivesPkill(t *testing.T) {
	steps := append(softwareHead(),
		step{cmd: "podman exec c1 pgrep -f " + testSessionID},
		step{cmd: "podman exec c1 pkill -f " + testSessionID},
	)
	for i := 0; i < 5; i++ {
		steps = append(steps, step{cmd: "podman exec c1 pgrep -f " + testSessionID})
	}
	a, f, _ := stopAdapter(t, "software", steps)
	if err := a.Stop(context.Background(), "ws1", []string{testSessionID}); err == nil {
		t.Error("err = nil, want an error for a process that survives pkill")
	}
	if n := f.count("sleep"); n != 5 {
		t.Errorf("sleeps = %d, want 5", n)
	}
}

func TestStopPgrepUnmeasurable(t *testing.T) {
	steps := append(softwareHead(), step{cmd: "podman exec c1 pgrep -f " + testSessionID, code: 2})
	a, _, calls := stopAdapter(t, "software", steps)
	if err := a.Stop(context.Background(), "ws1", []string{testSessionID}); err == nil {
		t.Error("err = nil, want an error for pgrep rc 2")
	}
	for _, c := range *calls {
		if strings.Contains(c, "pkill") {
			t.Errorf("pkill ran after an unmeasurable pgrep: %v", *calls)
		}
	}
}

func TestStopPodmanPsFails(t *testing.T) {
	steps := append(softwareHead()[:2], step{cmd: "podman ps --filter label=devcontainer.local_folder=/repo/main --format {{.ID}}", code: 125})
	a, _, _ := stopAdapter(t, "software", steps)
	if err := a.Stop(context.Background(), "ws1", []string{testSessionID}); err == nil {
		t.Error("err = nil, want an error for podman ps rc 125")
	}
}

func TestStopEmptySessionIDRejectedBeforeAnyCall(t *testing.T) {
	a, _, calls := stopAdapter(t, "software", nil)
	err := a.Stop(context.Background(), "ws1", []string{testSessionID, ""})
	if err == nil {
		t.Fatal("err = nil, want an error for an empty session id")
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none: an empty id would make pgrep and pkill match every process", *calls)
	}
}

func TestStopIsActiveRunnerError(t *testing.T) {
	a, _, _ := stopAdapter(t, "infra", []step{
		{cmd: "systemctl --user stop paia-forge-infra-issue-7-x"},
		{cmd: "systemctl --user is-active paia-forge-infra-issue-7-x", code: -1, err: context.DeadlineExceeded},
	})
	if err := a.Stop(context.Background(), "ws1", nil); err == nil {
		t.Error("err = nil, want an error for an is-active timeout")
	}
}

func TestStopPgrepUnmeasurableInRetryLoop(t *testing.T) {
	steps := append(softwareHead(),
		step{cmd: "podman exec c1 pgrep -f " + testSessionID},
		step{cmd: "podman exec c1 pkill -f " + testSessionID},
		step{cmd: "podman exec c1 pgrep -f " + testSessionID, code: 2},
	)
	a, _, calls := stopAdapter(t, "software", steps)
	if err := a.Stop(context.Background(), "ws1", []string{testSessionID}); err == nil {
		t.Error("err = nil, want an error for pgrep rc 2 after pkill")
	}
	if len(*calls) != len(steps) {
		t.Errorf("calls = %d, want %d", len(*calls), len(steps))
	}
}

func TestStopWorkspaceReadFails(t *testing.T) {
	a, _, calls := stopAdapter(t, "infra", nil)
	a.Client = New("http://127.0.0.1:1", a.Client.TokenFile)
	if err := a.Stop(context.Background(), "ws1", nil); err == nil {
		t.Error("err = nil, want an error when the workspace cannot be read")
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none", *calls)
	}
}
