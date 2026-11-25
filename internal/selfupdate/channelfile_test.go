package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadChannelMissingIsRelease(t *testing.T) {
	c, err := ReadChannel(filepath.Join(t.TempDir(), "channel"))
	if err != nil || c != ChannelRelease {
		t.Fatalf("got %q, %v; want release, nil", c, err)
	}
}

func TestReadChannelRejectsGarbage(t *testing.T) {
	for _, content := range []string{"stable", ""} {
		p := filepath.Join(t.TempDir(), "channel")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadChannel(p)
		if err == nil || !strings.Contains(err.Error(), p) {
			t.Errorf("content %q: err = %v, want error naming %s", content, err, p)
		}
	}
}

func TestReadChannelTrimsNewline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "channel")
	if err := os.WriteFile(p, []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadChannel(p)
	if err != nil || c != ChannelDev {
		t.Fatalf("got %q, %v; want dev, nil", c, err)
	}
}

func TestWriteThenReadChannel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "channel")
	if err := WriteChannel(p, ChannelDev); err != nil {
		t.Fatal(err)
	}
	c, err := ReadChannel(p)
	if err != nil || c != ChannelDev {
		t.Fatalf("got %q, %v; want dev, nil", c, err)
	}
}
