package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBoardStartTakesRawArgumentsAndExitCodes runs the real command: the arguments `<nr>` and
// `--reopen <nr>` reach boardstart.Run unparsed, and its exit code is the process exit code.
func TestBoardStartTakesRawArgumentsAndExitCodes(t *testing.T) {
	toml := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(toml, []byte("[forge]\nrepo = \"Owner/repo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", toml)
	for _, tc := range []struct {
		args []string
		out  string
		code int
	}{
		{[]string{"board-start", "7"}, "board: skipped — no [forge.board] configured\n", 0},
		{[]string{"board-start", "--reopen", "7"}, "board: skipped — no [forge.board] configured\n", 0},
		{[]string{"board-start", "--reopen"}, "board: usage: lighthouse board-start --reopen <nr>\n", 1},
		{[]string{"board-start", "--help"}, "board: usage: lighthouse board-start <nr>\n", 0},
	} {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(""), &out, &errOut)
		root.SetArgs(tc.args)
		if code := execute(root, &errOut); code != tc.code || out.String() != tc.out || errOut.Len() != 0 {
			t.Errorf("%v = %d, %q, stderr %q; want %d, %q", tc.args, code, out.String(), errOut.String(), tc.code, tc.out)
		}
	}
}
