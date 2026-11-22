// Package levels models the entry levels (instapniveaus): which component of
// a pack is off, logging or on at a level, and what a repo has installed.
package levels

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Stand is the state of one component.
type Stand string

const (
	StandOff Stand = "off"
	StandLog Stand = "log"
	StandOn  Stand = "on"
)

// UnmarshalText rejects every stand other than off, log and on.
func (s *Stand) UnmarshalText(b []byte) error {
	switch v := Stand(b); v {
	case StandOff, StandLog, StandOn:
		*s = v
		return nil
	}
	return fmt.Errorf("unknown stand %q (want off, log or on)", string(b))
}

// CoreGates are the gates no level may turn off.
var CoreGates = []string{"main-commit-gate", "worktree-check", "pr-create-gate"}

// Table is one level's [<level>.components] and [<level>.flags].
type Table struct {
	Components map[string]Stand
	Flags      map[string]bool
}

// File is one pack's levels.toml.
type File struct {
	Order  []string            // top-level `order`, core only
	Levels map[string]Table    // [<level>.components], [<level>.flags]
	Base   []string            // components that are always installed
	On     []string            // components that are `on` at every level
	Owners map[string]string   // [owners]: package-relative path -> component or "base"
	Calls  map[string][]string // [calls]: "skill:x" -> skills it hands over to
}

type rawTable struct {
	Components map[string]Stand `toml:"components"`
	Flags      map[string]bool  `toml:"flags"`
}

type rawFile struct {
	Order  []string            `toml:"order"`
	Base   []string            `toml:"base"`
	On     []string            `toml:"on"`
	Owners map[string]string   `toml:"owners"`
	Calls  map[string][]string `toml:"calls"`
}

var reservedKeys = []string{"order", "base", "on", "owners", "calls"}

// Parse reads one levels.toml. Every top-level table that is not reserved is
// a level.
func Parse(data []byte) (File, error) {
	var rf rawFile
	md, err := toml.Decode(string(data), &rf)
	if err != nil {
		return File{}, fmt.Errorf("levels.toml: %w", err)
	}
	var all map[string]toml.Primitive
	if _, err := toml.Decode(string(data), &all); err != nil {
		return File{}, fmt.Errorf("levels.toml: %w", err)
	}
	f := File{Order: rf.Order, Base: rf.Base, On: rf.On, Owners: rf.Owners, Calls: rf.Calls, Levels: map[string]Table{}}
	for name, prim := range all {
		if slices.Contains(reservedKeys, name) {
			continue
		}
		var rt rawTable
		if err := md.PrimitiveDecode(prim, &rt); err != nil {
			return File{}, fmt.Errorf("levels.toml: level %q: %w", name, err)
		}
		f.Levels[name] = Table(rt)
	}
	return f, nil
}

// Resolved is the stand per component at one level.
type Resolved struct {
	Level      string
	Components map[string]Stand
	Flags      map[string]bool
	names      []string
}

// Resolve merges the packs' tables for level and applies overrides on top.
func Resolve(files []File, level string, overrides map[string]Stand) (Resolved, error) {
	known := map[string]bool{}
	names := map[string]bool{}
	r := Resolved{Level: level, Components: map[string]Stand{}, Flags: map[string]bool{}}
	found := false
	for _, f := range files {
		for _, c := range f.Base {
			names[c] = true
		}
		for _, c := range f.On {
			names[c] = true
		}
		for lvl, t := range f.Levels {
			known[lvl] = true
			for c := range t.Components {
				names[c] = true
			}
			if lvl != level {
				continue
			}
			found = true
			for c, s := range t.Components {
				r.Components[c] = s
			}
			for k, v := range t.Flags {
				r.Flags[k] = v
			}
		}
	}
	if !found {
		list := make([]string, 0, len(known))
		for l := range known {
			list = append(list, l)
		}
		sort.Strings(list)
		return Resolved{}, fmt.Errorf("unknown level %q (known: %s)", level, strings.Join(list, ", "))
	}
	for c, s := range overrides {
		r.Components[c] = s
	}
	for c := range names {
		r.names = append(r.names, c)
	}
	sort.Strings(r.names)
	return r, nil
}

// Stand returns the stand of component; an unlisted component is on.
func (r Resolved) Stand(component string) Stand {
	if s, ok := r.Components[component]; ok {
		return s
	}
	return StandOn
}

// Names lists every component any pack names, sorted.
func (r Resolved) Names() []string { return slices.Clone(r.names) }
