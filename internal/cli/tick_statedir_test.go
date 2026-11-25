package cli

import (
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/testpath"
)

func TestTickStateDirPath(t *testing.T) {
	x, h, o := testpath.Abs("x"), testpath.Abs("h"), testpath.Abs("o")
	xDispatch := filepath.Join(x, "lighthouse", "dispatch")
	cases := []struct {
		name string
		env  map[string]string
		want string
		err  bool
	}{
		{"xdg absolute", map[string]string{"XDG_STATE_HOME": x}, xDispatch, false},
		{"home fallback", map[string]string{"XDG_STATE_HOME": "rel", "HOME": h}, filepath.Join(h, ".local", "state", "lighthouse", "dispatch"), false},
		{"no usable base", map[string]string{"HOME": "rel"}, "", true},
		{"ignores LIGHTHOUSE_STATE_DIR", map[string]string{"LIGHTHOUSE_STATE_DIR": o, "XDG_STATE_HOME": x}, xDispatch, false},
	}
	for _, c := range cases {
		getenv := func(k string) string { return c.env[k] }
		got, err := tickEnv{Getenv: getenv}.stateDir()
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%s: %q, %v; want %q err=%v", c.name, got, err, c.want, c.err)
		}
	}
	// Same path as the hook and collector derive without the override.
	getenv := func(k string) string { return map[string]string{"XDG_STATE_HOME": x}[k] }
	a, _ := tickEnv{Getenv: getenv}.stateDir()
	b, _ := collect.StateDir(getenv)
	if a != b {
		t.Errorf("tick %q != collect %q", a, b)
	}
	set := testpath.Abs("set")
	if got, _ := (tickEnv{StateDir: set, Getenv: getenv}).stateDir(); got != set {
		t.Errorf("explicit StateDir: %q", got)
	}
}
