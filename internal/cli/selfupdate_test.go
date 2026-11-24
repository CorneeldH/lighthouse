package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/selfupdate"
)

// Classification: these tests fail when the self-update command is not registered, when a test
// binary stops counting as a dev build, or when an output line changes; they observe stdout and
// the exit code only.

func TestSelfUpdateSkipsDevBuild(t *testing.T) {
	channelHome(t)
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
			"self-update: up to date (v0.11.3; newest on channel v0.11.3)\n"},
		{"up to date after dev to release", selfupdate.Result{Outcome: selfupdate.UpToDate, From: "v0.29.0-dev.3", To: "v0.28.0"}, "lighthouse.service",
			"self-update: up to date (v0.29.0-dev.3; newest on channel v0.28.0)\n"},
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

// Classification: the tests below fail when --channel is not validated before it is stored or
// passed on, when the stored file is not read, or when the file moves off $HOME.

// channelHome points HOME at a temp dir, unsets LIGHTHOUSE_HOST_TOML and returns the channel file path.
func channelHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIGHTHOUSE_HOST_TOML", "")
	return filepath.Join(home, ".config", "lighthouse", "channel")
}

// fakeRun replaces selfupdateRun and returns a pointer to the options it saw.
func fakeRun(t *testing.T, res selfupdate.Result) *selfupdate.Options {
	t.Helper()
	var got selfupdate.Options
	old := selfupdateRun
	selfupdateRun = func(_ context.Context, o selfupdate.Options) (selfupdate.Result, error) {
		got = o
		return res, nil
	}
	t.Cleanup(func() { selfupdateRun = old })
	return &got
}

func runSelfUpdate(args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut)
	root.SetArgs(args)
	code := execute(root, &errOut)
	return out.String(), errOut.String(), code
}

func TestSelfUpdateChannelFlagIsStored(t *testing.T) {
	path := channelHome(t)
	if _, e, code := runSelfUpdate("self-update", "--channel", "dev"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, e)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) != "dev" {
		t.Fatalf("channel file = %q, %v; want dev", b, err)
	}
}

func TestSelfUpdatePassesChannelToRun(t *testing.T) {
	channelHome(t)
	got := fakeRun(t, selfupdate.Result{Outcome: selfupdate.UpToDate, To: "v1.0.0"})
	if _, e, code := runSelfUpdate("self-update", "--channel", "dev"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, e)
	}
	if got.Channel != selfupdate.ChannelDev {
		t.Errorf("Channel = %q, want dev", got.Channel)
	}
}

func TestSelfUpdateUsesStoredChannel(t *testing.T) {
	path := channelHome(t)
	got := fakeRun(t, selfupdate.Result{Outcome: selfupdate.UpToDate, To: "v1.0.0"})
	if _, e, code := runSelfUpdate("self-update"); code != 0 || got.Channel != selfupdate.ChannelRelease {
		t.Fatalf("no file: exit %d (%s), Channel %q, want release", code, e, got.Channel)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, e, code := runSelfUpdate("self-update"); code != 0 || got.Channel != selfupdate.ChannelDev {
		t.Fatalf("stored dev: exit %d (%s), Channel %q, want dev", code, e, got.Channel)
	}
}

func TestSelfUpdateRejectsUnknownChannel(t *testing.T) {
	path := channelHome(t)
	if _, _, code := runSelfUpdate("self-update", "--channel", "stable"); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("channel file exists after invalid value: %v", err)
	}
}

func TestChannelCommandPrintsStored(t *testing.T) {
	path := channelHome(t)
	if out, _, code := runSelfUpdate("channel"); code != 0 || out != "release\n" {
		t.Fatalf("no file: exit %d, out %q", code, out)
	}
	if err := selfupdate.WriteChannel(path, selfupdate.ChannelDev); err != nil {
		t.Fatal(err)
	}
	if out, _, code := runSelfUpdate("channel"); code != 0 || out != "dev\n" {
		t.Fatalf("stored: exit %d, out %q", code, out)
	}
}

func TestSelfUpdateRejectsEmptyChannelFlag(t *testing.T) {
	path := channelHome(t)
	fakeRun(t, selfupdate.Result{Outcome: selfupdate.UpToDate, To: "v1.0.0"})
	if _, e, code := runSelfUpdate("self-update", "--channel", ""); code != 1 || !strings.Contains(e, "unknown channel") {
		t.Errorf("exit %d, stderr %q; want 1 and unknown channel", code, e)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("channel file exists after empty value: %v", err)
	}
}

func TestCorruptChannelFileFailsWithoutFallback(t *testing.T) {
	path := channelHome(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	old := selfupdateRun
	selfupdateRun = func(context.Context, selfupdate.Options) (selfupdate.Result, error) {
		called = true
		return selfupdate.Result{}, nil
	}
	t.Cleanup(func() { selfupdateRun = old })
	for _, args := range [][]string{{"channel"}, {"self-update"}} {
		out, e, code := runSelfUpdate(args...)
		if code != 1 || out != "" || !strings.Contains(e, path) {
			t.Errorf("%v: exit %d, out %q, stderr %q; want 1, no stdout, path in stderr", args, code, out, e)
		}
	}
	if called {
		t.Error("self-update ran with a corrupt channel file")
	}
}
