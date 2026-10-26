package usage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rolloutLineB = `{"timestamp":"2026-09-28T21:40:00.000Z","type":"event_msg","payload":{"rate_limits":{"limit_id":"codex","primary":{"used_percent":70.2,"resets_at":1790637809},"secondary":{"used_percent":80.9,"resets_at":1791047879}}}}` + "\n"

// writeRollout writes content to <home>/sessions/2026/09/28/<name> and returns the path.
func writeRollout(t *testing.T, home, name, content string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "09", "28")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func rolloutFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/codex/rollout-fragment.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func wantRolloutState(path string) CodexState {
	return CodexState{
		Source:      "rollout-log",
		GeneratedAt: "2026-09-28T22:00:00Z",
		AgeS:        2100,
		SourceFile:  path,
		LowerBound:  true,
		Session:     CodexWindow{Pct: 54, ResetAt: "2026-09-28T23:23:29Z"},
		Week:        CodexWindow{Pct: 66, ResetAt: "2026-10-03T17:17:59Z"},
	}
}

func TestReadRolloutFixture(t *testing.T) {
	home := t.TempDir()
	path := writeRollout(t, home, "rollout-x.jsonl", rolloutFixture(t))
	got, err := readRollout([]string{home}, codexFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRolloutState(path) {
		t.Errorf("got %+v, want %+v", got, wantRolloutState(path))
	}
}

func TestReadRolloutNewestOverHomes(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeRollout(t, a, "rollout-x.jsonl", rolloutFixture(t))
	pathB := writeRollout(t, b, "rollout-y.jsonl", rolloutLineB)
	got, err := readRollout([]string{a, b}, codexFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.Pct != 70 || got.Week.Pct != 80 || got.AgeS != 1200 || got.SourceFile != pathB {
		t.Errorf("got %+v, want pct 70/80, age 1200, file %s", got, pathB)
	}
}

func TestReadRolloutLongLine(t *testing.T) {
	home := t.TempDir()
	long := `{"payload":{"rate_limits":` + strings.Repeat("x", 200*1024) + "\n"
	path := writeRollout(t, home, "rollout-x.jsonl", long+rolloutFixture(t))
	got, err := readRollout([]string{home}, codexFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRolloutState(path) {
		t.Errorf("got %+v, want %+v", got, wantRolloutState(path))
	}
}

func TestReadRolloutSkips(t *testing.T) {
	line := func(limitID, ts, secondary string) string {
		return `{"timestamp":"` + ts + `","payload":{"rate_limits":{` + limitID +
			`"primary":{"used_percent":1,"resets_at":1790637809},"secondary":` + secondary + `}}}` + "\n"
	}
	win := `{"used_percent":2,"resets_at":1791047879}`
	cases := map[string]string{
		"other limit id": line(`"limit_id":"premium",`, "2026-09-28T21:25:00.000Z", win),
		"no time zone":   line("", "2026-09-28T21:25:00", win),
		"null secondary": line("", "2026-09-28T21:25:00Z", "null"),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			writeRollout(t, home, "rollout-x.jsonl", content)
			if got, err := readRollout([]string{home}, codexFixedNow); err == nil {
				t.Errorf("got %+v, want an error", got)
			}
		})
	}
	t.Run("no sessions dir", func(t *testing.T) {
		if got, err := readRollout([]string{t.TempDir()}, codexFixedNow); err == nil {
			t.Errorf("got %+v, want an error", got)
		}
	})
	t.Run("no homes", func(t *testing.T) {
		if got, err := readRollout(nil, codexFixedNow); err == nil {
			t.Errorf("got %+v, want an error", got)
		}
	})
}

func TestCodexReadFallsBackToRollout(t *testing.T) {
	home := t.TempDir()
	path := writeRollout(t, home, "rollout-x.jsonl", rolloutFixture(t))
	r, _ := fakeReader(t, "eof", home)
	got, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRolloutState(path) {
		t.Errorf("got %+v, want %+v", got, wantRolloutState(path))
	}
}

func TestCodexReadNotMeasurable(t *testing.T) {
	home := t.TempDir()
	r, _ := fakeReader(t, "eof", home)
	got, err := r.Read(context.Background())
	if err == nil {
		t.Fatalf("got %+v, want an error", got)
	}
	for _, want := range []string{"app-server: ", "rollout-log: "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if got != (CodexState{}) {
		t.Errorf("got %+v, want zero state", got)
	}
}
