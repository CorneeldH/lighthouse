package gitpath

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestNative(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trailing newline", "/a/b\n", filepath.FromSlash("/a/b")},
		{"trailing crlf", "/a/b\r\n", filepath.FromSlash("/a/b")},
		{"surrounding spaces", "  /a/b ", filepath.FromSlash("/a/b")},
		{"empty", "", ""},
		{"only newline", "\n", ""},
		{"relative git dir", ".git\n", ".git"},
		{"cleaned", "/a//b/../c", filepath.Clean("/a//b/../c")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Native(tt.in); got != tt.want {
				t.Errorf("Native(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNativeWindowsForm(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only: native form uses backslashes")
	}
	if got, want := Native("C:/Users/x\n"), `C:\Users\x`; got != want {
		t.Errorf("Native(%q) = %q, want %q", "C:/Users/x\n", got, want)
	}
}
