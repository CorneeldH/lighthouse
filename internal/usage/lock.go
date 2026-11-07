package usage

import (
	"errors"

	"github.com/CorneeldH/lighthouse/internal/flock"
)

// tryLock takes the exclusive lock on path without waiting. held is false,
// without an error, when another open file holds the lock. unlock closes the
// file, which releases the lock.
func tryLock(path string) (unlock func(), held bool, err error) {
	f, err := flock.TryLock(path)
	if errors.Is(err, flock.ErrHeld) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() { _ = f.Close() }, true, nil
}
