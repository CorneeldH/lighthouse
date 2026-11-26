package repoconfig

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Experiment is one [[experiment]] block: a share of the trajectories gets Choice for Component.
type Experiment struct {
	ID, Component, Choice string
	Share                 float64   // (0, 0.20]
	Sample                int       // > 0, variant trajectories
	Start                 time.Time // midnight local time, from "YYYY-MM-DD"
}

// Assignment is the label of the variant: "variant:<Component>=<Choice>".
func (a Experiment) Assignment() string {
	return "variant:" + a.Component + "=" + a.Choice
}

type rawExperiment struct {
	ID        string   `toml:"id"`
	Component string   `toml:"component"`
	Choice    string   `toml:"choice"`
	Share     *float64 `toml:"share"`
	Sample    *int     `toml:"sample"`
	Start     string   `toml:"start"`
}

// maxExperimentShare is the highest share of trajectories one experiment may take.
const maxExperimentShare = 0.20

// experimentChoices maps each component that can be assigned to its allowed choices.
var experimentChoices = map[string][]string{
	"plan_review": {"codex", "opus"},
}

// experimentsFrom validates the [[experiment]] blocks. It reports every problem, naming the
// block by its position and, when it has one, its id.
func experimentsFrom(raws []rawExperiment, md toml.MetaData) ([]Experiment, error) {
	var errs []error
	for _, k := range md.Undecoded() {
		if key := k.String(); key == "experiment" || strings.HasPrefix(key, "experiment.") {
			errs = append(errs, fmt.Errorf("unknown key %s", key))
		}
	}
	var out []Experiment
	seen := map[string]bool{}
	for i, r := range raws {
		at := fmt.Sprintf("experiment[%d]", i)
		if r.ID != "" {
			at = fmt.Sprintf("experiment[%d] %q", i, r.ID)
		}
		fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(at+": "+format, a...)) }
		switch {
		case r.ID == "":
			fail("id is required")
		case seen[r.ID]:
			fail("duplicate id %q", r.ID)
		}
		seen[r.ID] = true
		choices, ok := experimentChoices[r.Component]
		switch {
		case r.Component == "":
			fail("component is required")
		case !ok:
			fail("component %q is nog niet indeelbaar", r.Component)
		case !slices.Contains(choices, r.Choice):
			fail("choice %q is not one of %s", r.Choice, strings.Join(choices, ", "))
		}
		e := Experiment{ID: r.ID, Component: r.Component, Choice: r.Choice}
		switch {
		case r.Share == nil:
			fail("share is required")
		case *r.Share <= 0 || *r.Share > maxExperimentShare:
			fail("share %v must be above 0 and at most %v", *r.Share, maxExperimentShare)
		default:
			e.Share = *r.Share
		}
		switch {
		case r.Sample == nil:
			fail("sample is required")
		case *r.Sample < 1:
			fail("sample must be at least 1")
		default:
			e.Sample = *r.Sample
		}
		if r.Start == "" {
			fail("start is required")
		} else if s, err := time.ParseInLocation(time.DateOnly, r.Start, time.Local); err != nil {
			fail("start %q is not a date YYYY-MM-DD", r.Start)
		} else {
			e.Start = s
		}
		out = append(out, e)
	}
	return out, errors.Join(errs...)
}
