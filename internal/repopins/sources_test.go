package repopins

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

type sourceEntry struct {
	Path       string  `toml:"path"`
	New        *bool   `toml:"new"`
	Pin        *string `toml:"pin"`
	SourceRepo *string `toml:"source_repo"`
	SourceSHA  *string `toml:"source_sha"`
	SourcePath *string `toml:"source_path"`
}

func sourceEntries(t *testing.T) []sourceEntry {
	t.Helper()
	var doc struct {
		File []sourceEntry `toml:"file"`
	}
	if _, err := toml.Decode(read(t, "pack", "SOURCES.toml"), &doc); err != nil {
		t.Fatalf("parse pack/SOURCES.toml: %v", err)
	}
	if len(doc.File) == 0 {
		t.Fatal("pack/SOURCES.toml has no [[file]] entry")
	}
	return doc.File
}

// packFiles returns every file under pack/<package>/, as a slash path relative to the repo
// root: what the embed puts in the binary, without Python caches and macOS junk.
func packFiles(t *testing.T) map[string]bool {
	t.Helper()
	root := repoRoot(t)
	out := map[string]bool{}
	pkgs, err := os.ReadDir(filepath.Join(root, "pack"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		if !p.IsDir() {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, "pack", p.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "__pycache__" || d.Name() == ".pytest_cache" {
					return fs.SkipDir
				}
				return nil
			}
			if d.Name() == ".DS_Store" || strings.HasSuffix(d.Name(), ".pyc") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// A production change that fails this test: a file is added under pack/ without an entry in
// pack/SOURCES.toml, or an entry appears twice.
func TestEveryPackFileHasASource(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range sourceEntries(t) {
		if seen[e.Path] {
			t.Errorf("path %s appears twice in pack/SOURCES.toml", e.Path)
		}
		seen[e.Path] = true
	}
	var missing []string
	for f := range packFiles(t) {
		if !seen[f] {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no entry in pack/SOURCES.toml: %v", missing)
	}
}

// A production change that fails this test: a pack file is renamed or removed while its
// SOURCES.toml entry stays.
func TestNoSourceEntryWithoutFile(t *testing.T) {
	files := packFiles(t)
	for _, e := range sourceEntries(t) {
		if !files[e.Path] {
			t.Errorf("entry without a file: %s", e.Path)
		}
	}
}

var (
	ownerRepo = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)
	hex40     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// A production change that fails this test: an entry gets two provenances (for example `new`
// and `pin`), none, or an incomplete or malformed set of source fields.
func TestSourceEntryHasExactlyOneProvenance(t *testing.T) {
	for _, e := range sourceEntries(t) {
		anySource := e.SourceRepo != nil || e.SourceSHA != nil || e.SourcePath != nil
		kinds := 0
		for _, on := range []bool{e.New != nil, e.Pin != nil, anySource} {
			if on {
				kinds++
			}
		}
		switch {
		case kinds != 1:
			t.Errorf("%s: exactly one of new, pin or the source fields, got %d kinds", e.Path, kinds)
		case e.New != nil:
			if !*e.New {
				t.Errorf("%s: new must be true", e.Path)
			}
		case e.Pin != nil:
			if *e.Pin == "" {
				t.Errorf("%s: pin must be a non-empty string", e.Path)
			}
		case e.SourceRepo == nil || e.SourceSHA == nil || e.SourcePath == nil:
			t.Errorf("%s: source fields must come together", e.Path)
		default:
			if !ownerRepo.MatchString(*e.SourceRepo) {
				t.Errorf("%s: source_repo must be owner/repo, got %q", e.Path, *e.SourceRepo)
			}
			if !hex40.MatchString(*e.SourceSHA) {
				t.Errorf("%s: source_sha must be 40 hex, got %q", e.Path, *e.SourceSHA)
			}
			if *e.SourcePath == "" {
				t.Errorf("%s: source_path must be non-empty", e.Path)
			}
		}
	}
}
