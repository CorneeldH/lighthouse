// Package flock takes exclusive, non-blocking advisory locks on files.
package flock

import "errors"

// ErrHeld reports that another open file description holds the lock.
var ErrHeld = errors.New("lock held")
