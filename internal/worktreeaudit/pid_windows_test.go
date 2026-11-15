//go:build windows

package worktreeaudit

import (
	"os"
	"testing"
)

// twin of test_native_windows_is_not_measurable_and_never_signalled
func TestWindowsPidIsNotMeasurable(t *testing.T) {
	for _, pid := range []int{os.Getpid(), 4242} {
		if got := PidAlive(pid); got != nil {
			t.Fatalf("PidAlive(%d) = %v, want nil on Windows", pid, *got)
		}
	}
}
