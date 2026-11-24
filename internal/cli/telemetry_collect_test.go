package cli

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/store"
)

var tcBase = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

// tcRig is a HOME with Claude project folders, a repo with a lighthouse.toml and a database.
type tcRig struct {
	t     *testing.T
	home  string
	repo  string
	db    string
	state string
	now   time.Time
	after func()
}

func newTCRig(t *testing.T) *tcRig {
	t.Helper()
	root := t.TempDir()
	r := &tcRig{t: t, home: filepath.Join(root, "home"), repo: filepath.Join(root, "repo"),
		db: filepath.Join(root, "l.db"), state: filepath.Join(root, "state"), now: tcBase.Add(5 * time.Hour)}
	if err := os.MkdirAll(r.repo, 0o700); err != nil {
		t.Fatal(err)
	}
	mrGit(t, r.repo, nil, "init", "-q", "-b", "main")
	r.write(filepath.Join(r.repo, "lighthouse.toml"), "[forge]\nrepo = \"o/r\"\n")
	return r
}

func (r *tcRig) write(path, content string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// tcLine is a user line, or an assistant line when id is set; every line carries its cwd.
func tcLine(sid, cwd string, min int, id string) string {
	ts := tcBase.Add(time.Duration(min) * time.Minute).Format(time.RFC3339Nano)
	if id == "" {
		return fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":%q,"gitBranch":"main","timestamp":%q,"message":{"role":"user","content":"hello"}}`, sid, cwd, ts)
	}
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":%q,"gitBranch":"main","timestamp":%q,"version":"2.1.0","message":{"id":%q,"model":"claude-sonnet-5-5","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`, sid, cwd, ts, id)
}

// project writes one transcript of session sid into the project folder name under HOME.
func (r *tcRig) project(name, sid, cwd string, lines ...string) string {
	path := filepath.Join(r.home, ".claude", "projects", name, sid+".jsonl")
	r.write(path, strings.Join(lines, "\n")+"\n")
	return path
}

func (r *tcRig) env() collectEnv {
	return collectEnv{
		Getenv: func(k string) string {
			switch k {
			case "HOME":
				return r.home
			case "LIGHTHOUSE_STATE_DIR":
				return r.state
			}
			return ""
		},
		Now: func() time.Time { return r.now },
		AfterMeasure: func() {
			if r.after != nil {
				r.after()
			}
		},
	}
}

func (r *tcRig) run(args ...string) (int, string, string) {
	r.t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newTelemetryCommand(r.env()))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"telemetry", "collect"}, args...))
	return execute(root, &errOut), out.String(), errOut.String()
}

func (r *tcRig) base(extra ...string) []string {
	return append([]string{"--repo-dir", r.repo, "--db", r.db}, extra...)
}

func (r *tcRig) query(q string, args ...any) []string {
	r.t.Helper()
	db, err := sql.Open("sqlite", r.db)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(q, args...)
	if err != nil {
		r.t.Fatalf("%s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, s.String)
	}
	sort.Strings(out)
	return out
}

func TestCollectFindsWorktreeTranscripts(t *testing.T) {
	r := newTCRig(t)
	root := filepath.Dir(r.repo)
	linked := filepath.Join(root, "linked-wt")
	mrGit(t, r.repo, nil, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
	mrGit(t, r.repo, nil, "worktree", "add", "-q", "-b", "fix-x", linked)
	removed := filepath.Join(root, "worktrees", "github", "github.com", "O", "R", "work-issue-9-x")
	cases := []struct{ dir, sid, cwd string }{
		{"-repo", "s-repo", filepath.Join(r.repo, "internal")},
		{"-repo--claude-worktrees-a", "s-root", filepath.Join(r.repo, ".claude", "worktrees", "a")},
		{"-linked", "s-linked", linked},    // a git worktree outside the worktree root
		{"plain-name", "s-forge", removed}, // a removed kenn-forge worktree, no name prefix
		{"-other", "s-other", filepath.Join(root, "elsewhere", "other-project")},
		{"-lookalike", "s-look", filepath.Join(root, "x", "o", "other", "r")}, // o and r not contiguous
	}
	for _, c := range cases {
		r.project(c.dir, c.sid, c.cwd, tcLine(c.sid, c.cwd, 0, ""), tcLine(c.sid, c.cwd, 1, "m-"+c.sid))
	}
	code, out, errOut := r.run(r.base()...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errOut, out)
	}
	got := r.query(`SELECT session_key FROM sessions`)
	want := []string{"s-forge", "s-linked", "s-repo", "s-root"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sessions = %v, want %v\n%s", got, want, out)
	}
	if strings.Contains(out, "s-other") || strings.Contains(out, "s-look") {
		t.Errorf("an unrelated transcript was collected:\n%s", out)
	}
}

func TestCollectExitCodes(t *testing.T) {
	r := newTCRig(t)
	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Errorf("clean run: exit %d, stderr %q", code, errOut)
	}
	if code, _, _ := r.run(append(r.base(), "extra")...); code != 2 {
		t.Errorf("argument: exit %d, want 2", code)
	}
	if code, _, _ := r.run(append(r.base(), "--no-such-flag")...); code != 2 {
		t.Errorf("unknown flag: exit %d, want 2", code)
	}
	r.write(filepath.Join(r.repo, "lighthouse.toml"), "[forge\nrepo = ")
	if code, _, _ := r.run(r.base()...); code != 1 {
		t.Errorf("broken lighthouse.toml: exit %d, want 1", code)
	}
}

func TestCollectPrintsSkipReason(t *testing.T) {
	r := newTCRig(t)
	// A transcript with a cwd but no user or assistant line: nothing to store.
	path := r.project("-repo", "s-bad", r.repo, fmt.Sprintf(`{"type":"summary","cwd":%q}`, r.repo), "not json {")
	code, out, errOut := r.run(r.base()...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if want := "skip " + path + ": transcript has no lines\n"; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if n := r.query(`SELECT count(*) FROM sessions`); n[0] != "0" {
		t.Errorf("sessions = %v, want none", n)
	}
	if n := r.query(`SELECT count(*) FROM setups`); n[0] != "0" {
		t.Errorf("setups = %v, want none", n)
	}
}

func TestCollectDryRunLeavesOldSchemaAlone(t *testing.T) {
	r := newTCRig(t)
	r.project("-repo", "s1", r.repo, tcLine("s1", r.repo, 0, ""), tcLine("s1", r.repo, 1, "m1"))
	s, err := store.OpenShared(context.Background(), r.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`PRAGMA user_version = 10`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	snapshot := func() string {
		var b strings.Builder
		ents, _ := os.ReadDir(filepath.Dir(r.db))
		for _, e := range ents {
			data, _ := os.ReadFile(filepath.Join(filepath.Dir(r.db), e.Name()))
			fmt.Fprintf(&b, "%s %x\n", e.Name(), data)
		}
		return b.String()
	}
	before := snapshot()
	code, out, errOut := r.run(r.base("--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errOut, out)
	}
	if v := r.query(`PRAGMA user_version`); v[0] != "10" {
		t.Errorf("user_version = %v after --dry-run, want 10", v)
	}
	if after := snapshot(); after != before {
		t.Errorf("the database directory changed under --dry-run")
	}
	if _, err := os.Stat(r.state); err == nil {
		t.Errorf("--dry-run created the state directory %s (a sidecar)", r.state)
	}
	if !strings.Contains(out, "s1.jsonl") {
		t.Errorf("--dry-run did not name the transcript it found:\n%s", out)
	}
}

func TestCollectDryRunWithoutDatabase(t *testing.T) {
	r := newTCRig(t)
	r.project("-repo", "s1", r.repo, tcLine("s1", r.repo, 0, ""), tcLine("s1", r.repo, 1, "m1"))
	code, out, errOut := r.run(r.base("--dry-run")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "does not exist") || !strings.Contains(out, "s1.jsonl") {
		t.Errorf("output lacks the missing database or the found transcript:\n%s", out)
	}
	if _, err := os.Stat(r.db); err == nil {
		t.Error("--dry-run created the database")
	}
}

func TestCollectDryRunWritesNothing(t *testing.T) {
	r := newTCRig(t)
	r.project("-repo", "s1", r.repo, tcLine("s1", r.repo, 0, ""), tcLine("s1", r.repo, 1, "m1"))
	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Fatalf("first pass: exit %d, %q", code, errOut)
	}
	r.project("-repo", "s2", r.repo, tcLine("s2", r.repo, 0, ""), tcLine("s2", r.repo, 1, "m2"))
	if code, out, errOut := r.run(r.base("--dry-run")...); code != 0 || !strings.Contains(out, "s2.jsonl") {
		t.Fatalf("dry run: exit %d, %q, %q", code, out, errOut)
	}
	if got := r.query(`SELECT session_key FROM sessions`); strings.Join(got, ",") != "s1" {
		t.Errorf("sessions = %v after a dry run, want only s1", got)
	}
}

func TestCollectGrowingTranscript(t *testing.T) {
	r := newTCRig(t)
	path := r.project("-repo", "s1", r.repo, tcLine("s1", r.repo, 0, ""), tcLine("s1", r.repo, 1, "m1"))
	// The file grows after the size is measured: this pass leaves the new lines, the next takes them.
	r.after = func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = f.WriteString(tcLine("s1", r.repo, 2, "m2") + "\n")
		_ = f.Close()
		r.after = nil
	}
	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Fatalf("first pass: exit %d, %q", code, errOut)
	}
	if got := r.query(`SELECT output_tokens FROM usage`); strings.Join(got, ",") != "5" {
		t.Errorf("output_tokens after the first pass = %v, want one message (5)", got)
	}
	if got := r.query(`SELECT recorded_at FROM usage`); got[0] != r.now.Format(time.RFC3339Nano) {
		t.Errorf("recorded_at = %v, want the moment before reading %s", got, r.now.Format(time.RFC3339Nano))
	}
	r.now = r.now.Add(time.Minute)
	if code, _, errOut := r.run(r.base()...); code != 0 {
		t.Fatalf("second pass: exit %d, %q", code, errOut)
	}
	if got := r.query(`SELECT output_tokens FROM usage`); strings.Join(got, ",") != "10" {
		t.Errorf("output_tokens after the second pass = %v, want both messages (10)", got)
	}
}
