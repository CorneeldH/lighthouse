package triage

import (
	"bytes"
	_ "embed" // Schema
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Model is the model of the judgment (spec: the builder section).
const Model = "opus"

// JudgmentTimeout bounds one judgment; a timeout is an invalid judgment (spec: the builder section).
const JudgmentTimeout = 300 * time.Second

// ClosedWindow is how far back closed milestone issues are handed to the judgment (spec: the
// facts Go passes as data).
const ClosedWindow = 30 * 24 * time.Hour

// Schema is the JSON schema of the judgment, passed to claude --json-schema.
//
//go:embed schema.json
var Schema string

// Question is one design question of gate 4c.
type Question struct {
	Text        string
	Options     []string
	Recommended int // index into Options
}

// Judgment is a validated judgment.
type Judgment struct {
	RouteLine          string
	Gate               int    // 0, 1, 2 or 4
	Sub                string // "a", "b", "c"; "" unless Gate is 4
	Redundancy         string // "none", "duplicate" or "done"
	RedundancyIssue    int
	RedundancyEvidence string
	Headless           bool
	HeadlessReason     string
	Questions          []Question // only at 4c
}

// Skill is one entry of the skill catalog.
type Skill struct{ Name, Description string }

// Context is what the judgment needs besides the facts of one issue.
type Context struct {
	Language string // [project] language
	Labels   repoconfig.Labels
	Catalog  []Skill
	Gate0    *Gate0 // nil: Go found no block
}

// Catalog reads name and description from the frontmatter of repoDir/.claude/skills/*/SKILL.md,
// then the superpowers:* skills of the installed plugin. warning is non-empty when the plugin
// is missing; the result never fails on one unreadable SKILL.md (it is left out).
func Catalog(repoDir, pluginsFile string) (skills []Skill, warning string, err error) {
	repoPaths, err := filepath.Glob(filepath.Join(repoDir, ".claude", "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, "", err
	}
	for _, p := range repoPaths {
		if s, ok := readSkill(p, ""); ok {
			skills = append(skills, s)
		}
	}
	install, warning := pluginInstall(repoDir, pluginsFile)
	if install == "" {
		return skills, warning, nil
	}
	pluginPaths, err := filepath.Glob(filepath.Join(install, "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, "", err
	}
	for _, p := range pluginPaths {
		if s, ok := readSkill(p, "superpowers:"); ok {
			skills = append(skills, s)
		}
	}
	return skills, "", nil
}

// pluginInstall finds the install path of the superpowers plugin: the project entry for repoDir,
// else the user entry. install is "" and warning set when there is none.
func pluginInstall(repoDir, pluginsFile string) (install, warning string) {
	data, err := os.ReadFile(pluginsFile)
	if err != nil {
		return "", "superpowers plugin not found: " + err.Error()
	}
	var file struct {
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			ProjectPath string `json:"projectPath"`
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return "", "superpowers plugin not found: " + pluginsFile + " is not readable: " + err.Error()
	}
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for k := range file.Plugins {
			if !yield(k) {
				return
			}
		}
	}) {
		if !strings.HasPrefix(key, "superpowers@") {
			continue
		}
		entries := file.Plugins[key]
		for _, e := range entries {
			if e.ProjectPath != "" && e.ProjectPath == repoDir && e.InstallPath != "" {
				return e.InstallPath, ""
			}
		}
		for _, e := range entries {
			if e.Scope == "user" && e.InstallPath != "" {
				return e.InstallPath, ""
			}
		}
	}
	return "", "superpowers plugin not found in " + pluginsFile
}

// readSkill reads name and description from the frontmatter between the first two --- lines.
// A skill without a name is left out.
func readSkill(path, prefix string) (Skill, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Skill{}, false
	}
	var s Skill
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if v, ok := strings.CutPrefix(line, "name:"); ok {
			s.Name = unquote(v)
		} else if v, ok := strings.CutPrefix(line, "description:"); ok {
			s.Description = unquote(v)
		}
	}
	if s.Name == "" {
		return Skill{}, false
	}
	s.Name = prefix + s.Name
	return s, true
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if u, err := strconv.Unquote(v); err == nil {
			return u
		}
		return v[1 : len(v)-1]
	}
	return v
}

var errInvalid = errors.New("invalid judgment")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errInvalid, fmt.Sprintf(format, a...))
}

type rawQuestion struct {
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
}

type rawJudgment struct {
	RouteLine          string        `json:"route_line"`
	Gate               int           `json:"gate"`
	Sub                string        `json:"sub"`
	Redundancy         string        `json:"redundancy"`
	RedundancyIssue    int           `json:"redundancy_issue"`
	RedundancyEvidence string        `json:"redundancy_evidence"`
	Headless           bool          `json:"headless"`
	HeadlessReason     string        `json:"headless_reason"`
	DesignQuestions    []rawQuestion `json:"design_questions"`
}

