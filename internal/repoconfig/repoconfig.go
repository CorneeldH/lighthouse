// Package repoconfig reads the parts of a served repository's lighthouse.toml that
// lighthouse tick needs: the forge slug, the board, and the [controller] table.
package repoconfig

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Repo is the dispatch-relevant view of one lighthouse.toml.
type Repo struct {
	Dir                  string
	Slug                 string // [forge] repo
	Kind                 string // [forge] kind, default "github"
	DefaultBranch        string // [forge] default_branch, default "main"
	Language             string // [project] language, default "en"
	Board                *Board // nil when [forge.board] is absent
	Controller           *Controller
	UsageGuardController bool // [usage_guard] controller
	Labels               Labels
	Triage               *Triage // nil when [controller.triage] is absent
	// TriageErr is an unknown key, a value below 1 or an invalid regex in [controller.triage]. Load does not
	// fail on it: it is shared with the tick, which must keep serving the repo.
	TriageErr error
}

// Labels is [labels]: the label names, defaults filled by Load. The two prefixes have no
// default and are "" when the repo has none.
type Labels struct {
	BlockedIssue, BlockedExternal, NeedsShaping, NeedsDecision, NeedsHands, NeedsTesting string
	LocationPrefix, AreaPrefix                                                           string
}

// Blocking returns the six blocked_* and needs_* label names, in that order.
func (l Labels) Blocking() []string {
	return []string{l.BlockedIssue, l.BlockedExternal, l.NeedsShaping, l.NeedsDecision, l.NeedsHands, l.NeedsTesting}
}

// Triage is [controller.triage]. Load fills the defaults: MilestonesOnly and WaitForNight
// true, MaxForwarded 5, MaxJudgments 15.
type Triage struct {
	MilestonesOnly, WaitForNight bool
	MaxForwarded, MaxJudgments   int
	// MilestoneTitleExclude is [controller.triage] milestone_title_exclude: nil means no
	// filter (absent or empty).
	MilestoneTitleExclude *regexp.Regexp
}

// Board is [forge.board]: the project and the field and option IDs.
type Board struct {
	ProjectNumber   int               `toml:"project_number"`
	ProjectID       string            `toml:"project_id"`
	StatusFieldID   string            `toml:"status_field_id"`
	PriorityFieldID string            `toml:"priority_field_id"`
	StatusOptions   map[string]string `toml:"status_options"`
	PriorityOptions map[string]string `toml:"priority_options"`
}

// Controller is [controller]. Load fills the defaults: Profile "software", SpecReview and
// PlanReview true. AtRoundLimit is read from the TOML key named in rawController.
type Controller struct {
	Profile      string
	SpecReview   bool
	PlanReview   bool
	AtRoundLimit string
}

type rawController struct {
	Profile      string     `toml:"profile"`
	SpecReview   *bool      `toml:"spec_review"`
	PlanReview   *bool      `toml:"plan_review"`
	AtRoundLimit string     `toml:"bij_rondegrens"`
	Triage       *rawTriage `toml:"triage"`
}

type rawTriage struct {
	MilestonesOnly *bool `toml:"milestones_only"`
	WaitForNight   *bool `toml:"wait_for_night"`
	MaxForwarded   *int  `toml:"max_forwarded"`
	MaxJudgments   *int  `toml:"max_judgments"`

	MilestoneTitleExclude *string `toml:"milestone_title_exclude"`
}

type rawFile struct {
	Forge struct {
		Repo          string `toml:"repo"`
		Kind          string `toml:"kind"`
		DefaultBranch string `toml:"default_branch"`
		Board         *Board `toml:"board"`
	} `toml:"forge"`
	Project struct {
		Language string `toml:"language"`
	} `toml:"project"`
	Labels struct {
		BlockedIssue    string `toml:"blocked_issue"`
		BlockedExternal string `toml:"blocked_external"`
		NeedsShaping    string `toml:"needs_shaping"`
		NeedsDecision   string `toml:"needs_decision"`
		NeedsHands      string `toml:"needs_hands"`
		NeedsTesting    string `toml:"needs_testing"`
		LocationPrefix  string `toml:"location_prefix"`
		AreaPrefix      string `toml:"area_prefix"`
	} `toml:"labels"`
	Controller *rawController `toml:"controller"`
	UsageGuard struct {
		Controller bool `toml:"controller"`
	} `toml:"usage_guard"`
}

