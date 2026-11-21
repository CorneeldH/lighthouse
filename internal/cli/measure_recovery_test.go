package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
)

func mrGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func mrCommit(t *testing.T, dir, file, subject string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(subject+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := at.Format(time.RFC3339)
	env := []string{"GIT_AUTHOR_DATE=" + d, "GIT_COMMITTER_DATE=" + d}
	mrGit(t, dir, env, "add", file)
	mrGit(t, dir, env, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", subject)
}

var (
	mrMerge7 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	mrMerge8 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	mrAfter  = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
)

// mrFake is a narrow forge; it counts the calls that cost API budget.
type mrFake struct {
	merged             []forge.PRSummary
	heads              []forge.PullHead
	issues             []forge.Issue
	mergedErr, issErr  error
	searches, headList int
	query              string
}

func (f *mrFake) MergedPRs(context.Context, time.Time, time.Time) ([]forge.PRSummary, error) {
	return f.merged, f.mergedErr
}

func (f *mrFake) PullHeads(context.Context) ([]forge.PullHead, error) {
	f.headList++
	return f.heads, nil
}

func (f *mrFake) SearchIssues(_ context.Context, text string) ([]forge.Issue, error) {
	f.searches++
	f.query = text
	return f.issues, f.issErr
}

type mrRig struct {
	t      *testing.T
	work   string // the working repo that pushes to origin
	origin string
	clone  string // what --repo-dir points at
	db     string
	fake   *mrFake
	now    time.Time
}

// newMRRig builds a bare origin, a clone that holds only the merge commit of #7, and then adds
// the fix of #7 and the merge commit of #8 to origin alone: the run must fetch them.
func newMRRig(t *testing.T) *mrRig {
	t.Helper()
	root := t.TempDir()
	r := &mrRig{t: t, work: filepath.Join(root, "work"), origin: filepath.Join(root, "origin.git"),
		clone: filepath.Join(root, "clone"), db: filepath.Join(root, "l.db"), now: mrAfter}
	mrGit(t, root, nil, "init", "-q", "--bare", "-b", "main", r.origin)
	mrGit(t, root, nil, "init", "-q", "-b", "main", r.work)
	mrGit(t, r.work, nil, "remote", "add", "origin", r.origin)
	mrCommit(t, r.work, "a.txt", "feat(a): maak a (#7)", mrMerge7)
	mrGit(t, r.work, nil, "push", "-q", "origin", "main")
	mrGit(t, root, nil, "clone", "-q", r.origin, r.clone)
	mrCommit(t, r.work, "a.txt", "fix(a): herstel a", mrMerge7.Add(24*time.Hour))
	mrCommit(t, r.work, "b.txt", "feat(b): maak b (#8)", mrMerge8)
	mrGit(t, r.work, nil, "push", "-q", "origin", "main")
	if err := os.WriteFile(filepath.Join(r.clone, "lighthouse.toml"), []byte("[forge]\nrepo = \"o/r\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenShared(context.Background(), r.db)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	r.fake = &mrFake{
		merged: []forge.PRSummary{{Number: 7, MergedAt: mrMerge7}},
		heads:  []forge.PullHead{{Number: 7, HeadRef: "issue-3-eerste"}},
	}
	return r
}

func (r *mrRig) run(args ...string) (int, string, string) {
	r.t.Helper()
	env := measureRecoveryEnv{
		Getenv: func(string) string { return "" },
		Now:    func() time.Time { return r.now },
		Open: func(context.Context, repoconfig.Repo) (recoveryForge, error) {
			return r.fake, nil
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
	return append([]string{"--repo-dir", r.clone, "--db", r.db}, extra...)
}

func (r *mrRig) count(query string) int {
	r.t.Helper()
	db, err := sql.Open("sqlite", r.db)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func TestMeasureRecoveryFetchesMeasuresAndIsIdempotent(t *testing.T) {
	r := newMRRig(t)
	r.fake.merged = []forge.PRSummary{
		{Number: 7, MergedAt: mrMerge7}, {Number: 8, MergedAt: mrMerge8},
		{Number: 9, MergedAt: mrMerge8}, {Number: 10, MergedAt: mrMerge8},
	}
	r.fake.heads = []forge.PullHead{
		{Number: 7, HeadRef: "issue-3-eerste"}, {Number: 8, HeadRef: "issue-4-tweede"},
		{Number: 9, HeadRef: "fix-los"}, {Number: 10, HeadRef: "issue-5-zonder-commit"},
	}
	before := mrGit(t, r.clone, nil, "rev-parse", "origin/main")

	code, out, errOut := r.run(r.base()...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"#7 recovered 1 items, 0 fix-pr-rondes\n",
		"#8 open 0 items, 0 fix-pr-rondes\n",
		"#9 overgeslagen: no_issue_branch\n",
		"#10 overgeslagen: no_merge_commit\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if mrGit(t, r.clone, nil, "rev-parse", "origin/main") == before {
		t.Error("origin/main did not move: the run did not fetch")
	}
	if r.fake.searches != 1 {
		t.Errorf("SearchIssues called %d times, want 1 per run", r.fake.searches)
	}
	if want := "label:bug created:>=2026-11-10"; r.fake.query != want {
		t.Errorf("query = %q, want %q", r.fake.query, want)
	}

	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Fatalf("second run: exit %d, stderr %q", code, errOut)
	}
	if n := r.count(`SELECT count(*) FROM recoveries`); n != 2 {
		t.Errorf("recoveries = %d after two runs, want 2", n)
	}
	if n := r.count(`SELECT count(*) FROM recovery_items`); n != 1 {
		t.Errorf("recovery_items = %d after two runs, want 1", n)
	}
}

func TestMeasureRecoveryBugIssueCounts(t *testing.T) {
	r := newMRRig(t)
	r.fake.issues = []forge.Issue{{Number: 30, Title: "stuk door #7", Labels: []string{"bug"}, CreatedAt: mrMerge7.Add(48 * time.Hour)}}
	if code, out, errOut := r.run(r.base()...); code != 0 || !strings.Contains(out, "#7 recovered 2 items") {
		t.Fatalf("exit %d, out %q, stderr %q; want the fix and the bug issue", code, out, errOut)
	}
}

func TestMeasureRecoveryWithoutFixInWindowIsOpen(t *testing.T) {
	r := newMRRig(t)
	r.now = mrMerge7.Add(2 * time.Hour) // the fix of the rig is a day later than this clock
	code, out, errOut := r.run(r.base()...)
	if code != 0 || !strings.Contains(out, "#7 open 1 items") {
		t.Fatalf("exit %d, out %q, stderr %q; want open", code, out, errOut)
	}
}

func TestMeasureRecoveryCountsFixPRRounds(t *testing.T) {
	r := newMRRig(t)
	db, err := sql.Open("sqlite", r.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt, started_at)
		VALUES ('o/r', 3, 's', 'start', 'fixpr', 'fixpr:7:abc', 1, ?)`,
		mrMerge7.Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := r.run(r.base()...); !strings.Contains(out, "#7 recovered 1 items, 1 fix-pr-rondes") {
		t.Fatalf("out %q", out)
	}
}

func TestMeasureRecoveryClosedRowCallsNoForgeSearch(t *testing.T) {
	r := newMRRig(t)
	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Fatalf("first run: exit %d, stderr %q", code, errOut)
	}
	*r.fake = mrFake{merged: r.fake.merged, heads: r.fake.heads}
	if code, out, errOut := r.run(r.base()...); code != 0 || out != "" {
		t.Fatalf("second run: exit %d, out %q, stderr %q; want silence", code, out, errOut)
	}
	if r.fake.searches != 0 || r.fake.headList != 0 {
		t.Errorf("closed row: %d searches, %d head lists, want none", r.fake.searches, r.fake.headList)
	}
}

func TestMeasureRecoveryClosesOpenRowOutsideDays(t *testing.T) {
	r := newMRRig(t)
	r.now = mrMerge7.Add(2 * time.Hour)
	if code, out, errOut := r.run(r.base()...); code != 0 || !strings.Contains(out, "#7 open") {
		t.Fatalf("first run: exit %d, out %q, stderr %q", code, out, errOut)
	}
	r.now = mrAfter
	r.fake.merged = nil // #7 is no longer in the last day
	code, out, errOut := r.run(r.base("--days", "1")...)
	if code != 0 || !strings.Contains(out, "#7 recovered 1 items") {
		t.Fatalf("exit %d, out %q, stderr %q; want the open row closed", code, out, errOut)
	}
	if n := r.count(`SELECT count(*) FROM recoveries WHERE status = 'recovered'`); n != 1 {
		t.Errorf("recovered rows = %d, want 1", n)
	}
	if r.fake.headList != 1 { // only the first run needed heads
		t.Errorf("PullHeads called %d times in total, want 1: the open row carries its issue", r.fake.headList)
	}
}

func TestMeasureRecoveryErrorsWriteNothing(t *testing.T) {
	cases := map[string]func(r *mrRig){
		"missing origin":  func(r *mrRig) { mrGit(t, r.clone, nil, "remote", "remove", "origin") },
		"merged PRs fail": func(r *mrRig) { r.fake.mergedErr = errors.New("boom") },
		"search fails":    func(r *mrRig) { r.fake.issErr = errors.New("rate limited") },
		"gitlab": func(r *mrRig) {
			if err := os.WriteFile(filepath.Join(r.clone, "lighthouse.toml"), []byte("[forge]\nrepo = \"o/r\"\nkind = \"gitlab\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newMRRig(t)
			r.fake.merged = []forge.PRSummary{{Number: 7, MergedAt: mrMerge7}, {Number: 8, MergedAt: mrMerge8}}
			r.fake.heads = append(r.fake.heads, forge.PullHead{Number: 8, HeadRef: "issue-4-tweede"})
			setup(r)
			code, _, errOut := r.run(r.base()...)
			if code != 1 || errOut == "" {
				t.Fatalf("exit %d, stderr %q; want 1 with a message", code, errOut)
			}
			if n := r.count(`SELECT count(*) FROM recoveries`); n != 0 {
				t.Errorf("recoveries = %d, want nothing written", n)
			}
		})
	}
}

func TestMeasureRecoveryUsage(t *testing.T) {
	r := newMRRig(t)
	for name, args := range map[string][]string{
		"no repo-dir": {"--db", r.db},
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
