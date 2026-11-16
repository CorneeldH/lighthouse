package agentaudit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
)

// Conflicts reports the conflicts of spec §4: blocking pairs outside lighthouse,
// contradicting permission rules across sources, registered hooks without a script and
// hook scripts without a registration. It reads only the inventory and stats scripts.
func Conflicts(inv *Inventory, env Env) []Finding {
	fs := findings{}
	blockingPairs(fs, inv.Hooks)
	permissionConflicts(fs, inv.Permissions)
	missingScripts(fs, inv.Hooks, env.HookNames)
	unregisteredScripts(fs, inv)
	return fs.list()
}

// HookStatuses is a COULD NOT CHECK status per hook whose script exists but could not be
// read, named <source>/<path>: such a hook is neither blocking nor an overlap candidate,
// and it is not clean either. A script that does not exist is a missing-script conflict
// instead.
func HookStatuses(inv *Inventory) []Status {
	var out []Status
	seen := map[string]bool{}
	for _, h := range inv.Hooks {
		id := hookID(h)
		if h.Unreadable == "" || notExist(h.Script) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Status{Source: id, State: StateCouldNotCh, Reason: h.Unreadable})
	}
	return out
}

func notExist(p string) bool {
	_, err := os.Stat(p)
	return errors.Is(err, fs.ErrNotExist)
}

// blockingPairs reports two blocking hooks on the same event and normalized matcher,
// neither of them lighthouse's. An unreadable hook is never Blocking, so never in a pair.
func blockingPairs(fs findings, hooks []Hook) {
	var cands []Hook
	for _, h := range hooks {
		if h.Blocking && h.Source != SourceLighthouse {
			cands = append(cands, h)
		}
	}
	for i, a := range cands {
		for _, b := range cands[i+1:] {
			m := normalizeMatcher(a.Matcher)
			if a.Event != b.Event || m != normalizeMatcher(b.Matcher) || sameHook(a, b) {
				continue
			}
			first, second := a, b
			if hookID(second) < hookID(first) {
				first, second = second, first
			}
			key := fmt.Sprintf("%s:%s:%s:%s|%s", KindBlockingPair, a.Event, m, hookID(first), hookID(second))
			text := fmt.Sprintf("%s: %s en %s blokkeren allebei", eventLabel(a.Event, m), hookLabel(first), hookLabel(second))
			fs.add(KindBlockingPair, key, text, first.Source, second.Source)
		}
	}
}

// sameHook reports whether a and b run the same thing: the same script, also when two
// settings files register it, or the same key.
func sameHook(a, b Hook) bool {
	return (a.Script != "" && a.Script == b.Script) || hookID(a) == hookID(b)
}

// permissionConflicts reports an allow rule in one source against a deny or ask rule in
// another, where one rule covers the other. The key names the narrower rule.
func permissionConflicts(fs findings, perms []Permission) {
	for _, a := range perms {
		if a.Kind != "allow" {
			continue
		}
		for _, b := range perms {
			if b.Kind == "allow" || b.Source == a.Source {
				continue
			}
			var rule string
			switch {
			case covers(a.Rule, b.Rule):
				rule = b.Rule
			case covers(b.Rule, a.Rule):
				rule = a.Rule
			default:
				continue
			}
			text := fmt.Sprintf("%s allow %s tegenover %s %s %s", a.Source, a.Rule, b.Source, b.Kind, b.Rule)
			fs.add(KindPermission, KindPermission+":"+rule, text, a.Source, b.Source)
		}
	}
}

// covers reports whether permission rule a matches everything rule b matches: the same
// rule, a bare tool against a use of it, `Tool(prefix:*)` against `Tool(prefix …)`, or
// `Tool(prefix*)` against `Tool(prefix…)`.
func covers(a, b string) bool {
	if a == b {
		return true
	}
	toolA, argA, hasA := splitRule(a)
	toolB, argB, hasB := splitRule(b)
	switch {
	case toolA != toolB:
		return false
	case !hasA:
		return true
	case !hasB:
		return false
	}
	if prefix, ok := strings.CutSuffix(argA, ":*"); ok {
		argB = strings.TrimSuffix(argB, ":*")
		return argB == prefix || strings.HasPrefix(argB, prefix+" ")
	}
	if prefix, ok := strings.CutSuffix(argA, "*"); ok {
		return strings.HasPrefix(argB, prefix)
	}
	return false
}

// splitRule splits `Tool(arg)` into its tool and argument; has is false for a bare tool.
func splitRule(rule string) (tool, arg string, has bool) {
	open := strings.IndexByte(rule, '(')
	if open < 0 || !strings.HasSuffix(rule, ")") {
		return rule, "", false
	}
	return rule[:open], rule[open+1 : len(rule)-1], true
}

// missingScripts reports a registered hook whose script does not exist, and a lighthouse
// `run.sh <name>` whose name the binary does not know. An inline command has no script.
func missingScripts(fs findings, hooks []Hook, hookNames []string) {
	known := map[string]bool{}
	for _, n := range hookNames {
		known[n] = true
	}
	for _, h := range hooks {
		var reason string
		name, isRunSh := runShName(h.Command)
		switch {
		case h.Script != "" && notExist(h.Script):
			reason = " bestaat niet"
		case h.Source == SourceLighthouse && isRunSh && !known[name]:
			reason = ": de binary kent geen hook " + name
		default:
			continue
		}
		text := fmt.Sprintf("%s: %s (in %s)%s", eventLabel(h.Event, h.Matcher), hookLabel(h), h.File, reason)
		fs.add(KindMissing, KindMissing+":"+h.Source+"/"+h.Command, text, h.Source)
	}
}

// unregisteredScripts reports a file under a hooks directory that no hook runs and whose
// name appears in no hook's script or inline command (a helper is named there).
func unregisteredScripts(fs findings, inv *Inventory) {
	for _, entry := range inv.Scripts {
		source, shown, _ := strings.Cut(entry, "/")
		named := regexp.MustCompile(`(^|[^\w.-])` + regexp.QuoteMeta(path.Base(shown)) + `($|[^\w-])`)
		used := false
		for _, h := range inv.Hooks {
			if (h.Script != "" && h.Shown == shown) || named.Match(h.Content) {
				used = true
				break
			}
		}
		if !used {
			fs.add(KindUnregistered, KindUnregistered+":"+entry, source+" "+shown+": geen hook noemt dit script", source)
		}
	}
}
