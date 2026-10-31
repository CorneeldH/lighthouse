package kennforge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// TestStopKillsRealUnit proves the unit half of "no process left" against real systemd.
// It skips only without a user bus or systemd-run; is-system-running is no guard, because
// it exits 1 on a degraded host.
func TestStopKillsRealUnit(t *testing.T) {
	if err := exec.Command("systemctl", "--user", "show-environment").Run(); err != nil {
		t.Skipf("no user systemd: %v", err)
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("no systemd-run")
	}
	dir := filepath.Join(t.TempDir(), fmt.Sprintf("ws-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	unit := UnitName("lhtest", dir)
	t.Cleanup(func() { _ = exec.Command("systemctl", "--user", "stop", unit).Run() })
	if out, err := exec.Command("systemd-run", "--user", "--unit="+unit, "sleep", "600").CombinedOutput(); err != nil {
		t.Fatalf("systemd-run: %v: %s", err, out)
	}
	pidOut, err := exec.Command("systemctl", "--user", "show", "-p", "MainPID", "--value", unit).Output()
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(string(pidOut))
	if pid == "" || pid == "0" {
		t.Fatalf("MainPID = %q, want a running process", pid)
	}
	if _, err := os.Stat("/proc/" + pid); err != nil {
		t.Fatalf("process %s not running before Stop: %v", pid, err)
	}

	f, a := newFakeForge(t)
	f.path = dir
	a.Profile = "lhtest"
	a.Run = workplace.ExecRunner()
	if err := a.Stop(context.Background(), "ws1", nil); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("systemctl", "--user", "is-active", unit).Output()
	if s := strings.TrimSpace(string(out)); s == "active" {
		t.Errorf("unit still %q", s)
	}
	if _, err := os.Stat("/proc/" + pid); err == nil {
		t.Errorf("process %s still exists after Stop", pid)
	}
}
