package repopins

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

const prereleaseTagRE = `^v[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.-]+$`

const tagWithUI = `ci/tag-with-ui.sh "$tag" "$RUNNER_TEMP/ui-dist"`

// workflow returns the text of a workflow file under .github/workflows.
func workflow(t *testing.T, name string) string {
	t.Helper()
	return read(t, ".github", "workflows", name)
}

// jobOf returns one job of a workflow and fails when the workflow has no such job.
func jobOf(t *testing.T, file, name string) string {
	t.Helper()
	body, ok := jobs(workflow(t, file))[name]
	if !ok {
		t.Fatalf("%s has no job %q", file, name)
	}
	return body
}

// steps cuts a job into its steps: each block that starts at a six-space `- ` line under `steps:`.
func steps(job string) []string {
	_, body, found := strings.Cut(job, "\n    steps:\n")
	if !found {
		return nil
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "      - ") {
			out = append(out, line)
		} else if len(out) > 0 {
			out[len(out)-1] += "\n" + line
		}
	}
	return out
}

// stepUsing returns the first step of a job that contains sub, and fails when there is none.
func stepUsing(t *testing.T, job, sub string) string {
	t.Helper()
	for _, s := range steps(job) {
		if strings.Contains(s, sub) {
			return s
		}
	}
	t.Fatalf("no step containing %q in job:\n%s", sub, job)
	return ""
}

// stepNamed returns the index of the step called name, and fails when there is none.
func stepNamed(t *testing.T, all []string, name string) int {
	t.Helper()
	for i, s := range all {
		if strings.SplitN(s, "\n", 2)[0] == "      - name: "+name {
			return i
		}
	}
	t.Fatalf("no step named %q", name)
	return -1
}

// mustContain fails for each needle that text lacks.
func mustContain(t *testing.T, where, text string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(text, n) {
			t.Errorf("%s lacks %q", where, n)
		}
	}
}

// mustNotContain fails for each needle that text has.
func mustNotContain(t *testing.T, where, text string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(text, n) {
			t.Errorf("%s has %q", where, n)
		}
	}
}

// mustMatch fails when the regexp (multiline) does not match text.
func mustMatch(t *testing.T, where, text, pattern string) {
	t.Helper()
	if !regexp.MustCompile("(?m)" + pattern).MatchString(text) {
		t.Errorf("%s does not match /%s/", where, pattern)
	}
}

// inOrder fails unless every needle is in text, first occurrences in the given order.
func inOrder(t *testing.T, where, text string, needles ...string) {
	t.Helper()
	prev := -1
	for _, n := range needles {
		i := strings.Index(text, n)
		if i < 0 {
			t.Errorf("%s lacks %q", where, n)
			return
		}
		if i < prev {
			t.Errorf("%s has %q before an earlier step it must follow (needles %q)", where, n, needles)
			return
		}
		prev = i
	}
}

// topOf returns a workflow's text before `jobs:`.
func topOf(text string) string {
	top, _, _ := strings.Cut(text, "\njobs:\n")
	return top
}

// onBlock returns the `on:` block of a workflow, without its comments above and the permissions below.
func onBlock(t *testing.T, text string) string {
	t.Helper()
	_, rest, found := strings.Cut(text, "\non:\n")
	if !found {
		t.Fatal("workflow has no `on:` block")
	}
	block, _, _ := strings.Cut(rest, "\npermissions:")
	return block
}

// A release that loses a trigger stops releasing; one that cancels in progress tears a tag push.
func TestReleaseTriggersAndConcurrency(t *testing.T) {
	mustContain(t, "release.yml", workflow(t, "release.yml"),
		"branches: [main]",
		`cron: "0 6 * * 1"`,
		"workflow_dispatch",
		"group: release-${{ github.event_name }}",
		"cancel-in-progress: false",
	)
}

// A push that is no longer atomic, or a tag made before the UI commit, tags a main that was
// never pushed (or pushes main without its tag).
func TestReleasePushesAtomically(t *testing.T) {
	release := jobOf(t, "release.yml", "release")
	mustContain(t, "release job", release,
		`git push --atomic origin "$main_sha:refs/heads/main" "refs/tags/$tag"`,
		"for attempt in 1 2 3",
	)
	mustNotContain(t, "release job", release, `git tag -a "$tag"`)
	inOrder(t, "release job", release, "git commit -m", "main_sha=$(git rev-parse HEAD)", tagWithUI, "git push --atomic")
}

