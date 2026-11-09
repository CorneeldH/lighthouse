package kpi

import (
	"reflect"
	"testing"
)

func TestEncode(t *testing.T) {
	got := encode(object{{"a", 1}, {"b", object{}}, {"c", []string{}}, {"d", nil}, {"e", pyFloat(1)}, {"f", "x \t\"\\\x01é\b\f <>&"}})
	want := "{\n  \"a\": 1,\n  \"b\": {},\n  \"c\": [],\n  \"d\": null,\n  \"e\": 1.0,\n  \"f\": \"x \\t\\\"\\\\\\u0001é\\b\\f <>&\"\n}"
	if got != want {
		t.Errorf("encode =\n%q\nwant\n%q", got, want)
	}
}

func TestEncodeNested(t *testing.T) {
	got := encode(object{{"l", []string{"x", "y"}}, {"o", object{{"k", true}}}})
	want := "{\n  \"l\": [\n    \"x\",\n    \"y\"\n  ],\n  \"o\": {\n    \"k\": true\n  }\n}"
	if got != want {
		t.Errorf("encode =\n%q\nwant\n%q", got, want)
	}
}

func TestPyRound(t *testing.T) {
	for _, tt := range []struct {
		x    float64
		n    int
		want float64
	}{{2.25, 1, 2.2}, {0.125, 2, 0.12}, {2.675, 2, 2.67}, {0.35, 1, 0.3}, {2.0 / 3, 3, 0.667}} {
		if got := pyRound(tt.x, tt.n); got != tt.want {
			t.Errorf("pyRound(%v, %d) = %v, want %v", tt.x, tt.n, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		name string
		got  object
		want object
	}{
		{"empty", summary(nil), object{{"n", 0}}},
		{"odd", summary([]int{4, 1, 3}), object{{"n", 3}, {"min", 1}, {"median", 3}, {"max", 4}, {"mean", pyFloat(2.7)}}},
		{"even", summary([]int{1, 2, 2, 4}), object{{"n", 4}, {"min", 1}, {"median", pyFloat(2)}, {"max", 4}, {"mean", pyFloat(2.2)}}},
		{"int even", summaryInt([]int{1, 2, 3, 4}), object{{"n", 4}, {"median", 2}, {"max", 4}}},
		{"int empty", summaryInt(nil), object{{"n", 0}, {"median", nil}, {"max", nil}}},
		{"float", summaryFloat([]float64{1.25, 3.75}), object{{"n", 2}, {"median", pyFloat(2.5)}, {"max", pyFloat(3.8)}}},
		{"float empty", summaryFloat(nil), object{{"n", 0}, {"median", nil}, {"max", nil}}},
	}
	for _, tt := range tests {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Errorf("%s: got %#v, want %#v", tt.name, tt.got, tt.want)
		}
	}
}

func TestPyPath(t *testing.T) {
	for in, want := range map[string]string{"./gone/": "gone", "a//b": "a/b", "a/../b": "a/../b", "/": "/", "": ".", "//a": "//a", "///a": "/a"} {
		if got := pyPath(in); got != want {
			t.Errorf("pyPath(%q) = %q, want %q", in, got, want)
		}
	}
}
