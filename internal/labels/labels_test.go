package labels

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/projectconfig"
)

var names = []string{
	"blocked-issue", "blocked-external", "needs-shaping",
	"needs-decision", "needs-hands", "needs-testing",
}

const (
	tomlForge   = "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Owner/repo\"\n"
	tomlNoForge = "[project]\nlanguage = \"en\"\n"
)

func template(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "pack", "core", "templates", "labels.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type fakeGH struct {
	calls  [][]string
	remote []string
	// createErr is returned for `label create`; listErr for `api`.
	createErr, listErr error
}

func (f *fakeGH) install(t *testing.T) {
	t.Helper()
	old := runGH
	t.Cleanup(func() { runGH = old })
	runGH = func(args []string) (string, error) {
		f.calls = append(f.calls, args)
		switch {
		case len(args) > 0 && args[0] == "api":
			if f.listErr != nil {
				return "", f.listErr
			}
			return strings.Join(f.remote, "\n") + "\n", nil
		case len(args) > 1 && args[0] == "label" && args[1] == "create":
			return "", f.createErr
		}
		return "", nil
	}
}

func (f *fakeGH) creates() [][]string {
	var out [][]string
	for _, c := range f.calls {
		if len(c) > 1 && c[0] == "label" && c[1] == "create" {
			out = append(out, c)
		}
	}
	return out
}

type result struct {
	out, err string
	rc       error
}

