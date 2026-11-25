package repoconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/levels"
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
	if c == nil || c.Profile != "software" || !c.SpecReview || !c.PlanReview || c.AtRoundLimit != "doorgaan" {
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
		NeedsMind: "needs-mind", NeedsHands: "needs-hands",
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
needs_mind = "3-needs-mind"
needs_hands = "3-needs-hands"
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
		"3-blocked-issue", "3-blocked-external", "3-needs-mind", "3-needs-hands",
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
		{"labels.needs_mind", r.Labels.NeedsMind, "needs-mind"},
		{"labels.needs_hands", r.Labels.NeedsHands, "needs-hands"},
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

func TestLoadReadsHostAndTokenEnv(t *testing.T) {
	r, err := Load(write(t, "[forge]\nkind = \"gitlab\"\nrepo = \"g/sub/p\"\nhost = \"git.example.org\"\ntoken_env = \"WORK_GITLAB\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Host != "git.example.org" || r.TokenEnv != "WORK_GITLAB" {
		t.Errorf("host/token_env: %q, %q", r.Host, r.TokenEnv)
	}
	r, err = Load(write(t, "[forge]\nrepo = \"o/r\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Host != "" || r.TokenEnv != "" {
		t.Errorf("absent host/token_env must be empty: %q, %q", r.Host, r.TokenEnv)
	}
}

func TestLoadAllRefusesDuplicateSlug(t *testing.T) {
	dirA := write(t, "[forge]\nrepo = \"o/r\"\n")
	dirB := write(t, "[forge]\nrepo = \"o/x\"\n")
	dirC := write(t, "[forge]\nrepo = \"o/r\"\n")
	got := LoadAll([]string{dirA, dirB, dirC})
	if len(got) != 3 {
		t.Fatalf("got %d results", len(got))
	}
	if got[0].Err == nil || got[2].Err == nil || got[1].Err != nil {
		t.Fatalf("errs: %v, %v, %v", got[0].Err, got[1].Err, got[2].Err)
	}
	if got[0].Repo.Slug != "o/r" || got[2].Repo.Slug != "o/r" {
		t.Errorf("slug must stay filled: %q, %q", got[0].Repo.Slug, got[2].Repo.Slug)
	}
	if want := dirA + ": [forge] repo o/r is also the repo of " + dirC; got[0].Err.Error() != want {
		t.Errorf("err 0 = %q, want %q", got[0].Err, want)
	}
	if want := dirC + ": [forge] repo o/r is also the repo of " + dirA; got[2].Err.Error() != want {
		t.Errorf("err 2 = %q, want %q", got[2].Err, want)
	}
	if got[1].Path != dirB || got[1].Repo.Slug != "o/x" {
		t.Errorf("unique repo: %+v", got[1])
	}
}

func TestLoadAllKeepsLoadError(t *testing.T) {
	dir := t.TempDir()
	got := LoadAll([]string{dir})
	_, want := Load(dir)
	if len(got) != 1 || got[0].Path != dir || got[0].Err == nil || got[0].Err.Error() != want.Error() {
		t.Fatalf("got %+v, want err %v", got, want)
	}
	if got[0].Repo.Slug != "" {
		t.Errorf("Repo must be empty on a Load error: %+v", got[0].Repo)
	}
}

func TestControllerLocations(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n[controller.locations]\nmac = \"mac\"\ncontainer = \"software\"\n"))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"mac": "mac", "container": "software"}
		got := r.Controller.Locations
		if len(got) != len(want) || got["mac"] != "mac" || got["container"] != "software" {
			t.Errorf("Locations = %v, want %v", got, want)
		}
	})
	t.Run("absent", func(t *testing.T) {
		r, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n"))
		if err != nil {
			t.Fatal(err)
		}
		if r.Controller.Locations != nil {
			t.Errorf("Locations = %v, want nil", r.Controller.Locations)
		}
	})
	t.Run("empty profile", func(t *testing.T) {
		_, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller]\n[controller.locations]\nmac = \"\"\n"))
		if err == nil || !strings.Contains(err.Error(), "controller.locations.mac") || !strings.Contains(err.Error(), "empty") {
			t.Errorf("err = %v, want one naming controller.locations.mac and empty", err)
		}
	})
	t.Run("without controller table", func(t *testing.T) {
		_, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n[controller.locations]\nmac = \"mac\"\n"))
		if err == nil || !strings.Contains(err.Error(), "[controller] table required") {
			t.Errorf("err = %v, want [controller] table required", err)
		}
	})
}

