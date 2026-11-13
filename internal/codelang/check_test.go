package codelang

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// minimalTOML is MINIMAL_TOML of tests/pack/_testkit.py.
const minimalTOML = "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"

// row is one [line, kind, text] of check.json.
type row struct {
	Line int
	Kind string
	Text string
}

func (r *row) UnmarshalJSON(raw []byte) error {
	var fields []any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if len(fields) != 3 {
		return errors.New("a check row has three fields")
	}
	line, _ := fields[0].(float64)
	kind, _ := fields[1].(string)
	text, _ := fields[2].(string)
	*r = row{int(line), kind, text}
	return nil
}

func rows(found []Finding) []row {
	out := []row{}
	for _, f := range found {
		out = append(out, row{f.Line, f.Kind, f.Text})
	}
	return out
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// repoWith writes lighthouse.toml with code_language on and extra after it.
func repoWith(t *testing.T, extra string) string {
	t.Helper()
	root := t.TempDir()
	head := "language = \"en\"\ncode_language = \"en\"\n" + extra
	writeFile(t, filepath.Join(root, "lighthouse.toml"),
		[]byte(strings.Replace(minimalTOML, "language = \"en\"\n", head, 1)))
	return root
}

func TestCheckFixture(t *testing.T) {
	var cases []struct {
		Name         string            `json:"name"`
		TOMLExtra    string            `json:"toml_extra"`
		Off          bool              `json:"off"`
		Files        map[string]string `json:"files"`
		FilesB64     map[string]string `json:"files_b64"`
		SessionTable *string           `json:"session_table"`
		Path         string            `json:"path"`
		Findings     []row             `json:"findings"`
		Doubts       []row             `json:"doubts"`
		CannotCheck  bool              `json:"cannot_check"`
	}
	readFixture(t, "check.json", &cases)
	if len(cases) != 31 {
		t.Fatalf("check.json has %d cases, want 31", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			tmp := t.TempDir()
			root := filepath.Join(tmp, "repo")
			on := "code_language = \"en\"\n"
			if c.Off {
				on = ""
			}
			head := "language = \"en\"\n" + on + c.TOMLExtra
			writeFile(t, filepath.Join(root, "lighthouse.toml"),
				[]byte(strings.Replace(minimalTOML, "language = \"en\"\n", head, 1)))
			for rel, text := range c.Files {
				writeFile(t, filepath.Join(root, rel), []byte(text))
			}
			for rel, encoded := range c.FilesB64 {
				data, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(root, rel), data)
			}
			session := ""
			if c.SessionTable != nil {
				session = filepath.Join(tmp, "session", "lighthouse.toml")
				writeFile(t, session, []byte(minimalTOML+*c.SessionTable))
			}
			got, err := Check(DefaultScorer, filepath.Join(root, c.Path), root, session)
			if c.CannotCheck {
				var cc *CannotCheckError
				if !errors.As(err, &cc) {
					t.Fatalf("want a CannotCheckError, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if g, w := rows(got.Findings), nonNil(c.Findings); !reflect.DeepEqual(g, w) {
				t.Errorf("findings %+v, want %+v", g, w)
			}
			if g, w := rows(got.Doubts), nonNil(c.Doubts); !reflect.DeepEqual(g, w) {
				t.Errorf("doubts %+v, want %+v", g, w)
			}
		})
	}
}

func nonNil(r []row) []row {
	if r == nil {
		return []row{}
	}
	return r
}

func TestCheckOffWithoutTOML(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte("# dit is niet goed\nx = 1\n"))
	got, err := Check(DefaultScorer, path, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 0 || len(got.Doubts) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestBadChainNamesTheMissingEnvFile(t *testing.T) {
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte("x = 1\n"))
	session := filepath.Join(t.TempDir(), "session", "lighthouse.toml")
	writeFile(t, session, []byte(minimalTOML+"\n[code_language_llm]\nurl = \"http://h/v1\"\nmodels = [\"m1\"]\n"+
		"env_file = \"/nonexistent/lighthouse-test.env\"\n"))
	_, err := Check(DefaultScorer, path, root, session)
	var cc *CannotCheckError
	want := "code_language_llm.env_file does not exist: /nonexistent/lighthouse-test.env"
	if !errors.As(err, &cc) || cc.Reason != want {
		t.Fatalf("err = %v, want CannotCheckError %q", err, want)
	}
}

func TestMissingDataIsCannotCheck(t *testing.T) {
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte("x = 1\n"))
	broken := func() (*Scorer, error) { return nil, errors.New("cannot read code_language_freq.tsv.gz: gone") }
	_, err := Check(broken, path, root, "")
	var cc *CannotCheckError
	if !errors.As(err, &cc) || cc.Reason != "cannot read code_language_freq.tsv.gz: gone" {
		t.Fatalf("err = %v, want CannotCheckError with the data error", err)
	}
}

