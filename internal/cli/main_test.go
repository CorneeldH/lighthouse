package cli

import (
	"os"
	"testing"
)

// TestMain points the state directory at a temporary one: a forge opened with os.Getenv opens
// the forge cache there, and a test must never write into the real one.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lighthouse-test-state-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
