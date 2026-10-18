// Package manifest reads and writes .lighthouse/manifest.toml, the source of truth for
// what `lighthouse init` installed (spec, Versiemarker en manifest).
//
// Per unit it records package, version and the sha256 of the content as init wrote it.
// `--upgrade` compares the current content against that hash: equal means safe to
// replace; different means changed locally.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

// Path is the manifest's path in a repository.
const Path = ".lighthouse/manifest.toml"

const header = "# Managed by lighthouse init. Do not edit by hand.\n\n"

// Entry is one installed unit.
type Entry struct {
	Kind    packs.Kind `toml:"kind"`
	Path    string     `toml:"path"`
	ID      string     `toml:"id,omitempty"`
	Package string     `toml:"package"`
	Version string     `toml:"version"`
	SHA256  string     `toml:"sha256"`
}

// Key returns the unit key of e.
func (e Entry) Key() packs.Key { return packs.Key{Kind: e.Kind, Path: e.Path, ID: e.ID} }

// Manifest is the whole file.
type Manifest struct {
	Version  string   `toml:"version"`
	Packages []string `toml:"packages"`
	Units    []Entry  `toml:"unit"`
}

var hexSum = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Read reads and validates the manifest of the repository at root. A missing manifest
// gives an error for which errors.Is(err, fs.ErrNotExist) holds.
func Read(root string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(Path)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return m, fmt.Errorf("%s: %w", Path, fs.ErrNotExist)
		}
		return m, fmt.Errorf("%s: %w", Path, err)
	}
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: not valid TOML: %w", Path, err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return Manifest{}, fmt.Errorf("%s: unknown key %s", Path, extra[0])
	}
	seen := map[packs.Key]bool{}
	for i, e := range m.Units {
		switch e.Kind {
		case packs.KindFile, packs.KindBlock, packs.KindHook, packs.KindPermission:
		default:
			return Manifest{}, fmt.Errorf("%s: unit %d: unknown kind %q", Path, i+1, e.Kind)
		}
		if e.Path == "" || e.Package == "" || e.Version == "" {
			return Manifest{}, fmt.Errorf("%s: unit %d: path, package and version are required", Path, i+1)
		}
		if (e.Kind == packs.KindHook || e.Kind == packs.KindPermission) && e.ID == "" {
			return Manifest{}, fmt.Errorf("%s: unit %d: a %s unit needs an id", Path, i+1, e.Kind)
		}
		if !packs.IsInstallPath(e.Kind, e.Path) {
			return Manifest{}, fmt.Errorf("%s: unit %d: path %q is not a lighthouse path for a %s unit", Path, i+1, e.Path, e.Kind)
		}
		if !hexSum.MatchString(e.SHA256) {
			return Manifest{}, fmt.Errorf("%s: unit %d (%s): sha256 must be 64 hex characters", Path, i+1, e.Path)
		}
		if seen[e.Key()] {
			return Manifest{}, fmt.Errorf("%s: unit %d: %s %s %s appears twice", Path, i+1, e.Kind, e.Path, e.ID)
		}
		seen[e.Key()] = true
	}
	sortUnits(m.Units)
	return m, nil
}

func sortUnits(units []Entry) {
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].Path != units[j].Path {
			return units[i].Path < units[j].Path
		}
		if units[i].ID != units[j].ID {
			return units[i].ID < units[j].ID
		}
		return units[i].Kind < units[j].Kind
	})
}

// Encode renders m deterministically: a header line, then the units sorted by (Path, ID).
func Encode(m Manifest) []byte {
	m.Units = append([]Entry(nil), m.Units...)
	sortUnits(m.Units)
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(m); err != nil {
		// Only plain strings and slices of structs: encoding cannot fail.
		panic(err)
	}
	return buf.Bytes()
}

// Lookup returns the entry for k.
func (m Manifest) Lookup(k packs.Key) (Entry, bool) {
	for _, e := range m.Units {
		if e.Key() == k {
			return e, true
		}
	}
	return Entry{}, false
}

// Sum returns the hex sha256 of b.
func Sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
