package dispatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

var allInstalled = map[string]bool{
	"lighthouse-start-issue": true,
	"lighthouse-fix-pr":      true,
	"lighthouse-review-spec": true,
	"lighthouse-review-plan": true,
}

func TestJudgeVerdicts(t *testing.T) {
	tests := []struct {
		name    string
		next    string
		lane    string
		want    Verdict
		command string
		native  bool
		arg     string
	}{
		{"empty", "  ", "", VerdictRejected, "", false, "(empty)"},
		{"end", "/lighthouse-close-issue", "", VerdictEnd, "/lighthouse-close-issue", false, ""},
		{"brainstorm", "/superpowers:brainstorming", "", VerdictBrainstorm, "/superpowers:brainstorming", false, ""},
		{"interactive", "/superpowers:executing-plans /a/b.md", "", VerdictInteractive, "/superpowers:executing-plans /a/b.md", false, ""},
		{"lane with non-interactive", "/lighthouse-start-issue 7", "native", VerdictRejected, "", false, "/lighthouse-start-issue 7"},
		{"unknown lane on interactive", "/superpowers:executing-plans /a/b.md", "x", VerdictInteractive, "", false, ""},
		{"unknown lane", "/lighthouse-fix-pr 3", "x", VerdictRejected, "", false, "/lighthouse-fix-pr 3"},
		{"not allowed", "/rm -rf /", "", VerdictRejected, "", false, "/rm -rf /"},
		{"fix-pr number", "/lighthouse-fix-pr 3", "", VerdictStart, "/lighthouse-fix-pr 3", false, ""},
		{"legacy prefix", "/paia-start-issue 7", "", VerdictStart, "/lighthouse-start-issue 7", false, ""},
		{"review spec", "/lighthouse-review-spec /a/b.md", "", VerdictStart, "/lighthouse-review-spec /a/b.md", false, ""},
		{"review plan", "/lighthouse-review-plan /a/p.md", "", VerdictStart, "/lighthouse-review-plan /a/p.md", false, ""},
		{"legacy review name", "/lighthouse-spec-adversarial-review /a/b.md", "", VerdictStart, "/lighthouse-review-spec /a/b.md", false, ""},
		{"legacy prefix and review name", "/paia-plan-adversarial-review /a/p.md", "", VerdictStart, "/lighthouse-review-plan /a/p.md", false, ""},
		{"native interactive", "/superpowers:executing-plans /a/b.md", "native", VerdictStart, "/superpowers:executing-plans /a/b.md", true, ""},
		{"bad shape", "/lighthouse-start-issue 8", "", VerdictRejected, "", false, "/lighthouse-start-issue 8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Judge(tt.next, 7, tt.lane, allInstalled)
			if got.Verdict != tt.want || got.Native != tt.native || got.Arg != tt.arg {
				t.Fatalf("Judge = %+v, want verdict %v native %v arg %q", got, tt.want, tt.native, tt.arg)
			}
			if tt.command != "" && got.Command != tt.command {
				t.Fatalf("Command = %q, want %q", got.Command, tt.command)
			}
		})
	}
}

func TestJudgeSkillMissing(t *testing.T) {
	got := Judge("/paia-fix-pr 3", 7, "", map[string]bool{})
	if got.Verdict != VerdictSkillMissing || got.Arg != "lighthouse-fix-pr" {
		t.Fatalf("Judge = %+v", got)
	}
	// superpowers skills are not checked against the worktree.
	got = Judge("/superpowers:writing-plans /a/b.md", 7, "", map[string]bool{})
	if got.Verdict != VerdictStart {
		t.Fatalf("Judge = %+v", got)
	}
}

