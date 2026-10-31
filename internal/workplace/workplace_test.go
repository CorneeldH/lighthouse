package workplace

import (
	"context"
	"testing"
	"time"
)

func TestStand(t *testing.T) {
	cases := []struct {
		state string
		want  Stand
	}{
		{"working", Working},
		{"done", Done},
		{"idle", Waiting},
		{"", Waiting},
	}
	for _, c := range cases {
		if got := (Session{State: c.state}).Stand(); got != c.want {
			t.Errorf("state %q: got %v, want %v", c.state, got, c.want)
		}
	}
}

func TestExecRunner(t *testing.T) {
	run := ExecRunner()
	ctx := context.Background()

	out, code, err := run(ctx, 5*time.Second, "sh", "-c", "echo hi; exit 0")
	if err != nil || code != 0 || out != "hi\n" {
		t.Errorf("success: out=%q code=%d err=%v", out, code, err)
	}
	_, code, err = run(ctx, 5*time.Second, "sh", "-c", "exit 3")
	if err != nil || code != 3 {
		t.Errorf("non-zero exit: code=%d err=%v, want code 3 and no error", code, err)
	}
	if _, _, err = run(ctx, 5*time.Second, "lighthouse-no-such-binary"); err == nil {
		t.Error("missing binary: want error")
	}
	if _, _, err = run(ctx, 100*time.Millisecond, "sleep", "5"); err == nil {
		t.Error("timeout: want error")
	}
}

func TestExecRunnerTimeoutWithGrandchildHoldingStdout(t *testing.T) {
	start := time.Now()
	_, _, err := ExecRunner()(context.Background(), 200*time.Millisecond, "sh", "-c", "sleep 30 & wait")
	if err == nil {
		t.Fatal("want timeout error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("returned after %v, want near the 200ms timeout", d)
	}
}

func TestExecRunnerSignalKilledIsError(t *testing.T) {
	_, code, err := ExecRunner()(context.Background(), 5*time.Second, "sh", "-c", "kill -9 $$")
	if err == nil {
		t.Errorf("killed by signal: want error, got code %d and no error", code)
	}
}
