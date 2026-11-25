package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/agentaudit"
	hist "github.com/CorneeldH/lighthouse/internal/history"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
	"github.com/CorneeldH/lighthouse/internal/transcripts"
	"github.com/CorneeldH/lighthouse/internal/version"
	"github.com/CorneeldH/lighthouse/pack"
)

// historyEnv is what `lighthouse history` takes from its surroundings, so a test can replace it.
type historyEnv struct {
	Getenv func(string) string
	Cwd    func() (string, error)
	Stdout io.Writer
	// ProcVersion is the path of /proc/version, read to recognise WSL.
	ProcVersion string
}

func defaultHistoryEnv() historyEnv {
	return historyEnv{Getenv: os.Getenv, Cwd: os.Getwd, Stdout: os.Stdout, ProcVersion: "/proc/version"}
}

// newHistoryCommand is `lighthouse history [--sources] [--since <date>] [--projects-dir <dir>]`.
func newHistoryCommand(env historyEnv) *cobra.Command {
	var sources bool
	var since, projectsDir string
	cmd := &cobra.Command{
		Use:   "history [--sources] [--since <date>] [--projects-dir <dir>]",
		Short: "Analyse the Claude Code transcripts of this repo and print a JSON report",
		Long: "Read the Claude Code transcripts that belong to the repo in the current directory and print\n" +
			"one JSON report: per part what was measured, what it means for lighthouse and a proposal,\n" +
			"plus a recommended start level. Reads only; writes nothing, uses no network and never\n" +
			"prints a prompt, tool output or file content.\n\n" +
			"--sources adds per finding at most 5 files with at most 10 line numbers each. --since\n" +
			"(YYYY-MM-DD or RFC 3339) keeps only records after that moment. --projects-dir replaces\n" +
			"$CLAUDE_CONFIG_DIR/projects, else $HOME/.claude/projects.\n\n" +
			"Exit: 0 report, also without transcripts | 1 the projects directory cannot be read | 2 wrong\n" +
			"usage, also a directory that is not a git repository.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usageErr(fmt.Errorf("%s: takes no arguments, got %d", cmd.CommandPath(), len(args)))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHistory(cmd.Context(), env, sources, since, projectsDir)
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	cmd.Flags().BoolVar(&sources, "sources", false, "add the files and line numbers behind each finding")
	cmd.Flags().StringVar(&since, "since", "", "only records after this date (YYYY-MM-DD or RFC 3339)")
	cmd.Flags().StringVar(&projectsDir, "projects-dir", "", "the Claude projects directory; default $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects")
	return cmd
}

func runHistory(ctx context.Context, env historyEnv, sources bool, since, projectsDir string) error {
	var opts hist.AnalyzeOptions
	opts.Sources = sources
	if since != "" {
		t, err := parseSince(since)
		if err != nil {
			return usageErr(fmt.Errorf("history: --since %q: want YYYY-MM-DD or RFC 3339", since))
		}
		opts.Since = t
	}
	cwd, err := env.Cwd()
	if err != nil {
		return err
	}
	top, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return usageErr(fmt.Errorf("lighthouse history: %s is not a git repository", cwd))
	}
	root := filepath.Clean(strings.TrimSpace(string(top)))

	if projectsDir == "" {
		if projectsDir, err = defaultProjectsDir(env.Getenv); err != nil {
			return err
		}
	}
	slug := repoSlugFromRemote(root)
	var slugSegs []string
	if slug != "" {
		slugSegs = strings.Split(slug, "/")
	}
	roots := []string{root}
	wts, err := gitWorktrees(ctx, root)
	if err != nil {
		return err
	}
	roots = append(roots, wts...)
	found, err := transcripts.Find(projectsDir, roots, slugSegs, transcripts.Options{FoldCase: true})
	if err != nil {
		return fmt.Errorf("history: reading %s: %w", projectsDir, err)
	}

	var files []hist.SessionFiles
	seen := map[string]bool{}
	var dirs []string
	for _, s := range found {
		files = append(files, hist.SessionFiles{ID: s.ID, Paths: append([]string{s.Path}, s.Subs...)})
		if d := filepath.Dir(s.Path); !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)

	henv := hist.Env{Repo: slug, TrunkNames: trunkNames(ctx, root), ProjectsDir: projectsDir, ProjectDirs: dirs, RepoRoot: root}
	henv.AuditHooks, henv.AuditErr = auditHooks(root)
	rep := hist.Analyze(files, henv, opts)
	if len(files) == 0 && isWSL(env.ProcVersion) {
		rep.Hint = "No sessions found. On WSL the transcripts of a native Windows Claude Code may lie under " +
			"/mnt/c/Users/<naam>/.claude/projects; ask the user before running again with --projects-dir."
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(env.Stdout, "%s\n", data)
	return err
}

func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// defaultProjectsDir is $CLAUDE_CONFIG_DIR/projects, else $HOME/.claude/projects.
func defaultProjectsDir(getenv func(string) string) (string, error) {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects"), nil
	}
	home := getenv("HOME")
	if !filepath.IsAbs(home) {
		return "", errors.New("history: HOME is not an absolute path: no place to find the transcripts")
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func isWSL(procVersion string) bool {
	b, err := os.ReadFile(procVersion)
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// trunkNames is the bare default branch from origin/HEAD, or nil when git has none.
func trunkNames(ctx context.Context, root string) []string {
	out, err := exec.CommandContext(ctx, "git", "-C", root, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		return nil
	}
	if name := strings.TrimPrefix(strings.TrimSpace(string(out)), "refs/remotes/origin/"); name != "" {
		return []string{name}
	}
	return nil
}

// repoSlugFromRemote is owner/name of the origin remote of dir, parsed like init does; ""
// without an origin or with a remote that does not parse.
func repoSlugFromRemote(dir string) string {
	url, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	forge, ok := projectconfig.ForgeFromRemote(strings.TrimSpace(string(url)), nil)
	if !ok {
		return ""
	}
	return forge.Repo
}

// auditHooks finds the project, local and user hooks that overlap a guard of any pack in the
// binary: before init there is no manifest, so the packs come from the binary itself.
func auditHooks(root string) ([]hist.HookOverlap, error) {
	aenv, err := agentaudit.DefaultEnv(root)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(pack.Files, ".")
	if err != nil {
		return nil, err
	}
	aenv.Packs = nil
	var guards []packs.Guard
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := fs.Sub(pack.Files, e.Name())
		if err != nil {
			return nil, err
		}
		p, err := packs.Load(sub, e.Name(), version.String())
		if err != nil {
			return nil, err
		}
		aenv.Packs = append(aenv.Packs, p)
		guards = append(guards, p.Guards...)
	}
	return hist.OverlapFromAudit(aenv, guards)
}
