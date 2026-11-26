package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery"
	"github.com/CorneeldH/lighthouse/internal/telemetry/recovery/recoverytest"
)

// Classification: cli-output (exit code, stdout) over git and SQLite with a fake forge. These
// tests fail if the command stops passing --repo-dir, --days and --db to the measurement, prints
// something else than the measurement, or maps an error or wrong usage to another exit code.
// What the measurement does is tested in internal/telemetry/recovery.

// mrGit runs git in dir; other tests of this package use it too.
var mrGit = recoverytest.Git

type mrRig struct {
	*recoverytest.Rig
	t       *testing.T
	openErr error
}

func newMRRig(t *testing.T) *mrRig {
	t.Helper()
	return &mrRig{Rig: recoverytest.NewRig(t), t: t}
}

func (r *mrRig) run(args ...string) (int, string, string) {
	r.t.Helper()
	env := measureRecoveryEnv{
		Getenv: func(string) string { return "" },
		Now:    func() time.Time { return recoverytest.After },
		Open: func(context.Context, repoconfig.Repo) (recovery.Forge, error) {
			if r.openErr != nil {
				return nil, r.openErr
			}
			return r.Fake, nil
		},
	}
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newMeasureRecoveryCommand(env))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"measure-recovery"}, args...))
	return execute(root, &errOut), out.String(), errOut.String()
}

func (r *mrRig) base(extra ...string) []string {
	return append([]string{"--repo-dir", r.Clone, "--db", r.DB}, extra...)
}

func TestMeasureRecoveryCommandMeasuresIntoDB(t *testing.T) {
	r := newMRRig(t)
	r.Fake.Merged = []forge.PRSummary{{Number: 7, MergedAt: recoverytest.Merge7}, {Number: 9, MergedAt: recoverytest.Merge8}}
	r.Fake.Heads = append(r.Fake.Heads, forge.PullHead{Number: 9, HeadRef: "fix-los"})
	code, out, errOut := r.run(r.base("--days", "3")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if want := "#7 recovered 1 items, 0 fix-pr rounds\n#9 skipped: no_issue_branch\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if want := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC); !r.Fake.Since.Equal(want) {
		t.Errorf("merged PRs since %v, want %v: --days 3", r.Fake.Since, want)
	}
	if n := r.Count(`SELECT count(*) FROM recoveries WHERE pr = 7 AND status = 'recovered'`); n != 1 {
		t.Errorf("recovered rows of #7 in --db = %d, want 1", n)
	}
}

func TestMeasureRecoveryCommandDefaultDays(t *testing.T) {
	r := newMRRig(t)
	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if want := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC); !r.Fake.Since.Equal(want) {
		t.Errorf("merged PRs since %v, want %v: the default of 14 days", r.Fake.Since, want)
	}
}

func TestMeasureRecoveryCommandErrorsExitOne(t *testing.T) {
	cases := map[string]func(r *mrRig){
		"missing origin":  func(r *mrRig) { recoverytest.Git(t, r.Clone, nil, "remote", "remove", "origin") },
		"merged PRs fail": func(r *mrRig) { r.Fake.MergedErr = errors.New("boom") },
		"forge open":      func(r *mrRig) { r.openErr = errors.New("no token") },
		"bad config":      func(r *mrRig) { r.WriteConfig("[forge]\n") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newMRRig(t)
			setup(r)
			code, _, errOut := r.run(r.base()...)
			if code != 1 || errOut == "" {
				t.Fatalf("exit %d, stderr %q; want 1 with a message", code, errOut)
			}
			if n := r.Count(`SELECT count(*) FROM recoveries`); n != 0 {
				t.Errorf("recoveries = %d, want nothing written", n)
			}
		})
	}
}

func TestMeasureRecoveryUsage(t *testing.T) {
	r := newMRRig(t)
	for name, args := range map[string][]string{
		"no repo-dir": {"--db", r.DB},
		"days zero":   r.base("--days", "0"),
		"unknown":     r.base("--nope"),
		"extra arg":   r.base("x"),
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, errOut := r.run(args...); code != 2 {
				t.Errorf("exit %d, stderr %q; want 2", code, errOut)
			}
		})
	}
}
