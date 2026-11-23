package cli

import (
	"context"
	"os"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/prbody"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// defaultLabelForge opens the forge for `init --labels`: tokens from the environment, then
// `gh auth token` for a GitHub repo, and a GitHub API URL only when it is loopback.
func defaultLabelForge() func(root string) forge.Forge {
	o := newForgeOpener(os.Getenv, prbody.LoopbackAPIURL(os.Getenv(prbody.GitHubAPIURLEnv)))
	o.ghToken = ghAuthToken
	return labelForgeWith(o)
}

// labelForgeWith opens the forge of the lighthouse.toml that config.Path names. It returns nil
// when the forge cannot be opened (no configuration, no forge.repo, no token): init never fails
// on that, it prints the missing labels instead.
func labelForgeWith(o *forgeOpener) func(root string) forge.Forge {
	return func(string) forge.Forge {
		path, err := config.Path()
		if err != nil {
			return nil
		}
		repo, err := repoconfig.LoadFile(path)
		if err != nil || repo.Slug == "" {
			return nil
		}
		rf, err := o.forRepo(context.Background(), repo)
		if err != nil {
			return nil
		}
		return rf.Forge
	}
}
