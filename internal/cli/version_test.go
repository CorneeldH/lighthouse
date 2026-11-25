package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runVersion(args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs(args)
	code := execute(root, &errOut)
	return out.String(), errOut.String(), code
}

func TestVersionCompare(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"v0.28.0-rc.1", "v0.28.0", "-1\n"},
		{"0.29.0", "v0.29.0-rc.2", "1\n"},
		{"0.28.0", "v0.28.0", "0\n"},
		{"v0.28.0", "v0.28.0", "0\n"},
	}
	for _, tc := range cases {
		t.Run(tc.a+" "+tc.b, func(t *testing.T) {
			out, errOut, code := runVersion("version", "compare", tc.a, tc.b)
			if code != 0 || out != tc.want || errOut != "" {
				t.Errorf("code=%d out=%q err=%q, want 0 %q", code, out, errOut, tc.want)
			}
		})
	}
}

func TestVersionCompareRejectsInvalid(t *testing.T) {
	for _, bad := range []string{"dev", "1.2"} {
		t.Run(bad, func(t *testing.T) {
			out, errOut, code := runVersion("version", "compare", bad, "v0.28.0")
			if code != 1 || out != "" || !strings.Contains(errOut, `not a release tag: "v`+bad+`"`) {
				t.Errorf("code=%d out=%q err=%q, want exit 1 naming %q", code, out, errOut, bad)
			}
			_, errOut, code = runVersion("version", "compare", "v0.28.0", bad)
			if code != 1 || !strings.Contains(errOut, `not a release tag: "v`+bad+`"`) {
				t.Errorf("second arg: code=%d err=%q", code, errOut)
			}
		})
	}
}

func TestVersionStillPrintsVersion(t *testing.T) {
	out, _, code := runVersion("version")
	if code != 0 || out != "lighthouse dev\n" {
		t.Errorf("code=%d out=%q", code, out)
	}
}
