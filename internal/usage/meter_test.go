package usage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// meterNow is the clock of the meter tests; usage_full parses against it.
var meterNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// fakeRun is a RunUsage that returns out and err and counts its calls. It
// checks the contract of every call: the workdir is Paths.Workdir() and
// exists, and the context carries the refresh deadline.
type fakeRun struct {
	t     *testing.T
	p     Paths
	out   []byte
	err   error
	calls int
}

func (f *fakeRun) run(ctx context.Context, workdir string) ([]byte, error) {
	f.t.Helper()
	f.calls++
	if workdir != f.p.Workdir() {
		f.t.Errorf("workdir = %q, want %q", workdir, f.p.Workdir())
	}
	if fi, err := os.Stat(workdir); err != nil || !fi.IsDir() {
		f.t.Errorf("workdir %q is not a directory: %v", workdir, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		f.t.Error("context has no deadline")
	} else if left := time.Until(deadline); left < 55*time.Second || left > refreshTimeout {
		f.t.Errorf("deadline in %v, want between 55s and %v", left, refreshTimeout)
	}
	return f.out, f.err
}

func newTestMeter(t *testing.T, p Paths, out []byte, running bool) (Meter, *fakeRun) {
	t.Helper()
	f := &fakeRun{t: t, p: p, out: out}
	return Meter{
		Paths:         p,
		Config:        DefaultConfig(),
		Now:           func() time.Time { return meterNow },
		Loc:           amsterdam(t),
		RunUsage:      f.run,
		ClaudeRunning: func() bool { return running },
	}, f
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s still present: %v", filepath.Base(path), err)
	}
}

// cacheAged is a cache file whose generated_at lies age before meterNow.
func cacheAged(age time.Duration) string {
	return `{"schema":1,"generated_at":"` + meterNow.Add(-age).Format(timeLayout) +
		`","session":{"pct":1,"reset_at":"","reset_local":""},"week":{"pct":2,"reset_at":"","reset_local":""}}` + "\n"
}

func TestRefreshWritesCacheAndAlarm(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	m, f := newTestMeter(t, p, fixture(t, "usage_full.json"), true)

	got, err := m.Refresh(false)
	if err != nil || got != Refreshed {
		t.Fatalf("Refresh = %q, %v; want refreshed", got, err)
	}
	if f.calls != 1 {
		t.Errorf("RunUsage calls = %d, want 1", f.calls)
	}
	want := `{"schema":1,"generated_at":"2026-09-05T12:00:00Z",` +
		`"session":{"pct":99,"reset_at":"2026-09-06T12:30:00Z","reset_local":"14:30"},` +
		`"week":{"pct":48,"reset_at":"2026-09-10T14:59:00Z","reset_local":"do 16:59"}}` + "\n"
	if c := readFile(t, p.Cache()); c != want {
		t.Errorf("cache = %q, want %q", c, want)
	}
	if a := readFile(t, p.Alarm()); a != "venster 99 14:30 2026-09-06T12:30:00Z\n" {
		t.Errorf("alarm = %q", a)
	}
}

func TestRefreshCreatesDir(t *testing.T) {
	p := Paths{Dir: filepath.Join(t.TempDir(), "new", "lighthouse")}
	m, _ := newTestMeter(t, p, fixture(t, "usage_no_model.json"), true)

	got, err := m.Refresh(false)
	if err != nil || got != Refreshed {
		t.Fatalf("Refresh = %q, %v; want refreshed", got, err)
	}
	if _, _, ok := ReadState(p.Cache()); !ok {
		t.Error("no readable cache written")
	}
}

func TestRefreshRemovesAlarmBelowThreshold(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	writeFile(t, p.Alarm(), "venster 99 14:30 2026-09-06T12:30:00Z\n")
	m, _ := newTestMeter(t, p, fixture(t, "usage_no_model.json"), true)

	if got, err := m.Refresh(false); err != nil || got != Refreshed {
		t.Fatalf("Refresh = %q, %v; want refreshed", got, err)
	}
	assertAbsent(t, p.Alarm())
}

