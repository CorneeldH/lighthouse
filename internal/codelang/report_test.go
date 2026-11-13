package codelang

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Classification: this is a hook/gate change; the likely regression surface is the report on
// stderr, the notice and the findings layer 2 adds (the tests of code_language.py that ran
// main() and resolve()).

const (
	dutchComment = "# dit is niet goed\nx = 1\n"
	layer1Line   = "lees_bestand = 1\n"
	doubtLine    = "legacy_processed_lijst_weg = 2\n"
)

// llmTable is _table of the Python tests: the chain at url with the test key.
func llmTable(url string) string {
	return "\n[code_language_llm]\nurl = \"" + url + "\"\nmodels = [\"m1\"]\nkey_env = \"LLM_TEST_KEY\"\n"
}

// sessionTOML writes the session repo's lighthouse.toml with table and returns its path.
func sessionTOML(t *testing.T, dir, table string) string {
	t.Helper()
	path := filepath.Join(dir, "session", "lighthouse.toml")
	writeFile(t, path, []byte(minimalTOML+table))
	return path
}

// resolveFile is _resolve of the Python tests: Check with the session's chain, then Resolve.
func resolveFile(t *testing.T, root, path, session string, underGates bool) ([]Finding, string) {
	t.Helper()
	in, err := Check(DefaultScorer, path, root, session)
	if err != nil {
		t.Fatal(err)
	}
	return Resolve(context.Background(), in, path, underGates, t.TempDir())
}

type lineLayer struct {
	Line  int
	Layer string
}

func linesLayers(found []Finding) []lineLayer {
	out := []lineLayer{}
	for _, f := range found {
		out = append(out, lineLayer{f.Line, f.Layer})
	}
	return out
}

func TestFindingLayer(t *testing.T) {
	root := gitRepo(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(dutchComment))
	res := hookOn(t, path)
	lines := strings.Split(res.Stderr, "\n")
	if res.Code != 2 || len(lines) < 2 || lines[1] != path+":1: comment: dit is niet goed [layer 1]" {
		t.Fatalf("code %d stderr %q, want 2 and the comment as layer 1 on line 2", res.Code, res.Stderr)
	}
}

func TestMainOutput(t *testing.T) {
	root := gitRepo(t, "")
	var many strings.Builder
	for n := range 25 {
		fmt.Fprintf(&many, "# dit is niet goed\nx%d = 1\n", n)
	}
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(many.String()))
	res := hookOn(t, path)
	if res.Code != 2 || !strings.HasSuffix(res.Stderr, "\n") {
		t.Fatalf("code %d stderr %q, want 2 and a report ending in a newline", res.Code, res.Stderr)
	}
	out := strings.Split(strings.TrimSuffix(res.Stderr, "\n"), "\n")
	comments := 0
	for _, line := range out {
		if strings.Contains(line, ": comment: ") {
			comments++
		}
	}
	last, secondLast := out[len(out)-1], out[len(out)-2]
	switch {
	case out[0] != "code-language-check: "+path+" is not English":
		t.Errorf("first line %q", out[0])
	case !strings.HasPrefix(out[1], path+":1: comment: "):
		t.Errorf("second line %q", out[1])
	case comments != 20:
		t.Errorf("%d comment lines, want 20", comments)
	case secondLast != "… and 5 more":
		t.Errorf("second-last line %q", secondLast)
	case last != `The whole file must be English. A line that is deliberately not English gets "lighthouse: allow-language".`:
		t.Errorf("last line %q", last)
	}

	clean := filepath.Join(root, "b.py")
	writeFile(t, clean, []byte("x = 1\n"))
	if res := hookOn(t, clean); !reflect.DeepEqual(res, emptyResult()) {
		t.Errorf("clean file: %+v, want nothing", res)
	}

	broken := filepath.Join(root, "c.py")
	writeFile(t, broken, []byte("def f(:\n"))
	res = hookOn(t, broken)
	if res.Code != 2 || res.Output != nil || res.Text != "" ||
		!strings.HasPrefix(res.Stderr, "code-language-check: COULD NOT CHECK — ") ||
		strings.Count(res.Stderr, "\n") != 1 || !strings.HasSuffix(res.Stderr, "\n") {
		t.Errorf("syntax error: %+v, want 2 and one COULD NOT CHECK line", res)
	}
}

