package lane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/initcmd"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

// ErrTouchesWorkflows means the result reaches .github/workflows/: pushing it needs the
// `workflows` permission, which the lane does not get, so it becomes an issue instead of a PR.
var ErrTouchesWorkflows = errors.New("the change touches .github/workflows/")

const sourcesPath = "pack/SOURCES.toml"

// BranchName is the rolling branch of a pin key: pins/<key> with every character outside
// [A-Za-z0-9._-] (the / of a key included) as -.
func BranchName(key string) string {
	return "pins/" + branchUnsafe.ReplaceAllString(key, "-")
}

var branchUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+|\.\.+`)

// PrepareBranch puts a clean merge on the rolling branch pins/<key> on origin and returns its
// name. In a fresh worktree from origin's default branch it writes mr, raises the pin to headSHA
// (`upstream-commit` of every variant, or `source_sha`), brings pack/SOURCES.toml along (a new
// file gets `pin = "<key>"`, a removed one loses its entry), writes a changelog.d/ fragment,
// runs `init --upgrade --yes` from the worktree's own pack/ so the installed copies follow,
// commits as github-actions[bot] and force-pushes. The worktree is removed afterwards, so the
// next pin starts from the default branch again. A result that reaches .github/workflows/,
// through mr or through init --upgrade, pushes nothing and returns ErrTouchesWorkflows.
func PrepareBranch(ctx context.Context, root string, p pins.SourcePin, mr MergeResult, headSHA string) (string, error) {
	switch {
	case !mr.Clean:
		return "", fmt.Errorf("pin %s: merge has conflicts: %s", p.Key, strings.Join(mr.Conflicts, ", "))
	case !shaRe.MatchString(headSHA):
		return "", fmt.Errorf("pin %s: upstream HEAD %q is not a 40-hex sha", p.Key, headSHA)
	}
	base, err := defaultBranchHead(ctx, root)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "lighthouse-pins-wt-*")
	if err != nil {
		return "", err
	}
	wt := filepath.Join(tmp, "wt")
	defer func() {
		_, _ = laneGit(context.WithoutCancel(ctx), root, "worktree", "remove", "--force", wt)
		_ = os.RemoveAll(tmp)
		_, _ = laneGit(context.WithoutCancel(ctx), root, "worktree", "prune")
	}()
	if _, err := laneGit(ctx, root, "worktree", "add", "--quiet", "--detach", wt, base); err != nil {
		return "", err
	}
	if err := applyChange(wt, p, mr, headSHA); err != nil {
		return "", fmt.Errorf("pin %s: %w", p.Key, err)
	}
	if err := upgradeInstalled(wt); err != nil {
		return "", fmt.Errorf("pin %s: %w", p.Key, err)
	}
	if _, err := laneGit(ctx, wt, "add", "--all"); err != nil {
		return "", err
	}
	out, err := laneGit(ctx, wt, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return "", err
	}
	var workflows []string
	for _, f := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(f, workflowsDir) {
			workflows = append(workflows, f)
		}
	}
	if len(workflows) > 0 {
		return "", fmt.Errorf("pin %s: %w: %s", p.Key, ErrTouchesWorkflows, strings.Join(workflows, ", "))
	}
	msg := fmt.Sprintf("chore(pins): %s volgt upstream %s tot %s", p.Key, p.Repo, headSHA[:7])
	if _, err := laneGit(ctx, wt, "commit", "--quiet", "-m", msg); err != nil {
		return "", err
	}
	branch := BranchName(p.Key)
	// The branch belongs to the lane, never to a person: a leftover of an earlier run is replaced.
	if _, err := laneGit(ctx, wt, "push", "--quiet", "--force", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return "", err
	}
	return branch, nil
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// defaultBranchHead fetches origin's default branch (its HEAD, whatever its name) and returns
// the commit.
func defaultBranchHead(ctx context.Context, root string) (string, error) {
	out, err := laneGit(ctx, root, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", err
	}
	var name string
	for _, line := range strings.Split(string(out), "\n") {
		if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			name, _, _ = strings.Cut(ref, "\t")
		}
	}
	if name == "" {
		return "", fmt.Errorf("origin names no default branch: %q", out)
	}
	if _, err := laneGit(ctx, root, "fetch", "--quiet", "--no-tags", "origin", "refs/heads/"+name); err != nil {
		return "", err
	}
	sha, err := laneGit(ctx, root, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	return strings.TrimSpace(string(sha)), err
}

// applyChange writes the merge, the raised pin, SOURCES.toml and the changelog fragment into wt.
func applyChange(wt string, p pins.SourcePin, mr MergeResult, headSHA string) error {
	for _, rel := range append(slices.Collect(maps.Keys(mr.Files)), mr.Removed...) {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return fmt.Errorf("path %q leaves the repository", rel)
		}
	}
	var added []string
	for rel, data := range mr.Files {
		abs := filepath.Join(wt, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			added = append(added, rel)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, data, 0o644); err != nil {
			return err
		}
	}
	for _, rel := range mr.Removed {
		if err := os.Remove(filepath.Join(wt, filepath.FromSlash(rel))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	sources, err := os.ReadFile(filepath.Join(wt, sourcesPath))
	if err != nil {
		return err
	}
	text := string(sources)
	if isFilePin(p) {
		if text, err = raiseSourceSHA(text, p.Key, p.Commit, headSHA); err != nil {
			return err
		}
	} else {
		for _, f := range p.Files {
			if err := raiseSkillCommit(filepath.Join(wt, filepath.FromSlash(f)), p.Commit, headSHA); err != nil {
				return err
			}
		}
	}
	text = editSources(text, p.Key, added, mr.Removed)
	if _, err := toml.Decode(text, &struct{}{}); err != nil {
		return fmt.Errorf("%s after the edit: %w", sourcesPath, err)
	}
	if err := os.WriteFile(filepath.Join(wt, sourcesPath), []byte(text), 0o644); err != nil {
		return err
	}
	fragment := fmt.Sprintf("---\ntype: changed\n---\n`%s` follows upstream `%s` up to commit `%s`.\n", p.Key, p.Repo, headSHA[:7])
	name := strings.TrimPrefix(BranchName(p.Key), "pins/")
	if err := os.MkdirAll(filepath.Join(wt, "changelog.d"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(wt, "changelog.d", "pins-"+name+".md"), []byte(fragment), 0o644)
}

// raiseSkillCommit sets `upstream-commit` in the frontmatter metadata of a SKILL.md from old
// to sha.
func raiseSkillCommit(file, old, sha string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	for i, line := range lines[1:] {
		if strings.TrimRight(line, "\r\n") == "---" {
			break
		}
		if key, _, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && key == "upstream-commit" && strings.Contains(line, old) {
			lines[i+1] = strings.Replace(line, old, sha, 1)
			return os.WriteFile(file, []byte(strings.Join(lines, "")), 0o644)
		}
	}
	return fmt.Errorf("%s: no upstream-commit %s in the frontmatter", file, old)
}

// sourcesEntries splits SOURCES.toml into its head and one chunk per [[file]] entry, each chunk
// from its blank separator line on, so that dropping a chunk leaves the rest as it was.
func sourcesEntries(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	var chunks []string
	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) != "[[file]]" {
			continue
		}
		cut := i
		for cut > start && strings.TrimSpace(lines[cut-1]) == "" {
			cut--
		}
		chunks = append(chunks, strings.Join(lines[start:cut], ""))
		start = cut
	}
	return append(chunks, strings.Join(lines[start:], ""))
}

var entryPathRe = regexp.MustCompile(`(?m)^\s*path\s*=\s*"([^"]*)"\s*$`)

