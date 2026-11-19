// Package unblock is `lighthouse unblock-dependents`: it releases the issues that waited on a
// just-closed issue, on the forge of the repo (GitHub or GitLab).
//
// On GitHub the release lane (`.github/workflows/lighthouse-unblock-dependents.yml`) runs it on
// every `issues: closed`, as the bot from `[ci]`; without a lane close-issue runs it with the
// user's token. For each open issue that the closed issue blocks it counts the blockers that are
// still open. If none are open any more and the issue carries the blocking label of the config,
// that label is removed and the issue gets a comment. The closed issue gets one comment back
// with what was released and what still waits.
//
// Norms, binding for every change to this package:
//
//   - Only `completed` releases. An issue that was closed as `not_planned` or `duplicate` did
//     not do the work; the block stays and a human decides. GitLab has no close reason: there
//     an empty one counts as `completed`.
//   - Only what carries the label, in this repo. A dependent without the blocking label is not
//     read further, not edited and not in the reply; a second run after a successful release
//     therefore posts nothing new. One that still waits is mentioned again. A dependent in
//     another repository is neither read nor edited.
//   - Comments first, then the label. First the reply comment on the closed issue, then per
//     dependent the comment and only then the label. If a comment fails, the label stays and
//     a restart catches it; a label that is already gone is not an error.
//   - Not read is not zero. If reading the blockers of one issue fails, that label stays and
//     the run is red; "could not read" never counts as released.
//
// Output and comments are English; the reply on the closed issue starts with the fixed line
// ReleaseLine, which `close-issue` reads.
package unblock

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// ReleaseLine opens the reply on the closed issue; close-issue reads it.
const ReleaseLine = "Released lane after this close:"

