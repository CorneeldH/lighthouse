// Package worktreeaudit is `lighthouse worktree-audit`: one join over worktrees, PR state,
// branches and sessions, judged on a ladder whose first hit wins and whose top is safety. It ports
// worktree_audit.py; the fixtures in tests/pack/fixtures/worktree_audit hold both to the same
// output.
//
// The row is a branch, not a worktree: most cleanable things on origin have no worktree. An
// orphan directory under `.claude/worktrees/` and a detached worktree get a row of their own.
//
// It mutates nothing. Git runs only through runGit, which refuses every verb outside a read-only
// allowlist; the commands it prints are for a human to paste.
package worktreeaudit

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/CorneeldH/lighthouse/internal/pytext"
)

// Row is one line of the audit. A nil pointer is "not measured", never "clean". The JSON names
// are the field names of the Python dataclass, which the fixtures use.
type Row struct {
	Name       string  `json:"name"`
	Local      bool    `json:"local"`
	Remote     bool    `json:"remote"`
	Worktree   *string `json:"worktree"`
	Locked     bool    `json:"locked"`
	LockPID    *int    `json:"lock_pid"`
	LockReason *string `json:"lock_reason"` // the text after `locked `; nil for a bare `locked`
	Dirty      *bool   `json:"dirty"`
	Unpushed   *int    `json:"unpushed"`
	PRNumber   *int    `json:"pr_number"`
	PRState    *string `json:"pr_state"` // open, merged or closed
	// Epoch seconds: GitHub gives UTC, git a local offset, so only epoch compares.
	PRMergedAt     *float64 `json:"pr_merged_at"`
	TipCommittedAt *float64 `json:"tip_committed_at"`
	RootCommit     *bool    `json:"root_commit"`  // true: the tip commit has no parent
	PRLookupOK     bool     `json:"pr_lookup_ok"` // false: the PR lookup itself failed
	// nil: the content check did not run or failed; empty: measured, nothing unique.
	UniqueFiles []string `json:"unique_files"`
	OwnCommits  *int     `json:"own_commits"` // commits not in the default branch
	LastSession *float64 `json:"last_session"`
	SessionID   *string  `json:"session_id"`
	Live        bool     `json:"live"`
	PortBlock   *string  `json:"port_block"`
	OrphanDir   *string  `json:"orphan_dir"`
	DirEntries  []string `json:"dir_entries"`
}

// NewRow is a Row with the defaults of the Python dataclass: the PR lookup succeeded.
func NewRow(name string) Row { return Row{Name: name, PRLookupOK: true} }

// WorktreeInfo is one block of `git worktree list --porcelain`.
type WorktreeInfo struct {
	Path       string  `json:"path"`
	Branch     *string `json:"branch"`
	Locked     bool    `json:"locked"`
	LockPID    *int    `json:"lock_pid"`
	LockReason *string `json:"lock_reason"`
}

// PullInfo is the PR that weighs heaviest on a head branch.
type PullInfo struct {
	Number   int      `json:"number"`
	State    string   `json:"state"`
	MergedAt *float64 `json:"merged_at"`
}

// SessionInfo is a running session from `~/.claude/sessions/*.json`.
type SessionInfo struct {
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

// Transcript is the newest transcript of a worktree: its mtime and its session id.
type Transcript struct {
	MTime     float64
	SessionID string
}

// BranchProbe is what git measured for one branch; nil is "not measured".
type BranchProbe struct {
	Dirty          *bool    `json:"dirty"`
	Unpushed       *int     `json:"unpushed"`
	UniqueFiles    []string `json:"unique_files"`
	OwnCommits     *int     `json:"own_commits"`
	TipCommittedAt *float64 `json:"tip_committed_at"`
	RootCommit     *bool    `json:"root_commit"`
}

// OrphanDir is a directory under `.claude/worktrees/` that git does not list.
type OrphanDir struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	DirEntries []string `json:"dir_entries"`
}

