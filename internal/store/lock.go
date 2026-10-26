package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked reports that another server already holds the database lock.
var ErrLocked = errors.New("database locked")

// LockedError names the lock file another lighthouse serve holds.
type LockedError struct{ Path string }

func (e *LockedError) Error() string { return "another lighthouse serve holds " + e.Path }

// Is makes errors.Is(err, ErrLocked) true.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// acquireLock takes an exclusive, non-blocking flock on path. The lock lives
// as long as the returned file stays open.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &LockedError{Path: path}
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}
