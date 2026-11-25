package history

import (
	"fmt"
	"testing"
)

func TestHits_Caps(t *testing.T) {
	var h Hits
	for f := 1; f <= 7; f++ {
		for l := 1; l <= 12; l++ {
			h.Add(fmt.Sprintf("f%d", f), l, fmt.Sprintf("2026-10-01T10:%02d:00Z", f))
		}
	}
	top := h.Top()
	if len(top) != 5 {
		t.Fatalf("files %d", len(top))
	}
	if top[0].File != "f7" || top[4].File != "f3" {
		t.Fatalf("order %v", top)
	}
	if len(top[0].Lines) != 10 || top[0].Lines[0] != 12 || top[0].Lines[9] != 3 {
		t.Fatalf("lines %v", top[0].Lines)
	}
}

func TestHits_TimestampBeatsCallOrder(t *testing.T) {
	var h Hits
	h.Add("new", 1, "2026-10-02T10:00:00Z")
	h.Add("old", 1, "2026-10-01T10:00:00Z")
	if top := h.Top(); top[0].File != "new" {
		t.Fatalf("%v", top)
	}
	var g Hits
	g.Add("a", 1, "")
	g.Add("b", 1, "")
	if top := g.Top(); top[0].File != "b" {
		t.Fatalf("%v", top)
	}
}
