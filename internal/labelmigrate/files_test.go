package labelmigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeToml(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadOldNamesWithoutLabelsTable(t *testing.T) {
	got, err := ReadOldNames(writeToml(t, "[forge]\nrepo = \"a/b\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != OldDefaults {
		t.Fatalf("got %+v, want %+v", got, OldDefaults)
	}
}

func TestReadOldNamesCustom(t *testing.T) {
	got, err := ReadOldNames(writeToml(t, "[labels]\nneeds_shaping = \"3-needs-shaping\"\nneeds_mind = \"x\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Shaping != "3-needs-shaping" || got.Decision != OldDefaults.Decision || got.Testing != OldDefaults.Testing {
		t.Fatalf("got %+v", got)
	}
}

func TestReadOldNamesMissingFile(t *testing.T) {
	if _, err := ReadOldNames(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("want error for a missing file")
	}
}

const taxonomy = "# Labels\n\nintro line\n\n" +
	"| Label | Color | Meaning |\n|---|---|---|\n" +
	"| `3-needs-shaping` | `D93F0B` | shape it |\n" +
	"| `3-needs-decision` | `D93F0B` | decide |\n" +
	"| `3-needs-testing` | `0E8A16` | test |\n" +
	"| `3-needs-hands` | `0E8A16` | hands |\n" +
	"| `2-area:web` | `AAAAAA` | web |\n\ntrailer\n"

var old = OldNames{"3-needs-shaping", "3-needs-decision", "3-needs-testing"}

func TestRewriteTaxonomyMergesRows(t *testing.T) {
	got, err := RewriteTaxonomy([]byte(taxonomy), old, "3-needs-mind", "3-needs-hands")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Labels\n\nintro line\n\n" +
		"| Label | Color | Meaning |\n|---|---|---|\n" +
		"| `3-needs-mind` | `FBCA04` | shape it |\n" +
		"| `3-needs-hands` | `0E8A16` | hands |\n" +
		"| `2-area:web` | `AAAAAA` | web |\n\ntrailer\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRewriteTaxonomyRenamesTestingWithoutHands(t *testing.T) {
	src := "| Label | Colour |\n|---|---|\n| `3-needs-testing` | `0E8A16` |\n"
	got, err := RewriteTaxonomy([]byte(src), old, "3-needs-mind", "3-needs-hands")
	if err != nil {
		t.Fatal(err)
	}
	if want := "| Label | Colour |\n|---|---|\n| `3-needs-hands` | `0E8A16` |\n"; string(got) != want {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteTaxonomyIdempotent(t *testing.T) {
	once, err := RewriteTaxonomy([]byte(taxonomy), old, "3-needs-mind", "3-needs-hands")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := RewriteTaxonomy(once, old, "3-needs-mind", "3-needs-hands")
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Fatalf("not idempotent:\n%s\n---\n%s", once, twice)
	}
	if strings.Count(string(once), "3-needs-mind") != 1 {
		t.Fatalf("want one mind row:\n%s", once)
	}
}

func TestRewriteTaxonomyNoOldRowsUnchanged(t *testing.T) {
	src := "no table here\n| a | b |\n"
	got, _ := RewriteTaxonomy([]byte(src), old, "m", "h")
	if string(got) != src {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteLabelsTable(t *testing.T) {
	src := "[forge]\nrepo = \"a/b\"\n\n[labels]\n# human labels\nneeds_shaping = \"3-needs-shaping\"\nneeds_decision = \"3-needs-decision\"\nneeds_testing = \"3-needs-testing\"\nneeds_hands = \"3-needs-hands\"\narea_prefix = \"2-area:\"\n\n[paths]\nneeds_shaping = \"keep\"\n"
	got, err := RewriteLabelsTable([]byte(src), "3-needs-mind")
	if err != nil {
		t.Fatal(err)
	}
	want := "[forge]\nrepo = \"a/b\"\n\n[labels]\n# human labels\nneeds_mind = \"3-needs-mind\"\nneeds_hands = \"3-needs-hands\"\narea_prefix = \"2-area:\"\n\n[paths]\nneeds_shaping = \"keep\"\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	again, _ := RewriteLabelsTable(got, "3-needs-mind")
	if string(again) != string(got) {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

func TestRewriteLabelsTableHeaderWithComment(t *testing.T) {
	src := "[labels] # human labels\nneeds_shaping = \"3-needs-shaping\"\narea_prefix = \"2-area:\"\n\n[paths] # p\nneeds_shaping = \"keep\"\n"
	got, err := RewriteLabelsTable([]byte(src), "3-needs-mind")
	if err != nil {
		t.Fatal(err)
	}
	want := "[labels] # human labels\nneeds_mind = \"3-needs-mind\"\narea_prefix = \"2-area:\"\n\n[paths] # p\nneeds_shaping = \"keep\"\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRewriteLabelsTableHeaderWithCommentKeepsExistingMind(t *testing.T) {
	src := "[labels] # x\nneeds_mind = \"3-needs-mind\"\nneeds_shaping = \"3-needs-shaping\"\n"
	got, _ := RewriteLabelsTable([]byte(src), "3-needs-mind")
	if want := "[labels] # x\nneeds_mind = \"3-needs-mind\"\n"; string(got) != want {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteLabelsTableHandsWithExistingMind(t *testing.T) {
	src := "[labels]\nneeds_mind = \"9-needs-mind\"\nneeds_testing = \"9-needs-testing\"\n"
	got, _ := RewriteLabelsTableHands([]byte(src), "9-needs-mind", "9-needs-hands")
	if want := "[labels]\nneeds_mind = \"9-needs-mind\"\nneeds_hands = \"9-needs-hands\"\n"; string(got) != want {
		t.Fatalf("got %q", got)
	}
	again, _ := RewriteLabelsTableHands(got, "9-needs-mind", "9-needs-hands")
	if string(again) != string(got) {
		t.Fatalf("not idempotent: %q", again)
	}
}
