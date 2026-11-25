package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/gitpath"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
	"github.com/CorneeldH/lighthouse/internal/telemetry/collect"
	"github.com/CorneeldH/lighthouse/internal/telemetry/transcript"
	"github.com/CorneeldH/lighthouse/internal/transcripts"
	"github.com/CorneeldH/lighthouse/internal/worktreeroot"
)

// collectEnv is what `lighthouse telemetry collect` takes from its surroundings, so a test can
// replace it.
type collectEnv struct {
	Getenv func(string) string
	Now    func() time.Time
	// AfterMeasure, when set, runs after the size of a transcript is measured and before it is
	// read: the moment a growing file is cut off.
	AfterMeasure func()
}

func defaultCollectEnv() collectEnv {
	return collectEnv{Getenv: os.Getenv, Now: time.Now}
}

// collectOpts are the switches of one collection.
type collectOpts struct {
	RepoDir, DB       string
	SinceLast, DryRun bool
}

// collectSummary is what runCollect did: Lines is the report, one line per transcript, and
// Failed counts the transcripts whose processing went wrong.
type collectSummary struct {
	Lines  []string
	Found  int
	Failed int
}

func newTelemetryCommand(env collectEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Collect telemetry into lighthouse.db",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newTelemetryCollectCommand(env))
	return cmd
}

// newTelemetryCollectCommand is `lighthouse telemetry collect [--repo-dir <path>] [--db <path>] [--since-last] [--dry-run]`.
func newTelemetryCollectCommand(env collectEnv) *cobra.Command {
	var opts collectOpts
	cmd := &cobra.Command{
		Use:   "collect [--repo-dir <path>] [--db <path>] [--since-last] [--dry-run]",
		Short: "Collect the Claude Code transcripts of a repo into lighthouse.db",
		Long: "Find the transcripts under ~/.claude/projects that belong to the repo (its directory, its\n" +
			"worktree root, its git worktrees, or any directory whose path holds <owner>/<repo>) and\n" +
			"write the setup and the token usage of each, in one transaction per transcript. The\n" +
			"subagent transcripts of a session belong to its main transcript. A transcript that is\n" +
			"left alone gets one line: skip <path>: <reason>.\n\n" +
			"--since-last leaves a transcript that did not change since the last pass. --dry-run opens\n" +
			"the database read-only without migrating it, writes nothing (no sidecar either) and\n" +
			"prints what it would do; a database that does not exist is reported.\n\n" +
			"Exit: 0 collected, also when transcripts were skipped | 1 a processing, config, git or\n" +
			"database error, also a pass that failed in part | 2 wrong usage.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usageErr(fmt.Errorf("%s: takes no arguments, got %d", cmd.CommandPath(), len(args)))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			sum, err := runCollect(cmd.Context(), env, opts)
			if len(sum.Lines) > 0 {
				_, _ = io.WriteString(cmd.OutOrStdout(), strings.Join(sum.Lines, "\n")+"\n")
			}
			if err != nil {
				return &exitError{code: 1, err: err}
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	cmd.Flags().StringVar(&opts.RepoDir, "repo-dir", ".", "the checkout of the repo to collect for")
	cmd.Flags().StringVar(&opts.DB, "db", "", "the database; default the lighthouse.db of the state directory")
	cmd.Flags().BoolVar(&opts.SinceLast, "since-last", false, "leave a transcript that did not change since the last pass")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "write nothing; read the database read-only and print what would happen")
	return cmd
}

// runCollect is the whole collection, apart from cobra: tick calls it too. The summary is filled
// as far as the pass came, also when the error is not nil.
func runCollect(ctx context.Context, env collectEnv, opts collectOpts) (collectSummary, error) {
	var sum collectSummary
	repoDir, err := filepath.Abs(opts.RepoDir)
	if err != nil {
		return sum, err
	}
	repo, err := repoconfig.Load(repoDir)
	if err != nil {
		return sum, err
	}
	cfg, err := config.Load(filepath.Join(repoDir, "lighthouse.toml"))
	if err != nil {
		return sum, err
	}
	plansDir, err := configValue(cfg, "paths.plans")
	if err != nil {
		return sum, err
	}
	wtValue, err := configValue(cfg, "worktree.root")
	if err != nil {
		return sum, err
	}
	main := repoDir
	if m, err := worktreeroot.MainCheckout(repoDir); err == nil {
		main = m
	}
	roots := []string{repoDir}
	// worktree.root is a discovery root only when it lies inside the main checkout (the
	// repo-local default). An absolute root outside it may be shared by several repos, so
	// everything below it would pull their sessions in; the git worktrees and the slug rule find
	// this repo's worktrees there.
	if wr := worktreeroot.Resolve(main, wtValue); wr == main || worktreeroot.Contains(main, wr) {
		roots = append(roots, wr)
	}
	wts, err := gitWorktrees(ctx, repoDir)
	if err != nil {
		return sum, err
	}
	roots = append(roots, wts...)

	home := env.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return sum, errors.New("HOME is not an absolute path: no place to find the transcripts")
	}
	found, err := transcripts.Find(filepath.Join(home, ".claude", "projects"), roots, strings.Split(repo.Slug, "/"), transcripts.Options{})
	if err != nil {
		return sum, err
	}
	sum.Found = len(found)

	stateDir, err := collect.StateDir(env.Getenv)
	if err != nil {
		return sum, err
	}
	dbPath := opts.DB
	if dbPath == "" {
		if dbPath, err = store.DefaultPath(); err != nil {
			return sum, err
		}
	}
	var st *store.Store
	if opts.DryRun {
		st, err = store.OpenReadOnly(ctx, dbPath)
		if errors.Is(err, fs.ErrNotExist) {
			sum.addf("database %s does not exist: nothing would be compared", dbPath)
			for _, f := range found {
				sum.addf("found %s", f.Path)
			}
			return sum, nil
		}
	} else {
		st, err = store.OpenShared(ctx, dbPath)
	}
	if err != nil {
		return sum, err
	}
	defer func() { _ = st.Close() }()
	if opts.DryRun {
		have, want, err := store.SchemaVersions(ctx, st.DB)
		if err != nil {
			return sum, err
		}
		if have < want {
			sum.addf("database %s is at schema %d, this lighthouse writes %d: --dry-run does not migrate", dbPath, have, want)
			for _, f := range found {
				sum.addf("found %s", f.Path)
			}
			return sum, nil
		}
	}

	procOpt := collect.Options{
		Repo: repo.Slug, DefaultBranch: repo.DefaultBranch, PlansDir: plansDir, RepoDir: repoDir,
		StateDir: stateDir, Now: env.Now, SinceLast: opts.SinceLast, DryRun: opts.DryRun,
	}
	for _, f := range found {
		out, err := collectOne(ctx, env, st, f, procOpt)
		switch {
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission):
			sum.addf("skip %s: %v", f.Path, err)
		case err != nil:
			sum.Failed++
			sum.addf("fail %s: %v", f.Path, err)
		case out.Kind == "skipped":
			sum.addf("skip %s: %s", f.Path, out.Reason)
		case opts.DryRun:
			sum.addf("would collect %s as %s", f.Path, out.Kind)
		default:
			sum.addf("%s %s", out.Kind, f.Path)
		}
	}
	if sum.Failed > 0 {
		return sum, fmt.Errorf("%d of %d transcripts failed", sum.Failed, sum.Found)
	}
	return sum, nil
}

