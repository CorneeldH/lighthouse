package dispatch

import (
	"strings"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

// placement is the profile and the node a start or resume runs on, or the reason it waits.
// A run that exists keeps its place: a resume goes where its session ran, a retry where its
// failed start went, whatever the issue's labels say now. A new run follows the location
// label once the repo has [controller.locations]: no label is the repo's own profile, one
// label a profile of [controller.locations], more than one or an unknown value a deferral.
// Without the table the label is ignored. The node is the one the profile has in host.toml;
// "" is this host.
func (c issueCtx) placement(a Action) (profile, node, deferReason string) {
	if a.Kind == KindResume {
		for _, row := range c.repo.History {
			if row.SessionID == a.SessionID {
				return c.rowPlacement(row)
			}
		}
	}
	var last *runs.Row
	for i := range c.repo.History {
		if row := &c.repo.History[i]; row.Issue == c.it.Issue && row.TriggerKey == a.TriggerKey {
			last = row
		}
	}
	if last != nil {
		return c.rowPlacement(*last)
	}
	profile = c.repo.Controller.Profile
	// Without [controller.locations] the label binds nothing: a repo that set location_prefix
	// before the table existed dispatches as it did.
	if prefix := c.repo.LocationPrefix; prefix != "" && len(c.repo.Controller.Locations) > 0 {
		var values []string
		for _, label := range c.it.Labels {
			if value, ok := strings.CutPrefix(label, prefix); ok {
				values = append(values, value)
			}
		}
		switch len(values) {
		case 0:
		case 1:
			mapped, known := c.repo.Controller.Locations[values[0]]
			if !known {
				return "", "", "locatie " + values[0] + " onbekend"
			}
			profile = mapped
		default:
			return "", "", "meer dan één locatielabel"
		}
	}
	return profile, c.profileNodes[profile], ""
}

// rowPlacement is the place a run of the history took: its profile and its node, as the claim
// recorded them. Apply has written the profile of every run since the runs table exists.
func (c issueCtx) rowPlacement(row runs.Row) (profile, node, deferReason string) {
	return row.Profile, row.Node, ""
}
