//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package flock

import (
	"errors"
	"os"
)

// TryLock has no flock here: it fails before it opens the lock file.
func TryLock(string) (*os.File, error) {
	return nil, errors.New("file lock not supported on this platform")
}
