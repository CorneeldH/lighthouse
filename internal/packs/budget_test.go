package packs

import (
	"io/fs"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/agentsmd"
	"github.com/CorneeldH/lighthouse/internal/wordcount"
	"github.com/CorneeldH/lighthouse/pack"
)

// The ceilings of the instruction blocks, in words as `wc -w` counts them, markers included. A
// block points to a skill or a rule and carries no explanation. Raising a ceiling is a deliberate
// choice, made in the PR; it never follows from a block that simply grew.
var blockCeilings = map[string]int{"core": 287, "go": 50, "python": 57, "shell": 54}

// blocksCeiling is the ceiling for all blocks together, the heaviest combination.
const blocksCeiling = 448

func TestInstructionBlocksWithinCeiling(t *testing.T) {
	entries, err := fs.ReadDir(pack.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	total := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		body, err := fs.ReadFile(pack.Files, name+"/instructions/AGENTS.md")
		if err != nil {
			// A pack without a block, like expo, counts 0 words.
			continue
		}
		seen[name] = true
		doc, err := agentsmd.Set(nil, name, body)
		if err != nil {
			t.Fatal(err)
		}
		n, err := wordcount.Count(doc)
		if err != nil {
			t.Fatal(err)
		}
		total += n
		t.Logf("block %q counts %d words", name, n)
		ceiling, ok := blockCeilings[name]
		if !ok {
			t.Errorf("block %q has no ceiling in blockCeilings", name)
			continue
		}
		if n > ceiling {
			t.Errorf("block %q counts %d words, ceiling %d: trim it to pointers to a skill or rule", name, n, ceiling)
		}
	}
	for name := range blockCeilings {
		if !seen[name] {
			t.Errorf("blockCeilings has %q but there is no such block", name)
		}
	}
	t.Logf("all blocks together count %d words", total)
	if total > blocksCeiling {
		t.Errorf("all blocks together count %d words, ceiling %d: trim them to pointers to a skill or rule", total, blocksCeiling)
	}
}
