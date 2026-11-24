package history

import (
	"regexp"
	"strings"
)

var (
	issueCmdRe = regexp.MustCompile(`\b(?:gh\s+issue|lighthouse\s+forge\s+issue|glab\s+issue)\b`)
	prCmdRe    = regexp.MustCompile(`\b(?:gh\s+pr|lighthouse\s+forge\s+pr|glab\s+mr)\b`)
	prCreateRe = regexp.MustCompile(`\b(?:gh\s+pr|lighthouse\s+forge\s+pr|glab\s+mr)\s+create\b`)
)

// isTaskFile reports whether a written path looks like a task list kept outside the issue
// tracker: TODO.md, docs/tasks/**, *.sqlite, *.db or memory/**.
func isTaskFile(path string) bool {
	p := strings.ReplaceAll(path, `\`, "/")
	base := p[strings.LastIndex(p, "/")+1:]
	if base == "TODO.md" || strings.HasSuffix(base, ".sqlite") || strings.HasSuffix(base, ".db") {
		return true
	}
	padded := "/" + p
	return strings.Contains(padded, "/docs/tasks/") || strings.Contains(padded, "/memory/")
}

type workTracking struct {
	prNumbers map[int]bool
	creates   map[string]int  // session -> pr create calls
	linked    map[string]bool // sessions with a pr-link
	issues    int
	prCmds    int
	fileTasks int
	hits      Hits
}

func newWorkTracking() *workTracking {
	return &workTracking{prNumbers: map[int]bool{}, creates: map[string]int{}, linked: map[string]bool{}}
}

func (d *workTracking) Add(r Record) {
	sess := r.SessionID
	if sess == "" {
		sess = r.File
	}
	hit := false
	if r.Type == "pr-link" && r.PRNumber > 0 {
		d.prNumbers[r.PRNumber] = true
		d.linked[sess] = true
		hit = true
	}
	for _, c := range r.Commands {
		if issueCmdRe.MatchString(c) {
			d.issues++
			hit = true
		}
		if prCmdRe.MatchString(c) {
			d.prCmds++
			hit = true
		}
		if prCreateRe.MatchString(c) {
			d.creates[sess]++
		}
	}
	for _, p := range r.WrittenPaths {
		if isTaskFile(p) {
			d.fileTasks++
			hit = true
		}
	}
	if hit {
		d.hits.Add(r.File, r.Line, r.Timestamp)
	}
}

// PRs counts unique pr-link numbers plus the pr create calls of sessions without a
// pr-link, because Claude Code writes a pr-link after a successful create.
func (d *workTracking) PRs() int {
	n := len(d.prNumbers)
	for sess, c := range d.creates {
		if !d.linked[sess] {
			n += c
		}
	}
	return n
}

// IssueCommands counts the issue commands (gh issue, forge issue, glab issue).
func (d *workTracking) IssueCommands() int { return d.issues }

func (d *workTracking) Finish(Env) Finding {
	prs := d.PRs()
	f := Finding{
		Key:        "work_tracking",
		Status:     "none",
		Confidence: "high",
		Measured: map[string]any{
			"issue_commands": d.issues,
			"pr_commands":    d.prCmds,
			"prs":            prs,
			"file_tasks":     d.fileTasks,
		},
		Meaning: "Work outside issues does not fit the chain from issue to PR.",
		Proposal: Proposal{Kind: "advice",
			Text: "Move work to GitHub issues and track it there."},
	}
	if d.issues+prs+d.prCmds+d.fileTasks > 0 {
		f.Status = "found"
		f.Sources = d.hits.Top()
	}
	if d.issues+prs > 0 {
		f.Proposal = Proposal{Kind: "knob", Flag: "--labels",
			Text: "Install the issue labels so issues and PRs carry the chain."}
	}
	return f
}
