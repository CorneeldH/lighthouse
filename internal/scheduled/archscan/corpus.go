package archscan

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	lhconfig "github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/safefs"
)

// The corpus is input for the agent and never authority: the agent can edit it, so nothing
// after the session reads it. Check and Finish read the forge again.

const (
	corpusPath = ".lighthouse/run/architecture-scan-corpus.json"
	resultPath = "architecture-scan-result.md"
	stagingDir = ".lighthouse/run"
	// maxIssueBody is the cut, in characters, of the body of every issue but the scan issues.
	maxIssueBody = 2000
	// maxCorpus is the limit of the whole corpus in bytes, as max_corpus_bytes of reflect.
	maxCorpus = 400000
	// maxHotspots caps the hotspots of the corpus, highest Changes first.
	maxHotspots = 200
	gitLimit    = 2 * time.Minute
	fetchLimit  = 5 * time.Minute
)

// Hotspot is one file and how many commits changed it within the window.
type Hotspot struct {
	Path    string `json:"path"`
	Changes int    `json:"changes"`
}

// corpusScan is a scan issue: its body (with the check marks) and its comments, oldest first.
type corpusScan struct {
	Number   int      `json:"number"`
	Body     string   `json:"body"`
	Comments []string `json:"comments"`
}

type corpusIssue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
	Body   string   `json:"body"`
}

// corpus is what the headless skill reads instead of the forge and git.
type corpus struct {
	Language string `json:"language"`
	// SearchDirs is paths.search_dirs, the paths the hotspots are counted over.
	SearchDirs []string `json:"search_dirs"`
	// Paths are the map documents of [paths] (tenets, areas, strategy) that exist in the work tree.
	Paths      []string      `json:"paths"`
	Hotspots   []Hotspot     `json:"hotspots"`
	Base       string        `json:"base"`
	Issues     []corpusIssue `json:"issues"`
	Open       *corpusScan   `json:"open_scan"`
	LastClosed *corpusScan   `json:"last_closed_scan"`
}

