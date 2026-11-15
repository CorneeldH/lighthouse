//go:build !windows

package worktreeaudit

import (
	"errors"
	"math"
	"syscall"
)

// PidAlive measures pid with signal 0: true when the process exists (also when it belongs to
// someone else: EPERM), false only for ESRCH, nil when it cannot be measured. A pid of 0 or less
// would signal a process group and a pid beyond the C int range overflows: neither reaches kill.
func PidAlive(pid int) *bool {
	if pid <= 0 || pid > math.MaxInt32 {
		return nil
	}
	return aliveFromKill(syscall.Kill(pid, 0))
}

// aliveFromKill maps the result of kill(pid, 0).
func aliveFromKill(err error) *bool {
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return ptr(true)
	case errors.Is(err, syscall.ESRCH):
		return ptr(false)
	}
	return nil
}