func TestLoadReport(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		want    Report
		wantErr string // "" means ReportErr must be nil
	}{
		{"geldig", "[report]\nenabled = true\nsink = \"file\"\ndir = \"verslagen\"\n",
			Report{Enabled: true, Sink: "file", Dir: "verslagen"}, ""},
		{"zonder tabel", "", Report{Sink: "stdout"}, ""},
		{"enabled als string", "[report]\nenabled = \"true\"\n", Report{Sink: "stdout"}, "report.enabled"},
		{"onbekende sink", "[report]\nsink = \"wiki\"\n", Report{Sink: "stdout"}, "report.sink"},
		{"file zonder dir", "[report]\nsink = \"file\"\n", Report{Sink: "file"}, "report.dir"},
		{"absolute dir", "[report]\nsink = \"file\"\ndir = \"/abs\"\n", Report{Sink: "file"}, "report.dir"},
		{"dir buiten repo", "[report]\nsink = \"file\"\ndir = \"../buiten\"\n", Report{Sink: "file"}, "report.dir"},
		{"dir ontsnapt via ..", "[report]\nsink = \"file\"\ndir = \"a/../../b\"\n", Report{Sink: "file"}, "report.dir"},
		{"dir geen string", "[report]\nsink = \"file\"\ndir = 5\n", Report{Sink: "file"}, "report.dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Load(write(t, full+tt.table))
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}
			if tt.wantErr == "" {
				if r.ReportErr != nil {
					t.Fatalf("ReportErr = %v, want nil", r.ReportErr)
				}
			} else if r.ReportErr == nil || !strings.Contains(r.ReportErr.Error(), tt.wantErr) {
				t.Fatalf("ReportErr = %v, want text with %q", r.ReportErr, tt.wantErr)
			}
			if tt.wantErr == "" && r.Report != tt.want {
				t.Errorf("Report = %+v, want %+v", r.Report, tt.want)
			}
		})
	}
}

