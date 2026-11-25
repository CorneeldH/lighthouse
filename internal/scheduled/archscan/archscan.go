// Package archscan is the scheduled job architecture-scan: a periodic, unattended scan of the
// architecture of a repository that leaves one decision issue. This file holds the registration,
// the gate, the launch, the staging of the corpus, the checks and the publishing of the result.
package archscan

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	lhconfig "github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/safefs"
)

// Name is the job name a [[schedule]] block names in `job`.
const Name = "architecture-scan"

func init() { scheduled.Register(New()) }

// Job is the architecture-scan job. The registered instance has no params: Configure returns
// the instance for one schedule.
type Job struct {
	cfg   config
	bound bool
}

var (
	_ scheduled.Job          = (*Job)(nil)
	_ scheduled.Configurable = (*Job)(nil)
)

// New returns the unconfigured job, as the registry holds it.
func New() scheduled.Job { return &Job{cfg: defaults()} }

// Configure implements scheduled.Configurable: it validates the params of s.
func (j *Job) Configure(s repoconfig.Schedule) (scheduled.Job, error) {
	c, err := parse(s.Params)
	if err != nil {
		return nil, err
	}
	return &Job{cfg: c, bound: true}, nil
}

// Name implements scheduled.Job.
func (*Job) Name() string { return Name }

var errUnbound = errors.New("archscan: job is not configured for a schedule (scheduled.Bind)")

// maxBody is the limit, in characters, of the body of the scan issue, marker and warning
// included: under the 65536 of GitHub.
const maxBody = 60000

// skipNoCommits is the skip of a slot whose origin/<default> is the base of the open scan issue.
const skipNoCommits = "no commits since last scan"

func defaultBranch(env scheduled.Env) string {
	if env.Repo.DefaultBranch == "" {
		return "main"
	}
	return env.Repo.DefaultBranch
}

// Gate implements scheduled.Job: after a fetch, HEAD of origin/<default> against the base in the
// marker of the open scan issue decides. Every failure is an error, never a skip: the tick then
// keeps the slot and the next tick tries again.
func (j *Job) Gate(ctx context.Context, env scheduled.Env) (scheduled.Verdict, error) {
	if !j.bound {
		return scheduled.Verdict{}, errUnbound
	}
	if env.Forge == nil {
		return scheduled.Verdict{}, fmt.Errorf("%s: the repo has no forge", Name)
	}
	label := env.Repo.Labels.NeedsDecision
	if label == "" {
		return scheduled.Verdict{}, fmt.Errorf("%s: labels.needs_decision is not set", Name)
	}
	branch := defaultBranch(env)
	if _, err := git(ctx, env, env.Repo.Dir, fetchLimit, "fetch", "--quiet", "origin", branch); err != nil {
		return scheduled.Verdict{}, err
	}
	head, err := revParse(ctx, env, env.Repo.Dir, "origin/"+branch)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	open, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "open")
	if err != nil {
		return scheduled.Verdict{}, fmt.Errorf("%s: %w", Name, err)
	}
	if len(open) > 0 {
		if m, ok := scheduled.ParseMarker(open[0].Body); ok && m.Base == head {
			return scheduled.Verdict{Skip: skipNoCommits}, nil
		}
	}
	return scheduled.Verdict{RunID: runIDAt(env.Now)}, nil
}

// Launch implements scheduled.Job.
func (j *Job) Launch(scheduled.Verdict, scheduled.Env) (scheduled.LaunchPlan, error) {
	if !j.bound {
		return scheduled.LaunchPlan{}, errUnbound
	}
	return scheduled.LaunchPlan{
		Command:      j.cfg.command + " --corpus " + corpusPath + " --out " + resultPath,
		Instructions: instructions,
		Tools:        slices.Clone(j.cfg.tools),
		MaxRuntime:   j.cfg.maxRuntime,
	}, nil
}

const instructions = "You are running unattended from a schedule. There is no human to answer questions: ask none.\n" +
	"Read the issues, the hotspots and the earlier scans only from the corpus file you get with --corpus (`" + corpusPath + "`), " +
	"not from the forge.\n" +
	"Write exactly one file, at the path you get with --out (`" + resultPath + "`). Create no issues. " +
	"Do NOT create a git commit and do NOT push: the controller checks the result and publishes it itself.\n" +
	"A file anywhere else, or a symlink, fails the run.\n"