func setup(t *testing.T, f *fakeGH, toml string, taxonomy bool) string {
	t.Helper()
	f.install(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lighthouse.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if taxonomy {
		p := filepath.Join(root, projectconfig.DefaultLabelsPath)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(template(t)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func run(root string, create bool) result {
	var out, errw bytes.Buffer
	rc := Sync(root, create, &out, &errw)
	return result{out.String(), errw.String(), rc}
}

func createLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "gh label create ") {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestParseTemplate(t *testing.T) {
	parsed := ParseTaxonomy(template(t))
	var got []string
	for _, l := range parsed {
		got = append(got, l.Name)
		if len(l.Color) != 6 || l.Color != strings.ToUpper(l.Color) {
			t.Errorf("color %q is not six upper-case hex", l.Color)
		}
	}
	want := append([]string{}, names...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestListsMissingWithoutCreate(t *testing.T) {
	f := &fakeGH{remote: []string{"needs-hands"}}
	root := setup(t, f, tomlForge, true)
	r := run(root, false)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	lines := createLines(r.out)
	if len(lines) != 5 {
		t.Fatalf("%d lines:\n%s", len(lines), r.out)
	}
	for _, l := range lines {
		if !strings.Contains(l, " --repo Owner/repo --color ") || strings.Contains(l, "needs-hands") {
			t.Errorf("bad line %q", l)
		}
	}
	if !strings.Contains(r.out, "gh label create blocked-issue --repo Owner/repo --color D93F0B\n") {
		t.Errorf("no blocked-issue line:\n%s", r.out)
	}
	if len(f.creates()) != 0 {
		t.Fatalf("created without --create: %v", f.creates())
	}
}

func TestShellJoin(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"space", "good first issue", `'good first issue'`},
		{"apostrophe", "it's", `'it'"'"'s'`},
		{"empty", "", `''`},
		{"safe", "a-b_c.d", "a-b_c.d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shellJoin([]string{c.in}); got != c.want {
				t.Errorf("shellJoin(%q) = %s, want %s", c.in, got, c.want)
			}
		})
	}
}

func TestListsMissingLabelWithSpaceQuoted(t *testing.T) {
	f := &fakeGH{remote: append([]string{}, names...)}
	root := setup(t, f, tomlForge, true)
	p := filepath.Join(root, projectconfig.DefaultLabelsPath)
	b, _ := os.ReadFile(p)
	extra := "\n| Label | Color |\n|---|---|\n| `good first issue` | `7057FF` |\n"
	if err := os.WriteFile(p, append(b, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(root, false)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	want := "gh label create 'good first issue' --repo Owner/repo --color 7057FF\n"
	if !strings.Contains(r.out, want) {
		t.Errorf("no quoted line %q:\n%s", want, r.out)
	}
}

func TestCreateMakesOnlyMissing(t *testing.T) {
	f := &fakeGH{remote: []string{"needs-hands"}}
	root := setup(t, f, tomlForge, true)
	r := run(root, true)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	creates := f.creates()
	if len(creates) != 5 {
		t.Fatalf("%d creates: %v", len(creates), creates)
	}
	for _, c := range creates {
		j := strings.Join(c, " ")
		if strings.Contains(j, "needs-hands") || !strings.Contains(j, "--color") {
			t.Errorf("bad create %q", j)
		}
	}
	if !strings.Contains(r.out, "labels: created blocked-issue\n") {
		t.Errorf("out:\n%s", r.out)
	}
}

func TestExistingLabelCaseInsensitive(t *testing.T) {
	f := &fakeGH{remote: []string{"Needs-Hands"}}
	root := setup(t, f, tomlForge, true)
	if r := run(root, true); r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	for _, c := range f.creates() {
		if strings.Contains(strings.Join(c, " "), "needs-hands") {
			t.Errorf("created an existing label: %v", c)
		}
	}
	if len(f.creates()) != 5 {
		t.Errorf("%d creates, want 5", len(f.creates()))
	}
}

func TestAllPresent(t *testing.T) {
	f := &fakeGH{remote: names}
	root := setup(t, f, tomlForge, true)
	r := run(root, true)
	if r.rc != nil {
		t.Fatal(r.rc)
	}
	if !strings.Contains(r.out, "labels: all 6 present on Owner/repo") {
		t.Errorf("out:\n%s", r.out)
	}
	if len(f.creates()) != 0 {
		t.Errorf("creates: %v", f.creates())
	}
}

func TestReadsLabelsPaginated(t *testing.T) {
	f := &fakeGH{}
	root := setup(t, f, tomlForge, true)
	run(root, false)
	if len(f.calls) == 0 {
		t.Fatal("no gh call")
	}
	j := strings.Join(f.calls[0], " ")
	if f.calls[0][0] != "api" || !strings.Contains(j, "--paginate") || !strings.Contains(j, "repos/Owner/repo/labels") {
		t.Errorf("first call %q", j)
	}
}

func TestGHMissingPrintsAllLines(t *testing.T) {
	f := &fakeGH{listErr: errors.New("gh did not run: not found")}
	root := setup(t, f, tomlForge, true)
	r := run(root, true)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	if !strings.Contains(r.out, "could not read") {
		t.Errorf("out:\n%s", r.out)
	}
	if n := len(createLines(r.out)); n != 6 {
		t.Errorf("%d lines, want 6", n)
	}
	if len(f.creates()) != 0 {
		t.Errorf("creates: %v", f.creates())
	}
}

func TestCreateFailurePrintsLine(t *testing.T) {
	f := &fakeGH{createErr: errors.New("gh exited 1")}
	root := setup(t, f, tomlForge, true)
	r := run(root, true)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	lines := createLines(r.out)
	for _, n := range names {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, "gh label create "+n+" ") {
				found = true
			}
		}
		if !found {
			t.Errorf("no line for %s:\n%s", n, r.out)
		}
	}
}

func TestNoForgeRepoPrintsAllLines(t *testing.T) {
	f := &fakeGH{}
	root := setup(t, f, tomlNoForge, true)
	r := run(root, true)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	lines := createLines(r.out)
	if len(lines) != 6 {
		t.Fatalf("%d lines:\n%s", len(lines), r.out)
	}
	for _, l := range lines {
		if strings.Contains(l, "--repo") {
			t.Errorf("--repo in %q", l)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("gh called: %v", f.calls)
	}
}

func TestMissingTaxonomyIsError(t *testing.T) {
	f := &fakeGH{}
	root := setup(t, f, tomlForge, false)
	r := run(root, false)
	if r.rc == nil {
		t.Fatal("no error")
	}
	if !strings.Contains(r.err, projectconfig.DefaultLabelsPath) {
		t.Errorf("stderr %q lacks %s", r.err, projectconfig.DefaultLabelsPath)
	}
}

func TestRemoteLabelWithSpace(t *testing.T) {
	f := &fakeGH{remote: append([]string{"good first issue"}, names...)}
	root := setup(t, f, tomlForge, true)
	p := filepath.Join(root, projectconfig.DefaultLabelsPath)
	b, _ := os.ReadFile(p)
	extra := "\n| Label | Color |\n|---|---|\n| `good first issue` | `7057FF` |\n"
	if err := os.WriteFile(p, append(b, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(root, true)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	if len(f.creates()) != 0 {
		t.Errorf("creates: %v", f.creates())
	}
	if !strings.Contains(r.out, "labels: all 7 present on Owner/repo") {
		t.Errorf("out:\n%s", r.out)
	}
}

func TestParseTaxonomyAcceptsDutchAndAreaHeaders(t *testing.T) {
	text := "| Rang | Label | Kleur | Bindt wanneer |\n|---|---|---|---|\n" +
		"| 1 | `blocked-issue` | `d93f0b` | x |\n\n" +
		"| Area | Kleur | Vuistregel |\n|---|---|---|\n" +
		"| `area:core` | `0e8a16` | y |\n\n" +
		"| LABEL | Colour |\n|---|---|\n| `needs-hands` | `AABBCC` |\n"
	want := []Label{{"blocked-issue", "D93F0B"}, {"area:core", "0E8A16"}, {"needs-hands", "AABBCC"}}
	if got := ParseTaxonomy(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
