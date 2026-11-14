package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Classification: cli-output. Fails when an undetermined turn no longer exits 1 with its reason on
// stdout, the contract the Stop gates read (the cases with a repo are in test_turn_roots.py).
func TestTurnRootsCommandUndetermined(t *testing.T) {
	tests := []struct{ in, wantOut string }{
		{"{not json", "the payload is not valid JSON.\n"},
		{"[]", "the payload is no JSON object.\n"},
		{`{"cwd":"/x"}`, "the payload has no transcript_path.\n"},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		root := NewRoot(strings.NewReader(tt.in), &out, &errOut)
		root.SetArgs([]string{"turn-roots", "--match", "*.go"})
		code := execute(root, &errOut)
		if code != 1 || out.String() != tt.wantOut || errOut.String() != "" {
			t.Errorf("%q: code %d out %q err %q, want 1 %q \"\"", tt.in, code, out.String(), errOut.String(), tt.wantOut)
		}
	}
}
