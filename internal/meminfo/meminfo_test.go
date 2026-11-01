package meminfo

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMeminfo(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAvailableGiB(t *testing.T) {
	path := writeMeminfo(t, "MemTotal:       65536000 kB\nMemFree:         1000 kB\nMemAvailable:   9961472 kB\n")
	got, err := AvailableGiB(path)
	if err != nil || got != 9.5 {
		t.Errorf("AvailableGiB = %v, %v; want 9.5", got, err)
	}
}

func TestAvailableGiBMissingLine(t *testing.T) {
	path := writeMeminfo(t, "MemTotal:       65536000 kB\nMemFree:         1000 kB\n")
	if got, err := AvailableGiB(path); err == nil {
		t.Errorf("AvailableGiB = %v, nil; want an error without MemAvailable", got)
	}
	if got, err := AvailableGiB(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Errorf("AvailableGiB = %v, nil; want an error for a missing file", got)
	}
}
