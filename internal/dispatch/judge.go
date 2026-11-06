package dispatch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Verdict is the judgement on a handoff command.
type Verdict int

const (
	VerdictStart Verdict = iota
	VerdictEnd
	VerdictBrainstorm
	VerdictInteractive
	VerdictRejected
	VerdictSkillMissing
)

const (
	cmdClose       = "/lighthouse-close-issue"
	cmdBrainstorm  = "/superpowers:brainstorming"
	cmdInteractive = "/superpowers:executing-plans"
	laneNative     = "native"
	maxSlug        = 40
)

// allowlist is the set of commands a run may start, after the /paia- rewrite.
var allowlist = map[string]bool{
	"/lighthouse-start-issue":                  true,
	"/lighthouse-spec-adversarial-review":      true,
	"/lighthouse-plan-adversarial-review":      true,
	"/superpowers:writing-plans":               true,
	"/superpowers:subagent-driven-development": true,
	"/lighthouse-fix-pr":                       true,
	"/lighthouse-write-pr":                     true,
}

var (
	pathArg   = regexp.MustCompile(`^/[A-Za-z0-9._/\-]+\.md$`)
	numberArg = regexp.MustCompile(`^[0-9]+$`)
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
)

// Judgement is the verdict with the command it applies to. Command has /paia- rewritten to
// /lighthouse-; Arg carries the reason argument of a Rejected or SkillMissing verdict.
type Judgement struct {
	Verdict Verdict
	Command string
	Native  bool
	Arg     string
}

// rewrite turns a /paia-<name> command into /lighthouse-<name>.
func rewrite(command string) string {
	if rest, ok := strings.CutPrefix(command, "/paia-"); ok {
		return "/lighthouse-" + rest
	}
	return command
}

// Judge decides what the dispatcher does with the next command of a handoff.
func Judge(next string, issue int, lane string, installed map[string]bool) Judgement {
	fields := strings.Fields(next)
	if len(fields) == 0 {
		return Judgement{Verdict: VerdictRejected, Arg: "(empty)"}
	}
	command, args := rewrite(fields[0]), fields[1:]
	full := strings.Join(append([]string{command}, args...), " ")
	j := Judgement{Command: full}
	switch command {
	case cmdClose:
		j.Verdict = VerdictEnd
		return j
	case cmdBrainstorm:
		j.Verdict = VerdictBrainstorm
		return j
	}
	interactive := command == cmdInteractive
	native := interactive && lane == laneNative
	if interactive && !native {
		j.Verdict = VerdictInteractive
		return j
	}
	reject := Judgement{Verdict: VerdictRejected, Command: full, Arg: next}
	if lane != "" && !native {
		return reject
	}
	if !allowlist[command] && !native {
		return reject
	}
	if name, ok := strings.CutPrefix(command, "/lighthouse-"); ok && !installed["lighthouse-"+name] {
		return Judgement{Verdict: VerdictSkillMissing, Command: full, Arg: "lighthouse-" + name}
	}
	if !argFits(command, args, issue) {
		return reject
	}
	j.Verdict, j.Native = VerdictStart, native
	return j
}

func argFits(command string, args []string, issue int) bool {
	if len(args) != 1 {
		return false
	}
	switch command {
	case "/lighthouse-start-issue", "/lighthouse-write-pr":
		return args[0] == strconv.Itoa(issue)
	case "/lighthouse-fix-pr":
		return numberArg.MatchString(args[0])
	default:
		return pathArg.MatchString(args[0])
	}
}

// ReviewSkip jumps over a review the config turned off: spec review to writing-plans, plan
// review to subagent-driven-development, with the same argument. skipKey is "" when nothing
// is skipped, else the heading key of the note for the Started comment.
func ReviewSkip(next string, c repoconfig.Controller) (command, skipKey string) {
	fields := strings.Fields(next)
	if len(fields) == 0 {
		return next, ""
	}
	rest := strings.Join(fields[1:], " ")
	switch rewrite(fields[0]) {
	case "/lighthouse-spec-adversarial-review":
		if !c.SpecReview {
			return strings.TrimSpace("/superpowers:writing-plans " + rest), "skip_spec_review"
		}
	case "/lighthouse-plan-adversarial-review":
		if !c.PlanReview {
			return strings.TrimSpace("/superpowers:subagent-driven-development " + rest), "skip_plan_review"
		}
	}
	return next, ""
}

// InNight reports whether now falls in the window "HH:MM-HH:MM", read in now's zone. A window
// across midnight wraps; an unreadable or empty window is false.
func InNight(night string, now time.Time) bool {
	from, to, ok := strings.Cut(night, "-")
	if !ok {
		return false
	}
	start, err1 := time.Parse("15:04", from)
	end, err2 := time.Parse("15:04", to)
	if err1 != nil || err2 != nil {
		return false
	}
	minutes := func(t time.Time) int { return t.Hour()*60 + t.Minute() }
	s, e, n := minutes(start), minutes(end), minutes(now)
	switch {
	case s == e:
		return false
	case s < e:
		return n >= s && n < e
	default:
		return n >= s || n < e
	}
}

// Executing reports whether command executes work, so the stacking rule applies. The spec
// and plan phases do not.
func Executing(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return true
	}
	switch rewrite(fields[0]) {
	case "/lighthouse-spec-adversarial-review", "/lighthouse-plan-adversarial-review", "/superpowers:writing-plans":
		return false
	}
	return true
}

// Slug makes a branch-name fragment of a title: lower case, every run outside [a-z0-9] one
// "-", trimmed, at most 40 bytes, cut back to a "-" when it falls inside a word.
func Slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > maxSlug {
		if s[maxSlug] != '-' {
			if i := strings.LastIndex(s[:maxSlug], "-"); i >= 0 {
				s = s[:i]
			} else {
				s = s[:maxSlug]
			}
		} else {
			s = s[:maxSlug]
		}
		s = strings.Trim(s, "-")
	}
	return s
}

// Installed lists the skills in <dir>/.claude/skills that have a SKILL.md. A missing skills
// directory is an empty set.
func Installed(dir string) (map[string]bool, error) {
	root := filepath.Join(dir, ".claude", "skills")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), "SKILL.md")); err == nil {
			out[e.Name()] = true
		}
	}
	return out, nil
}
