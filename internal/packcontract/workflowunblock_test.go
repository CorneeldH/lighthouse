package packcontract

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// The release lane pack/core/workflows/unblock-dependents.yml. Workflows read no TOML: every
// project value is an @@<key>@@ placeholder that init fills with packs.Fill. The lane only
// fetches the binary (`Get lighthouse`, run here as a script against fake tools) and calls
// `lighthouse unblock-dependents`. close-issue finds the run by its file name
// (TestCloseIssueFindsTheInstalledUnblockLane) and by the issue number at the end of its run-name.

const unblockWorkflow = "core/workflows/unblock-dependents.yml"

// unblockValues are the config values the tests fill in; none is a value of this repo.
var unblockValues = map[string]string{
	"ci.runs_on":                "runner-from-config",
	"ci.bot_client_id_var":      "CLIENT_ID_FROM_CONFIG",
	"ci.bot_private_key_secret": "KEY_FROM_CONFIG",
	"forge.default_branch":      "trunk-from-config",
}

type unblockStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
}

type unblockLane struct {
	RunName string `yaml:"run-name"`
	Jobs    map[string]struct {
		If     string        `yaml:"if"`
		RunsOn string        `yaml:"runs-on"`
		Steps  []unblockStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// unblockFilled fills the workflow the way init does, with unblockValues, and parses it.
func unblockFilled(t *testing.T) unblockLane {
	t.Helper()
	out, missing, err := packs.Fill([]byte(readPack(t, unblockWorkflow)), func(key string) (string, bool, error) {
		v, ok := unblockValues[key]
		return v, ok, nil
	})
	if err != nil || len(missing) > 0 {
		t.Fatalf("fill: missing %v, err %v", missing, err)
	}
	var lane unblockLane
	if err := yaml.Unmarshal(out, &lane); err != nil {
		t.Fatal(err)
	}
	if len(lane.Jobs) != 1 {
		t.Fatalf("%d jobs, want 1", len(lane.Jobs))
	}
	return lane
}

// unblockSteps are the steps of the one job.
func unblockSteps(t *testing.T) []unblockStep {
	t.Helper()
	for _, job := range unblockFilled(t).Jobs {
		return job.Steps
	}
	return nil
}

// unblockStepNamed is the step called name.
func unblockStepNamed(t *testing.T, name string) unblockStep {
	t.Helper()
	for _, s := range unblockSteps(t) {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no step %q", name)
	return unblockStep{}
}

// Classification: fails when runs-on, the bot's variable and secret, or the default branch in the
// job condition stop coming from the config, or a value of the source project stays in the file.
func TestUnblockProjectValuesComeFromConfig(t *testing.T) {
	lane := unblockFilled(t)
	for name, job := range lane.Jobs {
		if job.RunsOn != unblockValues["ci.runs_on"] {
			t.Errorf("%s runs-on %q", name, job.RunsOn)
		}
		want := "github.ref == 'refs/heads/" + unblockValues["forge.default_branch"] + "' && " +
			"(github.event_name == 'workflow_dispatch' || github.event.issue.state_reason == 'completed')"
		if got := flat(job.If); got != want {
			t.Errorf("%s if:\n got %s\nwant %s", name, got, want)
		}
	}
	token := unblockStepNamed(t, "App token for labels and comments")
	if got, want := token.With["client-id"], "${{ vars."+unblockValues["ci.bot_client_id_var"]+" }}"; got != want {
		t.Errorf("client-id %q, want %q", got, want)
	}
	if got, want := token.With["private-key"], "${{ secrets."+unblockValues["ci.bot_private_key_secret"]+" }}"; got != want {
		t.Errorf("private-key %q, want %q", got, want)
	}
	wantNone(t, strings.ToLower(readPack(t, unblockWorkflow)), "self-hosted", "_bot_", "refs/heads/main")
}

// unblockPlaceholderRe is the placeholder packs.Fill replaces.
var unblockPlaceholderRe = regexp.MustCompile(`@@([a-z_.]+)@@`)

// Classification: fails when the workflow gains a placeholder the config does not know, or one
// whose value is a list (printed one element per line, unescaped into the YAML).
func TestUnblockEveryPlaceholderIsAKnownScalarKey(t *testing.T) {
	var keys []string
	for _, m := range unblockPlaceholderRe.FindAllStringSubmatch(readPack(t, unblockWorkflow), -1) {
		if !slices.Contains(keys, m[1]) {
			keys = append(keys, m[1])
		}
	}
	sort.Strings(keys)
	want := []string{"ci.bot_client_id_var", "ci.bot_private_key_secret", "ci.runs_on", "forge.default_branch"}
	if !slices.Equal(keys, want) {
		t.Errorf("placeholders %v, want %v", keys, want)
	}
	for _, k := range keys {
		if kind := config.KindOf(k); kind != "str" {
			t.Errorf("%s is %s, want str", k, kind)
		}
	}
}

// Classification: fails when the run-name stops ending on the issue number close-issue looks for.
func TestUnblockRunNameEndsOnTheIssueNumber(t *testing.T) {
	got := unblockFilled(t).RunName
	if !strings.HasSuffix(got, " #${{ github.event.issue.number || inputs.issue }}") {
		t.Errorf("run-name %q does not end on the issue number", got)
	}
}

// Classification: fails when the last step stops calling `lighthouse unblock-dependents` with the
// issue through env, or the binary loses that subcommand or its --issue flag.
func TestUnblockCallsTheBinary(t *testing.T) {
	steps := unblockSteps(t)
	if len(steps) == 0 {
		t.Fatal("the job has no steps")
	}
	last := steps[len(steps)-1]
	if last.Run != `lighthouse unblock-dependents --issue "$ISSUE"` {
		t.Errorf("last step runs %q", last.Run)
	}
	if got := last.Env["ISSUE"]; got != "${{ github.event.issue.number || inputs.issue }}" {
		t.Errorf("ISSUE = %q", got)
	}
	cliFlags(t, []string{"unblock-dependents"}, "issue")
}

// Classification: fails when Go is set up for every run instead of only for a dev checkout, the
// step that decides it goes, the setup-go pin changes, the steps change order, or `Get lighthouse`
// loses its read token.
func TestUnblockGoIsSetUpOnlyForADevCheckout(t *testing.T) {
	var names []string
	for _, s := range unblockSteps(t) {
		names = append(names, s.Name)
	}
	wantOrder(t, strings.Join(names, "\n"),
		"Which lighthouse", "Set up Go", "Get lighthouse", "App token for labels and comments", "Release dependents")
	which := unblockStepNamed(t, "Which lighthouse")
	if which.ID != "lighthouse" {
		t.Errorf("Which lighthouse has id %q, want lighthouse", which.ID)
	}
	wantAll(t, which.Run,
		"if [ \"$version\" = dev ] && [ -f cmd/lighthouse/main.go ]; then\n  echo \"build=true\" >> \"$GITHUB_OUTPUT\"\nelse\n  echo \"build=false\" >> \"$GITHUB_OUTPUT\"\nfi")
	goStep := unblockStepNamed(t, "Set up Go")
	if goStep.If != "steps.lighthouse.outputs.build == 'true'" {
		t.Errorf("Set up Go if: %q", goStep.If)
	}
	if goStep.Uses != "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e" {
		t.Errorf("Set up Go uses %q", goStep.Uses)
	}
	if goStep.With["go-version-file"] != "go.mod" {
		t.Errorf("go-version-file %q", goStep.With["go-version-file"])
	}
	if got := unblockStepNamed(t, "Get lighthouse").Env["GH_TOKEN"]; got != "${{ github.token }}" {
		t.Errorf("Get lighthouse GH_TOKEN %q", got)
	}
}

// Classification: fails when the release download names another repository than the one the Go
// module (and so the release) lives in, or the owner appears anywhere else in the workflow.
func TestUnblockTheOneLiteralIsTheReleaseDownload(t *testing.T) {
	module := strings.TrimSuffix(reflect.TypeFor[packs.Unit]().PkgPath(), "/internal/packs")
	repo, ok := strings.CutPrefix(module, "github.com/")
	if !ok {
		t.Fatalf("module %q is not on github.com", module)
	}
	owner, _, _ := strings.Cut(repo, "/")
	var hits []string
	for line := range strings.SplitSeq(readPack(t, unblockWorkflow), "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(owner)) {
			hits = append(hits, strings.TrimSpace(line))
		}
	}
	if want := []string{"--repo " + repo + ` \`}; !slices.Equal(hits, want) {
		t.Errorf("lines naming %s: %q, want %q", owner, hits, want)
	}
}

// getLighthouse is one run of the `Get lighthouse` step.
type getLighthouse struct {
	code       int
	stdout     string
	runnerTemp string
	githubPath string
	calls      []string
}

// runGetLighthouse runs the `Get lighthouse` step with bash in a checkout whose manifest names
// version, with a cmd/lighthouse when cmd is set. gh, go and shasum are fakes that log their argv;
// sha256sum is one too, unless withSha256sum is false. The rest of PATH is only the real tools
// the step needs, so no tool of the host leaks in.
func runGetLighthouse(t *testing.T, version, runnerOS, arch string, cmd, withSha256sum bool) getLighthouse {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the step is a bash script")
	}
	bash := needTool(t, "bash")
	script := unblockStepNamed(t, "Get lighthouse").Run
	tmp := t.TempDir()
	checkout := filepath.Join(tmp, "checkout")
	writeFile(t, filepath.Join(checkout, ".lighthouse", "manifest.toml"),
		"# Managed\n\nversion = \""+version+"\"\n\n[[unit]]\nversion = \"0.0.1\"\n")
	if cmd {
		writeFile(t, filepath.Join(checkout, "cmd", "lighthouse", "main.go"), "package main\n")
	}
	bin := filepath.Join(tmp, "bin")
	for _, tool := range []string{"sed", "head", "mkdir", "mv", "chmod"} {
		path := needTool(t, tool)
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	fakes := filepath.Join(tmp, "fakes")
	calls := filepath.Join(tmp, "calls")
	writeFile(t, calls, "")
	fake := func(name, body string) {
		path := filepath.Join(fakes, name)
		writeFile(t, path, "#!"+bash+"\necho \""+name+" $*\" >> \"$FAKE_CALLS\"\n"+body)
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fake("gh", `dir=; while [ $# -gt 0 ]; do [ "$1" = --dir ] && dir=$2; shift; done
mkdir -p "$dir"; : > "$dir"/SHA256SUMS
for f in lighthouse_linux_amd64 lighthouse_linux_arm64 lighthouse_darwin_amd64 lighthouse_darwin_arm64; do
  printf '#!/bin/sh\n' > "$dir/$f"
done
`)
	if withSha256sum {
		fake("sha256sum", "")
	}
	fake("shasum", "")
	fake("go", `[ "$1" = build ] && [ "$2" = -o ] && : > "$3"
`)
	runnerTemp := filepath.Join(tmp, "runner")
	if err := os.MkdirAll(runnerTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	githubPath := filepath.Join(tmp, "github_path")
	c := exec.Command(bash, "-c", script)
	c.Dir = checkout
	c.Env = []string{
		"PATH=" + fakes + string(os.PathListSeparator) + bin,
		"HOME=" + tmp,
		"RUNNER_OS=" + runnerOS,
		"RUNNER_ARCH=" + arch,
		"RUNNER_TEMP=" + runnerTemp,
		"GITHUB_PATH=" + githubPath,
		"GH_TOKEN=t",
		"FAKE_CALLS=" + calls,
	}
	var stderr strings.Builder
	c.Stderr = &stderr
	out, err := c.Output()
	code := 0
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	logged, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSuffix(string(logged), "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if stderr.Len() > 0 {
		t.Logf("stderr of the step:\n%s", stderr.String())
	}
	return getLighthouse{code, string(out), runnerTemp, githubPath, lines}
}

// isExecutable reports whether path is a regular file with an execute bit.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// pathExists reports whether path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// callStarting reports whether a logged call starts with prefix.
func callStarting(calls []string, prefix string) bool {
	return slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

// Classification: fails when a supported runner gets the wrong release asset, the download skips
// the checksum, or the binary does not land executable as `lighthouse` on GITHUB_PATH.
func TestUnblockGetLighthouseMapsTheRunner(t *testing.T) {
	for _, c := range []struct{ os, arch, asset string }{
		{"Linux", "X64", "lighthouse_linux_amd64"},
		{"Linux", "ARM64", "lighthouse_linux_arm64"},
		{"macOS", "ARM64", "lighthouse_darwin_arm64"},
		{"macOS", "X64", "lighthouse_darwin_amd64"},
	} {
		t.Run(c.os+"/"+c.arch, func(t *testing.T) {
			r := runGetLighthouse(t, "0.12.1", c.os, c.arch, false, true)
			if r.code != 0 {
				t.Fatalf("exit %d, stdout %s", r.code, r.stdout)
			}
			binDir := r.runnerTemp + "/bin"
			download := slices.ContainsFunc(r.calls, func(call string) bool {
				return strings.HasPrefix(call, "gh release download v0.12.1 --repo ") &&
					strings.Contains(call+" ", " --pattern "+c.asset+" ") &&
					strings.Contains(call, " --pattern SHA256SUMS") &&
					strings.Contains(call+" ", " --dir "+binDir+" ")
			})
			if !download {
				t.Errorf("no download of %s: %q", c.asset, r.calls)
			}
			if !slices.Contains(r.calls, "sha256sum --check --ignore-missing SHA256SUMS") {
				t.Errorf("no checksum: %q", r.calls)
			}
			if callStarting(r.calls, "shasum") {
				t.Errorf("shasum next to sha256sum: %q", r.calls)
			}
			if !isExecutable(filepath.Join(binDir, "lighthouse")) {
				t.Error("bin/lighthouse is not an executable file")
			}
			if pathExists(filepath.Join(binDir, c.asset)) {
				t.Errorf("bin/%s is still there", c.asset)
			}
			if got, err := os.ReadFile(r.githubPath); err != nil || string(got) != binDir+"\n" {
				t.Errorf("GITHUB_PATH holds %q (%v), want %q", got, err, binDir+"\n")
			}
		})
	}
}

// Classification: fails when a runner without sha256sum (macOS) fails the step instead of checking
// with shasum.
func TestUnblockGetLighthouseVerifiesWithShasumWhereSha256sumIsMissing(t *testing.T) {
	r := runGetLighthouse(t, "0.12.1", "macOS", "ARM64", false, false)
	if r.code != 0 {
		t.Fatalf("exit %d, stdout %s", r.code, r.stdout)
	}
	if !slices.Contains(r.calls, "shasum -a 256 -c --ignore-missing SHA256SUMS") {
		t.Errorf("no shasum check: %q", r.calls)
	}
	if callStarting(r.calls, "sha256sum") {
		t.Errorf("sha256sum called: %q", r.calls)
	}
}

// Classification: fails when an unsupported runner OS or architecture downloads anything, puts a
// binary on the path, or exits without the error that names it.
func TestUnblockGetLighthouseRefusesAnUnknownRunner(t *testing.T) {
	for _, c := range []struct{ os, arch, message string }{
		{"Linux", "X86", "runner architecture X86"},
		{"Windows", "X64", "runner OS Windows"},
	} {
		t.Run(c.os+"/"+c.arch, func(t *testing.T) {
			r := runGetLighthouse(t, "0.12.1", c.os, c.arch, false, true)
			if r.code != 1 {
				t.Errorf("exit %d, want 1", r.code)
			}
			wantAll(t, r.stdout, "::error::no lighthouse binary for "+c.message+"\n")
			if callStarting(r.calls, "gh ") {
				t.Errorf("gh called: %q", r.calls)
			}
			if pathExists(filepath.Join(r.runnerTemp, "bin", "lighthouse")) {
				t.Error("bin/lighthouse exists")
			}
			if pathExists(r.githubPath) {
				t.Error("GITHUB_PATH was written")
			}
		})
	}
}

// Classification: fails when a `dev` manifest in a checkout with cmd/lighthouse downloads a
// release instead of building the binary from the checkout.
func TestUnblockGetLighthouseBuildsDevFromTheCheckout(t *testing.T) {
	r := runGetLighthouse(t, "dev", "Linux", "X64", true, true)
	if r.code != 0 {
		t.Fatalf("exit %d, stdout %s", r.code, r.stdout)
	}
	if want := []string{"go build -o " + r.runnerTemp + "/bin/lighthouse ./cmd/lighthouse"}; !slices.Equal(r.calls, want) {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
	if !pathExists(filepath.Join(r.runnerTemp, "bin", "lighthouse")) {
		t.Error("bin/lighthouse is missing")
	}
}

// Classification: fails when a `dev` manifest without the source to build runs anything, or exits
// without the error that says so.
func TestUnblockGetLighthouseRefusesDevWithoutASource(t *testing.T) {
	r := runGetLighthouse(t, "dev", "Linux", "X64", false, true)
	if r.code != 1 {
		t.Errorf("exit %d, want 1", r.code)
	}
	wantAll(t, r.stdout,
		"::error::.lighthouse/manifest.toml says version = \"dev\", but this checkout has no cmd/lighthouse to build\n")
	if len(r.calls) != 0 {
		t.Errorf("calls %q, want none", r.calls)
	}
	if pathExists(filepath.Join(r.runnerTemp, "bin", "lighthouse")) {
		t.Error("bin/lighthouse exists")
	}
}

// tokenName is one run of the `Token name` step.
type tokenName struct {
	code      int
	stdout    string
	stderr    string
	githubEnv string
}

// runTokenName runs the `Token name` step with `bash -e`, as the runner does, with GH_TOKEN set
// to the App token. lighthouse is a fake with body fakeBody; an empty fakeBody leaves it out of
// PATH. The rest of PATH is only the real tools the step needs.
func runTokenName(t *testing.T, fakeBody string) tokenName {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the step is a bash script")
	}
	bash := needTool(t, "bash")
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"mktemp", "grep", "cat"} {
		if err := os.Symlink(needTool(t, tool), filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	fakes := filepath.Join(tmp, "fakes")
	if err := os.MkdirAll(fakes, 0o755); err != nil {
		t.Fatal(err)
	}
	if fakeBody != "" {
		path := filepath.Join(fakes, "lighthouse")
		writeFile(t, path, "#!"+bash+"\n"+fakeBody)
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	githubEnv := filepath.Join(tmp, "github_env")
	c := exec.Command(bash, "-e", "-c", unblockStepNamed(t, "Token name").Run)
	c.Dir = tmp
	c.Env = []string{
		"PATH=" + fakes + string(os.PathListSeparator) + bin,
		"HOME=" + tmp,
		"TMPDIR=" + tmp,
		"GITHUB_ENV=" + githubEnv,
		"GH_TOKEN=app-token",
	}
	var stdout, stderr strings.Builder
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	code := 0
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(githubEnv)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return tokenName{code, stdout.String(), stderr.String(), string(env)}
}

// Classification: fails when the step stops exporting the App token under the name
// `forge.token_env` gives, or stops asking for exactly that key.
func TestUnblockTokenNameExportsUnderTheConfiguredName(t *testing.T) {
	got := runTokenName(t, "[ \"$1 $2\" = \"config forge.token_env\" ] || exit 2\necho FORGEJO_TOKEN\n")
	if got.code != 0 {
		t.Fatalf("exit %d, stderr %s", got.code, got.stderr)
	}
	if got.githubEnv != "FORGEJO_TOKEN=app-token\n" {
		t.Errorf("GITHUB_ENV = %q", got.githubEnv)
	}
}

// Classification: fails when an unset `forge.token_env` (rc 1, "is not set") starts failing the
// step or exporting a name.
func TestUnblockTokenNameUnsetExportsNothing(t *testing.T) {
	got := runTokenName(t, "echo \"config: $2 is not set\" >&2\nexit 1\n")
	if got.code != 0 {
		t.Fatalf("exit %d, stderr %s", got.code, got.stderr)
	}
	if got.githubEnv != "" {
		t.Errorf("GITHUB_ENV = %q, want empty", got.githubEnv)
	}
}

// Classification: fails when a failing `lighthouse config` other than "is not set" (an old binary
// with rc 1, any rc 2) is ignored instead of stopping the step with its stderr shown.
func TestUnblockTokenNameConfigFailureStopsTheStep(t *testing.T) {
	for _, c := range []struct{ name, body, shown string }{
		{"old binary", "echo \"unknown command\" >&2\nexit 1\n", "unknown command"},
		{"rc 2", "echo boom >&2\nexit 2\n", "boom"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := runTokenName(t, c.body)
			if got.code != 1 {
				t.Errorf("exit %d, want 1", got.code)
			}
			wantAll(t, got.stderr, c.shown)
			if got.githubEnv != "" {
				t.Errorf("GITHUB_ENV = %q, want empty", got.githubEnv)
			}
		})
	}
}

// Classification: fails when a missing lighthouse binary lets the step pass.
func TestUnblockTokenNameMissingBinaryStopsTheStep(t *testing.T) {
	got := runTokenName(t, "")
	if got.code == 0 {
		t.Error("exit 0 without a lighthouse binary")
	}
	if got.githubEnv != "" {
		t.Errorf("GITHUB_ENV = %q, want empty", got.githubEnv)
	}
}

// Classification: fails when a value that is no environment variable name (a space, a second
// line) reaches GITHUB_ENV instead of stopping the step.
func TestUnblockTokenNameInvalidNameStopsTheStep(t *testing.T) {
	got := runTokenName(t, "printf 'BAD NAME\\nX=1\\n'\n")
	if got.code != 1 {
		t.Errorf("exit %d, want 1", got.code)
	}
	wantAll(t, got.stdout, "not a valid environment variable name")
	if got.githubEnv != "" {
		t.Errorf("GITHUB_ENV = %q, want empty", got.githubEnv)
	}
}

// Classification: fails when `Token name` stops running after the binary and the App token and
// before the release, stops reading the App token, or the release loses GH_REPO.
func TestUnblockTokenNameRunsAfterTheBinaryAndBeforeTheRelease(t *testing.T) {
	var names []string
	for _, s := range unblockSteps(t) {
		names = append(names, s.Name)
	}
	wantOrder(t, strings.Join(names, "\n"),
		"Get lighthouse", "App token for labels and comments", "Token name", "Release dependents")
	if got := unblockStepNamed(t, "Token name").Env["GH_TOKEN"]; got != "${{ steps.app-token.outputs.token }}" {
		t.Errorf("Token name GH_TOKEN %q", got)
	}
	if got := unblockStepNamed(t, "Release dependents").Env["GH_REPO"]; got != "${{ github.repository }}" {
		t.Errorf("Release dependents GH_REPO %q", got)
	}
}
