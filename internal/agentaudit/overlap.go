package agentaudit

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

// Overlap reports the candidates of spec §3: a hook outside lighthouse on an event of a
// guard, with a matcher that can hit the same tool, whose content has a hit in every
// terms group of that guard (case-insensitive). A hook whose script could not be read is
// no candidate (HookStatuses reports it). One candidate per guard and hook, however many
// events or files register it.
func Overlap(inv *Inventory, guards []packs.Guard) []Finding {
	type candidate struct {
		h      Hook
		g      packs.Guard
		events []string
		hits   []string
	}
	byKey := map[string]*candidate{}
	for _, g := range guards {
		for _, h := range inv.Hooks {
			if h.Source == SourceLighthouse || h.Unreadable != "" ||
				!slices.Contains(g.Events, h.Event) || !matcherHits(g.Matcher, h.Matcher) {
				continue
			}
			hits, ok := termHits(g.Terms, string(h.Content))
			if !ok {
				continue
			}
			key := KindOverlap + ":" + g.Name + "|" + hookID(h)
			c, seen := byKey[key]
			if !seen {
				c = &candidate{h: h, g: g, hits: hits}
				byKey[key] = c
			}
			c.events = appendNew(c.events, eventLabel(h.Event, h.Matcher))
		}
	}
	out := make([]Finding, 0, len(byKey))
	for key, c := range byKey {
		quoted := make([]string, len(c.hits))
		for i, hit := range c.hits {
			quoted[i] = fmt.Sprintf("%q", hit)
		}
		out = append(out, Finding{
			Key:  key,
			Kind: KindOverlap,
			Text: []string{
				fmt.Sprintf("%s (%s) lijkt op lighthouse %s", hookLabel(c.h), strings.Join(c.events, ", "), c.g.Name),
				"lighthouse: " + c.g.Does,
				"treffers: " + strings.Join(quoted, ", "),
			},
			Sources: sortedPair(SourceLighthouse, c.h.Source),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func sortedPair(a, b string) []string {
	if b < a {
		a, b = b, a
	}
	return []string{a, b}
}

// termHits is the first term of each group found in content, ignoring case; ok is false
// when a group has no hit.
func termHits(groups [][]string, content string) (hits []string, ok bool) {
	lower := strings.ToLower(content)
	for _, group := range groups {
		i := slices.IndexFunc(group, func(term string) bool { return strings.Contains(lower, strings.ToLower(term)) })
		if i < 0 {
			return nil, false
		}
		hits = append(hits, group[i])
	}
	return hits, true
}

// normalizeMatcher is a matcher with "*" as empty and the alternatives of an alternation
// trimmed, sorted and without duplicates, so `Write|Edit` equals `Edit|Write`.
func normalizeMatcher(m string) string {
	m = strings.TrimSpace(m)
	if m == "*" {
		return ""
	}
	return strings.Join(alternatives(m), "|")
}

func alternatives(m string) []string {
	var out []string
	for _, part := range strings.Split(m, "|") {
		if part = strings.TrimSpace(part); part != "" {
			out = appendNew(out, part)
		}
	}
	sort.Strings(out)
	return out
}

// matcherHits reports whether two matchers can hit the same tool: equal after
// normalizing, either empty or "*" (every tool), or alternations that share a tool.
func matcherHits(a, b string) bool {
	na, nb := normalizeMatcher(a), normalizeMatcher(b)
	if na == "" || nb == "" {
		return true
	}
	for _, tool := range alternatives(na) {
		if slices.Contains(alternatives(nb), tool) {
			return true
		}
	}
	return false
}
