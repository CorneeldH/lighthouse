package worktreeaudit

import (
	"fmt"
	"strconv"
	"strings"
)

// Alive measures a pid: true when the process exists, false when it is gone, nil when that cannot
// be measured. PidAlive is the real one.
type Alive func(pid int) *bool

// Policy is what the ladder protects. DefaultBranch is always protected; ProtectedRefs and
// ProtectedPaths come from `[worktree_audit]` and are fnmatch patterns, as in Python.
type Policy struct {
	DefaultBranch  string
	ProtectedRefs  []string
	ProtectedPaths []string
}

// refs is PROTECTED_REFS of the Python: the default branch first.
func (p Policy) refs() []string { return append([]string{p.DefaultBranch}, p.ProtectedRefs...) }

// protectsRef reports whether name matches a protected ref pattern, without stripping anything.
func (p Policy) protectsRef(name string) bool {
	for _, pattern := range p.refs() {
		if fnmatch(name, pattern) {
			return true
		}
	}
	return false
}

func (p Policy) isProtected(r Row) bool {
	if p.protectsRef(strings.TrimPrefix(r.Name, "refs/heads/")) {
		return true
	}
	for _, entry := range r.DirEntries {
		for _, pattern := range p.ProtectedPaths {
			if fnmatch(entry, pattern) {
				return true
			}
		}
	}
	return false
}

// Verdict is one rung of the ladder. Only rank 6 and 8 are "safe to remove".
type Verdict struct {
	Rank      int
	Icon      string
	Text      string
	Modifiers []string
}

// Safe reports rank 6 or 8; everything above blocks.
func (v Verdict) Safe() bool { return v.Rank == 6 || v.Rank == 8 }

// Render is the verdict as the table shows it: icon, text and the modifiers in brackets.
func (v Verdict) Render() string {
	if len(v.Modifiers) == 0 {
		return v.Icon + " " + v.Text
	}
	return v.Icon + " " + v.Text + " [" + strings.Join(v.Modifiers, ", ") + "]"
}

func (v Verdict) has(modifier string) bool {
	for _, m := range v.Modifiers {
		if m == modifier {
			return true
		}
	}
	return false
}

// locked is a stale or server lock left on a verdict: git refuses to remove such a row.
func (v Verdict) locked() bool { return v.has("stale lock") || v.has("server-lock") }

const serverLockPrefix = "claude agent bridge-"

// lockState is rank 2 for a lock that blocks, or the modifier a stale or server lock leaves.
// Unknown and unmeasurable pids block: unknown is not dead.
func lockState(r Row, alive Alive) (*Verdict, []string) {
	if !r.Locked {
		return nil, nil
	}
	if r.LockReason != nil && strings.HasPrefix(*r.LockReason, serverLockPrefix) {
		return nil, []string{"server-lock"}
	}
	if r.LockPID == nil {
		return &Verdict{2, "⛔", "locked, pid unknown", nil}, nil
	}
	pid := *r.LockPID
	state := alive(pid)
	switch {
	case state == nil:
		return &Verdict{2, "⛔", fmt.Sprintf("locked, pid %d not measurable", pid), nil}, nil
	case *state:
		return &Verdict{2, "⛔", fmt.Sprintf("active session (pid %d)", pid), nil}, nil
	}
	return nil, []string{"stale lock"}
}

// branchVerdict is rank 5 to 8: the PR state, then the content check. Unknown is never clean.
func branchVerdict(r Row, mods []string) Verdict {
	switch {
	case r.PRState != nil && *r.PRState == "open":
		return Verdict{5, "⏸️", "PR #" + pyRepr(r.PRNumber) + " open", mods}
	case r.PRState != nil && *r.PRState == "merged":
		return Verdict{6, "✅", "safe to remove", mods}
	case !r.PRLookupOK:
		return Verdict{7, "⚠️", "PR status unknown — check", mods}
	case r.UniqueFiles == nil:
		return Verdict{7, "⚠️", "content check failed — check", mods}
	case len(r.UniqueFiles) > 0:
		return Verdict{7, "⚠️", fmt.Sprintf("unique work — review (%d files)", len(r.UniqueFiles)), mods}
	case r.OwnCommits != nil && *r.OwnCommits == 0:
		return Verdict{8, "✅", "empty — no own commits", mods}
	}
	return Verdict{8, "✅", "safe to remove", mods}
}

