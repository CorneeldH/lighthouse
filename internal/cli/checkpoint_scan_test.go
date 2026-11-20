package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/checkpointscan"
)

// runCK runs `lighthouse <args>` in-process and returns exit code, stdout and stderr.
func runCK(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs(args)
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func ckGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// ckRepo is a repository in the cwd with a bare remote `ckpt`, and the git identity and
// isolation a commit needs.
func ckRepo(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
		"LIGHTHOUSE_HOST_TOML": filepath.Join(t.TempDir(), "absent.toml"),
	} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	bare := filepath.Join(root, "ckpt.git")
	for _, d := range []string{repo, bare} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ckGit(t, repo, "init", "-q")
	ckGit(t, bare, "init", "-q", "--bare")
	ckGit(t, repo, "remote", "add", "ckpt", bare)
	t.Chdir(repo)
	return repo
}

// ckCheckpoint commits one transcript on ref and returns the commit.
func ckCheckpoint(t *testing.T, repo, ref, body string) string {
	t.Helper()
	blob := filepath.Join(repo, "blob.tmp")
	if err := os.WriteFile(blob, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	oid := ckGit(t, repo, "hash-object", "-w", blob)
	_ = os.Remove(blob)
	cmd := exec.Command("git", "-C", repo, "mktree")
	cmd.Stdin = strings.NewReader("100644 blob " + oid + "\tfull.jsonl\n")
	tree, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := ckGit(t, repo, "commit-tree", strings.TrimSpace(string(tree)), "-m", "checkpoint")
	ckGit(t, repo, "update-ref", ref, sha)
	return sha
}

var ckPAT = strings.Join([]string{"ghp_", "Zq9XmW2pL7vR4tY8uN3bK6cJ1hF5dG0sA2eQ"}, "")

const ckLeak = "{\"text\": \"GITHUB_PAT=%s\"}\n"

// ckGitleaksConfig is the repository's own `.gitleaks.toml`; it skips without gitleaks,
// unless LIGHTHOUSE_REQUIRE_GITLEAKS is set.
func ckGitleaksConfig(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("gitleaks"); err != nil {
		if os.Getenv("LIGHTHOUSE_REQUIRE_GITLEAKS") != "" {
			t.Fatal("gitleaks is not installed but LIGHTHOUSE_REQUIRE_GITLEAKS is set")
		}
		t.Skip("gitleaks is not installed")
	}
	cfg, err := filepath.Abs(filepath.Join("..", "..", ".gitleaks.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func leakBody() string { return strings.Replace(ckLeak, "%s", ckPAT, 1) }

func TestCheckpointScanIsHiddenFromHelp(t *testing.T) {
	code, out, _ := runCK(t, "--help")
	if code != 0 || strings.Contains(out, "checkpoint-scan") {
		t.Errorf("root --help: code %d, mentions checkpoint-scan: %v", code, strings.Contains(out, "checkpoint-scan"))
	}
	code, out, _ = runCK(t, "checkpoint-scan", "--help")
	if code != 0 || !strings.Contains(out, "run-bounded") || !strings.Contains(out, "entire-remote") {
		t.Errorf("checkpoint-scan --help: code %d, out %q", code, out)
	}
}

func TestEntireRemoteOutputMatchesPython(t *testing.T) {
	// checkpoint_scan.py:798-805: `yes`/`no` for self-push, then `yes`/`no` for the remote.
	for _, tc := range []struct{ name, settings, url, want string }{
		{"self push, ckpt url", `{"strategy_options": {"checkpoint_remote": {"provider": "github", "repo": "Acme/ckpt"}}}`, "git@github.com:Acme/ckpt.git", "yes\nyes\n"},
		{"no self push, other repo", `{"strategy_options": {"push_sessions": false, "checkpoint_remote": {"provider": "github", "repo": "Acme/ckpt"}}}`, "https://github.com/Acme/widgets.git", "no\nno\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top := t.TempDir()
			if err := os.Mkdir(filepath.Join(top, ".entire"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(top, ".entire", "settings.json"), []byte(tc.settings), 0o644); err != nil {
				t.Fatal(err)
			}

			code, out, errOut := runCK(t, "checkpoint-scan", "entire-remote", "--toplevel", top, "c", tc.url)

			if code != 0 || out != tc.want {
				t.Errorf("code %d, stdout %q, want %q (stderr %q)", code, out, tc.want, errOut)
			}
		})
	}
}

func TestRemoteExclusionOutput(t *testing.T) {
	ckRepo(t)

	code, out, _ := runCK(t, "checkpoint-scan", "remote-exclusion", "ckpt")
	if code != 0 || out != "--remotes=ckpt\n" {
		t.Errorf("configured remote: code %d, stdout %q", code, out)
	}
	code, out, _ = runCK(t, "checkpoint-scan", "remote-exclusion", "nowhere")
	if code != 0 || out != "\n" {
		t.Errorf("unknown remote: code %d, stdout %q", code, out)
	}
}

func TestScanSecretFixtureExits1(t *testing.T) {
	cfg := ckGitleaksConfig(t)
	repo := ckRepo(t)
	sha := ckCheckpoint(t, repo, "refs/entire/checkpoints/01/leak", leakBody())

	code, out, errOut := runCK(t, "checkpoint-scan", "scan", "--remote", "ckpt", "--config", cfg, "refs/entire/checkpoints/01/leak="+sha)

	want := "refs/entire/checkpoints/01/leak  full.jsonl  github-pat  REDACTED\n"
	if code != 1 || out != want {
		t.Errorf("code %d, stdout %q, want 1 and %q (stderr %q)", code, out, want, errOut)
	}
	if strings.Contains(out+errOut, ckPAT) {
		t.Error("the secret itself was printed")
	}
}

func TestScanCleanRefExits0(t *testing.T) {
	cfg := ckGitleaksConfig(t)
	repo := ckRepo(t)
	sha := ckCheckpoint(t, repo, "refs/entire/checkpoints/01/ok", "{\"text\": \"hello\"}\n")

	code, out, errOut := runCK(t, "checkpoint-scan", "scan", "--remote", "ckpt", "--config", cfg, "refs/entire/checkpoints/01/ok="+sha)

	if code != 0 || out != "" {
		t.Errorf("code %d, stdout %q (stderr %q)", code, out, errOut)
	}
}

func TestScanFullIgnoresALedgeredSecret(t *testing.T) {
	cfg := ckGitleaksConfig(t)
	repo := ckRepo(t)
	ref := "refs/entire/checkpoints/01/leak"
	sha := ckCheckpoint(t, repo, ref, leakBody())
	ledger, err := checkpointscan.LedgerPath(repo, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpointscan.AddToLedger(ledger, []string{ckGit(t, repo, "rev-parse", sha+":full.jsonl")}); err != nil {
		t.Fatal(err)
	}

	code, out, _ := runCK(t, "checkpoint-scan", "scan", "--remote", "ckpt", "--config", cfg, ref+"="+sha)
	if code != 0 || out != "" {
		t.Errorf("with the ledger: code %d, stdout %q; the ledger must be trusted", code, out)
	}
	code, out, _ = runCK(t, "checkpoint-scan", "scan", "--full", "--remote", "ckpt", "--config", cfg, ref+"="+sha)
	if code != 1 || !strings.Contains(out, "github-pat") {
		t.Errorf("--full: code %d, stdout %q; want 1 with the finding", code, out)
	}
}

func TestScanVolumeLimitRefusesWithExit2(t *testing.T) {
	cfg := ckGitleaksConfig(t)
	repo := ckRepo(t)
	sha := ckCheckpoint(t, repo, "refs/entire/checkpoints/01/ok", "{\"text\": \"hello\"}\n")
	t.Setenv(checkpointscan.VolumeLimitEnv, "1")

	code, _, errOut := runCK(t, "checkpoint-scan", "scan", "--remote", "ckpt", "--config", cfg, "refs/entire/checkpoints/01/ok="+sha)

	if code != 2 || !strings.Contains(errOut, "pre-push: BLOCKED — 1 checkpoint blobs (0 MB) are not yet scanned.") ||
		!strings.Contains(errOut, "run: lighthouse checkpoint-scan push ckpt") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestScanMissingConfigIsCouldNotCheck(t *testing.T) {
	repo := ckRepo(t)
	sha := ckCheckpoint(t, repo, "refs/entire/checkpoints/01/ok", "x\n")
	missing := filepath.Join(repo, "absent.toml")

	code, _, errOut := runCK(t, "checkpoint-scan", "scan", "--remote", "ckpt", "--config", missing, "refs/entire/checkpoints/01/ok="+sha)

	want := "secret-scan: COULD NOT CHECK — cannot find " + missing + "; scans must use the same config as CI\n"
	if code != 2 || errOut != want {
		t.Errorf("code %d, stderr %q, want %q", code, errOut, want)
	}
}

func TestParseRefSHAs(t *testing.T) {
	sha1, sha256 := strings.Repeat("a", 40), strings.Repeat("b", 64)
	got, err := parseRefSHAs([]string{"refs/a=" + sha1, "refs/b=" + sha256, "refs/gone=" + strings.Repeat("0", 40), "refs/gone256=" + strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"refs/a": sha1, "refs/b": sha256}
	if len(got) != 2 || got["refs/a"] != sha1 || got["refs/b"] != sha256 {
		t.Errorf("got %v, want %v (a deletion sends no objects)", got, want)
	}
	for _, bad := range []string{"refs/a=" + sha1[:39], "refs/a=" + strings.ToUpper(sha1), "nosha", "refs/a=" + sha1 + "0"} {
		if _, err := parseRefSHAs([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestUsageErrorsAreCouldNotCheckExit2(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"unknown flag", []string{"scan", "--nope", "--remote", "r", "x=" + sha}},
		{"scan without remote", []string{"scan", "x=" + sha}},
		{"scan without refs", []string{"scan", "--remote", "r"}},
		{"scan with a malformed ref", []string{"scan", "--remote", "r", "nosha"}},
		{"push without remote", []string{"push"}},
		{"push with two remotes", []string{"push", "a", "b"}},
		{"entire-remote without toplevel", []string{"entire-remote", "r", "u"}},
		{"entire-remote one argument", []string{"entire-remote", "--toplevel", ".", "r"}},
		{"remote-exclusion without remote", []string{"remote-exclusion"}},
		{"no subcommand", nil},
		{"unknown subcommand", []string{"bogus"}},
		{"a command after -- where none is taken", []string{"remote-exclusion", "r", "--", "echo"}},
		{"run-bounded arguments before --", []string{"run-bounded", "x", "--", "echo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCK(t, append([]string{"checkpoint-scan"}, tc.args...)...)

			if code != 2 || out != "" || !strings.Contains(errOut, "secret-scan: COULD NOT CHECK — ") {
				t.Errorf("code %d, stdout %q, stderr %q; want 2 and a COULD NOT CHECK line", code, out, errOut)
			}
		})
	}
}

func TestRunBoundedNeedsCommandAfterDashes(t *testing.T) {
	for _, args := range [][]string{{"run-bounded"}, {"run-bounded", "--"}} {
		code, _, errOut := runCK(t, append([]string{"checkpoint-scan"}, args...)...)

		if code != 2 || errOut != "secret-scan: COULD NOT CHECK — run-bounded needs a command after --\n" {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
}

func TestRunBoundedLeavesTheCommandAlone(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "absent.toml"))

	code, out, _ := runCK(t, "checkpoint-scan", "run-bounded", "--", "sh", "-c", `echo "$@"; exit 1`, "sh", "--remote", "-x")

	if code != 1 || out != "--remote -x\n" {
		t.Errorf("code %d, stdout %q; flags after -- belong to the command, its rc 1 passes through", code, out)
	}
}

func TestRunBoundedOtherExitIsCouldNotCheck(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "absent.toml"))

	code, _, errOut := runCK(t, "checkpoint-scan", "run-bounded", "--", "sh", "-c", "exit 3")

	if code != 2 || !strings.Contains(errOut, "secret-scan: COULD NOT CHECK — sh exited 3\n") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestUnknownErrorIsCouldNotCheckExit2(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		fn         func(out, errOut io.Writer) (int, error)
	}{
		{"plain error", "secret-scan: COULD NOT CHECK — boom\n", func(io.Writer, io.Writer) (int, error) { return 0, errors.New("boom") }},
		{"panic", "secret-scan: COULD NOT CHECK — panic: kaboom\n", func(io.Writer, io.Writer) (int, error) { panic("kaboom") }},
		{"could not check", "secret-scan: COULD NOT CHECK — no git\n", func(io.Writer, io.Writer) (int, error) {
			return 1, checkpointscan.CouldNotCheck{Msg: "no git"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			root := &cobra.Command{Use: "x", SilenceUsage: true, SilenceErrors: true}
			root.AddCommand(&cobra.Command{Use: "go", RunE: func(cmd *cobra.Command, _ []string) error {
				return ckRun(cmd, tc.fn)
			}})
			root.SetOut(&out)
			root.SetErr(&errOut)
			root.SetArgs([]string{"go"})

			code := execute(root, &errOut)

			if code != 2 || errOut.String() != tc.want {
				t.Errorf("code %d, stderr %q, want 2 and %q", code, errOut.String(), tc.want)
			}
		})
	}
}

func TestCkRunPassesTheExitCodeThrough(t *testing.T) {
	for _, want := range []int{0, 1, 2} {
		root := &cobra.Command{Use: "x", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(&cobra.Command{Use: "go", RunE: func(cmd *cobra.Command, _ []string) error {
			return ckRun(cmd, func(io.Writer, io.Writer) (int, error) { return want, nil })
		}})
		root.SetArgs([]string{"go"})
		var errOut bytes.Buffer

		if got := execute(root, &errOut); got != want || errOut.Len() != 0 {
			t.Errorf("want %d, got %d, stderr %q", want, got, errOut.String())
		}
	}
}

func TestScanAbortedBatchIsExit2NotFindings(t *testing.T) {
	ckGitleaksConfig(t) // skips without gitleaks
	repo := ckRepo(t)
	ref := "refs/entire/checkpoints/01/leak"
	sha := ckCheckpoint(t, repo, ref, leakBody())
	broken := filepath.Join(t.TempDir(), "broken.toml")
	if err := os.WriteFile(broken, []byte("[[[ not toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runCK(t, "checkpoint-scan", "scan", "--remote", "ckpt", "--config", broken, ref+"="+sha)

	if code != 2 || out != "" || !strings.Contains(errOut, "secret-scan: COULD NOT CHECK — ") {
		t.Errorf("code %d, stdout %q, stderr %q; a batch that could not be scanned is 2", code, out, errOut)
	}
}
