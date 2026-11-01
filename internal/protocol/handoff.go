package protocol

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrHandoff wraps every error of ParseHandoff.
var ErrHandoff = errors.New("protocol: handoff")

// Handoff is the fields of a `🤖 Handoff`. Lane is an ordinary field here; the caller checks
// it against its own policy.
type Handoff struct {
	Issue                                      int
	Worktree, Branch, Anchor, Next, When, Lane string
}

var issueField = regexp.MustCompile(`^#?([0-9]+)$`)

// ParseHandoff reads the fields of a handoff header, as parse_handoff of protocol.py does.
func ParseHandoff(h Header) (Handoff, error) {
	if h.Kind != KindHandoff {
		return Handoff{}, fmt.Errorf("%w: not a handoff but %s", ErrHandoff, h.Kind)
	}
	var missing []string
	for _, key := range []string{"issue", "worktree", "branch", "next", "when"} {
		if h.Fields[key] == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return Handoff{}, fmt.Errorf("%w: fields missing: %s", ErrHandoff, strings.Join(missing, ", "))
	}
	m := issueField.FindStringSubmatch(h.Fields["issue"])
	if m == nil {
		return Handoff{}, fmt.Errorf("%w: issue unreadable: %s", ErrHandoff, h.Fields["issue"])
	}
	issue, err := strconv.Atoi(m[1])
	if err != nil {
		return Handoff{}, fmt.Errorf("%w: issue unreadable: %s", ErrHandoff, h.Fields["issue"])
	}
	if when := h.Fields["when"]; when != "now" && when != "night" {
		return Handoff{}, fmt.Errorf("%w: when must be now or night, not %s", ErrHandoff, when)
	}
	return Handoff{
		Issue:    issue,
		Worktree: strings.TrimRight(h.Fields["worktree"], "/"),
		Branch:   h.Fields["branch"],
		Anchor:   h.Fields["anchor"],
		Next:     h.Fields["next"],
		When:     h.Fields["when"],
		Lane:     h.Fields["lane"],
	}, nil
}
