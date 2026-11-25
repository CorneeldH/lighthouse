package repopins

import (
	"regexp"
	"strings"
	"testing"
)

// A push trigger on release/* runs the frozen copy of the train from the release branch, so a fix
// of the release logic would need a cherry-pick and a new rc; without the cron, the milestone
// event and the dispatch the train never starts.
func TestTrainTriggers(t *testing.T) {
	on := onBlock(t, workflow(t, "release-train.yml"))
	mustContain(t, "release-train.yml on:", on, "milestone:", "schedule:", "workflow_dispatch:")
	mustNotContain(t, "release-train.yml on:", on, "push", "pull_request")
	mustMatch(t, "release-train.yml on:", on, `(?s)milestone:\s*\n\s+types: \[closed\]`)
	mustMatch(t, "release-train.yml on:", on, `(?s)action:.*options: \[split, reconcile, release\]`)
	mustMatch(t, "release-train.yml on:", on, `(?s)kind:.*options: \[minor, major\]`)
	mustContain(t, "release-train.yml", workflow(t, "release-train.yml"), "group: release-train", "cancel-in-progress: false")
}

// A train that takes its logic from the release branch uses the version frozen at the split.
func TestTrainChecksOutLogicFromMain(t *testing.T) {
	text := workflow(t, "release-train.yml")
	for _, name := range []string{"split", "plan"} {
		checkout := stepUsing(t, jobOf(t, "release-train.yml", name), "actions/checkout@")
		mustMatch(t, name+" checkout", checkout, `^\s+ref: main\s*$`)
	}
	pr := jobOf(t, "release-train.yml", "pr")
	var logic, branch string
	for _, s := range steps(pr) {
		switch {
		case strings.Contains(s, "path: logic"):
			logic = s
		case strings.Contains(s, "path: branch"):
			branch = s
		}
	}
	mustMatch(t, "pr logic checkout", logic, `^\s+ref: main\s*$`)
	mustMatch(t, "pr branch checkout", branch, `^\s+ref: \$\{\{ matrix\.branch \}\}\s*$`)
	// The Go code that decides always runs from the checkout of main.
	mustContain(t, "pr", pr, `cd "$GITHUB_WORKSPACE/logic" && go run ./ci/changelog`)
	mustNotContain(t, "release-train.yml", text, "ref: release/")
}

// A release PR opened with GITHUB_TOKEN starts no CI run and can never merge; the token is that of
// the GitHub App of `[ci]`, and the PR is made with it.
func TestTrainOpensPRsWithBotToken(t *testing.T) {
	pr := jobOf(t, "release-train.yml", "pr")
	mustContain(t, "pr job", pr, "actions/create-github-app-token@",
		"client-id: ${{ vars.PAIA_BOT_CLIENT_ID }}", "private-key: ${{ secrets.PAIA_BOT_PRIVATE_KEY }}",
		"GH_TOKEN: ${{ steps.app.outputs.token }}", "token: ${{ steps.app.outputs.token }}")
	mustNotContain(t, "pr job", pr, "github.token", "secrets.GITHUB_TOKEN")
	run := strings.Join(runBlocks(pr), "\n")
	mustContain(t, "pr run", run, "gh pr create", `--title "chore(release): $version"`, "--body-file")
	mustMatch(t, "pr run", run, `gh pr close`)
}

// A split on the close of any milestone would cut a release branch for work nobody called a release.
func TestTrainMilestoneJobOnlyOnClosed(t *testing.T) {
	split := jobOf(t, "release-train.yml", "split")
	mustContain(t, "split job", split, "github.event_name == 'milestone'", "inputs.action == 'split'",
		"go run ./ci/changelog milestone-release", "DESCRIPTION: ${{ github.event.milestone.description }}")
	inOrder(t, "split job", split, "milestone-release", "ci/changelog split", "git push origin")
	for _, name := range []string{"plan", "pr", "tag-rc", "release"} {
		job := jobOf(t, "release-train.yml", name)
		mustNotContain(t, "job "+name, job, "github.event.milestone")
	}
}

