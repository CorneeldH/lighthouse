//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package flock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// TryLock takes an exclusive, non-blocking flock on path, creating the file
// (0600) when missing. The lock lives as long as the returned file stays
// open. A lock held elsewhere gives an error that matches ErrHeld.
func TryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("lock %s: %w", path, ErrHeld)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}
