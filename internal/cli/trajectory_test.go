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

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

var trT0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func trAt(m int) time.Time { return trT0.Add(time.Duration(m) * time.Minute) }

type trFake struct {
	issue forge.Issue
	err   error
	calls int
}

func (f *trFake) Issue(context.Context, int) (forge.Issue, error) {
	f.calls++
	return f.issue, f.err
}

type trRig struct {
	t    *testing.T
	dir  string
	db   string
	fake *trFake
	now  time.Time
}

// newTRRig builds a repo dir and a db holding trajectory o/r#7: a run from 0 to 35 that asks a
// question at 30, an answer run from 70 to 130 (answer at 60), and a done event at 100.
func newTRRig(t *testing.T, answered bool) *trRig {
	t.Helper()
	root := t.TempDir()
	r := &trRig{t: t, dir: filepath.Join(root, "repo"), db: filepath.Join(root, "l.db"), fake: &trFake{}, now: trAt(200)}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "lighthouse.toml"), []byte("[forge]\nrepo = \"o/r\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.OpenShared(ctx, r.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	claim := func(key string, at time.Time) int64 {
		run, err := runs.Take(ctx, st.DB, runs.Claim{Repo: "o/r", Issue: 7, SessionID: runs.NewSessionID(),
			Mode: "resume", Trigger: "handoff", TriggerKey: key, Now: at})
		if err != nil {
			t.Fatal(err)
		}
		return run.ID
	}
	ev := func(id, ref string, run int64, kind telemetry.EventKind, at time.Time) {
		_, _, err := telemetry.RecordEvent(ctx, st.DB, telemetry.EventRecord{
			Origin: telemetry.Origin{Source: "ev", SourceID: id, RecordedAt: trT0},
			Repo:   "o/r", Issue: 7, RunID: run, Kind: kind, Ref: ref, At: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	r1 := claim("start:7", trAt(0))
	ev("q1", "q1", r1, telemetry.EventQuestion, trAt(30))
	if err := runs.Release(ctx, st.DB, r1, runs.Question, "", trAt(35)); err != nil {
		t.Fatal(err)
	}
	if answered {
		r2 := claim("answer:9", trAt(70))
		ev("a1", "q1", r2, telemetry.EventAnswer, trAt(60))
		ev("d1", "2", r2, telemetry.EventDone, trAt(100))
		if err := runs.Release(ctx, st.DB, r2, runs.Done, "", trAt(130)); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *trRig) run(args ...string) (int, string, string) {
	r.t.Helper()
	env := trajectoryEnv{
		Getenv: func(string) string { return "" },
		Now:    func() time.Time { return r.now },
		Open: func(context.Context, repoconfig.Repo) (trajectoryForge, error) {
			return r.fake, nil
		},
	}
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newTrajectoryCommand(env))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"trajectory"}, args...))
	return execute(root, &errOut), out.String(), errOut.String()
}

func (r *trRig) base(issue string) []string {
	return []string{issue, "--repo-dir", r.dir, "--db", r.db}
}