// A `ui` job with write rights, an unpinned checkout or no ui-dist upload builds the SPA with
// more power than it needs, or ships a release without it.
func TestUIJobBuildsInAReadOnlyJob(t *testing.T) {
	ui := jobOf(t, "release.yml", "ui")
	mustMatch(t, "ui job", ui, `^    needs: macos\s*$`)
	mustContain(t, "ui job", ui, "contents: read", "node-version-file: web/.nvmrc")
	mustNotContain(t, "ui job", ui, "contents: write")
	checkout := stepUsing(t, ui, "actions/checkout@")
	mustMatch(t, "ui checkout", checkout, `^\s+ref: \$\{\{ github\.sha \}\}\s*$`)
	mustContain(t, "ui checkout", checkout, "persist-credentials: false")
	for _, command := range []string{"npm ci", "npm run build"} {
		var own string
		for _, s := range steps(ui) {
			if regexp.MustCompile(`(?m)^\s+(?:- )?run: ` + regexp.QuoteMeta(command) + `\s*$`).MatchString(s) {
				own = s
				break
			}
		}
		if own == "" {
			t.Errorf("ui has no own step `run: %s`", command)
			continue
		}
		mustMatch(t, "ui step "+command, own, `^\s+working-directory: web\s*$`)
	}
	upload := stepUsing(t, ui, "actions/upload-artifact@")
	for _, line := range []string{"name: ui-dist", "path: internal/web/dist", "if-no-files-found: error"} {
		mustMatch(t, "ui upload", upload, `^\s+`+regexp.QuoteMeta(line)+`\s*$`)
	}
}

// upload-artifact drops dotfiles by default; without `include-hidden-files` the release ships
// a UI bundle that lacks them.
func TestUIJobUploadsHiddenFiles(t *testing.T) {
	for _, file := range []string{"release.yml", "prerelease.yml"} {
		upload := stepUsing(t, jobOf(t, file, "ui"), "actions/upload-artifact@")
		mustMatch(t, file+" ui upload", upload, `^\s+include-hidden-files: true\s*$`)
	}
}

// A job that can write and also runs npm or setup-node runs third-party install scripts with
// the release token in reach; `release` without the ui-dist download tags a build without UI.
func TestWritingJobsRunNoNPM(t *testing.T) {
	for file, name := range map[string]string{"release.yml": "release", "prerelease.yml": "tag"} {
		job := jobOf(t, file, name)
		mustNotContain(t, file+" "+name, job, "npm", "setup-node")
	}
	release := jobOf(t, "release.yml", "release")
	mustMatch(t, "release job", release, `^    needs: \[macos, ui\]\s*$`)
	download := stepUsing(t, release, "actions/download-artifact@")
	mustMatch(t, "release download", download, `^\s+name: ui-dist\s*$`)
	mustMatch(t, "release download", download, `^\s+path: \$\{\{ runner\.temp \}\}/ui-dist\s*$`)
}

// A retry that rebuilds the SPA, or tags before it has reset to origin/main, tags a stale tree.
func TestReleaseRetryDoesNotRebuild(t *testing.T) {
	release := jobOf(t, "release.yml", "release")
	_, loop, found := strings.Cut(release, "for attempt in 1 2 3")
	if !found {
		t.Fatal("release has no retry loop")
	}
	mustNotContain(t, "retry loop", loop, "npm")
	inOrder(t, "retry loop", loop, "git reset --hard origin/main", tagWithUI)
}

// A second job that keeps the token in .git/config, or a checkout without
// `persist-credentials: false`, lets later steps push with the release token.
func TestOnlyReleaseJobPersistsCredentials(t *testing.T) {
	persisting := map[string]bool{"release.yml/release": true, "prerelease.yml/tag": true}
	total := 0
	for _, file := range []string{"release.yml", "build.yml", "macos.yml", "prerelease.yml"} {
		text := workflow(t, file)
		for name, job := range jobs(text) {
			n := strings.Count(job, "persist-credentials: true")
			total += n
			if n > 0 && !persisting[file+"/"+name] {
				t.Errorf("%s job %s persists credentials", file, name)
			}
		}
		checkouts := len(regexp.MustCompile(`uses:\s*actions/checkout@`).FindAllString(text, -1))
		exempt := 0 // the one pushing checkout of release.yml and prerelease.yml
		if file == "release.yml" || file == "prerelease.yml" {
			exempt = 1
		}
		if got := strings.Count(text, "persist-credentials: false"); got != checkouts-exempt {
			t.Errorf("%s has %d `persist-credentials: false` for %d checkouts, want %d", file, got, checkouts, checkouts-exempt)
		}
	}
	if total != 2 {
		t.Errorf("%d `persist-credentials: true` in total, want 2", total)
	}
}

