package protocol

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHandoff(t *testing.T) {
	valid := func() map[string]string {
		return map[string]string{
			"issue": "#12", "worktree": "/w/x/", "branch": "issue-12-x", "anchor": "plan.md",
			"next": "/lighthouse-start-issue 12", "when": "now", "lane": "native",
		}
	}
	without := func(key string) map[string]string {
		f := valid()
		delete(f, key)
		return f
	}
	with := func(key, value string) map[string]string {
		f := valid()
		f[key] = value
		return f
	}
	tests := []struct {
		name    string
		fields  map[string]string
		want    Handoff
		wantErr string
	}{
		{"all fields", valid(), Handoff{Issue: 12, Worktree: "/w/x", Branch: "issue-12-x", Anchor: "plan.md",
			Next: "/lighthouse-start-issue 12", When: "now", Lane: "native"}, ""},
		{"bare issue number", with("issue", "7"), Handoff{Issue: 7, Worktree: "/w/x", Branch: "issue-12-x",
			Anchor: "plan.md", Next: "/lighthouse-start-issue 12", When: "now", Lane: "native"}, ""},
		{"legacy night", with("when", "night"), Handoff{Issue: 12, Worktree: "/w/x", Branch: "issue-12-x",
			Anchor: "plan.md", Next: "/lighthouse-start-issue 12", When: "night", Lane: "native"}, ""},
		{"missing issue", without("issue"), Handoff{}, "fields missing: issue"},
		{"missing worktree", without("worktree"), Handoff{}, "fields missing: worktree"},
		{"missing branch", without("branch"), Handoff{}, "fields missing: branch"},
		{"missing next", without("next"), Handoff{}, "fields missing: next"},
		{"missing when", without("when"), Handoff{}, "fields missing: when"},
		{"issue unreadable", with("issue", "abc"), Handoff{}, "issue unreadable: abc"},
		{"when unknown", with("when", "later"), Handoff{}, "when must be now or night, not later"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHandoff(Header{Kind: KindHandoff, Fields: tt.fields})
			if tt.wantErr != "" {
				if !errors.Is(err, ErrHandoff) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want ErrHandoff containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseHandoffLegacyNight(t *testing.T) {
	header, ok := ParseHeader("🤖 Handoff\nissue: 3\nworktree: /w\nbranch: b\nvolgende: /x\nwanneer: nacht")
	if !ok {
		t.Fatal("no header")
	}
	got, err := ParseHandoff(header)
	if err != nil || got.When != "night" {
		t.Fatalf("got %+v, %v; want When night", got, err)
	}
}

func TestParseHandoffWrongKind(t *testing.T) {
	_, err := ParseHandoff(Header{Kind: KindDone})
	if !errors.Is(err, ErrHandoff) {
		t.Fatalf("err = %v, want ErrHandoff", err)
	}
}

func TestParseHandoffAfterAndRoborev(t *testing.T) {
	got, err := ParseHandoff(Header{Kind: KindHandoff, Fields: map[string]string{
		"issue": "#396", "worktree": "/w", "branch": "issue-396-x", "next": "push-workflows",
		"when": "now", "after": "/lighthouse-write-pr 396", "roborev": "done abc123",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.After != "/lighthouse-write-pr 396" || got.Roborev != "done abc123" {
		t.Errorf("After, Roborev = %q, %q", got.After, got.Roborev)
	}
}

func TestPushedHandoffParses(t *testing.T) {
	for _, language := range []string{"nl", "en"} {
		body := RenderPushed(396, "/w/x", "issue-396-x", "aaa..bbb", ".github/workflows/ci.yml", "/lighthouse-write-pr 396", language)
		h, ok := ParseHeader(body)
		if !ok {
			t.Fatalf("%s: no header in %q", language, body)
		}
		got, err := ParseHandoff(h)
		if err != nil {
			t.Fatalf("%s: %v", language, err)
		}
		want := Handoff{Issue: 396, Worktree: "/w/x", Branch: "issue-396-x", Next: "/lighthouse-write-pr 396", When: "now"}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", language, got, want)
		}
		if h.Fields["range"] != "aaa..bbb" || h.Fields["workflows"] != ".github/workflows/ci.yml" {
			t.Errorf("%s: fields = %v", language, h.Fields)
		}
	}
}

func TestRenderPushNote(t *testing.T) {
	got := RenderPushNote("aaa..bbb", ".github/workflows/ci.yml", "nl")
	want := "🤖 Workflow-bestanden gepusht door de pusher\nrange: aaa..bbb\nworkflows: .github/workflows/ci.yml"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if _, ok := ParseHeader(got); ok {
		t.Error("a push note must not parse as a protocol kind")
	}
}
