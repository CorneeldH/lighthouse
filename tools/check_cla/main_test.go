package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// --- throwaway git repositories ------------------------------------------------------------

type person struct{ name, email string }

func (p person) env() []string {
	return []string{
		"GIT_AUTHOR_NAME=" + p.name, "GIT_AUTHOR_EMAIL=" + p.email,
		"GIT_COMMITTER_NAME=" + p.name, "GIT_COMMITTER_EMAIL=" + p.email,
	}
}

var (
	alice  = person{"Alice Example", "alice@example.com"}
	bob    = person{"Bob Example", "bob@example.com"}
	owner  = person{"Corneel den Hartogh", "owner@example.com"}
	anySHA = "<any>"
)

// isolateGit keeps the developer's git configuration out of the tests: a global `user.email` or
// a `commit.gpgsign` must not steer them.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func git(t *testing.T, repo string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, nil, "init", "-q", "-b", "main")
	return repo
}

// commit makes a commit with the given message, author and committer.
func commit(t *testing.T, repo, message string, author, committer person) {
	t.Helper()
	writeFile(t, filepath.Join(repo, "file.txt"), message)
	git(t, repo, nil, "add", "file.txt")
	git(t, repo, []string{
		"GIT_AUTHOR_NAME=" + author.name, "GIT_AUTHOR_EMAIL=" + author.email,
		"GIT_COMMITTER_NAME=" + committer.name, "GIT_COMMITTER_EMAIL=" + committer.email,
	}, "commit", "-q", "-m", message)
}

// commitMessageFile commits with `-F`, for a message that does not travel over a command line.
func commitMessageFile(t *testing.T, repo, message string, who person) {
	t.Helper()
	writeFile(t, filepath.Join(repo, "file.txt"), message)
	git(t, repo, nil, "add", "file.txt")
	msg := filepath.Join(t.TempDir(), "message.txt")
	writeFile(t, msg, message)
	git(t, repo, who.env(), "commit", "-q", "--cleanup=verbatim", "-F", msg)
}

// rangeRepo is a repository with one commit on branch `base`; the test adds commits after it.
func rangeRepo(t *testing.T) string {
	t.Helper()
	repo := initRepo(t)
	commit(t, repo, "base commit", alice, alice)
	git(t, repo, nil, "branch", "base")
	return repo
}

