package initcmd

import (
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/created"
)

// packsWithPermissions is the test pack plus one permissions.allow rule.
func packsWithPermissions() func(*Options) {
	return func(o *Options) {
		m := testPacks()
		m["core/permissions.json"] = file(`{"allow": ["Bash(lighthouse status)"]}`)
		o.Packs = m
		o.Yes = true
	}
}

func readCreated(t *testing.T, root string) created.Created {
	t.Helper()
	c, found, err := created.Read(root)
	if err != nil || !found {
		t.Fatalf("created.Read: found=%v err=%v\n%s", found, err, read(t, root, created.Path))
	}
	return c
}

func sameList(got, want []string) bool {
	return strings.Join(got, "|") == strings.Join(want, "|")
}

// Red when init writes no created.toml or records a different set.
func TestInitRecordsCreatedInEmptyRepo(t *testing.T) {
	root := newRepo(t)
	if r := run(t, root, "", packsWithPermissions()); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	c := readCreated(t, root)
	wantFiles := []string{"lighthouse.toml", "AGENTS.md", "CLAUDE.md", ".claude/settings.json", ".gitattributes"}
	if !sameList(c.Files, wantFiles) {
		t.Errorf("files = %v, want %v", c.Files, wantFiles)
	}
	if want := []string{"*.sh eol=lf", ".claude/hooks/lighthouse/** eol=lf"}; !sameList(c.Gitattributes, want) {
		t.Errorf("gitattributes = %v, want %v", c.Gitattributes, want)
	}
	if want := []string{"hooks", "permissions", "permissions.allow", "autoMemoryEnabled"}; !sameList(c.SettingsKeys, want) {
		t.Errorf("settings_keys = %v, want %v", c.SettingsKeys, want)
	}
	if c.SettingsLanguage != "" {
		t.Errorf("settings_language = %q, want none", c.SettingsLanguage)
	}
	if got := read(t, root, created.Path); !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("created.toml must end in exactly one newline: %q", got)
	}
	if u := run(t, root, "", func(o *Options) { packsWithPermissions()(o); o.Upgrade = true }); u.out != "No changes.\n" {
		t.Errorf("upgrade after init: %q", u.out)
	}
}

// Red when init records a file or setting it did not create.
func TestInitRecordsNothingForExistingFiles(t *testing.T) {
	root := newRepo(t)
	write(t, root, "AGENTS.md", "# mine\n")
	write(t, root, "lighthouse.toml", "[project]\nlanguage = \"en\"\n")
	write(t, root, ".claude/settings.json", "{\n  \"hooks\": {}\n}\n")
	write(t, root, ".gitattributes", "*.sh eol=lf\n.claude/hooks/lighthouse/** eol=lf\n")
	if r := run(t, root, "", packsWithPermissions()); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	// The one thing init adds outside the units is the permissions the project lacked.
	c := readCreated(t, root)
	if len(c.Files) != 0 || len(c.Gitattributes) != 0 || c.SettingsLanguage != "" {
		t.Errorf("recorded what already existed: %+v", c)
	}
	if want := []string{"permissions", "permissions.allow", "autoMemoryEnabled"}; !sameList(c.SettingsKeys, want) {
		t.Errorf("settings_keys = %v, want %v (hooks existed already)", c.SettingsKeys, want)
	}
}

// Red when --upgrade does not add the language it newly set, or drops earlier items.
func TestUpgradeAddsLanguageToCreated(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".claude/settings.json", "{\n  \"hooks\": {},\n  \"autoMemoryEnabled\": false\n}\n")
	initYes(t, root)
	before := readCreated(t, root)
	if len(before.SettingsKeys) != 0 {
		t.Fatalf("hooks existed already, got settings_keys %v", before.SettingsKeys)
	}
	toml := read(t, root, "lighthouse.toml")
	if !strings.Contains(toml, "language = \"en\"") {
		t.Fatalf("lighthouse.toml has no language to change:\n%s", toml)
	}
	write(t, root, "lighthouse.toml", strings.Replace(toml, "language = \"en\"", "language = \"nl\"", 1))
	if r := run(t, root, "", func(o *Options) { o.Upgrade, o.Yes = true, true }); r.code != 0 {
		t.Fatalf("upgrade: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	c := readCreated(t, root)
	if !sameList(c.SettingsKeys, []string{"language"}) || c.SettingsLanguage != "dutch" {
		t.Errorf("settings = %v / %q, want [language] / dutch", c.SettingsKeys, c.SettingsLanguage)
	}
	if !sameList(c.Files, before.Files) || !sameList(c.Gitattributes, before.Gitattributes) {
		t.Errorf("earlier items changed: before %+v, after %+v", before, c)
	}
}

// Red when a run rewrites created.toml without creating anything, or writes one for nothing.
func TestUpgradeCreatedOnlyGrows(t *testing.T) {
	root := newRepo(t)
	initYes(t, root)
	before := read(t, root, created.Path)
	if r := run(t, root, "", func(o *Options) { o.Upgrade, o.Yes = true, true }); r.code != 0 || r.out != "No changes.\n" {
		t.Fatalf("upgrade: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, created.Path); got != before {
		t.Errorf("created.toml changed:\n%s\n--- was ---\n%s", got, before)
	}

	// A file that holds the same items in another layout is left as it is.
	hand := "# kept\n" + before
	write(t, root, created.Path, hand)
	if r := run(t, root, "", func(o *Options) { o.Upgrade, o.Yes = true, true }); r.code != 0 {
		t.Fatalf("upgrade: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if got := read(t, root, created.Path); got != hand {
		t.Errorf("created.toml rewritten without news:\n%s", got)
	}

	// Everything existed already: nothing is created, so no created.toml.
	other := newRepo(t)
	write(t, other, "AGENTS.md", "# mine\n")
	write(t, other, "CLAUDE.md", "@AGENTS.md\n")
	write(t, other, "lighthouse.toml", "[project]\nlanguage = \"en\"\n")
	write(t, other, ".claude/settings.json", "{\n  \"hooks\": {},\n  \"autoMemoryEnabled\": false\n}\n")
	write(t, other, ".gitattributes", "*.sh eol=lf\n.claude/hooks/lighthouse/** eol=lf\n")
	if r := run(t, other, "", yes); r.code != 0 {
		t.Fatalf("init: rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if exists(other, created.Path) {
		t.Errorf("created.toml written although init created nothing outside the units:\n%s", read(t, other, created.Path))
	}
}
