package mapplanned

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/issuestate"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/safefs"
)

// Name is the job name a [[schedule]] block names in `job`.
const Name = "map-planned"

const (
	corpusPath = ".lighthouse/run/map-planned-corpus.json"
	resultPath = ".lighthouse/run/map-planned-result.json"
	maxRuntime = 30 * time.Minute
	gitLimit   = 2 * time.Minute
	fetchLimit = 5 * time.Minute
)

func init() { scheduled.Register(New()) }

// Job is the map-planned job: it puts the open issues of a planned label on the document that
// maps that label. The registered instance is unbound: Configure returns the instance for one
// schedule, and the unbound one refuses to run.
type Job struct {
	bound bool
}

var (
	_ scheduled.Job          = (*Job)(nil)
	_ scheduled.Configurable = (*Job)(nil)
)

// New returns the unconfigured job, as the registry holds it.
func New() scheduled.Job { return &Job{} }

var errUnbound = errors.New("map-planned: job is not configured for a schedule (scheduled.Bind)")

// Configure implements scheduled.Configurable. The job has no params, so it refuses every one:
// a key would otherwise be ignored silently.
func (*Job) Configure(s repoconfig.Schedule) (scheduled.Job, error) {
	if len(s.Params) > 0 {
		keys := slices.Sorted(maps.Keys(s.Params))
		for i, k := range keys {
			keys[i] = scheduled.ShowPath(k)
		}
		return nil, fmt.Errorf("params: job %s takes no params, got %s", Name, strings.Join(keys, ", "))
	}
	return &Job{bound: true}, nil
}

// Name implements scheduled.Job.
func (*Job) Name() string { return Name }

// dueIssue is one open issue the gate lets through, with the row that plans it and the trusted
// answer to the latest job question, if any.
type dueIssue struct {
	Row    Row
	Issue  forge.Issue
	Answer *Answer
}

// Gate implements scheduled.Job: after a fetch, the rows of the labels document on
// origin/<default> and their documents decide. Score is the number of issues due.
func (j *Job) Gate(ctx context.Context, env scheduled.Env) (scheduled.Verdict, error) {
	if !j.bound {
		return scheduled.Verdict{}, errUnbound
	}
	if env.Repo.Paths.Labels == "" {
		return scheduled.Verdict{Skip: skipNoDocuments}, nil
	}
	branch := defaultBranch(env)
	if _, err := git(ctx, env, env.Repo.Dir, fetchLimit, "fetch", "--quiet", "origin", branch); err != nil {
		return scheduled.Verdict{}, err
	}
	rows, err := plannedRows(ctx, env, env.Repo.Dir, "origin/"+branch)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	if len(rows) == 0 {
		return scheduled.Verdict{Skip: skipNoDocuments}, nil
	}
	due, err := j.dueOf(ctx, env, env.Repo.Dir, "origin/"+branch, rows)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	if len(due) == 0 {
		return scheduled.Verdict{Skip: "every planned issue is on its map"}, nil
	}
	issues := map[int]bool{}
	for _, d := range due {
		issues[d.Issue.Number] = true
	}
	return scheduled.Verdict{Score: len(issues)}, nil
}

const skipNoDocuments = "no planned documents"

// due are the open issues the gate lets through at ref in the repo dir, per row: Gate and Stage
// both use it. Without a labels document, or without a planned row, it is empty.
func (j *Job) due(ctx context.Context, env scheduled.Env, dir, ref string) ([]dueIssue, error) {
	if !j.bound {
		return nil, errUnbound
	}
	rows, err := plannedRows(ctx, env, dir, ref)
	if err != nil {
		return nil, err
	}
	return j.dueOf(ctx, env, dir, ref, rows)
}