func TestRefreshTTL(t *testing.T) {
	cases := []struct {
		name      string
		age       time.Duration
		ttl       time.Duration
		force     bool
		want      Outcome
		wantCalls int
	}{
		{"299s old", 299 * time.Second, 300 * time.Second, false, Fresh, 0},
		{"300s old", 300 * time.Second, 300 * time.Second, false, Refreshed, 1},
		{"10s old forced", 10 * time.Second, 300 * time.Second, true, Refreshed, 1},
		{"ttl zero", 0, 0, false, Refreshed, 1},
		{"generated in the future", -time.Hour, 300 * time.Second, false, Refreshed, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Paths{Dir: t.TempDir()}
			old := cacheAged(c.age)
			writeFile(t, p.Cache(), old)
			m, f := newTestMeter(t, p, fixture(t, "usage_full.json"), true)
			m.Config.TTL = c.ttl

			got, err := m.Refresh(c.force)
			if err != nil || got != c.want {
				t.Fatalf("Refresh = %q, %v; want %q", got, err, c.want)
			}
			if f.calls != c.wantCalls {
				t.Errorf("RunUsage calls = %d, want %d", f.calls, c.wantCalls)
			}
			if c.want == Fresh && readFile(t, p.Cache()) != old {
				t.Error("fresh cache was rewritten")
			}
		})
	}
}

func TestRefreshCorruptCacheRefreshes(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	writeFile(t, p.Cache(), "{")
	m, f := newTestMeter(t, p, fixture(t, "usage_full.json"), true)

	if got, err := m.Refresh(false); err != nil || got != Refreshed {
		t.Fatalf("Refresh = %q, %v; want refreshed", got, err)
	}
	if f.calls != 1 {
		t.Errorf("RunUsage calls = %d, want 1", f.calls)
	}
}

func TestRefreshKeepsOldCache(t *testing.T) {
	oldCache := cacheAged(time.Hour)
	oldAlarm := "week 96 do_16:59 2026-09-10T14:59:00Z\n"
	cases := []struct {
		name       string
		out        []byte
		runErr     error
		panics     bool
		alarmIsDir bool
	}{
		{name: "run error", runErr: errors.New("claude: not logged in")},
		{name: "missing week", out: fixture(t, "usage_missing_week.json")},
		{name: "not json", out: []byte("not json")},
		{name: "run panics", panics: true},
		{name: "alarm write fails", out: fixture(t, "usage_full.json"), alarmIsDir: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Paths{Dir: t.TempDir()}
			writeFile(t, p.Cache(), oldCache)
			if c.alarmIsDir {
				if err := os.MkdirAll(filepath.Join(p.Alarm(), "child"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, p.Alarm(), oldAlarm)
			}
			m, f := newTestMeter(t, p, c.out, true)
			f.err = c.runErr
			if c.panics {
				m.RunUsage = func(context.Context, string) ([]byte, error) { panic("boom") }
			}

			got, err := m.Refresh(false)
			if got != Failed || err == nil {
				t.Fatalf("Refresh = %q, %v; want failed with an error", got, err)
			}
			if b := readFile(t, p.Cache()); b != oldCache {
				t.Errorf("cache = %q, want the old cache", b)
			}
			if !c.alarmIsDir {
				if a := readFile(t, p.Alarm()); a != oldAlarm {
					t.Errorf("alarm = %q, want the old alarm", a)
				}
			}
			unlock, held, err := tryLock(p.Lock())
			if err != nil || !held {
				t.Fatalf("lock not released after failure: held %v, %v", held, err)
			}
			unlock()
		})
	}
}

func TestRefreshBusy(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	unlock, held, err := tryLock(p.Lock())
	if err != nil || !held {
		t.Fatalf("tryLock = %v, %v", held, err)
	}
	defer unlock()
	m, f := newTestMeter(t, p, fixture(t, "usage_full.json"), true)

	if got, err := m.Refresh(true); err != nil || got != Busy {
		t.Fatalf("Refresh = %q, %v; want busy", got, err)
	}
	if f.calls != 0 {
		t.Errorf("RunUsage calls = %d, want 0", f.calls)
	}
	assertAbsent(t, p.Cache())
}

