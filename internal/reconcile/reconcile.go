// Package reconcile decides, per unit, what `lighthouse init` does with it: compare the
// desired content (the package), the current content (the repo) and the hash recorded
// in the manifest. A local change never disappears silently (spec, decision 4).
package reconcile

import (
	"bytes"
	"sort"

	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// Action is what happens to one unit.
type Action string

// The actions. Adopt: the unit is already there as the package has it, but not yet in
// the manifest; nothing changes on disk. Conflict: the repo has content that is neither
// the package's nor what lighthouse wrote last time.
const (
	Add       Action = "add"
	Replace   Action = "replace"
	Remove    Action = "remove"
	Unchanged Action = "unchanged"
	Adopt     Action = "adopt"
	Conflict  Action = "conflict"
)

// Current is the content of a unit in the repo.
type Current struct {
	Content []byte
	Present bool
}

// Change is the decision for one unit.
type Change struct {
	Key     packs.Key
	Action  Action
	Desired *packs.Unit     // nil for a unit the packages no longer have
	Entry   *manifest.Entry // nil for a unit not in the manifest
	Current Current
}

// Plan decides for every unit in desired or in the manifest, sorted by (Path, ID).
func Plan(desired []packs.Unit, m manifest.Manifest, current map[packs.Key]Current) []Change {
	byKey := map[packs.Key]*Change{}
	for i := range desired {
		u := &desired[i]
		byKey[u.Key()] = &Change{Key: u.Key(), Desired: u}
	}
	for i := range m.Units {
		e := &m.Units[i]
		c, ok := byKey[e.Key()]
		if !ok {
			c = &Change{Key: e.Key()}
			byKey[e.Key()] = c
		}
		c.Entry = e
	}
	changes := make([]Change, 0, len(byKey))
	for k, c := range byKey {
		c.Current = current[k]
		c.Action = decide(c)
		changes = append(changes, *c)
	}
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i].Key, changes[j].Key
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Kind < b.Kind
	})
	return changes
}

func decide(c *Change) Action {
	cur := c.Current
	untouched := c.Entry != nil && cur.Present && manifest.Sum(cur.Content) == c.Entry.SHA256
	if c.Desired == nil {
		switch {
		case !cur.Present, untouched:
			return Remove
		default:
			return Conflict
		}
	}
	same := cur.Present && bytes.Equal(cur.Content, c.Desired.Content)
	switch {
	case c.Entry == nil && !cur.Present:
		return Add
	case c.Entry == nil && same:
		return Adopt
	case c.Entry == nil:
		return Conflict
	case same:
		return Unchanged
	case untouched:
		return Replace
	default:
		return Conflict
	}
}
