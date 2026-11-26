package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServedRepos(t *testing.T) {
	root := t.TempDir()
	mk := func(name, toml string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if toml != "" {
			if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte(toml), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	a := mk("a", "[forge]\nrepo = \"o/a\"\n")
	b := mk("b", "[forge]\nrepo = \"o/b\"\n")
	bad := mk("bad", "")
	dup := mk("dup", "[forge]\nrepo = \"o/a\"\n")
	host := func(dirs ...string) string {
		q := make([]string, len(dirs))
		for i, d := range dirs {
			q[i] = `"` + d + `"`
		}
		p := filepath.Join(root, "host.toml")
		if err := os.WriteFile(p, []byte("[dispatch]\nrepos = ["+strings.Join(q, ",")+"]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	got := servedRepos(host(a, b))
	if len(got) != 2 || got[0].Slug != "o/a" || got[0].Dir != a || got[1].Slug != "o/b" {
		t.Errorf("all ok: %+v", got)
	}
	if got := servedRepos(host(a, bad)); got != nil {
		t.Errorf("one failing to load: %+v, want nil", got)
	}
	if got := servedRepos(host(a, dup)); got != nil {
		t.Errorf("duplicate slug: %+v, want nil", got)
	}
	if got := servedRepos(filepath.Join(root, "nope.toml")); len(got) != 0 {
		t.Errorf("missing host config: %+v, want none", got)
	}
}
