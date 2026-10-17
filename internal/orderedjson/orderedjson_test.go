package orderedjson

import (
	"strings"
	"testing"
)

func roundTrip(t *testing.T, in string) string {
	t.Helper()
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse(%q): %v", in, err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

func TestRoundTripKeepsKeyOrder(t *testing.T) {
	got := roundTrip(t, `{"z":1,"a":{"y":2,"b":3},"m":[{"q":1,"c":2}]}`)
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"y\": 2,\n    \"b\": 3\n  },\n  \"m\": [\n    {\n      \"q\": 1,\n      \"c\": 2\n    }\n  ]\n}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNumbersSurvive(t *testing.T) {
	got := roundTrip(t, `{"a":15,"b":1.50,"c":-2e3}`)
	for _, lit := range []string{`"a": 15`, `"b": 1.50`, `"c": -2e3`} {
		if !strings.Contains(got, lit) {
			t.Errorf("missing %s in\n%s", lit, got)
		}
	}
}

func TestNoHTMLEscape(t *testing.T) {
	got := roundTrip(t, `{"a":"<&> \"$X\""}`)
	if !strings.Contains(got, `"<&> \"$X\""`) {
		t.Fatalf("escaped: %s", got)
	}
}

func TestEmptyContainers(t *testing.T) {
	if got := roundTrip(t, `{"a":{},"b":[]}`); got != "{\n  \"a\": {},\n  \"b\": []\n}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestScalars(t *testing.T) {
	if got := roundTrip(t, `{"a":true,"b":false,"c":null}`); got != "{\n  \"a\": true,\n  \"b\": false,\n  \"c\": null\n}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRejects(t *testing.T) {
	for _, in := range []string{
		`{"a":1,"a":2}`,
		`{"a":1} x`,
		"// c\n{}",
		`{"a":1,}`,
		``,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q): no error", in)
		}
	}
}

func TestCanonicalSortsKeys(t *testing.T) {
	v, err := Parse([]byte(`{"b":1,"a":{"d":[1,{"z":0,"y":1}],"c":"<"}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":{"c":"<","d":[1,{"y":1,"z":0}]},"b":1}`; string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestObjectSetDeleteKeys(t *testing.T) {
	o := NewObject()
	o.Set("b", 1)
	o.Set("a", 2)
	o.Set("b", 3)
	if k := strings.Join(o.Keys(), ","); k != "b,a" {
		t.Fatalf("keys %s", k)
	}
	if v, ok := o.Get("b"); !ok || v != 3 {
		t.Fatalf("get b = %v %v", v, ok)
	}
	o.Delete("b")
	if k := strings.Join(o.Keys(), ","); k != "a" {
		t.Fatalf("keys after delete %s", k)
	}
	if _, ok := o.Get("b"); ok {
		t.Fatal("b still there")
	}
}
