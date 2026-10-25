//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package usage

import "errors"

// tryLock has no flock here: every refresh fails and the old cache stays.
func tryLock(string) (unlock func(), held bool, err error) {
	return nil, false, errors.New("file lock not supported on this platform")
}