// squashArtefact explains an unpushed count that only reflects a squash-merge: the tip was
// committed no later than the merge. Missing timestamps keep the block.
func squashArtefact(r Row) bool {
	if r.PRState == nil || *r.PRState != "merged" || r.PRMergedAt == nil || r.TipCommittedAt == nil {
		return false
	}
	return *r.TipCommittedAt <= *r.PRMergedAt
}

// testLeakage is one parentless commit, local only, without a PR: `git checkout --orphan` in the
// wrong repo. It names the cause; it never makes a row safe.
func testLeakage(r Row) bool {
	return r.RootCommit != nil && *r.RootCommit && r.Local && !r.Remote && r.PRState == nil && r.PRNumber == nil
}

// unpushedVerdict is rank 4, or mods plus "squash-merge" when the count is that artefact.
func unpushedVerdict(r Row, mods []string) (*Verdict, []string) {
	if r.Local && r.Unpushed == nil {
		return &Verdict{4, "⚠️", "unpushed commits unknown — check", mods}, mods
	}
	if r.Unpushed != nil && *r.Unpushed != 0 {
		switch {
		case testLeakage(r):
			return &Verdict{4, "⚠️", "orphan commit without parent — likely test leakage", mods}, mods
		case squashArtefact(r):
			mods = append(append([]string(nil), mods...), "squash-merge")
		default:
			return &Verdict{4, "⚠️", strconv.Itoa(*r.Unpushed) + " local commits — check", mods}, mods
		}
	}
	return nil, mods
}

// Judge is the ladder of worktree_audit.judge: first hit wins, safety on top. Rank 3 and 4 sit
// above rank 6, so a merged branch with a dirty tree or unpushed commits is never safe.
func (p Policy) Judge(r Row, alive Alive) Verdict {
	if p.isProtected(r) {
		return Verdict{1, "🔒", "never touch", nil}
	}
	lock, mods := lockState(r, alive)
	if lock != nil {
		return *lock
	}
	if r.Live {
		return Verdict{2, "⛔", "running session", mods}
	}
	if r.Worktree != nil && (r.Dirty == nil || *r.Dirty) {
		text := "tree status unreadable"
		if r.Dirty != nil {
			text = "save first"
		}
		return Verdict{3, "⛔", text, mods}
	}
	verdict, mods := unpushedVerdict(r, mods)
	if verdict != nil {
		return *verdict
	}
	if r.Local || r.Remote {
		return branchVerdict(r, mods)
	}
	if r.Worktree != nil {
		return Verdict{9, "❓", "detached worktree — manual", mods}
	}
	return Verdict{9, "❓", "orphan — manual", mods}
}

// pyRepr is how an f-string shows an optional int: the number, or None.
func pyRepr(n *int) string {
	if n == nil {
		return "None"
	}
	return strconv.Itoa(*n)
}

// fnmatch is fnmatch.fnmatch on POSIX: case-sensitive, `*` matches any run (also `/`), `?` one
// character, `[seq]` and `[!seq]` a set with ranges; an unclosed `[` is a literal. path.Match
// differs: there `*` stops at `/`, and `entire/*` would not protect `entire/checkpoints/v1`.
func fnmatch(name, pattern string) bool {
	return matchTokens([]rune(name), compileGlob([]rune(pattern)))
}

type globKind int

const (
	globLiteral globKind = iota
	globAny
	globStar
	globSet
)

type globToken struct {
	kind   globKind
	r      rune
	negate bool
	ranges [][2]rune
}

// compileGlob follows fnmatch.translate: `]` right after `[` or `[!` is a member, and a reversed
// range such as `z-a` drops out together with its two ends.
func compileGlob(p []rune) []globToken {
	var out []globToken
	for i := 0; i < len(p); {
		c := p[i]
		i++
		switch c {
		case '*':
			if len(out) == 0 || out[len(out)-1].kind != globStar {
				out = append(out, globToken{kind: globStar})
			}
		case '?':
			out = append(out, globToken{kind: globAny})
		case '[':
			j := i
			if j < len(p) && p[j] == '!' {
				j++
			}
			if j < len(p) && p[j] == ']' {
				j++
			}
			for j < len(p) && p[j] != ']' {
				j++
			}
			if j >= len(p) {
				out = append(out, globToken{kind: globLiteral, r: '['})
				continue
			}
			out = append(out, compileSet(p[i:j]))
			i = j + 1
		default:
			out = append(out, globToken{kind: globLiteral, r: c})
		}
	}
	return out
}

// compileSet builds the set of `[stuff]` the way fnmatch.translate splits it into chunks at `-`.
func compileSet(stuff []rune) globToken {
	tok := globToken{kind: globSet}
	body := stuff
	if len(body) > 0 && body[0] == '!' {
		tok.negate = true
	}
	// The chunks between the range dashes; the first dash is searched from the second
	// character (third after `!`), as Python does.
	var chunks [][]rune
	start, k := 0, 1
	if tok.negate {
		k = 2
	}
	for {
		dash := -1
		for x := k; x < len(body); x++ {
			if body[x] == '-' {
				dash = x
				break
			}
		}
		if dash < 0 {
			break
		}
		chunks = append(chunks, body[start:dash])
		start = dash + 1
		k = dash + 3
	}
	if tail := body[start:]; len(tail) > 0 || len(chunks) == 0 {
		chunks = append(chunks, tail)
	} else {
		chunks[len(chunks)-1] = append(append([]rune(nil), chunks[len(chunks)-1]...), '-') // a trailing dash is a member
	}
	for c := len(chunks) - 1; c > 0; c-- {
		prev, cur := chunks[c-1], chunks[c]
		if len(prev) > 0 && len(cur) > 0 && prev[len(prev)-1] > cur[0] {
			chunks[c-1] = append(append([]rune(nil), prev[:len(prev)-1]...), cur[1:]...)
			chunks = append(chunks[:c], chunks[c+1:]...)
		}
	}
	if tok.negate {
		chunks[0] = chunks[0][1:]
	}
	// Each dash between two chunks is a range from the last rune of one to the first of the next.
	for c, chunk := range chunks {
		for x, r := range chunk {
			first := x == 0 && c > 0
			last := x == len(chunk)-1 && c < len(chunks)-1
			if first || last {
				continue // an end of a range, added below
			}
			tok.ranges = append(tok.ranges, [2]rune{r, r})
		}
		// Python only gets an empty chunk here by crashing on it; skip such a range.
		if prev := chunks[max(c-1, 0)]; c > 0 && len(prev) > 0 && len(chunk) > 0 {
			tok.ranges = append(tok.ranges, [2]rune{prev[len(prev)-1], chunk[0]})
		}
	}
	return tok
}

func (t globToken) matches(r rune) bool {
	in := false
	for _, rg := range t.ranges {
		if r >= rg[0] && r <= rg[1] {
			in = true
			break
		}
	}
	return in != t.negate
}

func matchTokens(name []rune, toks []globToken) bool {
	if len(toks) == 0 {
		return len(name) == 0
	}
	t := toks[0]
	if t.kind == globStar {
		for i := 0; i <= len(name); i++ {
			if matchTokens(name[i:], toks[1:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	switch t.kind {
	case globLiteral:
		if name[0] != t.r {
			return false
		}
	case globSet:
		if !t.matches(name[0]) {
			return false
		}
	}
	return matchTokens(name[1:], toks[1:])
}
