//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package flock

import (
	"errors"
	"os"
)

// TryLock has no flock here: it fails before it opens the lock file.
func TryLock(string) (*os.File, error) {
	return nil, errors.New("file lock not supported on this platform")
}

// Lock has no flock here either.
func Lock(string) (*os.File, error) {
	return nil, errors.New("file lock not supported on this platform")
}

// TryFile has no flock here.
func TryFile(*os.File) (bool, error) {
	return false, errors.New("file lock not supported on this platform")
}
