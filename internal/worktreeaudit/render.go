package worktreeaudit

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/pytext"
	"github.com/CorneeldH/lighthouse/internal/worktreeroot"
)

// View renders rows: it judges them with Policy, measuring lock pids with Alive.
type View struct {
	Policy Policy
	Alive  Alive
	Root   string
}

const (
	tableHeader = "| worktree | branch | PR state | unpushed commits | dirty tree | last session | live? | verdict |"
	tableRule   = "|---|---|---|---|---|---|---|---|"
)

type judged struct {
	row     Row
	verdict Verdict
}

func (v View) judge(rows []Row) []judged {
	out := make([]judged, len(rows))
	for i, r := range rows {
		out[i] = judged{r, v.Policy.Judge(r, v.Alive)}
	}
	return out
}

// fmtTime is an epoch in local time as `MM-DD HH:MM`, or `—`.
func fmtTime(stamp *float64) string {
	if stamp == nil {
		return "—"
	}
	sec := math.Floor(*stamp)
	return time.Unix(int64(sec), int64((*stamp-sec)*1e9)).In(time.Local).Format("01-02 15:04")
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	return pyName(*s)
}

func rowLine(r Row, v Verdict) string {
	location := r.Worktree
	if location == nil || *location == "" {
		location = r.OrphanDir
	}
	pr := "—"
	if r.PRNumber != nil && *r.PRNumber != 0 {
		state := "None"
		if r.PRState != nil {
			state = *r.PRState
		}
		pr = "#" + strconv.Itoa(*r.PRNumber) + " " + state
	}
	unpushed := "?"
	if r.Unpushed != nil {
		unpushed = strconv.Itoa(*r.Unpushed)
	}
	dirty := "?"
	if r.Dirty != nil {
		dirty = map[bool]string{true: "yes", false: "no"}[*r.Dirty]
	}
	live := "—"
	if r.Live {
		live = "LIVE"
	}
	return fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |",
		orDash(location), r.Name, pr, unpushed, dirty, fmtTime(r.LastSession), live, v.Render())
}

func nonEmpty(s *string) bool { return s != nil && *s != "" }

// safeLists is the paste lists: safe and not locked, since git refuses a locked row.
func safeLists(pairs []judged) (worktrees, local, remote []string) {
	for _, p := range pairs {
		if !p.verdict.Safe() || p.verdict.locked() {
			continue
		}
		if nonEmpty(p.row.Worktree) {
			worktrees = append(worktrees, *p.row.Worktree)
		}
		if p.row.Local {
			local = append(local, p.row.Name)
		}
		if p.row.Remote {
			remote = append(remote, p.row.Name)
		}
	}
	return worktrees, local, remote
}

// commandBlocks in paste order: a branch checked out in a worktree cannot be deleted, so
// `worktree remove` comes first. Nothing here unlocks or kills.
func commandBlocks(worktrees, local, remote []string) []string {
	lines := []string{"Commands — read the table first; this script runs nothing.", ""}
	if len(worktrees) > 0 {
		lines = append(lines, "```bash")
		for _, path := range worktrees {
			lines = append(lines, `git worktree remove "`+path+`"`)
		}
		lines = append(lines, "```")
	}
	if len(local) > 0 {
		lines = append(lines, "```bash", "git branch -D "+strings.Join(local, " "), "```")
	}
	if len(remote) > 0 {
		lines = append(lines, "```bash", "git push origin --delete "+strings.Join(remote, " "), "```")
	}
	if len(worktrees)+len(local)+len(remote) == 0 {
		lines = append(lines, "Nothing with verdict ✅ — no commands.")
	}
	return lines
}

func pyOpt(s *string) string {
	if s == nil {
		return "None"
	}
	return *s
}

func staleLockedSection(pairs []judged) []string {
	var lines []string
	for _, p := range pairs {
		if p.verdict.Safe() && p.verdict.has("stale lock") {
			lines = append(lines, fmt.Sprintf("  %s   (lock-pid %s, no longer running)", pyOpt(p.row.Worktree), pyRepr(p.row.LockPID)))
		}
	}
	if lines == nil {
		return nil
	}
	return append([]string{"", "Locked with a dead pid — release the lock by hand first, then run again:"}, lines...)
}

