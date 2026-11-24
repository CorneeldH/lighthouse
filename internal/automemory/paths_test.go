package automemory

import "testing"

func TestIsMemoryPath(t *testing.T) {
	const home = "/home/u"
	cases := []struct {
		name, path, cfg string
		want            bool
	}{
		{"unix file", "/home/u/.claude/projects/-home-u-r/memory/a.md", "", true},
		{"unix dir", "/home/u/.claude/projects/-home-u-r/memory", "", true},
		{"windows backslash", `D:\Users\u\.claude\projects\D--r\memory\a.md`, "", true},
		{"windows lower", "d:/users/u/.claude/projects/d--r/memory/a.md", "", true},
		{"tilde", "~/.claude/projects/p/memory/x.md", "", true},
		{"$HOME", "$HOME/.claude/projects/p/memory/x.md", "", true},
		{"config dir", "/cfg/projects/p/memory/a.md", "/cfg", true},
		{"memoryx", "/home/u/.claude/projects/p/memoryx/a.md", "", false},
		{"notes", "/home/u/.claude/projects/p/notes/a.md", "", false},
		{"repo memory", "/home/u/repo/memory/a.md", "", false},
		{"settings", "/home/u/.claude/settings.json", "", false},
		{"config dir memoryx", "/cfg/projects/p/memoryx/a.md", "/cfg", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsMemoryPath(c.path, home, c.cfg); got != c.want {
				t.Errorf("IsMemoryPath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		`C:\Foo\Bar`: "c:/foo/bar",
		"~/x":        "/home/u/x",
		"~":          "/home/u",
		"$HOME/x":    "/home/u/x",
		"${HOME}/x":  "/home/u/x",
		"/Other/~/x": "/other/~/x",
	}
	for in, want := range cases {
		if got := Normalize(in, "/home/u"); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeProject(t *testing.T) {
	cases := map[string]string{
		"/home/x/repo":                    "-home-x-repo",
		`D:\x\repo`:                       "D--x-repo",
		"/home/corneel/.cache/lighthouse": "-home-corneel--cache-lighthouse",
	}
	for in, want := range cases {
		if got := EncodeProject(in); got != want {
			t.Errorf("EncodeProject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigDir(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "CLAUDE_CONFIG_DIR" {
				return v
			}
			return ""
		}
	}
	if got := ConfigDir(env("/cfg"), "/home/u"); got != "/cfg" {
		t.Errorf("with env: %q", got)
	}
	if got := ConfigDir(env(""), "/home/u"); got != "/home/u/.claude" {
		t.Errorf("without env: %q", got)
	}
}
