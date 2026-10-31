package dispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestReadSnapshotFixture reads the snapshot that TestGatherCommandMatchesFixture in
// internal/cli writes from a fake GitHub: decide is tested on this file.
func TestReadSnapshotFixture(t *testing.T) {
	const path = "testdata/snapshot.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := ReadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != 1 {
		t.Errorf("Version = %d, want 1", snap.Version)
	}
	again, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(again, '\n'), raw) {
		t.Errorf("snapshot does not round-trip: re-encoded\n%s\nwant\n%s", again, raw)
	}

	v2 := bytes.Replace(raw, []byte(`"version": 1,`), []byte(`"version": 2,`), 1)
	if bytes.Equal(v2, raw) {
		t.Fatal(`fixture has no "version": 1,`)
	}
	other := filepath.Join(t.TempDir(), "v2.json")
	if err := os.WriteFile(other, v2, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(other); err == nil {
		t.Error("ReadSnapshot of a version 2 copy gave no error")
	}
}
