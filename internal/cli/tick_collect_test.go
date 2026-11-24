package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// rawWorkplace is a fakeWorkplace that also has RawTranscripts: the whole files per session id
// from raw, or the error in errs. events logs "raw <workspace>" and "remove <workspace>" in order.
type rawWorkplace struct {
	*fakeWorkplace
	rmu    sync.Mutex
	raw    map[string][]workplace.FileDelta
	errs   map[string]error
	events []string
}

var _ workplace.RawTranscripts = (*rawWorkplace)(nil)

func (w *rawWorkplace) RawTranscript(_ context.Context, workspaceID, sessionID string) ([]workplace.FileDelta, error) {
	w.rmu.Lock()
	defer w.rmu.Unlock()
	w.events = append(w.events, "raw "+workspaceID)
	if err := w.errs[sessionID]; err != nil {
		return nil, err
	}
	return w.raw[sessionID], nil
}

func (w *rawWorkplace) Remove(ctx context.Context, workspaceID string, o workplace.RemoveOptions) error {
	w.rmu.Lock()
	w.events = append(w.events, "remove "+workspaceID)
	w.rmu.Unlock()
	return w.fakeWorkplace.Remove(ctx, workspaceID, o)
}

func (w *rawWorkplace) eventLog() []string {
	w.rmu.Lock()
	defer w.rmu.Unlock()
	return slices.Clone(w.events)
}

// rawTick is #945 with a rawWorkplace for o/r.
func rawTick(t *testing.T) (tickEnv, string, *rawWorkplace) {
	t.Helper()
	env, db := tickFixture(t, repo945(t))
	_, fw := sweepable(t, &env, "p1", &gitState{ahead: "0"})
	w := &rawWorkplace{fakeWorkplace: fw, raw: map[string][]workplace.FileDelta{}, errs: map[string]error{}}
	env.Workplace = func(repoconfig.Repo, string, string) workplace.Workplace { return w }
	return env, db, w
}

// mainTranscript is a transcript of session sid: a user line and one assistant line with out
// output tokens, at t0-50m and t0-49m.
func mainTranscript(sid string, out int) []workplace.FileDelta {
	data := fmt.Sprintf(`{"type":"user","sessionId":%q,"timestamp":%q,"message":{"role":"user","content":"go"}}`+"\n"+
		`{"type":"assistant","sessionId":%q,"timestamp":%q,"version":"2.1.0","message":{"id":"m1","model":"claude-sonnet-5-5","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":10,"output_tokens":%d,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`+"\n",
		sid, t0.Add(-50*time.Minute).Format(time.RFC3339Nano), sid, t0.Add(-49*time.Minute).Format(time.RFC3339Nano), out)
	return []workplace.FileDelta{{Name: sid + ".jsonl", Size: int64(len(data)), Data: []byte(data)}}
}

// closedRun is a done run of o/r#945 on ws945 with session sid, started an hour before t0.
func closedRun(t *testing.T, db, sid string) int64 {
	t.Helper()
	ctx := context.Background()
	conn := openStore(t, db)
	r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 945, SessionID: sid, Mode: "start",
		Trigger: "handoff", TriggerKey: "start:945", Now: t0.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, r.ID, ws945, branch945); err != nil {
		t.Fatal(err)
	}
	if err := runs.Release(ctx, conn, r.ID, runs.Done, "", t0.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func countSetups(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM setups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTickCollectsAtMostHourly(t *testing.T) {
	env, db, w := rawTick(t)
	ctx := context.Background()
	conn := openStore(t, db)
	sid := runs.NewSessionID()
	r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 945, SessionID: sid, Mode: "start",
		Trigger: "handoff", TriggerKey: "start:945", Now: t0.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, r.ID, ws945, branch945); err != nil {
		t.Fatal(err)
	}
	if err := runs.SetSession(ctx, conn, r.ID, "k-945"); err != nil {
		t.Fatal(err)
	}
	w.sessions[ws945] = []workplace.Session{{Key: "k-945", WorkspaceID: ws945, State: "working", SessionID: sid}}
	w.raw[sid] = mainTranscript(sid, 5)

	now := t0
	env.Now = func() time.Time { return now }
	raws := func() int {
		n := 0
		for _, e := range w.eventLog() {
			if strings.HasPrefix(e, "raw ") {
				n++
			}
		}
		return n
	}
	var errOut bytes.Buffer
	for i, step := range []struct {
		at   time.Duration
		want int
	}{{0, 1}, {30 * time.Minute, 1}, {61 * time.Minute, 2}} {
		now = t0.Add(step.at)
		errOut.Reset()
		if err := tick(ctx, env, db, &errOut); err != nil {
			t.Fatalf("tick %d = %v\n%s", i+1, err, errOut.String())
		}
		if got := raws(); got != step.want {
			t.Fatalf("after tick %d: %d collections, want %d\n%s", i+1, got, step.want, errOut.String())
		}
	}
	if n := countSetups(t, conn); n != 1 {
		t.Errorf("setups = %d, want 1 (a second collection updates the row)", n)
	}
	stateDir, err := collect.StateDir(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "telemetry", "last-collect")); err != nil {
		t.Errorf("the hour boundary is not on disk: %v", err)
	}
}

