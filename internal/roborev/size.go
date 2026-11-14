// Package roborev is the gate of the local roborev review that `write-pr` runs before
// `gh pr create`, the port of roborev_gate.py: the size rule, the per-branch state files, the
// classification of raw review output and the record of decisions.
package roborev

import (
	"strconv"
	"strings"
)

// Threshold is the number of code lines from which a review runs.
const Threshold = 100

var nonCodeSuffixes = []string{
	".md", ".rst", ".txt", ".toml", ".yaml", ".yml", ".json", ".lock", ".csv", ".ini", ".cfg", ".cff",
}

// counts reports whether a changed path counts as code: not docs/, not prose or data, except
// under .github/workflows/.
func counts(path string) bool {
	if strings.HasPrefix(path, "docs/") {
		return false
	}
	if strings.HasPrefix(path, ".github/workflows/") {
		return true
	}
	for _, suffix := range nonCodeSuffixes {
		if strings.HasSuffix(path, suffix) {
			return false
		}
	}
	return true
}

// CountCodeLines is the added plus removed code lines of `git diff --numstat --no-renames -z`
// output. -z keeps path names unquoted: without it git writes "docs/caf\303\251.md" and the
// docs/ and extension rules would not see the path. Binary files ("-") do not count.
func CountCodeLines(numstat string) int {
	total := 0
	for _, entry := range strings.Split(numstat, "\x00") {
		parts := strings.SplitN(entry, "\t", 3)
		if len(parts) != 3 || parts[0] == "-" || parts[1] == "-" || !counts(parts[2]) {
			continue
		}
		added, errA := strconv.Atoi(parts[0])
		removed, errR := strconv.Atoi(parts[1])
		if errA == nil && errR == nil {
			total += added + removed
		}
	}
	return total
}