// Load reads <dir>/lighthouse.toml.
func Load(dir string) (Repo, error) {
	path := filepath.Join(dir, "lighthouse.toml")
	var raw rawFile
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return Repo{}, fmt.Errorf("read %s: %w", path, err)
	}
	if raw.Forge.Repo == "" {
		return Repo{}, fmt.Errorf("%s: missing [forge] repo", path)
	}
	r := Repo{
		Dir:                  dir,
		Slug:                 raw.Forge.Repo,
		Kind:                 cmp.Or(raw.Forge.Kind, "github"),
		DefaultBranch:        cmp.Or(raw.Forge.DefaultBranch, "main"),
		Language:             cmp.Or(raw.Project.Language, "en"),
		Board:                raw.Forge.Board,
		UsageGuardController: raw.UsageGuard.Controller,
		Labels: Labels{
			BlockedIssue:    cmp.Or(raw.Labels.BlockedIssue, "blocked-issue"),
			BlockedExternal: cmp.Or(raw.Labels.BlockedExternal, "blocked-external"),
			NeedsShaping:    cmp.Or(raw.Labels.NeedsShaping, "needs-shaping"),
			NeedsDecision:   cmp.Or(raw.Labels.NeedsDecision, "needs-decision"),
			NeedsHands:      cmp.Or(raw.Labels.NeedsHands, "needs-hands"),
			NeedsTesting:    cmp.Or(raw.Labels.NeedsTesting, "needs-testing"),
			LocationPrefix:  raw.Labels.LocationPrefix,
			AreaPrefix:      raw.Labels.AreaPrefix,
		},
	}
	// [controller.triage] alone makes an implicit [controller] table, of type "" and not "Hash".
	if c := raw.Controller; c != nil && md.Type("controller") == "Hash" {
		r.Controller = &Controller{
			Profile:      c.Profile,
			SpecReview:   c.SpecReview == nil || *c.SpecReview,
			PlanReview:   c.PlanReview == nil || *c.PlanReview,
			AtRoundLimit: c.AtRoundLimit,
		}
		if r.Controller.Profile == "" {
			r.Controller.Profile = "software"
		}
	}
	if t := raw.Controller; t != nil && t.Triage != nil {
		r.Triage, r.TriageErr = triageFrom(t.Triage, md)
	}
	return r, nil
}

// triageFrom fills the defaults of [controller.triage] and reports an unknown key, a
// value below 1 or an invalid regex, naming the key as written in lighthouse.toml.
func triageFrom(t *rawTriage, md toml.MetaData) (*Triage, error) {
	out := &Triage{
		MilestonesOnly: t.MilestonesOnly == nil || *t.MilestonesOnly,
		WaitForNight:   t.WaitForNight == nil || *t.WaitForNight,
		MaxForwarded:   5,
		MaxJudgments:   15,
	}
	var errs []error
	for _, k := range md.Undecoded() {
		if key := k.String(); strings.HasPrefix(key, "controller.triage") {
			errs = append(errs, fmt.Errorf("unknown key %s", key))
		}
	}
	for _, n := range []struct {
		key string
		in  *int
		out *int
	}{
		{"controller.triage.max_forwarded", t.MaxForwarded, &out.MaxForwarded},
		{"controller.triage.max_judgments", t.MaxJudgments, &out.MaxJudgments},
	} {
		switch {
		case n.in == nil:
		case *n.in < 1:
			errs = append(errs, fmt.Errorf("%s must be at least 1", n.key))
		default:
			*n.out = *n.in
		}
	}
	if t.MilestoneTitleExclude != nil && *t.MilestoneTitleExclude != "" {
		re, err := regexp.Compile(*t.MilestoneTitleExclude)
		if err != nil {
			errs = append(errs, fmt.Errorf("controller.triage.milestone_title_exclude: %w", err))
		} else {
			out.MilestoneTitleExclude = re
		}
	}
	return out, errors.Join(errs...)
}

// Triageable returns nil when triage may write in r: TriageErr nil, Dispatchable, a
// [controller.triage] table and a "To do" status option; else one error joining every reason.
func Triageable(r Repo) error {
	var errs []error
	if r.TriageErr != nil {
		errs = append(errs, r.TriageErr)
	}
	if err := Dispatchable(r); err != nil {
		errs = append(errs, err)
	}
	if r.Triage == nil {
		errs = append(errs, errors.New("missing [controller.triage]"))
	}
	if r.Board != nil && r.Board.StatusOptions["To do"] == "" {
		errs = append(errs, errors.New("missing forge.board.status_options.To do"))
	}
	return errors.Join(errs...)
}

// Dispatchable returns nil when the repo can be served, else one error joining every reason.
// Each reason names the key as written in lighthouse.toml.
func Dispatchable(r Repo) error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if r.Controller == nil {
		add("missing [controller]")
	}
	if !r.UsageGuardController {
		add("usage_guard.controller is not true")
	}
	b := r.Board
	if b == nil {
		add("missing [forge.board]")
	} else {
		if b.ProjectNumber <= 0 {
			add("forge.board.project_number must be positive")
		}
		if b.ProjectID == "" {
			add("forge.board.project_id is empty")
		}
		if b.StatusFieldID == "" {
			add("forge.board.status_field_id is empty")
		}
		for _, name := range []string{"Controller", "On hold"} {
			if b.StatusOptions[name] == "" {
				add("missing forge.board.status_options.%s", name)
			}
		}
		if b.PriorityFieldID == "" {
			add("forge.board.priority_field_id is empty")
		}
	}
	return errors.Join(errs...)
}
