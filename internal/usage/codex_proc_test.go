//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package usage

import (
	"errors"
	"syscall"
	"testing"
)

// requireProcessGone fails when the process with this pid still exists.
func requireProcessGone(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("kill(pid, 0) = %v, want ESRCH: child left behind", err)
	}
}
