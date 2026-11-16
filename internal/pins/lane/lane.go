package lane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

// Env is what one lane run needs.
type Env struct {
	// Root is a checkout of origin's default-branch head: lighthouse.toml and pack/ are read
	// here, and MergeSource reads its "ours" from here.
	Root     string
	Git      pins.GitSource
	Releases forge.ReleaseReader
	// Forge is the repository the lane writes its issues and draft PRs to. Run never merges.
	Forge forge.Forge
	// Log gets one line per pin; nil discards.
	Log io.Writer
}

// Run measures every pin upstream and writes what the outcome asks: a draft PR for a skill or
// file pin whose merge is clean, a conflict issue when it is not (or when it reaches
// .github/workflows/), a behind issue for a plugin or tool, and an error issue for a pin that
// cannot be measured or merged. Decide keeps one open item per pin per track. A failing pin does
// not stop the others; Run returns their errors joined, each naming its pin, and an error-track
// pin counts as failed even when its issue was written.
func Run(ctx context.Context, env Env) error {
	if env.Forge == nil {
		return errors.New("lane: no forge")
	}
	if env.Log == nil {
		env.Log = io.Discard
	}
	r := &runner{env: env, sources: map[string]pins.SourcePin{}, tools: map[string]pins.Pin{}}
	// Check reports a SOURCES.toml or lighthouse.toml that does not load as an error pin of its
	// own, so these lookups only need what did load.
	if srcs, err := pins.FindSourcePins(env.Root); err == nil {
		for _, sp := range srcs {
			r.sources[sp.Key] = sp
		}
	}
	if tps, err := pins.LoadPins(filepath.Join(env.Root, "lighthouse.toml")); err == nil {
		for _, p := range tps {
			r.tools[p.Name] = p
		}
	}
	rs := pins.Check(ctx, pins.Env{Root: env.Root, Git: env.Git, Releases: env.Releases},
		pins.CheckOptions{Upstream: true})
	var errs []error
	for _, pr := range rs {
		if err := r.pin(ctx, pr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type runner struct {
	env     Env
	sources map[string]pins.SourcePin
	tools   map[string]pins.Pin
	base    string // origin's default branch, once asked
}

// pin handles one measured pin.
func (r *runner) pin(ctx context.Context, pr pins.PinResult) error {
	res := pr.Upstream
	if res == nil {
		return nil
	}
	key := pr.Key
	switch res.Status {
	case pins.StatusCurrent:
		return r.apply(ctx, key, Outcome{Target: res.Target, Measurable: true}, ItemContent{}, "current")
	case pins.StatusError:
		return r.fail(ctx, key, res.Detail)
	case pins.StatusBehind:
		if sp, ok := r.sources[key]; ok {
			return r.source(ctx, sp)
		}
		if p, ok := r.tools[key]; ok {
			return r.release(ctx, p, *res)
		}
		return r.fail(ctx, key, "behind upstream, but the lane knows no pin "+key)
	default:
		return r.fail(ctx, key, fmt.Sprintf("unexpected upstream status %q", res.Status))
	}
}

// fail puts the pin on the error track and returns its error, so the run ends red as well.
func (r *runner) fail(ctx context.Context, key, detail string) error {
	content := ItemContent{
		Title: "pins: " + key + " is niet te meten",
		Body:  RenderErrorBody(Marker(key, KindError, ""), key, detail),
	}
	err := fmt.Errorf("pin %s: %s", key, detail)
	return errors.Join(err, r.apply(ctx, key, Outcome{Target: detail}, content, "error"))
}

// release writes the behind issue of a plugin or tool pin.
func (r *runner) release(ctx context.Context, p pins.Pin, res pins.Result) error {
	compare := fmt.Sprintf("https://github.com/%s/compare/%s%s...%s%s", p.Repo, p.TagPrefix, p.Version, p.TagPrefix, res.Target)
	content := ItemContent{
		Title: fmt.Sprintf("pins: %s %s staat achter upstream %s", p.Name, p.Version, res.Target),
		Body:  RenderBehindBody(Marker(p.Name, KindBehind, res.Target), p.Name, p.Version, res.Target, res.Releases, compare),
	}
	return r.apply(ctx, p.Name, Outcome{Kind: KindBehind, Target: res.Target, Measurable: true}, content, "behind")
}

// source merges a skill or file pin that is behind. A clean merge becomes a draft PR on
// pins/<key>, pushed only when Decide asks for a new or updated PR; a conflict, or a result that
// reaches .github/workflows/, becomes a conflict issue; a merge that cannot run is an error.
func (r *runner) source(ctx context.Context, sp pins.SourcePin) error {
	cl, err := r.env.Git.Clone(ctx, sp.Repo)
	if err != nil {
		return r.fail(ctx, sp.Key, err.Error())
	}
	defer func() { _ = cl.Close() }()
	head, err := cl.Head()
	if err != nil {
		return r.fail(ctx, sp.Key, err.Error())
	}
	commits, err := cl.PathLog(sp.Commit, sp.Path)
	if err != nil {
		return r.fail(ctx, sp.Key, err.Error())
	}
	mr, err := MergeSource(ctx, r.env.Root, sp, cl)
	if err != nil {
		return r.fail(ctx, sp.Key, err.Error())
	}
	compare := fmt.Sprintf("https://github.com/%s/compare/%s...%s", sp.Repo, sp.Commit, head)
	conflict := func(reason string) error {
		content := ItemContent{
			Title: fmt.Sprintf("pins: %s botst met upstream %s", sp.Key, head[:min(7, len(head))]),
			Body:  RenderConflictBody(Marker(sp.Key, KindConflict, head), sp.Key, mr.Conflicts, reason, commits, compare),
		}
		return r.apply(ctx, sp.Key, Outcome{Kind: KindConflict, Target: head, Measurable: true}, content, "conflict")
	}
	if !mr.Clean {
		return conflict("De 3-way-merge met upstream conflicteert.")
	}

	found, err := r.find(ctx, sp.Key)
	if err != nil {
		return err
	}
	out := Outcome{Kind: KindPR, Target: head, Measurable: true}
	actions := Decide(sp.Key, out, found)
	if !writesPR(actions) {
		return r.applyActions(ctx, sp.Key, actions, ItemContent{}, "pr")
	}
	branch, err := PrepareBranch(ctx, r.env.Root, sp, mr, head)
	switch {
	case errors.Is(err, ErrTouchesWorkflows): // also when mr.TouchesWorkflows
		return conflict("De wijziging raakt " + workflowsDir + " en daarheen pusht de lane niet: " + err.Error())
	case err != nil:
		return fmt.Errorf("pin %s: %w", sp.Key, err)
	}
	stat, err := r.diffStat(ctx, branch)
	if err != nil {
		return fmt.Errorf("pin %s: %w", sp.Key, err)
	}
	base, err := r.defaultBranch(ctx)
	if err != nil {
		return fmt.Errorf("pin %s: %w", sp.Key, err)
	}
	content := ItemContent{
		Title:  fmt.Sprintf("pins: %s volgt upstream tot %s", sp.Key, head[:7]),
		Body:   RenderPRBody(Marker(sp.Key, KindPR, head), sp.Key, commits, stat, compare),
		Branch: branch,
		Base:   base,
	}
	return r.applyActions(ctx, sp.Key, actions, content, "pr")
}

// writesPR reports whether actions create or update a PR, which needs the branch pushed first.
func writesPR(actions []Action) bool {
	for _, a := range actions {
		if (a.Op == Create || a.Op == Update) && a.Kind == KindPR {
			return true
		}
	}
	return false
}

// diffStat is the --stat of the one commit PrepareBranch pushed to branch.
func (r *runner) diffStat(ctx context.Context, branch string) (string, error) {
	out, err := laneGit(ctx, r.env.Root, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(string(out), "\t")
	if !shaRe.MatchString(sha) {
		return "", fmt.Errorf("origin has no %s after the push: %q", branch, out)
	}
	stat, err := laneGit(ctx, r.env.Root, "diff", "--stat=100", sha+"^", sha)
	return string(stat), err
}

func (r *runner) defaultBranch(ctx context.Context) (string, error) {
	if r.base == "" {
		b, err := defaultBranch(ctx, r.env.Root)
		if err != nil {
			return "", err
		}
		r.base = b
	}
	return r.base, nil
}

// find lists the items of key, open and closed.
func (r *runner) find(ctx context.Context, key string) ([]forge.Item, error) {
	found, err := r.env.Forge.FindByMarker(ctx, markerPrefix+escape(key)+" ")
	if err != nil {
		return nil, fmt.Errorf("pin %s: find items: %w", key, err)
	}
	return found, nil
}

// apply finds the items of key, decides and applies with content.
func (r *runner) apply(ctx context.Context, key string, out Outcome, content ItemContent, label string) error {
	found, err := r.find(ctx, key)
	if err != nil {
		return err
	}
	return r.applyActions(ctx, key, Decide(key, out, found), content, label)
}

func (r *runner) applyActions(ctx context.Context, key string, actions []Action, content ItemContent, label string) error {
	_, _ = fmt.Fprintf(r.env.Log, "%s: %s, %s\n", key, label, describe(actions))
	return Applier{F: r.env.Forge}.Apply(ctx, key, actions, content)
}

// describe names the actions for the log, such as "create pr, close #12".
func describe(actions []Action) string {
	if len(actions) == 0 {
		return "nothing to do"
	}
	var parts []string
	for _, a := range actions {
		switch a.Op {
		case Create:
			parts = append(parts, "create "+string(a.Kind))
		case Update:
			parts = append(parts, fmt.Sprintf("update #%d", a.Item.Number))
		case CloseByLane:
			parts = append(parts, fmt.Sprintf("close #%d", a.Item.Number))
		}
	}
	return strings.Join(parts, ", ")
}