func TestReportExplainsModuleNameFinding(t *testing.T) {
	root := gitRepo(t, "")
	path := filepath.Join(root, "lees_bestand.py")
	writeFile(t, path, []byte("x = 1\n"))
	res := hookOn(t, path)
	out := strings.Split(strings.TrimSuffix(res.Stderr, "\n"), "\n")
	want := "The module name 'lees_bestand' is not English either; the marker cannot clear it: " +
		"rename the file or add it to `code_language_exempt`."
	if res.Code != 2 || len(out) < 2 || !strings.HasPrefix(out[len(out)-2], "The whole file must be English.") ||
		out[len(out)-1] != want {
		t.Fatalf("module name: code %d stderr %q", res.Code, res.Stderr)
	}

	other := filepath.Join(root, "b.py")
	writeFile(t, other, []byte(dutchComment))
	if res := hookOn(t, other); res.Code != 2 || strings.Contains(res.Stderr, "rename the file") {
		t.Errorf("no module name finding: code %d stderr %q", res.Code, res.Stderr)
	}
}

func TestReportNamesPath(t *testing.T) {
	root := gitRepo(t, "code_language_paths = [\"pack/**\"]\n")
	path := filepath.Join(root, "pack", "bestanden", "a.toml")
	writeFile(t, path, []byte("x = 1\n"))
	res := hookOn(t, path)
	out := strings.Split(strings.TrimSuffix(res.Stderr, "\n"), "\n")
	want := "The directory name 'bestanden' is not English either; the marker cannot clear it: " +
		"rename the file or its directory, or add the file to `code_language_exempt`."
	if res.Code != 2 || !strings.Contains(res.Stderr, "\n"+path+":1: directory: bestanden [layer 1]\n") ||
		out[len(out)-1] != want {
		t.Errorf("code %d stderr %q", res.Code, res.Stderr)
	}
}

// The limit counts runes, as Python's len does: 70 two-byte runes are cut to 60, a name never.
func TestReportCutsAtSixtyRunes(t *testing.T) {
	long := strings.Repeat("é", 70)
	got := Report("/r/a.py", []Finding{
		{Line: 3, Kind: "comment", Text: long, Layer: "layer 1"},
		{Line: 4, Kind: "name", Text: long, Layer: "layer 1"},
	})
	want := "code-language-check: /r/a.py is not English\n" +
		"/r/a.py:3: comment: " + strings.Repeat("é", 60) + " [layer 1]\n" +
		"/r/a.py:4: name: " + long + " [layer 1]\n" +
		`The whole file must be English. A line that is deliberately not English gets "lighthouse: allow-language".`
	if got != want {
		t.Errorf("report\n%s\nwant\n%s", got, want)
	}
}

func TestNotENBecomesFinding(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "answer")
	dir := t.TempDir()
	session := sessionTOML(t, dir, llmTable(fake.url))
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	found, notice := resolveFile(t, root, path, session, false)
	if got, want := linesLayers(found), []lineLayer{{1, "layer 1"}, {2, "layer 2 (m1)"}}; !reflect.DeepEqual(got, want) || notice != "" {
		t.Fatalf("found %v notice %q, want %v and none", got, notice, want)
	}
	if found[1].Doubt {
		t.Errorf("the layer 2 finding is still a doubt: %+v", found[1])
	}
	prompt := fake.seen()[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(prompt, "legacy_processed_lijst_weg") || strings.Contains(prompt, "lees_bestand") {
		t.Errorf("prompt %q: want the doubt and not the layer 1 finding", prompt)
	}
}

