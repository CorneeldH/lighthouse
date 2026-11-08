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
	if err := Dispatchable(r); err != nil {
		t.Fatalf("own repo is served by tick: %v", err)
	}
	if r.Controller.Profile != "software" {
		t.Errorf("controller profile = %q, want software", r.Controller.Profile)
	}
}

func TestLoadKindAndLanguage(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\nkind = \"gitlab\"\ndefault_branch = \"trunk\"\n[project]\nlanguage = \"nl\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "gitlab" || r.Language != "nl" || r.DefaultBranch != "trunk" {
		t.Errorf("kind/language/default branch: %q %q %q", r.Kind, r.Language, r.DefaultBranch)
	}
	d, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "github" || d.Language != "en" || d.DefaultBranch != "main" {
		t.Errorf("defaults: %q %q %q", d.Kind, d.Language, d.DefaultBranch)
	}
}

func TestLoadRejectsUnknownKind(t *testing.T) {
	_, err := Load(write(t, "[forge]\nrepo = \"o/r\"\nkind = \"forgejo\"\n"))
	if err == nil || !strings.Contains(err.Error(), "forge.kind") {
		t.Fatalf("want an error naming forge.kind, got %v", err)
	}
	for _, body := range []string{"[forge]\nrepo = \"o/r\"\nkind = \"\"\n", "[forge]\nrepo = \"o/r\"\n"} {
		r, err := Load(write(t, body))
		if err != nil || r.Kind != "github" {
			t.Errorf("%q: kind %q, err %v; want github", body, r.Kind, err)
		}
	}
}

func TestLoadLabelsDefaults(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Labels{
		BlockedIssue: "blocked-issue", BlockedExternal: "blocked-external",
		NeedsShaping: "needs-shaping", NeedsDecision: "needs-decision",
		NeedsHands: "needs-hands", NeedsTesting: "needs-testing",
	}
	if r.Labels != want {
		t.Errorf("labels = %+v, want %+v", r.Labels, want)
	}
}

func TestLoadLabelsFromFile(t *testing.T) {
	body := `[forge]
repo = "o/r"

[labels]
blocked_issue = "3-blocked-issue"
blocked_external = "3-blocked-external"
needs_shaping = "3-needs-shaping"
needs_decision = "3-needs-decision"
needs_hands = "3-needs-hands"
needs_testing = "3-needs-testing"
location_prefix = "1-loc:"
area_prefix = "2-area:"
`
	r, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	l := r.Labels
	if l.LocationPrefix != "1-loc:" || l.AreaPrefix != "2-area:" {
		t.Errorf("prefixes: %+v", l)
	}
	want := []string{
		"3-blocked-issue", "3-blocked-external", "3-needs-shaping",
		"3-needs-decision", "3-needs-hands", "3-needs-testing",
	}
	got := l.Blocking()
	if len(got) != len(want) {
		t.Fatalf("Blocking() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Blocking()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadTriageAbsent(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Triage != nil || r.TriageErr != nil {
		t.Errorf("triage = %+v, err = %v; want both unset", r.Triage, r.TriageErr)
	}
}

func TestLoadTriageDefaults(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n[controller.triage]\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Triage{MilestonesOnly: true, WaitForNight: true, MaxForwarded: 5, MaxJudgments: 15}
	if r.Triage == nil || *r.Triage != want || r.TriageErr != nil {
		t.Errorf("triage = %+v, err = %v; want %+v", r.Triage, r.TriageErr, want)
	}
}

func TestLoadTriageValues(t *testing.T) {
	body := "[forge]\nrepo = \"o/r\"\n[controller]\n[controller.triage]\nmilestones_only = false\nmax_forwarded = 2\n"
	r, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	want := Triage{MilestonesOnly: false, WaitForNight: true, MaxForwarded: 2, MaxJudgments: 15}
	if r.Triage == nil || *r.Triage != want {
		t.Errorf("triage = %+v, want %+v", r.Triage, want)
	}
}

func TestLoadTriageMilestoneTitleExclude(t *testing.T) {
	for _, tc := range []struct{ name, line, want string }{
		{"absent", "", ""},
		{"empty", `milestone_title_exclude = ""`, ""},
		{"set", `milestone_title_exclude = "(ON-HOLD|TO-?DO|DONE)$"`, "(ON-HOLD|TO-?DO|DONE)$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n[controller.triage]\n"+tc.line+"\n"))
			if err != nil || r.TriageErr != nil {
				t.Fatalf("Load = %v, TriageErr = %v; want both nil", err, r.TriageErr)
			}
			re := r.Triage.MilestoneTitleExclude
			if tc.want == "" {
				if re != nil {
					t.Errorf("MilestoneTitleExclude = %v, want nil", re)
				}
				return
			}
			if re == nil || re.String() != tc.want {
				t.Errorf("MilestoneTitleExclude = %v, want %q", re, tc.want)
			}
		})
	}
}