// dueOf lists, per row, the open issues with its label (with a milestone when the row says so)
// that its document at ref does not name and whose job comments leave them unsettled and
// without an open question.
func (*Job) dueOf(ctx context.Context, env scheduled.Env, dir, ref string, rows []Row) ([]dueIssue, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	if env.Forge == nil || env.Trusted == nil {
		return nil, fmt.Errorf("%s: the repo has no forge", Name)
	}
	login, err := env.Forge.Login(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: login of the forge token: %w", Name, err)
	}
	type status struct {
		s Status
		a Answer
	}
	statuses := map[int]status{} // per issue number, read once across rows
	var due []dueIssue
	for _, row := range rows {
		mapped, err := documentIssues(ctx, env, dir, ref, row.Document)
		if err != nil {
			return nil, err
		}
		issues, err := env.Forge.ListIssues(ctx, forge.IssueFilter{State: "open", Label: row.Label})
		if err != nil {
			return nil, fmt.Errorf("%s: issues with label %s: %w", Name, row.Label, err)
		}
		for _, is := range issues {
			if mapped[is.Number] || (row.Planned == PlannedMilestone && is.Milestone == nil) {
				continue
			}
			st, seen := statuses[is.Number]
			if !seen {
				cs, err := env.Forge.Comments(ctx, is.Number)
				if err != nil {
					return nil, fmt.Errorf("%s: comments of #%d: %w", Name, is.Number, err)
				}
				s, a, err := StatusOf(ctx, cs, login, env.Trusted)
				if err != nil {
					return nil, fmt.Errorf("%s: #%d: %w", Name, is.Number, err)
				}
				st = status{s, a}
				statuses[is.Number] = st
			}
			switch st.s {
			case StatusNone:
				due = append(due, dueIssue{Row: row, Issue: is})
			case StatusAnswered:
				a := st.a
				due = append(due, dueIssue{Row: row, Issue: is, Answer: &a})
			case StatusOpen, StatusSettled:
			}
		}
	}
	return due, nil
}

