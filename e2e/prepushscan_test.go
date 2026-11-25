//go:build e2e && linux

package e2e

// The pre-push secret scan in tools/hooks/ (#194), as a black box (#364).
//
// The pushes are real: a fixture repo with bare remotes, the scan installed as its
// .git/hooks/pre-push, and the test reads what reached the remote. Only the cases git
// cannot reach without a network (a non-local remote URL) call the script directly with
// the ref lines git would hand it.
//
// gitleaks is needed for every real scan. Without it those tests skip, unless
// LIGHTHOUSE_REQUIRE_GITLEAKS is set: the secret-scan job in CI sets it, so a runner
// without gitleaks fails there instead of skipping.
//
// The scan bounds every gitleaks run through `lighthouse checkpoint-scan` (#198). No test
// depends on the host for that: each one gets its own host.toml and pass-through stubs for
// systemctl, systemd-run and choom first on PATH. The tests of the split between blob mode
// and commit range use a fake gitleaks that logs how it was called.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/stubbin"
)

const zeroSHA = "0000000000000000000000000000000000000000"

// fakePAT is a GitHub PAT that gitleaks' github-pat rule matches. Joined at runtime, so
// this file carries no match for the repository's own scan.
var fakePAT = strings.Join([]string{"ghp_", "Zq9XmW2pL7vR4tY8uN3bK6cJ1hF5dG0sA2eQ"}, "")

// passThrough drops everything up to the first `--` and runs the rest.
const passThrough = "while [ \"$#\" -gt 0 ]; do\n" +
	"  if [ \"$1\" = \"--\" ]; then shift; break; fi\n" +
	"  shift\n" +
	"done\n" +
	"exec \"$@\"\n"

// boundedStubs make `systemctl --user` work and let systemd-run and choom run their command.
var boundedStubs = map[string]string{
	"systemd-run": passThrough,
	"choom":       passThrough,
}

// systemdRunKilledExit is what a shell reports for a child the kernel killed at the ceiling.
const systemdRunKilledExit = 137

// fakeGitleaks logs its subcommand and arguments, one call per line, to $FAKE_GITLEAKS_LOG,
// prints a version, writes an empty report for `dir` and says, like gitleaks, how many bytes
// it scanned (the files in its cwd), and finds nothing.
const fakeGitleaks = `echo "$*" >> "$FAKE_GITLEAKS_LOG"
case "$1" in
  version) echo "fake 1" ;;
  dir)
    echo "INF scanned ~$(( $(cat -- * | wc -c) )) bytes" >&2
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "-r" ]; then echo "[]" > "$2"; fi
      shift
    done
    ;;
esac
exit 0
`

var (
	scanLHOnce sync.Once
	scanLH     string
	scanLHErr  string
)

// scanLighthouse is the lighthouse of this checkout, built once for all scan tests: the scan
// reads its settings and limits through `lighthouse checkpoint-scan`.
func scanLighthouse(t *testing.T) string {
	t.Helper()
	scanLHOnce.Do(func() {
		dir, err := os.MkdirTemp(e2eState, "scan-bin-")
		if err != nil {
			scanLHErr = err.Error()
			return
		}
		bin := filepath.Join(dir, "lighthouse")
		out, err := exec.Command("go", "build", "-o", bin, "github.com/CorneeldH/lighthouse/cmd/lighthouse").CombinedOutput()
		if err != nil {
			scanLHErr = err.Error() + "\n" + string(out)
			return
		}
		scanLH = bin
	})
	if scanLHErr != "" {
		t.Fatalf("go build: %s", scanLHErr)
	}
	return scanLH
}

// needGitleaks skips a test without gitleaks, or fails it where LIGHTHOUSE_REQUIRE_GITLEAKS is
// set (the secret-scan job), so a missing gitleaks there is never a silent skip.
func needGitleaks(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gitleaks"); err == nil {
		return
	}
	if os.Getenv("LIGHTHOUSE_REQUIRE_GITLEAKS") != "" {
		t.Fatal("gitleaks not on PATH while LIGHTHOUSE_REQUIRE_GITLEAKS is set")
	}
	t.Skip("gitleaks is not installed")
}

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type result struct {
	rc     int
	stderr string
}

// scanFx is a repo on branch `work`, a bare `origin` holding it and an empty bare `ckpt`,
// entire configured the way lighthouse is (no automatic push, `ckpt` as the checkpoint
// remote), and the scan as the repo's pre-push hook.
type scanFx struct {
	t                         *testing.T
	dir, repo, origin, ckpt   string
	stubs, lighthouse, script string
	env                       []string
}

