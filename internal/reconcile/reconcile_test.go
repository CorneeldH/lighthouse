package reconcile

import (
	"testing"

	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

func TestPlanTable(t *testing.T) {
	key := packs.Key{Kind: packs.KindFile, Path: "x.sh"}
	const absent = "<absent>"
	rows := []struct {
		desired, current, recorded string // "" = not there
		want                           Action
	}{
		{"a", absent, "", Add},
		{"a", "a", "", Adopt},
		{"a", "b", "", Conflict},
		{"a", "a", "a", Unchanged},
		{"a2", "a", "a", Replace},
		{"a", "b", "a", Conflict},
		{"a", "b", "b", Replace},
		{"a", absent, "a", Conflict},
		{"a", "a", "b", Unchanged},
		{"", "a", "a", Remove},
		{"", absent, "a", Remove},
		{"", "b", "a", Conflict},
	}
	for i, r := range rows {
		var desired []packs.Unit
		if r.desired != "" {
			desired = []packs.Unit{{Kind: key.Kind, Path: key.Path, Package: "core", Content: []byte(r.desired)}}
		}
		var m manifest.Manifest
		if r.recorded != "" {
			m.Units = []manifest.Entry{{Kind: key.Kind, Path: key.Path, Package: "core", Version: "dev", SHA256: manifest.Sum([]byte(r.recorded))}}
		}
		current := map[packs.Key]Current{}
		if r.current != absent {
			current[key] = Current{Content: []byte(r.current), Present: true}
		}
		changes := Plan(desired, m, current)
		if len(changes) != 1 {
			t.Fatalf("row %d: %d changes", i, len(changes))
		}
		c := changes[0]
		if c.Action != r.want {
			t.Errorf("row %d (D=%q C=%q M=%q): %s, want %s", i, r.desired, r.current, r.recorded, c.Action, r.want)
		}
		if (c.Desired == nil) != (r.desired == "") || (c.Entry == nil) != (r.recorded == "") || c.Key != key {
			t.Errorf("row %d: change %+v", i, c)
		}
	}
}

func TestPlanSortsByPathAndID(t *testing.T) {
	desired := []packs.Unit{
		{Kind: packs.KindHook, Path: ".claude/settings.json", ID: "b"},
		{Kind: packs.KindFile, Path: ".a"},
		{Kind: packs.KindHook, Path: ".claude/settings.json", ID: "a"},
	}
	changes := Plan(desired, manifest.Manifest{}, nil)
	got := ""
	for _, c := range changes {
		got += c.Key.Path + "#" + c.Key.ID + " "
	}
	if got != ".a# .claude/settings.json#a .claude/settings.json#b " {
		t.Fatalf("order %q", got)
	}
}
