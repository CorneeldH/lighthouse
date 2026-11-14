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
	held, err := TryFile(f)
	if err != nil || !held {
		_ = f.Close()
		if err == nil {
			err = ErrHeld
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}

// TryFile takes an exclusive, non-blocking flock on the open file f. A lock held by another open
// file description is not an error: it gives false, nil. Any other failure gives false and the error.
func TryFile(f *os.File) (held bool, err error) {
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// Lock is TryLock that waits for the holder instead of failing with ErrHeld.
func Lock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}