func TestJudgeArgumentShapes(t *testing.T) {
	tests := []struct {
		next string
		want Verdict
	}{
		{"/lighthouse-start-issue 8", VerdictRejected},
		{"/lighthouse-start-issue", VerdictRejected},
		{"/lighthouse-fix-pr 12a", VerdictRejected},
		{"/lighthouse-fix-pr 12", VerdictStart},
		{"/superpowers:writing-plans relative.md", VerdictRejected},
		{"/superpowers:writing-plans /a/b.md; rm", VerdictRejected},
		{`/superpowers:writing-plans "/a/b.md"`, VerdictRejected},
		{"/superpowers:writing-plans /a/b;c.md", VerdictRejected},
		{"/superpowers:writing-plans /a/$(x).md", VerdictRejected},
		{"/superpowers:writing-plans /a/b.md", VerdictStart},
		{"/superpowers:writing-plans", VerdictRejected},
	}
	for _, tt := range tests {
		if got := Judge(tt.next, 7, "", allInstalled); got.Verdict != tt.want {
			t.Errorf("Judge(%q) = %+v, want %v", tt.next, got, tt.want)
		}
	}
}

func TestReviewSkip(t *testing.T) {
	on := repoconfig.Controller{SpecReview: true, PlanReview: true}
	cmd, key := ReviewSkip("/lighthouse-review-spec /a/b.md", on)
	if cmd != "/lighthouse-review-spec /a/b.md" || key != "" {
		t.Fatalf("on: %q %q", cmd, key)
	}
	cmd, key = ReviewSkip("/lighthouse-review-spec /a/b.md", repoconfig.Controller{PlanReview: true})
	if cmd != "/superpowers:writing-plans /a/b.md" || key != "skip_spec_review" {
		t.Fatalf("spec off: %q %q", cmd, key)
	}
	cmd, key = ReviewSkip("/paia-plan-adversarial-review /a/p.md", repoconfig.Controller{SpecReview: true})
	if cmd != "/superpowers:subagent-driven-development /a/p.md" || key != "skip_plan_review" {
		t.Fatalf("plan off: %q %q", cmd, key)
	}
	cmd, key = ReviewSkip("/lighthouse-review-plan /a/p.md", repoconfig.Controller{SpecReview: true})
	if cmd != "/superpowers:subagent-driven-development /a/p.md" || key != "skip_plan_review" {
		t.Fatalf("plan off, new name: %q %q", cmd, key)
	}
	cmd, key = ReviewSkip("/lighthouse-start-issue 7", repoconfig.Controller{})
	if cmd != "/lighthouse-start-issue 7" || key != "" {
		t.Fatalf("other: %q %q", cmd, key)
	}
}

func TestInNight(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 31, h, m, 0, 0, time.UTC) }
	tests := []struct {
		window string
		now    time.Time
		want   bool
	}{
		{"21:00-08:00", at(20, 59), false},
		{"21:00-08:00", at(21, 0), true},
		{"21:00-08:00", at(7, 59), true},
		{"21:00-08:00", at(8, 0), false},
		{"01:00-03:00", at(2, 0), true},
		{"01:00-03:00", at(3, 0), false},
		{"x", at(2, 0), false},
		{"", at(2, 0), false},
		{"25:00-03:00", at(2, 0), false},
	}
	for _, tt := range tests {
		if got := InNight(tt.window, tt.now); got != tt.want {
			t.Errorf("InNight(%q, %s) = %v, want %v", tt.window, tt.now.Format("15:04"), got, tt.want)
		}
	}
}

func TestExecuting(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/lighthouse-review-spec /a.md":                  false,
		"/lighthouse-review-plan /a.md":                  false,
		"/lighthouse-spec-adversarial-review /a.md":      false,
		"/paia-plan-adversarial-review /a.md":            false,
		"/superpowers:writing-plans /a.md":               false,
		"/superpowers:subagent-driven-development /a.md": true,
		"/lighthouse-start-issue 7":                      true,
		"/lighthouse-fix-pr 3":                           true,
	} {
		if got := Executing(cmd); got != want {
			t.Errorf("Executing(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"De keten in decide en apply: handoff, start": "de-keten-in-decide-en-apply-handoff",
		"Één":      "n",
		"":         "",
		"--A  B--": "a-b",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"lighthouse-start-issue/SKILL.md", "other/README.md"} {
		full := filepath.Join(dir, ".claude", "skills", p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Installed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["lighthouse-start-issue"] {
		t.Fatalf("Installed = %v", got)
	}
	empty, err := Installed(t.TempDir())
	if err != nil || len(empty) != 0 {
		t.Fatalf("no skills dir: %v %v", empty, err)
	}
}
