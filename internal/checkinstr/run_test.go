package checkinstr

import (
	"path/filepath"
	"testing"
)

// Red if a native absolute path (C:\… on Windows) is joined to the repo root again, so the
// mirror check reads a file that does not exist and ignores the exceptions.
func TestExceptionsPathKeepsAbsolutePaths(t *testing.T) {
	native := filepath.Join(t.TempDir(), "mirror-exceptions.txt")
	cases := []struct{ value, want string }{
		{native, native},
		{"/srv/mirror-exceptions.txt", "/srv/mirror-exceptions.txt"},
		{"mirror/exceptions.txt", "/repo/mirror/exceptions.txt"},
	}
	for _, c := range cases {
		if got := exceptionsPath("/repo", c.value); got != c.want {
			t.Errorf("exceptionsPath(%q) = %q, want %q", c.value, got, c.want)
		}
	}
}
