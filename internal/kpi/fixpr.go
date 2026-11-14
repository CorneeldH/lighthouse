package kpi

import (
	"fmt"
	"io"
	"slices"
)

const fixPRSkillName = "lighthouse-fix-pr"

// FixPRMetrics are the rows of the fix-pr table, in output order.
var FixPRMetrics = []string{"leadin", "diagnose", "mistrigger", "scope"}

// scopeSkills are the two actions that belong after the fix-pr skill, not in it.
var scopeSkills = []string{"lighthouse-close-issue", "caveman:caveman-review"}

// fixPRMeasurer reads only <dir>/*.jsonl, never the worktree sibling directories.
type fixPRMeasurer struct {
	dir   string
	since *string
}

func (m *fixPRMeasurer) eachTurn(fn func(Turn)) error {
	return eachTurnIn(m.dir, false, m.since, fn)
}

// eachFixTurn is every turn whose prompt is a fix request.
func (m *fixPRMeasurer) eachFixTurn(fn func(Turn)) error {
	return m.eachTurn(func(t Turn) {
		if isFixRequest(t.Prompt) {
			fn(t)
		}
	})
}

// fixLeadin is the number of tool calls before the first mutating call; false if the turn
// never mutated. The Skill call that loads lighthouse-fix-pr does not count.
func fixLeadin(t Turn) (int, bool) {
	seen := 0
	for _, rec := range t.Records {
		for _, u := range toolUses(rec) {
			if u.Name == "Skill" && u.Input["skill"] == fixPRSkillName {
				continue
			}
			if isMutating(u.Name, u.Input) {
				return seen, true
			}
			seen++
		}
	}
	return 0, false
}

// fixDiagnostics counts read-only gh/git calls up to the first mutation.
func fixDiagnostics(t Turn) int {
	count := 0
	for _, rec := range t.Records {
		for _, u := range toolUses(rec) {
			if isMutating(u.Name, u.Input) {
				return count
			}
			if isDiagnostic(u.Name, u.Input) {
				count++
			}
		}
	}
	return count
}

func overrunsScope(t Turn) bool {
	loaded := loadedSkills(t)
	return slices.Contains(loaded, fixPRSkillName) &&
		slices.ContainsFunc(loaded, func(s string) bool { return slices.Contains(scopeSkills, s) })
}

func (m *fixPRMeasurer) leadin() (object, error) {
	var counts []int
	without := 0
	err := m.eachFixTurn(func(t Turn) {
		if v, ok := fixLeadin(t); ok {
			counts = append(counts, v)
		} else {
			without++
		}
	})
	if err != nil {
		return nil, err
	}
	return append(summary(counts), field{"without_mutation", without}), nil
}

func (m *fixPRMeasurer) diagnose() (object, error) {
	var counts []int
	err := m.eachFixTurn(func(t Turn) { counts = append(counts, fixDiagnostics(t)) })
	if err != nil {
		return nil, err
	}
	return summary(counts), nil
}

func (m *fixPRMeasurer) mistrigger() (object, error) {
	var loaded, missed int
	var examples []string
	err := m.eachTurn(func(t Turn) {
		if !slices.Contains(loadedSkills(t), fixPRSkillName) {
			return
		}
		loaded++
		if !isFixRequest(t.Prompt) {
			missed++
			examples = append(examples, truncRunes(t.Prompt, 120))
		}
	})
	if err != nil {
		return nil, err
	}
	return object{
		{"loaded", loaded},
		{"without_fix_request", missed},
		{"examples", examples[:min(len(examples), 5)]},
	}, nil
}

func (m *fixPRMeasurer) scope() (object, error) {
	var loaded, outside int
	err := m.eachTurn(func(t Turn) {
		if !slices.Contains(loadedSkills(t), fixPRSkillName) {
			return
		}
		loaded++
		if overrunsScope(t) {
			outside++
		}
	})
	if err != nil {
		return nil, err
	}
	return object{{"loaded", loaded}, {"past_the_boundary", outside}}, nil
}

// RunFixPR prints the fix-pr KPI table (or one row) and returns the exit code.
func RunFixPR(opts Options, out, errOut io.Writer) int {
	if opts.Metric != "" && !slices.Contains(FixPRMetrics, opts.Metric) {
		return RejectMetric(opts.Metric, FixPRMetrics, errOut)
	}
	dir, rc := resolveDir(opts, errOut)
	if rc != 0 {
		return rc
	}
	m := &fixPRMeasurer{dir: dir, since: opts.Since}
	rows := map[string]func() (object, error){
		"leadin": m.leadin, "diagnose": m.diagnose, "mistrigger": m.mistrigger, "scope": m.scope,
	}
	selected := FixPRMetrics
	if opts.Metric != "" {
		selected = []string{opts.Metric}
	}
	table := make(object, 0, len(selected))
	for _, name := range selected {
		row, err := rows[name]()
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "measure: %v\n", err)
			return 2
		}
		table = append(table, field{name, row})
	}
	_, _ = fmt.Fprintln(out, encode(table))
	return 0
}
