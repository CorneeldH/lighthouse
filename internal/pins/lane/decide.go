package lane

import (
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// Outcome is what one measurement of a pin asks of the lane.
type Outcome struct {
	// Kind is pr, conflict or behind; empty when the pin is current or not measurable.
	Kind ItemKind
	// Target is the target version (or upstream commit); for an error, the reason text.
	Target string
	// Measurable is false when the measurement ended in error.
	Measurable bool
}

// ActionOp is what the lane does to one item.
type ActionOp int

// The operations Decide returns.
const (
	Create ActionOp = iota + 1
	Update
	CloseByLane
)

// Action is one step for the lane. Item is the existing item for Update and CloseByLane;
// Kind and Target describe the item to create or the new body for an Update.
type Action struct {
	Op     ActionOp
	Item   forge.Item
	Kind   ItemKind
	Target string
}

// Decide applies the two tracks of "Geen dubbel werk" (behind track: pr, conflict, behind;
// error track: error) to the items found for key. It is pure: no I/O.
func Decide(key string, out Outcome, found []forge.Item) []Action {
	var openBehind, openError *forge.Item
	humanClosed := map[string]bool{} // target -> a human closed a behind-track item for it
	kinds := map[int]ItemKind{}
	targets := map[int]string{}
	for i := range found {
		it := found[i]
		k, kind, target, ok := ParseMarker(it.Body)
		if !ok || k != key {
			continue
		}
		kinds[it.Number], targets[it.Number] = kind, target
		switch {
		case it.State == "open" && kind == KindError:
			if openError == nil {
				openError = &found[i]
			}
		case it.State == "open":
			if openBehind == nil {
				openBehind = &found[i]
			}
		case kind != KindError && !strings.Contains(it.Body, LaneClosedMarker):
			humanClosed[target] = true
		}
	}

	var actions []Action
	if !out.Measurable {
		if openError != nil {
			return append(actions, Action{Op: Update, Item: *openError, Kind: KindError, Target: out.Target})
		}
		return append(actions, Action{Op: Create, Kind: KindError, Target: out.Target})
	}
	if openError != nil {
		actions = append(actions, Action{Op: CloseByLane, Item: *openError})
	}
	if out.Kind == "" { // current: an open item stays for the human to close
		return actions
	}
	switch {
	case openBehind != nil && kinds[openBehind.Number] == out.Kind:
		if targets[openBehind.Number] != out.Target {
			actions = append(actions, Action{Op: Update, Item: *openBehind, Kind: out.Kind, Target: out.Target})
		}
	case openBehind != nil:
		actions = append(actions,
			Action{Op: CloseByLane, Item: *openBehind},
			Action{Op: Create, Kind: out.Kind, Target: out.Target})
	case humanClosed[out.Target]:
	default:
		actions = append(actions, Action{Op: Create, Kind: out.Kind, Target: out.Target})
	}
	return actions
}
