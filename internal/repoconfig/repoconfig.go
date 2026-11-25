// Package repoconfig reads the parts of a served repository's lighthouse.toml that
// lighthouse tick needs: the forge slug, the board, and the [controller] table.
package repoconfig

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/levels"
)

// Repo is the dispatch-relevant view of one lighthouse.toml.
type Repo struct {
	Dir                  string
	Slug                 string // [forge] repo
	Kind                 string // [forge] kind, default "github"
	DefaultBranch        string // [forge] default_branch, default "main"
	Host                 string // [forge] host, "" when absent
	TokenEnv             string // [forge] token_env, "" when absent
	Language             string // [project] language, default "en"
	Board                *Board // nil when [forge.board] is absent
	Controller           *Controller
	UsageGuardController bool // [usage_guard] controller
	Labels               Labels
	Triage               *Triage // nil when [controller.triage] is absent
	// TriageErr is an unknown key, a value below 1 or an invalid regex in [controller.triage]. Load
	// does not fail on it: it is shared with the tick, which must keep serving the repo.
	TriageErr error
	// ControllerErr is an unreadable .lighthouse/components.toml, needed for the review defaults of
	// [controller]. Load does not fail on it: the knobs stay true and Dispatchable reports it.
	ControllerErr error
	// Report is [report]; Sink is "stdout" when unset. ReportErr is a wrong type, an unknown sink
	// or an unusable dir. Load does not fail on it: Slug, Triageable and Dispatchable keep working.
	Report    Report
	ReportErr error
	// Schedules is [[schedule]], validated by Load; empty when the repo has none.
	Schedules []Schedule
	// Paths is [paths]: repo-relative paths, "" when a key is not set.
	Paths Paths
	// SpecsDir and PlansDir are paths.specs and paths.plans, relative to the repo, with the
	// defaults of internal/config: the workflow pusher gives the files under them to its intent
	// reviewer.
	SpecsDir, PlansDir string
}

// Paths is the part of [paths] that lighthouse reads here: the labels document, the tenets
// document, the areas directory and the strategy document, all relative to the repo.
type Paths struct {
	Labels, Tenets, Areas, Strategy string
}

