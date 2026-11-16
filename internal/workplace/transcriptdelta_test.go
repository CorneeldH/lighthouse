package workplace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const deltaSession = "5b0c2f9e-3d41-4a8e-9c77-0f1e2d3c4b5a"

func writeDeltaFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func deltaByName(ds []FileDelta) map[string]FileDelta {
	m := map[string]FileDelta{}
	for _, d := range ds {
		m[d.Name] = d
	}
	return m
}

func TestReadDeltaFromOffset(t *testing.T) {
	dir := t.TempDir()
	writeDeltaFile(t, dir, deltaSession+".jsonl", "line-1\nline-2\n")
	writeDeltaFile(t, dir, deltaSession+"/subagents/agent-a.jsonl", "sub-1\nsub-2\n")
	writeDeltaFile(t, dir, deltaSession+"/subagents/agent-b.jsonl", "new\n")

	got, err := ReadDelta(dir, deltaSession, map[string]int64{
		deltaSession + ".jsonl":                   7,
		deltaSession + "/subagents/agent-a.jsonl": 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != deltaSession+".jsonl" {
		t.Fatalf("deltas = %+v, want the main transcript first and three files", got)
	}
	want := map[string]FileDelta{
		deltaSession + ".jsonl":                   {Name: deltaSession + ".jsonl", Size: 14, From: 7, Data: []byte("line-2\n")},
		deltaSession + "/subagents/agent-a.jsonl": {Name: deltaSession + "/subagents/agent-a.jsonl", Size: 12, From: 12, Data: []byte{}},
		deltaSession + "/subagents/agent-b.jsonl": {Name: deltaSession + "/subagents/agent-b.jsonl", Size: 4, From: 0, Data: []byte("new\n")},
	}
	for name, w := range want {
		g, ok := deltaByName(got)[name]
		if !ok || g.Size != w.Size || g.From != w.From || string(g.Data) != string(w.Data) {
			t.Errorf("%s = %+v (data %q), want size %d from %d data %q", name, g, g.Data, w.Size, w.From, w.Data)
		}
	}
}

// A file shorter than the offset was rotated or restarted: the whole file comes back, from 0.
func TestReadDeltaShorterFileReturnsWholeFile(t *testing.T) {
	dir := t.TempDir()
	writeDeltaFile(t, dir, deltaSession+".jsonl", "fresh\n")

	got, err := ReadDelta(dir, deltaSession, map[string]int64{deltaSession + ".jsonl": 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Size != 6 || got[0].From != 0 || string(got[0].Data) != "fresh\n" {
		t.Fatalf("deltas = %+v, want the whole file of 6 bytes from 0", got)
	}
}

func TestReadDeltaIgnoresForeignFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project")
	other := "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a"
	writeDeltaFile(t, dir, deltaSession+".jsonl", "mine\n")
	writeDeltaFile(t, dir, other+".jsonl", "other session\n")
	writeDeltaFile(t, dir, other+"/subagents/agent-x.jsonl", "other sub\n")
	writeDeltaFile(t, dir, deltaSession+"/subagents/notes.txt", "not a transcript\n")
	writeDeltaFile(t, root, "secret.jsonl", "outside\n")

	got, err := ReadDelta(dir, deltaSession, map[string]int64{
		"../secret.jsonl":       0,
		other + ".jsonl":        0,
		deltaSession + ".jsonl": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != deltaSession+".jsonl" || string(got[0].Data) != "mine\n" {
		t.Fatalf("deltas = %+v, want only %s.jsonl", got, deltaSession)
	}
}

func TestReadDeltaWithoutMainTranscriptIsNotExist(t *testing.T) {
	dir := t.TempDir()
	writeDeltaFile(t, dir, deltaSession+"/subagents/agent-a.jsonl", "sub\n")
	if _, err := ReadDelta(dir, deltaSession, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadDeltaRejectsUnsafeSession(t *testing.T) {
	for _, sid := range []string{"", "../x", "a/b"} {
		if _, err := ReadDelta(t.TempDir(), sid, nil); err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadDelta(%q) = %v, want a rejection", sid, err)
		}
	}
}

func TestSessionFile(t *testing.T) {
	cases := map[string]bool{
		deltaSession + ".jsonl":                   true,
		deltaSession + "/subagents/agent-a.jsonl": true,
		deltaSession + "/subagents/../x.jsonl":    false,
		deltaSession + "/subagents/a/b.jsonl":     false,
		deltaSession + "/subagents/agent-a.txt":   false,
		deltaSession + "/subagents/..":            false,
		"../" + deltaSession + ".jsonl":           false,
		"other.jsonl":                             false,
		".lighthouse-delta":                       false,
	}
	for name, want := range cases {
		if got := SessionFile(name, deltaSession); got != want {
			t.Errorf("SessionFile(%q) = %v, want %v", name, got, want)
		}
	}
}
