package kpi

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Options selects what a measure command prints. Dir == "" is the default transcript
// directory; Since == nil is no lower bound.
type Options struct {
	Metric string
	Dir    string
	Since  *string
}

// StartIssueMetrics are the rows of the start-issue table, in output order.
var StartIssueMetrics = []string{"leadin", "worktree-first", "mistrigger", "paths", "gate", "scout"}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// defaultTranscriptDir is ~/.claude/projects/<root>, where <root> is the repo root with every
// character outside [A-Za-z0-9] replaced by "-".
func defaultTranscriptDir() (string, error) {
	root, err := config.RepoRoot()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects", nonAlnum.ReplaceAllString(abs, "-")), nil
}

// RejectMetric reports an unknown metric (or an extra argument) and returns exit code 2.
func RejectMetric(name string, choices []string, errOut io.Writer) int {
	_, _ = fmt.Fprintf(errOut, "measure: unknown metric %q (choose from %s)\n", name, strings.Join(choices, ", "))
	return 2
}

// resolveDir returns the transcript directory, or an exit code 2 with the message on errOut.
func resolveDir(opts Options, errOut io.Writer) (string, int) {
	dir := opts.Dir
	if dir == "" {
		var err error
		if dir, err = defaultTranscriptDir(); err != nil {
			_, _ = fmt.Fprintf(errOut, "measure: %v\n", err)
			return "", 2
		}
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		_, _ = fmt.Fprintf(errOut, "transcript directory does not exist: %s\n", pyPath(dir))
		return "", 2
	}
	return dir, 0
}

// RunStartIssue prints the start-issue KPI table (or one row) and returns the exit code.
func RunStartIssue(opts Options, out, errOut io.Writer) int {
	if opts.Metric != "" && !slices.Contains(StartIssueMetrics, opts.Metric) {
		return RejectMetric(opts.Metric, StartIssueMetrics, errOut)
	}
	dir, rc := resolveDir(opts, errOut)
	if rc != 0 {
		return rc
	}
	m := &measurer{dir: dir, since: opts.Since}
	rows := map[string]func() (object, error){
		"leadin": m.leadin, "worktree-first": m.worktreeFirst, "mistrigger": m.mistrigger,
		"paths": m.paths, "gate": m.gate, "scout": m.scout,
	}
	selected := StartIssueMetrics
	if opts.Metric != "" {
		selected = []string{opts.Metric}
	}
	table := make(object, 0, len(selected))
	for _, name := range selected {
		row, err := rows[name]()
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "measure: %v\n", err)
			return 2
		}
		table = append(table, field{name, row})
	}
	_, _ = fmt.Fprintln(out, encode(table))
	return 0
}