func TestTrajectoryPrintsTimeline(t *testing.T) {
	r := newTRRig(t, true)
	code, out, errOut := r.run(r.base("7")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"question " + trAt(30).Format(time.RFC3339),
		"answer " + trAt(60).Format(time.RFC3339),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	pos := -1
	for _, c := range []string{"active ", "human ", "pickup "} {
		i := strings.Index(out, "\n"+c)
		if i < 0 {
			t.Fatalf("output lacks a %q line:\n%s", c, out)
		}
		if i < pos {
			t.Errorf("%q is out of precedence order:\n%s", c, out)
		}
		pos = i
	}
	if strings.Contains(out, "unexplained") {
		t.Errorf("unexplained shown while empty:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "human ") && !strings.HasSuffix(line, " measured") {
			t.Errorf("human line = %q, want measured", line)
		}
	}
}

func TestTrajectoryUsage(t *testing.T) {
	r := newTRRig(t, true)
	for _, args := range [][]string{{}, {"abc"}, {"0"}, {"7", "8"}} {
		if code, _, _ := r.run(append(args, "--repo-dir", r.dir, "--db", r.db)...); code != 2 {
			t.Errorf("args %v: exit %d, want 2", args, code)
		}
	}
}

func TestTrajectoryUnknownIssue(t *testing.T) {
	r := newTRRig(t, true)
	code, _, errOut := r.run(r.base("8")...)
	if code != 1 || !strings.Contains(errOut, "no trajectory for issue 8") {
		t.Errorf("exit %d, stderr %q; want 1 and the message", code, errOut)
	}
}

func TestTrajectoryUsesForgeClosedAt(t *testing.T) {
	r := newTRRig(t, false)
	_, open, _ := r.run(r.base("7")...)
	r.fake.issue = forge.Issue{Number: 7, State: "closed", ClosedAt: trAt(120)}
	code, closed, errOut := r.run(r.base("7")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if open == closed {
		t.Errorf("ClosedAt changed nothing:\n%s", closed)
	}
	if !strings.Contains(open, " running") || strings.Contains(closed, " running") {
		t.Errorf("open should run, closed should not:\nopen:\n%s\nclosed:\n%s", open, closed)
	}
}

func TestTrajectoryForgeErrorIsExit1(t *testing.T) {
	r := newTRRig(t, true)
	r.fake.err = errors.New("boom")
	if code, _, errOut := r.run(r.base("7")...); code != 1 || !strings.Contains(errOut, "boom") {
		t.Errorf("exit %d, stderr %q; want 1 with boom", code, errOut)
	}
}

// formatTimeline prints every moment with its marks (estimate, unanswered, a takeover's reason),
// then one line per cause present in precedence order, each with its kind and, for active that
// may hold CI/review time, incl. ci_review. Dropping a mark or reordering a cause fails this.
func TestFormatTimeline(t *testing.T) {
	m := func(n int) time.Duration { return time.Duration(n) * time.Minute }
	moments := telemetry.TrajectoryTimeline{
		Questions: []telemetry.Moment{
			{At: trAt(30), RunID: 1, Answered: true},
			{At: trAt(90), RunID: 3, Answered: true, Estimate: true},
			{At: trAt(150), SessionID: 2},
			{At: trAt(170), RunID: 5, Estimate: true},
		},
		Answers: []telemetry.Moment{
			{At: trAt(60), RunID: 2},
			{At: trAt(100), RunID: 4, Estimate: true},
		},
		Takeovers: []telemetry.Moment{
			{At: trAt(120), RunID: 4, Reason: "no handoff"},
			{At: trAt(140), SessionID: 2, Reason: "session"},
		},
	}
	momentLines := "" +
		"question 2026-11-20T09:30:00Z\n" +
		"question 2026-11-20T10:30:00Z estimate\n" +
		"question 2026-11-20T11:30:00Z unanswered\n" +
		"question 2026-11-20T11:50:00Z estimate unanswered\n" +
		"answer 2026-11-20T10:00:00Z\n" +
		"answer 2026-11-20T10:40:00Z estimate\n" +
		"takeover 2026-11-20T11:00:00Z no handoff\n" +
		"takeover 2026-11-20T11:20:00Z session\n"
	all := map[telemetry.Cause]telemetry.CauseTotal{
		telemetry.CauseUnexplained: {Duration: m(2)},
		telemetry.CausePickup:      {Duration: m(5), Estimate: true},
		telemetry.CauseCIReview:    {Duration: m(20)},
		telemetry.CauseUsage:       {Duration: m(15), Running: true},
		telemetry.CauseHuman:       {Duration: m(30), Estimate: true, Running: true},
		telemetry.CauseActive:      {Duration: m(60) + 30*time.Second, InclusiveCIReview: true},
	}
	tests := []struct {
		name    string
		byCause map[telemetry.Cause]telemetry.CauseTotal
		want    string
	}{
		{"every cause", all, momentLines +
			"active 1h0m30s measured incl. ci_review\n" +
			"human 30m0s running\n" +
			"usage 15m0s running\n" +
			"ci_review 20m0s measured\n" +
			"pickup 5m0s estimate\n" +
			"unexplained 2m0s measured\n"},
		{"no unexplained", map[telemetry.Cause]telemetry.CauseTotal{
			telemetry.CauseActive:   {Duration: m(40)},
			telemetry.CauseCIReview: {Duration: m(10), Running: true},
		}, momentLines +
			"active 40m0s measured\n" +
			"ci_review 10m0s running\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl := moments
			tl.ByCause = tt.byCause
			if got := formatTimeline(tl); got != tt.want {
				t.Errorf("formatTimeline =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
