// Package settings merges the hook registrations of the lighthouse packages into
// .claude/settings.json (spec, init punt 5).
//
// A registration whose command points into .claude/hooks/lighthouse/ belongs to
// lighthouse; every other registration belongs to the project and keeps its place.
// A file whose shape this package does not understand is an error, never rewritten.
package settings

import (
	"fmt"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

type group struct {
	event   string
	index   int
	matcher string
	obj     *orderedjson.Object
	entries []any
}

// walk validates the hooks section of doc and returns its groups in file order.
// events is nil when doc has no "hooks" key.
func walk(doc *orderedjson.Object) (*orderedjson.Object, []*group, error) {
	hv, ok := doc.Get("hooks")
	if !ok {
		return nil, nil, nil
	}
	events, ok := hv.(*orderedjson.Object)
	if !ok {
		return nil, nil, fmt.Errorf(`.claude/settings.json: "hooks" is not an object`)
	}
	var groups []*group
	for _, event := range events.Keys() {
		gv, _ := events.Get(event)
		arr, ok := gv.([]any)
		if !ok {
			return nil, nil, fmt.Errorf(".claude/settings.json: hooks.%s is not an array", event)
		}
		for i, g := range arr {
			obj, ok := g.(*orderedjson.Object)
			if !ok {
				return nil, nil, fmt.Errorf(".claude/settings.json: hooks.%s[%d] is not an object", event, i)
			}
			matcher := ""
			if mv, ok := obj.Get("matcher"); ok {
				if matcher, ok = mv.(string); !ok {
					return nil, nil, fmt.Errorf(".claude/settings.json: hooks.%s[%d].matcher is not a string", event, i)
				}
			}
			ev, _ := obj.Get("hooks")
			entries, ok := ev.([]any)
			if !ok {
				return nil, nil, fmt.Errorf(".claude/settings.json: hooks.%s[%d].hooks is not an array", event, i)
			}
			for j, e := range entries {
				where := fmt.Sprintf("hooks.%s[%d].hooks[%d]", event, i, j)
				entry, ok := e.(*orderedjson.Object)
				if !ok {
					return nil, nil, fmt.Errorf(".claude/settings.json: %s is not an object", where)
				}
				cv, hasCmd := entry.Get("command")
				if _, isStr := cv.(string); hasCmd && !isStr {
					return nil, nil, fmt.Errorf(".claude/settings.json: %s.command is not a string", where)
				}
				if tv, _ := entry.Get("type"); tv == "command" && !hasCmd {
					return nil, nil, fmt.Errorf(".claude/settings.json: %s has type command but no command", where)
				}
			}
			groups = append(groups, &group{event: event, index: i, matcher: matcher, obj: obj, entries: entries})
		}
	}
	return events, groups, nil
}

func command(entry any) string {
	obj, _ := entry.(*orderedjson.Object)
	if obj == nil {
		return ""
	}
	c, _ := obj.Get("command")
	s, _ := c.(string)
	return s
}

// Registrations returns every registration in doc, in file order.
func Registrations(doc *orderedjson.Object) ([]packs.Registration, error) {
	_, groups, err := walk(doc)
	if err != nil {
		return nil, err
	}
	var regs []packs.Registration
	for _, g := range groups {
		for _, e := range g.entries {
			regs = append(regs, packs.Registration{Event: g.event, Matcher: g.matcher, Entry: e.(*orderedjson.Object)})
		}
	}
	return regs, nil
}

// Commands returns the command of every command hook in doc, in file order.
func Commands(doc *orderedjson.Object) ([]string, error) {
	regs, err := Registrations(doc)
	if err != nil {
		return nil, err
	}
	var cmds []string
	for _, r := range regs {
		if c := command(r.Entry); c != "" {
			cmds = append(cmds, c)
		}
	}
	return cmds, nil
}

// Apply makes the lighthouse registrations in doc exactly want. An existing one keeps
// its place; a new one goes last in the group of its event and matcher, or in a new
// group last in its event. A lighthouse registration not in want is removed, and so is
// a group or event that becomes empty by that. Project registrations stay as they are.
func Apply(doc *orderedjson.Object, want []packs.Registration) error {
	events, groups, err := walk(doc)
	if err != nil {
		return err
	}
	wanted := map[string]packs.Registration{}
	for _, r := range want {
		wanted[packs.HookID(r.Event, r.Matcher, command(r.Entry))] = r
	}
	placed := map[string]bool{}
	emptied := map[*group]bool{}
	for _, g := range groups {
		var kept []any
		removed := false
		for _, e := range g.entries {
			c := command(e)
			if !packs.IsLighthouseCommand(c) {
				kept = append(kept, e)
				continue
			}
			id := packs.HookID(g.event, g.matcher, c)
			if r, ok := wanted[id]; ok && !placed[id] {
				kept = append(kept, r.Entry)
				placed[id] = true
				continue
			}
			removed = true
		}
		if kept == nil {
			kept = []any{}
		}
		g.entries = kept
		g.obj.Set("hooks", kept)
		if removed && len(kept) == 0 {
			emptied[g] = true
		}
	}
	if len(emptied) > 0 {
		for _, event := range events.Keys() {
			gv, _ := events.Get(event)
			arr := gv.([]any)
			var keep []any
			lost := false
			for _, g := range groups {
				if g.event != event {
					continue
				}
				if emptied[g] {
					lost = true
					continue
				}
				keep = append(keep, arr[g.index])
			}
			if lost && len(keep) == 0 {
				events.Delete(event)
			} else if lost {
				events.Set(event, keep)
			}
		}
	}
	for _, r := range want {
		id := packs.HookID(r.Event, r.Matcher, command(r.Entry))
		if placed[id] {
			continue
		}
		placed[id] = true
		if events == nil {
			events = orderedjson.NewObject()
			doc.Set("hooks", events)
		}
		appendRegistration(events, r)
	}
	return nil
}

func appendRegistration(events *orderedjson.Object, r packs.Registration) {
	gv, _ := events.Get(r.Event)
	arr, _ := gv.([]any)
	for _, g := range arr {
		obj := g.(*orderedjson.Object)
		mv, _ := obj.Get("matcher")
		m, _ := mv.(string)
		if m != r.Matcher {
			continue
		}
		ev, _ := obj.Get("hooks")
		obj.Set("hooks", append(ev.([]any), r.Entry))
		return
	}
	g := orderedjson.NewObject()
	if r.Matcher != "" {
		g.Set("matcher", r.Matcher)
	}
	g.Set("hooks", []any{r.Entry})
	events.Set(r.Event, append(arr, g))
}

// allowList returns the permissions object and its allow array, validated; either is
// nil when absent.
func allowList(doc *orderedjson.Object) (*orderedjson.Object, []any, error) {
	pv, ok := doc.Get("permissions")
	if !ok {
		return nil, nil, nil
	}
	perms, ok := pv.(*orderedjson.Object)
	if !ok {
		return nil, nil, fmt.Errorf(`.claude/settings.json: "permissions" is not an object`)
	}
	av, ok := perms.Get("allow")
	if !ok {
		return perms, nil, nil
	}
	allow, ok := av.([]any)
	if !ok {
		return nil, nil, fmt.Errorf(".claude/settings.json: permissions.allow is not an array")
	}
	for i, r := range allow {
		if _, ok := r.(string); !ok {
			return nil, nil, fmt.Errorf(".claude/settings.json: permissions.allow[%d] is not a string", i)
		}
	}
	return perms, allow, nil
}

// Allow returns the rules in permissions.allow, in file order.
func Allow(doc *orderedjson.Object) ([]string, error) {
	_, allow, err := allowList(doc)
	if err != nil {
		return nil, err
	}
	rules := make([]string, 0, len(allow))
	for _, r := range allow {
		rules = append(rules, r.(string))
	}
	return rules, nil
}

// ApplyAllow adds every rule of want that permissions.allow lacks, last, and removes
// every rule of drop. Other rules keep their place. Missing "permissions" or "allow"
// are created, last in their object, only when a rule is added.
func ApplyAllow(doc *orderedjson.Object, want, drop []string) error {
	perms, allow, err := allowList(doc)
	if err != nil {
		return err
	}
	dropSet := map[string]bool{}
	for _, r := range drop {
		dropSet[r] = true
	}
	have := map[string]bool{}
	out := make([]any, 0, len(allow)+len(want))
	for _, r := range allow {
		if dropSet[r.(string)] {
			continue
		}
		have[r.(string)] = true
		out = append(out, r)
	}
	for _, r := range want {
		if !have[r] {
			have[r] = true
			out = append(out, r)
		}
	}
	if len(out) == len(allow) && len(drop) == 0 {
		return nil
	}
	if allow == nil && len(out) == 0 {
		return nil
	}
	if perms == nil {
		perms = orderedjson.NewObject()
		doc.Set("permissions", perms)
	}
	perms.Set("allow", out)
	return nil
}
