package cli

import (
	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/boardstart"
	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// newBoardStartCommand is `lighthouse board-start <nr>` and `lighthouse board-start --reopen <nr>`,
// the port of board_start.py. The arguments are raw (no flag parsing): `<nr>` and `--reopen <nr>`
// are positional there, and a wrong one is a usage line on stdout, not a cobra error.
func newBoardStartCommand() *cobra.Command { return newBoardStartCommandWith(newDefaultForgeOpener()) }

// newBoardStartCommandWith opens the forge with o: the repo, board and token_env of
// lighthouse.toml (LIGHTHOUSE_TOML, else the repo root), and for GitHub the token of o's
// ghToken when no variable holds one.
func newBoardStartCommandWith(o *forgeOpener) *cobra.Command {
	return &cobra.Command{
		Use:   "board-start <nr> | --reopen <nr>",
		Short: "Move a picked-up issue to In progress on the project board (--reopen: from Done)",
		Long: "Without --reopen: To do and On hold become In progress; never fails hard, always exit 0.\n" +
			"With --reopen: Done becomes In progress, fail-closed (exit 1 on any deviation).\n" +
			"Without [forge.board] both print `board: skipped` and exit 0; a broken config exits 2.",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			env := boardstart.Env{Config: boardstart.LoadConfig, Forge: forgeOpenWith(o, loadRepoAtConfigPath)}
			code := boardstart.Run(cmd.Context(), env, args, cmd.OutOrStdout())
			if code == 0 {
				return nil
			}
			return &exitError{code: code, err: errSilent}
		},
	}
}

// loadRepoAtConfigPath reads the lighthouse.toml of config.Path as a repoconfig.Repo, which
// carries token_env.
func loadRepoAtConfigPath() (repoconfig.Repo, error) {
	path, err := config.Path()
	if err != nil {
		return repoconfig.Repo{}, err
	}
	return repoconfig.LoadFile(path)
}
