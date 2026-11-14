//go:build windows

package flock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes an exclusive, non-blocking LockFileEx lock on the first byte of
// path, creating the file when missing. The lock lives as long as the returned
// file stays open; a second handle, also in the same process, conflicts. A lock
// held elsewhere gives an error that matches ErrHeld.
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

// TryFile takes an exclusive, non-blocking LockFileEx lock on the first byte of the open file f,
// which needs read or write access. A lock held by another handle gives false, nil; any other
// failure gives false and the error.
func TryFile(f *os.File) (held bool, err error) {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
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
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}
