package packcontract

import (
	"regexp"
	"slices"
	"testing"
)

var forgeSections = []string{"board", "blockers", "sub-issues", "sluitwoorden", "proxy", "ci", "workflows"}

var forgeKindRe = regexp.MustCompile(`lighthouse config forge\.kind\b`)

var forgeRefRe = regexp.MustCompile(`\.\./lighthouse-forge/<kind>\.md §([\w-]+)`)

// Classification: fails when a chain skill loses its pointer to a lighthouse-forge section, names
// a section that does not exist, or stops reading the forge kind from config.
func TestChainSkillsPointAtForgeReference(t *testing.T) {
	for _, name := range []string{"start-issue", "route-issue", "write-issue", "write-pr"} {
		for variant, text := range variants(t, "core/skills/"+name) {
			t.Run(name+"/"+variant, func(t *testing.T) {
				refs := forgeRefRe.FindAllStringSubmatch(text, -1)
				if len(refs) == 0 {
					t.Fatal("names no `../lighthouse-forge/<kind>.md §<section>`")
				}
				for _, ref := range refs {
					if !slices.Contains(forgeSections, ref[1]) {
						t.Errorf("unknown forge section %q", ref[1])
					}
				}
				if !forgeKindRe.MatchString(text) {
					t.Error("does not read `lighthouse config forge.kind`")
				}
			})
		}
	}
}