// repoWithRegister is a repository whose `base` branch carries the register `text`.
func repoWithRegister(t *testing.T, text string) (repo, path string) {
	t.Helper()
	repo = initRepo(t)
	path = filepath.Join(repo, ".github", "cla-signatures.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, text)
	git(t, repo, nil, "add", ".github/cla-signatures.json")
	git(t, repo, owner.env(), "commit", "-q", "-m", "register")
	git(t, repo, nil, "branch", "base")
	return repo, path
}

// collect returns the identities in base..HEAD with the SHA blanked, as it changes every run.
func collect(t *testing.T, repo string) []Identity {
	t.Helper()
	ids, err := collectIdentities(repo, "base", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for i := range ids {
		ids[i].Commit = anySHA
	}
	return ids
}

// --- the command ---------------------------------------------------------------------------

const lighthouseTOML = "[forge]\nrepo = \"Other/repo\"\n"

// runIn runs the command against repo, with its register written to the default place.
func runIn(t *testing.T, repo string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(append([]string{"--repo", repo, "--base", "base"}, args...), &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeRegister(t *testing.T, repo, text string) {
	t.Helper()
	path := filepath.Join(repo, ".github", "cla-signatures.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, text)
}

func TestRunEmptyBaseOrHeadExits2(t *testing.T) {
	repo := rangeRepo(t)
	writeRegister(t, repo, registerText)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"empty base", []string{"--repo", repo, "--base", ""}, "`--base` is '' and `--head` is 'HEAD'; without a range"},
		{"empty head", []string{"--repo", repo, "--base", "base", "--head", ""}, "`--base` is 'base' and `--head` is ''; without a range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tc.args, &out, &errOut)
			if code != 2 || !strings.HasPrefix(out.String(), "## CLA: no range to measure\n\n") ||
				!strings.Contains(out.String(), tc.want) || errOut.String() != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunAllCoveredExits0(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", person{"Jane Doe", "jane@example.com"}, owner)
	writeRegister(t, repo, registerText)
	code, stdout, stderr := runIn(t, repo)
	if code != 0 || stdout != "CLA: 2 identities in base..HEAD, all covered.\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunBotCommitExits0(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "bump", person{"Some Bot", "bot@example.com"}, owner)
	writeRegister(t, repo, registerText)
	if code, stdout, _ := runIn(t, repo); code != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunUnsignedIdentityExits1WithReport(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", person{"Mallory", "mallory@example.com"}, owner)
	writeRegister(t, repo, registerText)
	writeFile(t, filepath.Join(repo, "lighthouse.toml"), lighthouseTOML)
	code, stdout, stderr := runIn(t, repo)
	if code != 1 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"## CLA: not every contributor has signed\n",
		"| `mallory@example.com` | `Mallory` | author | `",
		"[`CLA.md`](https://github.com/Other/repo/blob/main/CLA.md),",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "owner@example.com") {
		t.Errorf("a covered address is reported:\n%s", stdout)
	}
}

func TestRunUnknownCoAuthorExits1(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work\n\nCo-authored-by: Mallory <mallory@example.com>\n", owner, owner)
	writeRegister(t, repo, registerText)
	writeFile(t, filepath.Join(repo, "lighthouse.toml"), lighthouseTOML)
	code, stdout, _ := runIn(t, repo)
	if code != 1 || !strings.Contains(stdout, "| `mallory@example.com` | `Mallory` | co-author | `") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunUnsignedWithoutLighthouseTomlExits2(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", person{"Mallory", "mallory@example.com"}, owner)
	writeRegister(t, repo, registerText)
	code, stdout, _ := runIn(t, repo)
	if code != 2 || !strings.HasPrefix(stdout, "## CLA: lighthouse.toml is unreadable\n") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestReportLandsInStepSummary(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", person{"Mallory", "mallory@example.com"}, owner)
	writeRegister(t, repo, registerText)
	writeFile(t, filepath.Join(repo, "lighthouse.toml"), lighthouseTOML)
	summary := filepath.Join(t.TempDir(), "summary.md")
	writeFile(t, summary, "earlier step\n")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	code, stdout, _ := runIn(t, repo)
	got, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	// The summary is appended to, and holds the report without the extra newline print adds.
	if code != 1 || string(got) != "earlier step\n"+strings.TrimSuffix(stdout, "\n") {
		t.Fatalf("code=%d summary=%q stdout=%q", code, got, stdout)
	}
}

func TestRunInvalidRegisterExits2(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", owner, owner)
	writeRegister(t, repo, edited(t, "I have read the CLA document", "nope"))
	code, stdout, _ := runIn(t, repo)
	want := "## CLA: the register is invalid\n\n- signatures[0]: `agreement` must be exactly 'I have read the CLA document and I hereby sign the CLA.'\n\n"
	if code != 2 || stdout != want {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunUnreadableRegisterExits2(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", owner, owner)
	code, stdout, _ := runIn(t, repo, "--register", filepath.Join(repo, "absent.json"))
	want := "## CLA: the register is unreadable\n\n```\n" + filepath.Join(repo, "absent.json") + " does not exist\n```\n\n"
	if code != 2 || stdout != want {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunUnknownBaseExits2(t *testing.T) {
	repo := rangeRepo(t)
	writeRegister(t, repo, registerText)
	var out, errOut bytes.Buffer
	code := run([]string{"--repo", repo, "--base", "origin/does-not-exist"}, &out, &errOut)
	if code != 2 || !strings.HasPrefix(out.String(), "## CLA: git failed\n\n```\ngit ") {
		t.Fatalf("code=%d stdout=%q", code, out.String())
	}
}

func TestRunRegisterNotAppendOnlyExits2(t *testing.T) {
	repo, _ := repoWithRegister(t, registerText)
	writeRegister(t, repo, edited(t, `[`+sigText+`]`, `[]`))
	commit(t, repo, "work", owner, owner)
	code, stdout, _ := runIn(t, repo)
	want := "## CLA: the register was changed, not appended to\n\n" +
		"A signature that disappears or changes is a silently withdrawn grant. Only additions are allowed.\n\n" +
		"- the signature of `janedoe` was removed or changed compared to `base`\n\n"
	if code != 2 || stdout != want {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunSigningInTheSamePullRequestPasses(t *testing.T) {
	repo, _ := repoWithRegister(t, registerText)
	writeRegister(t, repo, edited(t, `"signatures": [`, `"signatures": [`+johnText+`,`))
	commit(t, repo, "work", person{"John Roe", "john@example.com"}, person{"John Roe", "john@example.com"})
	if code, stdout, _ := runIn(t, repo); code != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestRunDefaultsRegisterToTheRepo(t *testing.T) {
	repo := rangeRepo(t)
	commit(t, repo, "work", owner, owner)
	writeRegister(t, repo, registerText)
	var out, errOut bytes.Buffer
	t.Chdir(repo)
	if code := run([]string{"--base", "base"}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"stray"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 || out.String() != "" || errOut.String() == "" {
			t.Errorf("%v: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

// --- the workflow --------------------------------------------------------------------------
//
// Invariants of `.github/workflows/cla.yml` that no later edit may lose: the lane runs fork code
// on every pull request, so where and with what rights it runs is the whole point.

func loadWorkflow(t *testing.T) (text string, doc map[string]any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "cla.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return string(data), doc
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%v is not a mapping", v)
	}
	return m
}

func claSteps(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	job := asMap(t, asMap(t, doc["jobs"])["cla"])
	var steps []map[string]any
	for _, s := range job["steps"].([]any) {
		steps = append(steps, asMap(t, s))
	}
	return steps
}

func TestWorkflowNeverRunsSelfHostedAndIsReadOnly(t *testing.T) {
	_, doc := loadWorkflow(t)
	readOnly := map[string]any{"contents": "read"}
	if got := asMap(t, doc["permissions"]); !mapsEqual(got, readOnly) {
		t.Fatalf("top-level permissions %v", got)
	}
	jobs := asMap(t, doc["jobs"])
	if len(jobs) == 0 {
		t.Fatal("no jobs")
	}
	for name, j := range jobs {
		job := asMap(t, j)
		if job["runs-on"] != "ubuntu-latest" {
			t.Errorf("job %s runs on %v", name, job["runs-on"])
		}
		if got := asMap(t, job["permissions"]); !mapsEqual(got, readOnly) {
			t.Errorf("job %s permissions %v", name, got)
		}
	}
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestWorkflowReachesNoSecrets(t *testing.T) {
	text, _ := loadWorkflow(t)
	for _, bad := range []string{"pull_request_target", "secrets.", "secrets[", "secrets)", "github.token"} {
		if strings.Contains(text, bad) {
			t.Errorf("the workflow contains %q", bad)
		}
	}
}

func TestWorkflowRunsOnEveryPullRequest(t *testing.T) {
	_, doc := loadWorkflow(t)
	pr := asMap(t, asMap(t, doc["on"])["pull_request"])
	branches, _ := pr["branches"].([]any)
	if len(branches) != 1 || branches[0] != "main" {
		t.Errorf("branches %v", pr["branches"])
	}
	for _, key := range []string{"paths", "paths-ignore"} {
		if _, ok := pr[key]; ok {
			t.Errorf("a `%s` filter lets a pull request reach main uncovered", key)
		}
	}
}

func TestWorkflowChecksOutFullHistoryWithoutCredentials(t *testing.T) {
	_, doc := loadWorkflow(t)
	steps := claSteps(t, doc)
	var checkout map[string]any
	for _, s := range steps {
		if uses, _ := s["uses"].(string); strings.HasPrefix(uses, "actions/checkout@") {
			checkout = s
		}
	}
	if checkout == nil {
		t.Fatal("no checkout step")
	}
	with := asMap(t, checkout["with"])
	if with["fetch-depth"] != 0 || with["persist-credentials"] != false {
		t.Errorf("checkout with %v", with)
	}
	// An explicit ref checks out only that ancestry, so the base would not be local; and a later
	// `git fetch` would be anonymous.
	if _, ok := with["ref"]; ok {
		t.Error("an explicit `ref:` leaves the base out of the checkout")
	}
	for _, s := range steps {
		if run, _ := s["run"].(string); strings.Contains(run, "git fetch") {
			t.Errorf("a step fetches after the checkout: %q", run)
		}
	}
}

func TestWorkflowMeasuresThePullRequestRangeWithGo(t *testing.T) {
	_, doc := loadWorkflow(t)
	steps := claSteps(t, doc)
	check, setup := -1, -1
	for i, s := range steps {
		if uses, _ := s["uses"].(string); strings.HasPrefix(uses, "actions/setup-go@") {
			setup = i
		}
		if s["name"] == "Check the CLA" {
			check = i
		}
	}
	if check < 0 || setup < 0 || setup > check {
		t.Fatalf("setup-go at %d must come before the check at %d", setup, check)
	}
	with := asMap(t, steps[setup]["with"])
	if with["go-version-file"] != "go.mod" || with["cache"] != false {
		t.Errorf("setup-go with %v", with)
	}
	env := asMap(t, steps[check]["env"])
	if env["BASE_SHA"] != "${{ github.event.pull_request.base.sha }}" ||
		env["HEAD_SHA"] != "${{ github.event.pull_request.head.sha }}" {
		t.Errorf("env %v", env)
	}
	if want := `go run ./tools/check_cla --base "$BASE_SHA" --head "$HEAD_SHA"`; steps[check]["run"] != want {
		t.Errorf("run %q, want %q", steps[check]["run"], want)
	}
}
