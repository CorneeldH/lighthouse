package labels

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
)

var names = []string{
	"blocked-issue", "blocked-external", "needs-mind", "needs-hands",
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

// fakeForge is the forge Sync talks to; a method it does not define panics on the nil embedded
// interface.
type fakeForge struct {
	forge.Forge
	remote []string
	// createErr is returned by CreateLabel; listErr by Labels.
	createErr, listErr error
	listed             int
	created            []Label
}

func (f *fakeForge) Labels(context.Context) ([]string, error) {
	f.listed++
	return f.remote, f.listErr
}

func (f *fakeForge) CreateLabel(_ context.Context, name, color string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, Label{name, color})
	return nil
}

func (f *fakeForge) DeleteLabel(context.Context, string) error { return nil }

func (f *fakeForge) creates() []Label { return f.created }

type result struct {
	out, err string
	rc       error
}

func setup(t *testing.T, toml string, taxonomy bool) string {
	t.Helper()
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

func run(root string, create bool, f forge.Forge) result {
	var out, errw bytes.Buffer
	rc := Sync(root, create, f, &out, &errw)
	return result{out.String(), errw.String(), rc}
}

func createLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "lighthouse forge label create ") {
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
	f := &fakeForge{remote: []string{"needs-hands"}}
	root := setup(t, tomlForge, true)
	r := run(root, false, f)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	lines := createLines(r.out)
	if len(lines) != 3 {
		t.Fatalf("%d lines:\n%s", len(lines), r.out)
	}
	for _, l := range lines {
		if !strings.Contains(l, " --color ") || strings.Contains(l, "--repo") || strings.Contains(l, "needs-hands") {
			t.Errorf("bad line %q", l)
		}
	}
	if !strings.Contains(r.out, "lighthouse forge label create blocked-issue --color D93F0B\n") {
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
	f := &fakeForge{remote: append([]string{}, names...)}
	root := setup(t, tomlForge, true)
	p := filepath.Join(root, projectconfig.DefaultLabelsPath)
	b, _ := os.ReadFile(p)
	extra := "\n| Label | Color |\n|---|---|\n| `good first issue` | `7057FF` |\n"
	if err := os.WriteFile(p, append(b, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(root, false, f)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	want := "lighthouse forge label create 'good first issue' --color 7057FF\n"
	if !strings.Contains(r.out, want) {
		t.Errorf("no quoted line %q:\n%s", want, r.out)
	}
}

func TestCreateMakesOnlyMissing(t *testing.T) {
	f := &fakeForge{remote: []string{"needs-hands"}}
	root := setup(t, tomlForge, true)
	r := run(root, true, f)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	creates := f.creates()
	if len(creates) != 3 {
		t.Fatalf("%d creates: %v", len(creates), creates)
	}
	for _, c := range creates {
		if c.Name == "needs-hands" || len(c.Color) != 6 {
			t.Errorf("bad create %v", c)
		}
	}
	if !strings.Contains(r.out, "labels: created blocked-issue\n") {
		t.Errorf("out:\n%s", r.out)
	}
}

func TestExistingLabelCaseInsensitive(t *testing.T) {
	f := &fakeForge{remote: []string{"Needs-Hands"}}
	root := setup(t, tomlForge, true)
	if r := run(root, true, f); r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	for _, c := range f.creates() {
		if c.Name == "needs-hands" {
			t.Errorf("created an existing label: %v", c)
		}
	}
	if len(f.creates()) != 3 {
		t.Errorf("%d creates, want 3", len(f.creates()))
	}
}

func TestAllPresent(t *testing.T) {
	f := &fakeForge{remote: names}
	root := setup(t, tomlForge, true)
	r := run(root, true, f)
	if r.rc != nil {
		t.Fatal(r.rc)
	}
	if !strings.Contains(r.out, "labels: all 4 present on Owner/repo") {
		t.Errorf("out:\n%s", r.out)
	}
	if len(f.creates()) != 0 {
		t.Errorf("creates: %v", f.creates())
	}
}

func TestForgeListErrorPrintsAllLines(t *testing.T) {
	f := &fakeForge{listErr: errors.New("GET /labels: 401")}
	root := setup(t, tomlForge, true)
	r := run(root, true, f)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	if !strings.Contains(r.out, "could not read") {
		t.Errorf("out:\n%s", r.out)
	}
	if n := len(createLines(r.out)); n != 4 {
		t.Errorf("%d lines, want 4", n)
	}
	if len(f.creates()) != 0 {
		t.Errorf("creates: %v", f.creates())
	}
}

func TestCreateFailurePrintsLine(t *testing.T) {
	f := &fakeForge{createErr: errors.New("POST /labels: 403")}
	root := setup(t, tomlForge, true)
	r := run(root, true, f)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	lines := createLines(r.out)
	for _, n := range names {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, "lighthouse forge label create "+n+" ") {
				found = true
			}
		}
		if !found {
			t.Errorf("no line for %s:\n%s", n, r.out)
		}
	}
}

func TestNoForgeRepoPrintsAllLines(t *testing.T) {
	root := setup(t, tomlNoForge, true)
	r := run(root, true, nil)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	lines := createLines(r.out)
	if len(lines) != 4 {
		t.Fatalf("%d lines:\n%s", len(lines), r.out)
	}
	for _, l := range lines {
		if strings.Contains(l, "--repo") || strings.HasPrefix(l, "gh ") {
			t.Errorf("--repo or gh in %q", l)
		}
	}
}

// A nil forge (no token, or one that could not be opened) prints every label as a command that
// works on both forges, and never touches a forge.
func TestSyncFallbackPrintsForgeCommand(t *testing.T) {
	root := setup(t, tomlForge, true)
	r := run(root, true, nil)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	if n := len(createLines(r.out)); n != 4 {
		t.Errorf("%d lines, want 4:\n%s", n, r.out)
	}
	if strings.Contains(r.out, "gh ") || !strings.Contains(r.out, "could not read the labels of Owner/repo") {
		t.Errorf("out:\n%s", r.out)
	}
}

// A name with a colon and capitals goes to the forge as written.
func TestSyncKeepsNameWithColonAndCapitals(t *testing.T) {
	f := &fakeForge{}
	root := setup(t, tomlForge, true)
	p := filepath.Join(root, projectconfig.DefaultLabelsPath)
	b, _ := os.ReadFile(p)
	extra := "\n| Label | Color |\n|---|---|\n| `1-location:local` | `0e8a16` |\n| `Area:Core` | `AABBCC` |\n"
	if err := os.WriteFile(p, append(b, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(root, true, f); r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	got := map[string]string{}
	for _, c := range f.creates() {
		got[c.Name] = c.Color
	}
	if got["1-location:local"] != "0E8A16" || got["Area:Core"] != "AABBCC" {
		t.Errorf("created %v", got)
	}
	r := run(root, false, nil)
	if !strings.Contains(r.out, "lighthouse forge label create 1-location:local --color 0E8A16\n") ||
		!strings.Contains(r.out, "lighthouse forge label create Area:Core --color AABBCC\n") {
		t.Errorf("out:\n%s", r.out)
	}
}

// Both forges: the missing label is created, the existing one (other case) is not, and the
// colour reaches the server in the forge's own form (GitLab with a "#", GitHub without).
func TestSyncCreatesMissingOnForge(t *testing.T) {
	text := "| Label | Color |\n|---|---|\n| `needs-hands` | `d93f0b` |\n| `1-location:local` | `0e8a16` |\n"
	setupTaxonomy := func(t *testing.T) string {
		root := setup(t, "[forge]\nrepo = \"group/project\"\n", false)
		p := filepath.Join(root, projectconfig.DefaultLabelsPath)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	t.Run("github", func(t *testing.T) {
		srv := forgetest.New(t)
		srv.Labels = []string{"Needs-Hands"}
		f, err := forge.Open(forge.Config{Kind: "github", Repo: "group/project", Token: "t", BaseURL: srv.URL()})
		if err != nil {
			t.Fatal(err)
		}
		r := run(setupTaxonomy(t), true, f)
		if r.rc != nil || !strings.Contains(r.out, "labels: created 1-location:local\n") || strings.Contains(r.out, "created needs-hands") {
			t.Fatalf("rc %v\n%s\n%s", r.rc, r.out, r.err)
		}
		if got := srv.LabelColors["1-location:local"]; got != "0E8A16" {
			t.Errorf("colour on the server = %q, want 0E8A16", got)
		}
	})
	t.Run("gitlab", func(t *testing.T) {
		srv := gitlabtest.New(t)
		srv.SetLabels("Needs-Hands")
		f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "group/project", Token: "t", BaseURL: srv.URL()})
		if err != nil {
			t.Fatal(err)
		}
		r := run(setupTaxonomy(t), true, f)
		if r.rc != nil || !strings.Contains(r.out, "labels: created 1-location:local\n") || strings.Contains(r.out, "created needs-hands") {
			t.Fatalf("rc %v\n%s\n%s", r.rc, r.out, r.err)
		}
		if got := srv.LabelColor("1-location:local"); got != "#0E8A16" {
			t.Errorf("colour on the server = %q, want #0E8A16", got)
		}
	})
}

func TestMissingTaxonomyIsError(t *testing.T) {
	f := &fakeForge{}
	root := setup(t, tomlForge, false)
	r := run(root, false, f)
	if r.rc == nil {
		t.Fatal("no error")
	}
	if !strings.Contains(r.err, projectconfig.DefaultLabelsPath) {
		t.Errorf("stderr %q lacks %s", r.err, projectconfig.DefaultLabelsPath)
	}
}

func TestRemoteLabelWithSpace(t *testing.T) {
	f := &fakeForge{remote: append([]string{"good first issue"}, names...)}
	root := setup(t, tomlForge, true)
	p := filepath.Join(root, projectconfig.DefaultLabelsPath)
	b, _ := os.ReadFile(p)
	extra := "\n| Label | Color |\n|---|---|\n| `good first issue` | `7057FF` |\n"
	if err := os.WriteFile(p, append(b, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(root, true, f)
	if r.rc != nil {
		t.Fatal(r.rc, r.err)
	}
	if len(f.creates()) != 0 {
		t.Errorf("creates: %v", f.creates())
	}
	if !strings.Contains(r.out, "labels: all 5 present on Owner/repo") {
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
