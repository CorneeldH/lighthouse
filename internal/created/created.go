// Package created records what `lighthouse init` created outside the manifest
// units, in .lighthouse/created.toml, so `lighthouse uninstall` can undo exactly that.
package created

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Path is the repo-relative location of the file.
const Path = ".lighthouse/created.toml"

const header = "# Managed by lighthouse init. Do not edit by hand.\n"

// Created is what init created outside the units.
type Created struct {
	Files            []string `toml:"files"`
	Gitattributes    []string `toml:"gitattributes"`
	SettingsKeys     []string `toml:"settings_keys"`
	SettingsLanguage string   `toml:"settings_language"`
}

// Read loads the file under root. A missing file is found=false with a nil error;
// invalid TOML or an unknown key is an error naming Path.
func Read(root string) (c Created, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(Path)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Created{}, false, nil
		}
		return Created{}, false, fmt.Errorf("%s: %w", Path, err)
	}
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Created{}, false, fmt.Errorf("%s: not valid TOML: %w", Path, err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return Created{}, false, fmt.Errorf("%s: unknown key %s", Path, extra[0])
	}
	return c, true, nil
}

// Encode renders c: the header, then only the non-empty keys, lists in insertion order.
func Encode(c Created) []byte {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	list := func(key string, items []string) {
		if len(items) == 0 {
			return
		}
		quoted := make([]string, len(items))
		for i, it := range items {
			quoted[i] = quote(it)
		}
		fmt.Fprintf(&b, "%s = [%s]\n", key, strings.Join(quoted, ", "))
	}
	list("files", c.Files)
	list("gitattributes", c.Gitattributes)
	list("settings_keys", c.SettingsKeys)
	if c.SettingsLanguage != "" {
		fmt.Fprintf(&b, "settings_language = %s\n", quote(c.SettingsLanguage))
	}
	return []byte(b.String())
}

// quote renders s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(map[string]string{"v": s}); err != nil {
		return fmt.Sprintf("%q", s)
	}
	return strings.TrimSuffix(strings.TrimPrefix(b.String(), "v = "), "\n")
}

// Merge returns c grown with add: no duplicates, nothing removed. A non-empty
// SettingsLanguage in add replaces c's.
func (c Created) Merge(add Created) Created {
	out := Created{
		Files:            union(c.Files, add.Files),
		Gitattributes:    union(c.Gitattributes, add.Gitattributes),
		SettingsKeys:     union(c.SettingsKeys, add.SettingsKeys),
		SettingsLanguage: c.SettingsLanguage,
	}
	if add.SettingsLanguage != "" {
		out.SettingsLanguage = add.SettingsLanguage
	}
	return out
}

// Empty reports whether no field is filled.
func (c Created) Empty() bool {
	return len(c.Files) == 0 && len(c.Gitattributes) == 0 &&
		len(c.SettingsKeys) == 0 && c.SettingsLanguage == ""
}

func union(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := slices.Clone(a)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
