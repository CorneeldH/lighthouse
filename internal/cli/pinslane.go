package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/pins"
	"github.com/CorneeldH/lighthouse/internal/pins/lane"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// laneSetup builds the environment of `pins lane` for the repository at root. An error means
// the lane cannot run: nothing has been read from or written to the forge yet.
type laneSetup func(root string) (lane.Env, error)

// laneEnvFrom is the real environment of `pins lane`: the forge of `[forge]` in lighthouse.toml
// with the token getenv gives (none is an error, unlike `pins check`), real git, and the release
// API with the same token, and [ci] bot_login as the only author whose items count as lane
// items (not set: every author). githubURL overrides the GitHub API root; "" is the default.
func laneEnvFrom(getenv func(string) string, githubURL string) laneSetup {
	return func(root string) (lane.Env, error) {
		repo, err := repoconfig.Load(root)
		if err != nil {
			return lane.Env{}, err
		}
		token, err := forge.Token(repo.Kind, repo.TokenEnv, getenv)
		if err != nil {
			return lane.Env{}, err
		}
		f, err := forge.Open(forge.Config{Kind: repo.Kind, Repo: repo.Slug, Host: repo.Host, Token: token, BaseURL: githubURL})
		if err != nil {
			return lane.Env{}, err
		}
		rel := forge.New("", token)
		if githubURL != "" {
			rel.BaseURL = githubURL
		}
		bot, err := botLogin(root)
		if err != nil {
			return lane.Env{}, err
		}
		return lane.Env{Root: root, Git: pins.NewGitSource(), Releases: rel, Forge: f, BotLogin: bot}, nil
	}
}

// botLogin is [ci] bot_login of the lighthouse.toml in root; "" when it is not set.
func botLogin(root string) (string, error) {
	c, err := config.Load(filepath.Join(root, "lighthouse.toml"))
	if err != nil {
		return "", err
	}
	v, err := config.Lookup(c, "ci.bot_login")
	if errors.Is(err, config.ErrNotSet) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

func newPinsLaneCommand(setup laneSetup) *cobra.Command {
	return &cobra.Command{
		Use:   "lane",
		Short: "Measure every pin upstream and write its issue or draft PR",
		Long: "Measure every pin against upstream and write what it asks for, on the forge of [forge]:\n" +
			"a draft PR on pins/<key> for a skill or file whose merge is clean, an issue for a conflict,\n" +
			"for a plugin or tool behind its release and for a pin that cannot be measured. It never\n" +
			"merges. Run it in a checkout of the default branch's head, with a token that may write\n" +
			"contents, pull requests and issues. Exit 1 when a pin failed; the other pins still run.",
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
			env, err := setup(root)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "pins lane: COULD NOT CHECK — %s\n", err)
				return &exitError{code: 1, err: errSilent}
			}
			env.Log = cmd.OutOrStdout()
			if err := lane.Run(cmd.Context(), env); err != nil {
				return &exitError{code: 1, err: err}
			}
			return nil
		},
	}
}
