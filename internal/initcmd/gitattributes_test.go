package initcmd

import (
	"strings"
	"testing"
)

func TestEnsureGitattributes(t *testing.T) {
	const sh, hooks = "*.sh eol=lf", ".claude/hooks/lighthouse/** eol=lf"
	for _, tc := range []struct {
		name, old, want string
	}{
		{"empty", "", sh + "\n" + hooks + "\n"},
		{"own sh rule stays", "*.sh -text\n", "*.sh -text\n" + hooks + "\n"},
		{"no final newline", "* text=auto", "* text=auto\n" + sh + "\n" + hooks + "\n"},
		{"crlf document", "a\r\n", "a\r\n" + sh + "\r\n" + hooks + "\r\n"},
		{"crlf without final newline", "a\r\nb", "a\r\nb\r\n" + sh + "\r\n" + hooks + "\r\n"},
		{"both present", sh + "\n" + hooks + "\n", sh + "\n" + hooks + "\n"},
		{"hooks rule followed by -text", sh + "\n" + hooks + "\n.claude/hooks/lighthouse/** -text\n", sh + "\n" + hooks + "\n.claude/hooks/lighthouse/** -text\n"},
		{"hooks pattern without eol", sh + "\n.claude/hooks/lighthouse/** -text\n", sh + "\n.claude/hooks/lighthouse/** -text\n" + hooks + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in []byte
			if tc.old != "" {
				in = []byte(tc.old)
			}
			if got := string(ensureGitattributes(in)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := ensureGitattributes(nil); string(got) != "*.sh eol=lf\n.claude/hooks/lighthouse/** eol=lf\n" {
		t.Errorf("nil: %q", got)
	}
}

func TestInitWritesGitattributes(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "", all(yes))
	if r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	got := read(t, root, ".gitattributes")
	for _, l := range []string{"*.sh eol=lf", ".claude/hooks/lighthouse/** eol=lf"} {
		if !strings.Contains(got, l+"\n") {
			t.Errorf(".gitattributes lacks %q:\n%s", l, got)
		}
	}
	if !strings.Contains(r.out, ".gitattributes") {
		t.Errorf("output does not name .gitattributes:\n%s", r.out)
	}
	r = run(t, root, "", all(yes, upgrade))
	if r.code != 0 || !strings.Contains(r.out, "No changes.") {
		t.Errorf("upgrade rc %d, want No changes.:\n%s%s", r.code, r.out, r.err)
	}
}

func TestInitNoPackKeepsGitattributes(t *testing.T) {
	root := newRepo(t)
	write(t, root, "pyproject.toml", "")
	if r := run(t, root, "", all(yes, withPython)); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	before := read(t, root, ".gitattributes")
	if r := run(t, root, "", all(yes, upgrade, withPython, dropPack("python"))); r.code != 0 {
		t.Fatalf("no-pack: %s", r.err)
	}
	if after := read(t, root, ".gitattributes"); after != before {
		t.Errorf(".gitattributes changed by --no-pack:\n%s\nwas:\n%s", after, before)
	}
}

func TestMissingGitattributes(t *testing.T) {
	const sh, hooks = "*.sh eol=lf", ".claude/hooks/lighthouse/** eol=lf"
	for _, tc := range []struct {
		name, old string
		want      []string
	}{
		{"empty", "", []string{sh, hooks}},
		{"sh present", sh + "\n", []string{hooks}},
		{"both present", sh + "\n" + hooks + "\n", nil},
		{"hooks pattern without eol", sh + "\n.claude/hooks/lighthouse/** -text\n", []string{hooks}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in []byte
			if tc.old != "" {
				in = []byte(tc.old)
			}
			got := missingGitattributes(in)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
