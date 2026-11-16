package checkinstr

import (
	"errors"
	"fmt"
	"os"

	"github.com/CorneeldH/lighthouse/internal/wordcount"
)

// InstructionWords counts the always-loaded instructions at root the way check 10 does: CLAUDE.md
// plus the AGENTS.md it imports, with wordcount.Count. label names what was counted. err says
// why nothing was counted, as the text of the SKIPPED line after "size: ".
func InstructionWords(root string) (label string, words int, err error) {
	claude, agents := root+"/CLAUDE.md", root+"/AGENTS.md"
	if !isFile(claude) {
		return "", 0, fmt.Errorf("'%s' does not exist, so there is nothing to count here", claude)
	}
	label, counted := "CLAUDE.md", []string{claude}
	if isFile(agents) {
		label, counted = "CLAUDE.md + AGENTS.md", append(counted, agents)
	}
	for _, file := range counted {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", 0, fmt.Errorf("'%s' is not readable, so there is nothing to count here", file)
		}
		n, err := wordcount.Count(data)
		if errors.Is(err, wordcount.ErrNoWC) {
			return "", 0, errors.New("'wc' is missing on this machine, so CLAUDE.md cannot be counted here")
		}
		if err != nil {
			return "", 0, fmt.Errorf("'wc' could not count '%s', so there is nothing to count here", file)
		}
		words += n
	}
	return label, words, nil
}

// Check 10: without a counter the word count creeps up again. CLAUDE.md is an `@AGENTS.md`
// import plus the Claude remainder, so the check counts what Claude Code loads: both.
func checkSize(rp *repo, r *report) {
	label, words, err := InstructionWords(rp.root)
	switch {
	case err != nil:
		r.skip("size: " + err.Error())
	case words > rp.ceiling:
		r.fail(fmt.Sprintf("size: %s counts %d words, ceiling is %d", label, words, rp.ceiling))
	default:
		r.ok(fmt.Sprintf("%s counts %d words, within the ceiling of %d", label, words, rp.ceiling))
	}
}
