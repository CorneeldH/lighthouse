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
