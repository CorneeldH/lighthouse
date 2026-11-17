package repoconfig

import (
	"strings"
	"testing"
	"time"
)

const scheduleBlocks = `
[[schedule]]
name = "codebase-map"
job  = "map-refresh"
cron = "0 4 1 * *"

[schedule.params]
command   = "/gsd-map-codebase"
documents = ["STACK", "INTEGRATIONS", "ARCHITECTURE", "STRUCTURE", "CONVENTIONS", "TESTING", "CONCERNS"]

[[schedule]]
name    = "reflect"
job     = "reflect"
cron    = "0 1 * * MON"
weeks   = 2
anchor  = "2026-10-12"
profile = "writing"

[schedule.params]
command = "/paia-reflect-skills"
`

func TestLoadSchedules(t *testing.T) {
	r, err := Load(write(t, full+scheduleBlocks))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Schedules) != 2 {
		t.Fatalf("schedules: %+v", r.Schedules)
	}
	m, f := r.Schedules[0], r.Schedules[1]
	if m.Name != "codebase-map" || m.Job != "map-refresh" || m.Cron != "0 4 1 * *" || m.Weeks != 0 || !m.Anchor.IsZero() {
		t.Errorf("map: %+v", m)
	}
	if m.Profile != "writing" {
		t.Errorf("profile default = %q, want the [controller] profile", m.Profile)
	}
	if docs, ok := m.Params["documents"].([]any); !ok || len(docs) != 7 || docs[0] != "STACK" {
		t.Errorf("documents: %#v", m.Params["documents"])
	}
	want := time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)
	if f.Weeks != 2 || !f.Anchor.Equal(want) || f.Anchor.Location() != time.Local {
		t.Errorf("reflect: %+v", f)
	}
	if f.Params["command"] != "/paia-reflect-skills" {
		t.Errorf("params: %#v", f.Params)
	}
}

func TestLoadScheduleProfileDefaultsWithoutController(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[[schedule]]\nname = \"a\"\njob = \"reflect\"\ncron = \"0 1 * * *\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Schedules[0].Profile != "software" {
		t.Errorf("profile = %q", r.Schedules[0].Profile)
	}
}

func TestLoadScheduleExplicitProfileWins(t *testing.T) {
	r, err := Load(write(t, strings.Replace(full, `profile = "writing"`, `profile = "software"`, 1)+scheduleBlocks))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Schedules[1].Profile; got != "writing" {
		t.Errorf("explicit profile = %q, want writing", got)
	}
}

func TestLoadScheduleInvalid(t *testing.T) {
	block := func(body string) string {
		return "[[schedule]]\nname = \"a\"\njob = \"reflect\"\ncron = \"0 1 * * MON\"\n" + body
	}
	tests := []struct {
		name, toml, want string
	}{
		{"missing name", "[[schedule]]\njob = \"reflect\"\ncron = \"0 1 * * *\"\n", "name"},
		{"uppercase name", strings.Replace(block(""), `"a"`, `"Map_Refresh"`, 1), "name"},
		{"duplicate name", block("") + block(""), "duplicate"},
		{"unknown job", strings.Replace(block(""), `"reflect"`, `"shell"`, 1), "job"},
		{"missing job", "[[schedule]]\nname = \"a\"\ncron = \"0 1 * * *\"\n", "job"},
		{"four fields", strings.Replace(block(""), "0 1 * * MON", "0 1 * *", 1), "cron"},
		{"missing cron", "[[schedule]]\nname = \"a\"\njob = \"reflect\"\n", "cron"},
		{"weeks without anchor", block("weeks = 2\n"), "anchor"},
		{"anchor without weeks", block("anchor = \"2026-10-12\"\n"), "weeks"},
		{"weeks zero", block("weeks = 0\nanchor = \"2026-10-12\"\n"), "weeks"},
		{"weeks negative", block("weeks = -1\nanchor = \"2026-10-12\"\n"), "weeks"},
		{"bad anchor", block("weeks = 2\nanchor = \"morgen\"\n"), "anchor"},
		{"unknown key", block("wekes = 2\n"), "wekes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(write(t, full+tt.toml))
			if err == nil {
				t.Fatal("Load = nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}
