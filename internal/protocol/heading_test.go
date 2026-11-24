package protocol

import (
	"strings"
	"testing"
)

func TestHeadingLanguages(t *testing.T) {
	for _, c := range []struct{ key, language, want string }{
		{"kind_done", "nl", "Klaar"},
		{"kind_done", "de", "Done"},
		{"reason_state_lost", "nl", "runstaat kwijt"},
		{"reason_state_lost", "en", "run state lost"},
	} {
		if got := Heading(c.key, c.language); got != c.want {
			t.Errorf("Heading(%q, %q) = %q, want %q", c.key, c.language, got, c.want)
		}
	}
}

func TestHeadingUnknownKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Heading of an unknown key did not panic")
		}
	}()
	Heading("no_such_key", "nl")
}

func TestVariants(t *testing.T) {
	got := Variants("kind_done")
	if len(got) != 2 || got[0] != "Done" || got[1] != "Klaar" {
		t.Fatalf("Variants(kind_done) = %v, want [Done Klaar]", got)
	}
}

// The reason field of a Returned reads back as its key in either language, with or without an
// argument; a longer heading that starts with a shorter one is its own key.
func TestReasonKey(t *testing.T) {
	tests := []struct {
		field, key string
		ok         bool
	}{
		{"heeft brainstorm nodig", "reason_needs_brainstorm", true},
		{"asks for interactive execution", "reason_interactive", true},
		{"start mislukt: post: boom", "reason_start_failed", true},
		{"PR rood", "reason_pr_red", true},
		{"PR rood na 3 fix-pr-runs", "reason_pr_red_ceiling", true},
		{"PR red after 3 fix-pr runs: x", "reason_pr_red_ceiling", true},
		{"PR roodachtig", "", false},
		{"iets anders", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		if key, ok := ReasonKey(tt.field); key != tt.key || ok != tt.ok {
			t.Errorf("ReasonKey(%q) = %q, %v; want %q, %v", tt.field, key, ok, tt.key, tt.ok)
		}
	}
}

func TestQuestionPushHeadingsText(t *testing.T) {
	for _, c := range []struct{ language, pushed, permission string }{
		{"nl", "gepusht", "andere duidelijke toestemming"},
		{"en", "pushed", "another clear permission"},
	} {
		for _, key := range []string{"question_push_permit", "question_push_retry", "question_push_moved"} {
			got := Heading(key, c.language)
			for _, want := range []string{"`push`", c.permission} {
				if !strings.Contains(got, want) {
					t.Errorf("Heading(%q, %q) = %q, want it to contain %q", key, c.language, got, want)
				}
			}
		}
		if got := Heading("question_push_permit", c.language); !strings.Contains(got, c.pushed) {
			t.Errorf("Heading(question_push_permit, %q) = %q, want it to contain %q", c.language, got, c.pushed)
		}
	}
}
