package roborev

import (
	"reflect"
	"testing"
)

// Python's splitlines also breaks on \r, \v, \f and the Unicode separators, so a review with
// such a character yields the same lines as the script.
func TestSplitLinesBreaksLikePython(t *testing.T) {
	got := splitLines("a\r\nb\rc\x0bd e\n\nf\n")
	want := []string{"a", "b", "c", "d", "e", "", "f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitLines = %q, want %q", got, want)
	}
}

func TestClassifyJSONKeyOrder(t *testing.T) {
	obj := Classify("## roborev: Review Complete (`abc1234`)\n\n### Low\n\n- no location here\n").JSON()
	if got, want := obj.Keys(), []string{"verdict", "sha", "kind", "findings"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	list, _ := obj.Get("findings")
	first := list.([]any)[0].(interface{ Keys() []string })
	if got, want := first.Keys(), []string{"path", "line", "severity", "text"}; !reflect.DeepEqual(got, want) {
		t.Errorf("finding keys = %v, want %v", got, want)
	}
}

func TestCountCodeLinesIgnoresMalformedEntries(t *testing.T) {
	if got := CountCodeLines("1\t2\tx.go\x00garbage\x00a\tb\ty.go\x00"); got != 3 {
		t.Errorf("CountCodeLines = %d, want 3", got)
	}
}
