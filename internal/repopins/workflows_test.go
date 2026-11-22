package repopins

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// gateGoFlags returns the flags of the Go part of [test_gate] command in lighthouse.toml
// (`lighthouse gate go -- go test ...`).
func gateGoFlags(t *testing.T) []string {
	t.Helper()
	toml := read(t, "lighthouse.toml")
	m := regexp.MustCompile(`lighthouse gate go --\s+(go test[^&]*?)\s*(?:&&|")`).FindStringSubmatch(toml)
	if m == nil {
		t.Fatal("no `lighthouse gate go -- go test ...` in [test_gate] command of lighthouse.toml")
	}
	var flags []string
	for _, tok := range strings.Fields(m[1]) {
		if strings.HasPrefix(tok, "-") {
			flags = append(flags, tok)
		}
	}
	if len(flags) == 0 {
		t.Fatalf("the Go part of the gate has no flags to pin: %q", m[1])
	}
	return flags
}

// goTestCommands returns every `go test` run of a workflow, over all its jobs.
func goTestCommands(t *testing.T, workflow string) []string {
	t.Helper()
	var out []string
	for _, body := range jobs(read(t, ".github", "workflows", workflow)) {
		for _, run := range runBlocks(body) {
			if strings.HasPrefix(run, "go test ") {
				out = append(out, run)
			}
		}
	}
	return out
}

func assertGateFlags(t *testing.T, workflow string, want int) {
	t.Helper()
	flags := gateGoFlags(t)
	runs := goTestCommands(t, workflow)
	if len(runs) != want {
		t.Fatalf("%s has %d `go test` runs, want %d: %q", workflow, len(runs), want, runs)
	}
	for _, run := range runs {
		words := strings.Fields(run)
		for _, flag := range flags {
			found := false
			for _, w := range words {
				found = found || w == flag
			}
			if !found {
				t.Errorf("%s: %q lacks the gate flag %s", workflow, run, flag)
			}
		}
	}
}

func hasLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// A drift between the pin and the lint action would show only as a different lint result on CI.
func TestGolangciPinMatchesCI(t *testing.T) {
	pin := strings.TrimSpace(read(t, ".golangci-version"))
	ci := read(t, ".github", "workflows", "ci.yml")
	all := regexp.MustCompile(`(?s)uses: golangci/golangci-lint-action@\S+.*?\n\s+with:\n\s+version: (\S+)`).FindAllStringSubmatch(ci, -1)
	if len(all) == 0 {
		t.Fatal("no golangci-lint-action step with a version in ci.yml")
	}
	for _, m := range all {
		if m[1] != pin {
			t.Errorf("ci.yml lints with %s, .golangci-version pins %s", m[1], pin)
		}
	}
}

// A `go test` step without a flag of the gate runs a weaker test than the commit gate does.
func TestEveryCIGoTestHasGateFlags(t *testing.T) {
	assertGateFlags(t, "ci.yml", 6) // unit, e2e, the two canary steps, checkpointscan and the hook e2e with gitleaks
}

// A production change that fails this test: a `go test` step in macos.yml loses one of the gate flags, so macOS runs a
// weaker test than the commit gate.
func TestEveryMacOSGoTestHasGateFlags(t *testing.T) {
	assertGateFlags(t, "macos.yml", 2) // unit and e2e
}

// A production change that fails this test: the `CGO_ENABLED=0 go build ./...` step in ci.yml is removed or loses its
// `CGO_ENABLED=0` prefix.
func TestCIBuildsWithoutCGO(t *testing.T) {
	runs := runBlocks(read(t, ".github", "workflows", "ci.yml"))
	for _, run := range runs {
		if run == "CGO_ENABLED=0 go build ./..." {
			return
		}
	}
	t.Errorf("ci.yml has no `CGO_ENABLED=0 go build ./...` step, got runs %q", runs)
}

// A platform-only break would otherwise show first at the release.
func TestCICrossBuildsReleaseTargets(t *testing.T) {
	runs := runBlocks(read(t, ".github", "workflows", "ci.yml"))
	for _, target := range []string{"GOOS=windows GOARCH=amd64", "GOOS=darwin GOARCH=arm64"} {
		want := target + " CGO_ENABLED=0 go build ./..."
		found := false
		for _, run := range runs {
			found = found || run == want
		}
		if !found {
			t.Errorf("ci.yml has no %q step", want)
		}
	}
}

