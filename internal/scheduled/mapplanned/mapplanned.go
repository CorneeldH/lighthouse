package mapplanned

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
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
	branch := env.Repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
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
		if r.Label != "" {
			rows = append(rows, r)
		}
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

// errNotYet is what the parts of the run that task 7 of issue #476 adds return until then.
var errNotYet = errors.New("map-planned: not implemented yet")

// Stage implements scheduled.Job. STUB: the corpus is not written yet, so a run fails here
// before the agent starts.
func (*Job) Stage(context.Context, scheduled.Env, scheduled.Verdict) error { return errNotYet }

// Cleanup implements scheduled.Job. STUB: nothing is staged yet, so there is nothing to remove
// and no result to post.
func (*Job) Cleanup(context.Context, scheduled.Env, string) error { return nil }

// Allowed implements scheduled.Job. STUB: the run may write nothing yet.
func (*Job) Allowed(string) bool { return false }

// Check implements scheduled.Job. STUB: no result passes yet.
func (*Job) Check(context.Context, scheduled.Env, string) error { return errNotYet }

// Finish implements scheduled.Job. STUB: no result is finished yet.
func (*Job) Finish(context.Context, scheduled.Env, string, scheduled.Verdict) error { return errNotYet }
