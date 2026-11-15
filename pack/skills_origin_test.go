package pack_test

import (
	"io/fs"
	"path"
	"slices"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/pins"
	"github.com/CorneeldH/lighthouse/pack"
)

// TestEverySkillHasOrigin fails when a packaged SKILL.md lacks `metadata.origin` or carries
// a value outside the three origins; it names every offending path.
func TestEverySkillHasOrigin(t *testing.T) {
	valid := []string{"own", "extended", "external"}
	var skills int
	err := fs.WalkDir(pack.Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(p) != "SKILL.md" {
			return err
		}
		skills++
		data, err := fs.ReadFile(pack.Files, p)
		if err != nil {
			return err
		}
		origin, ok := pins.SkillMetadata(string(data))["origin"]
		if !ok {
			t.Errorf("%s: metadata.origin missing", p)
		} else if !slices.Contains(valid, origin) {
			t.Errorf("%s: metadata.origin %q is not one of %v", p, origin, valid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if skills == 0 {
		t.Fatal("no SKILL.md found in the embedded pack")
	}
}