func TestTickCollectErrorDoesNotStopTick(t *testing.T) {
	env, db, w := rawTick(t)
	sid := runs.NewSessionID()
	id := closedRun(t, db, sid)
	// As a real node error: not measurable and unanswered at once (nodeTranscript, serve()).
	w.errs[sid] = fmt.Errorf("transcript of %s on node n1: down: %w: %w", sid, workplace.ErrNotMeasurable, workplace.ErrUnanswered)

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if want := fmt.Sprintf("lighthouse: collect o/r: run %d: ", id); !strings.Contains(errOut.String(), want) ||
		!strings.Contains(errOut.String(), "down") {
		t.Errorf("errOut = %q, want a collect line with the run id and the reason", errOut.String())
	}
	if _, _, removed := w.calls(); !slices.Equal(removed, []string{ws945}) {
		t.Errorf("removed = %v, want the sweep to go on and remove %s", removed, ws945)
	}
}

func TestTickCollectUnreachableClosedRunIsNoFailure(t *testing.T) {
	for name, cause := range map[string]error{
		"gone":      fmt.Errorf("read: %w", workplace.ErrGone),
		"not exist": fmt.Errorf("transcript: %w", fs.ErrNotExist),
	} {
		t.Run(name, func(t *testing.T) {
			env, db, w := rawTick(t)
			sid := runs.NewSessionID()
			id := closedRun(t, db, sid)
			w.errs[sid] = cause
			var errOut bytes.Buffer
			if err := tick(context.Background(), env, db, &errOut); err != nil {
				t.Fatalf("tick = %v, want nil\n%s", err, errOut.String())
			}
			if want := fmt.Sprintf("lighthouse: collect o/r: run %d: unreachable, not processed", id); !strings.Contains(errOut.String(), want) {
				t.Errorf("errOut = %q, want %q", errOut.String(), want)
			}
		})
	}
}

func TestTickRunTranscriptViaAdapterNotHook(t *testing.T) {
	env, db, w := rawTick(t)
	sid := runs.NewSessionID()
	closedRun(t, db, sid)
	w.raw[sid] = mainTranscript(sid, 7)
	// A transcript on the file system of the host, with other numbers: it must not be read.
	home := t.TempDir()
	decoy := filepath.Join(home, ".claude", "projects", "-work-"+ws945, sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(decoy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decoy, mainTranscript(sid, 9999)[0].Data, 0o644); err != nil {
		t.Fatal(err)
	}
	base := env.Getenv
	env.Getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return base(k)
	}

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	var out int
	if err := openStore(t, db).QueryRow(`SELECT output_tokens FROM usage`).Scan(&out); err != nil {
		t.Fatalf("no usage row: %v\n%s", err, errOut.String())
	}
	if out != 7 {
		t.Errorf("output_tokens = %d, want 7 from RawTranscript", out)
	}
	if !slices.Contains(w.eventLog(), "raw "+ws945) {
		t.Errorf("events = %v, want a RawTranscript call", w.eventLog())
	}
}