func TestLoadReportErrKeepsRepoServable(t *testing.T) {
	r, err := Load(write(t, full+"[report]\nenabled = \"true\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Slug != "o/r" {
		t.Errorf("Slug = %q, want o/r", r.Slug)
	}
	if err := Dispatchable(r); err != nil {
		t.Errorf("Dispatchable: %v", err)
	}
}

const gitlabFull = `[forge]
kind = "gitlab"
repo = "g/sub/p"

[forge.board.status_options]
"Controller" = "Controller"
"On hold" = "On hold"

[forge.board.priority_options]
P1 = "priority::P1"

[controller]

[usage_guard]
controller = true
`

// A GitLab board has no project or field IDs: Dispatchable asks only for the status options.
func TestDispatchableGitLab(t *testing.T) {
	load := func(body string) Repo {
		r, err := Load(write(t, body))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if err := Dispatchable(load(gitlabFull)); err != nil {
		t.Fatalf("GitLab repo with status_options only: %v", err)
	}
	r0 := load(gitlabFull)
	delete(r0.Board.StatusOptions, "Controller")
	if err := Dispatchable(r0); err == nil || !strings.Contains(err.Error(), "forge.board.status_options.Controller") {
		t.Errorf("without Controller: err = %v", err)
	}
	// On hold is no longer required (red when "On hold" returns to the Dispatchable check).
	r1 := load(gitlabFull)
	delete(r1.Board.StatusOptions, "On hold")
	if err := Dispatchable(r1); err != nil {
		t.Errorf("without On hold: err = %v, want nil", err)
	}
	r := load(gitlabFull)
	r.Board.StatusOptions["On hold"] = "controller"
	r.Board.PriorityOptions["P2"] = "priority::P1"
	err := Dispatchable(r)
	for _, want := range []string{
		"forge.board.status_options.Controller and forge.board.status_options.On hold map to the same GitLab status",
		"forge.board.priority_options.P1 and forge.board.priority_options.P2 map to the same label",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("shared option: err = %v, want it to contain %q", err, want)
		}
	}
	// The same board on GitHub still misses its IDs.
	r = load(gitlabFull)
	r.Kind = "github"
	err = Dispatchable(r)
	for _, want := range []string{"project_number", "project_id", "status_field_id", "priority_field_id"} {
		if err == nil || !strings.Contains(err.Error(), "forge.board."+want) {
			t.Errorf("GitHub repo without %s: err = %v", want, err)
		}
	}
}

// LoadFile reads a lighthouse.toml under any file name, as LIGHTHOUSE_TOML may name one, with
// token_env, and its Dir is the file's directory.
func TestLoadFileAnyName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "other.toml")
	if err := os.WriteFile(path, []byte("[forge]\nrepo = \"o/r\"\ntoken_env = \"MY_TOKEN\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadFile(path)
	if err != nil || r.Slug != "o/r" || r.TokenEnv != "MY_TOKEN" || r.Dir != dir {
		t.Errorf("LoadFile = %+v, %v; want Slug o/r, TokenEnv MY_TOKEN, Dir %s", r, err, dir)
	}
}

func TestLoadProjectOwner(t *testing.T) {
	r, err := Load(write(t, strings.Replace(full, "project_id = \"PVT_x\"\n", "project_id = \"PVT_x\"\nproject_owner = \"acme\"\n", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Board.ProjectOwner != "acme" {
		t.Errorf("ProjectOwner = %q, want acme", r.Board.ProjectOwner)
	}
	r, err = Load(write(t, full))
	if err != nil {
		t.Fatal(err)
	}
	if r.Board.ProjectOwner != "" {
		t.Errorf("ProjectOwner = %q, want empty", r.Board.ProjectOwner)
	}
}

// Production change that turns it red: NeedsName without one of the two keys, or with
// blocked_*, returns the wrong name.
func TestLabelsNeedsName(t *testing.T) {
	l := Labels{BlockedIssue: "b-i", BlockedExternal: "b-e", NeedsMind: "n-m", NeedsHands: "n-h"}
	for _, tc := range []struct {
		labels []string
		want   string
	}{
		{[]string{"n-m"}, "n-m"}, {[]string{"x", "n-h"}, "n-h"},
		{[]string{"b-i", "b-e", "x"}, ""}, {nil, ""},
	} {
		if got := l.NeedsName(tc.labels); got != tc.want {
			t.Errorf("NeedsName(%v) = %q, want %q", tc.labels, got, tc.want)
		}
	}
	if got := (Labels{}).NeedsName([]string{""}); got != "" {
		t.Errorf("NeedsName with empty names = %q, want empty", got)
	}
}

// Fails when Load stops filling Repo.Paths from [paths], or when other [paths] keys
// (history, mirror_exceptions) make Load fail.
func TestLoadReadsPaths(t *testing.T) {
	body := "[forge]\nrepo = \"o/r\"\n[paths]\nlabels = \"docs/developer/labels.md\"\n" +
		"tenets = \"docs/t.md\"\nareas = \"docs/areas\"\nstrategy = \"docs/s.md\"\nhistory = \"h.md\"\nmirror_exceptions = [\"x\"]\n"
	r, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{Labels: "docs/developer/labels.md", Tenets: "docs/t.md", Areas: "docs/areas", Strategy: "docs/s.md"}
	if r.Paths != want {
		t.Errorf("Paths = %+v, want %+v", r.Paths, want)
	}
	d, err := Load(write(t, "[forge]\nrepo = \"o/r\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Paths != (Paths{}) {
		t.Errorf("Paths without [paths] = %+v, want zero", d.Paths)
	}
}

func writeInstalled(t *testing.T, dir string, components map[string]levels.Stand) {
	t.Helper()
	data := levels.Installed{Level: "x", Since: "2026-01-01T00:00:00Z", Components: components}.Marshal()
	writeRawInstalled(t, dir, string(data))
}

func writeRawInstalled(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, levels.InstalledPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const bareController = "[forge]\nrepo = \"o/r\"\n[controller]\n"

func TestLoadReviewKnobsFollowInstalled(t *testing.T) {
	const spec, plan = "skill:review-spec", "skill:review-plan"
	rows := []struct {
		name       string
		components map[string]levels.Stand // nil: no components.toml
		wantSpec   bool
		wantPlan   bool
	}{
		{"both off", map[string]levels.Stand{spec: levels.StandOff, plan: levels.StandOff}, false, false},
		{"plan off", map[string]levels.Stand{plan: levels.StandOff}, true, false},
		{"both on", map[string]levels.Stand{spec: levels.StandOn, plan: levels.StandOn}, true, true},
		{"no components.toml", nil, true, true},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			dir := write(t, bareController)
			if tc.components != nil {
				writeInstalled(t, dir, tc.components)
			}
			r, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			c := r.Controller
			if c.SpecReview != tc.wantSpec || c.PlanReview != tc.wantPlan || c.AtRoundLimit != "doorgaan" {
				t.Errorf("spec=%v plan=%v limit=%q, want %v %v doorgaan", c.SpecReview, c.PlanReview, c.AtRoundLimit, tc.wantSpec, tc.wantPlan)
			}
			if r.ControllerErr != nil {
				t.Errorf("ControllerErr: %v", r.ControllerErr)
			}
		})
	}
}

func TestLoadReviewKeyBeatsLevel(t *testing.T) {
	off := map[string]levels.Stand{"skill:review-spec": levels.StandOff, "skill:review-plan": levels.StandOff}
	rows := []struct {
		name       string
		body       string
		components map[string]levels.Stand
	}{
		{"light with spec_review true", bareController + "spec_review = true\n", off},
		{"strict with plan_review false", bareController + "plan_review = false\n", map[string]levels.Stand{}},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			dir := write(t, tc.body)
			writeInstalled(t, dir, tc.components)
			r, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c := r.Controller; !c.SpecReview || c.PlanReview {
				t.Errorf("spec=%v plan=%v, want true false", c.SpecReview, c.PlanReview)
			}
		})
	}
}

func TestLoadReviewOverrideOn(t *testing.T) {
	dir := write(t, bareController)
	writeInstalled(t, dir, map[string]levels.Stand{"skill:other": levels.StandOff})
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c := r.Controller; !c.SpecReview || !c.PlanReview {
		t.Errorf("spec=%v plan=%v, want true true", c.SpecReview, c.PlanReview)
	}
}

func TestLoadBrokenComponentsSetsControllerErr(t *testing.T) {
	dir := write(t, full[:strings.Index(full, "[controller]")]+"[controller]\nprofile = \"writing\"\n\n[usage_guard]\ncontroller = true\n")
	writeRawInstalled(t, dir, "level = [\n")
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("Load must not fail: %v", err)
	}
	want := filepath.Join(dir, ".lighthouse/components.toml")
	if r.ControllerErr == nil || !strings.Contains(r.ControllerErr.Error(), want) {
		t.Fatalf("ControllerErr = %v, want it to name %s", r.ControllerErr, want)
	}
	if c := r.Controller; !c.SpecReview || !c.PlanReview {
		t.Errorf("knobs must stay true: %+v", c)
	}
	derr := Dispatchable(r)
	if derr == nil || !strings.Contains(derr.Error(), want) {
		t.Errorf("Dispatchable = %v, want it to name %s", derr, want)
	}
}

func TestLoadComponentsNotReadWhenNotNeeded(t *testing.T) {
	rows := []struct{ name, body string }{
		{"no controller", strings.Replace(full, "[controller]", "[unrelated]", 1)},
		{"both keys set", strings.Replace(full, "spec_review = false", "spec_review = false\nplan_review = true", 1)},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			dir := write(t, tc.body)
			writeRawInstalled(t, dir, "level = [\n")
			r, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if r.ControllerErr != nil {
				t.Errorf("ControllerErr: %v", r.ControllerErr)
			}
			if derr := Dispatchable(r); derr != nil && strings.Contains(derr.Error(), "components.toml") {
				t.Errorf("Dispatchable names components.toml: %v", derr)
			}
		})
	}
}

func TestLoadFileRejectsOldLabelKeys(t *testing.T) {
	for _, key := range config.OldLabelKeys {
		t.Run(key, func(t *testing.T) {
			dir := write(t, "[forge]\nrepo = \"o/r\"\n\n[labels]\n"+key+" = \"x\"\n")
			_, err := LoadFile(filepath.Join(dir, "lighthouse.toml"))
			if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "lighthouse labels migrate") {
				t.Errorf("LoadFile error = %v, want it to name %s and lighthouse labels migrate", err, key)
			}
		})
	}
}

func TestDefaultLabelsNeedsMind(t *testing.T) {
	l := DefaultLabels()
	if l.NeedsMind != "needs-mind" || l.NeedsHands != "needs-hands" {
		t.Errorf("DefaultLabels = %+v, want needs-mind and needs-hands", l)
	}
}