func TestOutOfScopeDoesNotLoadData(t *testing.T) {
	root := repoWith(t, "code_language_paths = [\"pack/**\"]\n")
	loaded := 0
	scorer := func() (*Scorer, error) {
		loaded++
		return nil, &CannotCheckError{Reason: "frequencies loaded"}
	}
	outside := filepath.Join(root, "docs", "a.toml")
	writeFile(t, outside, []byte("x = 1\n"))
	got, err := Check(scorer, outside, root, "")
	if err != nil || len(got.Findings) != 0 || len(got.Doubts) != 0 || loaded != 0 {
		t.Fatalf("outside: got %+v, %v, loaded %d; want nothing, no load", got, err, loaded)
	}
	inside := filepath.Join(root, "pack", "notulen.toml")
	writeFile(t, inside, []byte("x = 1\n"))
	_, err = Check(scorer, inside, root, "")
	var cc *CannotCheckError
	if !errors.As(err, &cc) || cc.Reason != "frequencies loaded" || loaded != 1 {
		t.Fatalf("inside: err %v, loaded %d; want the scorer's error after one load", err, loaded)
	}
}

func TestManifestWithoutPathIsCannotCheck(t *testing.T) {
	root := repoWith(t, "")
	writeFile(t, filepath.Join(root, ".lighthouse", "manifest.toml"), []byte("[[unit]]\nkind = \"skill\"\n"))
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte("x = 1\n"))
	_, err := Check(DefaultScorer, path, root, "")
	var cc *CannotCheckError
	want := "cannot read " + filepath.Join(root, ".lighthouse", "manifest.toml") + ": "
	if !errors.As(err, &cc) || !strings.HasPrefix(cc.Reason, want) {
		t.Fatalf("err = %v, want a CannotCheckError starting %q", err, want)
	}
}

func TestCheckNameParts(t *testing.T) {
	// The suffix and stem of pathlib: a leading dot is part of the stem, the last dot splits.
	cases := []struct {
		name, suffix, stem string
	}{
		{"lees_bestand.py", ".py", "lees_bestand"},
		{".bashrc", "", ".bashrc"},
		{"a.tar.gz", ".gz", "a.tar"},
		{"trailing.", "", "trailing."},
		{"plain", "", "plain"},
	}
	for _, c := range cases {
		if s, st := nameParts(c.name); s != c.suffix || st != c.stem {
			t.Errorf("nameParts(%q) = %q, %q; want %q, %q", c.name, s, st, c.suffix, c.stem)
		}
	}
}