// Layer 2 hears the whole unit, not only the first line the report names it by.
func TestLayer2HearsTheWholeUnit(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "answer")
	in := Inspection{
		Doubts: []Finding{{Line: 2, Kind: "comment", Text: "first line", Body: "first line\nsecond line", Layer: "layer 1", Doubt: true}},
		Chain:  testChain(fake.url),
	}
	found, notice := Resolve(context.Background(), in, "/r/a.py", false, t.TempDir())
	if len(found) != 1 || found[0].Layer != "layer 2 (m1)" || notice != "" {
		t.Fatalf("found %+v notice %q", found, notice)
	}
	prompt := fake.seen()[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(prompt, "second line") {
		t.Errorf("prompt %q lacks the second line of the unit", prompt)
	}
}

// A module name that is also a name on line 1 is two findings, but one explanation.
func TestReportExplainsModuleNameOnce(t *testing.T) {
	got := Report("/r/lees_bestand.py", []Finding{
		{Line: 1, Kind: "name", Text: "lees_bestand", Layer: "layer 1"},
		{Line: 1, Kind: "name", Text: "lees_bestand", Layer: "layer 1"},
	})
	if n := strings.Count(got, "The module name 'lees_bestand'"); n != 1 {
		t.Errorf("module name explained %d times, want once:\n%s", n, got)
	}
}

func TestENDropsDoubt(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "en")
	session := sessionTOML(t, t.TempDir(), llmTable(fake.url))
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	found, notice := resolveFile(t, root, path, session, false)
	if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != "" || len(fake.seen()) != 1 {
		t.Errorf("found %v notice %q requests %d, want layer 1 only, no notice, one request", got, notice, len(fake.seen()))
	}
}

func TestLayer1OnlyNoCall(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "answer")
	session := sessionTOML(t, t.TempDir(), llmTable(fake.url))
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line))
	found, notice := resolveFile(t, root, path, session, false)
	if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != "" || len(fake.seen()) != 0 {
		t.Errorf("found %v notice %q requests %d, want layer 1 only and no call", got, notice, len(fake.seen()))
	}
}

// The written file's repo cannot choose where the session's key goes (R7).
func TestForeignChainIsNeverUsed(t *testing.T) {
	withKey(t)
	foreign := newFakeLLM(t, nil, "answer")
	own := newFakeLLM(t, nil, "en")
	root := repoWith(t, llmTable(foreign.url))
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	dir := t.TempDir()
	session := sessionTOML(t, dir, "")
	found, notice := resolveFile(t, root, path, session, false)
	if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != "" {
		t.Fatalf("no session chain: found %v notice %q", got, notice)
	}
	session = sessionTOML(t, dir, llmTable(own.url))
	found, notice = resolveFile(t, root, path, session, false)
	if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != "" {
		t.Errorf("session chain: found %v notice %q", got, notice)
	}
	if len(foreign.seen()) != 0 || !reflect.DeepEqual(own.authorization(), []string{"Bearer sekrit"}) {
		t.Errorf("foreign requests %d, own authorization %v", len(foreign.seen()), own.authorization())
	}
}

func TestNoChainIsSilent(t *testing.T) {
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	in, err := Check(DefaultScorer, path, root, "")
	if err != nil || len(in.Doubts) != 1 || in.Chain != nil {
		t.Fatalf("inspection %+v, %v; want one doubt and no chain", in, err)
	}
	found, notice := Resolve(context.Background(), in, path, false, t.TempDir())
	if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != "" {
		t.Errorf("found %v notice %q, want layer 1 only and no notice", got, notice)
	}
}

