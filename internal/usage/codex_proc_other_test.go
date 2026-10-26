//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package usage

import "testing"

// requireProcessGone cannot probe a pid here: the check is skipped.
func requireProcessGone(t *testing.T, _ int) {
	t.Helper()
	t.Log("process-gone check needs kill(pid, 0); skipped on this platform")
}
