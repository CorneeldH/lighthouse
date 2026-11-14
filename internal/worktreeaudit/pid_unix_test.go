//go:build !windows

package worktreeaudit

import (
	"os"
	"syscall"
	"testing"
)

func want(t *testing.T, what string, got *bool, w *bool) {
	t.Helper()
	switch {
	case got == nil && w == nil:
	case got == nil || w == nil || *got != *w:
		t.Errorf("%s = %v, want %v", what, deref(got), deref(w))
	}
}

// twin of test_own_pid_is_alive
func TestOwnPidIsAlive(t *testing.T) {
	want(t, "own pid", PidAlive(os.Getpid()), ptr(true))
}

// twin of test_eperm_counts_as_alive
func TestEPERMCountsAsAlive(t *testing.T) {
	want(t, "EPERM", aliveFromKill(syscall.EPERM), ptr(true))
}

// twin of test_only_esrch_is_dead
func TestOnlyESRCHIsDead(t *testing.T) {
	want(t, "ESRCH", aliveFromKill(syscall.ESRCH), ptr(false))
}

// twin of test_other_oserror_is_not_measurable
func TestOtherErrnoIsNotMeasurable(t *testing.T) {
	want(t, "EINVAL", aliveFromKill(syscall.EINVAL), nil)
}

// twin of test_pid_beyond_c_int_is_not_measurable
func TestPidBeyondCIntIsNotMeasurable(t *testing.T) {
	for _, pid := range []int{1 << 31, 1_000_000_000_000} {
		want(t, "pid beyond C int", PidAlive(pid), nil)
	}
}

// twin of test_non_positive_pid_is_not_measurable_and_never_signalled: 0 and -1 would signal a
// process group (the test's own with 0); nil, without a signal, is the only answer.
func TestNonPositivePidIsNotMeasurable(t *testing.T) {
	for _, pid := range []int{0, -1} {
		want(t, "non-positive pid", PidAlive(pid), nil)
	}
}