// `contents: write` anywhere but the jobs that tag or publish is a token with a right it never uses.
func TestWritePermissionOnlyWhereNeeded(t *testing.T) {
	allowed := map[string]bool{
		"release.yml/release":  true,
		"release.yml/build":    true,
		"build.yml/publish":    true,
		"prerelease.yml/tag":   true,
		"prerelease.yml/build": true,
	}
	found := map[string]bool{}
	for _, file := range []string{"release.yml", "build.yml", "macos.yml", "prerelease.yml"} {
		text := workflow(t, file)
		mustNotContain(t, file+" top-level permissions", topOf(text), "contents: write")
		for name, job := range jobs(text) {
			if strings.Contains(job, "contents: write") {
				found[file+"/"+name] = true
			}
		}
	}
	for k := range found {
		if !allowed[k] {
			t.Errorf("%s has `contents: write`", k)
		}
	}
	for k := range allowed {
		if !found[k] {
			t.Errorf("%s lost its `contents: write` (it tags or publishes)", k)
		}
	}
}

// A dropped target, `-trimpath` or `CGO_ENABLED=0` makes the release ship fewer or different binaries.
func TestBuildTargetsAndFlags(t *testing.T) {
	build := jobOf(t, "build.yml", "build")
	var got []string
	for _, m := range regexp.MustCompile(`goos:\s*(\w+)\s*\n\s*goarch:\s*(\w+)`).FindAllStringSubmatch(build, -1) {
		got = append(got, m[1]+"/"+m[2])
	}
	sort.Strings(got)
	want := "darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64"
	if g := strings.Join(got, " "); g != want {
		t.Errorf("build targets = %q, want %q", g, want)
	}
	mustContain(t, "build job", build, "-trimpath", "CGO_ENABLED=0", `"$RUNNER_TEMP/dist/`, "lighthouse_", ".exe")
}

// A binary built from a dirty tree, or one whose version nobody checks, goes out under the tag.
func TestBuildChecksVersionAndCleanTree(t *testing.T) {
	build := jobOf(t, "build.yml", "build")
	mustContain(t, "build job", build, `test "$out" = "lighthouse $TAG"`)
	inOrder(t, "build job", build, "git status --porcelain", "go build")
}

// A publish without the notes file, the re-upload flag or the checksums breaks the release page;
// a tag without a changelog section must fail, only a pre-release writes the fixed line.
func TestPublishUsesNotesFileAndClobber(t *testing.T) {
	publish := jobOf(t, "build.yml", "publish")
	mustContain(t, "publish job", publish,
		"go run ./ci/changelog notes", "--notes-file", "--clobber", "SHA256SUMS", "PRERELEASE",
		"Pre-release build of $TAG for testing; not a release.",
	)
	setup := regexp.MustCompile(`uses: actions/setup-go@[0-9a-f]{40} # v[\d.]+\n\s+with:\n\s+go-version-file: go.mod\n\s+cache: false`).FindStringIndex(publish)
	if setup == nil {
		t.Error("publish has no setup-go with go-version-file and cache: false")
	} else if setup[0] > strings.Index(publish, "go run ./ci/changelog notes") {
		t.Error("publish sets up Go after `go run ./ci/changelog notes`")
	}
	mustMatch(t, "publish job", publish,
		`if \[ "\$PRERELEASE" = true \]; then\s*\n\s*echo "Pre-release build of \$TAG[^\n]*\n`+
			`\s*else\s*\n\s*echo "publish: no CHANGELOG\.md section for \$TAG" >&2; exit 1\s*\n\s*fi`)
}

// An input or ref name interpolated into a run script is a script injection into a job with a token.
func TestNoInputsInRunScripts(t *testing.T) {
	for _, file := range []string{"release.yml", "build.yml", "macos.yml", "prerelease.yml"} {
		for _, block := range runBlocks(workflow(t, file)) {
			if strings.Contains(block, "${{") {
				t.Errorf("%s interpolates an expression in a run script: %s", file, block)
			}
		}
	}
}

// A push error after the server took the push, which drops the tag and skips the build, leaves a
// release without binaries; a rival run's tag of the same name must not count as ours.
func TestReleaseRecoversAPushThatLanded(t *testing.T) {
	release := jobOf(t, "release.yml", "release")
	mustContain(t, "release job", release,
		`git rev-parse "refs/tags/$tag"`, "sleep $((attempt * 5))")
	inOrder(t, "release job", release, `git ls-remote origin "refs/tags/$tag"`, `git tag -d "$tag"`)
}