// Stage implements scheduled.Job: it writes the corpus of the workplace into the staging
// directory.
func (j *Job) Stage(ctx context.Context, env scheduled.Env, _ scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	c, err := j.buildCorpus(ctx, env)
	if err != nil {
		return err
	}
	b, err := fit(c, maxCorpus)
	if err != nil {
		return err
	}
	// Look before creating: MkdirAll would follow a symlink among the existing parents.
	for _, rel := range []string{".lighthouse", stagingDir} {
		_, err := safefs.Stat(env.Dir, rel)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(filepath.Join(env.Dir, filepath.FromSlash(rel)), 0o755); err != nil {
				return safefs.FileErr(rel, err)
			}
		} else if err != nil {
			return err
		}
	}
	return safefs.WriteFresh(filepath.Join(env.Dir, filepath.FromSlash(corpusPath)), corpusPath, b)
}

// runID is the run id of the open run row of env.Schedule.Name in env.History, "" when there is
// none. Cleanup gets no Verdict, so it reads the id the gate recorded there.
func runID(env scheduled.Env) string {
	id := ""
	for _, row := range env.History {
		if !row.Open || row.Schedule != env.Schedule.Name {
			continue
		}
		if rec, err := scheduled.DecodeRecord(row.Verdict); err == nil {
			id = rec.Verdict.RunID
		}
	}
	return id
}

// changedPaths are the paths of dir that git status shows against HEAD: tracked changes,
// deletions included, and untracked files that are not ignored.
func changedPaths(ctx context.Context, env scheduled.Env, dir string) ([]string, error) {
	diff, err := git(ctx, env, dir, gitLimit, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	others, err := git(ctx, env, dir, gitLimit, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(diff+others, "\x00") {
		if p != "" && !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// readResult reads the result file of dir; a symlink or anything but a regular file is an error.
func readResult(dir string) ([]byte, error) {
	info, err := safefs.Stat(dir, resultPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: is not a regular file", resultPath)
	}
	b, err := os.ReadFile(filepath.Join(dir, resultPath))
	if err != nil {
		return nil, safefs.FileErr(resultPath, err)
	}
	return b, nil
}

// resultShown is nil when git status shows a non-blank result file in dir.
func resultShown(ctx context.Context, env scheduled.Env, dir string) error {
	paths, err := changedPaths(ctx, env, dir)
	if err != nil {
		return err
	}
	if !slices.Contains(paths, resultPath) {
		return fmt.Errorf("%s: git status does not show it (missing or ignored)", resultPath)
	}
	b, err := readResult(dir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) == "" {
		return fmt.Errorf("%s: is empty", resultPath)
	}
	return nil
}

// Cleanup implements scheduled.Job: it removes the corpus, and fails the run when git status
// does not show a non-blank result file (Postcheck would else skip Check and end the run
// done). A missing result is fine only when the open scan issue already carries the run id of
// this run: an earlier Postcheck of the run published and removed it.
func (j *Job) Cleanup(ctx context.Context, env scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	if err := safefs.UnlinkSymlinks(dir, stagingDir); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(corpusPath))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return safefs.FileErr(corpusPath, err)
	}
	// Leave what Stage made no trace; a directory the repo tracks is not empty and stays.
	_ = os.Remove(filepath.Join(dir, filepath.FromSlash(stagingDir)))
	_ = os.Remove(filepath.Join(dir, ".lighthouse"))
	missing := resultShown(ctx, env, dir)
	if missing == nil {
		return nil
	}
	published, err := publishedBy(ctx, env, runID(env))
	if err != nil {
		return fmt.Errorf("%w; the scan issue could not be read: %w", missing, err)
	}
	if published {
		return nil
	}
	return missing
}

// publishedBy reports whether the open scan issue carries run in its marker.
func publishedBy(ctx context.Context, env scheduled.Env, run string) (bool, error) {
	if run == "" {
		return false, nil
	}
	if env.Forge == nil {
		return false, fmt.Errorf("%s: the repo has no forge", Name)
	}
	open, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, env.Repo.Labels.NeedsDecision, "open")
	if err != nil {
		return false, err
	}
	if len(open) == 0 {
		return false, nil
	}
	m, _ := scheduled.ParseMarker(open[0].Body)
	return m.Run == run, nil
}

// Allowed implements scheduled.Job: the run may write the result file and nothing else.
func (*Job) Allowed(path string) bool { return path == resultPath }

// loadHeadings is the headings table of the target repository in env.Dir, never of the
// process cwd.
func loadHeadings(env scheduled.Env) (headings, error) {
	h, err := lhconfig.LoadHeadingsFile(filepath.Join(env.Dir, ".lighthouse", "headings.toml"))
	if err != nil {
		return nil, fmt.Errorf("headings: %w", err)
	}
	return h, nil
}

// reference is the live scan issue the ticks of a result are held against: the oldest open
// scan issue, or without one the last closed one. found is false when there is neither.
type reference struct {
	issue       forge.Issue
	open, found bool
	openNumbers []int
}

// liveReference reads the reference scan issue from the forge, its body fetched again.
func liveReference(ctx context.Context, env scheduled.Env) (reference, error) {
	var ref reference
	label := env.Repo.Labels.NeedsDecision
	open, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "open")
	if err != nil {
		return ref, err
	}
	for _, is := range open {
		ref.openNumbers = append(ref.openNumbers, is.Number)
	}
	var pick forge.Issue
	if len(open) > 0 {
		pick, ref.open = open[0], true
	} else {
		closed, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "closed")
		if err != nil {
			return ref, err
		}
		if len(closed) == 0 {
			return ref, nil
		}
		pick = newestClosed(closed)
	}
	live, err := env.Forge.Issue(ctx, pick.Number)
	if err != nil {
		return ref, fmt.Errorf("read scan issue #%d: %w", pick.Number, err)
	}
	ref.issue, ref.found = live, true
	return ref, nil
}

