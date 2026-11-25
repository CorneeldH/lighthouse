package repoconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/levels"
)

// TestPackLevelsDeriveControllerKnobs reads the real pack/core/levels.toml and pins the
// controller knobs each level derives.
func TestPackLevelsDeriveControllerKnobs(t *testing.T) {
	data, err := os.ReadFile("../../pack/core/levels.toml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := levels.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		spec, plan bool
		limit      string
	}{
		"light":    {false, false, "doorgaan"},
		"standard": {false, false, "doorgaan"},
		"strict":   {true, true, "doorgaan"},
	}
	if len(f.Order) != len(want) {
		t.Fatalf("levels %v: this table and update the section 'Controller knobs per level' in levels.md", f.Order)
	}
	for _, level := range f.Order {
		w, ok := want[level]
		if !ok {
			t.Fatalf("level %q missing from the table: update the section 'Controller knobs per level' in levels.md", level)
		}
		resolved, err := levels.Resolve([]levels.File{f}, level, nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".lighthouse"), 0o755); err != nil {
			t.Fatal(err)
		}
		inst := levels.Installed{Level: level, Since: "2026-01-01T00:00:00Z", Components: resolved.Components}
		if err := os.WriteFile(filepath.Join(dir, ".lighthouse", "components.toml"), inst.Marshal(), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "lighthouse.toml"), []byte("[forge]\nrepo = \"o/r\"\n[controller]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if r.Controller == nil {
			t.Fatalf("%s: no [controller]", level)
		}
		c := *r.Controller
		if c.SpecReview != w.spec || c.PlanReview != w.plan || c.AtRoundLimit != w.limit {
			t.Errorf("%s: spec_review=%v plan_review=%v at_round_limit=%q, want %v %v %q; update the section 'Controller knobs per level' in levels.md",
				level, c.SpecReview, c.PlanReview, c.AtRoundLimit, w.spec, w.plan, w.limit)
		}
	}
}