// git runs `git -C dir <args>` through env.Run, without hooks or fsmonitor. Output is returned
// untrimmed. An error names the subcommand and the exit code, never stderr or a path of this host.
func git(ctx context.Context, env scheduled.Env, dir string, limit time.Duration, args ...string) (string, error) {
	out, code, err := env.Run(ctx, limit, "git",
		append([]string{"-C", dir, "--literal-pathspecs", "--no-optional-locks", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false"}, args...)...)
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.ReplaceAll(safefs.Cause(err).Error(), dir, "<repo>"))
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", args[0], code)
	}
	return out, nil
}

// revParse is the commit ref names in dir.
func revParse(ctx context.Context, env scheduled.Env, dir, ref string) (string, error) {
	out, err := git(ctx, env, dir, gitLimit, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// hotspots counts, per file under paths, the commits within window (a git date such as
// "3 months") that changed it: most changes first, then by path.
func hotspots(ctx context.Context, env scheduled.Env, dir, window string, paths []string) ([]Hotspot, error) {
	args := []string{"-c", "core.quotePath=false", "log", "-z", "--since=" + window, "--no-renames", "--name-only", "--format=", "HEAD", "--"}
	out, err := git(ctx, env, dir, gitLimit, append(args, paths...)...)
	if err != nil {
		return nil, err
	}
	count := map[string]int{}
	// -z separates the paths by NUL and leaves them unquoted; the empty format adds newlines.
	for _, line := range strings.Split(out, "\x00") {
		if p := strings.Trim(line, "\r\n"); p != "" {
			count[p]++
		}
	}
	spots := make([]Hotspot, 0, len(count))
	for p, n := range count {
		spots = append(spots, Hotspot{Path: p, Changes: n})
	}
	slices.SortFunc(spots, func(a, b Hotspot) int {
		return cmp.Or(cmp.Compare(b.Changes, a.Changes), strings.Compare(a.Path, b.Path))
	})
	if len(spots) > maxHotspots {
		spots = spots[:maxHotspots]
	}
	return spots, nil
}

// searchDirs is paths.search_dirs of the lighthouse.toml in dir, its default when not set.
// repoconfig.Paths does not carry the key, so it is read here.
func searchDirs(dir string) ([]string, error) {
	c, err := lhconfig.Load(filepath.Join(dir, "lighthouse.toml"))
	if err != nil {
		return nil, fmt.Errorf("lighthouse.toml: %w", err)
	}
	v, err := lhconfig.Lookup(c, "paths.search_dirs")
	if err != nil {
		return nil, fmt.Errorf("paths.search_dirs: %w", err)
	}
	list, ok := strList(v)
	if !ok || len(list) == 0 {
		return nil, errors.New("paths.search_dirs: must be a non-empty list of strings")
	}
	for _, d := range list {
		if d != "." && (filepath.Clean(d) != d || !filepath.IsLocal(d)) {
			return nil, fmt.Errorf("paths.search_dirs: %s is not a clean relative path in the repo", scheduled.ShowPath(d))
		}
	}
	return list, nil
}

// cut keeps the first n characters of s; a cut body ends in an ellipsis within those n.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string([]rune(s)[:n-1]) + "…"
}

// scanOf reads the comments of issue is for the corpus.
func scanOf(ctx context.Context, f forge.Forge, is forge.Issue) (*corpusScan, error) {
	cs, err := f.Comments(ctx, is.Number)
	if err != nil {
		return nil, fmt.Errorf("comments of #%d: %w", is.Number, err)
	}
	s := &corpusScan{Number: is.Number, Body: is.Body, Comments: []string{}}
	for _, c := range cs {
		s.Comments = append(s.Comments, c.Body)
	}
	return s, nil
}

// newestClosed is the scan issue of closed that closed last; closed is not empty.
func newestClosed(closed []forge.Issue) forge.Issue {
	return slices.MaxFunc(closed, func(a, b forge.Issue) int {
		return cmp.Or(a.ClosedAt.Compare(b.ClosedAt), cmp.Compare(a.Number, b.Number))
	})
}

// buildCorpus reads the forge and the work tree in env.Dir into the corpus.
func (j *Job) buildCorpus(ctx context.Context, env scheduled.Env) (corpus, error) {
	c := corpus{Language: env.Repo.Language, Paths: []string{}, Issues: []corpusIssue{}}
	if env.Forge == nil {
		return c, fmt.Errorf("%s: the repo has no forge", Name)
	}
	dirs, err := searchDirs(env.Dir)
	if err != nil {
		return c, err
	}
	c.SearchDirs = dirs
	for _, p := range []string{env.Repo.Paths.Tenets, env.Repo.Paths.Areas, env.Repo.Paths.Strategy} {
		if p == "" {
			continue
		}
		if _, err := safefs.Stat(env.Dir, p); err == nil {
			c.Paths = append(c.Paths, p)
		}
	}
	if c.Base, err = revParse(ctx, env, env.Dir, "HEAD"); err != nil {
		return c, err
	}
	if c.Hotspots, err = hotspots(ctx, env, env.Dir, j.cfg.window, dirs); err != nil {
		return c, err
	}
	label := env.Repo.Labels.NeedsMind
	open, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "open")
	if err != nil {
		return c, err
	}
	scans := map[int]bool{}
	for _, is := range open {
		scans[is.Number] = true
	}
	if len(open) > 0 {
		if c.Open, err = scanOf(ctx, env.Forge, open[0]); err != nil {
			return c, err
		}
	}
	closed, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "closed")
	if err != nil {
		return c, err
	}
	if len(closed) > 0 {
		if c.LastClosed, err = scanOf(ctx, env.Forge, newestClosed(closed)); err != nil {
			return c, err
		}
	}
	issues, err := env.Forge.ListIssues(ctx, forge.IssueFilter{State: "open"})
	if err != nil {
		return c, fmt.Errorf("list open issues: %w", err)
	}
	for _, is := range issues {
		if scans[is.Number] {
			continue
		}
		c.Issues = append(c.Issues, corpusIssue{Number: is.Number, Title: is.Title,
			Labels: append([]string{}, is.Labels...), Body: cut(is.Body, maxIssueBody)})
	}
	return c, nil
}

// fit renders c within maxBytes. Over the limit the comments of the scan issues go first,
// oldest first (those of the last closed issue before those of the open one), then the other
// issue bodies are halved in turn. The scan issue bodies are never cut; a corpus that does not
// fit without them is an error.
func fit(c corpus, maxBytes int) ([]byte, error) {
	render := func() ([]byte, error) {
		b, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
	b, err := render()
	if err != nil {
		return nil, err
	}
	for _, s := range []*corpusScan{c.LastClosed, c.Open} {
		for s != nil && len(b) > maxBytes && len(s.Comments) > 0 {
			s.Comments = s.Comments[1:]
			if b, err = render(); err != nil {
				return nil, err
			}
		}
	}
	for limit := maxIssueBody / 2; len(b) > maxBytes; limit /= 2 {
		for i := range c.Issues {
			c.Issues[i].Body = cut(c.Issues[i].Body, limit)
		}
		if b, err = render(); err != nil {
			return nil, err
		}
		if limit == 0 {
			break
		}
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("corpus is %d bytes without comments and issue bodies, the limit is %d", len(b), maxBytes)
	}
	return b, nil
}
