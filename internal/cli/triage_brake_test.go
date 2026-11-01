package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// brakeMeter is a meter on a temporary cache, clock t0, with a cache of the given age and
// percentages. A refresh fails the test unless refreshErr is set; a claude process never runs.
func brakeMeter(t *testing.T, age time.Duration, session, week int, refreshErr error) usage.Meter {
	t.Helper()
	m := usage.NewMeter(usage.Paths{Dir: t.TempDir()}, usage.DefaultConfig())
	m.Now = func() time.Time { return t0 }
	m.RunUsage = func(context.Context, string) ([]byte, error) {
		if refreshErr == nil {
			t.Error("the meter refreshed: the cache should be fresh")
			return nil, errors.New("no refresh in this test")
		}
		return nil, refreshErr
	}
	m.ClaudeRunning = func() bool { return false }
	s := usage.State{Schema: 1, GeneratedAt: t0.Add(-age).UTC().Format("2006-01-02T15:04:05Z"),
		Session: usage.WindowState{Pct: session}, Week: usage.WindowState{Pct: week}}
	if err := usage.WriteState(m.Paths.Cache(), s); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUsageBrake(t *testing.T) {
	cases := []struct {
		name       string
		age        time.Duration
		session    int
		week       int
		refreshErr error
		hold       bool
		pause      string // content of the pause flag, "" for none
		want       string // "" is open
	}{
		{name: "fresh cache", age: 10 * time.Second, session: 10, week: 10},
		{name: "four minutes old at TTL 5m, no claude", age: 4 * time.Minute, session: 10, week: 10},
		{name: "eight minutes old, refresh busy", age: 8 * time.Minute, session: 10, week: 10, hold: true},
		{name: "eleven minutes old", age: 11 * time.Minute, session: 10, week: 10,
			refreshErr: errors.New("claude gone"), want: "usage: not measured"},
		{name: "session over the threshold", age: 10 * time.Second, session: 87, week: 10,
			want: "usage: session 87%, week 10%"},
		{name: "week over the threshold", age: 10 * time.Second, session: 10, week: 95,
			want: "usage: session 10%, week 95%"},
		{name: "pause flag", age: 10 * time.Second, session: 10, week: 10,
			pause: t0.Add(time.Hour).Format(time.RFC3339), want: "paused until " + t0.Add(time.Hour).Format(time.RFC3339)},
		{name: "expired pause flag", age: 10 * time.Second, session: 10, week: 10,
			pause: t0.Add(-time.Hour).Format(time.RFC3339)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := brakeMeter(t, c.age, c.session, c.week, c.refreshErr)
			if c.hold {
				held, err := flock.TryLock(m.Paths.Lock())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = held.Close() })
			}
			pauseFile := filepath.Join(t.TempDir(), "PAUSE")
			if c.pause != "" {
				if err := os.WriteFile(pauseFile, []byte(c.pause+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			b := usageBrake{Meter: m, PauseFile: pauseFile, Now: func() time.Time { return t0 }}
			reason, open := b.Check()
			if open != (c.want == "") || reason != c.want {
				t.Errorf("Check = %q, %v; want %q, open %v", reason, open, c.want, c.want == "")
			}
		})
	}
}

// triageRoot is a root with only the triage command, on the environment of fx.
func triageRoot(fx *triageFix, out, errOut *bytes.Buffer, args ...string) *cobra.Command {
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newTriageCommand(func() (triageEnv, error) { return fx.env, nil }))
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(append([]string{"triage", "--db", fx.db}, args...))
	return root
}