func entryPath(chunk string) string {
	if m := entryPathRe.FindStringSubmatch(chunk); m != nil {
		return m[1]
	}
	return ""
}

// raiseSourceSHA sets source_sha of the entry with path from old to sha.
func raiseSourceSHA(text, path, old, sha string) (string, error) {
	chunks := sourcesEntries(text)
	for i, c := range chunks {
		if i == 0 || entryPath(c) != path {
			continue
		}
		re := regexp.MustCompile(`(?m)^(\s*source_sha\s*=\s*")` + regexp.QuoteMeta(old) + `"`)
		if !re.MatchString(c) {
			return "", fmt.Errorf("%s: entry %s has no source_sha %s", sourcesPath, path, old)
		}
		chunks[i] = re.ReplaceAllString(c, `${1}`+sha+`"`)
		return strings.Join(chunks, ""), nil
	}
	return "", fmt.Errorf("%s: no entry for %s", sourcesPath, path)
}

// editSources drops the entries of removed and appends a `pin = "<key>"` entry for every added
// path that has none.
func editSources(text, key string, added, removed []string) string {
	gone := map[string]bool{}
	for _, r := range removed {
		gone[r] = true
	}
	listed := map[string]bool{}
	var b strings.Builder
	for i, c := range sourcesEntries(text) {
		p := entryPath(c)
		if i > 0 && gone[p] {
			continue
		}
		listed[p] = true
		b.WriteString(c)
	}
	out := b.String()
	sort.Strings(added)
	for _, a := range added {
		if listed[a] {
			continue
		}
		if !strings.HasSuffix(out, "\n") && out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("\n[[file]]\npath = %q\npin = %q\n", a, key)
	}
	return out
}

// upgradeInstalled runs `lighthouse init --upgrade --yes` in wt from wt/pack: the embedded
// packages would install the text from before the merge.
func upgradeInstalled(wt string) error {
	var out, errOut bytes.Buffer
	code := initcmd.Run(initcmd.Options{
		Dir: wt, Upgrade: true, Yes: true, Packs: os.DirFS(filepath.Join(wt, "pack")),
		In: strings.NewReader(""), Out: &out, Err: &errOut,
	})
	if code != 0 {
		return fmt.Errorf("init --upgrade: exit %d: %s", code, strings.TrimSpace(errOut.String()+"\n"+out.String()))
	}
	return nil
}
