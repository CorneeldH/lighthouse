package dispatch

import (
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// placementCtx is an issue 7 of a repo with the location prefix "1-loc:", the profile
// "software" and the locations mac and container.
func placementCtx(labels []string, history []runs.Row) issueCtx {
	return issueCtx{
		repo: RepoInput{
			LocationPrefix: "1-loc:",
			Controller: repoconfig.Controller{
				Profile:   "software",
				Locations: map[string]string{"mac": "mac", "container": "container"},
			},
			History: history,
		},
		it:           Item{Issue: 7, Labels: labels},
		profileNodes: map[string]string{"mac": "node-a"},
	}
}

func TestPlacement(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		labels      []string
		wantProfile string
		wantNode    string
		wantDefer   string
	}{
		{"no label is the repo profile", "1-loc:", nil, "software", "", ""},
		{"no prefix ignores a location label", "", []string{"1-loc:mac"}, "software", "", ""},
		{"label with a node", "1-loc:", []string{"1-loc:mac"}, "mac", "node-a", ""},
		{"label of this host", "1-loc:", []string{"1-loc:container"}, "container", "", ""},
		{"other labels do not count", "1-loc:", []string{"bug", "area:x"}, "software", "", ""},
		{"unknown location", "1-loc:", []string{"1-loc:beelink"}, "", "", "locatie beelink onbekend"},
		{"case counts", "1-loc:", []string{"1-loc:Mac"}, "", "", "locatie Mac onbekend"},
		{"space counts", "1-loc:", []string{"1-loc: mac"}, "", "", "locatie  mac onbekend"},
		{"two labels", "1-loc:", []string{"1-loc:mac", "1-loc:container"}, "", "", "meer dan één locatielabel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := placementCtx(tt.labels, nil)
			c.repo.LocationPrefix = tt.prefix
			profile, node, deferReason := c.placement(Action{Kind: KindStart, TriggerKey: "start:7"})
			if profile != tt.wantProfile || node != tt.wantNode || deferReason != tt.wantDefer {
				t.Errorf("placement = (%q, %q, %q), want (%q, %q, %q)",
					profile, node, deferReason, tt.wantProfile, tt.wantNode, tt.wantDefer)
			}
		})
	}
}

// A label that changes during a run moves neither the run nor its resume: the session goes on
// where it started.
func TestPlacementResumeKeepsNode(t *testing.T) {
	history := []runs.Row{{ID: 1, Issue: 7, SessionID: "s1", TriggerKey: "start:7", Profile: "mac", Node: "node-a"}}
	c := placementCtx([]string{"1-loc:container"}, history)
	profile, node, deferReason := c.placement(Action{Kind: KindResume, SessionID: "s1", TriggerKey: "answer:5"})
	if profile != "mac" || node != "node-a" || deferReason != "" {
		t.Errorf("placement = (%q, %q, %q), want (mac, node-a, \"\")", profile, node, deferReason)
	}
}

// A resume does not wait for a label that has become unknown or double.
func TestPlacementResumeIgnoresBadLabels(t *testing.T) {
	history := []runs.Row{{ID: 1, Issue: 7, SessionID: "s1", TriggerKey: "start:7", Profile: "mac", Node: "node-a"}}
	c := placementCtx([]string{"1-loc:mac", "1-loc:beelink"}, history)
	profile, node, deferReason := c.placement(Action{Kind: KindResume, SessionID: "s1", TriggerKey: "answer:5"})
	if profile != "mac" || node != "node-a" || deferReason != "" {
		t.Errorf("placement = (%q, %q, %q), want (mac, node-a, \"\")", profile, node, deferReason)
	}
}

// The second attempt of a trigger goes where the failed first start went.
func TestPlacementRetryKeepsNode(t *testing.T) {
	history := []runs.Row{
		{ID: 1, Issue: 7, SessionID: "s1", TriggerKey: "start:7", Profile: "mac", Node: "node-a", Outcome: runs.StartFailed},
	}
	c := placementCtx([]string{"1-loc:container"}, history)
	profile, node, deferReason := c.placement(Action{Kind: KindStart, TriggerKey: "start:7", Attempt: 2})
	if profile != "mac" || node != "node-a" || deferReason != "" {
		t.Errorf("placement = (%q, %q, %q), want (mac, node-a, \"\")", profile, node, deferReason)
	}
}

// A trigger key of another issue is another trigger.
func TestPlacementRetryOtherIssue(t *testing.T) {
	history := []runs.Row{{ID: 1, Issue: 8, SessionID: "s1", TriggerKey: "start:7", Profile: "mac", Node: "node-a"}}
	c := placementCtx(nil, history)
	profile, node, _ := c.placement(Action{Kind: KindStart, TriggerKey: "start:7"})
	if profile != "software" || node != "" {
		t.Errorf("placement = (%q, %q), want (software, \"\")", profile, node)
	}
}

// A planned start with a placement defer is a defer; for a fix-pr start the deferral still
// counts as wanted, which fixpr-placement-defer-stays-open.json covers through Decide.
func TestPlannedPlacementDefer(t *testing.T) {
	c := placementCtx([]string{"1-loc:beelink"}, nil)
	c.base = Action{Repo: "o/r", Issue: 7}
	p := c.planned(Action{Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: triggerFixPR}, c.now)
	if p.Kind != KindDefer || p.Reason != "locatie beelink onbekend" || !p.fixDeferred {
		t.Errorf("planned = %+v, want a defer for a fix-pr start", p)
	}
	c = placementCtx([]string{"1-loc:mac"}, nil)
	p = c.planned(Action{Kind: KindStart, Repo: "o/r", Issue: 7}, c.now)
	if p.Kind != KindStart || p.Profile != "mac" || p.Node != "node-a" || p.node != "node-a" || p.software {
		t.Errorf("planned = %+v, want a start on mac at node-a", p)
	}
}