// Validate reads the claude --output-format json envelope and checks it against the whole contract.
func Validate(envelope []byte, f Facts, c Context) (Judgment, error) {
	var env struct {
		IsError          bool            `json:"is_error"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(envelope, &env); err != nil {
		return Judgment{}, invalid("envelope is not JSON: %v", err)
	}
	if env.IsError {
		return Judgment{}, invalid("claude reported an error")
	}
	if len(env.StructuredOutput) == 0 || string(env.StructuredOutput) == "null" {
		return Judgment{}, invalid("no structured_output")
	}
	var raw rawJudgment
	dec := json.NewDecoder(bytes.NewReader(env.StructuredOutput))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Judgment{}, invalid("structured_output does not fit the schema: %v", err)
	}

	j := Judgment{
		Redundancy: raw.Redundancy, RedundancyIssue: raw.RedundancyIssue,
		RedundancyEvidence: strings.TrimSpace(raw.RedundancyEvidence),
		Headless:           raw.Headless, HeadlessReason: strings.TrimSpace(raw.HeadlessReason),
	}
	if c.Gate0 != nil {
		j.RouteLine = gate0Line(f.Number, *c.Gate0, c.Language)
	} else if err := checkRoute(&j, raw, f, c); err != nil {
		return Judgment{}, err
	}
	if err := checkRedundancy(j, f.Number); err != nil {
		return Judgment{}, err
	}
	if !j.Headless && j.HeadlessReason == "" {
		return Judgment{}, invalid("headless false without headless_reason")
	}
	return j, nil
}

func gate0Line(number int, g Gate0, language string) string {
	var parts []string
	for _, n := range g.Blockers {
		parts = append(parts, "#"+strconv.Itoa(n))
	}
	if g.Label != "" {
		parts = append(parts, g.Label)
	}
	return fmt.Sprintf("#%d %s 0: stop — %s", number, protocol.Heading("gate_line", language), strings.Join(parts, ", "))
}

func checkRoute(j *Judgment, raw rawJudgment, f Facts, c Context) error {
	re := regexp.MustCompile(`^#([0-9]+) ` + regexp.QuoteMeta(protocol.Heading("gate_line", c.Language)) +
		` ([0-4])([abc])?: (\S.*?) — (\S.*)$`)
	m := re.FindStringSubmatch(raw.RouteLine)
	if m == nil {
		return invalid("route_line %q does not match #<nr> %s <n>[abc]: <route> — <reason>", raw.RouteLine, protocol.Heading("gate_line", c.Language))
	}
	if n, _ := strconv.Atoi(m[1]); n != f.Number {
		return invalid("route_line is for #%s, not #%d", m[1], f.Number)
	}
	gate, _ := strconv.Atoi(m[2])
	sub := m[3]
	switch {
	case gate == 3:
		return invalid("gate 3 always falls through to 4 and is no outcome")
	case gate == 0:
		return invalid("gate 0 where Go found no block")
	case gate == 4 && sub == "":
		return invalid("gate 4 without sub-letter")
	case gate != 4 && sub != "":
		return invalid("sub-letter %s does not belong to gate %d", sub, gate)
	case raw.Gate != gate:
		return invalid("field gate %d does not match route_line gate %d", raw.Gate, gate)
	case raw.Sub != sub:
		return invalid("field sub %q does not match route_line sub %q", raw.Sub, sub)
	}
	for _, l := range []string{c.Labels.NeedsShaping, c.Labels.NeedsDecision} {
		if l != "" && slices.Contains(f.Labels, l) && sub != "c" {
			return invalid("the issue carries %s, so the route must be 4c, not %d%s", l, gate, sub)
		}
	}
	qs, err := checkQuestions(raw.DesignQuestions, sub == "c")
	if err != nil {
		return err
	}
	j.RouteLine, j.Gate, j.Sub, j.Questions = raw.RouteLine, gate, sub, qs
	return nil
}

func checkQuestions(rs []rawQuestion, want bool) ([]Question, error) {
	if !want {
		if len(rs) > 0 {
			return nil, invalid("design_questions only belong to 4c")
		}
		return nil, nil
	}
	if len(rs) < 1 || len(rs) > protocol.MaxQuestions {
		return nil, invalid("4c needs 1 to %d design_questions, got %d", protocol.MaxQuestions, len(rs))
	}
	out := make([]Question, len(rs))
	for i, q := range rs {
		if strings.TrimSpace(q.Question) == "" {
			return nil, invalid("design_questions[%d] has no question text", i)
		}
		if len(q.Options) < 2 || len(q.Options) > len(protocol.Letters) {
			return nil, invalid("design_questions[%d] needs 2 to %d options, got %d", i, len(protocol.Letters), len(q.Options))
		}
		if q.Recommended < 0 || q.Recommended >= len(q.Options) {
			return nil, invalid("design_questions[%d] recommended %d is outside the options", i, q.Recommended)
		}
		out[i] = Question{Text: q.Question, Options: q.Options, Recommended: q.Recommended}
	}
	return out, nil
}

// checkRedundancy checks the redundancy fields of a judgment of issue number.
func checkRedundancy(j Judgment, number int) error {
	switch j.Redundancy {
	case "none":
	case "duplicate":
		if j.RedundancyIssue <= 0 {
			return invalid("redundancy duplicate without redundancy_issue")
		}
		if j.RedundancyIssue == number {
			return invalid("redundancy duplicate of #%d itself", number)
		}
	case "done":
		if j.RedundancyEvidence == "" {
			return invalid("redundancy done without redundancy_evidence")
		}
	default:
		return invalid("redundancy %q is not none, duplicate or done", j.Redundancy)
	}
	return nil
}
