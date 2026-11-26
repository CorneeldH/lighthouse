// Package recoverytest is test support for packages that exercise the recovery measurement: a
// narrow fake forge and a rig of a bare origin, a clone and a database. It is not production
// behaviour: nothing outside _test.go files imports it.
package recoverytest

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// The merge times of the rig and the clock after both.
var (
	Merge7 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	Merge8 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	After  = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
)

// Forge is a narrow fake forge; it counts the calls that cost API budget. It embeds forge.Forge
// only so it can stand where a full forge is asked for: a method outside recovery.Forge panics.
type Forge struct {
	forge.Forge

	Merged            []forge.PRSummary
	Heads             []forge.PullHead
	Issues            []forge.Issue
	MergedErr, IssErr error
	// Since and Until are the span of the last MergedPRs call.
	Since, Until       time.Time
	Searches, HeadList int
	Query              string
	// Traits: IssueLabels and IssueComments are served for every issue; IssueErr fails one issue.
	IssueLabels   []string
	IssueComments []protocol.Comment
	IssueErr      map[int]error
	IssueCalls    int
	CommCalls     int
}

// Issue serves issue n with IssueLabels, or its IssueErr.
func (f *Forge) Issue(_ context.Context, n int) (forge.Issue, error) {
	f.IssueCalls++
	if err := f.IssueErr[n]; err != nil {
		return forge.Issue{}, err
	}
	return forge.Issue{Number: n, Labels: f.IssueLabels}, nil
}

// Comments serves IssueComments for every issue.
func (f *Forge) Comments(context.Context, int) ([]protocol.Comment, error) {
	f.CommCalls++
	return f.IssueComments, nil
}

// MergedPRs records the span and serves Merged.
func (f *Forge) MergedPRs(_ context.Context, since, until time.Time) ([]forge.PRSummary, error) {
	f.Since, f.Until = since, until
	return f.Merged, f.MergedErr
}

// PullHeads serves Heads.
func (f *Forge) PullHeads(context.Context) ([]forge.PullHead, error) {
	f.HeadList++
	return f.Heads, nil
}

// SearchIssues records the query and serves Issues.
func (f *Forge) SearchIssues(_ context.Context, text string) ([]forge.Issue, error) {
	f.Searches++
	f.Query = text
	return f.Issues, f.IssErr
}

// Triage is a triage comment of gate, posted before every merge of the rig.
func Triage(gate string) []protocol.Comment {
	return []protocol.Comment{{
		Body:      protocol.RenderTriageMarker(protocol.TriageMarker{Gate: gate}) + "\nroute",
		CreatedAt: Merge7.Add(-48 * time.Hour).Format(time.RFC3339),
	}}
}

// Git runs git in dir with extra environment and returns its trimmed output.
func Git(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file, subject string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(subject+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := at.Format(time.RFC3339)
	env := []string{"GIT_AUTHOR_DATE=" + d, "GIT_COMMITTER_DATE=" + d}
	Git(t, dir, env, "add", file)
	Git(t, dir, env, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", subject)
}

// Rig is a bare origin, a clone of it with a lighthouse.toml for o/r, a migrated database and
// a fake forge that serves #7 (head issue-3-eerste) as merged.
type Rig struct {
	t      *testing.T
	Origin string
	Clone  string // the repo the measurement runs in
	DB     string // path of the database
	Fake   *Forge
}

// NewRig builds a bare origin and a clone that holds only the merge commit of #7, and then adds
// the fix of #7 and the merge commit of #8 to origin alone: a measurement must fetch them.
func NewRig(t *testing.T) *Rig {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	r := &Rig{t: t, Origin: filepath.Join(root, "origin.git"), Clone: filepath.Join(root, "clone"),
		DB: filepath.Join(root, "l.db")}
	Git(t, root, nil, "init", "-q", "--bare", "-b", "main", r.Origin)
	Git(t, root, nil, "init", "-q", "-b", "main", work)
	Git(t, work, nil, "remote", "add", "origin", r.Origin)
	commit(t, work, "a.txt", "feat(a): maak a (#7)", Merge7)
	Git(t, work, nil, "push", "-q", "origin", "main")
	Git(t, root, nil, "clone", "-q", r.Origin, r.Clone)
	commit(t, work, "a.txt", "fix(a): herstel a", Merge7.Add(24*time.Hour))
	commit(t, work, "b.txt", "feat(b): maak b (#8)", Merge8)
	Git(t, work, nil, "push", "-q", "origin", "main")
	r.WriteConfig("[forge]\nrepo = \"o/r\"\n")
	s, err := store.OpenShared(context.Background(), r.DB)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	r.Fake = &Forge{
		Merged:        []forge.PRSummary{{Number: 7, MergedAt: Merge7}},
		Heads:         []forge.PullHead{{Number: 7, HeadRef: "issue-3-eerste"}},
		IssueComments: Triage("2"),
	}
	return r
}

// WriteConfig replaces the lighthouse.toml of the clone.
func (r *Rig) WriteConfig(toml string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.Clone, "lighthouse.toml"), []byte(toml), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// WithAreaPrefix gives the clone a config with area_prefix "2-area:".
func (r *Rig) WithAreaPrefix() {
	r.WriteConfig("[forge]\nrepo = \"o/r\"\n\n[labels]\narea_prefix = \"2-area:\"\n")
}

func (r *Rig) open() *sql.DB {
	r.t.Helper()
	db, err := sql.Open("sqlite", r.DB)
	if err != nil {
		r.t.Fatal(err)
	}
	return db
}

// Count runs a query that returns one number.
func (r *Rig) Count(query string) int {
	r.t.Helper()
	db := r.open()
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// Exec runs a statement on the database.
func (r *Rig) Exec(query string, args ...any) {
	r.t.Helper()
	db := r.open()
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(query, args...); err != nil {
		r.t.Fatal(err)
	}
}

// Text runs a query that returns one text value; NULL is "".
func (r *Rig) Text(query string) string {
	r.t.Helper()
	db := r.open()
	defer func() { _ = db.Close() }()
	var v sql.NullString
	if err := db.QueryRow(query).Scan(&v); err != nil {
		r.t.Fatal(err)
	}
	return v.String
}
