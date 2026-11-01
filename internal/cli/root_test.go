package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func TestInitHasPackFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"init", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--pack stringArray", "--no-pack stringArray", "lighthouse init --upgrade --pack"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}

// failingRoot is a root whose only command returns err.
func failingRoot(err error) *cobra.Command {
	root := &cobra.Command{Use: "x", SilenceUsage: true, SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error { return err }}
	root.SetArgs(nil)
	return root
}

func TestExecuteExitError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantErr  string
	}{
		{"code 2 prints the text", &exitError{code: 2, err: errors.New("boom")}, 2, "lighthouse: boom\n"},
		{"code 1 with errSilent prints nothing", &exitError{code: 1, err: errSilent}, 1, ""},
		{"nil inner error prints nothing", &exitError{code: 2}, 2, ""},
		{"wrapped exitError keeps its code", fmt.Errorf("x: %w", &exitError{code: 2, err: errors.New("boom")}), 2, "lighthouse: x: boom\n"},
		{"wrapped errSilent prints nothing", fmt.Errorf("x: %w", &exitError{code: 2, err: fmt.Errorf("y: %w", errSilent)}), 2, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			if got := execute(failingRoot(tc.err), &errOut); got != tc.wantCode {
				t.Errorf("code = %d, want %d", got, tc.wantCode)
			}
			if errOut.String() != tc.wantErr {
				t.Errorf("stderr = %q, want %q", errOut.String(), tc.wantErr)
			}
		})
	}
}

func TestExecutePlainError(t *testing.T) {
	var errOut bytes.Buffer
	if got := execute(failingRoot(errors.New("bad")), &errOut); got != 1 {
		t.Errorf("code = %d, want 1", got)
	}
	if errOut.String() != "lighthouse: bad\n" {
		t.Errorf("stderr = %q", errOut.String())
	}
	errOut.Reset()
	if got := execute(failingRoot(errSilent), &errOut); got != 1 || errOut.Len() != 0 {
		t.Errorf("errSilent: code = %d, stderr = %q", got, errOut.String())
	}
}