func TestTriageCommandExitCodes(t *testing.T) {
	t.Run("round ran", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut), &errOut); code != 0 {
			t.Errorf("exit code = %d, want 0\n%s", code, errOut.String())
		}
	})
	t.Run("no repo with triage", func(t *testing.T) {
		fx := triageFixture(t, strings.Replace(triageToml, "[controller.triage]\n", "", 1))
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut), &errOut); code != 1 {
			t.Errorf("exit code = %d, want 1\n%s", code, errOut.String())
		}
	})
	t.Run("pause flag", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		pauseFile := filepath.Join(t.TempDir(), "PAUSE")
		if err := park.WritePause(pauseFile, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		fx.env.Brake = usageBrake{Meter: brakeMeter(t, 10*time.Second, 10, 10, nil), PauseFile: pauseFile,
			Now: func() time.Time { return t0 }}
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut), &errOut); code != 1 {
			t.Errorf("exit code = %d, want 1\n%s", code, errOut.String())
		}
		if want := "lighthouse: triage skipped: paused until " + t0.Add(time.Hour).Format(time.RFC3339) + "\n"; errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
	})
	t.Run("no token", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		base := fx.env.Getenv
		fx.env.Getenv = func(k string) string {
			if k == "GH_TOKEN" || k == "GITHUB_TOKEN" {
				return ""
			}
			return base(k)
		}
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut), &errOut); code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
		if want := "lighthouse: triage: no GitHub token: set GH_TOKEN or GITHUB_TOKEN\n"; errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
	})
	t.Run("panic", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.issue(7, "2-area:cli")
		fx.onBoard(7, "t1", nil)
		fx.env.Judge = func(context.Context, string, string) ([]byte, error) { panic("boom") }
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut), &errOut); code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
		if want := "lighthouse: triage panicked: boom\n"; errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
	})
	t.Run("environment fails", func(t *testing.T) {
		root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(newTriageCommand(func() (triageEnv, error) { return triageEnv{}, errors.New("no HOME") }))
		root.SetArgs([]string{"triage"})
		var errOut bytes.Buffer
		if code := execute(root, &errOut); code != 2 || errOut.String() != "lighthouse: no HOME\n" {
			t.Errorf("exit code = %d, stderr = %q; want 2, %q", code, errOut.String(), "lighthouse: no HOME\n")
		}
	})
}

func TestTriageCommandFlags(t *testing.T) {
	t.Run("issue without dry-run", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut, "--issue", "o/r#7"), &errOut); code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
		if want := "lighthouse: --issue requires --dry-run\n"; errOut.String() != want {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
	})
	t.Run("issue twice", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		fx.issue(7, "2-area:cli")
		fx.issue(8, "2-area:cli")
		fx.judge.answers[7] = forward(7)
		fx.judge.answers[8] = forward(8)
		var out, errOut bytes.Buffer
		root := triageRoot(fx, &out, &errOut, "--dry-run", "--issue", "o/r#7", "--issue", "o/r#8")
		if code := execute(root, &errOut); code != 0 {
			t.Fatalf("exit code = %d, want 0\n%s", code, errOut.String())
		}
		if got := fx.judge.called(); len(got) != 2 {
			t.Errorf("judged %v, want issues 7 and 8", got)
		}
	})
	// The host unit counts exit code 1 as success: a typo in ExecStart must give 2.
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--dryrun"}, "lighthouse: unknown flag: --dryrun\n"},
		{"argument", []string{"now"}, "lighthouse: unknown command \"now\" for \"lighthouse triage\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := triageFixture(t, triageToml)
			var out, errOut bytes.Buffer
			if code := execute(triageRoot(fx, &out, &errOut, tc.args...), &errOut); code != 2 {
				t.Errorf("exit code = %d, want 2\n%s", code, errOut.String())
			}
			if errOut.String() != tc.want {
				t.Errorf("stderr = %q, want %q", errOut.String(), tc.want)
			}
			if n := fx.srv.Count("/user"); n != 0 {
				t.Errorf("/user requests = %d, want 0: the round must not start", n)
			}
		})
	}
	t.Run("bad issue", func(t *testing.T) {
		fx := triageFixture(t, triageToml)
		var out, errOut bytes.Buffer
		if code := execute(triageRoot(fx, &out, &errOut, "--dry-run", "--issue", "nonsense"), &errOut); code != 2 {
			t.Errorf("exit code = %d, want 2\n%s", code, errOut.String())
		}
	})
}
