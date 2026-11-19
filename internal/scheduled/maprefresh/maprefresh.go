// Package maprefresh is the scheduled job map-refresh: it keeps the codebase map (the documents a
// map command such as /gsd-map-codebase writes) fresh without a human deciding each period
// whether a refresh is worth it. A Go gate measures how far the code drifted since the map was
// last committed and routes the run: nothing, a fast map or a full map. The documents are checked
// for completeness and mirrored into a publish directory with an index.md. Behaviour follows
// scripts/codebase_map_refresh.py in personal_ai_agent.
package maprefresh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Name is the job name a [[schedule]] block names in `job`.
const Name = "map-refresh"

func init() { scheduled.Register(New()) }

// Job is the map-refresh job. The registered instance has no params: Configure returns the
// instance for one schedule, and the others refuse to run.
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

var errUnbound = errors.New("map-refresh: job is not configured for a schedule (scheduled.Bind)")

// Name implements scheduled.Job.
func (*Job) Name() string { return Name }

// Gate implements scheduled.Job: the drift of origin/<default> decides skip, fast or full.
func (j *Job) Gate(ctx context.Context, env scheduled.Env) (scheduled.Verdict, error) {
	if !j.bound {
		return scheduled.Verdict{}, errUnbound
	}
	branch := env.Repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	d, err := measure(ctx, env.Run, env.Repo.Dir, branch, j.cfg)
	if err != nil {
		return scheduled.Verdict{}, err
	}
	since := "never mapped"
	if d.Base != "" {
		since = "since " + d.Base[:min(8, len(d.Base))]
	}
	detail := fmt.Sprintf("drift score %d (%d changed files, %d new directories) %s", d.Score, d.Changed, d.NewDirs, since)
	if d.Base == "" {
		return scheduled.Verdict{Mode: modeFull, Score: d.Score}, nil
	}
	switch mode := route(d.Score, j.cfg.driftLow, j.cfg.driftHigh); mode {
	case "":
		return scheduled.Verdict{Skip: detail + ", below " + fmt.Sprint(j.cfg.driftLow), Score: d.Score}, nil
	case modeFast:
		return scheduled.Verdict{Mode: modeFast, Args: j.cfg.fastArgs, Score: d.Score}, nil
	default:
		return scheduled.Verdict{Mode: modeFull, Score: d.Score}, nil
	}
}

// Launch implements scheduled.Job.
func (j *Job) Launch(v scheduled.Verdict, _ scheduled.Env) (scheduled.LaunchPlan, error) {
	if !j.bound {
		return scheduled.LaunchPlan{}, errUnbound
	}
	command, limit := j.cfg.command, j.cfg.maxFull
	if v.Mode == modeFast {
		limit = j.cfg.maxFast
	}
	if v.Args != "" {
		command += " " + v.Args
	}
	return scheduled.LaunchPlan{
		Command:      command,
		Instructions: j.instructions(),
		Tools:        append([]string(nil), j.cfg.tools...),
		MaxRuntime:   limit,
	}, nil
}

func (j *Job) instructions() string {
	writes := "`" + j.cfg.mapDir + "/`"
	if j.cfg.publishDir != "" {
		writes += " and `" + j.cfg.publishDir + "/`"
	}
	return "You are running unattended from a schedule. There is no human to answer questions. " +
		"Refresh and overwrite the existing documents under `" + j.cfg.mapDir + "/` without asking for confirmation.\n" +
		"Do NOT create a git commit and do NOT push: the controller checks and commits the result itself.\n" +
		"Write only under " + writes + "; a file anywhere else fails the run. Do not create symlinks.\n"
}

// Stage implements scheduled.Job: the run needs nothing staged.
func (*Job) Stage(context.Context, scheduled.Env, scheduled.Verdict) error { return nil }

// Cleanup implements scheduled.Job: it unlinks every symlink the agent left in the directories
// the job reads and writes, so Check and Finish never read or write through one. A link is
// removed, never followed.
func (j *Job) Cleanup(_ context.Context, _ scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	for _, d := range j.dirs() {
		if err := unlinkSymlinks(dir, d); err != nil {
			return err
		}
	}
	return nil
}

// dirs are the directories the run may write.
func (j *Job) dirs() []string {
	if j.cfg.publishDir == "" {
		return []string{j.cfg.mapDir}
	}
	return []string{j.cfg.mapDir, j.cfg.publishDir}
}

// Allowed implements scheduled.Job: a file under map_dir or publish_dir.
func (j *Job) Allowed(path string) bool {
	if !j.bound {
		return false
	}
	for _, d := range j.dirs() {
		if strings.HasPrefix(path, d+"/") {
			return true
		}
	}
	return false
}

