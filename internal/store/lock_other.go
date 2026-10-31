//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package store

import (
	"errors"
	"os"
)

// acquireLock has no flock here: it fails before it opens the lock file.
func acquireLock(string) (*os.File, error) {
	return nil, errors.New("database lock not supported on this platform")
}