// A `web` job that stops proving the tag's tracked dist equals a fresh build lets a stale UI be released.
func TestWebJobChecksDistOfTheTag(t *testing.T) {
	web := jobOf(t, "build.yml", "web")
	tracked := "git ls-files --error-unmatch internal/web/dist/index.html"
	clean := `test -z "$(git status --porcelain --ignored -- internal/web/dist)"`
	mustContain(t, "web job", web, "node-version-file: web/.nvmrc", "contents: read")
	mustNotContain(t, "web job", web, "contents: write", "upload-artifact")
	inOrder(t, "web job", web, tracked, "npm ci", "npm run build", clean)
	for _, command := range []string{"npm ci", "npm run build"} {
		var own string
		for _, s := range steps(web) {
			if regexp.MustCompile(`(?m)^\s+run: ` + regexp.QuoteMeta(command) + `\s*$`).MatchString(s) {
				own = s
				break
			}
		}
		if own == "" {
			t.Errorf("web has no own step `run: %s`", command)
			continue
		}
		mustMatch(t, "web step "+command, own, `^\s+working-directory: web\s*$`)
	}
	for _, command := range []string{tracked, clean} {
		found := false
		for _, s := range steps(web) {
			found = found || strings.Contains(strings.Join(runBlocks(s), "\n"), command)
		}
		if !found {
			t.Errorf("no web step runs %q", command)
		}
	}
}

// A `build` that no longer waits for `web` builds binaries before the dist of the tag is proven.
func TestBuildWaitsForWeb(t *testing.T) {
	mustMatch(t, "build job", jobOf(t, "build.yml", "build"), `^    needs: \[resolve, web\]\s*$`)
}

// A `go install` of the tag that is no longer proven, or that goes through a proxy setting,
// ships a module whose embedded SPA nobody tested.
func TestGoInstallJobProvesSPA(t *testing.T) {
	job := jobOf(t, "build.yml", "go-install")
	mustMatch(t, "go-install job", job, `^    needs: \[resolve, web\]\s*$`)
	mustContain(t, "go-install job", job, "contents: read", "actions/setup-go@", "go-version-file: go.mod")
	mustNotContain(t, "go-install job", job, "contents: write")
	checkout := stepUsing(t, job, "actions/checkout@")
	mustMatch(t, "go-install checkout", checkout, `^\s+ref: refs/tags/\$\{\{ needs\.resolve\.outputs\.tag \}\}\s*$`)
	mustContain(t, "go-install checkout", checkout, "persist-credentials: false")
	run := strings.Join(runBlocks(job), "\n")
	mustContain(t, "go-install run", run,
		"GOPRIVATE=github.com/CorneeldH/lighthouse",
		`GOBIN="$RUNNER_TEMP/bin"`,
		`go install github.com/CorneeldH/lighthouse/cmd/lighthouse@"$TAG"`)
	inOrder(t, "go-install run", run, "go install", `ci/spa-smoke.sh "$RUNNER_TEMP/bin/lighthouse"`)
	for _, file := range []string{"build.yml", "ci.yml", "macos.yml", "pages.yml", "prerelease.yml", "release.yml"} {
		mustNotContain(t, file, workflow(t, file), "GOPROXY")
	}
}

// A release published without the `go install` proof ships a tag that cannot be installed.
func TestPublishNeedsGoInstall(t *testing.T) {
	mustMatch(t, "publish job", jobOf(t, "build.yml", "publish"), `^    needs: \[resolve, build, go-install\]\s*$`)
}

// A linux/amd64 binary that is no longer smoke-tested after `Check version` ships a broken SPA.
func TestBuildRunsSPASmoke(t *testing.T) {
	all := steps(jobOf(t, "build.yml", "build"))
	version := stepNamed(t, all, "Check version")
	smoke := stepNamed(t, all, "Check SPA")
	if version >= smoke {
		t.Error("`Check SPA` does not come after `Check version`")
	}
	condition := "if: matrix.goos == 'linux' && matrix.goarch == 'amd64'"
	mustContain(t, "Check version", all[version], condition)
	mustMatch(t, "Check SPA", all[smoke], `^\s+`+regexp.QuoteMeta(condition)+`\s*$`)
	mustContain(t, "Check SPA", all[smoke], `ci/spa-smoke.sh "$RUNNER_TEMP/dist/lighthouse_linux_amd64"`)
}

