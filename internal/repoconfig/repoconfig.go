// Package repoconfig reads the parts of a served repository's lighthouse.toml that
// lighthouse tick needs: the forge slug, the board, and the [controller] table.
package repoconfig

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Repo is the dispatch-relevant view of one lighthouse.toml.
type Repo struct {
	Dir                  string
	Slug                 string // [forge] repo
	Board                *Board // nil when [forge.board] is absent
	Controller           *Controller
	UsageGuardController bool // [usage_guard] controller
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
	Profile      string `toml:"profile"`
	SpecReview   *bool  `toml:"spec_review"`
	PlanReview   *bool  `toml:"plan_review"`
	AtRoundLimit string `toml:"bij_rondegrens"`
}

type rawFile struct {
	Forge struct {
		Repo  string `toml:"repo"`
		Board *Board `toml:"board"`
	} `toml:"forge"`
	Controller *rawController `toml:"controller"`
	UsageGuard struct {
		Controller bool `toml:"controller"`
	} `toml:"usage_guard"`
}

// Load reads <dir>/lighthouse.toml.
func Load(dir string) (Repo, error) {
	path := filepath.Join(dir, "lighthouse.toml")
	var raw rawFile
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return Repo{}, fmt.Errorf("read %s: %w", path, err)
	}
	if raw.Forge.Repo == "" {
		return Repo{}, fmt.Errorf("%s: missing [forge] repo", path)
	}
	r := Repo{
		Dir:                  dir,
		Slug:                 raw.Forge.Repo,
		Board:                raw.Forge.Board,
		UsageGuardController: raw.UsageGuard.Controller,
	}
	if c := raw.Controller; c != nil {
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
	return r, nil
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
