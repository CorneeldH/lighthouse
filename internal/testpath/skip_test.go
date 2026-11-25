package testpath

import (
	"path/filepath"
	"testing"
)

func TestSkipOnWindowsPOSIXAbs(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("on Windows the helper skips by design")
	}
	t.Run("sub", func(t *testing.T) {
		SkipOnWindowsPOSIXAbs(t)
		if t.Skipped() {
			t.Error("skipped on a non-Windows platform")
		}
	})
}
