package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "lighthouse dev\n" {
		t.Fatalf("version output = %q", got)
	}
}