// plannedRows are the rows with a Label, a Document and a Planned value of the labels document
// at ref. A labels document missing at ref is no rows; a ref that does not resolve is an error.
func plannedRows(ctx context.Context, env scheduled.Env, dir, ref string) ([]Row, error) {
	path := env.Repo.Paths.Labels
	if path == "" {
		return nil, nil
	}
	b, ok, err := show(ctx, env, dir, ref, path)
	if err != nil || !ok {
		return nil, err
	}
	all, err := Rows(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", scheduled.ShowPath(path), err)
	}
	var rows []Row
	for _, r := range all {
		if r.Label == "" {
			continue
		}
		// The path goes into git commands, the corpus and Check: only a clean path inside the repo.
		if pathpkg.Clean(r.Document) != r.Document || !filepath.IsLocal(r.Document) {
			return nil, fmt.Errorf("%s: label %s: document %s is not a clean relative path in the repo",
				scheduled.ShowPath(path), r.Label, scheduled.ShowPath(r.Document))
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// documentIssues are the issue numbers a document names at ref. A document missing at ref
// names none: every issue of its row is due, and the agent may write it.
func documentIssues(ctx context.Context, env scheduled.Env, dir, ref, path string) (map[int]bool, error) {
	b, ok, err := show(ctx, env, dir, ref, path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[int]bool{}, nil
	}
	return Issues(path, b)
}

// show reads path at ref. ok is false when ref has no such path; a ref that does not resolve
// to a commit is an error (git cat-file -e cannot tell the two apart).
func show(ctx context.Context, env scheduled.Env, dir, ref, path string) (b []byte, ok bool, err error) {
	if _, err := git(ctx, env, dir, gitLimit, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return nil, false, err
	}
	listed, err := git(ctx, env, dir, gitLimit, "ls-tree", ref, "--", path)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(listed) == "" {
		return nil, false, nil
	}
	out, err := git(ctx, env, dir, gitLimit, "show", ref+":"+path)
	if err != nil {
		return nil, false, err
	}
	return []byte(out), true, nil
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

// Launch implements scheduled.Job.
func (j *Job) Launch(scheduled.Verdict, scheduled.Env) (scheduled.LaunchPlan, error) {
	if !j.bound {
		return scheduled.LaunchPlan{}, errUnbound
	}
	return scheduled.LaunchPlan{
		Command:      "/lighthouse-map-planned --corpus " + corpusPath + " --result " + resultPath,
		Instructions: instructions,
		Tools:        []string{"Read", "Write", "Edit", "Glob", "Grep"},
		MaxRuntime:   maxRuntime,
	}, nil
}

const instructions = "You are running unattended from a schedule. There is no human to answer questions: " +
	"never ask one in this session; a question about an issue goes into the result file.\n" +
	"Read the issues from the corpus `" + corpusPath + "`, not from the forge.\n" +
	"Write only the documents the corpus names and the result file `" + resultPath + "`; " +
	"a file anywhere else fails the run. Do not create symlinks.\n" +
	"Do NOT create a git commit and do NOT push: the controller checks and commits the result itself.\n"

// Stage implements scheduled.Job: it writes the corpus of the issues due at HEAD of the
// workplace into the staging directory.
func (j *Job) Stage(ctx context.Context, env scheduled.Env, _ scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	rows, err := plannedRows(ctx, env, env.Dir, "HEAD")
	if err != nil {
		return err
	}
	due, err := j.dueOf(ctx, env, env.Dir, "HEAD", rows)
	if err != nil {
		return err
	}
	b, err := buildCorpus(env, rows, due)
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

const stagingDir = ".lighthouse/run"

// Cleanup implements scheduled.Job: it validates the result file as a whole, posts its
// questions and decisions (a wish question first gets labels.needs_decision), and removes the
// staging files. A missing result file is nothing to post. An issue whose job comments already
// hold an open question or a settled one gets no second comment, so a repeated Cleanup posts
// nothing twice. Nothing here reads the corpus.
func (j *Job) Cleanup(ctx context.Context, env scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	if err := safefs.UnlinkSymlinks(dir, stagingDir); err != nil {
		return err
	}
	res, err := readResult(dir)
	if err != nil {
		return err
	}
	if res != nil && len(res.Questions)+len(res.Decisions) > 0 {
		if err := postResult(ctx, env, dir, res); err != nil {
			return err
		}
	}
	for _, rel := range []string{corpusPath, resultPath} {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return safefs.FileErr(rel, err)
		}
	}
	// Leave what Stage made no trace; a directory the repo tracks is not empty and stays.
	_ = os.Remove(filepath.Join(dir, filepath.FromSlash(stagingDir)))
	_ = os.Remove(filepath.Join(dir, ".lighthouse"))
	return nil
}

// postResult validates res against the planned rows at the base of dir and the forge, then
// posts it.
func postResult(ctx context.Context, env scheduled.Env, dir string, res *result) error {
	if env.Forge == nil || env.Trusted == nil {
		return fmt.Errorf("%s: the repo has no forge", Name)
	}
	if res.hasWish() {
		if _, ok := issuestate.Name("needs_decision", env.Repo.Labels); !ok {
			return fmt.Errorf("%s: a wish question needs labels.needs_decision", Name)
		}
	}
	base, err := mergeBase(ctx, env, dir)
	if err != nil {
		return err
	}
	rows, err := plannedRows(ctx, env, dir, base)
	if err != nil {
		return err
	}
	posts, err := validate(ctx, env, res, rows)
	if err != nil {
		return err
	}
	login, err := env.Forge.Login(ctx)
	if err != nil {
		return fmt.Errorf("%s: login of the forge token: %w", Name, err)
	}
	for _, p := range posts {
		cs, err := env.Forge.Comments(ctx, p.issue)
		if err != nil {
			return fmt.Errorf("%s: comments of #%d: %w", Name, p.issue, err)
		}
		st, _, err := StatusOf(ctx, cs, login, env.Trusted)
		if err != nil {
			return fmt.Errorf("%s: #%d: %w", Name, p.issue, err)
		}
		if st != StatusNone && st != StatusAnswered {
			continue
		}
		// The label goes first: Apply is idempotent, so a comment that then fails leaves the issue
		// without a job comment and a next run asks again. The other order would leave an open
		// question without its label for good.
		if p.wish {
			if _, err := issuestate.Apply(ctx, env.Forge, p.issue, "needs_decision", env.Repo.Labels); err != nil {
				return fmt.Errorf("%s: label on #%d: %w", Name, p.issue, err)
			}
		}
		if _, err := env.Forge.CreateComment(ctx, p.issue, p.body); err != nil {
			return fmt.Errorf("%s: comment on #%d: %w", Name, p.issue, err)
		}
	}
	return nil
}

func (r *result) hasWish() bool {
	return slices.ContainsFunc(r.Questions, func(q resultQuestion) bool { return q.Topic == TopicWish })
}

// Allowed implements scheduled.Job. It gets no context and a resumed run calls it on a fresh
// instance, so it is a coarse, stateless filter on the form of path: never under .git/,
// .lighthouse/, .claude/ or .agents/; else a JSON or YAML file, or the fragment of Finish.
// Check decides which of those the run may really change.
func (*Job) Allowed(path string) bool {
	for _, d := range []string{".git", ".lighthouse", ".claude", ".agents"} {
		if path == d || strings.HasPrefix(path, d+"/") {
			return false
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".yaml", ".yml":
		return true
	}
	return isFragment(path)
}

// Check implements scheduled.Job. Its authority is labels.md at the base of the run (the
// merge-base of origin/<default> and HEAD: the start at the first Postcheck, the parent of the
// pushed commit on resume) and the forge, never the corpus. Every path changed since the base,
// untracked files included, is a document of a planned row or the fragment of Finish (only
// where the base has changelog.d/, and only with its fixed content). Every changed document
// parses, and every issue number it gained since the base is an open issue that carries the
// label of a row of that document, with a milestone when that row says Planned milestone.
func (j *Job) Check(ctx context.Context, env scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	base, err := mergeBase(ctx, env, dir)
	if err != nil {
		return err
	}
	rows, err := plannedRows(ctx, env, dir, base)
	if err != nil {
		return err
	}
	rowsOf := map[string][]Row{} // document -> its rows
	for _, r := range rows {
		rowsOf[r.Document] = append(rowsOf[r.Document], r)
	}
	changed, err := changedPaths(ctx, env, dir, base)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		// Postcheck checks only a run with changes, and a resumed one only a commit that changes
		// something: nothing against the base means the base is wrong.
		return errors.New("nothing changed since the base of the run")
	}
	issues := map[int]forge.Issue{}
	var fragments int
	for _, p := range changed {
		if docRows, ok := rowsOf[p]; ok {
			if err := checkDocument(ctx, env, dir, base, p, docRows, issues); err != nil {
				return err
			}
			continue
		}
		if isFragment(p) {
			if fragments++; fragments > 1 {
				return fmt.Errorf("more than one fragment under %s", fragmentDir)
			}
			if err := checkFragment(ctx, env, dir, base, p); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("%s is no planned document of %s at the base", scheduled.ShowPath(p), scheduled.ShowPath(env.Repo.Paths.Labels))
	}
	return nil
}

// checkDocument checks one changed document p of rows: a regular file that parses, whose new
// issue numbers since base are open issues on the forge that one of rows lets through, as the
// gate does (its label, and a milestone under Planned milestone). issues caches the issues read.
func checkDocument(ctx context.Context, env scheduled.Env, dir, base, p string, rows []Row, issues map[int]forge.Issue) error {
	b, err := readRegular(dir, p)
	if err != nil {
		return err
	}
	now, err := Issues(p, b)
	if err != nil {
		return err
	}
	before, err := documentIssues(ctx, env, dir, base, p)
	if err != nil {
		return err
	}
	added := slices.Sorted(maps.Keys(now))
	for _, n := range added {
		if before[n] {
			continue
		}
		is, ok := issues[n]
		if !ok {
			if env.Forge == nil {
				return fmt.Errorf("%s: the repo has no forge", Name)
			}
			var err error
			if is, err = env.Forge.Issue(ctx, n); err != nil {
				return fmt.Errorf("%s: #%d: %w", scheduled.ShowPath(p), n, err)
			}
			issues[n] = is
		}
		if is.State != "open" {
			return fmt.Errorf("%s: #%d is not open", scheduled.ShowPath(p), n)
		}
		if !slices.ContainsFunc(rows, func(r Row) bool {
			return slices.Contains(is.Labels, r.Label) && (r.Planned != PlannedMilestone || is.Milestone != nil)
		}) {
			var want []string
			for _, r := range rows {
				w := "label " + r.Label
				if r.Planned == PlannedMilestone {
					w += " with a milestone"
				}
				want = append(want, w)
			}
			return fmt.Errorf("%s: #%d does not carry %s", scheduled.ShowPath(p), n, strings.Join(want, " or "))
		}
	}
	return nil
}

// checkFragment checks the fragment p: the base has changelog.d/, and p holds exactly what
// Finish writes.
func checkFragment(ctx context.Context, env scheduled.Env, dir, base, p string) error {
	listed, err := git(ctx, env, dir, gitLimit, "ls-tree", base, "--", fragmentDir)
	if err != nil {
		return err
	}
	if !strings.Contains(listed, " tree ") {
		return fmt.Errorf("%s: the base has no %s/", scheduled.ShowPath(p), fragmentDir)
	}
	b, err := readRegular(dir, p)
	if err != nil {
		return err
	}
	if string(b) != fragmentBody(env.Repo.Language) {
		return fmt.Errorf("%s is not the fragment of the run", scheduled.ShowPath(p))
	}
	return nil
}

// readRegular reads p in dir, which must be a regular file with no symlink on the way.
func readRegular(dir, p string) ([]byte, error) {
	info, err := safefs.Stat(dir, p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", scheduled.ShowPath(p))
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil {
		return nil, safefs.FileErr(p, err)
	}
	return b, nil
}

// mergeBase is the base of the run in dir: the merge-base of origin/<default> and HEAD.
func mergeBase(ctx context.Context, env scheduled.Env, dir string) (string, error) {
	out, err := git(ctx, env, dir, gitLimit, "merge-base", "origin/"+defaultBranch(env), "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func defaultBranch(env scheduled.Env) string {
	if env.Repo.DefaultBranch == "" {
		return "main"
	}
	return env.Repo.DefaultBranch
}

// changedPaths are the paths of dir that differ from base: tracked changes, deletions included,
// and untracked files (a file the agent added is untracked at the first Postcheck).
func changedPaths(ctx context.Context, env scheduled.Env, dir, base string) ([]string, error) {
	diff, err := git(ctx, env, dir, gitLimit, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", base, "--")
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

// Finish implements scheduled.Job: where the work tree has changelog.d/ (a convention of this
// repo that a target repo may lack), it writes the run's internal fragment, named after the
// branch of the run, since a CI check may want a new fragment on every PR.
func (j *Job) Finish(ctx context.Context, env scheduled.Env, dir string, _ scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	info, err := safefs.Stat(dir, fragmentDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	branch, err := git(ctx, env, dir, gitLimit, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	name := fragmentName(strings.TrimSpace(branch))
	if !isFragment(name) {
		return fmt.Errorf("no fragment name for branch %q", strings.TrimSpace(branch))
	}
	return safefs.WriteFresh(filepath.Join(dir, filepath.FromSlash(name)), name, []byte(fragmentBody(env.Repo.Language)))
}
