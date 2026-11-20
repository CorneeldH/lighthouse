package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/postartefact"
	"github.com/CorneeldH/lighthouse/internal/prbody"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/pytext"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// pyInt is int(text) for the issue number: surrounding whitespace, a sign and single
// underscores between digits are fine, as in Python.
func pyInt(text string) (int, bool) {
	s := pytext.Strip(text)
	digits := strings.TrimLeft(s, "+-")
	if len(s)-len(digits) > 1 || digits == "" || strings.HasPrefix(digits, "_") || strings.HasSuffix(digits, "_") ||
		strings.Contains(digits, "__") {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-len(digits)] + strings.ReplaceAll(digits, "_", ""))
	return n, err == nil && !strings.ContainsAny(digits, "+-")
}

func defaultPostArtefactEnv() postartefact.Env {
	o := newForgeOpener(os.Getenv, prbody.LoopbackAPIURL(os.Getenv(prbody.GitHubAPIURLEnv)))
	o.ghToken = ghAuthToken
	return postArtefactEnvWith(o)
}

// postArtefactEnvWith is the environment of post-artefact with o as the opener of the forge.
// The repo is the one of the lighthouse.toml that config.Path names (LIGHTHOUSE_TOML is honoured),
// not of the working directory; git runs for real.
func postArtefactEnvWith(o *forgeOpener) postartefact.Env {
	return postartefact.Env{
		Run: procrun.Exec(postartefact.RunTimeout),
		Forge: func(string) (forge.Forge, error) {
			path, err := config.Path()
			if err != nil {
				return nil, err
			}
			repo, err := repoconfig.LoadFile(path)
			if err != nil {
				return nil, err
			}
			rf, err := o.forRepo(context.Background(), repo)
			if err != nil {
				return nil, err
			}
			return rf.Forge, nil
		},
		Config: func() (*config.Config, error) {
			path, err := config.Path()
			if err != nil {
				return nil, err
			}
			return config.Load(path)
		},
	}
}

// newPostArtefactCommand is `lighthouse post-artefact <nr> <file>`, the port of post_artefact.py.
// env starts `git`, opens the forge and reads the configuration; a test hands in a replay.
func newPostArtefactCommand(env postartefact.Env) *cobra.Command {
	cmd := scriptCommand(&cobra.Command{
		Use:   "post-artefact <nr> <file>",
		Short: "Post a committed spec or plan file as a version chain of issue comments",
		Long: "Post a committed spec or plan file as a version chain of issue comments.\n\n" +
			"  nr    issue number\n  file  spec or plan file\n\n" +
			"The file must be committed and clean, in the spec or plan directory of the config. A commit\n" +
			"that is not on the forge is pushed, except from the default branch, a detached HEAD or a branch\n" +
			"with an open PR. Unchanged content posts nothing.\n\n" +
			"Exit: 0 posted or unchanged | 1 every `artefact:` error (on stderr) | 2 wrong usage.",
		RunE: func(cmd *cobra.Command, args []string) error {
			nr, ok := pyInt(args[0])
			if !ok {
				return usageErr(fmt.Errorf("%s: argument nr: invalid int value: %q", cmd.CommandPath(), args[0]))
			}
			line, err := postartefact.Run(cmd.Context(), env, nr, args[1])
			if err != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &exitError{code: 1, err: errSilent}
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), line)
			return err
		},
	})
	return withArgs(cmd, func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return usageErr(fmt.Errorf("%s: the following arguments are required: nr, file", cmd.CommandPath()))
		}
		return nil
	})
}
