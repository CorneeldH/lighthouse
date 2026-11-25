package packcontract

import "testing"

// Classification: fails when write-issue or map-planned names the taxonomy document or one of the
// field names of its table between backticks. The list is fixed here because `internal/packcontract`
// reads only the pack, never the taxonomy file; the skills stay generic and learn the fields from
// the table in the repo they run in. Corpus fields of map-planned (`label`, `issue`, `document`)
// are not in the list.
func TestSkillsNameNoFieldOfTheDocument(t *testing.T) {
	banned := map[string]bool{}
	for _, name := range []string{"loop.json", "phase", "after", "kind", "trigger", "from", "to", "row", "what"} {
		banned[name] = true
	}
	for _, dir := range []string{writeIssue, mapPlanned} {
		for variant, body := range variants(t, dir) {
			t.Run(dir+"/"+variant, func(t *testing.T) {
				for _, m := range codeSpanRe.FindAllStringSubmatch(body, -1) {
					if banned[m[1]] {
						t.Errorf("skill names `%s` between backticks; it must stay generic", m[1])
					}
				}
			})
		}
	}
}
