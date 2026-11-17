package packs

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// Guard is the signature of one lighthouse hook: what it watches and what it does. The
// agent audit matches it against hooks from other sources to find overlap.
type Guard struct {
	Name    string     // hook name, the TOML table key
	Events  []string   `toml:"events"`
	Matcher string     `toml:"matcher"`
	Does    string     `toml:"does"`
	Terms   [][]string `toml:"terms"`
}

// parseGuards reads guards.toml: one table per hook, each with events, a matcher, a
// description and terms (every group must match; one term per group is enough). The
// result is sorted by name. name is the package name, used in errors.
func parseGuards(name string, data []byte) ([]Guard, error) {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("package %s: guards.toml: %s", name, fmt.Sprintf(format, a...))
	}
	var raw map[string]Guard
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fail("%v", err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return nil, fail("unknown key %s", extra[0])
	}
	guards := make([]Guard, 0, len(raw))
	for hook, g := range raw {
		g.Name = hook
		if len(g.Events) == 0 {
			return nil, fail("%s: no events", hook)
		}
		if len(g.Terms) == 0 {
			return nil, fail("%s: no terms", hook)
		}
		for i, group := range g.Terms {
			if len(group) == 0 {
				return nil, fail("%s: terms[%d] is an empty group", hook, i)
			}
			for _, term := range group {
				if term == "" {
					return nil, fail("%s: terms[%d] holds an empty term", hook, i)
				}
			}
		}
		guards = append(guards, g)
	}
	sort.Slice(guards, func(i, j int) bool { return guards[i].Name < guards[j].Name })
	return guards, nil
}