func serverLockedSection(pairs []judged) []string {
	var lines []string
	for _, p := range pairs {
		if p.verdict.Safe() && p.verdict.has("server-lock") {
			pid := "unknown"
			if p.row.LockPID != nil {
				pid = strconv.Itoa(*p.row.LockPID)
			}
			lines = append(lines, fmt.Sprintf("  %s   (server-pid %s, session ended)", pyOpt(p.row.Worktree), pid))
		}
	}
	if lines == nil {
		return nil
	}
	return append([]string{"", "Locked by the session server — session ended; release the lock by hand if you want it gone " +
		"(or wait for the next server start), then run again:"}, lines...)
}

// portBlocksSection shows the registrations of `.port-registry`; which process holds a port is not
// looked up.
func portBlocksSection(rows []Row) []string {
	var ported []Row
	for _, r := range rows {
		if r.PortBlock != nil {
			ported = append(ported, r)
		}
	}
	if len(ported) == 0 {
		return nil
	}
	sort.SliceStable(ported, func(i, j int) bool { return ported[i].Name < ported[j].Name })
	lines := []string{"", "Port blocks (registered in worktrees/.port-registry, read-only):"}
	for _, r := range ported {
		where := "no worktree"
		switch {
		case nonEmpty(r.Worktree):
			where = *r.Worktree
		case nonEmpty(r.OrphanDir):
			where = *r.OrphanDir
		}
		lines = append(lines, fmt.Sprintf("  %s  port %s  %s", r.Name, *r.PortBlock, where))
	}
	return append(lines, "The process that actually holds the port is not looked up here "+
		"— an empty list above does not mean that nothing runs.")
}

func joinLines(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// Cleanup is the default view: the table sorted by rank and name, then the paste commands and
// the sections of what they leave out.
func (v View) Cleanup(rows []Row) string {
	pairs := v.judge(rows)
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].verdict.Rank != pairs[j].verdict.Rank {
			return pairs[i].verdict.Rank < pairs[j].verdict.Rank
		}
		return pairs[i].row.Name < pairs[j].row.Name
	})
	lines := []string{tableHeader, tableRule}
	for _, p := range pairs {
		lines = append(lines, rowLine(p.row, p.verdict))
	}
	lines = append(lines, "")
	lines = append(lines, commandBlocks(safeLists(pairs))...)
	lines = append(lines, staleLockedSection(pairs)...)
	lines = append(lines, serverLockedSection(pairs)...)
	lines = append(lines, portBlocksSection(rows)...)
	return joinLines(lines)
}

// Sessions is a `claude --resume` line per row with a session: live first, then newest.
func (v View) Sessions(rows []Row) string {
	var pairs []judged
	for _, p := range v.judge(rows) {
		if nonEmpty(p.row.SessionID) {
			pairs = append(pairs, p)
		}
	}
	last := func(r Row) float64 {
		if r.LastSession == nil {
			return 0
		}
		return *r.LastSession
	}
	// sort(reverse=True) keeps equal keys in input order.
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i].row, pairs[j].row
		if a.Live != b.Live {
			return a.Live
		}
		return last(a) > last(b)
	})
	lines := []string{"| last session | live? | worktree | resume | verdict |", "|---|---|---|---|---|"}
	for _, p := range pairs {
		live := "dead"
		if p.row.Live {
			live = "LIVE"
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | `claude --resume %s` | %s |",
			fmtTime(p.row.LastSession), live, orDash(p.row.Worktree), *p.row.SessionID, p.verdict.Render()))
	}
	if len(pairs) == 0 {
		lines = append(lines, "| — | — | — | no sessions found | — |")
	}
	return joinLines(lines)
}

// Origin is Cleanup over the rows that are on origin.
func (v View) Origin(rows []Row) string {
	var remote []Row
	for _, r := range rows {
		if r.Remote {
			remote = append(remote, r)
		}
	}
	return v.Cleanup(remote)
}

// KPI is one line: worktrees under the `[worktree] root`, safe rows, remote branches, warnings.
func (v View) KPI(rows []Row) string {
	var worktrees, safe, remote, warn int
	for _, p := range v.judge(rows) {
		if nonEmpty(p.row.Worktree) && worktreeroot.Contains(v.Root, *p.row.Worktree) {
			worktrees++
		}
		if p.verdict.Safe() {
			safe++
		}
		if p.row.Remote {
			remote++
		}
		if p.verdict.Rank == 4 || p.verdict.Rank == 7 {
			warn++
		}
	}
	return fmt.Sprintf("worktrees=%d safe=%d remote_branches=%d warn=%d\n", worktrees, safe, remote, warn)
}

