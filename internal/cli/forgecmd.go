package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Exit codes of `lighthouse forge`: 0 is success.
const (
	forgeExitForge = 1 // the forge refused or failed; its message is on stderr
	forgeExitUsage = 2 // a wrong command line, or a missing configuration
)

// forgeEnv is what `lighthouse forge` takes from its surroundings, so a test can replace it.
// A nil out or err is the stream of the command.
type forgeEnv struct {
	open     func(ctx context.Context) (forge.Forge, error)
	out, err io.Writer
}

// stdout and stderr are the streams a subcommand writes to.
func (e *forgeEnv) stdout(cmd *cobra.Command) io.Writer {
	if e.out != nil {
		return e.out
	}
	return cmd.OutOrStdout()
}

func (e *forgeEnv) stderr(cmd *cobra.Command) io.Writer {
	if e.err != nil {
		return e.err
	}
	return cmd.ErrOrStderr()
}

// newDefaultForgeOpener is the opener of the commands that talk to the forge of this
// repository: tokens from the environment, and for a GitHub repo without one the token of
// `gh auth token`.
func newDefaultForgeOpener() *forgeOpener {
	o := newForgeOpener(os.Getenv, "")
	o.ghToken = ghAuthToken
	return o
}

// forgeOpenFrom opens the forge of the repo whose lighthouse.toml is at the git top of cwd.
// A missing or unreadable configuration, and a missing token, are exit 2.
func forgeOpenFrom(o *forgeOpener, cwd string) func(context.Context) (forge.Forge, error) {
	return func(ctx context.Context) (forge.Forge, error) {
		repo, err := repoconfig.Load(askRoot(cwd))
		if err != nil {
			return nil, &exitError{code: forgeExitUsage, err: err}
		}
		rf, err := o.forRepo(ctx, repo)
		var te *forge.TokenError
		if errors.As(err, &te) {
			return nil, &exitError{code: forgeExitUsage, err: err}
		}
		if err != nil {
			return nil, err
		}
		return rf.Forge, nil
	}
}

func defaultForgeEnv() *forgeEnv {
	cwd, _ := os.Getwd()
	return &forgeEnv{open: forgeOpenFrom(newDefaultForgeOpener(), cwd)}
}

// forgeUsage is a wrong command line: the message on stderr, exit 2.
func forgeUsage(cmd *cobra.Command, msg string) error {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: error: %s\n", cmd.CommandPath(), msg)
	return &exitError{code: forgeExitUsage, err: errSilent}
}

// forgeFail prints err on stderr and picks the exit code: an *exitError keeps its code, any
// other error is a forge error, exit 1.
func (e *forgeEnv) fail(cmd *cobra.Command, err error) error {
	code := forgeExitForge
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
	}
	_, _ = fmt.Fprintf(e.stderr(cmd), "%s: %v\n", cmd.CommandPath(), err)
	return &exitError{code: code, err: errSilent}
}

// newForgeCommand is `lighthouse forge`, the wrapper around the API of the repo's forge.
func newForgeCommand() *cobra.Command { return newForgeCommandWith(defaultForgeEnv()) }

func newForgeCommandWith(env *forgeEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forge",
		Short: "Read and write issues, pull requests and milestones of this repo's forge",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return forgeUsage(cmd, "a subcommand is required")
			}
			return forgeUsage(cmd, fmt.Sprintf("unknown command %q", args[0]))
		},
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error { return forgeUsage(c, err.Error()) })
	cmd.AddCommand(newForgeIssueCommand(env), newForgePRCommand(env), newForgeMilestoneCommand(env), newForgeLabelCommand(env))
	return cmd
}

// group is a parent command that only holds subcommands; with no or an unknown one it is
// exit 2.
func forgeGroup(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return forgeUsage(cmd, "a subcommand is required")
			}
			return forgeUsage(cmd, fmt.Sprintf("unknown command %q", args[0]))
		},
		RunE: func(*cobra.Command, []string) error { return nil },
	}
}

// forgeArgs checks the positional arguments of a leaf command: exactly n.
func forgeArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return forgeUsage(cmd, fmt.Sprintf("accepts %d argument(s), received %d", n, len(args)))
		}
		return nil
	}
}
