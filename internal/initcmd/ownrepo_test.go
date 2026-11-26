package initcmd

import (
	"path/filepath"
	"testing"
)

// TestOwnWorkflowsAreHosted pins that lighthouse's own workflows run on hosted runners: a
// self-hosted workflow without a dependabot[bot] guard leaves a Dependabot PR waiting for a
// runner it cannot use. It fails as soon as someone adds one without the guard.
func TestOwnWorkflowsAreHosted(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	got, err := selfHostedWorkflows(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("workflows with runs-on self-hosted and no dependabot[bot] guard: %v", got)
	}
}
