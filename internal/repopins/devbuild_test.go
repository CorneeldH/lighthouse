package repopins

import (
	"regexp"
	"strings"
	"testing"
)

// A dev build that also starts on a PR, a branch or a schedule builds code nobody merged.
func TestDevBuildTriggersOnMainPushOnly(t *testing.T) {
	on := onBlock(t, workflow(t, "dev-build.yml"))
	mustContain(t, "dev-build.yml on:", on, "push:", "branches: [main]")
	mustNotContain(t, "dev-build.yml on:", on, "pull_request", "schedule", "workflow_dispatch", "workflow_call")
}

// Two merges close together that run in different groups, or that cancel each other, number the
// same tag twice or skip a build.
func TestDevBuildSharesOneConcurrencyGroup(t *testing.T) {
	mustContain(t, "dev-build.yml", workflow(t, "dev-build.yml"), "group: dev-build\n", "cancel-in-progress: false")
}

// An `internal` fragment (no changelog entry) or a push without a fragment must not start a
// build; a releasable type must.
// The fragment directory is spelled in two pieces: ci/domains keeps Go from naming it as a path.
func TestDevBuildSkipsInternalOnly(t *testing.T) {
	decide := jobOf(t, "dev-build.yml", "decide")
	run := strings.Join(runBlocks(decide), "\n")
	mustContain(t, "decide run", run, "git diff-tree", "--diff-filter=A", "changelog"+".d/", "go run ./ci/changelog released", "go run ./ci/changelog next --kind dev")
	mustMatch(t, "decide run", run, `\(added\|changed\|fixed\|removed\)`)
	mustNotContain(t, "decide run", run, "internal")
	mustMatch(t, "decide job", decide, `^      tag: \$\{\{ steps\.decide\.outputs\.tag \}\}\s*$`)
	if regexp.MustCompile(`contents: write|persist-credentials: true`).MatchString(decide) {
		t.Error("decide job can push")
	}
	mustContain(t, "decide job", decide, "fetch-depth: 0")
}

// A build job that stops calling prerelease.yml with the decided tag, or runs without one,
// publishes nothing or a build without a tag.
func TestDevBuildCallsPrerelease(t *testing.T) {
	build := jobOf(t, "dev-build.yml", "build")
	mustMatch(t, "build job", build, `^    needs: decide\s*$`)
	mustMatch(t, "build job", build, `if: needs\.decide\.outputs\.tag != ''`)
	mustContain(t, "build job", build, "contents: write", "uses: ./.github/workflows/prerelease.yml")
	mustMatch(t, "build job", build, `^      tag: \$\{\{ needs\.decide\.outputs\.tag \}\}\s*$`)
}