// A release.yml that still releases on a push to main or on a schedule releases on a `fixed` merge
// again, which is what the new policy removes.
func TestReleaseHasNoPushOrScheduleTrigger(t *testing.T) {
	on := onBlock(t, workflow(t, "release.yml"))
	mustContain(t, "release.yml on:", on, "workflow_call:", "workflow_dispatch:")
	mustNotContain(t, "release.yml on:", on, "push", "schedule", "cron")
	mustMatch(t, "release.yml on:", on, `(?s)workflow_dispatch:\s*\n\s+inputs:\s*\n\s+branch:`)
	mustNotContain(t, "release.yml", workflow(t, "release.yml"), "--event", "--bump", "branches: [main]")
}

// A milestone release (vX.Y.0) that takes work merged after the rc was built ships code nobody
// soaked; a patch release has no rc and no such check.
func TestReleaseRefusesWhenParentIsNotTheRCSource(t *testing.T) {
	release := jobOf(t, "release.yml", "release")
	mustContain(t, "release job", release, `*.0)`, `git rev-parse HEAD^`, `"$tag-rc.$rc^"`, "something was merged after the soak")
	inOrder(t, "release job", release, "*.0)", "git rev-parse HEAD^", tagWithUI, "git push --atomic")
	mustMatch(t, "release job", release, `(?s)\*\.0\).*?exit 1.*?;;`)
}

// A sync folded into the tag step fails the release when main refuses the push; the sync stays a
// job of its own after the tag, with its own retry, and works only from the tag.
func TestReleaseSyncIsSeparateStep(t *testing.T) {
	sync := jobOf(t, "release.yml", "sync")
	mustMatch(t, "sync job", sync, `^    needs: \[resolve, release\]\s*$`)
	mustContain(t, "sync job", sync, "for attempt in 1 2 3", "go run ./ci/changelog sync", `refs/tags/${{ needs.resolve.outputs.version }}`,
		"git push origin HEAD:refs/heads/main", "contents: write")
	release := jobOf(t, "release.yml", "release")
	mustNotContain(t, "release job", release, "changelog sync", "refs/heads/main")
	inOrder(t, "sync job", sync, "git reset --hard origin/main", "go run ./ci/changelog sync", "git push origin HEAD:refs/heads/main")
	// build and sync do not wait for one another
	mustNotContain(t, "sync job", sync, "needs.build")
	mustNotContain(t, "build job", jobOf(t, "release.yml", "build"), "sync")
}

// A recovery dispatch without the branch input cannot repeat the release of a merged release PR
// that got no tag, and one that takes a tip which is no release commit tags the wrong thing.
func TestReleaseRecoveryDispatchTakesBranch(t *testing.T) {
	text := workflow(t, "release.yml")
	mustMatch(t, "release.yml", text, `(?s)workflow_dispatch:\s*\n\s+inputs:\s*\n\s+branch:\s*\n\s+description: [^\n]+\n\s+type: string\n\s+required: true`)
	resolve := jobOf(t, "release.yml", "resolve")
	mustContain(t, "resolve job", resolve, `^release/v[0-9]+\.[0-9]+$`, "not a chore(release) commit", "BRANCH: ${{ inputs.branch }}")
	// The tag is the version of the release commit on the tip, nothing else.
	mustMatch(t, "resolve job", resolve, `chore\\\(release\\\): v`)
	if regexp.MustCompile(`\$\{\{\s*inputs\.branch`).MatchString(strings.Join(runBlocks(text), "\n")) {
		t.Error("release.yml interpolates the branch input in a run script")
	}
	train := jobOf(t, "release-train.yml", "plan")
	mustContain(t, "plan job", train, "ACTION: ${{ inputs.action }}", "action release needs a branch")
}
