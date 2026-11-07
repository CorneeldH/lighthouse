package projectconfig

import (
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func appendMissing(t *testing.T, in string, want []Table) (string, []string, []string) {
	t.Helper()
	out, added, manual, err := AppendMissing([]byte(in), want)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	md, err := toml.Decode(string(out), &v)
	if err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, out)
	}
	for _, k := range added {
		if !md.IsDefined(strings.Split(k, ".")...) {
			t.Fatalf("added %s is not defined in\n%s", k, out)
		}
	}
	return string(out), added, manual
}

func TestAppendToEmpty(t *testing.T) {
	out, added, _ := appendMissing(t, "", CoreTables(Forge{Kind: "github", Repo: "acme/w"}, true))
	if want := "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"acme/w\"\n"; out != want {
		t.Fatalf("got %q", out)
	}
	if strings.Join(added, ",") != "project.language,forge.kind,forge.repo" {
		t.Fatalf("added %v", added)
	}
}

const paiaShaped = `# Project config. Committed.

[forge]
repo = "acme/w"   # the repo

[forge.board]
project_number = 1

[controller]
state_dir = "x/controller"
# knobs follow
plekken = 4
`

func TestAppendKeepsEverything(t *testing.T) {
	out, added, manual := appendMissing(t, paiaShaped, CoreTables(Forge{Kind: "github", Repo: "acme/w"}, true))
	want := strings.Replace(paiaShaped, "repo = \"acme/w\"   # the repo\n", "repo = \"acme/w\"   # the repo\nkind = \"github\"\n", 1) + "\n[project]\nlanguage = \"en\"\n"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if strings.Join(added, ",") != "project.language,forge.kind" || len(manual) != 0 {
		t.Fatalf("added %v manual %v", added, manual)
	}
}

func TestAppendBeforeTrailingComments(t *testing.T) {
	in := "[forge]\nrepo = \"a/b\"\n\n# about the next table\n[labels]\nx = \"y\"\n"
	out, _, _ := appendMissing(t, in, []Table{{Name: "forge", Keys: []KV{{"kind", `"github"`}}}})
	if want := "[forge]\nrepo = \"a/b\"\nkind = \"github\"\n\n# about the next table\n[labels]\nx = \"y\"\n"; out != want {
		t.Fatalf("got %q", out)
	}
}

func TestAppendToEmptySection(t *testing.T) {
	out, _, _ := appendMissing(t, "[forge]\n[labels]\n", []Table{{Name: "forge", Keys: []KV{{"kind", `"github"`}}}})
	if want := "[forge]\nkind = \"github\"\n[labels]\n"; out != want {
		t.Fatalf("got %q", out)
	}
}

func TestAppendWithoutFinalNewline(t *testing.T) {
	out, _, _ := appendMissing(t, "[labels]\nx = \"y\"", []Table{{Name: "project", Keys: []KV{{"language", `"en"`}}}})
	if want := "[labels]\nx = \"y\"\n\n[project]\nlanguage = \"en\"\n"; out != want {
		t.Fatalf("got %q", out)
	}
}

func TestAppendNothingMissing(t *testing.T) {
	in := "[project]\nlanguage = \"nl\"\n\n[forge]\nkind = \"github\"\nrepo = \"x/y\"\n"
	out, added, manual := appendMissing(t, in, CoreTables(Forge{Kind: "github", Repo: "acme/w"}, true))
	if out != in || len(added) != 0 || len(manual) != 0 {
		t.Fatalf("out %q added %v manual %v", out, added, manual)
	}
}

func TestManual(t *testing.T) {
	for _, in := range []string{
		"forge = { repo = \"a/b\" }\n",
		"forge.repo = \"a/b\"\n",
		"[forge.board]\nproject_number = 1\n",
	} {
		out, added, manual := appendMissing(t, in, []Table{{Name: "forge", Keys: []KV{{"kind", `"github"`}, {"repo", `"a/b"`}}}})
		if out != in || len(added) != 0 || strings.Join(manual, ",") == "" || !strings.Contains(strings.Join(manual, ","), "forge.kind") {
			t.Errorf("in %q: out %q added %v manual %v", in, out, added, manual)
		}
	}
}

