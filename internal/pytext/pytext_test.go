package pytext

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitLinesCutsAtEveryPythonSeparator(t *testing.T) {
	in := "a\vb\fc\x1cd\x1de\x1ef\U00000085g\U00002028h\U00002029i\r\nj\rk\nl"
	want := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	if got := SplitLines(in, false); !reflect.DeepEqual(got, want) {
		t.Errorf("SplitLines = %q, want %q", got, want)
	}
	if got := SplitLines("x\r\ny\n", true); !reflect.DeepEqual(got, []string{"x\r\n", "y\n"}) {
		t.Errorf("keepends = %q", got)
	}
	if got := SplitLines("", true); len(got) != 0 {
		t.Errorf("empty input gave %q", got)
	}
	if got := SplitLines("a\n\nb", false); !reflect.DeepEqual(got, []string{"a", "", "b"}) {
		t.Errorf("blank line = %q", got)
	}
}

func TestStripAndFieldsUseThePythonWhitespaceSet(t *testing.T) {
	if got := Strip("\x1c  x y \x1f\n"); got != "x y" {
		t.Errorf("Strip = %q", got)
	}
	if got := Fields("a\x1db　c"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("Fields = %q", got)
	}
}

func TestHTMLEscapeWritesPythonEntities(t *testing.T) {
	if got := HTMLEscape(`a&b<c>"d"'e'`); got != "a&amp;b&lt;c&gt;&quot;d&quot;&#x27;e&#x27;" {
		t.Errorf("HTMLEscape = %q", got)
	}
}

func TestUniversalNewlines(t *testing.T) {
	if got := UniversalNewlines("a\r\nb\rc\n"); got != "a\nb\nc\n" {
		t.Errorf("UniversalNewlines = %q", got)
	}
}

func TestJSONStringLikeJSONDumps(t *testing.T) {
	in := "\U000000e9\U0001f600<>&\U00002028\x7f\x01\t\"\\"
	// `\x5c` is the backslash: the expectations are what Python prints, escapes included.
	asciiWant := `"\x5cu00e9\x5cud83d\x5cude00<>&\x5cu2028\x5cu007f\x5cu0001\x5ct\x5c"\x5c\x5c"`
	asciiWant = strings.ReplaceAll(asciiWant, `\x5c`, "\\")
	if got := JSONString(in, true); got != asciiWant {
		t.Errorf("ensure_ascii = %s, want %s", got, asciiWant)
	}
	rawWant := "\"\U000000e9\U0001f600<>&\U00002028\x7f" + `\u0001\t\"\\` + "\""
	if got := JSONString(in, false); got != rawWant {
		t.Errorf("raw = %s, want %s", got, rawWant)
	}
}