// A checkout in build.yml that does not take `refs/tags/<resolved tag>` builds another commit than the tag.
func TestBuildCheckoutsPinTheTag(t *testing.T) {
	ref := "ref: refs/tags/${{ needs.resolve.outputs.tag }}"
	found := 0
	for name, job := range jobs(workflow(t, "build.yml")) {
		for _, s := range steps(job) {
			if strings.Contains(s, "actions/checkout@") {
				found++
				mustMatch(t, "build.yml job "+name+" checkout", s, `^\s+`+regexp.QuoteMeta(ref)+`\s*$`)
			}
		}
	}
	if found != 4 {
		t.Errorf("build.yml has %d checkouts, want 4", found)
	}
}

// A macOS gate that runs on a PR, a push or a schedule costs macOS minutes on every change;
// without workflow_call the release cannot wait for it.
func TestMacOSGateTriggers(t *testing.T) {
	on := onBlock(t, workflow(t, "macos.yml"))
	mustContain(t, "macos.yml on:", on, "workflow_call", "workflow_dispatch")
	mustNotContain(t, "macos.yml on:", on, "pull_request", "push", "schedule")
}

// A release that can start without a green macOS run (#337), or whose `ui` job races the download
// of its artifact, releases an untested commit.
func TestReleaseWaitsForMacOS(t *testing.T) {
	mustContain(t, "release.yml macos job", jobOf(t, "release.yml", "macos"), "uses: ./.github/workflows/macos.yml")
	mustMatch(t, "release job", jobOf(t, "release.yml", "release"), `^    needs: \[macos, ui\]\s*$`)
	mustMatch(t, "ui job", jobOf(t, "release.yml", "ui"), `^    needs: macos\s*$`)
}

// testedCommitCheck returns the `if` block of the release loop that compares HEAD with the commit macOS tested.
func testedCommitCheck(t *testing.T, release string) string {
	t.Helper()
	head := "            if [ \"$(git rev-parse HEAD)\" != \"$TESTED\" ]; then\n"
	start := strings.Index(release, head)
	if start < 0 {
		t.Fatal("no tested-commit check in the release job")
	}
	end := strings.Index(release[start:], "\n            fi\n")
	if end < 0 {
		t.Fatal("tested-commit check has no closing `fi`")
	}
	return release[start : start+end]
}

// A release that can tag a commit macOS did not test, or turns red when main merely moved on.
func TestReleaseOnlyReleasesTheTestedCommit(t *testing.T) {
	release := jobOf(t, "release.yml", "release")
	mustContain(t, "release job", release, "TESTED: ${{ github.sha }}")
	block := testedCommitCheck(t, release)
	inOrder(t, "release job", release, "for attempt in 1 2 3", "git reset --hard origin/main", block, "go run ./ci/changelog release")
	setup := regexp.MustCompile(`uses: actions/setup-go@[0-9a-f]{40} # v[\d.]+\n\s+with:\n\s+go-version-file: go.mod\n\s+cache: false`).FindStringIndex(release)
	if setup == nil {
		t.Error("release has no setup-go with go-version-file and cache: false")
	} else if setup[0] > strings.Index(release, "for attempt in 1 2 3") {
		t.Error("release sets up Go inside or after the retry loop")
	}
	mustMatch(t, "tested-commit check", block, `^              echo "release: [^"]+"; exit 0$`)
}

// A run by hand or a re-run that ends green without releasing because main moved on can replace
// the pending run of the newer commit, so nobody releases.
func TestOvertakenDispatchOrRerunFails(t *testing.T) {
	release := jobOf(t, "release.yml", "release")
	mustContain(t, "release job", release, "ATTEMPT: ${{ github.run_attempt }}")
	block := testedCommitCheck(t, release)
	loc := regexp.MustCompile(`(?m)^              if \[ "\$EVENT" = workflow_dispatch \] \|\| \[ "\$ATTEMPT" != 1 \]; then echo "release: [^"]+" >&2; exit 1; fi$`).FindStringIndex(block)
	if loc == nil {
		t.Fatalf("no dispatch-or-rerun failure line in:\n%s", block)
	}
	if loc[0] > strings.Index(block, "exit 0") {
		t.Error("the dispatch-or-rerun failure comes after the green `exit 0`")
	}
}

