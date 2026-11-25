package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/gitpath"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

// defaultPinsEnv is the real environment of `pins check`: real git, the GitHub release API with
// the token of the environment (anonymous without one) and real commands.
func defaultPinsEnv(root string) pins.Env {
	token, _ := forge.Token("github", "", os.Getenv) // no token is allowed: anonymous requests
	return pins.Env{Root: root, Git: pins.NewGitSource(), Releases: forge.New("", token), Run: pins.ExecRunner}
}

func newPinsCommand(env func(root string) pins.Env, laneEnv laneSetup) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pins",
		Short: "Check what lighthouse took over from other repositories and tools",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newPinsCheckCommand(env), newPinsLaneCommand(laneEnv))
	return cmd
}

func newPinsCheckCommand(env func(root string) pins.Env) *cobra.Command {
	var opts pins.CheckOptions
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Measure every pin upstream and locally",
		Long: "Measure every pin: skills and files against their upstream repository, plugins and tools\n" +
			"against their latest release and against what is installed here. Each measurement has its own\n" +
			"status (current, behind, ahead, error); they are never merged. Exit 1 when one is an error.\n" +
			"Without --upstream and --local both run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := repoRoot(dir)
			if err != nil {
				return err
			}
			rs := pins.Check(cmd.Context(), env(root), opts)
			if asJSON {
				if err := pins.RenderJSON(cmd.OutOrStdout(), rs); err != nil {
					return err
				}
			} else {
				pins.RenderText(cmd.OutOrStdout(), rs)
			}
			for _, r := range rs {
				if r.HasError() {
					return &exitError{code: 1, err: errSilent}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.Upstream, "upstream", false, "measure against upstream (network)")
	cmd.Flags().BoolVar(&opts.Local, "local", false, "measure against what is installed here (no network)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON instead of text")
	return cmd
}

// repoRoot is the top level of the git repository that contains dir.
func repoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository", dir)
	}
	return gitpath.Native(string(out)), nil
}