// Inputs are the arguments of build_rows: everything collected, nothing judged.
type Inputs struct {
	Worktrees      []WorktreeInfo
	LocalBranches  []string
	RemoteBranches []string
	Pulls          map[string]PullInfo
	Probes         map[string]BranchProbe
	Sessions       map[string]SessionInfo
	Transcripts    map[string]Transcript
	DetachedDirty  map[string]*bool
	Orphans        []OrphanDir
	Ports          map[string]string
	PRLookupOK     bool
}

var digitsRe = regexp.MustCompile(`\p{Nd}+`)

// portFor is the port block of the first run of digits in name that is a registry key. The
// digits are Unicode digits, as Python's `\d`.
func portFor(name string, ports map[string]string) *string {
	for _, chunk := range digitsRe.FindAllString(name, -1) {
		if port, ok := ports[chunk]; ok {
			return &port
		}
	}
	return nil
}

// isLive is true for a measured true and for an unmeasurable pid: only a measured false is dead.
func isLive(state *bool) bool { return state == nil || *state }

// pyName is Path(p).name: the last component, "" for a root or an empty path.
func pyName(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" || p == "." {
		return ""
	}
	return filepath.Base(p)
}

func ptr[T any](v T) *T { return &v }

func sessionOf(path string, in Inputs, alive Alive) (sid *string, last *float64, live bool) {
	session, hasSession := in.Sessions[path]
	transcript, hasTranscript := in.Transcripts[path]
	if hasTranscript {
		last = ptr(transcript.MTime)
	}
	switch {
	case hasSession:
		sid = ptr(session.SessionID)
	case hasTranscript:
		sid = ptr(transcript.SessionID)
	}
	return sid, last, hasSession && isLive(alive(session.PID))
}

