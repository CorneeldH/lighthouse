// Package reflect is the scheduled job reflect: it has a skill read what people asked of Claude in
// the sessions of a repository and write one document of candidate skills. A Go gate counts the
// sessions since the last round and skips a round with too little new material; Go extracts the
// user prompts into one corpus file, so the agent never reads a raw session. Interactive and
// controller sessions are kept apart in the corpus, so the skill can weigh each kind by itself.
// The agent may write one document and index.md under output_dir, nothing else. Behaviour follows
// scripts/reflect_skills_run.py in personal_ai_agent.
package reflect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/safefs"
)

// Name is the job name a [[schedule]] block names in `job`.
const Name = "reflect"

func init() { scheduled.Register(New()) }

const (
	corpusRel = ".lighthouse/run/reflect-corpus.md"
	docSuffix = "-skill-kandidaten.md"
	indexName = "index.md"
	gitLimit  = 2 * time.Minute
)

var docDate = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})` + regexp.QuoteMeta(docSuffix) + `$`)

// Job is the reflect job. The registered instance has no params: Configure returns the instance
// for one schedule, and the others refuse to run.
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

var errUnbound = errors.New("reflect: job is not configured for a schedule (scheduled.Bind)")

// Name implements scheduled.Job.
func (*Job) Name() string { return Name }

func git(ctx context.Context, env scheduled.Env, dir string, args ...string) (string, error) {
	out, code, err := env.Run(ctx, gitLimit, "git",
		append([]string{"-C", dir, "--literal-pathspecs", "--no-optional-locks", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false"}, args...)...)
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.ReplaceAll(safefs.Cause(err).Error(), dir, "<repo>"))
	}
	if code != 0 {
		return "", fmt.Errorf("git %s: exit code %d", args[0], code)
	}
	return out, nil
}

// window is how many days back the round looks: from the newest document under output_dir at ref
// up to now, but never below window_min_days or above window_max_days. Without a document it is
// the minimum.
func (j *Job) window(ctx context.Context, env scheduled.Env, dir, ref string) (int, error) {
	out, err := git(ctx, env, dir, "ls-tree", "--name-only", ref, j.cfg.outputDir+"/")
	if err != nil {
		return 0, err
	}
	var last time.Time
	for _, name := range strings.Split(out, "\n") {
		m := docDate.FindStringSubmatch(filepath.Base(strings.TrimSpace(name)))
		if m == nil {
			continue
		}
		if d, err := time.Parse(time.DateOnly, m[1]); err == nil && d.After(last) {
			last = d
		}
	}
	if last.IsZero() {
		return j.cfg.windowMin, nil
	}
	today, _ := time.Parse(time.DateOnly, env.Now.Format(time.DateOnly))
	days := int(today.Sub(last).Hours() / 24)
	return max(j.cfg.windowMin, min(j.cfg.windowMax, days)), nil
}

// recent are the sessions of the repo written within the window, without those of scheduled runs
// (also the earlier rounds of this job), newest first.
func (j *Job) recent(env scheduled.Env, days int) ([]sessionFile, error) {
	files, err := findSessions(env.Home, env.Repo)
	if err != nil {
		return nil, err
	}
	_, scheduledIDs := classify(env.History)
	cutoff := env.Now.AddDate(0, 0, -days)
	var out []sessionFile
	for _, f := range files {
		if !f.Mod.Before(cutoff) && !scheduledIDs[f.ID] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Mod.After(out[b].Mod) })
	return out, nil
}

// Gate implements scheduled.Job: the sessions of both kinds since the last round decide.
func (j *Job) Gate(ctx context.Context, env scheduled.Env) (scheduled.Verdict, error) {
	if !j.bound {
		return scheduled.Verdict{}, errUnbound
	}
	branch := env.Repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	days, err := j.window(ctx, env, env.Repo.Dir, "origin/"+branch)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	files, err := j.recent(env, days)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	controller, _ := classify(env.History)
	var nCtl int
	for _, f := range files {
		if kind(f.ID, controller) == kindController {
			nCtl++
		}
	}
	nInt := len(files) - nCtl
	if len(files) < j.cfg.minSessions {
		return scheduled.Verdict{Skip: fmt.Sprintf("%d new sessions in the last %d days (interactive %d, controller %d), below %d",
			len(files), days, nInt, nCtl, j.cfg.minSessions), Score: len(files)}, nil
	}
	return scheduled.Verdict{Score: len(files)}, nil
}

func (j *Job) outPath(env scheduled.Env) string {
	return j.cfg.outputDir + "/" + env.Now.Format(time.DateOnly) + docSuffix
}

// Launch implements scheduled.Job.
func (j *Job) Launch(_ scheduled.Verdict, env scheduled.Env) (scheduled.LaunchPlan, error) {
	if !j.bound {
		return scheduled.LaunchPlan{}, errUnbound
	}
	out := j.outPath(env)
	return scheduled.LaunchPlan{
		Command:      j.cfg.command + " --corpus " + corpusRel + " --out " + out,
		Instructions: j.instructions(out),
		Tools:        append([]string(nil), j.cfg.tools...),
		MaxRuntime:   j.cfg.maxRuntime,
	}, nil
}

func (j *Job) instructions(out string) string {
	return "You are running unattended from a schedule. There is no human to answer questions: ask none.\n" +
		"Read only the corpus file you get with --corpus; open nothing under ~/.claude/projects/. " +
		"The corpus has two sections, Interactief and Controller: weigh them apart.\n" +
		"Write exactly one file, at the path you get with --out (`" + out + "`). Create no issues and no skill files. " +
		"Do NOT create a git commit and do NOT push: the controller checks and commits the result itself.\n" +
		"A file anywhere else, or a symlink, fails the run.\n"
}

// Stage implements scheduled.Job: it writes the corpus into the work tree.
func (j *Job) Stage(ctx context.Context, env scheduled.Env, _ scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	days, err := j.window(ctx, env, env.Dir, "HEAD")
	if err != nil {
		return err
	}
	files, err := j.recent(env, days)
	if err != nil {
		return err
	}
	controller, _ := classify(env.History)
	var inter, ctrl []corpusSession
	for _, f := range files {
		prompts := extractPrompts(f.Path, j.cfg.maxPrompt, j.cfg.maxCorpus)
		if len(prompts) == 0 {
			continue
		}
		s := corpusSession{Date: f.Mod.Local().Format(time.DateOnly), ID: f.ID[:min(8, len(f.ID))], Prompts: prompts}
		if kind(f.ID, controller) == kindController {
			ctrl = append(ctrl, s)
		} else {
			inter = append(inter, s)
		}
	}
	// Look before creating: MkdirAll would follow a symlink among the existing parents.
	for _, rel := range []string{".lighthouse", ".lighthouse/run"} {
		_, err := safefs.Stat(env.Dir, rel)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(filepath.Join(env.Dir, filepath.FromSlash(rel)), 0o755); err != nil {
				return safefs.FileErr(rel, err)
			}
		} else if err != nil {
			return err
		}
	}
	return safefs.WriteFresh(filepath.Join(env.Dir, filepath.FromSlash(corpusRel)), corpusRel, buildCorpus(inter, ctrl, j.cfg.maxCorpus))
}

// Cleanup implements scheduled.Job: the corpus goes before the containment, so it never reaches
// the branch, and every symlink the agent left under output_dir is unlinked.
func (j *Job) Cleanup(_ context.Context, _ scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	for _, d := range []string{".lighthouse/run", j.cfg.outputDir} {
		if err := safefs.UnlinkSymlinks(dir, d); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(corpusRel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return safefs.FileErr(corpusRel, err)
	}
	// Leave what Stage made no trace; a directory the repo tracks is not empty and stays.
	_ = os.Remove(filepath.Join(dir, ".lighthouse", "run"))
	_ = os.Remove(filepath.Join(dir, ".lighthouse"))
	return nil
}

// Allowed implements scheduled.Job: one dated document and index.md directly under output_dir.
// Which date is not known here; Check requires that exactly one document changed.
func (j *Job) Allowed(path string) bool {
	if !j.bound {
		return false
	}
	name, ok := strings.CutPrefix(path, j.cfg.outputDir+"/")
	return ok && (name == indexName || docDate.MatchString(name))
}

// changedDocs are the documents under output_dir the run added or changed, by git status.
func (j *Job) changedDocs(ctx context.Context, env scheduled.Env, dir string) ([]string, error) {
	out, err := git(ctx, env, dir, "status", "--porcelain", "-z", "--untracked-files=all", "--", j.cfg.outputDir)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	if strings.Trim(out, "\x00") == "" {
		// A clean checkout of an already committed result (a resumed run checks the pushed
		// commit): the document is what that commit added over its parent.
		diff, err := git(ctx, env, dir, "diff-tree", "-r", "--no-commit-id", "--no-renames", "--name-status", "-z", "--root", "HEAD", "--", j.cfg.outputDir)
		if err != nil {
			return nil, err
		}
		fields = nil
		parts := strings.Split(diff, "\x00")
		for i := 0; i+1 < len(parts); i += 2 {
			fields = append(fields, fmt.Sprintf("%-2s %s", parts[i], parts[i+1]))
		}
	}
	var docs []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		status, p := f[:2], f[3:]
		if strings.ContainsAny(status, "RC") {
			i++ // the old name follows
		}
		if name, ok := strings.CutPrefix(p, j.cfg.outputDir+"/"); ok && docDate.MatchString(name) {
			if strings.Contains(status, "D") {
				return nil, fmt.Errorf("%s was deleted", scheduled.ShowPath(p))
			}
			docs = append(docs, p)
		}
	}
	return docs, nil
}

// Check implements scheduled.Job: exactly one document is new or changed, a regular file of at
// least min_lines lines.
func (j *Job) Check(ctx context.Context, env scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	docs, err := j.changedDocs(ctx, env, dir)
	if err != nil {
		return err
	}
	switch len(docs) {
	case 0:
		return errors.New("no document written under " + scheduled.ShowPath(j.cfg.outputDir))
	case 1:
	default:
		shown := make([]string, len(docs))
		for i, d := range docs {
			shown[i] = scheduled.ShowPath(d)
		}
		return fmt.Errorf("want exactly one document, found %d: %s", len(docs), strings.Join(shown, ", "))
	}
	info, err := safefs.Stat(dir, docs[0])
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", scheduled.ShowPath(docs[0]))
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(docs[0])))
	if err != nil {
		return safefs.FileErr(docs[0], err)
	}
	if n := strings.Count(string(b), "\n") + btoi(len(b) > 0 && b[len(b)-1] != '\n'); n < j.cfg.minLines {
		return fmt.Errorf("%s has %d lines, want at least %d", scheduled.ShowPath(docs[0]), n, j.cfg.minLines)
	}
	return nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Finish implements scheduled.Job: it writes index.md, the list of rounds, newest first.
func (j *Job) Finish(_ context.Context, env scheduled.Env, dir string, _ scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	if _, err := safefs.Stat(dir, j.cfg.outputDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(j.cfg.outputDir)))
	if err != nil {
		return safefs.FileErr(j.cfg.outputDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && docDate.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	slices.Reverse(names)
	var b strings.Builder
	if env.Repo.Language == "nl" {
		b.WriteString("# Skill-kandidaten\n\nMachinaal geschreven rondes: wat er in de sessies terugkomt en een skill kan worden. " +
			"**Niet met de hand aanpassen**: de volgende ronde schrijft deze lijst opnieuw.\n\n")
	} else {
		b.WriteString("# Skill candidates\n\nMachine-written rounds: what recurs in the sessions and could become a skill. " +
			"**Do not edit by hand**: the next round rewrites this list.\n\n")
	}
	for _, n := range names {
		fmt.Fprintf(&b, "- [%s](%s)\n", docDate.FindStringSubmatch(n)[1], n)
	}
	idx := j.cfg.outputDir + "/" + indexName
	return safefs.WriteFresh(filepath.Join(dir, filepath.FromSlash(idx)), idx, []byte(b.String()))
}
