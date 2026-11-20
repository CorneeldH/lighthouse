package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// defaultReleasesRepo is the repository `lighthouse releases` reads without --repo, the same
// one self-update downloads from.
const defaultReleasesRepo = "CorneeldH/lighthouse"

// newReleasesCommand is `lighthouse releases`, with the token of the environment or of
// `gh auth token`: anonymous requests allow 60 an hour, too few for `upgrade`.
func newReleasesCommand() *cobra.Command { return newReleasesCommandWith(lazyReleaseReader{}) }

// lazyReleaseReader resolves the token only when the command runs, so building the root does
// not start `gh auth token` for every other command.
type lazyReleaseReader struct{}

func (lazyReleaseReader) Releases(ctx context.Context, repo string) ([]forge.Release, error) {
	token, _ := forge.Token("github", "", os.Getenv)
	if token == "" {
		token = ghAuthToken()
	}
	return forge.New("", token).Releases(ctx, repo)
}

func newReleasesCommandWith(rr forge.ReleaseReader) *cobra.Command {
	var repo, field string
	cmd := &cobra.Command{
		Use:   "releases",
		Short: "List the releases of lighthouse (or another repo) as JSON, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rels, err := rr.Releases(cmd.Context(), repo)
			var text string
			if err == nil {
				items := make([]any, len(rels))
				for i, r := range rels {
					items[i] = map[string]any{
						"tag": r.Tag, "name": r.Name, "body": r.Body, "url": r.URL,
						"draft": r.Draft, "prerelease": r.Prerelease,
					}
				}
				text, err = forgeResult{value: items, field: field}.render()
			}
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", cmd.CommandPath(), err)
				return &exitError{code: forgeExitForge, err: errSilent}
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	cmd.Flags().StringVar(&repo, "repo", defaultReleasesRepo, "repository as owner/name")
	addFieldFlag(cmd, &field)
	return cmd
}