// BuildRows is the join of build_rows, pure: a row per branch (local or remote, sorted by code
// point), then one per detached worktree, then one per orphan directory.
func BuildRows(in Inputs, alive Alive) []Row {
	byBranch := map[string]WorktreeInfo{}
	for _, w := range in.Worktrees {
		if w.Branch != nil && *w.Branch != "" {
			byBranch[*w.Branch] = w
		}
	}
	local, remote := set(in.LocalBranches), set(in.RemoteBranches)
	names := make([]string, 0, len(local)+len(remote))
	for name := range local {
		names = append(names, name)
	}
	for name := range remote {
		if !local[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names) // UTF-8 byte order is code point order, as Python's sorted

	var rows []Row
	for _, name := range names {
		r := NewRow(name)
		r.Local, r.Remote = local[name], remote[name]
		if w, ok := byBranch[name]; ok {
			r.Worktree, r.Locked, r.LockPID, r.LockReason = ptr(w.Path), w.Locked, w.LockPID, w.LockReason
			r.SessionID, r.LastSession, r.Live = sessionOf(w.Path, in, alive)
		}
		probe := in.Probes[name]
		r.Dirty, r.Unpushed, r.UniqueFiles, r.OwnCommits = probe.Dirty, probe.Unpushed, probe.UniqueFiles, probe.OwnCommits
		r.TipCommittedAt, r.RootCommit = probe.TipCommittedAt, probe.RootCommit
		if pull, ok := in.Pulls[name]; ok {
			r.PRNumber, r.PRState, r.PRMergedAt = ptr(pull.Number), ptr(pull.State), pull.MergedAt
		}
		r.PRLookupOK = in.PRLookupOK
		r.PortBlock = portFor(name, in.Ports)
		rows = append(rows, r)
	}
	// A detached worktree is in neither branch list and is no orphan: without a row of its own it
	// would vanish from the table.
	for _, w := range in.Worktrees {
		if w.Branch != nil {
			continue
		}
		r := NewRow(pyName(w.Path))
		r.Worktree, r.Locked, r.LockPID, r.LockReason = ptr(w.Path), w.Locked, w.LockPID, w.LockReason
		r.Dirty = in.DetachedDirty[w.Path]
		r.SessionID, r.LastSession, r.Live = sessionOf(w.Path, in, alive)
		r.PortBlock = portFor(r.Name, in.Ports)
		rows = append(rows, r)
	}
	for _, o := range in.Orphans {
		r := NewRow(o.Name)
		r.OrphanDir, r.DirEntries = ptr(o.Path), o.DirEntries
		r.PortBlock = portFor(o.Name, in.Ports)
		rows = append(rows, r)
	}
	return rows
}

func set(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

// pyDigits is int() of a run of Unicode decimal digits; false when it does not fit an int.
func pyDigits(digits string) (int, bool) {
	n := 0
	for _, r := range digits {
		start := r
		for unicode.Is(unicode.Nd, start-1) {
			start--
		}
		d := int(r-start) % 10 // every Nd range is a run of 0-9
		if n > (int(^uint(0)>>1)-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}

var lockPIDRe = regexp.MustCompile(`\(pid (\p{Nd}+)`)

// ParseWorktrees parses `git worktree list --porcelain`. Lines split as str.splitlines does. The
// lock pid is the first `(pid N` in the lock line; a pid too large for an int reads as unknown.
func ParseWorktrees(porcelain string) []WorktreeInfo {
	var out []WorktreeInfo
	var cur *WorktreeInfo
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range pytext.SplitLines(porcelain, false) {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &WorktreeInfo{Path: line[len("worktree "):]}
		case cur == nil:
			// Python keeps the fields until the next `worktree` line and drops them without a path.
		case strings.HasPrefix(line, "branch refs/heads/"):
			cur.Branch = ptr(line[len("branch refs/heads/"):])
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
			cur.LockReason = nil
			if strings.HasPrefix(line, "locked ") {
				cur.LockReason = ptr(line[len("locked "):])
			}
			cur.LockPID = nil
			if m := lockPIDRe.FindStringSubmatch(line); m != nil {
				if pid, ok := pyDigits(m[1]); ok {
					cur.LockPID = &pid
				}
			}
		}
	}
	flush()
	return out
}

// ParseBranches parses `git branch --format=%(refname:short)`. With remote it keeps only
// `origin/<name>` (not `origin/HEAD`) and strips the prefix.
func ParseBranches(text string, remote bool) []string {
	var names []string
	for _, line := range pytext.SplitLines(text, false) {
		name := pytext.Strip(line)
		if name == "" {
			continue
		}
		if remote {
			prefix, rest, _ := strings.Cut(name, "/")
			if prefix != "origin" || rest == "" || rest == "HEAD" {
				continue
			}
			name = rest
		}
		names = append(names, name)
	}
	return names
}

// ParsePortRegistry reads `<key>=<port>` lines; a later key wins.
func ParsePortRegistry(text string) map[string]string {
	ports := map[string]string{}
	for _, line := range pytext.SplitLines(text, false) {
		key, value, found := strings.Cut(line, "=")
		if found && pytext.Strip(key) != "" {
			ports[pytext.Strip(key)] = pytext.Strip(value)
		}
	}
	return ports
}

// EncodeProjectDir is the directory name of a path under `~/.claude/projects/`: `/`, `_` and `.`
// become `-`.
func EncodeProjectDir(path string) string {
	return strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(path)
}

// IssueBranches is the sorted union of the PR heads and the row names with prefix `issue-<nr>-`;
// the dash keeps issue 3 from matching `issue-32-x`.
func IssueBranches(rows []Row, nr int, prHeads []string) []string {
	prefix := "issue-" + itoa(nr) + "-"
	found := map[string]bool{}
	for _, r := range rows {
		if strings.HasPrefix(r.Name, prefix) {
			found[r.Name] = true
		}
	}
	for _, head := range prHeads {
		found[head] = true
	}
	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
