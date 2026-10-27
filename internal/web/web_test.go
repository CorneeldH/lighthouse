package web

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func readIndex(t *testing.T, root fs.FS) string {
	t.Helper()
	b, err := fs.ReadFile(Choose(root), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestChooseBuilt(t *testing.T) {
	m := fstest.MapFS{
		"dist/index.html":        {Data: []byte("spa")},
		"placeholder/index.html": {Data: []byte("ph")},
	}
	if got := readIndex(t, m); got != "spa" {
		t.Errorf("index = %q, want spa", got)
	}
}

func TestChoosePlaceholder(t *testing.T) {
	m := fstest.MapFS{
		"dist/.gitkeep":          {},
		"placeholder/index.html": {Data: []byte("ph")},
	}
	if got := readIndex(t, m); got != "ph" {
		t.Errorf("index = %q, want ph", got)
	}
}

func TestPlaceholderNamesBuild(t *testing.T) {
	sub, err := fs.Sub(embedded, "placeholder")
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"cd web && npm ci && npm run build",
		`name="lighthouse-ui" content="placeholder"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("placeholder lacks %q", want)
		}
	}
}
