package cli

import "testing"

func TestPersonalSkills(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", "/some/dir")
		t.Setenv("HOME", "/home/x")
		if got := personalSkills(); got != "/some/dir" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("home", func(t *testing.T) {
		t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", "")
		t.Setenv("HOME", "/home/x")
		if got := personalSkills(); got != "/home/x/.claude/skills" {
			t.Errorf("got %q", got)
		}
	})
	// Red when an unset HOME gives "/.claude/skills", a path that can exist on a machine.
	t.Run("no home", func(t *testing.T) {
		t.Setenv("CHECK_INSTRUCTIONS_PERSONAL", "")
		t.Setenv("HOME", "")
		if got := personalSkills(); got != "$HOME/.claude/skills" {
			t.Errorf("got %q", got)
		}
	})
}
