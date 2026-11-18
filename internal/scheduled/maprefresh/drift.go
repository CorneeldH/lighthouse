package maprefresh

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	modeFast = "fast"
	modeFull = "full"
)

// drift is the measure of how far the code moved since the map was last committed.
type drift struct {
	Base    string // last commit that touched map_dir, "" when the repo was never mapped
	Changed int    // changed files under drift_paths since Base
	NewDirs int    // directories under drift_paths that Base lacks
	Score   int
}

// route maps a score onto "" (skip), fast or full: below low is skip, up to and including high is
// fast, above is full.
func route(score, low, high int) string {
	switch {
	case score < low:
		return ""
	case score <= high:
		return modeFast
	default:
		return modeFull
	}
}

const gitTimeout = 2 * time.Minute

// git runs `git -C dir <args>` through run. Output is returned untrimmed. An error names the
// subcommand and the exit code, never stderr or a path of this host.
func git(ctx context.Context, run workplace.Runner, dir string, timeout time.Duration, args ...string) (string, error) {
	full := append([]string{"-C", dir, "--literal-pathspecs", "--no-optional-locks"}, args...)
	out, code, err := run(ctx, timeout, "git", full...)
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.ReplaceAll(cause(err).Error(), dir, "<repo>"))
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", args[0], code)
	}
	return out, nil
}

// nul splits the -z output of git into its non-empty fields.
func nul(out string) []string {
	var fields []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			fields = append(fields, f)
		}
	}
	return fields
}

// measure measures the drift of origin/<default> in the repo dir: after a fetch, so the answer is
// about the remote and not about a stale tracking branch.
func measure(ctx context.Context, run workplace.Runner, dir, defaultBranch string, c config) (drift, error) {
	if _, err := git(ctx, run, dir, 5*time.Minute, "fetch", "--quiet", "origin", defaultBranch); err != nil {
		return drift{}, err
	}
	ref := "origin/" + defaultBranch
	if _, err := git(ctx, run, dir, gitTimeout, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return drift{}, err
	}
	base, err := git(ctx, run, dir, gitTimeout, "rev-list", "-1", ref, "--", c.mapDir)
	if err != nil {
		return drift{}, err
	}
	base = strings.TrimSpace(base)
	if base == "" {
		// Never mapped: more than any threshold, so the first run is the full map.
		return drift{Score: c.driftHigh + 1}, nil
	}
	diff, err := git(ctx, run, dir, gitTimeout, append([]string{"diff", "--name-only", "-z", base, ref, "--"}, c.driftPaths...)...)
	if err != nil {
		return drift{}, err
	}
	d := drift{Base: base, Changed: len(nul(diff))}
	before, err := sourceDirs(ctx, run, dir, base, c.driftPaths)
	if err != nil {
		return drift{}, err
	}
	after, err := sourceDirs(ctx, run, dir, ref, c.driftPaths)
	if err != nil {
		return drift{}, err
	}
	for p := range after {
		if !before[p] {
			d.NewDirs++
		}
	}
	d.Score = d.Changed + c.newDirWeight*d.NewDirs
	return d, nil
}

// sourceDirs is every directory that holds a tracked file under paths at ref.
func sourceDirs(ctx context.Context, run workplace.Runner, dir, ref string, paths []string) (map[string]bool, error) {
	out, err := git(ctx, run, dir, gitTimeout, append([]string{"ls-tree", "-r", "--name-only", "-z", ref, "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{}
	for _, f := range nul(out) {
		dirs[path.Dir(f)] = true
	}
	return dirs, nil
}