// A production change that fails this test: pages.yml publishes another folder than `path: docs/site`.
func TestPagesPublishesTheSiteFolder(t *testing.T) {
	if !hasLine(read(t, ".github", "workflows", "pages.yml"), "path: docs/site") {
		t.Error("pages.yml does not publish `path: docs/site`")
	}
}

// A production change that fails this test: pages.yml drops the `main` branch, the `docs/site/**` path filter or
// `workflow_dispatch`, so a site change no longer reaches Pages.
func TestPagesRunsOnSiteChangesOnMain(t *testing.T) {
	text := read(t, ".github", "workflows", "pages.yml")
	for _, want := range []string{"branches: [main]", `- "docs/site/**"`, "workflow_dispatch:"} {
		if !hasLine(text, want) {
			t.Errorf("pages.yml lacks the line %q", want)
		}
	}
}

// A production change that fails this test: pages.yml drops `pages: write`, `id-token: write` or `contents: read`, so the deploy
// fails at run time.
func TestPagesPermissions(t *testing.T) {
	text := read(t, ".github", "workflows", "pages.yml")
	for _, want := range []string{"pages: write", "id-token: write", "contents: read"} {
		if !hasLine(text, want) {
			t.Errorf("pages.yml lacks the permission %q", want)
		}
	}
}

// A tag can move; only a full commit SHA, with its tag as a comment, pins an action.
func TestEveryWorkflowPinsActionsBySHA(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflow files found: %v", err)
	}
	uses := regexp.MustCompile(`^\s*(?:- )?uses:\s*(\S+)`)
	pinned := regexp.MustCompile(`@[0-9a-f]{40} # v\d+(\.\d+)*\s*$`)
	for _, file := range files {
		rel := filepath.Base(file)
		for _, line := range strings.Split(read(t, ".github", "workflows", rel), "\n") {
			m := uses.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "./.github/workflows/") {
				continue
			}
			if !pinned.MatchString(line) {
				t.Errorf("%s: action not pinned by SHA with a version comment: %s", rel, strings.TrimSpace(line))
			}
		}
	}
}

// ciText is ci.yml without its comment lines, so a pin cannot be satisfied by prose.
func ciText(t *testing.T) string {
	t.Helper()
	return stripComments(read(t, ".github", "workflows", "ci.yml"))
}

func stripComments(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ciJobs returns the jobs of ci.yml, comment lines removed.
func ciJobs(t *testing.T) map[string]string {
	t.Helper()
	return jobs(ciText(t))
}

// ciJob returns one job of ci.yml and fails when it is gone.
func ciJob(t *testing.T, name string) string {
	t.Helper()
	body, ok := ciJobs(t)[name]
	if !ok {
		t.Fatalf("ci.yml has no job %q", name)
	}
	return body
}

// ciGoJobs returns `go` and every job split off from it (`go-*`).
func ciGoJobs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, body := range ciJobs(t) {
		if name == "go" || strings.HasPrefix(name, "go-") {
			out[name] = body
		}
	}
	return out
}

// stepsOf cuts a job into its steps: each starts with a `      - ` line.
func stepsOf(body string) []string {
	var steps []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "      - ") {
			steps = append(steps, line)
		} else if len(steps) > 0 {
			steps[len(steps)-1] += "\n" + line
		}
	}
	return steps
}

func stepsWith(body, needle string) []string {
	var out []string
	for _, s := range stepsOf(body) {
		if strings.Contains(s, needle) {
			out = append(out, s)
		}
	}
	return out
}

func assertContains(t *testing.T, what, text string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(text, n) {
			t.Errorf("%s lacks %q", what, n)
		}
	}
}

func assertMatches(t *testing.T, what, text, pattern string) []int {
	t.Helper()
	loc := regexp.MustCompile(pattern).FindStringIndex(text)
	if loc == nil {
		t.Errorf("%s does not match %s", what, pattern)
	}
	return loc
}

