package projectconfig

import (
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
	out, added, _ := appendMissing(t, "", CoreTables("acme/w", true))
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
	out, added, manual := appendMissing(t, paiaShaped, CoreTables("acme/w", true))
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
	out, added, manual := appendMissing(t, in, CoreTables("acme/w", true))
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
	out, _, _, err := AppendMissing([]byte("[forge\n"), CoreTables("a/b", true))
	if err == nil || out != nil {
		t.Fatalf("err %v out %q", err, out)
	}
}

func TestForgeRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/w.git":         "acme/w",
		"https://github.com/acme/w":             "acme/w",
		"https://github.com/acme/w/":            "acme/w",
		"git@github.com:acme/w.git":             "acme/w",
		"ssh://git@github.com/acme/w":           "acme/w",
		"https://github.com/Acme-1/my.repo.git": "Acme-1/my.repo",
	} {
		if got, ok := ForgeRepo(in); !ok || got != want {
			t.Errorf("ForgeRepo(%q) = %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"https://gitlab.com/a/b", "", "https://github.com/a", "https://github.com/a/b/c", "git@github.com:a/b\"c"} {
		if got, ok := ForgeRepo(in); ok {
			t.Errorf("ForgeRepo(%q) = %q, want false", in, got)
		}
	}
}

func TestCoreTablesWithoutRepo(t *testing.T) {
	tables := CoreTables("", false)
	if len(tables) != 1 || tables[0].Name != "project" {
		t.Fatalf("tables %+v", tables)
	}
}
