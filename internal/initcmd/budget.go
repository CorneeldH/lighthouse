package initcmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

const ceilingKey = "project.instructions_word_ceiling"

var errNoClaudeMD = errors.New("CLAUDE.md does not exist")

// content is the text a path holds before or after the run; ok false when it is absent.
func (p *planned) content(after bool) (b []byte, ok bool) {
	if p == nil {
		return nil, false
	}
	if !after {
		return p.old, p.existed
	}
	if p.delete {
		return nil, false
	}
	return p.new, p.new != nil
}

func fileWords(plan map[string]*planned, rel string, after bool, count func([]byte) (int, error)) (int, error) {
	b, ok := plan[rel].content(after)
	if !ok {
		return 0, nil
	}
	return count(b)
}

// instructionWords counts what check 10 counts: CLAUDE.md, plus AGENTS.md when it
// exists, before or after the run. @AGENTS.md is not expanded.
// Known gap: when CLAUDE.md is a symlink to AGENTS.md the two are separate plan keys, so
// init counts old+new where check 10 counts new+new; the total is off by the delta. The
// "does not import AGENTS.md" warning fires in that setup.
func instructionWords(plan map[string]*planned, after bool, count func([]byte) (int, error)) (label string, words int, err error) {
	if c := plan[claudeMDPath]; c != nil && c.unreadable != nil {
		return "", 0, fmt.Errorf("CLAUDE.md is not readable: %w", c.unreadable)
	}
	if _, ok := plan[claudeMDPath].content(after); !ok && after {
		return "", 0, errNoClaudeMD
	}
	label = claudeMDPath
	if _, ok := plan[agentsMDPath].content(after); ok {
		label += " + " + agentsMDPath
	}
	for _, rel := range []string{claudeMDPath, agentsMDPath} {
		n, err := fileWords(plan, rel, after, count)
		if err != nil {
			return "", 0, err
		}
		words += n
	}
	return label, words, nil
}

// reportBudget says what the instruction blocks add to AGENTS.md and where the
// instruction files end up against the ceiling. It stays silent when neither file
// changes, and it never stops init: over the ceiling is a warning.
func reportBudget(o Options, plan map[string]*planned, lookup packs.Lookup) {
	notMeasurable := func(reason any) {
		_, _ = fmt.Fprintf(o.Err, "instruction blocks: budget not measurable: %v\n", reason)
	}
	// Never silent about an unreadable CLAUDE.md, also when nothing else changes.
	if c := plan[claudeMDPath]; c != nil && c.unreadable != nil {
		notMeasurable(fmt.Errorf("CLAUDE.md is not readable: %w", c.unreadable))
		return
	}
	// A missing CLAUDE.md without instruction changes stays silent on purpose: the budget
	// does not move, and check 10 itself skips loudly when CLAUDE.md is missing.
	if !plan[claudeMDPath].changesOrNil() && !plan[agentsMDPath].changesOrNil() {
		return
	}
	if _, _, err := instructionWords(plan, false, o.CountWords); err != nil {
		notMeasurable(err)
		return
	}
	label, total, err := instructionWords(plan, true, o.CountWords)
	if err != nil {
		notMeasurable(err)
		return
	}
	before, err := fileWords(plan, agentsMDPath, false, o.CountWords)
	if err != nil {
		notMeasurable(err)
		return
	}
	after, err := fileWords(plan, agentsMDPath, true, o.CountWords)
	if err != nil {
		notMeasurable(err)
		return
	}
	raw, ok, err := lookup(ceilingKey)
	if err != nil {
		notMeasurable(err)
		return
	}
	if !ok {
		notMeasurable(ceilingKey + " is not set")
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		notMeasurable(fmt.Sprintf("%s is %q, not a number", ceilingKey, raw))
		return
	}
	_, _ = fmt.Fprintf(o.Out, "instruction blocks: %+d words; %s becomes %d, ceiling %d\n", after-before, label, total, limit)
	if total > limit {
		_, _ = fmt.Fprintf(o.Err, "warning: %s is %d words over the ceiling of %d; trim it or raise [project] instructions_word_ceiling\n", label, total-limit, limit)
	}
}

func (p *planned) changesOrNil() bool { return p != nil && p.changes() }
