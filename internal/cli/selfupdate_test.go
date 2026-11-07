package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/selfupdate"
)

// Classification: these tests fail when the self-update command is not registered, when a test
// binary stops counting as a dev build, or when an output line changes; they observe stdout and
// the exit code only.

func TestSelfUpdateSkipsDevBuild(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs([]string{"self-update"})
	if code := execute(root, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if want := "self-update: skipped — dev build\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}

func TestReportSelfUpdate(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  selfupdate.Result
		unit string
		want string
	}{
		{"dev", selfupdate.Result{Outcome: selfupdate.SkippedDev, From: "dev"}, "lighthouse.service",
			"self-update: skipped — dev build\n"},
		{"up to date", selfupdate.Result{Outcome: selfupdate.UpToDate, From: "v0.11.3", To: "v0.11.3"}, "",
			"self-update: up to date (v0.11.3)\n"},
		{"up to date ignores unit", selfupdate.Result{Outcome: selfupdate.UpToDate, To: "v0.11.3"}, "lighthouse.service",
			"self-update: up to date (v0.11.3)\n"},
		{"replaced with restart", selfupdate.Result{Outcome: selfupdate.Replaced, From: "v0.11.2", To: "v0.11.3"}, "lighthouse.service",
			"self-update: replaced v0.11.2 with v0.11.3\nself-update: restarted lighthouse.service\n"},
		{"replaced without restart", selfupdate.Result{Outcome: selfupdate.Replaced, From: "v0.11.2", To: "v0.11.3"}, "",
			"self-update: replaced v0.11.2 with v0.11.3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			reportSelfUpdate(&out, tc.res, tc.unit)
			if out.String() != tc.want {
				t.Errorf("got %q, want %q", out.String(), tc.want)
			}
		})
	}
}