// A run that closed is collected every tick until it has a setups row after ended_at, and
// before the sweep removes its workspace, also when the hour boundary has not passed.
func TestTickCollectsBeforeSweep(t *testing.T) {
	env, db, w := rawTick(t)
	sid := runs.NewSessionID()
	closedRun(t, db, sid)
	w.raw[sid] = mainTranscript(sid, 5)
	stateDir, err := collect.StateDir(env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	last := filepath.Join(stateDir, "telemetry", "last-collect")
	if err := os.MkdirAll(filepath.Dir(last), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(last, []byte(t0.Add(-time.Minute).Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v\n%s", err, errOut.String())
	}
	if want := []string{"raw " + ws945, "remove " + ws945}; !slices.Equal(w.eventLog(), want) {
		t.Errorf("events = %v, want %v: collect first, then sweep", w.eventLog(), want)
	}
	if n := countSetups(t, openStore(t, db)); n != 1 {
		t.Errorf("setups = %d, want 1", n)
	}
}

func TestTickSkipsRemovedWorkspaceRuns(t *testing.T) {
	env, db, w := rawTick(t)
	sid := runs.NewSessionID()
	closedRun(t, db, sid)
	w.errs[sid] = errors.New("must not be asked")
	if err := runs.MarkWorkspaceRemoved(context.Background(), openStore(t, db), "o/r", "", ws945, t0); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); err != nil {
		t.Fatalf("tick = %v, want nil\n%s", err, errOut.String())
	}
	if ev := w.eventLog(); len(ev) != 0 {
		t.Errorf("events = %v, want no call for a run whose workspace is removed", ev)
	}
}

// ErrUnanswered alone is a failure too, as is any error that is not about the transcript missing.
func TestTickCollectUnansweredAloneIsFailure(t *testing.T) {
	env, db, w := rawTick(t)
	sid := runs.NewSessionID()
	closedRun(t, db, sid)
	w.errs[sid] = fmt.Errorf("node n1 did not answer: %w", workplace.ErrUnanswered)
	var errOut bytes.Buffer
	if err := tick(context.Background(), env, db, &errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
}

// openRunTick is rawTick with an open run of ws945 (an hour old) and its error cause.
func openRunTick(t *testing.T, cause error) (tickEnv, string, *bytes.Buffer) {
	t.Helper()
	env, db, w := rawTick(t)
	ctx := context.Background()
	conn := openStore(t, db)
	sid := runs.NewSessionID()
	r, err := runs.Take(ctx, conn, runs.Claim{Repo: "o/r", Issue: 945, SessionID: sid, Mode: "start",
		Trigger: "handoff", TriggerKey: "start:945", Now: t0.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.SetWorkplace(ctx, conn, r.ID, ws945, branch945); err != nil {
		t.Fatal(err)
	}
	if err := runs.SetSession(ctx, conn, r.ID, "k-945"); err != nil {
		t.Fatal(err)
	}
	w.sessions[ws945] = []workplace.Session{{Key: "k-945", WorkspaceID: ws945, State: "working", SessionID: sid}}
	w.errs[sid] = cause
	return env, db, &bytes.Buffer{}
}

// A freshly started run has no transcript yet: one line, no failure.
func TestTickCollectOpenRunUnreachableIsNoFailure(t *testing.T) {
	for name, cause := range map[string]error{
		"not measurable": fmt.Errorf("no project dir: %w", workplace.ErrNotMeasurable),
		"not exist":      fmt.Errorf("no transcript yet: %w", fs.ErrNotExist),
	} {
		t.Run(name, func(t *testing.T) {
			env, db, errOut := openRunTick(t, cause)
			if err := tick(context.Background(), env, db, errOut); err != nil {
				t.Fatalf("tick = %v, want nil\n%s", err, errOut.String())
			}
			if !strings.Contains(errOut.String(), ": unreachable, not processed: ") {
				t.Errorf("errOut = %q, want an unreachable line", errOut.String())
			}
		})
	}
}

func TestTickCollectOpenRunGenericErrorIsFailure(t *testing.T) {
	env, db, errOut := openRunTick(t, errors.New("boom"))
	if err := tick(context.Background(), env, db, errOut); !errors.Is(err, errSilent) {
		t.Fatalf("tick = %v, want errSilent\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "boom") || strings.Contains(errOut.String(), "unreachable") {
		t.Errorf("errOut = %q, want the reason as a failure line", errOut.String())
	}
}
