//go:build unix

package hookcheck

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A hook that exits 0 but leaves a child holding its stdout is judged by its exit, and the child
// is stopped instead of left running.
func TestSmokeExit0WithChildHoldingOutput(t *testing.T) {
	root := fakeRoot(t, "repo")
	pidFile := filepath.Join(t.TempDir(), "pid")
	body := "sleep 30 &\necho $! >'" + pidFile + "'\nexit 0\n"
	regs := []registration{{Event: "Stop", Timeout: 20 * time.Second, Command: fakeHook(t, root, "bg.sh", body)}}

	start := time.Now()
	out, findings := smokeOut(testEnv(t, root), regs, "")
	took := time.Since(start)
	if findings != 0 || out != "ok    hook bg.sh (Stop)\n" {
		t.Fatalf("findings %d, out:\n%s\nwant 0 and the ok line", findings, out)
	}
	if took > 5*time.Second {
		t.Fatalf("took %v, want about the WaitDelay of 2s, not the sleep of 30s", took)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	// The kill is asynchronous, and init reaps the orphan: give it a moment.
	for range 50 {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the child %d of the hook still runs after the check", pid)
}
