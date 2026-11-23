package initcmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// markerTail matches the level markers at the end of a block line: one word each, with a
// space before it.
var markerTail = regexp.MustCompile(`(?: <!--level:([^ >]+)-->)+$`)

var markerOne = regexp.MustCompile(` <!--level:([^ >]+)-->`)

// filterBlock filters the managed block of a package by level. A line that ends in one or
// more markers `<!--level:<component>-->` is dropped when none of its components is on;
// the markers are stripped from the lines that stay, so at strict the block is the source
// without its markers. The core block gets one warning line per core gate that is not on.
// A unit that is not a block is returned as it is.
func filterBlock(u packs.Unit, r levels.Resolved) packs.Unit {
	if u.Kind != packs.KindBlock {
		return u
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(string(u.Content), "\n") {
		body := strings.TrimSuffix(line, "\n")
		tail := markerTail.FindString(body)
		if tail == "" {
			out.WriteString(line)
			continue
		}
		on := false
		for _, m := range markerOne.FindAllStringSubmatch(tail, -1) {
			on = on || r.Stand(m[1]) == levels.StandOn
		}
		if on {
			out.WriteString(strings.TrimSuffix(body, tail) + line[len(body):])
		}
	}
	if u.Package == "core" {
		s := out.String()
		if s != "" && !strings.HasSuffix(s, "\n") {
			out.WriteString("\n")
		}
		for _, g := range levels.CoreGates {
			if st := r.Stand(g); st != levels.StandOn {
				fmt.Fprintf(&out, "- Warning: %s is %s in this repo (override in [components]): it works outside the chain.\n", g, st)
			}
		}
	}
	u.Content = []byte(out.String())
	return u
}

// filterBlockUnits applies filterBlock to every block among units.
func filterBlockUnits(units []packs.Unit, r levels.Resolved) []packs.Unit {
	out := make([]packs.Unit, len(units))
	for i, u := range units {
		out[i] = filterBlock(u, r)
	}
	return out
}
