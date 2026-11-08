package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runGate(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv("LIGHTHOUSE_GATE_FILES", "")
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs(append([]string{"gate"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func TestGateUnknownSubcommandIsUnchecked(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus", "--", "x"}, {"nod", "--", "lint"}} {
		code, _, errOut := runGate(t, args...)
		if code != 3 || !strings.Contains(errOut, "COULD NOT CHECK") {
			t.Errorf("gate %q: code=%d stderr=%q", args, code, errOut)
		}
	}
}

func TestGateCommandPassesFlags(t *testing.T) {
	code, out, errOut := runGate(t, "go", "--", "sh", "-c", `echo "$@"`, "x", "-race")
	if code != 0 || out != "-race ./...\n" {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestGateGoPassesExitCode(t *testing.T) {
	code, _, errOut := runGate(t, "go", "--", "sh", "-c", "exit 5")
	if code != 5 || errOut != "" {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}
