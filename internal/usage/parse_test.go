package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

func amsterdam(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func utc(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// envelope wraps a /usage result text in the JSON the CLI prints.
func envelope(result string) []byte {
	return []byte(`{"is_error": false, "result": "` + result + `"}`)
}

func TestParseFixtures(t *testing.T) {
	loc := amsterdam(t)
	defaultNow := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		file string
		now  time.Time
		want Windows
	}{
		{"usage_full.json", defaultNow, Windows{
			Session: Window{99, utc(t, "2026-09-06T12:30:00Z")},
			Week:    Window{48, utc(t, "2026-09-10T14:59:00Z")},
		}},
		{"usage_whole_hour.json", time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), Windows{
			Session: Window{28, utc(t, "2026-09-10T22:30:00Z")},
			Week:    Window{3, utc(t, "2026-09-17T15:00:00Z")},
		}},
		{"usage_session_no_reset.json", time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC), Windows{
			Session: Window{0, time.Time{}},
			Week:    Window{61, utc(t, "2026-10-15T15:00:00Z")},
		}},
		{"usage_no_model.json", defaultNow, Windows{
			Session: Window{7, utc(t, "2026-09-06T17:29:00Z")},
			Week:    Window{49, utc(t, "2026-09-10T14:59:00Z")},
		}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			got, err := Parse(fixture(t, c.file), c.now, loc)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Session.Pct != c.want.Session.Pct || !got.Session.Reset.Equal(c.want.Session.Reset) {
				t.Errorf("Session = %+v, want %+v", got.Session, c.want.Session)
			}
			if got.Week.Pct != c.want.Week.Pct || !got.Week.Reset.Equal(c.want.Week.Reset) {
				t.Errorf("Week = %+v, want %+v", got.Week, c.want.Week)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	loc := amsterdam(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	week := `\nCurrent week (all models): 48% used · resets Sep 10, 4:59pm (Europe/Amsterdam)`
	isError := strings.Replace(string(fixture(t, "usage_full.json")), `"is_error": false`, `"is_error": true`, 1)
	cases := []struct {
		name string
		in   []byte
	}{
		{"missing week", fixture(t, "usage_missing_week.json")},
		{"result not a string", []byte(`{"result": 42}`)},
		{"not json", []byte(`not json`)},
		{"is_error", []byte(isError)},
		{"session reset is junk", envelope(`Current session: 99% used · soon` + week)},
		{"reset day does not exist", envelope(`Current session: 99% used · resets Sep 31, 5pm (Europe/Amsterdam)` + week)},
		{"reset month unknown", envelope(`Current session: 99% used · resets Foo 6, 5pm (Europe/Amsterdam)` + week)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.in, now, loc)
			if err == nil {
				t.Fatal("Parse: want error, got nil")
			}
			if got != (Windows{}) {
				t.Errorf("Windows = %+v, want zero", got)
			}
		})
	}
}

func TestParseResetYearRollover(t *testing.T) {
	loc := amsterdam(t)
	now := time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)
	in := envelope(`Current session: 10% used · resets Jan 2, 5pm (Europe/Amsterdam)\nCurrent week (all models): 10% used · resets Jan 2, 5pm (Europe/Amsterdam)`)
	got, err := Parse(in, now, loc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := utc(t, "2027-01-02T16:00:00Z"); !got.Session.Reset.Equal(want) {
		t.Errorf("Session.Reset = %v, want %v", got.Session.Reset, want)
	}
}

func TestParseResetDST(t *testing.T) {
	loc := amsterdam(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	in := envelope(`Current session: 10% used · resets Oct 24, 9am (Europe/Amsterdam)\nCurrent week (all models): 10% used · resets Oct 26, 9am (Europe/Amsterdam)`)
	got, err := Parse(in, now, loc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := utc(t, "2026-10-24T07:00:00Z"); !got.Session.Reset.Equal(want) {
		t.Errorf("Session.Reset = %v, want %v", got.Session.Reset, want)
	}
	if want := utc(t, "2026-10-26T08:00:00Z"); !got.Week.Reset.Equal(want) {
		t.Errorf("Week.Reset = %v, want %v", got.Week.Reset, want)
	}
}
