//go:build unix

package pusher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReviewTimeoutIsReject(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	slow := "#!/bin/sh\nsleep 60 &\necho $! > " + pidFile + "\nwait\n"
	setupReview(t, true, map[string]string{"r1": slow, "r2": answer(approveJSON)})
	start := time.Now()
	v := Review(context.Background(), cfgOf(2*time.Second, rv("secrets", "r1"), rv("injection", "r2")), sampleDiff, nil, nil)
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("review took %s, the timeout did not stop it", took)
	}
	if v.Approve || v.Results[0].Approve || !strings.Contains(v.Results[0].Why, "timeout") {
		t.Fatalf("timeout must reject: %+v", v)
	}
	text, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	for _, c := range strings.TrimSpace(string(text)) {
		pid = pid*10 + int(c-'0')
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still runs: the process group was not killed", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
