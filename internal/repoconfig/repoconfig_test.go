package repoconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const full = `[forge]
repo = "o/r"

[forge.board]
project_number = 3
project_id = "PVT_x"
status_field_id = "SF"
priority_field_id = "PF"

[forge.board.status_options]
"Controller" = "c1"
"On hold" = "h1"

[forge.board.priority_options]
P0 = "p0"

[controller]
profile = "writing"
spec_review = false
bij_rondegrens = "stop"

[usage_guard]
controller = true
`

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadFull(t *testing.T) {
	r, err := Load(write(t, full))
	if err != nil {
		t.Fatal(err)
	}
	if r.Slug != "o/r" || !r.UsageGuardController {
		t.Errorf("slug/usage guard: %+v", r)
	}
	b := r.Board
	if b == nil || b.ProjectNumber != 3 || b.ProjectID != "PVT_x" || b.StatusFieldID != "SF" ||
		b.PriorityFieldID != "PF" || b.StatusOptions["On hold"] != "h1" || b.PriorityOptions["P0"] != "p0" {
		t.Errorf("board: %+v", b)
	}
	c := r.Controller
	if c == nil || c.Profile != "writing" || c.SpecReview || !c.PlanReview || c.AtRoundLimit != "stop" {
		t.Errorf("controller: %+v", c)
	}
}

func TestLoadDefaults(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := r.Controller
	if c == nil || c.Profile != "software" || !c.SpecReview || !c.PlanReview {
		t.Errorf("controller defaults: %+v", c)
	}
	if r.Board != nil || r.UsageGuardController {
		t.Errorf("board/usage guard should be unset: %+v", r)
	}
}

func TestLoadMissingRepo(t *testing.T) {
	if _, err := Load(write(t, "[forge]\nkind = \"github\"\n")); err == nil {
		t.Fatal("want error without [forge] repo")
	}
}

func TestLoadMissingFileAndParseError(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "lighthouse.toml")) {
		t.Errorf("missing file: %v", err)
	}
	bad := write(t, "[forge\n")
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("parse error: %v", err)
	}
}

func TestDispatchable(t *testing.T) {
	complete := func() Repo {
		r, err := Load(write(t, full))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if err := Dispatchable(complete()); err != nil {
		t.Fatalf("complete repo: %v", err)
	}
	rows := []struct {
		name   string
		mutate func(*Repo)
		want   string
	}{
		{"no controller", func(r *Repo) { r.Controller = nil }, "[controller]"},
		{"usage guard", func(r *Repo) { r.UsageGuardController = false }, "usage_guard.controller"},
		{"no board", func(r *Repo) { r.Board = nil }, "[forge.board]"},
		{"project_number", func(r *Repo) { r.Board.ProjectNumber = 0 }, "forge.board.project_number"},
		{"project_id", func(r *Repo) { r.Board.ProjectID = "" }, "forge.board.project_id"},
		{"status_field_id", func(r *Repo) { r.Board.StatusFieldID = "" }, "forge.board.status_field_id"},
		{"status Controller", func(r *Repo) { delete(r.Board.StatusOptions, "Controller") }, "forge.board.status_options.Controller"},
		{"status On hold", func(r *Repo) { delete(r.Board.StatusOptions, "On hold") }, "forge.board.status_options.On hold"},
		{"priority_field_id", func(r *Repo) { r.Board.PriorityFieldID = "" }, "forge.board.priority_field_id"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			r := complete()
			tc.mutate(&r)
			err := Dispatchable(r)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error naming %q, got %v", tc.want, err)
			}
		})
	}
}

func TestOwnLighthouseToml(t *testing.T) {
	r, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if r.Slug != "CorneeldH/lighthouse" || r.Board == nil || r.Board.StatusOptions["Controller"] != "7c834c1d" {
		t.Fatalf("own config: %+v", r)
	}
	err = Dispatchable(r)
	if err == nil {
		t.Fatal("own repo has no [controller] yet; want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "[controller]") || !strings.Contains(msg, "usage_guard.controller") ||
		strings.Contains(msg, "forge.board") {
		t.Errorf("reasons: %v", msg)
	}
}
