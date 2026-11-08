package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/version"
)

// newHookCommand is `lighthouse hook <name> [stand]`. The hook writes its own messages and the
// exit code is returned silently.
func newHookCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "hook <name> [stand]",
		Short: "Run one hook of the harness",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			stand := ""
			if len(args) == 2 {
				stand = args[1]
			}
			code := hook.Run(cmd.Context(), args[0], stand, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), hookEnv())
			if code == 0 {
				return nil
			}
			return &exitError{code: code, err: errSilent}
		},
	}
}

// hookEnv gathers the state of the repository and the binary. What cannot be read stays empty:
// a hook outside a repository still runs, and the notices that need the manifest stay quiet.
// Without a state directory the notices stay quiet too.
func hookEnv() hook.Env {
	env := hook.Env{BinaryVersion: version.String()}
	if dir, err := hookStateDir(); err == nil {
		env.StateDir = dir
	}
	root, err := config.RepoRoot()
	if err != nil {
		return env
	}
	env.Root = root
	if m, err := manifest.Read(root); err == nil {
		env.ManifestVersion = m.Version
	}
	return env
}

// hookStateDir is lighthouse/hook-notices under XDG_STATE_HOME, or under $HOME/.local/state
// when XDG_STATE_HOME is not an absolute path. Without an absolute HOME it is an error: a
// shared directory such as /tmp is no place for markers of one user.
func hookStateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := os.Getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("neither XDG_STATE_HOME nor HOME is an absolute path: no place for the hook notices")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "lighthouse", "hook-notices"), nil
}