func TestLoadTriageBadKeyDoesNotFailLoad(t *testing.T) {
	for name, tc := range map[string]struct{ line, key string }{
		"unknown key":   {"max_forward = 2", "controller.triage.max_forward"},
		"below one":     {"max_judgments = 0", "controller.triage.max_judgments"},
		"invalid regex": {`milestone_title_exclude = "("`, "controller.triage.milestone_title_exclude"},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n[controller.triage]\n"+tc.line+"\n"))
			if err != nil {
				t.Fatalf("Load must not fail: %v", err)
			}
			if r.Controller == nil {
				t.Error("Controller must stay set so the tick keeps serving the repo")
			}
			if r.TriageErr == nil || !strings.Contains(r.TriageErr.Error(), tc.key) {
				t.Errorf("TriageErr = %v, want it to name %s", r.TriageErr, tc.key)
			}
			if err := Triageable(r); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("Triageable = %v, want it to name %s", err, tc.key)
			}
		})
	}
}

func TestLoadTriageWithoutController(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller.triage]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Triage == nil {
		t.Error("Triage must be set")
	}
	if r.Controller != nil {
		t.Errorf("Controller = %+v, want nil: [controller.triage] alone is no [controller]", r.Controller)
	}
	if err := Dispatchable(r); err == nil || !strings.Contains(err.Error(), "missing [controller]") {
		t.Errorf("Dispatchable = %v, want missing [controller]", err)
	}
}

func TestTriageable(t *testing.T) {
	withToDo := strings.Replace(full, `"Controller" = "c1"`, `"Controller" = "c1"`+"\n"+`"To do" = "t1"`, 1)
	withTriage := withToDo + "\n[controller.triage]\n"
	tests := []struct {
		name, body, wantErr string
	}{
		{"ok", withTriage, ""},
		{"no To do", full + "\n[controller.triage]\n", "missing forge.board.status_options.To do"},
		{"no triage", withToDo, "missing [controller.triage]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Load(write(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			err = Triageable(r)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Triageable = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Triageable = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// Red when a default goes missing or changes: the literal values are the documented defaults.
func TestLoadDefaultsFromConfig(t *testing.T) {
	r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ key, got, want string }{
		{"forge.default_branch", r.DefaultBranch, "main"},
		{"labels.blocked_issue", r.Labels.BlockedIssue, "blocked-issue"},
		{"labels.blocked_external", r.Labels.BlockedExternal, "blocked-external"},
		{"labels.needs_shaping", r.Labels.NeedsShaping, "needs-shaping"},
		{"labels.needs_decision", r.Labels.NeedsDecision, "needs-decision"},
		{"labels.needs_hands", r.Labels.NeedsHands, "needs-hands"},
		{"labels.needs_testing", r.Labels.NeedsTesting, "needs-testing"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: Load %q, want %q", tt.key, tt.got, tt.want)
		}
	}
}

// Red when a key renamed in internal/config silently turns into an empty default.
func TestDefaultStringPanicsWithoutStringDefault(t *testing.T) {
	for _, key := range []string{"labels.no_such_label", "usage_guard.resume", "test_gate.slots"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("defaultString(%q) did not panic", key)
				}
			}()
			_ = defaultString(key)
		}()
	}
}
