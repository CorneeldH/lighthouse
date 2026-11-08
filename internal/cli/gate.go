package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/gate"
)

// gateEnv is the environment a gate runs in: the cwd, the file list of the commit and the
// streams of the command.
func gateEnv(cmd *cobra.Command) (gate.Env, error) {
	dir, err := os.Getwd()
	if err != nil {
		return gate.Env{}, err
	}
	return gate.Env{
		Dir:       dir,
		FilesPath: os.Getenv("LIGHTHOUSE_GATE_FILES"),
		Stdout:    cmd.OutOrStdout(),
		Stderr:    cmd.ErrOrStderr(),
	}, nil
}

// newGateCommand is `lighthouse gate <name> -- CMD...`, the test-gate selectors. Flag parsing
// is off in a subcommand: `-race` and `--` after the name belong to CMD, not to cobra.
func newGateCommand() *cobra.Command {
	gateCmd := &cobra.Command{
		Use:   "gate <name> [args]",
		Short: "Run a test-gate selector",
		Args:  cobra.ArbitraryArgs,
		// Without RunE cobra prints the help and returns 0: a silently green gate.
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "(none)"
			if len(args) > 0 {
				name = args[0]
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "gate: COULD NOT CHECK — unknown gate %s\n", name)
			return &exitError{code: gate.Unchecked, err: errSilent}
		},
	}
	gateCmd.AddCommand(&cobra.Command{
		Use:                "go -- CMD...",
		Short:              "Run CMD on the Go packages the commit touches",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := gateEnv(cmd)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "gate-go: COULD NOT CHECK — %s\n", err)
				return &exitError{code: gate.Unchecked, err: errSilent}
			}
			if rc := gate.Go(args, env); rc != 0 {
				return &exitError{code: rc, err: errSilent}
			}
			return nil
		},
	})
	return gateCmd
}
