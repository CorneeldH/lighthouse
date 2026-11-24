//go:build windows

package codelang

import "testing"

// A payload on Windows carries `C:\…`: absFile must keep it, not glue the cwd in front (#559).
func TestAbsFileWindows(t *testing.T) {
	if got := absFile(`C:\r\a.go`, `C:\cwd`); got != `C:\r\a.go` {
		t.Errorf("absFile = %q, want %q", got, `C:\r\a.go`)
	}
}
