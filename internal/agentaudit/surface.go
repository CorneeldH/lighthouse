package agentaudit

import "sort"

// SourceSurface is the always-loaded surface of one source: how many skills, commands
// and agents, the characters of their names plus descriptions, and a rough token count.
type SourceSurface struct {
	Source               string
	Count, Chars, Tokens int
}

// Surface is the measured always-loaded surface of Claude Code.
type Surface struct {
	PerSource []SourceSurface // by Tokens descending, equal tokens by source name
	Top       []Skill         // the largest items, by name plus description characters
	TooLong   []Skill         // items whose description exceeds the maximum
}

// Measure counts what Claude Code always loads: name plus description of every skill,
// command and agent. Skills for another harness (Harness "codex") are inventoried but
// not part of this surface. Tokens are (chars+3)/4, a rough estimate rounded up.
func Measure(inv *Inventory, top, maxDesc int) Surface {
	var counted []Skill
	bySource := map[string]*SourceSurface{}
	for _, s := range inv.Skills {
		if s.Harness == "codex" {
			continue
		}
		counted = append(counted, s)
		ss := bySource[s.Source]
		if ss == nil {
			ss = &SourceSurface{Source: s.Source}
			bySource[s.Source] = ss
		}
		ss.Count++
		ss.Chars += size(s)
	}

	var out Surface
	for _, ss := range bySource {
		ss.Tokens = (ss.Chars + 3) / 4
		out.PerSource = append(out.PerSource, *ss)
	}
	sort.Slice(out.PerSource, func(i, j int) bool {
		a, b := out.PerSource[i], out.PerSource[j]
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		return a.Source < b.Source
	})

	sort.Slice(counted, func(i, j int) bool {
		a, b := counted[i], counted[j]
		if size(a) != size(b) {
			return size(a) > size(b)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Path < b.Path
	})
	for i := 0; i < len(counted) && i < top; i++ {
		out.Top = append(out.Top, counted[i])
	}
	for _, s := range counted {
		if s.DescChars > maxDesc {
			out.TooLong = append(out.TooLong, s)
		}
	}
	return out
}

func size(s Skill) int { return s.NameChars + s.DescChars }
