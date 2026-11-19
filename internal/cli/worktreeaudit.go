package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/worktreeaudit"
)

func defaultWorktreeAuditEnv() worktreeaudit.Env {
	return worktreeAuditEnv(newDefaultForgeOpener())
}

// worktreeAuditEnv starts git for real and opens the forge of lighthouse.toml (LIGHTHOUSE_TOML,
// else the repo root) with o.
func worktreeAuditEnv(o *forgeOpener) worktreeaudit.Env {
	return worktreeaudit.Env{
		Run: procrun.Exec(worktreeaudit.GitTimeout),
		Config: func() (*config.Config, error) {
			path, err := config.Path()
			if err != nil {
				return nil, err
			}
			return config.Load(path)
		},
		Forge: forgeOpenWith(o, loadRepoAtConfigPath),
		Alive: worktreeaudit.PidAlive,
	}
}

// auditViews are the mutually exclusive view flags, in the order argparse defined them.
var auditViews = []string{"cleanup", "sessions", "origin", "kpi", "only", "issue"}

// ownCwd is Path.cwd().resolve(): the cwd with its symlinks resolved.
func ownCwd() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(wd); err == nil {
		return real, nil
	}
	return wd, nil
}

// newWorktreeAuditCommand is `lighthouse worktree-audit`, the port of worktree_audit.py. env starts
// git, opens the forge, reads the config and measures pids.
func newWorktreeAuditCommand(env worktreeaudit.Env) *cobra.Command {
	var cleanup, sessions, origin, kpi bool
	var only, issue, repo string
	cmd := scriptCommand(&cobra.Command{
		Use:   "worktree-audit [--cleanup|--sessions|--origin|--kpi|--only <branch>|--issue <nr>] [--repo <path>]",
		Short: "Audit worktrees, branches, PR state and sessions. Changes nothing.",
		Long: "Audit worktrees, branches, PR state and sessions. Changes nothing.\n\n" +
			"One row per branch (local or on origin), plus detached worktrees and orphan directories under\n" +
			".claude/worktrees, each judged on a ladder whose first hit wins. Git runs read-only; the printed\n" +
			"commands are for a human to paste. The default branch comes from `[forge] default_branch`, the\n" +
			"protected refs and paths from `[worktree_audit]`, the repo of the PR lookup from `[forge] repo`.\n\n" +
			"Exit: 0 ok (--only: cleanable) | 1 --only: not cleanable, or git cannot read the repo |\n" +
			"2 wrong usage, a broken config, or --issue: the PR lookup failed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var chosen []string
			for _, name := range auditViews {
				if cmd.Flags().Changed(name) {
					chosen = append(chosen, name)
				}
			}
			if len(chosen) > 1 {
				return usageErr(fmt.Errorf("argument --%s: not allowed with argument --%s", chosen[1], chosen[0]))
			}
			nr := 0
			if cmd.Flags().Changed("issue") {
				var ok bool
				if nr, ok = pyInt(issue); !ok {
					return usageErr(fmt.Errorf("argument --issue: invalid int value: '%s'", issue))
				}
			}
			e := env
			e.Stderr = cmd.ErrOrStderr()
			if e.Home == "" {
				e.Home, _ = os.UserHomeDir()
			}
			audit, err := worktreeaudit.Collect(cmd.Context(), e, repo)
			if err != nil {
				var failure *worktreeaudit.Failure
				if errors.As(err, &failure) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), failure.Message)
					return &exitError{code: failure.Code, err: errSilent}
				}
				return err
			}
			view := worktreeaudit.View{Policy: audit.Policy, Alive: e.Alive}
			out := cmd.OutOrStdout()
			switch {
			case cmd.Flags().Changed("only"):
				cwd, err := ownCwd()
				if err != nil {
					return err
				}
				text, ok := view.Only(audit.Rows, only, cwd)
				if _, err := fmt.Fprint(out, text); err != nil {
					return err
				}
				if !ok {
					return &exitError{code: 1, err: errSilent}
				}
			case cmd.Flags().Changed("issue"):
				cwd, err := ownCwd()
				if err != nil {
					return err
				}
				heads, ok := worktreeaudit.IssuePRHeads(cmd.Context(), e, nr)
				if _, err := fmt.Fprint(out, view.Issue(audit.Rows, nr, heads, ok, cwd)); err != nil {
					return err
				}
				if !ok {
					return &exitError{code: 2, err: errSilent}
				}
			case sessions:
				_, err = fmt.Fprint(out, view.Sessions(audit.Rows))
			case origin:
				_, err = fmt.Fprint(out, view.Origin(audit.Rows))
			case kpi:
				_, err = fmt.Fprint(out, view.KPI(audit.Rows))
			default:
				_, err = fmt.Fprint(out, view.Cleanup(audit.Rows))
			}
			return err
		},
	})
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "full table + command blocks (default)")
	cmd.Flags().BoolVar(&sessions, "sessions", false, "claude --resume per row, sorted by last session")
	cmd.Flags().BoolVar(&origin, "origin", false, "remote rows only")
	cmd.Flags().BoolVar(&kpi, "kpi", false, "the KPI numbers, one line")
	cmd.Flags().StringVar(&only, "only", "", "one branch + cleanup: yes|no (exit 0 only on yes), for lighthouse-close-issue")
	cmd.Flags().StringVar(&issue, "issue", "", "all branches of an issue, one block each with cleanup: yes|no "+
		"(exit 2 if the PR lookup fails), for lighthouse-close-issue")
	cmd.Flags().StringVar(&repo, "repo", "", "path to the main checkout (default: from git worktree list)")
	return cmd
}