// errorLine is the first non-empty line of err.
func errorLine(err error) string {
	for _, line := range strings.Split(err.Error(), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return err.Error()
}

// readDependents is (message, open dependents); a message means: nothing to do, and why.
func readDependents(ctx context.Context, f forge.Forge, nr int) (string, []forge.Blocker, error) {
	issue, err := f.Issue(ctx, nr)
	if err != nil {
		return "", nil, err
	}
	if issue.State != "closed" {
		return fmt.Sprintf("#%d is open: nothing to release.", nr), nil, nil
	}
	reason := issue.StateReason
	if reason == "" && f.Kind() == "gitlab" {
		reason = "completed" // GitLab has no close reason
	}
	if reason != "completed" {
		return fmt.Sprintf("#%d closed as `%s`: blocks stay.", nr, cmp.Or(reason, "None")), nil, nil
	}
	blocking, err := f.Blocking(ctx, nr)
	if err != nil {
		return "", nil, err
	}
	var dependents []forge.Blocker
	for _, b := range blocking {
		if b.State == "OPEN" {
			dependents = append(dependents, b)
		}
	}
	if len(dependents) == 0 {
		return fmt.Sprintf("#%d blocked no open issues.", nr), nil, nil
	}
	return "", dependents, nil
}

// verdict is what phase 1 decides for one dependent, without writing.
type verdict int

const (
	free    verdict = iota // released: comment and label
	waits                  // still waits: in the reply only
	nothing                // no label, or another repo: in the summary only
)

// judge is the verdict and the summary text for the dependent m of this repo.
func judge(ctx context.Context, f forge.Forge, repo string, m int, label string) (verdict, string, error) {
	issue, err := f.Issue(ctx, m)
	if err != nil {
		return 0, "", err
	}
	if !slices.Contains(issue.Labels, label) {
		return nothing, fmt.Sprintf("does not carry `%s`, nothing to do", label), nil
	}
	blockers, err := f.BlockedBy(ctx, m)
	if err != nil {
		return 0, "", err
	}
	var open []string
	for _, b := range blockers {
		if b.State != "OPEN" {
			continue
		}
		if strings.EqualFold(b.Repo, repo) {
			open = append(open, fmt.Sprintf("#%d", b.Issue))
		} else {
			open = append(open, fmt.Sprintf("%s#%d", b.Repo, b.Issue))
		}
	}
	if len(open) > 0 {
		return waits, "still waits for " + strings.Join(open, ", "), nil
	}
	return free, fmt.Sprintf("released, `%s` comes off", label), nil
}

// judgeAll is phase 1: the exit code, the numbers to release and the lines for the reply
// comment. It appends one summary line per dependent to lines.
func judgeAll(ctx context.Context, f forge.Forge, repo string, dependents []forge.Blocker, lines *[]string, label string) (int, []int, []string) {
	code := 0
	var released []int
	var reply []string
	for _, dep := range dependents {
		if !strings.EqualFold(dep.Repo, repo) {
			*lines = append(*lines, fmt.Sprintf("- %s#%d: in another repository, not released", dep.Repo, dep.Issue))
			continue
		}
		m := dep.Issue
		kind, text, err := judge(ctx, f, repo, m, label)
		if err != nil {
			code = 1
			*lines = append(*lines, fmt.Sprintf("- #%d: **Error:** %s", m, errorLine(err)))
			continue
		}
		*lines = append(*lines, fmt.Sprintf("- #%d: %s", m, text))
		switch kind {
		case free:
			released = append(released, m)
			reply = append(reply, fmt.Sprintf("- #%d is released: `%s` comes off", m, label))
		case waits:
			reply = append(reply, fmt.Sprintf("- #%d %s", m, text))
		}
	}
	return code, released, reply
}

func release(ctx context.Context, f forge.Forge, nr, m int, label string) error {
	text := fmt.Sprintf("Released: the last open blocker #%d is closed; `%s` comes off.", nr, label)
	if _, err := f.CreateComment(ctx, m, text); err != nil {
		return err
	}
	_, err := f.EditIssue(ctx, m, forge.IssueEdit{RemoveLabels: []string{label}})
	return err
}

// header opens the summary of issue nr.
func header(nr int) []string { return []string{fmt.Sprintf("## Release lane for #%d", nr), ""} }

// NotOpened is the summary when the forge of repo did not open: red, nothing read.
func NotOpened(nr int, err error) []string {
	return append(header(nr), "**Error:** released issues not read: "+errorLine(err))
}

// Run releases the dependents of issue nr on f, the forge of repo, and returns the exit code
// and the summary lines.
//
// Three phases: read, the reply comment on nr, only then the dependents. If the reply comment
// fails, nothing has been touched yet and a restart redoes everything.
func Run(ctx context.Context, f forge.Forge, repo string, nr int, label string, dryRun bool) (int, []string) {
	lines := header(nr)
	message, dependents, err := readDependents(ctx, f, nr)
	if err != nil {
		return 1, append(lines, fmt.Sprintf("**Error:** #%d not read: %s", nr, errorLine(err)))
	}
	if message != "" {
		return 0, append(lines, message)
	}
	code, released, reply := judgeAll(ctx, f, repo, dependents, &lines, label)
	if dryRun {
		return code, append(lines, "", "_dry-run: nothing written._")
	}
	if len(reply) == 0 {
		return code, lines
	}
	body := strings.Join(append([]string{ReleaseLine, ""}, reply...), "\n")
	if _, err := f.CreateComment(ctx, nr, body); err != nil {
		lines = append(lines, fmt.Sprintf("**Error:** comment on #%d: %s; nothing touched", nr, errorLine(err)))
		return 1, lines
	}
	for _, m := range released {
		if err := release(ctx, f, nr, m, label); err != nil {
			code = 1
			lines = append(lines, fmt.Sprintf("- #%d: **Error:** %s", m, errorLine(err)))
		}
	}
	return code, lines
}

// Write prints the summary lines to out.
func Write(out io.Writer, lines []string) error {
	_, err := io.WriteString(out, strings.Join(lines, "\n")+"\n")
	return err
}
