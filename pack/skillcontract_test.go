package pack

import (
	"io/fs"
	"strings"
	"testing"
)

// The workflow route must never end in a push-workflows handoff above a skip
// record: the pusher cannot push what the review gate did not clear (#551).
func TestWorkflowRouteSkillsForbidPushWorkflowsOnSkip(t *testing.T) {
	for _, path := range []string{
		"core/skills/write-pr/SKILL.md",
		"core/skills/write-handoff/SKILL.md",
	} {
		b, err := fs.ReadFile(Files, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"no handoff with `next: push-workflows`", "never to the pusher"} {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
	}
}