// slashPath is str(PurePath(p)) on POSIX: no empty or `.` components, no trailing slash.
func slashPath(p string) string {
	p = filepath.ToSlash(p)
	var parts []string
	for _, part := range strings.Split(p, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	joined := strings.Join(parts, "/")
	switch {
	case strings.HasPrefix(p, "/"):
		return "/" + joined
	case joined == "":
		return "."
	}
	return joined
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// inside reports wt == cwd or wt among the parents of cwd.
func inside(wt, cwd string) bool {
	w, c := slashPath(wt), slashPath(cwd)
	for {
		if samePath(w, c) {
			return true
		}
		i := strings.LastIndex(c, "/")
		var parent string
		switch {
		case i > 0:
			parent = c[:i]
		case i == 0 && c != "/":
			parent = "/"
		case i < 0 && c != ".":
			parent = "."
		default:
			return false
		}
		c = parent
	}
}

// Only is the view of close-issue: one row, then `cleanup: yes` with at most two commands (never
// `push --delete`), or `cleanup: no — <reason>`. Cleanable is safe, not locked, and the own cwd is
// not in the worktree.
func (v View) Only(rows []Row, branch, cwd string) (string, bool) {
	var match []Row
	for _, r := range rows {
		if r.Name == branch {
			match = append(match, r)
		}
	}
	if len(match) == 0 {
		return "cleanup: no — branch " + branch + " not found\n", false
	}
	if len(match) > 1 {
		// A detached worktree or orphan dir carries its directory name, which can be the branch's.
		lines := []string{tableHeader, tableRule}
		for _, p := range v.judge(match) {
			lines = append(lines, rowLine(p.row, p.verdict))
		}
		lines = append(lines, "", fmt.Sprintf("cleanup: no — %d rows named %s (detached worktree or orphan dir)", len(match), branch))
		return joinLines(lines), false
	}
	target := match[0]
	verdict := v.Policy.Judge(target, v.Alive)
	lines := []string{tableHeader, tableRule, rowLine(target, verdict), ""}
	hasWT := nonEmpty(target.Worktree)
	reason := ""
	switch {
	case !verdict.Safe():
		reason = verdict.Render()
	case verdict.locked():
		reason = verdict.Render() + " (locked)"
	case hasWT && inside(*target.Worktree, cwd):
		reason = "own cwd is in " + slashPath(*target.Worktree)
	case !hasWT && !target.Local:
		reason = "nothing local to clean up"
	}
	if reason != "" {
		return joinLines(append(lines, "cleanup: no — "+reason)), false
	}
	lines = append(lines, "cleanup: yes")
	if hasWT {
		lines = append(lines, `git worktree remove "`+*target.Worktree+`"`)
	}
	if target.Local {
		lines = append(lines, "git branch -D "+target.Name)
	}
	return joinLines(lines), true
}

// Issue is a `branch: <b>` block of Only per branch of issue nr. A branch without a merged PR is
// never cleanable; headsOK false (the PR lookup failed) is no signal, and no signal is no yes.
func (v View) Issue(rows []Row, nr int, heads []string, headsOK bool, cwd string) string {
	if !headsOK {
		return fmt.Sprintf("cleanup: no — PR lookup for issue #%d failed\n", nr)
	}
	branches := IssueBranches(rows, nr, heads)
	if len(branches) == 0 {
		return fmt.Sprintf("cleanup: no — no branches found for issue #%d\n", nr)
	}
	blocks := make([]string, 0, len(branches))
	for _, branch := range branches {
		text, ok := v.Only(rows, branch, cwd)
		var match []Row
		for _, r := range rows {
			if r.Name == branch {
				match = append(match, r)
			}
		}
		merged := len(match) == 1 && match[0].PRState != nil && *match[0].PRState == "merged"
		if ok && !merged {
			// Python cuts at `cleanup: yes` in str.splitlines and joins with \n.
			lines := pytext.SplitLines(text, false)
			head := lines
			for i, line := range lines {
				if line == "cleanup: yes" {
					head = lines[:i]
					break
				}
			}
			text = joinLines(append(append([]string(nil), head...), "cleanup: no — no merged PR"))
		}
		blocks = append(blocks, "branch: "+branch+"\n"+text)
	}
	return strings.Join(blocks, "\n")
}
