package pins

import "testing"

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := ParseVersion(s)
	if err != nil {
		t.Fatalf("ParseVersion(%q): %v", s, err)
	}
	return v
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.169.0-alpha.11", "0.169.0", -1},
		{"0.169.0", "0.169.0-alpha.11", 1},
		{"0.9.0", "0.10.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0+build1", "1.0.0+build2", 0},
		{"1.0.0-alpha.2", "1.0.0-alpha.11", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"2.0.0", "1.9.9", 1},
	}
	for _, c := range cases {
		if got := mustParse(t, c.a).Compare(mustParse(t, c.b)); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionString_keepsPrereleaseAndBuild(t *testing.T) {
	for _, s := range []string{"0.169.0-alpha.11", "1.2.3+abc", "1.2.3-rc.1+abc", "0.5.1"} {
		if got := mustParse(t, s).String(); got != s {
			t.Errorf("String() = %q, want %q", got, s)
		}
	}
}

func TestParseVersion_rejects(t *testing.T) {
	for _, s := range []string{"1.2", "v1.2.3", "abc", "", "1.2.3.4", "01.2.3", "1.2.3-"} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("ParseVersion(%q) succeeded, want error", s)
		}
	}
}

func TestFirstVersionIn(t *testing.T) {
	cases := []struct{ out, want string }{
		{"kenn-forge version 0.5.1 (commit abc)", "0.5.1"},
		{"codex-cli 0.169.0-alpha.11", "0.169.0-alpha.11"},
		{"tool 1.0.0\nother 2.0.0", "1.0.0"},
	}
	for _, c := range cases {
		v, ok := FirstVersionIn(c.out)
		if !ok || v.String() != c.want {
			t.Errorf("FirstVersionIn(%q) = %q, %v; want %q", c.out, v.String(), ok, c.want)
		}
	}
	if _, ok := FirstVersionIn("no version here 1.2"); ok {
		t.Error("FirstVersionIn found a version in text without one")
	}
}
