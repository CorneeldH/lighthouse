package dispatch

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const cmdSDD = "/superpowers:subagent-driven-development"

// phases maps a command to the phase name the UI shows.
var phases = map[string]string{
	"/lighthouse-start-issue":      "start-issue",
	"/lighthouse-review-spec":      "spec-review",
	"/lighthouse-review-plan":      "plan-review",
	"/superpowers:writing-plans":   "writing-plans",
	cmdSDD:                         "sdd",
	"/superpowers:executing-plans": "executing-plans",
	"/lighthouse-fix-pr":           "fix-pr",
	"/lighthouse-write-pr":         "write-pr",
	"/lighthouse-close-issue":      "close-issue",
	"/superpowers:brainstorming":   "brainstorming",
}

var (
	planTask   = regexp.MustCompile(`(?m)^#{2,4} (?:Task|Taak) (\d+)\b`)
	ledgerDone = regexp.MustCompile(`(?m)^(?:Task|Taak) (\d+)\b[^:\n]*: complete`)
)

// Phase names the phase a run is in, from the first field of its command.
func Phase(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	name := rewrite(fields[0])
	if p, ok := phases[name]; ok {
		return p
	}
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimPrefix(name, "lighthouse-")
	return strings.TrimPrefix(name, "superpowers:")
}

// Progress is where a subagent-driven-development run stands in its plan.
type Progress struct {
	Task, Tasks int
	Step        string // "build", "review" or "final-review"
}

// ReadProgress reads the plan and the sdd ledger of a subagent-driven-development run. It gives
// false for any other command and for anything it cannot read: a run's work directory may be
// gone or half written, and that must never stop the tick.
func ReadProgress(command string) (Progress, bool) {
	fields := strings.Fields(command)
	if len(fields) != 2 || rewrite(fields[0]) != cmdSDD || !pathArg.MatchString(fields[1]) {
		return Progress{}, false
	}
	plan := filepath.Clean(fields[1])
	root, ok := gitRoot(filepath.Dir(plan))
	if !ok {
		return Progress{}, false
	}
	id := plan
	// IsLocal, not a ".." prefix: a directory named "..plans" is inside the root.
	if rel, err := filepath.Rel(root, plan); err == nil && filepath.IsLocal(rel) {
		id = rel
	}
	work, ok := workDir(root, id)
	if !ok {
		return Progress{}, false
	}
	ledger, err := os.ReadFile(filepath.Join(work, "progress.md"))
	if err != nil {
		return Progress{}, false
	}
	planText, err := os.ReadFile(plan)
	if err != nil {
		return Progress{}, false
	}
	tasks := len(distinct(planTask, string(planText)))
	if tasks == 0 {
		return Progress{}, false
	}
	current := 1
	for n := range distinct(ledgerDone, string(ledger)) {
		current = max(current, n+1)
	}
	switch {
	case current > tasks:
		return Progress{Task: tasks, Tasks: tasks, Step: "final-review"}, true
	case fileExists(filepath.Join(work, "task-"+strconv.Itoa(current)+"-report.md")):
		return Progress{Task: current, Tasks: tasks, Step: "review"}, true
	}
	return Progress{Task: current, Tasks: tasks, Step: "build"}, true
}

// gitRoot climbs from dir to the first directory holding a .git entry, file or directory.
func gitRoot(dir string) (string, bool) {
	for {
		if fileExists(filepath.Join(dir, ".git")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// workDir finds the sdd work directory of the plan with this id.
func workDir(root, id string) (string, bool) {
	base := filepath.Join(root, ".superpowers", "sdd")
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		dir := filepath.Join(base, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "plan-path"))
		if err == nil && strings.TrimSpace(string(b)) == id {
			return dir, fileExists(filepath.Join(dir, "progress.md"))
		}
	}
	return "", false
}

// distinct returns the set of numbers captured by re in text.
func distinct(re *regexp.Regexp, text string) map[int]bool {
	out := map[int]bool{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil {
			out[n] = true
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