// A pre-release with an automatic trigger publishes test builds on its own; without its `tag`
// input nobody can name the tag.
func TestPrereleaseTriggerAndInput(t *testing.T) {
	on := onBlock(t, workflow(t, "prerelease.yml"))
	mustContain(t, "prerelease.yml on:", on, "workflow_dispatch:")
	mustMatch(t, "prerelease.yml on:", on, `^    inputs:\s*\n      tag:`)
	for _, trigger := range []string{"push", "pull_request", "schedule", "workflow_call"} {
		if regexp.MustCompile(`(?m)^  ` + trigger + `\b`).MatchString(on) {
			t.Errorf("prerelease.yml has the trigger %s", trigger)
		}
	}
}

// A `ui` or `tag` job that lets a tag that is not vX.Y.Z-<suffix> through, or checks it after the
// build, turns a pre-release into a real release tag.
func TestPrereleaseRejectsNonPrereleaseTags(t *testing.T) {
	for _, name := range []string{"ui", "tag"} {
		job := jobOf(t, "prerelease.yml", name)
		if !strings.Contains(job, `grep -Eq "`+prereleaseTagRE+`"`) && !strings.Contains(job, `grep -Eq '`+prereleaseTagRE+`'`) {
			t.Errorf("prerelease %s does not check the tag against %s", name, prereleaseTagRE)
		}
		mustContain(t, "prerelease "+name, job, "exit 1")
	}
	inOrder(t, "prerelease ui", jobOf(t, "prerelease.yml", "ui"), prereleaseTagRE, "npm ci")
	inOrder(t, "prerelease tag", jobOf(t, "prerelease.yml", "tag"), prereleaseTagRE, "ci/tag-with-ui.sh")
}

// A `tag` job that pushes more than refs/tags/<tag>, or may overwrite an existing tag, moves main or a published release.
func TestPrereleaseTagJobPushesOnlyTheTag(t *testing.T) {
	tag := jobOf(t, "prerelease.yml", "tag")
	mustMatch(t, "tag job", tag, `^    needs: ui\s*$`)
	mustContain(t, "tag job", tag, "contents: write")
	checkout := stepUsing(t, tag, "actions/checkout@")
	mustContain(t, "tag checkout", checkout, "persist-credentials: true")
	mustMatch(t, "tag checkout", checkout, `^\s+ref: \$\{\{ github\.sha \}\}\s*$`)
	run := strings.Join(runBlocks(tag), "\n")
	mustContain(t, "tag run", run, `git push origin "refs/tags/$tag"`, "already exists on the remote", tagWithUI, `echo "tag=$tag" >> "$GITHUB_OUTPUT"`)
	mustNotContain(t, "tag run", run, "--atomic", "HEAD:", "main", "--force", "-f ")
	exists := `git ls-remote --exit-code --tags origin "refs/tags/$tag"`
	capture := exists + " >/dev/null || status=$?"
	refuse := `if [ "$status" -eq 0 ]; then`
	failure := `elif [ "$status" -ne 2 ]; then`
	// ls-remote exit codes: 0 = tag exists (refuse), 2 = no such tag (free), anything else = error.
	inOrder(t, "tag run", run, "status=0", capture, refuse, failure, "ci/tag-with-ui.sh", "git push")
	if i, j := strings.Index(run, refuse), strings.Index(run, failure); i >= 0 && j > i {
		mustContain(t, "refuse branch", run[i:j], "already exists on the remote", "exit 1")
		if k := strings.Index(run, "ci/tag-with-ui.sh"); k > j {
			mustContain(t, "error branch", run[j:k], "exit 1")
		}
	}
	mustMatch(t, "tag job", tag, `^      tag: \$\{\{ steps\.\w+\.outputs\.tag \}\}\s*$`)
	download := stepUsing(t, tag, "actions/download-artifact@")
	mustMatch(t, "tag download", download, `^\s+name: ui-dist\s*$`)
	mustMatch(t, "tag download", download, `^\s+path: \$\{\{ runner\.temp \}\}/ui-dist\s*$`)
}

// A `build` that stops calling build.yml for the tag job's tag publishes nothing for the pre-release.
func TestPrereleaseBuildCallsBuildWorkflow(t *testing.T) {
	build := jobOf(t, "prerelease.yml", "build")
	mustMatch(t, "build job", build, `^    needs: tag\s*$`)
	mustContain(t, "build job", build, "contents: write", "uses: ./.github/workflows/build.yml")
	mustMatch(t, "build job", build, `^      tag: \$\{\{ needs\.tag\.outputs\.tag \}\}\s*$`)
}
