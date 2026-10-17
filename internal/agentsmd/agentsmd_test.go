package agentsmd

import (
	"strings"
	"testing"
)

const block = "<!-- lighthouse:begin core -->\nx\n<!-- lighthouse:end core -->\n"

func set(t *testing.T, doc, body string) string {
	t.Helper()
	out, err := Set([]byte(doc), "core", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestMarkers(t *testing.T) {
	if Begin("core") != "<!-- lighthouse:begin core -->" || End("core") != "<!-- lighthouse:end core -->" {
		t.Fatal(Begin("core"), End("core"))
	}
}

func TestSetAppendsToExistingDoc(t *testing.T) {
	if got := set(t, "# Project\n\nText.\n", "x\n"); got != "# Project\n\nText.\n\n"+block {
		t.Fatalf("got %q", got)
	}
}

func TestSetAppendsToDocWithoutFinalNewline(t *testing.T) {
	if got := set(t, "# Project", "x\n"); got != "# Project\n\n"+block {
		t.Fatalf("got %q", got)
	}
}

func TestSetOnEmptyDoc(t *testing.T) {
	if got := set(t, "", "x\n"); got != block {
		t.Fatalf("got %q", got)
	}
}

func TestSetReplacesOnlyInside(t *testing.T) {
	doc := "before\n\n<!-- lighthouse:begin core -->\nold\nlines\n<!-- lighthouse:end core -->\n\nafter  \n"
	want := "before\n\n<!-- lighthouse:begin core -->\nnew\n<!-- lighthouse:end core -->\n\nafter  \n"
	if got := set(t, doc, "new\n"); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestFind(t *testing.T) {
	body, found, err := Find([]byte("a\n"+block+"b\n"), "core")
	if err != nil || !found || string(body) != "x\n" {
		t.Fatalf("find = %q %v %v", body, found, err)
	}
	_, found, err = Find([]byte("a\n"), "core")
	if err != nil || found {
		t.Fatalf("found in doc without block: %v %v", found, err)
	}
}

func TestFindToleratesCRLF(t *testing.T) {
	doc := "intro\r\n\r\n<!-- lighthouse:begin core -->\r\nx\r\n<!-- lighthouse:end core -->  \r\noutro\r\n"
	body, found, err := Find([]byte(doc), "core")
	if err != nil || !found || string(body) != "x\n" {
		t.Fatalf("find = %q %v %v", body, found, err)
	}
	out := set(t, doc, "y\n")
	if !strings.HasPrefix(out, "intro\r\n\r\n<!-- lighthouse:begin core -->\r\n") || !strings.HasSuffix(out, "outro\r\n") {
		t.Fatalf("CRLF outside the block lost: %q", out)
	}
}

func TestFindIgnoresOtherPackages(t *testing.T) {
	doc := "<!-- lighthouse:begin python -->\np\n<!-- lighthouse:end python -->\n"
	if _, found, err := Find([]byte(doc), "core"); err != nil || found {
		t.Fatalf("found = %v err = %v", found, err)
	}
	out := set(t, doc, "x\n")
	if !strings.HasPrefix(out, doc) {
		t.Fatalf("python block changed: %q", out)
	}
}

func TestMalformed(t *testing.T) {
	cases := map[string]string{
		"begin without end": "a\n<!-- lighthouse:begin core -->\nx\n",
		"end without begin": "a\n<!-- lighthouse:end core -->\n",
		"twice":             block + block,
		"nested":            "<!-- lighthouse:begin core -->\n<!-- lighthouse:begin python -->\n<!-- lighthouse:end python -->\n<!-- lighthouse:end core -->\n",
		"wrong end":         "<!-- lighthouse:begin core -->\n<!-- lighthouse:end python -->\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := Find([]byte(doc), "core")
			if err == nil || !strings.Contains(err.Error(), "AGENTS.md:") {
				t.Fatalf("err = %v", err)
			}
			if _, err := Set([]byte(doc), "core", []byte("y\n")); err == nil {
				t.Fatal("Set accepted a malformed doc")
			}
		})
	}
}

func TestMalformedNamesTheLine(t *testing.T) {
	_, _, err := Find([]byte("a\nb\n<!-- lighthouse:end core -->\n"), "core")
	if err == nil || !strings.Contains(err.Error(), "AGENTS.md:3:") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemove(t *testing.T) {
	doc := "# Project\n\nText.\n\n" + block + "\nTail.\n"
	out, err := Remove([]byte(doc), "core")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# Project\n\nText.\n\nTail.\n" {
		t.Fatalf("got %q", out)
	}
	if again, err := Remove(out, "core"); err != nil || string(again) != string(out) {
		t.Fatalf("remove of absent block changed doc: %q %v", again, err)
	}
}

func TestSetThenRemoveRestores(t *testing.T) {
	doc := "# Project\n\nText.\n"
	out, err := Remove([]byte(set(t, doc, "x\n")), "core")
	if err != nil || string(out) != doc {
		t.Fatalf("got %q %v", out, err)
	}
}

func TestSetUsesTheDocNewline(t *testing.T) {
	doc := "intro\r\n"
	out := set(t, doc, "a\nb\n")
	if strings.Count(out, "\n") != strings.Count(out, "\r\n") {
		t.Fatalf("mixed endings: %q", out)
	}
	replaced := set(t, out, "c\n")
	if strings.Count(replaced, "\n") != strings.Count(replaced, "\r\n") || !strings.Contains(replaced, "\r\nc\r\n") {
		t.Fatalf("mixed endings on replace: %q", replaced)
	}
}
