package packcontract

import (
	"strings"
	"testing"
)

// architecture-scan: the skill the scheduled job architecture-scan starts. Headless it reads the
// corpus and writes one result file in the format internal/scheduled/archscan parses; by hand it
// presents the candidates in the chat. It names no path of any one repo.

const architectureScan = "core/skills/architecture-scan"

// Classification: fails when the skill stops naming the two arguments, one of the four section
// heading keys the job requires, one of the three claim marks or the reject checkbox the parser
// reads, the search paths the hotspots come from, the follow-up skill, or one of the three ratings.
func TestArchitectureScanSkillContract(t *testing.T) {
	for variant, body := range variants(t, architectureScan) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, frontmatter(t, body), "name: lighthouse-architecture-scan")
			f := flat(body)
			wantAll(t, f, "`--corpus", "`--out",
				"`heading:scan_candidates`", "`heading:scan_still_open`", "`heading:scan_method`", "`heading:scan_top`",
				"[nagemeten:", "[niet nagemeten]", "[niet meetbaar vanaf hier]", "- [ ] Afgewezen", // lighthouse: allow-language
				"paths.search_dirs", "/lighthouse-write-issue", "Strong", "Worth exploring", "Speculative")
		})
	}
}

// Classification: fails when the skill names a source path or a paia path, which would make it
// right for one repo only.
func TestArchitectureScanSkillNamesNoRepoPath(t *testing.T) {
	for variant, body := range variants(t, architectureScan) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, body, "src/")
			wantNone(t, strings.ToLower(body), "paia")
		})
	}
}