func (s *collectSummary) addf(format string, args ...any) {
	s.Lines = append(s.Lines, fmt.Sprintf(format, args...))
}

// configValue is the value of key for cfg, by the function `lighthouse config <key>` uses.
func configValue(cfg *config.Config, key string) (string, error) {
	v, err := config.Lookup(cfg, key)
	if err != nil {
		return "", err
	}
	lines, err := config.Lines(key, v)
	if err != nil {
		return "", err
	}
	if len(lines) != 1 {
		return "", fmt.Errorf("%s: want one value, got %d", key, len(lines))
	}
	return lines[0], nil
}

// gitWorktrees lists the worktree paths of the repo at dir, the main checkout included.
func gitWorktrees(ctx context.Context, dir string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list in %s: %w", dir, err)
	}
	var paths []string
	for _, ln := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(ln, "worktree "); ok {
			paths = append(paths, gitpath.Native(p))
		}
	}
	return paths, nil
}

// collectOne measures, reads and processes one session. The moment before the measuring is
// recorded_at; each file is read up to the size it had then, so lines that arrive later wait
// for the next pass.
func collectOne(ctx context.Context, env collectEnv, st *store.Store, s transcripts.Session, opt collect.Options) (collect.Outcome, error) {
	recorded := env.Now()
	type part struct {
		name, path string
		size       int64
	}
	var parts []part
	var mtime time.Time
	add := func(name, path string) error {
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		parts = append(parts, part{name, path, fi.Size()})
		if fi.ModTime().After(mtime) {
			mtime = fi.ModTime()
		}
		return nil
	}
	if err := add(s.ID+".jsonl", s.Path); err != nil {
		return collect.Outcome{}, err
	}
	for _, sub := range s.Subs {
		if err := add(s.ID+"/subagents/"+filepath.Base(sub), sub); err != nil {
			return collect.Outcome{}, err
		}
	}
	if env.AfterMeasure != nil {
		env.AfterMeasure()
	}
	var files []transcript.File
	var opened []*os.File
	defer func() {
		for _, fh := range opened {
			_ = fh.Close()
		}
	}()
	for _, p := range parts {
		fh, err := os.Open(p.path)
		if err != nil {
			return collect.Outcome{}, err
		}
		opened = append(opened, fh)
		files = append(files, transcript.File{Name: p.name, R: io.LimitReader(fh, p.size)})
	}
	return collect.Process(ctx, st.DB, collect.Source{
		SessionID: s.ID, TranscriptPath: s.Path, Files: files, MTime: mtime, Recorded: recorded,
	}, opt)
}
