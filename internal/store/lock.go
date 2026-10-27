package store

import (
	"errors"
)

// ErrLocked reports that another server already holds the database lock.
var ErrLocked = errors.New("database locked")

// LockedError names the lock file another lighthouse serve holds.
type LockedError struct{ Path string }

func (e *LockedError) Error() string { return "another lighthouse serve holds " + e.Path }

// Is makes errors.Is(err, ErrLocked) true.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }
