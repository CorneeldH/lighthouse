package worktreeaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/pytext"
)

// GitTimeout bounds one git call.
const GitTimeout = 60 * time.Second

// worktreeDir is where lighthouse puts its worktrees, relative to the main checkout.
const worktreeDir = ".claude/worktrees"

// Env is what Collect reaches outside itself. Run starts git (a test hands in a fake); a git call
// gets GitTimeout on top of whatever Run enforces. Forge opens the forge of `[forge] repo` for the
// PR lookups; it is only called when that is set.
type Env struct {
	Run    procrun.Func
	Config func() (*config.Config, error)
	Forge  func(ctx context.Context) (forge.Forge, error)
	Alive  Alive
	Home   string    // the home directory; `.claude/sessions` and `.claude/projects` hang below it
	Stderr io.Writer // the notice that the PR lookup is skipped or failed
}

// Failure stops the command: Message goes to stderr, Code is the exit code.
type Failure struct {
	Code    int
	Message string
}

func (f *Failure) Error() string { return f.Message }

// Audit is what Collect found: the rows and the policy the ladder judges them with.
type Audit struct {
	Rows   []Row
	Policy Policy
}

// The read-only git allowlist: exactly the verbs this command issues. A verb outside it is a
// refusal, never an execution.
var (
	readonlyVerbs       = set([]string{"worktree", "branch", "status", "rev-list", "diff", "cat-file", "remote"})
	readonlyBranchFlags = set([]string{"-a", "--all", "-r", "--remotes", "-v", "-vv", "--verbose", "--show-current"})
)

// assertReadonly is the gate of _assert_readonly. `branch --merged` is refused on purpose:
// squash-merge makes it lie; merge detection runs over the PR head branch.
func assertReadonly(args []string) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	if !readonlyVerbs[verb] {
		return fmt.Errorf("git %s: read-only commands only", verb)
	}
	switch verb {
	case "worktree":
		if len(args) < 2 || args[1] != "list" {
			sub := "<none>"
			if len(args) > 1 {
				sub = args[1]
			}
			return fmt.Errorf("git worktree %s: read-only commands only", sub)
		}
	case "branch":
		for _, a := range args[1:] {
			ok := strings.HasPrefix(a, "-") &&
				(readonlyBranchFlags[a] || strings.HasPrefix(a, "--format=") || strings.HasPrefix(a, "--sort="))
			if !ok {
				return errors.New("git branch: read-only commands only")
			}
		}
	case "remote":
		if len(args) < 2 || (args[1] != "get-url" && args[1] != "-v") {
			return errors.New("git remote: read-only commands only")
		}
	}
	return nil
}

// collector carries one run of Collect.
type collector struct {
	ctx     context.Context
	env     Env
	policy  Policy
	refused error // the first git call the allowlist refused
}

// git runs a read-only git command in dir ("" is the current directory) and returns its stdout,
// or nil when it failed: an unknown that the ladder never reads as clean. A verb outside the
// allowlist never reaches Run; Collect then fails with the refusal.
func (c *collector) git(dir string, args ...string) *string {
	if err := assertReadonly(args); err != nil {
		if c.refused == nil {
			c.refused = err
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(c.ctx, GitTimeout)
	defer cancel()
	res, err := c.env.Run(ctx, dir, nil, "git", args...)
	if err != nil || res.Code != 0 {
		return nil
	}
	return ptr(pytext.UniversalNewlines(res.Stdout))
}

func repoUnreadable(command string) error {
	return &Failure{Code: 1, Message: command + " failed — cannot read the repo."}
}

// policyFrom reads the default branch, the protected refs and paths and the repo of the PR lookup
// ("" when not set) from cfg.
func policyFrom(cfg *config.Config) (Policy, string, error) {
	var p Policy
	branch, err := config.Lookup(cfg, "forge.default_branch")
	if err != nil {
		return p, "", err
	}
	p.DefaultBranch = pyStr(branch)
	for key, dst := range map[string]*[]string{
		"worktree_audit.protected_refs":  &p.ProtectedRefs,
		"worktree_audit.protected_paths": &p.ProtectedPaths,
	} {
		v, err := config.Lookup(cfg, key)
		if err != nil {
			return p, "", err
		}
		list, _ := v.([]any)
		for _, item := range list {
			*dst = append(*dst, pyStr(item))
		}
	}
	repo, err := config.Lookup(cfg, "forge.repo")
	switch {
	case errors.Is(err, config.ErrNotSet):
		return p, "", nil
	case err != nil:
		return p, "", err
	}
	return p, pyStr(repo), nil
}

// PolicyFromConfig is the policy of a parsed lighthouse.toml.
func PolicyFromConfig(cfg *config.Config) (Policy, error) {
	p, _, err := policyFrom(cfg)
	return p, err
}

// Collect gathers the rows of the repo whose main checkout is repoRoot ("" for the first worktree
// git lists). A git listing that fails, or a broken config, is a *Failure.
func Collect(ctx context.Context, env Env, repoRoot string) (*Audit, error) {
	c := &collector{ctx: ctx, env: env}
	porcelain := c.git("", "worktree", "list", "--porcelain")
	if porcelain == nil {
		return nil, repoUnreadable("git worktree list --porcelain")
	}
	worktrees := ParseWorktrees(*porcelain)
	root := repoRoot
	switch {
	case root != "":
	case len(worktrees) > 0:
		root = worktrees[0].Path // the main checkout is always the first block
	default:
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root = wd
	}
	localRaw := c.git(root, "branch", "--format=%(refname:short)")
	if localRaw == nil {
		return nil, repoUnreadable("git branch --format=%(refname:short)")
	}
	remoteRaw := c.git(root, "branch", "-r", "--format=%(refname:short)")
	if remoteRaw == nil {
		return nil, repoUnreadable("git branch -r --format=%(refname:short)")
	}
	local, remote := ParseBranches(*localRaw, false), ParseBranches(*remoteRaw, true)

	cfg, err := env.Config()
	var repo string
	if err == nil {
		c.policy, repo, err = policyFrom(cfg)
	}
	if err != nil {
		// Exit 2, not 1: under --only, 1 already means "not cleanable".
		return nil, &Failure{Code: 2, Message: "worktree-audit: " + err.Error()}
	}
	if repo == "" {
		_, _ = fmt.Fprintln(env.Stderr, "worktree-audit: PR lookup skipped — [forge] repo is not set, so every PR state is unknown")
	}
	pulls, lookupOK := map[string]PullInfo{}, false
	if repo != "" {
		var perr error
		if pulls, perr = readPulls(ctx, env); perr != nil {
			// A failed lookup is no signal, and no signal is no cleanup yes: never "no PR".
			_, _ = fmt.Fprintln(env.Stderr, "worktree-audit: PR lookup failed — "+perr.Error())
			pulls = map[string]PullInfo{}
		} else {
			lookupOK = true
		}
	}

	byBranch := map[string]WorktreeInfo{}
	for _, w := range worktrees {
		if w.Branch != nil && *w.Branch != "" {
			byBranch[*w.Branch] = w
		}
	}
	in := Inputs{
		Worktrees:      worktrees,
		LocalBranches:  local,
		RemoteBranches: remote,
		Pulls:          pulls,
		Probes:         c.probeAll(local, remote, byBranch, pulls, root),
		Sessions:       loadSessions(filepath.Join(env.Home, ".claude", "sessions")),
		Transcripts:    map[string]Transcript{},
		DetachedDirty:  map[string]*bool{},
		Orphans:        findOrphans(root, worktrees),
		Ports:          map[string]string{},
		PRLookupOK:     lookupOK,
	}
	projects := filepath.Join(env.Home, ".claude", "projects")
	for _, w := range worktrees {
		if t, ok := latestTranscript(projects, w.Path); ok {
			in.Transcripts[w.Path] = t
		}
		if w.Branch == nil {
			in.DetachedDirty[w.Path] = dirtyOf(c.git(w.Path, "status", "--porcelain"))
		}
	}
	for _, o := range in.Orphans {
		for _, entry := range o.DirEntries {
			if entry != ".port-registry" {
				continue
			}
			in.Ports = map[string]string{}
			if text, err := os.ReadFile(filepath.Join(o.Path, ".port-registry")); err == nil {
				in.Ports = ParsePortRegistry(pytext.UniversalNewlines(string(text)))
			}
		}
	}
	if c.refused != nil {
		return nil, &Failure{Code: 1, Message: "worktree-audit: " + c.refused.Error()}
	}
	return &Audit{Rows: BuildRows(in, env.Alive), Policy: c.policy}, nil
}

func dirtyOf(status *string) *bool {
	if status == nil {
		return nil
	}
	return ptr(pytext.Strip(*status) != "")
}

// readPulls is the heaviest pull or merge request per head branch, over every one of the forge.
func readPulls(ctx context.Context, env Env) (map[string]PullInfo, error) {
	f, err := env.Forge(ctx)
	if err != nil {
		return nil, err
	}
	changes, err := f.ListChanges(ctx, "all")
	if err != nil {
		return nil, err
	}
	return ParsePulls(changes)
}

// IssuePRHeads asks the forge of `[forge] repo` for the merged PR heads of issue nr; false when
// the lookup failed or there is no repo, with the reason on Env.Stderr. That is never "no PRs".
func IssuePRHeads(ctx context.Context, env Env, nr int) ([]string, bool) {
	heads, err := issuePRHeads(ctx, env, nr)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "worktree-audit: PR lookup for issue #%d failed — %v\n", nr, err)
		return nil, false
	}
	return heads, true
}

func issuePRHeads(ctx context.Context, env Env, nr int) ([]string, error) {
	cfg, err := env.Config()
	if err != nil {
		return nil, err
	}
	_, repo, err := policyFrom(cfg)
	switch {
	case err != nil:
		return nil, err
	case repo == "":
		return nil, errors.New("[forge] repo is not set")
	}
	f, err := env.Forge(ctx)
	if err != nil {
		return nil, err
	}
	changes, err := f.IssueChanges(ctx, nr)
	if err != nil {
		return nil, err
	}
	return ParseIssuePRHeads(changes, repo, nr), nil
}

// probeAll measures every branch that no protected ref covers; the ref of a remote-only branch is
// `origin/<name>`, and a merged PR skips the content check.
func (c *collector) probeAll(local, remote []string, byBranch map[string]WorktreeInfo, pulls map[string]PullInfo, root string) map[string]BranchProbe {
	localSet := set(local)
	names := map[string]bool{}
	for _, n := range append(append([]string(nil), local...), remote...) {
		names[n] = true
	}
	out := map[string]BranchProbe{}
	for name := range names {
		if c.policy.protectsRef(name) {
			continue
		}
		ref := "origin/" + name
		if localSet[name] {
			ref = name
		}
		var wt *WorktreeInfo
		if w, ok := byBranch[name]; ok {
			wt = &w
		}
		pull, hasPull := pulls[name]
		out[name] = c.probe(wt, root, ref, hasPull && pull.State == "merged")
	}
	return out
}

func countOf(out *string) *int {
	if out == nil {
		return nil
	}
	s := pytext.Strip(*out)
	if !allDigits(s) {
		return nil
	}
	n, ok := pyDigits(s)
	if !ok {
		return nil
	}
	return &n
}

// probe measures what prune does not catch: a dirty tree, commits on no remote, the tip's shape
// and, unless skipContent, the commits and files the default branch lacks. nil stays "not
// measured"; skipContent never turns into an empty list.
func (c *collector) probe(wt *WorktreeInfo, root, ref string, skipContent bool) BranchProbe {
	var p BranchProbe
	if wt != nil {
		p.Dirty = dirtyOf(c.git(wt.Path, "status", "--porcelain"))
	}
	p.Unpushed = countOf(c.git(root, "rev-list", "--count", ref, "--not", "--remotes"))
	p.TipCommittedAt, p.RootCommit = parseTipShape(c.git(root, "rev-list", "-1", "--timestamp", "--parents", ref))
	if skipContent {
		return p
	}
	base := c.policy.DefaultBranch
	p.OwnCommits = countOf(c.git(root, "rev-list", "--count", base+".."+ref))
	if changed := c.git(root, "diff", "--name-only", base+"..."+ref); changed != nil {
		p.UniqueFiles = []string{}
		for _, path := range pytext.SplitLines(*changed, false) {
			path = pytext.Strip(path)
			if path != "" && c.git(root, "cat-file", "-e", base+":"+path) == nil {
				p.UniqueFiles = append(p.UniqueFiles, path)
			}
		}
	}
	return p
}

// findOrphans is every directory under `<root>/.claude/worktrees/` that git does not list, with
// its sorted entries. A directory that cannot be read has no entries; no base, no orphans.
func findOrphans(root string, worktrees []WorktreeInfo) []OrphanDir {
	known := map[string]bool{}
	for _, w := range worktrees {
		known[w.Path] = true
	}
	base := filepath.Join(root, worktreeDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []OrphanDir
	for _, e := range entries { // os.ReadDir sorts by name
		path := filepath.Join(base, e.Name())
		if info, err := os.Stat(path); err != nil || !info.IsDir() || known[path] {
			continue
		}
		names := []string{}
		if children, err := os.ReadDir(path); err == nil {
			for _, child := range children {
				names = append(names, child.Name())
			}
		}
		out = append(out, OrphanDir{Name: e.Name(), Path: path, DirEntries: names})
	}
	return out
}

// jsonFiles is every entry of dir with the suffix, sorted by name (pathlib's glob, also dotfiles).
func jsonFiles(dir, suffix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// loadSessions reads `~/.claude/sessions/*.json`: only running sessions are there. A missing
// directory is no sessions; a file that cannot be read or parsed is skipped.
func loadSessions(dir string) map[string]SessionInfo {
	var records []any
	for _, name := range jsonFiles(dir, ".json") {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var v any
		if json.Unmarshal(data, &v) != nil {
			continue
		}
		records = append(records, v)
	}
	return ParseSessions(records)
}

// pyStem is Path(name).stem: the name without its last suffix; a leading dot is no suffix.
func pyStem(name string) string {
	if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		return name[:i]
	}
	return name
}

// latestTranscript is the mtime and session id of the newest `*.jsonl` of a worktree under
// projectsRoot; closed sessions show up here, not in `~/.claude/sessions`.
func latestTranscript(projectsRoot, worktreePath string) (Transcript, bool) {
	dir := filepath.Join(projectsRoot, EncodeProjectDir(worktreePath))
	var newest Transcript
	found := false
	for _, name := range jsonFiles(dir, ".jsonl") {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		mtime := float64(info.ModTime().UnixNano()) / 1e9
		if !found || mtime > newest.MTime {
			newest, found = Transcript{MTime: mtime, SessionID: pyStem(name)}, true
		}
	}
	return newest, found
}