func TestFailedChainNotice(t *testing.T) {
	for _, c := range []struct {
		mode   string
		key    bool
		reason string
	}{
		{"http500", true, "m1: HTTP 500"},
		{"garbage", true, "m1: unreadable answer"},
		{"answer", false, "key: LLM_TEST_KEY is set in neither the environment nor env_file"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			if c.key {
				withKey(t)
			} else {
				t.Setenv("LLM_TEST_KEY", "")
			}
			fake := newFakeLLM(t, nil, c.mode)
			session := sessionTOML(t, t.TempDir(), llmTable(fake.url))
			root := repoWith(t, "")
			path := filepath.Join(root, "a.py")
			writeFile(t, path, []byte(layer1Line+doubtLine))
			found, notice := resolveFile(t, root, path, session, false)
			want := "code-language-check: layer 2 did not answer (" + c.reason + "); 1 doubtful units not checked:\n" +
				path + ":2: name: legacy_processed_lijst_weg"
			if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != want {
				t.Errorf("found %v notice\n%q\nwant\n%q", got, notice, want)
			}
			if !c.key && len(fake.seen()) != 0 {
				t.Errorf("%d requests without a key, want none", len(fake.seen()))
			}
		})
	}
}

func TestUnderGatesSkips(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "answer")
	session := sessionTOML(t, t.TempDir(), llmTable(fake.url))
	root := repoWith(t, "")
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	found, notice := resolveFile(t, root, path, session, true)
	want := "code-language-check: NOTICE — layer 2 did not answer (layer 2 skipped under " +
		"bash-write-check); 1 doubtful units not checked:\n" + path + ":2: name: legacy_processed_lijst_weg"
	if got := linesLayers(found); !reflect.DeepEqual(got, []lineLayer{{1, "layer 1"}}) || notice != want || len(fake.seen()) != 0 {
		t.Errorf("found %v notice %q requests %d", got, notice, len(fake.seen()))
	}
}

func TestNoticeTruncatesLongText(t *testing.T) {
	long := strings.Repeat("x", 70)
	in := Inspection{
		Doubts: []Finding{{Line: 3, Kind: "comment", Text: long, Body: long, Layer: "layer 1", Doubt: true}},
		Chain:  &Chain{URL: "u", Models: []string{"m1"}},
	}
	_, notice := Resolve(context.Background(), in, "/r/a.py", true, t.TempDir())
	if lines := strings.Split(notice, "\n"); len(lines) != 2 || lines[1] != "/r/a.py:3: comment: "+strings.Repeat("x", 60) {
		t.Errorf("notice %q", notice)
	}
}

func TestMainNoticeContract(t *testing.T) {
	root := gitRepo(t, "")
	t.Setenv("LIGHTHOUSE_UNDER_BASH_WRITE_CHECK", "1")
	t.Setenv("LIGHTHOUSE_TOML", sessionTOML(t, t.TempDir(), llmTable("http://unused.invalid/v1")))
	path := filepath.Join(root, "a.py")
	writeFile(t, path, []byte(doubtLine))
	res := hookOn(t, path)
	want := "code-language-check: NOTICE — layer 2 did not answer (layer 2 skipped under " +
		"bash-write-check); 1 doubtful units not checked:\n" + path + ":1: name: legacy_processed_lijst_weg\n"
	if res.Code != 0 || res.Text != want || res.Output != nil || res.Stderr != "" {
		t.Errorf("notice only: %+v, want Text %q", res, want)
	}

	path = filepath.Join(root, "b.py")
	writeFile(t, path, []byte(layer1Line+doubtLine))
	res = hookOn(t, path)
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, "code-language-check: "+path+" is not English\n") ||
		!strings.HasSuffix(res.Stderr, "allow-language\".\n\ncode-language-check: NOTICE — layer 2 did not answer "+
			"(layer 2 skipped under bash-write-check); 1 doubtful units not checked:\n"+
			path+":2: name: legacy_processed_lijst_weg\n") || res.Text != "" {
		t.Errorf("findings and notice: %+v", res)
	}
}
