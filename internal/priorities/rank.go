package priorities

import (
	"regexp"
	"strconv"
	"strings"
)

// rankLine matches a trimmed description line that carries the rank of a milestone.
var rankLine = regexp.MustCompile(`(?i)^priority:\s*([1-9][0-9]*)$`)

// matchRank reports the rank on a line, if the trimmed line is a valid Priority line.
func matchRank(line string) (int, bool) {
	m := rankLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParseRank returns the rank of the first valid `Priority: N` line in a milestone description.
func ParseRank(description string) (rank int, ok bool) {
	for _, line := range strings.Split(description, "\n") {
		if n, found := matchRank(line); found {
			return n, true
		}
	}
	return 0, false
}

// SetRank replaces the first valid Priority line with rank, keeping its indentation and trailing
// whitespace, or appends a new line after a blank line when there is none.
func SetRank(description string, rank int) string {
	newLine := "Priority: " + strconv.Itoa(rank)
	lines := strings.Split(description, "\n")
	for i, line := range lines {
		if _, found := matchRank(line); !found {
			continue
		}
		core := strings.TrimSpace(line)
		start := strings.Index(line, core)
		lines[i] = line[:start] + newLine + line[start+len(core):]
		return strings.Join(lines, "\n")
	}
	if description == "" {
		return newLine
	}
	return description + "\n\n" + newLine
}

// WithDescription returns repos with the description of one milestone replaced. It copies the
// touched Repo and its Milestones slice and never mutates the input.
func WithDescription(repos []Repo, slug string, number int, description string) []Repo {
	out := make([]Repo, len(repos))
	copy(out, repos)
	for i, r := range out {
		if r.Slug != slug {
			continue
		}
		ms := make([]Milestone, len(r.Milestones))
		copy(ms, r.Milestones)
		for j := range ms {
			if ms[j].Number == number {
				ms[j].Description = description
			}
		}
		out[i].Milestones = ms
	}
	return out
}
