package hostconfig

import "testing"

func TestPath(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("LIGHTHOUSE_HOST_TOML", "/a/b.toml")
		if got := Path(); got != "/a/b.toml" {
			t.Errorf("Path() = %q, want /a/b.toml", got)
		}
	})
	t.Run("home default", func(t *testing.T) {
		t.Setenv("LIGHTHOUSE_HOST_TOML", "")
		t.Setenv("HOME", "/h")
		if got := Path(); got != "/h/.config/lighthouse/host.toml" {
			t.Errorf("Path() = %q, want /h/.config/lighthouse/host.toml", got)
		}
	})
}