// newScanEnv is the environment of every scan test: its own host.toml and the bounding
// stubs first on PATH, the built lighthouse after them.
func newScanEnv(t *testing.T) *scanFx {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lh := scanLighthouse(t)
	host := filepath.Join(dir, "host.toml")
	writeAt(t, host, "[host]\nname = \"h\"\ntailscale_ip = \"100.64.0.1\"\n")
	stubs := filepath.Join(dir, "stubs")
	if err := os.Mkdir(stubs, 0o755); err != nil {
		t.Fatal(err)
	}
	stubbin.RequirePOSIX(t, "the scan is a bash script that runs these stubs through sh")
	stubbin.Make(t, stubs, "systemctl", stubbin.Spec{})
	for name, body := range boundedStubs {
		stubbin.MakeScript(t, stubs, name, body)
	}
	env := overrideEnv(append(cleanEnv(), gitEnv...),
		"LIGHTHOUSE_HOST_TOML="+host,
		"PATH="+stubs+string(os.PathListSeparator)+filepath.Dir(lh)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &scanFx{t: t, dir: dir, stubs: stubs, lighthouse: lh, env: env,
		script: repoFile(t, "tools/hooks/pre-push-secret-scan.sh")}
}

func newScanFx(t *testing.T) *scanFx {
	t.Helper()
	fx := newScanEnv(t)
	fx.repo = fx.initRepo("repo")
	fx.origin, fx.ckpt = fx.bare("origin.git"), fx.bare("ckpt.git")
	fx.git(fx.repo, "remote", "add", "origin", fx.origin)
	fx.git(fx.repo, "remote", "add", "ckpt", fx.ckpt)
	fx.git(fx.repo, "push", "-q", "origin", "work")
	fx.settings(map[string]any{"push_sessions": false}, map[string]any{"checkpoint_push_remote": "ckpt"})
	writeExe(t, filepath.Join(fx.repo, ".git", "hooks"), "pre-push", "#!/bin/sh\nexec bash \""+fx.script+"\" \"$@\"\n")
	return fx
}

func (fx *scanFx) run(dir, stdin string, extra []string, name string, args ...string) (int, string, string) {
	fx.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = overrideEnv(fx.env, extra...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exitErr) {
		fx.t.Fatalf("%s %v: %v", name, args, err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

// git runs git in dir and fails the test on a non-zero exit; it returns the trimmed stdout.
func (fx *scanFx) git(dir string, args ...string) string {
	fx.t.Helper()
	return fx.gitIn(dir, "", args...)
}

func (fx *scanFx) gitIn(dir, stdin string, args ...string) string {
	fx.t.Helper()
	rc, out, errOut := fx.run(dir, stdin, nil, "git", append([]string{"-C", dir}, args...)...)
	if rc != 0 {
		fx.t.Fatalf("git %v: rc %d\n%s", args, rc, errOut)
	}
	return strings.TrimSpace(out)
}

func (fx *scanFx) initRepo(name string) string {
	fx.t.Helper()
	repo := filepath.Join(fx.dir, name)
	if err := os.Mkdir(repo, 0o755); err != nil {
		fx.t.Fatal(err)
	}
	fx.git(repo, "init", "-q", "-b", "work")
	writeAt(fx.t, filepath.Join(repo, "README.md"), "# repo\n")
	fx.git(repo, "add", "README.md")
	fx.git(repo, "commit", "-qm", "init")
	return repo
}

func (fx *scanFx) bare(name string) string {
	fx.t.Helper()
	p := filepath.Join(fx.dir, name)
	fx.git(fx.dir, "init", "-q", "--bare", p)
	return p
}

func (fx *scanFx) commitIn(repo, name, text string) string {
	fx.t.Helper()
	writeAt(fx.t, filepath.Join(repo, name), text)
	fx.git(repo, "add", name)
	fx.git(repo, "commit", "-qm", "edit "+name)
	return fx.git(repo, "rev-parse", "HEAD")
}

func (fx *scanFx) commit(name, text string) string {
	fx.t.Helper()
	return fx.commitIn(fx.repo, name, text)
}

func (fx *scanFx) push(remote string, refspecs ...string) result {
	fx.t.Helper()
	return fx.pushEnv(nil, remote, refspecs...)
}

func (fx *scanFx) pushEnv(extra []string, remote string, refspecs ...string) result {
	fx.t.Helper()
	rc, _, errOut := fx.run(fx.repo, "", extra, "git", append([]string{"-C", fx.repo, "push", remote}, refspecs...)...)
	return result{rc, errOut}
}

func (fx *scanFx) remoteRefs(bare string) []string {
	fx.t.Helper()
	out := fx.git(bare, "for-each-ref", "--format=%(refname)")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func (fx *scanFx) settings(strategy, local map[string]any) {
	fx.t.Helper()
	write := func(name string, body map[string]any) {
		data, err := json.Marshal(body)
		if err != nil {
			fx.t.Fatal(err)
		}
		writeAt(fx.t, filepath.Join(fx.repo, ".entire", name), string(data))
	}
	write("settings.json", map[string]any{"enabled": true, "strategy_options": strategy})
	if local != nil {
		write("settings.local.json", map[string]any{"strategy_options": local})
	}
}

// checkpointRef writes a checkpoint ref the way entire's git-refs backend does: an orphan
// commit under refs/entire/checkpoints/ holding a transcript.
func (fx *scanFx) checkpointRef(name, text string) string {
	fx.t.Helper()
	blob := fx.gitIn(fx.repo, text, "hash-object", "-w", "--stdin")
	tree := fx.gitIn(fx.repo, "100644 blob "+blob+"\tfull.jsonl\n", "mktree")
	sha := fx.git(fx.repo, "commit-tree", tree, "-m", "checkpoint")
	ref := "refs/entire/checkpoints/" + name[len(name)-2:] + "/" + name
	fx.git(fx.repo, "update-ref", ref, sha)
	return ref
}

// runScan calls the script directly, with path as the whole PATH.
func (fx *scanFx) runScan(remote, url, lines, path string) result {
	fx.t.Helper()
	rc, _, errOut := fx.run(fx.repo, lines, []string{"PATH=" + path}, "bash", fx.script, remote, url)
	return result{rc, errOut}
}

// useFakeGitleaks puts the fake gitleaks first on PATH and returns its log.
func (fx *scanFx) useFakeGitleaks() string {
	fx.t.Helper()
	log := filepath.Join(fx.dir, "gitleaks.log")
	fx.env = overrideEnv(fx.env, "FAKE_GITLEAKS_LOG="+log)
	stubbin.MakeScript(fx.t, fx.stubs, "gitleaks", fakeGitleaks)
	return log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func firstWords(lines []string) []string {
	var out []string
	for _, l := range lines {
		w, _, _ := strings.Cut(l, " ")
		out = append(out, w)
	}
	return out
}

// noGitleaksPath is a PATH with git, bash, cat, dirname and the built lighthouse, and no gitleaks.
func (fx *scanFx) noGitleaksPath() string {
	fx.t.Helper()
	return toolDir(fx.t, fx.lighthouse, []string{"git", "bash", "cat", "dirname"}, "", nil)
}

func wantBlocked(t *testing.T, r result, msg string) {
	t.Helper()
	if r.rc == 0 {
		t.Fatalf("push passed, want it blocked\n%s", r.stderr)
	}
	if !strings.Contains(r.stderr, msg) {
		t.Fatalf("stderr lacks %q:\n%s", msg, r.stderr)
	}
}

func wantPassed(t *testing.T, r result) {
	t.Helper()
	if r.rc != 0 {
		t.Fatalf("rc %d, want 0\n%s", r.rc, r.stderr)
	}
}

func TestPrePushScanCleanPushPasses(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	sha := fx.commit("a.txt", "nothing to see\n")
	wantPassed(t, fx.push("origin", "work"))
	if got := fx.git(fx.origin, "rev-parse", "refs/heads/work"); got != sha {
		t.Fatalf("origin work = %s, want %s", got, sha)
	}
}

// lighthouse's own settings name a checkpoint repo but no checkpoint push remote: the empty
// last setting must not end the hook.
func TestPrePushScanPassesWithoutACheckpointPushRemote(t *testing.T) {
	fx := newScanFx(t)
	if err := os.Remove(filepath.Join(fx.repo, ".entire", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
	fx.settings(map[string]any{"push_sessions": false,
		"checkpoint_remote": map[string]any{"provider": "github", "repo": "o/r"}}, nil)
	sha := fx.commit("a.txt", "nothing to see\n")
	wantPassed(t, fx.push("origin", "work"))
	if got := fx.git(fx.origin, "rev-parse", "refs/heads/work"); got != sha {
		t.Fatalf("origin work = %s, want %s", got, sha)
	}
}

func TestPrePushScanBlocksASecretInTheTipOfANewBranch(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	fx.git(fx.repo, "checkout", "-qb", "leak")
	fx.commit("env.txt", "GITHUB_PAT="+fakePAT+"\n")
	wantBlocked(t, fx.push("origin", "leak"), "BLOCKED")
	if slices.Contains(fx.remoteRefs(fx.origin), "refs/heads/leak") {
		t.Fatal("refs/heads/leak reached origin")
	}
}

func TestPrePushScanBlocksASecretAddedAndRemovedBeforeTheTip(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	fx.commit("env.txt", "GITHUB_PAT="+fakePAT+"\n")
	fx.commit("env.txt", "GITHUB_PAT=\n")
	before := fx.git(fx.origin, "rev-parse", "refs/heads/work")
	wantBlocked(t, fx.push("origin", "work"), "BLOCKED")
	if got := fx.git(fx.origin, "rev-parse", "refs/heads/work"); got != before {
		t.Fatalf("origin work moved to %s", got)
	}
}

// The range is what the remote lacks: an old leak there is history's problem, not this
// push's, and must not block every later push.
func TestPrePushScanSkipsASecretAlreadyOnTheRemote(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	fx.commit("env.txt", "GITHUB_PAT="+fakePAT+"\n")
	fx.git(fx.repo, "push", "-q", "--no-verify", "origin", "work")
	fx.commit("env.txt", "GITHUB_PAT=\n")
	wantPassed(t, fx.push("origin", "work"))
}

// With a separate pushurl --remotes excludes nothing: only the remote sha of the ref line
// keeps an old leak on the push repo out of the range.
func TestPrePushScanSkipsASecretAlreadyOnAnotherPushURL(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	fx.git(fx.repo, "remote", "set-url", "--push", "origin", fx.bare("push.git"))
	fx.git(fx.repo, "push", "-q", "--no-verify", "origin", "work")
	fx.commit("env.txt", "GITHUB_PAT="+fakePAT+"\n")
	fx.git(fx.repo, "push", "-q", "--no-verify", "origin", "work")
	fx.commit("env.txt", "GITHUB_PAT=\n")
	wantPassed(t, fx.push("origin", "work"))
}

// refs/remotes/origin/* follow the fetch URL. With a separate pushurl the push repo may lack
// what they hold, so they exclude nothing from the range.
func TestPrePushScanScansTheFetchURLCommitForAnotherPushURL(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	push := fx.bare("push.git")
	fx.git(fx.repo, "remote", "set-url", "--push", "origin", push)
	fx.git(fx.repo, "checkout", "-qb", "leak")
	fx.commit("env.txt", "GITHUB_PAT="+fakePAT+"\n")
	fx.git(fx.repo, "push", "-q", "--no-verify", fx.origin, "leak")
	fx.git(fx.repo, "fetch", "-q", "origin")
	if !strings.Contains(fx.git(fx.repo, "for-each-ref", "--format=%(refname)"), "refs/remotes/origin/leak") {
		t.Fatal("setup: no refs/remotes/origin/leak")
	}
	wantBlocked(t, fx.push("origin", "leak"), "found a secret")
	if slices.Contains(fx.remoteRefs(push), "refs/heads/leak") {
		t.Fatal("refs/heads/leak reached the push URL")
	}
}

// Push URL and fetch URL are one: what refs/remotes/origin/* hold is there. The leak is pushed
// past the hook to another branch, so the ref line of `next` carries no remote sha; only
// --remotes=origin leaves the leak out.
func TestPrePushScanSkipsCommitsOnTheRemoteTrackingRefs(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	fx.git(fx.repo, "checkout", "-qb", "leak")
	fx.commit("env.txt", "GITHUB_PAT="+fakePAT+"\n")
	fx.git(fx.repo, "push", "-q", "--no-verify", "origin", "leak")
	fx.commit("env.txt", "GITHUB_PAT=\n")
	wantPassed(t, fx.push("origin", "HEAD:refs/heads/next"))
}

func TestPrePushScanBlocksACheckpointRefToAnotherRemote(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	ref := fx.checkpointRef("01AAAAAAAAAAAAAAAAAAAAAAAA", "{\"text\": \"clean\"}\n")
	wantBlocked(t, fx.push("origin", ref+":"+ref), "checkpoint remote")
	if slices.Contains(fx.remoteRefs(fx.origin), ref) {
		t.Fatalf("%s reached origin", ref)
	}
}

func TestPrePushScanBlocksAShadowBranchToAnotherRemote(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	fx.git(fx.repo, "branch", "entire/abc1234-e3b0c4")
	wantBlocked(t, fx.push("origin", "entire/abc1234-e3b0c4"), "checkpoint remote")
	if slices.Contains(fx.remoteRefs(fx.origin), "refs/heads/entire/abc1234-e3b0c4") {
		t.Fatal("the shadow branch reached origin")
	}
}

func TestPrePushScanCleanCheckpointRefReachesTheCheckpointRemote(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	ref := fx.checkpointRef("01BBBBBBBBBBBBBBBBBBBBBBBB", "{\"text\": \"clean\"}\n")
	wantPassed(t, fx.push("ckpt", ref+":"+ref))
	if !slices.Contains(fx.remoteRefs(fx.ckpt), ref) {
		t.Fatalf("%s did not reach ckpt", ref)
	}
}

func TestPrePushScanBlocksACheckpointRefWithASecret(t *testing.T) {
	needGitleaks(t)
	fx := newScanFx(t)
	clean := fx.checkpointRef("01CCCCCCCCCCCCCCCCCCCCCCCC", "{\"text\": \"clean\"}\n")
	leak := fx.checkpointRef("01DDDDDDDDDDDDDDDDDDDDDDDD", "{\"text\": \"GITHUB_PAT="+fakePAT+"\"}\n")
	wantBlocked(t, fx.push("ckpt", "refs/entire/checkpoints/*:refs/entire/checkpoints/*"), "BLOCKED")
	refs := fx.remoteRefs(fx.ckpt)
	if slices.Contains(refs, clean) || slices.Contains(refs, leak) {
		t.Fatalf("a checkpoint of the blocked push reached ckpt: %v", refs)
	}
}

func TestPrePushScanEntireRefsGoThroughBlobScan(t *testing.T) {
	fx := newScanFx(t)
	log := fx.useFakeGitleaks()
	var refs []string
	for _, c := range []string{"F", "G", "H"} {
		refs = append(refs, fx.checkpointRef("01"+strings.Repeat(c, 24), "{\"text\": \"checkpoint "+c+"\"}\n"))
	}
	wantPassed(t, fx.push("ckpt", "refs/entire/checkpoints/*:refs/entire/checkpoints/*"))
	remote := fx.remoteRefs(fx.ckpt)
	for _, ref := range refs {
		if !slices.Contains(remote, ref) {
			t.Fatalf("%s did not reach ckpt: %v", ref, remote)
		}
	}
	words := firstWords(calls(t, log))
	if !slices.Contains(words, "dir") || slices.Contains(words, "git") {
		t.Fatalf("want gitleaks dir and no gitleaks git, got %v", words)
	}
}

// refs/heads/entire/* sit on the work branch: blob mode would read the whole history the
// checkpoint remote lacks, so they go through the commit range.
func TestPrePushScanShadowBranchUsesCommitRange(t *testing.T) {
	fx := newScanFx(t)
	log := fx.useFakeGitleaks()
	fx.git(fx.repo, "branch", "entire/shadow")
	wantPassed(t, fx.push("ckpt", "entire/shadow"))
	var scans []string
	for _, line := range calls(t, log) {
		if w, _, _ := strings.Cut(line, " "); w == "git" || w == "dir" {
			scans = append(scans, line)
		}
	}
	if len(scans) == 0 {
		t.Fatal("no gitleaks scan ran")
	}
	for _, line := range scans {
		if !strings.HasPrefix(line, "git ") || !strings.Contains(line, "--log-opts=") {
			t.Fatalf("want only `gitleaks git --log-opts=...`, got %q", line)
		}
	}
}

func TestPrePushScanCeilingBlocksPush(t *testing.T) {
	for _, kind := range []string{"branch", "checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			// Without a fake gitleaks a host without one lets a push to a local path through
			// unscanned, and the ceiling would never be reached.
			fx := newScanFx(t)
			fx.useFakeGitleaks()
			stubbin.Make(t, fx.stubs, "systemd-run", stubbin.Spec{Exit: systemdRunKilledExit})
			var r result
			if kind == "branch" {
				fx.commit("a.txt", "nothing to see\n")
				before := fx.git(fx.origin, "rev-parse", "refs/heads/work")
				r = fx.push("origin", "work")
				if got := fx.git(fx.origin, "rev-parse", "refs/heads/work"); got != before {
					t.Fatalf("origin work moved to %s", got)
				}
			} else {
				ref := fx.checkpointRef("01KKKKKKKKKKKKKKKKKKKKKKKK", "{\"text\": \"clean\"}\n")
				r = fx.push("ckpt", ref+":"+ref)
				if slices.Contains(fx.remoteRefs(fx.ckpt), ref) {
					t.Fatalf("%s reached ckpt", ref)
				}
				// The batch at the ceiling is not clean: nothing of it in the ledger.
				ledgers, _ := filepath.Glob(filepath.Join(fx.repo, ".git", "lighthouse", "secret-scan", "clean-*.txt"))
				for _, p := range ledgers {
					data, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					if strings.TrimSpace(string(data)) != "" {
						t.Fatalf("ledger %s holds %q", p, data)
					}
				}
			}
			wantBlocked(t, r, "ABORTED at memory ceiling")
			if strings.Contains(r.stderr, "found a secret") {
				t.Fatalf("a ceiling hit reads as a finding:\n%s", r.stderr)
			}
		})
	}
}

func TestPrePushScanRefusesABulkPushWithAPointer(t *testing.T) {
	fx := newScanFx(t)
	log := fx.useFakeGitleaks()
	for _, c := range []string{"P", "Q", "R"} {
		fx.checkpointRef("01"+strings.Repeat(c, 24), "{\"text\": \""+strings.Repeat(c, 600)+"\"}\n")
	}
	r := fx.pushEnv([]string{"LIGHTHOUSE_SCAN_VOLUME_LIMIT=1000"},
		"ckpt", "refs/entire/checkpoints/*:refs/entire/checkpoints/*")
	wantBlocked(t, r, "checkpoint-scan push ckpt")
	// The refusal is its own message, not a scan that did not complete.
	if !strings.Contains(r.stderr, "pre-push: BLOCKED — 3 checkpoint blobs (") ||
		strings.Contains(r.stderr, "the scan did not complete") {
		t.Fatalf("want the volume refusal, got:\n%s", r.stderr)
	}
	if slices.Contains(firstWords(calls(t, log)), "dir") {
		t.Fatal("gitleaks dir ran on a refused bulk push")
	}
	for _, ref := range fx.remoteRefs(fx.ckpt) {
		if strings.HasPrefix(ref, "refs/entire/") {
			t.Fatalf("%s reached ckpt", ref)
		}
	}
}

// entire pushes its own refs with `git push --no-verify`, past this hook (#194).
func TestPrePushScanRefusesWhileEntirePushesByItself(t *testing.T) {
	fx := newScanFx(t)
	fx.settings(map[string]any{"push_sessions": true}, map[string]any{"checkpoint_push_remote": "ckpt"})
	wantBlocked(t, fx.push("origin", "work"), "push_sessions")
}

// The hook hands the remote's URL to the checkpoint-remote rule. The URL forms themselves are
// TestIsCheckpointRemoteByNameAndURLAndSCP in internal/checkpointscan.
func TestPrePushScanMatchesTheCheckpointRemoteByItsURL(t *testing.T) {
	fx := newScanFx(t)
	fx.settings(map[string]any{"push_sessions": false,
		"checkpoint_remote": map[string]any{"provider": "github", "repo": "Acme/ckpt"}}, nil)
	ref := fx.checkpointRef("01EEEEEEEEEEEEEEEEEEEEEEEE", "{\"text\": \"clean\"}\n")
	line := ref + " " + fx.git(fx.repo, "rev-parse", ref) + " " + ref + " " + zeroSHA + "\n"
	path := fx.noGitleaksPath()
	// With gitleaks absent the scan refuses after the ref check, so the message tells the two
	// rules apart: the ref check passes for the checkpoint repo only.
	wantBlocked(t, fx.runScan("pub", "https://github.com/Acme/widgets.git", line, path), "checkpoint remote")
	wantBlocked(t, fx.runScan("c", "https://github.com/Acme/ckpt.git", line, path), "gitleaks is not installed")
}

func TestPrePushScanWithoutGitleaksRefusesARealRemoteAndAllowsALocalPath(t *testing.T) {
	fx := newScanFx(t)
	line := "refs/heads/work " + fx.git(fx.repo, "rev-parse", "HEAD") + " refs/heads/work " + zeroSHA + "\n"
	path := fx.noGitleaksPath()
	wantBlocked(t, fx.runScan("origin", "https://github.com/Acme/widgets.git", line, path), "gitleaks is not installed")
	wantPassed(t, fx.runScan("origin", fx.origin, line, path))
}

func TestPrePushScanWithoutLighthouseRefuses(t *testing.T) {
	fx := newScanFx(t)
	line := "refs/heads/work " + fx.git(fx.repo, "rev-parse", "HEAD") + " refs/heads/work " + zeroSHA + "\n"
	path := toolDir(t, fx.lighthouse, []string{"git", "bash", "cat", "dirname"}, "lighthouse", nil)
	wantBlocked(t, fx.runScan("origin", fx.origin, line, path), "COULD NOT CHECK — lighthouse is not on PATH")
}

func TestPrePushScanRefDeletionNeedsNoScan(t *testing.T) {
	fx := newScanFx(t)
	line := "(delete) " + zeroSHA + " refs/heads/old 1111111111111111111111111111111111111111\n"
	wantPassed(t, fx.runScan("origin", "https://github.com/Acme/widgets.git", line, fx.noGitleaksPath()))
}

// ciScanCommand is the one `gitleaks git` command of CI's secret-scan step.
func ciScanCommand(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(repoFile(t, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	runs := regexp.MustCompile(`(?m)^\s*run: (gitleaks git .*?)\s*$`).FindAllStringSubmatch(string(data), -1)
	if len(runs) != 1 {
		t.Fatalf("want one `run: gitleaks git` step in ci.yml, got %d", len(runs))
	}
	return runs[0][1]
}

// The command of CI's secret-scan step, run as CI runs it: BASE..HEAD of a PR whose middle
// commit adds a secret that its last commit removes.
func TestSecretScanCIStepIsRedOnASecretInsideThePRRange(t *testing.T) {
	needGitleaks(t)
	fx := newScanEnv(t)
	repo := fx.initRepo("repo")
	config, err := os.ReadFile(repoFile(t, ".gitleaks.toml"))
	if err != nil {
		t.Fatal(err)
	}
	base := fx.commitIn(repo, ".gitleaks.toml", string(config))
	cmd := ciScanCommand(t)
	scan := func(head string) int {
		rc, _, _ := fx.run(repo, "", []string{"BASE=" + base, "HEAD=" + head}, "bash", "-c", cmd)
		return rc
	}
	if rc := scan(fx.commitIn(repo, "a.txt", "nothing to see\n")); rc != 0 {
		t.Fatalf("clean range: rc %d, want 0", rc)
	}
	fx.commitIn(repo, "env.txt", "GITHUB_PAT="+fakePAT+"\n")
	if rc := scan(fx.commitIn(repo, "env.txt", "GITHUB_PAT=\n")); rc != 1 {
		t.Fatalf("range with an added and removed secret: rc %d, want 1", rc)
	}
}

const entirePrePush = `#!/bin/sh
# Entire CLI hooks
if command -v entire >/dev/null 2>&1; then entire hooks git pre-push "$1"; else :; fi
`

func (fx *scanFx) install(repo string) {
	fx.t.Helper()
	if rc, _, errOut := fx.run(repo, "", nil, "bash", repoFile(fx.t, "tools/hooks/install.sh")); rc != 0 {
		fx.t.Fatalf("install.sh: rc %d\n%s", rc, errOut)
	}
}

func readHook(t *testing.T, hook string) string {
	t.Helper()
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("%s is not executable", hook)
	}
	data, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSecretScanInstallPutsTheScanBeforeEntireAndIsIdempotent(t *testing.T) {
	fx := newScanEnv(t)
	repo := fx.initRepo("repo")
	hook := filepath.Join(repo, ".git", "hooks", "pre-push")
	writeExe(t, filepath.Dir(hook), "pre-push", entirePrePush)
	fx.install(repo)
	first := readHook(t, hook)
	scan, entire := strings.Index(first, "pre-push-secret-scan.sh"), strings.Index(first, "entire hooks git pre-push")
	if !strings.HasPrefix(first, "#!/bin/sh\n") || scan < 0 || entire < 0 || scan > entire {
		t.Fatalf("want the scan above entire's line under #!/bin/sh, got:\n%s", first)
	}
	fx.install(repo)
	if again := readHook(t, hook); again != first {
		t.Fatalf("a second install changed the hook:\n%s", again)
	}
}

func TestSecretScanInstallWithoutAHookWritesOne(t *testing.T) {
	fx := newScanEnv(t)
	repo := fx.initRepo("repo")
	fx.install(repo)
	if hook := readHook(t, filepath.Join(repo, ".git", "hooks", "pre-push")); !strings.Contains(hook, "pre-push-secret-scan.sh") {
		t.Fatalf("hook lacks the scan:\n%s", hook)
	}
}

// A checkout without tools/hooks/ (and a main checkout without it) must not push unscanned.
func TestSecretScanInstalledHookRefusesWithoutAScanScript(t *testing.T) {
	fx := newScanEnv(t)
	fx.repo = fx.initRepo("repo")
	origin := fx.bare("origin.git")
	fx.git(fx.repo, "remote", "add", "origin", origin)
	fx.install(fx.repo)
	wantBlocked(t, fx.push("origin", "work"), "refusing to push unscanned")
	if refs := fx.remoteRefs(origin); len(refs) != 0 {
		t.Fatalf("refs reached origin: %v", refs)
	}
}

func TestSecretScanGitleaksConfigAllowsTheBetaHeaderName(t *testing.T) {
	needGitleaks(t)
	dir := t.TempDir()
	line := "{\"anthropic-beta\":\"oauth-2025-04-20\",\"interleaved-thinking-2025-05-14\"}\n"
	clean, dirty := filepath.Join(dir, "t.jsonl"), filepath.Join(dir, "u.jsonl")
	writeAt(t, clean, line)
	// A real key on the line of the allowed match: that line stays red.
	writeAt(t, dirty, strings.Replace(line, "}", ",\"token\":\""+fakePAT+"\"}", 1))
	scan := func(path string) int {
		cmd := exec.Command("gitleaks", "dir", path, "--config", repoFile(t, ".gitleaks.toml"), "--no-banner", "--exit-code", "1")
		_ = cmd.Run()
		return cmd.ProcessState.ExitCode()
	}
	if rc := scan(clean); rc != 0 {
		t.Fatalf("beta header: rc %d, want 0", rc)
	}
	if rc := scan(dirty); rc != 1 {
		t.Fatalf("beta header with a PAT: rc %d, want 1", rc)
	}
}

// allowed says whether an allowlist of .gitleaks.toml drops this match of rule. Go's regexp
// is RE2, the engine gitleaks itself runs these regexes with.
func allowed(t *testing.T, rule, match string) bool {
	t.Helper()
	var config struct {
		Allowlists []struct {
			TargetRules []string `toml:"targetRules"`
			Regexes     []string `toml:"regexes"`
		} `toml:"allowlists"`
	}
	if _, err := toml.DecodeFile(repoFile(t, ".gitleaks.toml"), &config); err != nil {
		t.Fatal(err)
	}
	for _, allow := range config.Allowlists {
		if len(allow.TargetRules) > 0 && !slices.Contains(allow.TargetRules, rule) {
			continue
		}
		for _, re := range allow.Regexes {
			if regexp.MustCompile(re).MatchString(match) {
				return true
			}
		}
	}
	return false
}

const allowUUID = "e51f5dd8-a2ee-42c9-83aa-d9e5f0412345"

// forgeKey has the shape of a kenn-forge runtime_session_key: 16 hex, underscore, 16 hex.
// Joined, so gitleaks does not read this file as a key with a value.
var forgeKey = strings.Join([]string{"0f1e2d3c4b5a6978", "8796a5b4c3d2e1f0"}, "_")

// prose comes from a session brief (#228), joined for the same reason.
var prose = strings.Join([]string{"credentials", strings.Join([]string{"OnFailure", "notification"}, "/")}, ", ")

// beta is the whole header; the cut-off forms are slices of it, since written out in this file
// gitleaks itself would flag them.
var beta = strings.Join([]string{"oauth-2025-04-20", "\",\"", "interleaved-thinking-2025-05-14", "\""}, "")

// gitleaks cuts a match off at a chunk boundary, at either end.
func TestSecretScanAllowlistTakesCutOffHeaderAndAttributionKey(t *testing.T) {
	for _, match := range []string{
		beta,
		beta[:len(beta)-1],
		beta[:len(beta)-9],
		beta[1 : len(beta)-11],
		"turnAttributionKey\":\"" + allowUUID + "\"",
		"tionKey\":\"" + allowUUID + "\"",
		"ionKey\":\"" + allowUUID[:30],
		"runtime_session_key\":\"" + forgeKey + "\"",
		"session_key\":\"" + forgeKey + "\"",
		"key\":\"" + forgeKey[:20],
		prose + " ",
		prose,
		prose + "`",
		prose + "'",
		prose + "\"",
		prose + "s ",
		prose + "s'",
	} {
		if !allowed(t, "generic-api-key", match) {
			t.Errorf("not allowed: %q", match)
		}
	}
}

func TestSecretScanAllowlistKeepsARealKeyNextToThemRed(t *testing.T) {
	for _, match := range []string{
		"oauth-2025-04-20\",\"" + fakePAT + "\"",
		"apiKey\":\"" + allowUUID + "\"",
		"turnAttributionKey\":\"" + fakePAT + "\"",
		"runtime_session_key\":\"" + fakePAT + "\"",
		"api_key\":\"" + forgeKey + "\"",
		"runtime_session_key\":\"" + forgeKey + "x\"",
		prose[:13] + fakePAT + " ",
		prose + fakePAT[4:] + " ",
	} {
		if allowed(t, "generic-api-key", match) {
			t.Errorf("allowed: %q", match)
		}
	}
}
