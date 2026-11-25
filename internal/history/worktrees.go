package history

import (
	"regexp"
	"strings"
)

var (
	worktreeAddRe = regexp.MustCompile(`\bworktree\s+add\b(.*)`)
	claudeWtRe    = regexp.MustCompile(`(?:^|/)\.claude/worktrees/([^/]+)`)
	oneDriveRe    = regexp.MustCompile(`(?:^|/)onedrive[^/]*(?:/|$)`)
	dropboxRe     = regexp.MustCompile(`(?:^|/)dropbox[^/]*(?:/|$)`)
	iCloudRe      = regexp.MustCompile(`(?:^|/)icloud[^/]*(?:/|$)|(?:^|/)library/mobile documents(?:/|$)`)
)

// addFlagsWithValue are the `git worktree add` flags that take a separate value.
var addFlagsWithValue = map[string]bool{"-b": true, "-B": true, "--reason": true}

// worktreeAddPath returns the path argument of the first `git worktree add` in a command,
// or "" when there is none or it is a shell variable.
func worktreeAddPath(cmd string) string {
	m := worktreeAddRe.FindStringSubmatch(cmd)
	if m == nil {
		return ""
	}
	rest := m[1]
	if i := strings.IndexAny(rest, ";&|\n"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	for rest != "" {
		var tok string
		if q := rest[0]; q == '\'' || q == '"' {
			end := strings.IndexByte(rest[1:], q)
			if end < 0 {
				tok, rest = rest[1:], ""
			} else {
				tok, rest = rest[1:end+1], strings.TrimSpace(rest[end+2:])
			}
		} else if i := strings.IndexAny(rest, " \t"); i >= 0 {
			tok, rest = rest[:i], strings.TrimSpace(rest[i:])
		} else {
			tok, rest = rest, ""
		}
		if strings.HasPrefix(tok, "-") {
			if addFlagsWithValue[tok] {
				_, rest = splitToken(rest)
			}
			continue
		}
		if strings.HasPrefix(tok, "$") {
			return ""
		}
		return tok
	}
	return ""
}

func splitToken(s string) (string, string) {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i:])
	}
	return s, ""
}

func normPath(p string) string {
	return strings.TrimRight(strings.ToLower(strings.ReplaceAll(p, `\`, "/")), "/")
}

// syncFolder names the synchronisation folder a normalised path lies in, or "".
func syncFolder(p string) string {
	switch {
	case oneDriveRe.MatchString(p):
		return "OneDrive"
	case dropboxRe.MatchString(p):
		return "Dropbox"
	case iCloudRe.MatchString(p):
		return "iCloud"
	}
	return ""
}

// worktrees measures where worktrees are made. It keeps only counts and fixed pattern
// names; the paths themselves stay in memory to count distinct ones and never reach the report.
type worktrees struct {
	parent   string // normalised parent directory of the repo root
	adds     int
	patterns map[string]int
	syncs    map[string]int
	addPaths map[string]bool
	cwds     map[string]bool
	hits     Hits
}

func newWorktrees(env Env) Detector {
	d := &worktrees{patterns: map[string]int{}, syncs: map[string]int{}, addPaths: map[string]bool{}, cwds: map[string]bool{}}
	if root := normPath(env.RepoRoot); root != "" {
		if i := strings.LastIndexByte(root, '/'); i > 0 {
			d.parent = root[:i]
		}
	}
	return d
}

func (d *worktrees) Add(r Record) {
	hit := false
	for _, c := range r.Commands {
		raw := worktreeAddPath(c)
		if !worktreeAddRe.MatchString(c) {
			continue
		}
		d.adds++
		hit = true
		if raw == "" {
			continue
		}
		p := normPath(raw)
		d.addPaths[p] = true
		if claudeWtRe.MatchString(p) {
			d.patterns["~/.claude/worktrees"]++
		}
		if d.sibling(p) {
			d.patterns["repo-sibling"]++
		}
		if s := syncFolder(p); s != "" {
			d.patterns["sync_folder"]++
			d.syncs[s]++
		}
	}
	if cwd := normPath(r.Cwd); cwd != "" {
		if s := syncFolder(cwd); s != "" {
			d.syncs[s]++
			d.cwds[cwd] = true
			hit = true
		} else if claudeWtRe.MatchString(cwd) {
			d.cwds[cwd] = true
			hit = true
		}
	}
	if hit {
		d.hits.Add(r.File, r.Line, r.Timestamp)
	}
}

// sibling reports whether p lies directly beside the repo root: a relative ../ path, or
// a path in the parent directory of the root.
func (d *worktrees) sibling(p string) bool {
	if strings.HasPrefix(p, "../") {
		return !strings.Contains(p[3:], "/")
	}
	if d.parent == "" || !strings.HasPrefix(p, d.parent+"/") {
		return false
	}
	return !strings.Contains(p[len(d.parent)+1:], "/")
}

// distinct counts worktree paths once: the add paths, plus the cwds that match a
// worktree pattern and do not lie under an add path.
func (d *worktrees) distinct() int {
	seen := map[string]bool{}
	for p := range d.addPaths {
		seen[p] = true
	}
	for c := range d.cwds {
		key := c
		if m := claudeWtRe.FindStringSubmatchIndex(c); m != nil {
			key = c[:m[3]]
		}
		under := false
		for p := range d.addPaths {
			if c == p || strings.HasPrefix(c, p+"/") {
				under = true
				break
			}
		}
		if !under {
			seen[key] = true
		}
	}
	return len(seen)
}

func (d *worktrees) Finish(Env) Finding {
	f := Finding{
		Key:        "worktrees",
		Status:     "none",
		Confidence: "high",
		Measured: map[string]any{
			"adds":               d.adds,
			"distinct_worktrees": d.distinct(),
			"path_patterns":      d.patterns,
		},
		Meaning: "Shows where worktrees are made; worktrees in a synchronisation folder give lock and copy problems.",
		Proposal: Proposal{Kind: "advice",
			Text: "Keep worktrees outside synchronisation folders, for example under ~/.claude/worktrees."},
	}
	if len(d.syncs) > 0 {
		f.Measured["sync_folders"] = d.syncs
	}
	if d.adds > 0 || len(d.cwds) > 0 {
		f.Status = "found"
		f.Sources = d.hits.Top()
	}
	if len(d.syncs) == 0 && f.Status == "found" {
		f.Proposal.Text = "Worktrees are not in a synchronisation folder; no change needed."
	}
	return f
}
