package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/telemetry"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
)

var verSeq int

// seedSuperpowers inserts a standard run that started m minutes after t0, with a setups row
// carrying superpowers version v; it returns the run id.
func seedSuperpowers(t *testing.T, db *sql.DB, m int, v string) int64 {
	t.Helper()
	verSeq++
	at := t0.Add(time.Duration(m) * time.Minute).UTC().Format(time.RFC3339Nano)
	res, err := db.Exec(`INSERT INTO runs (repo, issue, session_id, mode, trigger, trigger_key, attempt,
		assignment, started_at) VALUES ('o/r', ?, ?, 'start', 'handoff', ?, 1, 'standard', ?)`, verSeq, verSeq, verSeq, at)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO setups (source, source_id, recorded_at, run_id, superpowers_version)
		VALUES ('t', ?, ?, ?, ?)`, verSeq, at, id, v); err != nil {
		t.Fatalf("insert setup: %v", err)
	}
	return id
}

// proposeFixture is a fake forge with a repo o/r in language, a database and an env whose clock
// is now.
type proposeFixture struct {
	t     *testing.T
	env   tickEnv
	db    *sql.DB
	srv   *forgetest.Server
	repos []repoconfig.Repo
	tick  map[string]forge.Forge
	now   time.Time
}

func newProposeFixture(t *testing.T, language string) *proposeFixture {
	t.Helper()
	env, dbPath := tickFixture(t)
	srv := fakeForge(t, &env)
	repo := repoconfig.Repo{Slug: "o/r", Language: language}
	f := &proposeFixture{t: t, env: env, db: openStore(t, dbPath), srv: srv, repos: []repoconfig.Repo{repo},
		tick: map[string]forge.Forge{"o/r": botForge(t, env, repo).Forge}, now: t0}
	f.env.Now = func() time.Time { return f.now }
	return f
}

func (f *proposeFixture) propose(slug string) (bool, string) {
	f.t.Helper()
	var errOut bytes.Buffer
	failed := proposeVersionChanges(context.Background(), f.env, f.db, hostconfig.Telemetry{ProposalsRepo: slug}, f.repos, f.tick, &errOut)
	return failed, errOut.String()
}

func (f *proposeFixture) issues() []map[string]any {
	var out []map[string]any
	for n := 1; n < 100; n++ {
		if it, ok := f.srv.Issues[n]; ok {
			out = append(out, it)
		}
	}
	return out
}

func (f *proposeFixture) existing(n int, state, body string) {
	if f.srv.Issues == nil {
		f.srv.Issues = map[int]map[string]any{}
	}
	f.srv.Issues[n] = map[string]any{"number": n, "title": "old", "body": body, "state": state}
}

func TestVersionMarker_FullComment(t *testing.T) {
	cases := []struct {
		c    telemetry.VersionChange
		want string
	}{
		{telemetry.VersionChange{Dimension: telemetry.DimSuperpowers, New: "7.0.2"}, "<!-- lighthouse-version-change: superpowers →7.0.2 -->"},
		{telemetry.VersionChange{Dimension: telemetry.DimHarness, Series: "codex", New: "0.6"}, "<!-- lighthouse-version-change: harness codex →0.6 -->"},
		{telemetry.VersionChange{Dimension: telemetry.DimModel, Series: "native", New: "claude-sonnet-5-5"}, "<!-- lighthouse-version-change: model native →claude-sonnet-5-5 -->"},
	}
	for _, c := range cases {
		if got := versionMarker(c.c); got != c.want {
			t.Errorf("versionMarker = %q, want %q", got, c.want)
		}
	}
}

func TestPropose_OneIssueThenNothing(t *testing.T) {
	f := newProposeFixture(t, "en")
	seedSuperpowers(t, f.db, 0, "7.0.1")
	first := seedSuperpowers(t, f.db, 5, "7.0.2")
	if failed, out := f.propose("o/r"); failed || out != "" {
		t.Fatalf("failed = %v, log = %q", failed, out)
	}
	got := f.issues()
	if len(got) != 1 {
		t.Fatalf("issues = %d, want 1", len(got))
	}
	body := fmt.Sprint(got[0]["body"])
	for _, want := range []string{"<!-- lighthouse-version-change: superpowers →7.0.2 -->", "7.0.1 → 7.0.2",
		fmt.Sprintf("run %d", first), "task_review", "final_review", "2026-11-24-issue-388-explore-exploit-design.md", "#388"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	// An hour later, a run on the same version is no change, and the issue stands.
	seedSuperpowers(t, f.db, 90, "7.0.2")
	f.now = t0.Add(time.Hour)
	if failed, out := f.propose("o/r"); failed || out != "" {
		t.Fatalf("second: failed = %v, log = %q", failed, out)
	}
	if n := len(f.issues()); n != 1 {
		t.Errorf("issues = %d after a run on the same version, want 1", n)
	}
	// And a second look with the marker present creates nothing either.
	f.now = t0.Add(3 * time.Hour)
	f.propose("o/r")
	if n := len(f.issues()); n != 1 {
		t.Errorf("issues = %d on a third look, want 1", n)
	}
}

func TestPropose_PrefixVersionNotFound(t *testing.T) {
	f := newProposeFixture(t, "en")
	f.existing(1, "open", "<!-- lighthouse-version-change: superpowers →7.0.20 -->")
	seedSuperpowers(t, f.db, 0, "7.0.1")
	seedSuperpowers(t, f.db, 5, "7.0.2")
	f.propose("o/r")
	if n := len(f.issues()); n != 2 {
		t.Errorf("issues = %d, want the existing one and a new one", n)
	}
}

func TestPropose_ClosedProposalNotRecreated(t *testing.T) {
	f := newProposeFixture(t, "en")
	f.existing(1, "closed", "x\n<!-- lighthouse-version-change: superpowers →7.0.2 -->\n")
	seedSuperpowers(t, f.db, 0, "7.0.1")
	seedSuperpowers(t, f.db, 5, "7.0.2")
	f.propose("o/r")
	if n := len(f.issues()); n != 1 {
		t.Errorf("issues = %d, want only the closed one", n)
	}
}

func TestPropose_LateRowNoSecondProposal(t *testing.T) {
	f := newProposeFixture(t, "en")
	f.existing(1, "open", "<!-- lighthouse-version-change: superpowers →B -->")
	seedSuperpowers(t, f.db, 0, "A")
	seedSuperpowers(t, f.db, 20, "B")
	// A row that arrives late, started between A and B, with another value.
	seedSuperpowers(t, f.db, 10, "X")
	f.propose("o/r")
	if n := len(f.issues()); n != 1 {
		t.Errorf("issues = %d, want only the existing one", n)
	}
}

func TestPropose_OnlyLatestPerSeries(t *testing.T) {
	f := newProposeFixture(t, "en")
	for i, v := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0"} {
		seedSuperpowers(t, f.db, i*5, v)
	}
	f.propose("o/r")
	got := f.issues()
	if len(got) != 1 || !strings.Contains(fmt.Sprint(got[0]["body"]), "→1.3.0 -->") {
		t.Errorf("issues = %v, want one, for 1.3.0", got)
	}
}

func TestPropose_HourGate(t *testing.T) {
	f := newProposeFixture(t, "en")
	seedSuperpowers(t, f.db, 0, "1.0.0")
	seedSuperpowers(t, f.db, 5, "1.1.0")
	f.srv.Fail = map[string]int{"GET /repos/o/r/issues": 500}
	failed, out := f.propose("o/r")
	if !failed || strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "lighthouse: version change: ") {
		t.Fatalf("failed = %v, log = %q, want failed and one line", failed, out)
	}
	calls := f.srv.Count("/repos/o/r/issues")
	if calls == 0 {
		t.Fatal("the forge was not asked")
	}
	stateDir, err := collect.StateDir(f.env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "telemetry", "last-version-check")); err != nil {
		t.Errorf("time file after a failed look: %v", err)
	}
	f.now = t0.Add(59 * time.Minute)
	if failed, out := f.propose("o/r"); failed || out != "" {
		t.Errorf("within the hour: failed = %v, log = %q", failed, out)
	}
	if got := f.srv.Count("/repos/o/r/issues"); got != calls {
		t.Errorf("forge calls within the hour: %d, want %d", got, calls)
	}
	f.now = t0.Add(61 * time.Minute)
	f.propose("o/r")
	if got := f.srv.Count("/repos/o/r/issues"); got == calls {
		t.Errorf("no forge call after the hour")
	}
}

func TestPropose_NoProposalsRepoOrNotDispatchRepo(t *testing.T) {
	for name, slug := range map[string]string{"unset": "", "other": "x/y"} {
		t.Run(name, func(t *testing.T) {
			f := newProposeFixture(t, "en")
			seedSuperpowers(t, f.db, 0, "1.0.0")
			seedSuperpowers(t, f.db, 5, "1.1.0")
			failed, out := f.propose(slug)
			if failed || strings.Count(out, "\n") != 1 || !strings.Contains(out, "proposals_repo") {
				t.Errorf("failed = %v, log = %q, want one line naming proposals_repo", failed, out)
			}
			if len(f.srv.Posts) != 0 || len(f.issues()) != 0 {
				t.Errorf("wrote to the forge: %v", f.srv.Posts)
			}
		})
	}
}

func TestPropose_ForgeNotOpenThisTickFails(t *testing.T) {
	f := newProposeFixture(t, "en")
	f.tick = map[string]forge.Forge{}
	failed, out := f.propose("o/r")
	if !failed || strings.Count(out, "\n") != 1 {
		t.Errorf("failed = %v, log = %q, want failed and one line", failed, out)
	}
}

func TestPropose_SkipAndNoComponent(t *testing.T) {
	f := newProposeFixture(t, "en")
	seedSuperpowers(t, f.db, 0, "7.0.2")
	seedSuperpowers(t, f.db, 5, "7.0.1") // older than seen: a Skip
	if failed, out := f.propose("o/r"); failed || out != "" {
		t.Fatalf("failed = %v, log = %q", failed, out)
	}
	if n := len(f.issues()); n != 0 {
		t.Errorf("issues = %d, want none for a Skip", n)
	}
}

func TestPropose_Language(t *testing.T) {
	for language, want := range map[string]string{"nl": "Versiewissel: superpowers 1.0.0 → 1.1.0", "en": "Version change: superpowers 1.0.0 → 1.1.0"} {
		t.Run(language, func(t *testing.T) {
			f := newProposeFixture(t, language)
			seedSuperpowers(t, f.db, 0, "1.0.0")
			seedSuperpowers(t, f.db, 5, "1.1.0")
			f.propose("o/r")
			got := f.issues()
			if len(got) != 1 || got[0]["title"] != want {
				t.Fatalf("issues = %v, want title %q", got, want)
			}
			body := fmt.Sprint(got[0]["body"])
			if nl := strings.Contains(body, "start de meting niet zelf"); nl != (language == "nl") {
				t.Errorf("body language wrong:\n%s", body)
			}
		})
	}
}

// A forge that fails for the proposals leaves the rest of the tick running: the collection came
// first, the sweep still removes the workspace, and the tick ends red.
func TestTickVersionStepFailureDoesNotStopTick(t *testing.T) {
	env, db, w := rawTick(t)
	sid := runs.NewSessionID()
	closedRun(t, db, sid)
	w.raw[sid] = mainTranscript(sid, 5)
	conn := openStore(t, db)
	seedSuperpowers(t, conn, -120, "1.0.0")
	seedSuperpowers(t, conn, -100, "1.1.0")
	srv := fakeForge(t, &env)
	srv.Fail = map[string]int{"GET /repos/o/r/issues": 500}

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lighthouse: version change: ") {
		t.Errorf("errOut = %q, want a version change line", errOut.String())
	}
	if ev := w.eventLog(); len(ev) != 2 || ev[1] != "remove "+ws945 {
		t.Errorf("events = %v, want the sweep to remove %s after the failed step", ev, ws945)
	}
}