func TestInvalidTOML(t *testing.T) {
	out, _, _, err := AppendMissing([]byte("[forge\n"), CoreTables(Forge{Kind: "github", Repo: "a/b"}, true))
	if err == nil || out != nil {
		t.Fatalf("err %v out %q", err, out)
	}
}

func TestForgeFromRemote(t *testing.T) {
	const own = "git.example.org"
	cases := []struct {
		name, in string
		hosts    []string
		want     Forge
		ok       bool
	}{
		{"github https .git", "https://github.com/acme/w.git", nil, Forge{"github", "acme/w", ""}, true},
		{"github https", "https://github.com/acme/w", nil, Forge{"github", "acme/w", ""}, true},
		{"github slash", "https://github.com/acme/w/", nil, Forge{"github", "acme/w", ""}, true},
		{"github scp", "git@github.com:acme/w.git", nil, Forge{"github", "acme/w", ""}, true},
		{"github ssh", "ssh://git@github.com/acme/w", nil, Forge{"github", "acme/w", ""}, true},
		{"github chars", "https://github.com/Acme-1/my.repo.git", nil, Forge{"github", "Acme-1/my.repo", ""}, true},
		{"gitlab https subgroup", "https://gitlab.com/g/s/r.git/", nil, Forge{"gitlab", "g/s/r", ""}, true},
		{"gitlab scp", "git@gitlab.com:g/s/r.git", nil, Forge{"gitlab", "g/s/r", ""}, true},
		{"gitlab ssh", "ssh://git@gitlab.com/g/s/r", nil, Forge{"gitlab", "g/s/r", ""}, true},
		{"gitlab two segments", "https://gitlab.com/g/r", nil, Forge{"gitlab", "g/r", ""}, true},
		{"gitlab host case", "https://GitLab.com/g/r", nil, Forge{"gitlab", "g/r", ""}, true},
		{"own https", "https://git.example.org/g/s/r", []string{own}, Forge{"gitlab", "g/s/r", own}, true},
		{"own scp", "git@git.example.org:g/r.git", []string{own}, Forge{"gitlab", "g/r", own}, true},
		{"own ssh port", "ssh://git@git.example.org:2222/g/s/r.git", []string{own}, Forge{"gitlab", "g/s/r", own}, true},
		{"own https port", "https://git.example.org:8443/g/r", []string{own}, Forge{"gitlab", "g/r", own}, true},
		{"own mixed case", "https://Git.Example.org/g/r", []string{"GIT.example.org"}, Forge{"gitlab", "g/r", own}, true},
		{"own after junk host", "https://git.example.org/g/r", []string{"bad host", "", own}, Forge{"gitlab", "g/r", own}, true},
		{"gitlab.com in list", "https://gitlab.com/g/r", []string{"gitlab.com"}, Forge{"gitlab", "g/r", ""}, true},
		{"gitlab one segment", "https://gitlab.com/solo", nil, Forge{}, false},
		{"github three segments", "https://github.com/a/b/c", nil, Forge{}, false},
		{"github one segment", "https://github.com/a", nil, Forge{}, false},
		{"unknown host", "git@git.example.org:g/s/r.git", nil, Forge{}, false},
		{"other known host", "git@git.example.org:g/r.git", []string{"other.org"}, Forge{}, false},
		{"quote in segment", "https://gitlab.com/g/s\"/r", nil, Forge{}, false},
		{"angle in segment", "git@gitlab.com:g/<x>/r", nil, Forge{}, false},
		{"github quote", "git@github.com:a/b\"c", nil, Forge{}, false},
		{"empty segment", "https://gitlab.com/g//r", nil, Forge{}, false},
		{"regex host not a wildcard", "https://gitXexample.org/g/r", []string{own}, Forge{}, false},
		{"hostile host entry ignored", "https://gitlab.com/g/r", []string{"a\"b"}, Forge{"gitlab", "g/r", ""}, true},
		{"empty", "", nil, Forge{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ForgeFromRemote(c.in, c.hosts)
			if ok != c.ok || got != c.want {
				t.Errorf("ForgeFromRemote(%q, %v) = %+v %v, want %+v %v", c.in, c.hosts, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestCoreTablesWithoutRepo(t *testing.T) {
	tables := CoreTables(Forge{}, false)
	if len(tables) != 1 || tables[0].Name != "project" {
		t.Fatalf("tables %+v", tables)
	}
}

func TestCoreTablesForge(t *testing.T) {
	cases := []struct {
		name string
		f    Forge
		want []KV
	}{
		{"github", Forge{"github", "a/b", ""}, []KV{{"kind", `"github"`}, {"repo", `"a/b"`}}},
		{"gitlab.com", Forge{"gitlab", "g/s/r", ""}, []KV{{"kind", `"gitlab"`}, {"repo", `"g/s/r"`}}},
		{"own host", Forge{"gitlab", "g/r", "git.example.org"}, []KV{{"kind", `"gitlab"`}, {"repo", `"g/r"`}, {"host", `"git.example.org"`}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tables := CoreTables(c.f, true)
			if len(tables) != 2 || tables[1].Name != "forge" || !slices.Equal(tables[1].Keys, c.want) {
				t.Fatalf("tables %+v, want forge keys %v", tables, c.want)
			}
		})
	}
}

func TestParseFragment(t *testing.T) {
	got, err := ParseFragment([]byte("# c\n[test_gate]\ncommand = \"uv run pytest -q\"\n\nunchecked_exit_codes = [3, 4, 5]\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Table{{Name: "test_gate", Keys: []KV{{"command", `"uv run pytest -q"`}, {"unchecked_exit_codes", "[3, 4, 5]"}}}}
	if len(got) != 1 || got[0].Name != want[0].Name || len(got[0].Keys) != 2 || got[0].Keys[0] != want[0].Keys[0] || got[0].Keys[1] != want[0].Keys[1] {
		t.Fatalf("got %#v", got)
	}
}

func TestParseFragmentTwoTables(t *testing.T) {
	got, err := ParseFragment([]byte("[a]\nx = 1\n[b.c]\ny = \"z\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Name != "b.c" || got[1].Keys[0] != (KV{"y", `"z"`}) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseFragmentRejects(t *testing.T) {
	cases := map[string]struct{ in, line string }{
		"key before header":   {"a = 1\n[t]\nb = 2\n", "line 1"},
		"multi-line value":    {"[t]\na = [1,\n2]\n", "line 2"},
		"empty table":         {"[t]\n[u]\nb = 1\n", "line 1"},
		"empty last table":    {"[t]\nb = 1\n[u]\n", "line 3"},
		"table twice":         {"[t]\na = 1\n[t]\nb = 1\n", "line 3"},
		"key twice":           {"[t]\na = 1\na = 2\n", "line 3"},
		"array of tables":     {"[[t]]\na = 1\n", "line 1"},
		"dotted key":          {"[t]\na.b = 1\n", "line 2"},
		"upper-case table":    {"[T]\na = 1\n", "line 1"},
		"no value":            {"[t]\na = \n", "line 2"},
		"trailing comment":    {"[t]\na = 1 # c\n", "line 2"},
		"subtable of a table": {"[t]\na = 1\n[t.a]\nb = 1\n", "line 3"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFragment([]byte(c.in))
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), c.line) {
				t.Fatalf("error %q does not name %s", err, c.line)
			}
		})
	}
}
