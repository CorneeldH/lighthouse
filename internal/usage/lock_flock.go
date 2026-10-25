//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package usage

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock on path without waiting. held is false,
// without an error, when another open file holds the lock. unlock closes the
// file, which releases the lock.
func tryLock(path string) (unlock func(), held bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = f.Close() }, true, nil
}
