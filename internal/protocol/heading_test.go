package protocol

import "testing"

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