// Check implements scheduled.Job: every document exists under map_dir, as a regular file of at
// least min_lines lines. A document the agent deleted fails like one it never wrote.
func (j *Job) Check(_ context.Context, _ scheduled.Env, dir string) error {
	if !j.bound {
		return errUnbound
	}
	var broken []string
	for _, name := range j.cfg.documents {
		rel := j.cfg.mapDir + "/" + name + ".md"
		info, err := stat(dir, rel)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			broken = append(broken, name+" (missing)")
			continue
		case err != nil:
			broken = append(broken, err.Error())
			continue
		case !info.Mode().IsRegular():
			broken = append(broken, name+" (not a regular file)")
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			broken = append(broken, fileErr(rel, err).Error())
			continue
		}
		if n := lineCount(b); n < j.cfg.minLines {
			broken = append(broken, fmt.Sprintf("%s (%d lines, want at least %d)", name, n, j.cfg.minLines))
		}
	}
	if len(broken) > 0 {
		return fmt.Errorf("incomplete document set: %s", strings.Join(broken, ", "))
	}
	return nil
}

func lineCount(b []byte) int {
	n := strings.Count(string(b), "\n")
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// Finish implements scheduled.Job. When map_dir is byte-equal to HEAD nothing was refreshed: the
// publish directory is put back as committed and no PR follows. Otherwise the documents are
// copied (never linked) into publish_dir, with an index.md that names mode, score, base and date.
func (j *Job) Finish(ctx context.Context, env scheduled.Env, dir string, v scheduled.Verdict) error {
	if !j.bound {
		return errUnbound
	}
	g := func(args ...string) (string, error) {
		return git(ctx, env.Run, dir, 2*time.Minute, append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false"}, args...)...)
	}
	changed, err := g("status", "--porcelain", "-z", "--untracked-files=all", "--", j.cfg.mapDir)
	if err != nil {
		return err
	}
	if changed == "" {
		return j.restorePublishDir(g)
	}
	if j.cfg.publishDir == "" {
		return nil
	}
	base, err := g("rev-list", "-1", "HEAD", "--", j.cfg.mapDir)
	if err != nil {
		return err
	}
	// Look before creating: MkdirAll would follow a symlink among the existing parents.
	if _, err := stat(dir, j.cfg.publishDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(j.cfg.publishDir)), 0o755); err != nil {
		return fileErr(j.cfg.publishDir, err)
	}
	if _, err := stat(dir, j.cfg.publishDir); err != nil {
		return err
	}
	for _, name := range j.cfg.documents {
		src := j.cfg.mapDir + "/" + name + ".md"
		if info, err := stat(dir, src); err != nil {
			return err
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: not a regular file", scheduled.ShowPath(src))
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src)))
		if err != nil {
			return fileErr(src, err)
		}
		dst := j.cfg.publishDir + "/" + name + ".md"
		if err := writeFresh(filepath.Join(dir, filepath.FromSlash(dst)), dst, b); err != nil {
			return err
		}
	}
	idx := j.cfg.publishDir + "/index.md"
	return writeFresh(filepath.Join(dir, filepath.FromSlash(idx)), idx,
		[]byte(j.index(env, v, strings.TrimSpace(base))))
}

// restorePublishDir puts publish_dir back as committed (its tracked files as they were, whatever
// else is in it gone), so a run that refreshed nothing leaves nothing to commit.
func (j *Job) restorePublishDir(g func(args ...string) (string, error)) error {
	if j.cfg.publishDir == "" {
		return nil
	}
	tracked, err := g("ls-tree", "-r", "--name-only", "HEAD", "--", j.cfg.publishDir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(tracked) != "" {
		if _, err := g("checkout", "--quiet", "HEAD", "--", j.cfg.publishDir); err != nil {
			return err
		}
	}
	_, err = g("clean", "-fdq", "--", j.cfg.publishDir)
	return err
}

// index is the landing page of the published map: provenance, so a reader can judge its age.
func (j *Job) index(env scheduled.Env, v scheduled.Verdict, base string) string {
	nl := env.Repo.Language == "nl"
	date := env.Now.Format(time.DateOnly)
	baseText := "(none)"
	if base != "" {
		baseText = "`" + base[:min(8, len(base))] + "`"
	}
	var b strings.Builder
	if nl {
		fmt.Fprintf(&b, "# Codebase-kaart\n\nMachinaal geschreven momentopname van deze repository, gespiegeld uit `%s/`. "+
			"**Niet met de hand aanpassen**: de volgende verversing overschrijft deze bestanden.\n\n"+
			"- Laatste verversing: **%s**\n- Modus: **%s**\n- Drift sinds de vorige kaart: **%d** (gewijzigde bronbestanden, nieuwe mappen zwaarder gewogen)\n"+
			"- Vorige kaartcommit: %s\n\n", j.cfg.mapDir, date, v.Mode, v.Score, baseText)
	} else {
		fmt.Fprintf(&b, "# Codebase map\n\nMachine-written snapshot of this repository, mirrored from `%s/`. "+
			"**Do not edit these files by hand**: the next refresh overwrites them.\n\n"+
			"- Last refresh: **%s**\n- Mode: **%s**\n- Drift since the previous map: **%d** (changed source files, new directories weighted)\n"+
			"- Previous map commit: %s\n\n", j.cfg.mapDir, date, v.Mode, v.Score, baseText)
	}
	for _, name := range j.cfg.documents {
		fmt.Fprintf(&b, "- [%s](%s.md)\n", name, name)
	}
	return b.String()
}
