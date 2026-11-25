package worktreeaudit

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseWorktreesNative(t *testing.T) {
	tests := []struct {
		name, in, want string
		goos           string // when set, the case runs only on this GOOS
	}{
		{"space in path", "worktree /a b/c\nHEAD x\nbranch refs/heads/y\n\n", filepath.FromSlash("/a b/c"), ""},
		{"windows drive path", "worktree C:/a b/c\nHEAD x\nbranch refs/heads/y\n\n", `C:\a b\c`, "windows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.goos != "" && runtime.GOOS != tt.goos {
				t.Skipf("%s only", tt.goos)
			}
			got := ParseWorktrees(tt.in)
			if len(got) != 1 || got[0].Path != tt.want {
				t.Fatalf("ParseWorktrees = %+v, want one path %q", got, tt.want)
			}
			if got[0].Branch == nil || *got[0].Branch != "y" {
				t.Errorf("branch = %v, want y (refs/heads/ must stay as is)", got[0].Branch)
			}
		})
	}
}