// Report is [report]: whether lighthouse report runs, where it writes and, for sink "file", the
// directory relative to the repo.
type Report struct {
	Enabled   bool
	Sink, Dir string
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

// NeedsName returns the first of the four needs_* label names that labels carries, "" when none.
func (l Labels) NeedsName(labels []string) string {
	for _, name := range []string{l.NeedsShaping, l.NeedsDecision, l.NeedsHands, l.NeedsTesting} {
		if name != "" && slices.Contains(labels, name) {
			return name
		}
	}
	return ""
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
	ProjectOwner    string            `toml:"project_owner"`
	PriorityFieldID string            `toml:"priority_field_id"`
	StatusOptions   map[string]string `toml:"status_options"`
	PriorityOptions map[string]string `toml:"priority_options"`
}

// Controller is [controller]. Load fills the defaults: Profile "software"; SpecReview and
// PlanReview follow the installed level (false when skill:review-spec / skill:review-plan is off
// in .lighthouse/components.toml, else true), and an explicit key always wins; AtRoundLimit
// defaults to the continue value. AtRoundLimit is read from the TOML key named in rawController.
type Controller struct {
	Profile      string
	SpecReview   bool
	PlanReview   bool
	AtRoundLimit string
	// Locations maps the value after [labels] location_prefix to a profile name; nil without
	// the [controller.locations] table.
	Locations map[string]string
}

type rawController struct {
	Profile      string     `toml:"profile"`
	SpecReview   *bool      `toml:"spec_review"`
	PlanReview   *bool      `toml:"plan_review"`
	AtRoundLimit string     `toml:"bij_rondegrens"`
	Triage       *rawTriage `toml:"triage"`

	Locations map[string]string `toml:"locations"`
}

type rawTriage struct {
	MilestonesOnly        *bool   `toml:"milestones_only"`
	WaitForNight          *bool   `toml:"wait_for_night"`
	MaxForwarded          *int    `toml:"max_forwarded"`
	MaxJudgments          *int    `toml:"max_judgments"`
	MilestoneTitleExclude *string `toml:"milestone_title_exclude"`
}

type rawFile struct {
	Forge struct {
		Repo          string `toml:"repo"`
		Kind          string `toml:"kind"`
		DefaultBranch string `toml:"default_branch"`
		Host          string `toml:"host"`
		TokenEnv      string `toml:"token_env"`
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
	Paths struct {
		Labels   string `toml:"labels"`
		Tenets   string `toml:"tenets"`
		Areas    string `toml:"areas"`
		Strategy string `toml:"strategy"`
		Specs    string `toml:"specs"`
		Plans    string `toml:"plans"`
	} `toml:"paths"`
	Report   toml.Primitive `toml:"report"`
	Schedule []rawSchedule  `toml:"schedule"`
}

// DefaultLabels are the blocking label names of a repo without a repo config: the defaults of
// internal/config. The location and area prefixes have no default.
func DefaultLabels() Labels {
	return Labels{
		BlockedIssue:    defaultString("labels.blocked_issue"),
		BlockedExternal: defaultString("labels.blocked_external"),
		NeedsShaping:    defaultString("labels.needs_shaping"),
		NeedsDecision:   defaultString("labels.needs_decision"),
		NeedsHands:      defaultString("labels.needs_hands"),
		NeedsTesting:    defaultString("labels.needs_testing"),
	}
}

// defaultString is the default of key from internal/config, the one source of defaults.
// Every key asked for has a string default there; one without is a programming error, so it
// panics instead of turning into an empty default.
func defaultString(key string) string {
	v, _ := config.Default(key)
	s, ok := v.(string)
	if !ok {
		panic("repoconfig: " + key + " has no string default in internal/config")
	}
	return s
}

// Load reads <dir>/lighthouse.toml.
func Load(dir string) (Repo, error) {
	return LoadFile(filepath.Join(dir, "lighthouse.toml"))
}

// LenientTokenEnv is [forge] token_env of the lighthouse.toml in dir, read without validating
// anything else: a repo whose config is invalid (a bad [[schedule]] block, say) still names its
// token variable. "" when the file is unreadable or has none.
func LenientTokenEnv(dir string) string {
	var raw struct {
		Forge struct {
			TokenEnv string `toml:"token_env"`
		} `toml:"forge"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "lighthouse.toml"), &raw); err != nil {
		return ""
	}
	return raw.Forge.TokenEnv
}

// LoadFile reads the lighthouse.toml at path, whatever its file name (LIGHTHOUSE_TOML); Dir is
// the directory it is in.
func LoadFile(path string) (Repo, error) {
	dir := filepath.Dir(path)
	var raw rawFile
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return Repo{}, fmt.Errorf("read %s: %w", path, err)
	}
	if raw.Forge.Repo == "" {
		return Repo{}, fmt.Errorf("%s: missing [forge] repo", path)
	}
	if k := raw.Forge.Kind; k != "" && k != "github" && k != "gitlab" {
		return Repo{}, fmt.Errorf("%s: [forge] kind %q is not github or gitlab (forge.kind)", path, k)
	}
	r := Repo{
		Dir:                  dir,
		Slug:                 raw.Forge.Repo,
		Kind:                 cmp.Or(raw.Forge.Kind, "github"),
		DefaultBranch:        cmp.Or(raw.Forge.DefaultBranch, defaultString("forge.default_branch")),
		Host:                 raw.Forge.Host,
		TokenEnv:             raw.Forge.TokenEnv,
		Language:             cmp.Or(raw.Project.Language, "en"),
		Board:                raw.Forge.Board,
		UsageGuardController: raw.UsageGuard.Controller,
		Paths:                Paths{Labels: raw.Paths.Labels, Tenets: raw.Paths.Tenets, Areas: raw.Paths.Areas, Strategy: raw.Paths.Strategy},
		SpecsDir:             cmp.Or(raw.Paths.Specs, defaultString("paths.specs")),
		PlansDir:             cmp.Or(raw.Paths.Plans, defaultString("paths.plans")),
		Labels: Labels{
			BlockedIssue:    cmp.Or(raw.Labels.BlockedIssue, defaultString("labels.blocked_issue")),
			BlockedExternal: cmp.Or(raw.Labels.BlockedExternal, defaultString("labels.blocked_external")),
			NeedsShaping:    cmp.Or(raw.Labels.NeedsShaping, defaultString("labels.needs_shaping")),
			NeedsDecision:   cmp.Or(raw.Labels.NeedsDecision, defaultString("labels.needs_decision")),
			NeedsHands:      cmp.Or(raw.Labels.NeedsHands, defaultString("labels.needs_hands")),
			NeedsTesting:    cmp.Or(raw.Labels.NeedsTesting, defaultString("labels.needs_testing")),
			LocationPrefix:  raw.Labels.LocationPrefix,
			AreaPrefix:      raw.Labels.AreaPrefix,
		},
	}
	// [controller.triage] alone makes an implicit [controller] table, of type "" and not "Hash".
	if c := raw.Controller; c != nil && len(c.Locations) > 0 && md.Type("controller") != "Hash" {
		return Repo{}, errors.New("controller.locations: [controller] table required")
	}
	if c := raw.Controller; c != nil && md.Type("controller") == "Hash" {
		specReview, planReview := true, true
		if c.SpecReview == nil || c.PlanReview == nil {
			installed, _, err := levels.ReadInstalled(dir)
			if err != nil {
				r.ControllerErr = fmt.Errorf("%s: %w", filepath.Join(dir, levels.InstalledPath), err)
			} else {
				specReview = installed.Stand("skill:review-spec") != levels.StandOff
				planReview = installed.Stand("skill:review-plan") != levels.StandOff
			}
		}
		if c.SpecReview != nil {
			specReview = *c.SpecReview
		}
		if c.PlanReview != nil {
			planReview = *c.PlanReview
		}
		r.Controller = &Controller{
			Profile:      c.Profile,
			SpecReview:   specReview,
			PlanReview:   planReview,
			AtRoundLimit: cmp.Or(c.AtRoundLimit, "doorgaan"),
			Locations:    c.Locations,
		}
		for _, value := range slices.Sorted(maps.Keys(c.Locations)) {
			if c.Locations[value] == "" {
				return Repo{}, fmt.Errorf("controller.locations.%s: empty profile name", value)
			}
		}
		if r.Controller.Profile == "" {
			r.Controller.Profile = "software"
		}
	}
	if t := raw.Controller; t != nil && t.Triage != nil {
		r.Triage, r.TriageErr = triageFrom(t.Triage, md)
	}
	r.Report, r.ReportErr = Report{Sink: "stdout"}, nil
	if md.IsDefined("report") {
		r.Report, r.ReportErr = reportFrom(raw.Report, md)
	}
	schedules, err := schedulesFrom(raw.Schedule, md, r.Controller)
	if err != nil {
		return Repo{}, fmt.Errorf("%s: %w", path, err)
	}
	r.Schedules = schedules
	return r, nil
}

// Loaded is the result of loading one path in LoadAll.
type Loaded struct {
	Path string
	Repo Repo
	Err  error
}

// LoadAll loads every path, in order. A path whose Load fails carries that error and an empty
// Repo. Two paths that load with the same slug both carry an error naming the other path; their
// Repo stays filled so the caller sees the slug.
func LoadAll(paths []string) []Loaded {
	out := make([]Loaded, len(paths))
	for i, p := range paths {
		r, err := Load(p)
		out[i] = Loaded{Path: p, Repo: r, Err: err}
	}
	loaded := make([]bool, len(out))
	for i := range out {
		loaded[i] = out[i].Err == nil
	}
	for i := range out {
		if !loaded[i] {
			continue
		}
		for j := range out {
			if j != i && loaded[j] && out[j].Repo.Slug == out[i].Repo.Slug {
				out[i].Err = fmt.Errorf("%s: [forge] repo %s is also the repo of %s", out[i].Path, out[i].Repo.Slug, out[j].Path)
				break
			}
		}
	}
	return out
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
	if r.ControllerErr != nil {
		errs = append(errs, r.ControllerErr)
	}
	if !r.UsageGuardController {
		add("usage_guard.controller is not true")
	}
	b := r.Board
	if b == nil {
		add("missing [forge.board]")
	} else {
		// A GitLab board has no project or field IDs: its status is the built-in one.
		if r.Kind != "gitlab" {
			if b.ProjectNumber <= 0 {
				add("forge.board.project_number must be positive")
			}
			if b.ProjectID == "" {
				add("forge.board.project_id is empty")
			}
			if b.StatusFieldID == "" {
				add("forge.board.status_field_id is empty")
			}
		}
		for _, name := range []string{"Controller"} {
			if b.StatusOptions[name] == "" {
				add("missing forge.board.status_options.%s", name)
			}
		}
		if r.Kind != "gitlab" && b.PriorityFieldID == "" {
			add("forge.board.priority_field_id is empty")
		}
		// GitLab reads a status or label back to its name: two names on one would flip at random.
		if r.Kind == "gitlab" {
			if a, c, dup := config.SharedOption(b.StatusOptions, true); dup {
				add("forge.board.status_options.%s and forge.board.status_options.%s map to the same GitLab status", a, c)
			}
			if a, c, dup := config.SharedOption(b.PriorityOptions, false); dup {
				add("forge.board.priority_options.%s and forge.board.priority_options.%s map to the same label", a, c)
			}
		}
	}
	return errors.Join(errs...)
}

// reportFrom decodes [report]. Every key decodes as any so a wrong type becomes an error naming
// the key instead of failing Load. On error the Report keeps the values that were valid.
func reportFrom(prim toml.Primitive, md toml.MetaData) (Report, error) {
	var in struct {
		Enabled any `toml:"enabled"`
		Sink    any `toml:"sink"`
		Dir     any `toml:"dir"`
	}
	out := Report{Sink: "stdout"}
	if err := md.PrimitiveDecode(prim, &in); err != nil {
		return out, fmt.Errorf("report: %w", err)
	}
	var errs []error
	switch v := in.Enabled.(type) {
	case nil:
	case bool:
		out.Enabled = v
	default:
		errs = append(errs, errors.New("report.enabled must be true or false"))
	}
	switch v := in.Sink.(type) {
	case nil:
	case string:
		if v != "stdout" && v != "file" {
			errs = append(errs, fmt.Errorf("report.sink %q is not stdout or file", v))
		} else {
			out.Sink = v
		}
	default:
		errs = append(errs, errors.New("report.sink must be a string"))
	}
	switch v := in.Dir.(type) {
	case nil:
	case string:
		clean := filepath.Clean(v)
		if v == "" || filepath.IsAbs(v) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			errs = append(errs, fmt.Errorf("report.dir %q must be a relative path inside the repo", v))
		} else {
			out.Dir = v
		}
	default:
		errs = append(errs, errors.New("report.dir must be a string"))
	}
	if out.Sink == "file" && out.Dir == "" && !hasErrAbout(errs, "report.dir") {
		errs = append(errs, errors.New("report.dir is required with report.sink = file"))
	}
	return out, errors.Join(errs...)
}

func hasErrAbout(errs []error, key string) bool {
	return slices.ContainsFunc(errs, func(e error) bool { return strings.Contains(e.Error(), key) })
}