// Check implements scheduled.Job. Its authority is the work tree and the forge, never the
// corpus: the result is the only changed path, parses against the headings of the repo, holds
// no secret, names only open issues other than the scan issue under still open, keeps every
// tick of the live reference scan issue, and fits the body limit with marker and warning.
func (j *Job) Check(ctx context.Context, env scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	if env.Forge == nil {
		return fmt.Errorf("%s: the repo has no forge", Name)
	}
	paths, err := changedPaths(ctx, env, dir)
	if err != nil {
		return err
	}
	if len(paths) != 1 || paths[0] != resultPath {
		return fmt.Errorf("want exactly one file, %s; changed: %s", resultPath, strings.Join(paths, ", "))
	}
	// The secret scan after Finish never sees this file: it is not committed.
	if findings := scheduled.ScanSecrets(dir, []string{resultPath}); len(findings) > 0 {
		return fmt.Errorf("secret scan: %s", strings.Join(findings, "; "))
	}
	h, err := loadHeadings(env)
	if err != nil {
		return err
	}
	b, err := readResult(dir)
	if err != nil {
		return err
	}
	res, err := parseResult(b, h)
	if err != nil {
		return err
	}
	ref, err := liveReference(ctx, env)
	if err != nil {
		return err
	}
	var errs []error
	for _, nr := range res.StillOpen {
		if slices.Contains(ref.openNumbers, nr) {
			errs = append(errs, fmt.Errorf("still open: #%d is the scan issue", nr))
			continue
		}
		is, err := env.Forge.Issue(ctx, nr)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("still open: #%d: %w", nr, err))
		case is.State != "open":
			errs = append(errs, fmt.Errorf("still open: #%d is not open", nr))
		}
	}
	live := map[string]bool{}
	if ref.found {
		live = RejectedKeys(ref.issue.Body)
	}
	rejected := map[string]bool{}
	for _, c := range res.Candidates {
		rejected[c.Key] = c.Rejected
	}
	for _, key := range slices.Sorted(maps.Keys(live)) {
		if !rejected[key] {
			errs = append(errs, fmt.Errorf("candidate `%s` is rejected in #%d, but not in the result", key, ref.issue.Number))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	base, err := revParse(ctx, env, dir, "HEAD")
	if err != nil {
		return err
	}
	m := scheduled.Marker{Kind: Name, Version: 1, Run: cmp.Or(runID(env), runIDAt(env.Now)), Base: base, First: env.Now.Format(dateLayout), Scans: 1}
	content := res.Body
	if ref.open {
		prev, _ := scheduled.ParseMarker(ref.issue.Body)
		m.First, m.Scans = prev.First, prev.Scans+1
		content = res.WithCheckboxes(live)
	}
	warning, err := warningFunc(env, h)
	if err != nil {
		return err
	}
	body := m.String() + "\n" + warning(m.First, m.Scans) + "\n\n" + content
	if n := utf8.RuneCountInString(body); n > maxBody {
		return fmt.Errorf("the scan issue body would be %d characters, the limit is %d", n, maxBody)
	}
	return nil
}

// dateLayout is the layout of the dates in the marker and the headings of the issue.
const dateLayout = "2006-01-02"

// runIDLayout is the time layout of a run id after "scan-".
const runIDLayout = "20060102T1504"

func runIDAt(t time.Time) string { return "scan-" + t.UTC().Format(runIDLayout) }

// scanDate is the date of the scan of run id, else first.
func scanDate(run, first string) string {
	if t, err := time.Parse(runIDLayout, strings.TrimPrefix(run, "scan-")); err == nil {
		return t.Format(dateLayout)
	}
	return first
}

// warningFunc is line 2 of the body: empty on a first scan, then
// "<scan_unresolved> <first>, <n> scans" in the repo language.
func warningFunc(env scheduled.Env, h headings) (func(first string, scans int) string, error) {
	since, err := h.Heading("scan_unresolved", env.Repo.Language)
	if err != nil {
		return nil, err
	}
	return func(first string, scans int) string {
		if scans < 2 {
			return ""
		}
		return fmt.Sprintf("%s %s, %d scans", since, first, scans)
	}, nil
}

// removeResult removes the result file of dir; a missing file is fine.
func removeResult(dir string) error {
	if err := os.Remove(filepath.Join(dir, resultPath)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return safefs.FileErr(resultPath, err)
	}
	return nil
}

// Finish implements scheduled.Job: it publishes the checked result through
// scheduled.PublishDecisionIssue and removes the result file on every route, so Postcheck has
// nothing to commit and the run ends done without a PR. It posts nothing when the open scan
// issue already carries this run, or when there is neither an open scan issue nor a candidate
// without a tick. On an update the helper re-reads the live body: a box ticked there after
// Check stays ticked.
func (j *Job) Finish(ctx context.Context, env scheduled.Env, dir string, v scheduled.Verdict) (err error) {
	defer func() {
		if rmErr := removeResult(dir); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	if !j.bound {
		return errUnbound
	}
	if env.Forge == nil {
		return fmt.Errorf("%s: the repo has no forge", Name)
	}
	if v.RunID == "" {
		return fmt.Errorf("%s: the verdict has no run id", Name)
	}
	label := env.Repo.Labels.NeedsDecision
	if label == "" {
		return fmt.Errorf("%s: labels.needs_decision is not set", Name)
	}
	open, err := scheduled.FindDecisionIssue(ctx, env.Forge, Name, label, "open")
	if err != nil {
		return err
	}
	var prev scheduled.Marker
	if len(open) > 0 {
		prev, _ = scheduled.ParseMarker(open[0].Body)
		if prev.Run == v.RunID {
			return nil
		}
	}
	h, err := loadHeadings(env)
	if err != nil {
		return err
	}
	b, err := readResult(dir)
	if err != nil {
		return err
	}
	res, err := parseResult(b, h)
	if err != nil {
		return err
	}
	if len(open) == 0 && !slices.ContainsFunc(res.Candidates, func(c Candidate) bool { return !c.Rejected }) {
		return nil
	}
	base, err := revParse(ctx, env, dir, "HEAD")
	if err != nil {
		return err
	}
	warning, err := warningFunc(env, h)
	if err != nil {
		return err
	}
	candidates, err := h.Heading("scan_candidates", env.Repo.Language)
	if err != nil {
		return err
	}
	scanOf, err := h.Heading("scan_of", env.Repo.Language)
	if err != nil {
		return err
	}
	_, err = scheduled.PublishDecisionIssue(ctx, env.Forge, scheduled.Publish{
		Kind:         Name,
		Title:        Name + ": " + candidates,
		Label:        label,
		ExtraLabels:  slices.Clone(j.cfg.labels),
		Warning:      warning,
		Run:          v.RunID,
		Base:         base,
		Now:          env.Now,
		Body:         res.Body,
		MaxBody:      maxBody,
		Merge:        func(live string) string { return res.WithCheckboxes(RejectedKeys(live)) },
		CommentTitle: scanOf + " " + scanDate(prev.Run, prev.First),
	})
	return err
}
