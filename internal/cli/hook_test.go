package cli

import (
	"bytes"
	"testing"
)

// Classification: a cli-output change; the surface is the exit code and stderr of an unknown hook.
func TestHookCommandUnknown(t *testing.T) {
	t.Setenv("LIGHTHOUSE_REPO_ROOT", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader([]byte(`{"session_id":"s"}`)), &out, &errOut)
	root.SetArgs([]string{"hook", "nope", "pre"})
	code := execute(root, &errOut)
	want := "COULD NOT CHECK: unknown hook nope — install the latest lighthouse release\n"
	if code != 3 || out.String() != "" || errOut.String() != want {
		t.Errorf("code %d out %q err %q, want 3 %q", code, out.String(), errOut.String(), want)
	}
}

// Classification: a config change; the surface is where the notice markers go, or that they go
// nowhere (and the notices stay quiet) when no private state directory is known.
func TestHookStateDir(t *testing.T) {
	tests := []struct {
		name, xdg, home, want string
	}{
		{"xdg", "/x/state", "/h", "/x/state/lighthouse/hook-notices"},
		{"relative xdg falls back to home", "rel/state", "/h", "/h/.local/state/lighthouse/hook-notices"},
		{"no xdg", "", "/h", "/h/.local/state/lighthouse/hook-notices"},
		{"no home", "", "", ""},
		{"relative home", "rel/state", "rel", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tt.xdg)
			t.Setenv("HOME", tt.home)
			got, err := hookStateDir()
			if got != tt.want || (err != nil) != (tt.want == "") {
				t.Errorf("hookStateDir() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