// A production change that fails this test: a `paths:` filter returns to the `on:` block of ci.yml.
func TestCINoPathsFilter(t *testing.T) {
	on, _, _ := strings.Cut(ciText(t), "\npermissions:")
	if strings.Contains(on, "paths") {
		t.Errorf("the `on:` block of ci.yml has a paths filter:\n%s", on)
	}
}

// A production change that fails this test: a domain job runs without consulting the `changes` job.
func TestCIDomainJobsWaitForChanges(t *testing.T) {
	for _, name := range []string{"python", "go", "web"} {
		body := ciJob(t, name)
		assertContains(t, name, body, "needs: changes", "needs.changes.outputs."+name+" == 'true'")
	}
}

// A production change that fails this test: the canary job drops a canary, a setup action or its gate on the `changes` job.
func TestCICanaryRunsBothCanariesWithEveryTool(t *testing.T) {
	canary := ciJob(t, "canary")
	assertContains(t, "canary", canary,
		"needs: changes",
		"needs.changes.outputs.go == 'true'",
		"needs.changes.outputs.python == 'true'",
		"- run: go test -race -tags canary ./internal/importcheck/",
		"- run: uv run pytest -q tests/pack/python/test_imports_gate_canary.py")
	for _, action := range []string{"setup-go", "setup-uv", "setup-node"} {
		assertMatches(t, "canary action "+action, canary, `uses: \S*`+action+`@[0-9a-f]{40} # v[\d.]+`)
	}
}

// A production change that fails this test: the `changes` job stops classifying the diff with ci/domains, or loses
// `shell: bash` (without it `git diff | classify` has no pipefail).
func TestCIChangesJobClassifies(t *testing.T) {
	changes := ciJob(t, "changes")
	assertContains(t, "changes", changes, "fetch-depth: 0", "--no-renames", " -z ", "go run ./ci/domains classify")
	assertMatches(t, "changes", changes, `(?m)^        shell: bash\s*$`)
}

// A production change that fails this test: the web job drops a check or the generated-client drift test.
func TestCIWebJobRunsEveryCheck(t *testing.T) {
	assertContains(t, "web", ciJob(t, "web"),
		"node-version-file: web/.nvmrc", "npm ci", "npm run lint", "npm run typecheck", "npm test",
		"npm run gen", "git status --porcelain -- src/api", "npm run build")
}

// A production change that fails this test: the python job drops its Go setup before pytest, so the `lighthouse_bin`
// fixture cannot build the binary (#274). Task 3 rewrites this for pack-python.
func TestCIPythonJobSetsUpGoBeforePytest(t *testing.T) {
	python := ciJob(t, "python")
	loc := assertMatches(t, "python", python,
		`uses: actions/setup-go@[0-9a-f]{40} # v[\d.]+\n\s+with:\n\s+go-version-file: go.mod`)
	pytest := strings.Index(python, "run: uv run pytest -n auto")
	if loc == nil || pytest < 0 || loc[0] > pytest {
		t.Errorf("python does not set up Go before `uv run pytest -n auto`:\n%s", python)
	}
}

// A production change that fails this test: a PR job runs on macOS again; macOS is the gate before the release (#337).
// Task 3 rewrites this for pack-python.
func TestCIPRRunsNoMacOSJob(t *testing.T) {
	if strings.Contains(ciText(t), "macos-latest") {
		t.Error("ci.yml runs a job on macos-latest")
	}
	for _, name := range []string{"python", "go-test", "go-e2e"} {
		body := ciJob(t, name)
		assertMatches(t, name, body, `(?m)^    runs-on: ubuntu-latest\s*$`)
		if strings.Contains(body, "matrix.os") {
			t.Errorf("%s uses matrix.os", name)
		}
	}
}

// A production change that fails this test: web or binary stops caching the npm download for `npm ci`.
func TestCINodeJobsCacheNpm(t *testing.T) {
	for _, name := range []string{"web", "binary"} {
		setup := stepsWith(ciJob(t, name), "actions/setup-node")
		if len(setup) != 1 {
			t.Errorf("%s has %d setup-node steps, want 1", name, len(setup))
			continue
		}
		assertMatches(t, name, setup[0], `(?m)^\s+cache: npm\s*$`)
		assertContains(t, name, setup[0], "cache-dependency-path: web/package-lock.json")
	}
}

