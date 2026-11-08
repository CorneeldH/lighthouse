//go:build linux

package local

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tickUnit is the tick's service unit, relative to this package.
const tickUnit = "../../../deploy/systemd/lighthouse-tick.service"

// killMode reads the KillMode= value of the unit at path.
func killMode(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "KillMode="); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("%s has no KillMode= line", path)
	return ""
}

// TestTickUnitKeepsDetachedRun proves that a run the tick starts with setsid -f outlives the
// tick's unit under the unit's KillMode. It skips only without a user bus or systemd-run.
func TestTickUnitKeepsDetachedRun(t *testing.T) {
	if err := exec.Command("systemctl", "--user", "show-environment").Run(); err != nil {
		t.Skipf("no user systemd: %v", err)
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("no systemd-run")
	}
	mode := killMode(t, tickUnit)
	pidFile := filepath.Join(t.TempDir(), "detached.pid")
	t.Cleanup(func() { killPidFile(pidFile) })
	unit := fmt.Sprintf("lh-tick-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("systemctl", "--user", "stop", unit).Run() })

	script := fmt.Sprintf(`setsid -f sh -c "echo \$\$ > '%s'; exec sleep 600"`, pidFile)
	out, err := exec.Command("systemd-run", "--user", "--wait", "--collect", "-p", "KillMode="+mode,
		"--unit="+unit, "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("systemd-run: %v: %s", err, out)
	}

	// setsid -f returns before its child writes, so the file may come after the unit ended.
	var pid int
	if !waitFor(5*time.Second, func() bool {
		var ok bool
		pid, ok = readPid(t, pidFile)
		return ok
	}) {
		t.Fatalf("no pid in %s 5 s after the unit ended (KillMode=%s): the detached child did not survive", pidFile, mode)
	}
	if st, ok := procState(t, pid); !ok || st == "Z" {
		t.Errorf("detached child %d after the unit ended: state %q, alive %v; want a live process (KillMode=%s)", pid, st, ok, mode)
	}
}
