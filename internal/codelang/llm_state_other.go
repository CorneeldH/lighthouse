//go:build !unix

package codelang

// noFollow has no open flag here; private never holds, so no marker is opened.
const noFollow = 0

// private never holds without unix ownership and modes, so the chain keeps no marker and
// has no quiet period.
func private(string) bool { return false }