// A production change that fails this test: binary waits for web again, or stops building the SPA, the binary or the smoke.
func TestCIBinaryRunsWithoutWeb(t *testing.T) {
	binary := ciJob(t, "binary")
	assertMatches(t, "binary", binary, `(?m)^    needs: changes\s*$`)
	if strings.Contains(binary, "needs.web") {
		t.Error("binary needs the web job")
	}
	assertContains(t, "binary", binary,
		"outputs.web == 'true'", "outputs.go == 'true'", "CGO_ENABLED=0 go build ./cmd/lighthouse", "ci/spa-smoke.sh")
	var build []string
	for _, s := range stepsOf(binary) {
		if regexp.MustCompile(`(?m)run: npm run build\s*$`).MatchString(s) {
			build = append(build, s)
		}
	}
	if len(build) != 1 || regexp.MustCompile(`(?m)^\s*if:`).MatchString(build[0]) {
		t.Errorf("want one unconditional `npm run build` step, got %q", build)
	}
	ci, run, goBuild := strings.Index(binary, "run: npm ci"), strings.Index(binary, "run: npm run build"), strings.Index(binary, "go build ./cmd/lighthouse")
	if ci < 0 || run < ci || goBuild < run {
		t.Errorf("binary does not run npm ci, npm run build and go build in that order (%d, %d, %d)", ci, run, goBuild)
	}
	if strings.Contains(ciText(t), "web-dist") {
		t.Error("ci.yml hands a web-dist artifact around again")
	}
}

// A production change that fails this test: a job split off from `go` runs without the go output of the `changes` job.
func TestCIGoJobsWaitForChanges(t *testing.T) {
	goJobs := ciGoJobs(t)
	if len(goJobs) < 2 {
		t.Fatalf("want at least 2 go jobs, got %d", len(goJobs))
	}
	for name, body := range goJobs {
		assertMatches(t, name, body, `(?m)^    needs: changes\s*$`)
		assertContains(t, name, body, "if: needs.changes.outputs.go == 'true'")
	}
}

// A production change that fails this test: a Go check drops out of every go job while the go jobs are split.
func TestCIGoLintGatesStayInGoJobs(t *testing.T) {
	var all []string
	for _, body := range ciGoJobs(t) {
		all = append(all, body)
	}
	assertContains(t, "the go jobs", strings.Join(all, "\n"),
		"golangci/golangci-lint-action@",
		"- run: CGO_ENABLED=0 go build ./...",
		"- run: GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...",
		"- run: GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...",
		"- run: go test -race -shuffle=on ./...",
		"- run: go test -race -shuffle=on -tags e2e ./e2e/ -v",
		"name: installed pack/core matches pack/")
}

// A production change that fails this test: the e2e run moves back into the job of the race tests, which made that job
// the slowest.
func TestCIE2ERunsApartFromRaceTests(t *testing.T) {
	goJobs := ciGoJobs(t)
	var race, e2e []string
	for name, body := range goJobs {
		if strings.Contains(body, "- run: go test -race -shuffle=on ./...") {
			race = append(race, name)
		}
		if strings.Contains(body, "- run: go test -race -shuffle=on -tags e2e ./e2e/ -v") {
			e2e = append(e2e, name)
		}
	}
	if len(race) != 1 || len(e2e) != 1 || race[0] == e2e[0] {
		t.Fatalf("want the race tests and the e2e in two different jobs, got race %q, e2e %q", race, e2e)
	}
	if !strings.Contains(goJobs[e2e[0]], "astral-sh/setup-uv@") || strings.Contains(goJobs[race[0]], "astral-sh/setup-uv@") {
		t.Errorf("setup-uv belongs in %s and not in %s", e2e[0], race[0])
	}
}

// A production change that fails this test: ci.yml gets a different number of action references, so a new `uses:`
// slipped in unnoticed. TestEveryWorkflowPinsActionsBySHA checks each one for its SHA.
func TestCIPinsEveryActionCount(t *testing.T) {
	uses := regexp.MustCompile(`(?m)^\s*(?:- )?uses: (\S+)`).FindAllStringSubmatch(ciText(t), -1)
	if len(uses) != 33 {
		t.Errorf("ci.yml has %d actions, want 33", len(uses))
	}
}

