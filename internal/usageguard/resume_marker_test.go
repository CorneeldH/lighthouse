package usageguard

import (
	"os"
	"path/filepath"
	"testing"
)

// The resume speaker writes the marker of an ordinary session: handoff_path is the handoff the
// reason names, and source is the stand. Without a session id, or at prompt, there is none.
func TestResumeMarkerFields(t *testing.T) {
	for _, stand := range []string{"tool", "stop"} {
		t.Run(stand, func(t *testing.T) {
			d, _ := testDirs(t)
			c := mkCtx(t, stand, map[string]any{"session_id": "s1", "cwd": "/w"})
			dec, warns := decideWith(c, d, optsResume)
			if dec.Output == nil || len(warns) != 0 {
				t.Fatalf("output %v warns %v", dec.Output, warns)
			}
			m := readMarkerFile(t, d, "s1")
			want := map[string]any{
				"session_id":   "s1",
				"cwd":          "/w",
				"window":       "venster",
				"reset_at":     "2026-09-06T12:30:00Z",
				"created_at":   "2026-09-06T11:58:00Z",
				"source":       stand,
				"handoff_path": filepath.Join(d.Resume, "s1.handoff.md"),
			}
			if len(m) != len(want) {
				t.Errorf("marker %v, want keys %v", m, want)
			}
			for k, v := range want {
				if m[k] != v {
					t.Errorf("%s = %v, want %v", k, m[k], v)
				}
			}
		})
	}
}

func TestResumeWritesNoMarker(t *testing.T) {
	tests := []struct {
		name, stand string
		payload     map[string]any
	}{
		{"prompt", "prompt", map[string]any{"session_id": "s1"}},
		{"no session", "tool", map[string]any{}},
		{"unsafe session", "tool", map[string]any{"session_id": "../x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := testDirs(t)
			dec, _ := decideWith(mkCtx(t, tt.stand, tt.payload), d, optsResume)
			if dec.Output == nil {
				t.Fatal("no output")
			}
			entries, err := os.ReadDir(d.Resume)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("resume dir has %d entries, want none", len(entries))
			}
		})
	}
}
