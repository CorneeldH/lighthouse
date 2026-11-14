//go:build !windows

package repoidentity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// twin of test_cli_exitcodes: the Python CLI gave exit 0 for the same origin, 1 plus the reason
// for another, and 2 when git failed. SameOrigin carries the same three outcomes.
func TestSameOriginOutcomes(t *testing.T) {
	base := t.TempDir()
	fixturesetup.Build(t, base, []map[string]any{
		{"repo": "project", "branch": "main"},
		{"origin": "project", "url": "git@github.com:Acme/widgets.git"},
		{"repo": "ander", "branch": "main"},
		{"repo": "broken", "branch": "main"},
	})
	project := filepath.Join(base, "project")
	// A worktree of a separate bare clone whose origin is the https form of the same repo.
	clone := filepath.Join(base, "clone.git")
	fixturesetup.Git(t, base, "clone", "-q", "--bare", project, clone)
	fixturesetup.Git(t, clone, "remote", "set-url", "origin", "https://github.com/Acme/widgets.git")
	work := filepath.Join(base, "work")
	fixturesetup.Git(t, clone, "worktree", "add", "-q", work, "-b", "work")

	ctx := context.Background()
	if same, reason, err := SameOrigin(ctx, project, work); err != nil || !same || reason != "" {
		t.Errorf("same repo: %v %q %v, want true", same, reason, err)
	}
	same, reason, err := SameOrigin(ctx, project, filepath.Join(base, "ander"))
	if err != nil || same || reason != "origin none vs git@github.com:Acme/widgets.git" {
		t.Errorf("other repo: %v %q %v", same, reason, err)
	}
	if err := os.WriteFile(filepath.Join(base, "broken", ".git", "config"), []byte("[kapot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = SameOrigin(ctx, project, filepath.Join(base, "broken"))
	var oe *OriginError
	if !errors.As(err, &oe) || oe.Error() == "" {
		t.Errorf("broken config: err = %v, want an *OriginError with text", err)
	}
}
