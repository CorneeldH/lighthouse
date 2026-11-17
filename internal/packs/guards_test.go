package packs

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CorneeldH/lighthouse/pack"
)

const guardsTOML = `[x]
events = ["PreToolUse"]
matcher = "Bash"
does = "d"
terms = [["a"], ["b", "c"]]
`

func TestLoadReadsGuards(t *testing.T) {
	fsys := fstest.MapFS{"guards.toml": {Data: []byte(guardsTOML)}}
	p, err := Load(fsys, "x", "dev")
	if err != nil {
		t.Fatal(err)
	}
	want := []Guard{{
		Name:    "x",
		Events:  []string{"PreToolUse"},
		Matcher: "Bash",
		Does:    "d",
		Terms:   [][]string{{"a"}, {"b", "c"}},
	}}
	if !reflect.DeepEqual(p.Guards, want) {
		t.Errorf("Guards = %+v, want %+v", p.Guards, want)
	}
	for _, u := range p.Units {
		if strings.Contains(u.Path, "guards.toml") {
			t.Errorf("guards.toml is installed as %s", u.Path)
		}
	}
}

func TestLoadSortsGuardsByName(t *testing.T) {
	data := guardsTOML + "\n[b]\nevents = [\"Stop\"]\nmatcher = \"\"\ndoes = \"d\"\nterms = [[\"a\"]]\n\n[a]\nevents = [\"Stop\"]\nmatcher = \"\"\ndoes = \"d\"\nterms = [[\"a\"]]\n"
	p, err := Load(fstest.MapFS{"guards.toml": {Data: []byte(data)}}, "x", "dev")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range p.Guards {
		names = append(names, g.Name)
	}
	if want := []string{"a", "b", "x"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestLoadRejectsBadGuard(t *testing.T) {
	cases := map[string]string{
		"no terms":    "[x]\nevents = [\"Stop\"]\nmatcher = \"\"\ndoes = \"d\"\n",
		"empty group": "[x]\nevents = [\"Stop\"]\nmatcher = \"\"\ndoes = \"d\"\nterms = [[\"a\"], []]\n",
		"empty term":  "[x]\nevents = [\"Stop\"]\nmatcher = \"\"\ndoes = \"d\"\nterms = [[\"\"]]\n",
		"no events":   "[x]\nmatcher = \"\"\ndoes = \"d\"\nterms = [[\"a\"]]\n",
		"unknown key": "[x]\nevents = [\"Stop\"]\nmatcher = \"\"\ndoes = \"d\"\nterms = [[\"a\"]]\nbogus = 1\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(fstest.MapFS{"guards.toml": {Data: []byte(data)}}, "x", "dev")
			if err == nil || !strings.Contains(err.Error(), "guards.toml") {
				t.Errorf("err = %v, want an error naming guards.toml", err)
			}
		})
	}
}

func TestEveryBlockingHookHasAGuard(t *testing.T) {
	core, err := fs.Sub(pack.Files, "core")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(core, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, g := range p.Guards {
		have[g.Name] = true
	}
	blocking, err := fs.ReadFile(core, "hooks/blocking")
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Fields(string(blocking))
	if len(names) == 0 {
		t.Fatal("hooks/blocking is empty")
	}
	for _, name := range names {
		if !have[name] {
			t.Errorf("blocking hook %s has no guard in core/guards.toml", name)
		}
	}

	goPack, err := fs.Sub(pack.Files, "go")
	if err != nil {
		t.Fatal(err)
	}
	gp, err := Load(goPack, "go", "dev")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range gp.Guards {
		found = found || g.Name == "go-lint-gate"
	}
	if !found {
		t.Error("go pack has no guard go-lint-gate")
	}
}
