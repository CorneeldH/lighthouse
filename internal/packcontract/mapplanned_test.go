package packcontract

import (
	"strings"
	"testing"
)

// map-planned: the skill the scheduled job starts. It reads the corpus and writes the result
// under the field names of internal/scheduled/mapplanned, asks only the two question topics,
// and names no label or path of any one repo.

const mapPlanned = "core/skills/map-planned"

// Classification: fails when the skill stops naming the two arguments, the heading key of the
// placement, the `no-item` decision, the `off` letter, a corpus or result field the job reads,
// or one of the two mandatory options.
func TestMapPlannedSkillContract(t *testing.T) {
	for variant, body := range variants(t, mapPlanned) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, frontmatter(t, body), "name: lighthouse-map-planned",
				"Use when the scheduled job map-planned starts it, or by hand to put planned issues on their map")
			f := flat(body)
			wantAll(t, f, "`--corpus", "`--result", "`heading:map_placement`", "no-item", "`off`",
				"new part on the map", "not on the map", "`place`", "`wish`",
				"`answer`", "topic", "letter", "`taxonomy`", "`tenets`", "`areas`", "documents",
				"`questions`", "`decisions`", "`recommended`", "options", "`planned`")
			wantOrder(t, body, "new part on the map", "not on the map")
		})
	}
}

// Classification: fails when the skill names a label or a path of the repo that uses it, which
// would make it right for one repo only.
func TestMapPlannedSkillNamesNoLabelOrPath(t *testing.T) {
	for variant, body := range variants(t, mapPlanned) {
		t.Run(variant, func(t *testing.T) {
			lower := strings.ToLower(body)
			for _, word := range []string{"loop", "kaart", "loop.json", "kaart.yaml"} {
				if strings.Contains(lower, word) {
					t.Errorf("contains %q", word)
				}
			}
		})
	}
}

// Classification: fails when the skill runs the wish test on an issue whose answer is not null
// (a place answer would ask the wish again), stops asking again after a free wish answer (the
// issue would stay due every day with nothing done), or asks a wish again that has a letter.
func TestMapPlannedSkillAsksAWishOnce(t *testing.T) {
	for variant, body := range variants(t, mapPlanned) {
		t.Run(variant, func(t *testing.T) {
			f := flat(body)
			wantAll(t, f, "Run the wish test only when the issue's `answer` is `null`",
				"A wish answered with a letter is never asked again",
				"Only an `answer` with topic `wish` and an empty `letter`",
				"asks the wish question again")
		})
	}
}
