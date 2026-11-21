package hook

import (
	"slices"
	"sort"
	"strings"
)

// couldNotCheck is the token of a gate that cannot check; such a block carries no reason ID.
const couldNotCheck = "COULD NOT CHECK"

// Reason is one declared way a hook blocks: the hook name and a stable ID.
type Reason struct{ Hook, ID string }

// Tag is the marker a block message carries: "[<hook>/<id>]".
func (r Reason) Tag() string { return "[" + r.Hook + "/" + r.ID + "]" }

// reasons holds the declared IDs per hook, in declaration order; guarded by mu.
var reasons = map[string][]string{}

// Declare registers the block reasons of hookName and returns them. It panics on an empty or
// duplicate ID, and on a second Declare for the same hook: that is a programming error.
func Declare(hookName string, ids ...string) []Reason {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := reasons[hookName]; ok {
		panic("hook: reasons of " + hookName + " declared twice")
	}
	out := make([]Reason, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			panic("hook: empty reason ID for " + hookName)
		}
		if seen[id] {
			panic("hook: duplicate reason " + hookName + "/" + id)
		}
		seen[id] = true
		out = append(out, Reason{hookName, id})
	}
	reasons[hookName] = slices.Clone(ids)
	return out
}

// Reasons returns the IDs of hookName in declaration order; false when it declared none.
func Reasons(hookName string) ([]string, bool) {
	mu.Lock()
	defer mu.Unlock()
	ids, ok := reasons[hookName]
	return slices.Clone(ids), ok
}

// DeclaredHooks are the hook names that declared reasons, sorted.
func DeclaredHooks() []string {
	mu.Lock()
	defer mu.Unlock()
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func declared(r Reason) bool {
	mu.Lock()
	defer mu.Unlock()
	return slices.Contains(reasons[r.Hook], r.ID)
}

// Block is a blocking result (exit 2) whose first line starts with the tag of r. It panics when
// r was not declared.
func Block(r Reason, stderr string) Result {
	if !declared(r) {
		panic("hook: undeclared reason " + r.Tag())
	}
	return Result{Code: 2, Stderr: r.Tag() + " " + stderr}
}

// BlockTagged is a blocking result for a message that already carries a tag per finding line.
// It panics when a tag in used is not in stderr or was not declared.
func BlockTagged(stderr string, used ...Reason) Result {
	for _, r := range used {
		if !declared(r) {
			panic("hook: undeclared reason " + r.Tag())
		}
		if !strings.Contains(stderr, r.Tag()) {
			panic("hook: message lacks " + r.Tag())
		}
	}
	return Result{Code: 2, Stderr: stderr}
}

// CouldNotCheck is a blocking result for a gate that cannot check. It carries no reason ID and
// panics when the message lacks the COULD NOT CHECK token.
func CouldNotCheck(stderr string) Result {
	if !strings.Contains(stderr, couldNotCheck) {
		panic("hook: message lacks " + couldNotCheck)
	}
	return Result{Code: 2, Stderr: stderr}
}