func TestTick(t *testing.T) {
	expired := "venster 99 14:30 2026-09-05T11:00:00Z\n"
	cases := []struct {
		name      string
		running   bool
		alarm     string
		want      Outcome
		wantCalls int
	}{
		{"no claude", false, "", Idle, 0},
		{"claude running", true, "", Refreshed, 1},
		{"no claude prunes expired alarm", false, expired, Idle, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Paths{Dir: t.TempDir()}
			if c.alarm != "" {
				writeFile(t, p.Alarm(), c.alarm)
			}
			m, f := newTestMeter(t, p, fixture(t, "usage_no_model.json"), c.running)

			got, err := m.Tick()
			if err != nil || got != c.want {
				t.Fatalf("Tick = %q, %v; want %q", got, err, c.want)
			}
			if f.calls != c.wantCalls {
				t.Errorf("RunUsage calls = %d, want %d", f.calls, c.wantCalls)
			}
			if c.alarm != "" {
				assertAbsent(t, p.Alarm())
			}
		})
	}
}

func TestTickPruneErrorDoesNotStop(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	// A directory on the alarm path: PruneAlarm cannot read it.
	if err := os.MkdirAll(p.Alarm(), 0o755); err != nil {
		t.Fatal(err)
	}
	m, _ := newTestMeter(t, p, fixture(t, "usage_no_model.json"), true)

	if got, err := m.Tick(); err != nil || got != Refreshed {
		t.Fatalf("Tick = %q, %v; want refreshed", got, err)
	}
}

func TestUsageCommand(t *testing.T) {
	workdir := t.TempDir()
	cmd := usageCommand(context.Background(), workdir)
	if want := []string{"claude", "-p", "/usage", "--output-format", "json"}; !slices.Equal(cmd.Args, want) {
		t.Errorf("Args = %q, want %q", cmd.Args, want)
	}
	if cmd.Dir != workdir {
		t.Errorf("Dir = %q, want %q", cmd.Dir, workdir)
	}
	for _, kv := range []string{"LIGHTHOUSE_USAGE_REFRESH=1", "PATH=" + os.Getenv("PATH")} {
		if !slices.Contains(cmd.Env, kv) {
			t.Errorf("Env lacks %q", kv)
		}
	}
	if cmd.WaitDelay != waitDelay {
		t.Errorf("WaitDelay = %v, want %v", cmd.WaitDelay, waitDelay)
	}
}

func TestUsageCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh script")
	}
	bin := t.TempDir()
	// The trailing echo keeps the shell from exec'ing sleep: after the kill of
	// the shell, sleep is a grandchild that still holds stdout open.
	script := "#!/bin/sh\nsleep 30\necho late\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := usageCommand(ctx, t.TempDir()).Output()
	if err == nil {
		t.Fatal("Output: want error after the deadline, got nil")
	}
	d := time.Since(start)
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("ctx.Err() = %v, want DeadlineExceeded: the command failed before the deadline", ctx.Err())
	}
	if d < 150*time.Millisecond {
		t.Errorf("Output returned after %v, want it to run until the 200ms deadline", d)
	}
	if d > 10*time.Second {
		t.Errorf("Output returned after %v, want within 10s", d)
	}
}

func TestRunUsageStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a /bin/sh script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'Not logged in' >&2\necho second >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := runUsage(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("runUsage: want error, got nil")
	}
	if got := err.Error(); got != "exit status 1: Not logged in" {
		t.Errorf("error = %q, want %q", got, "exit status 1: Not logged in")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Error("error no longer wraps *exec.ExitError")
	}
}

func TestClaudeRunning(t *testing.T) {
	cases := []struct {
		name string
		code int
		err  error
		want bool
	}{
		{"match", 0, nil, true},
		{"no match", 1, nil, false},
		{"syntax error", 2, nil, true},
		{"fatal error", 3, nil, true},
		{"no pgrep", 0, exec.ErrNotFound, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := claudeRunning(func() (int, error) { return c.code, c.err })
			if got != c.want {
				t.Errorf("claudeRunning = %v, want %v", got, c.want)
			}
		})
	}
}