func TestScopeDirsFixture(t *testing.T) {
	var cases []struct {
		Relative string    `json:"relative"`
		Globs    []string  `json:"globs"`
		Want     *[]string `json:"want"`
	}
	readFixture(t, "scope_dirs.json", &cases)
	if len(cases) == 0 {
		t.Fatal("scope_dirs.json is empty")
	}
	for _, c := range cases {
		got, ok := ScopeDirs(c.Relative, c.Globs)
		if c.Want == nil {
			if ok {
				t.Errorf("ScopeDirs(%q, %q) = %q, want None", c.Relative, c.Globs, got)
			}
			continue
		}
		if !ok || !reflect.DeepEqual(nonNilStrings(got), *c.Want) {
			t.Errorf("ScopeDirs(%q, %q) = %q, %v; want %q", c.Relative, c.Globs, got, ok, *c.Want)
		}
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		glob, name string
		want       bool
	}{
		{"pack/**", "pack/a/b.md", true},
		{"pack/**", "notes/a.md", false},
		{"*.py", "a/b.py", true}, // fnmatch: * also crosses /
		{"*.py", "a.pyc", false},
		{"?.md", "a.md", true},
		{"?.md", "ab.md", false},
		{"[!a]x", "bx", true},
		{"[!a]x", "ax", false},
		{"[a-c]x", "bx", true},
		{"[a-c]x", "dx", false},
		{"[]a]x", "]x", true},
		{"[!-a]x", "-x", false},
		{"[z-a]x", "zx", false}, // an empty range matches nothing
		{"[!z-a]x", "qx", true}, // ... and its negation any character
		// No POSIX classes: the set is "a[:digit:" and the last "]" is literal.
		{"[a[:digit:]]", "[]", true},
		{"[a[:digit:]]", "[", false},
		{"[a[:digit:]]", "5]", false},
		{"[ -#]", "\"", true}, // a range over punctuation
		{"x[", "x[", true},    // an unclosed bracket is literal
		{"x[", "xa", false},
		{"a.b", "aXb", false}, // regexp metacharacters are literal
		{"a+b", "a+b", true},
		{"tests/fixtures/*", "tests/fixtures/a/b.py", true},
		{"pack/", "pack/x", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.glob, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.glob, c.name, got, c.want)
		}
	}
}

// unitRow is one [call, text, verdict] of a units fixture.
type unitRow [3]string

func TestUnitsFixture(t *testing.T) {
	dirs := []string{filepath.Join(fixtureDir, "units")}
	if extra := os.Getenv("LIGHTHOUSE_CODELANG_UNITS"); extra != "" {
		dirs = []string{extra}
	}
	scorer, err := DefaultScorer()
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, dir := range dirs {
		found, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	if len(files) == 0 {
		t.Fatalf("no units fixtures in %q", dirs)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Source   string        `json:"source"`
				Language string        `json:"lang"`
				Units    []unitRow     `json:"units"`
				Found    []wantFinding `json:"found"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			if want := strings.ReplaceAll(fx.Source, "/", "__") + ".json"; filepath.Base(file) != want {
				t.Fatalf("file name %s, want %s", filepath.Base(file), want)
			}
			src, err := os.ReadFile(filepath.Join(repoRoot, fx.Source))
			if err != nil {
				t.Fatal(err)
			}
			units, err := scanUnits(fx.Language, string(src))
			if err != nil {
				t.Fatal(err)
			}
			got := []unitRow{}
			for _, u := range units {
				if u.Name {
					got = append(got, unitRow{"name", u.Text, string(scorer.JudgeName(u.Text))})
				} else {
					got = append(got, unitRow{"text", u.Body, string(scorer.JudgeText(u.Body, u.LineBreaks))})
				}
			}
			sort.Slice(got, func(i, j int) bool {
				for k := range 3 {
					if got[i][k] != got[j][k] {
						return got[i][k] < got[j][k]
					}
				}
				return false
			})
			if want := nonNilUnits(fx.Units); !reflect.DeepEqual(got, want) {
				t.Errorf("units differ\n%s", unitsDiff(got, want))
			}
			found, err := Scan(scorer, fx.Language, string(src))
			if err != nil {
				t.Fatal(err)
			}
			gotFound := []wantFinding{}
			for _, f := range found {
				gotFound = append(gotFound, wantFinding{f.Line, f.Kind, f.Text, f.Doubt})
			}
			wantFound := fx.Found
			if wantFound == nil {
				wantFound = []wantFinding{}
			}
			if !reflect.DeepEqual(gotFound, wantFound) {
				t.Errorf("found %+v, want %+v", gotFound, wantFound)
			}
		})
	}
}

func nonNilUnits(u []unitRow) []unitRow {
	if u == nil {
		return []unitRow{}
	}
	return u
}

// unitsDiff lists the rows only one side has, as a multiset.
func unitsDiff(got, want []unitRow) string {
	count := map[unitRow]int{}
	for _, u := range got {
		count[u]++
	}
	for _, u := range want {
		count[u]--
	}
	var lines []string
	for u, n := range count {
		switch {
		case n > 0:
			lines = append(lines, "  only Go:     "+strings.Join(u[:], " | "))
		case n < 0:
			lines = append(lines, "  only Python: "+strings.Join(u[:], " | "))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