// A production change that fails this test: the fragment check starts depending on the domains of the change, or loses
// its Go setup (without a build cache, so it cannot lock out the others).
func TestCIChangelogJobRunsOnEveryPR(t *testing.T) {
	job := ciJob(t, "changelog")
	if strings.Contains(job, "needs:") || strings.Contains(job, "needs.changes") {
		t.Error("changelog depends on another job")
	}
	ifs := regexp.MustCompile(`(?m)^\s+if: .*$`).FindAllString(job, -1)
	if len(ifs) != 1 || ifs[0] != "    if: github.event_name == 'pull_request'" {
		t.Errorf("changelog conditions = %q, want only the pull_request one", ifs)
	}
	assertContains(t, "changelog", job, "runs-on: ubuntu-latest", "fetch-depth: 0",
		`go run ./ci/changelog check --base "$BASE" --head "$HEAD"`)
	loc := assertMatches(t, "changelog", job,
		`uses: actions/setup-go@[0-9a-f]{40} # v[\d.]+\n\s+with:\n\s+go-version-file: go.mod\n\s+cache: false`)
	if loc != nil && loc[0] > strings.Index(job, "go run ./ci/changelog") {
		t.Error("changelog sets up Go after `go run ./ci/changelog`")
	}
}

// A production change that fails this test: a manual run stops enabling web, python and go, or loses the all-domains input.
func TestCIDispatchTurnsOnEveryDomain(t *testing.T) {
	text := ciText(t)
	assertContains(t, "ci.yml", text, "workflow_dispatch:", "all-domains:")
	block, _, _ := strings.Cut(text[strings.Index(text, "all-domains:"):], "permissions:")
	assertContains(t, "the all-domains input", block, "type: boolean", "default: true")
	assertContains(t, "changes", ciJob(t, "changes"), "web=$ALL", "python=$ALL", "go=$ALL")
}

// A production change that fails this test: binary stops running Playwright after the smoke, or Node and `npm ci` can
// be skipped.
func TestCIBinaryRunsPlaywright(t *testing.T) {
	binary := ciJob(t, "binary")
	install := "npx playwright install --with-deps chromium"
	if !strings.Contains(binary, install) {
		t.Fatalf("binary lacks %q", install)
	}
	run := regexp.MustCompile(`LIGHTHOUSE_BIN=\S+ npm run e2e`).FindStringIndex(binary)
	if run == nil {
		t.Fatal("binary has no `LIGHTHOUSE_BIN=... npm run e2e` step")
	}
	if smoke, inst := strings.Index(binary, "ci/spa-smoke.sh"), strings.Index(binary, install); smoke < 0 || smoke >= inst || inst >= run[0] {
		t.Error("binary does not run the smoke, the playwright install and the e2e in that order")
	}
	setup := stepsWith(binary, "actions/setup-node")
	npmCI := stepsWith(binary, "run: npm ci")
	if len(setup) != 1 || len(npmCI) != 1 {
		t.Fatalf("want one setup-node and one npm ci step, got %d and %d", len(setup), len(npmCI))
	}
	for _, step := range append(setup, npmCI...) {
		if regexp.MustCompile(`(?m)^\s*(- )?if:`).MatchString(step) {
			t.Errorf("step can be skipped:\n%s", step)
		}
	}
	if node, npm := strings.Index(binary, "actions/setup-node"), strings.Index(binary, "run: npm ci"); node >= npm || npm >= run[0] {
		t.Error("binary does not run setup-node, npm ci and the e2e in that order")
	}
}

// A production change that fails this test: git stops tracking ci/spa-smoke.sh as executable.
func TestCISmokeScriptExecutable(t *testing.T) {
	assertTrackedExecutable(t, "ci/spa-smoke.sh")
}

// assertTrackedExecutable fails when git does not track rel with mode 100755.
func assertTrackedExecutable(t *testing.T, rel string) {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-s", rel)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("COULD NOT CHECK: git ls-files -s %s: %v", rel, err)
	}
	if !strings.HasPrefix(string(out), "100755") {
		t.Errorf("git does not track %s as executable: %q", rel, out)
	}
}
