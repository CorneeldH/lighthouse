package store

import (
	"errors"
	"os"

	"github.com/CorneeldH/lighthouse/internal/flock"
)

// ErrLocked reports that another server already holds the database lock.
var ErrLocked = errors.New("database locked")

// LockedError names the lock file another lighthouse serve holds.
type LockedError struct{ Path string }

func (e *LockedError) Error() string { return "another lighthouse serve holds " + e.Path }

// Is makes errors.Is(err, ErrLocked) true.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// acquireLock takes the single-server lock on path. The lock lives as long
// as the returned file stays open.
func acquireLock(path string) (*os.File, error) {
	f, err := flock.TryLock(path)
	if errors.Is(err, flock.ErrHeld) {
		return nil, &LockedError{Path: path}
	}
	return f, err
}
