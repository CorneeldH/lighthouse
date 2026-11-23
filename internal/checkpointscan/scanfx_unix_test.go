//go:build unix

package checkpointscan

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git blob ids are SHA-1
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// fakeGitleaksScript stands in for gitleaks. `version` prints $FAKE_GITLEAKS_VERSION (default
// `fake 1`). `dir` logs the blob files it got, reports FAKESECRET in a file as a finding and
// says how many bytes it read. Knobs:
//
//	FAKE_GITLEAKS_KILL_AT=<n>    SIGKILL itself on the n-th call
//	FAKE_GITLEAKS_RC=<rc>        exit with this rc instead
//	FAKE_GITLEAKS_SCANNED=none   say nothing about the bytes; short: one byte too few
//	FAKE_GITLEAKS_REPORT=none    write no report; garbage: write a broken one; null: a JSON null
//	FAKE_GITLEAKS_HOOK=<sh>      run this shell command on every call (a ref moves mid-scan)
//	FAKE_GITLEAKS_MEMBER=<path>  name the finding as an archive member: `<file>!<path>`
const fakeGitleaksScript = `#!/bin/sh
if [ "$1" = version ]; then echo "${FAKE_GITLEAKS_VERSION:-fake 1}"; exit 0; fi
n=$(cat "$FAKE_GITLEAKS_LOG.calls" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$FAKE_GITLEAKS_LOG.calls"
[ "$FAKE_GITLEAKS_KILL_AT" = "$n" ] && kill -9 $$
echo "$@" > "$FAKE_GITLEAKS_LOG.args"
[ -n "$FAKE_GITLEAKS_HOOK" ] && sh -c "$FAKE_GITLEAKS_HOOK"
report=; cfg=; prev=
for a; do
  [ "$prev" = -r ] && report=$a
  [ "$prev" = --config ] && cfg=$a
  prev=$a
done
# gitleaks runs in the blob dir: a config path relative to the repo would be lost.
[ -f "$cfg" ] || { echo "no config $cfg" >&2; exit 9; }
scanned=0; findings=
for f in *; do
  echo "$f" >> "$FAKE_GITLEAKS_LOG"
  sz=$(wc -c < "$f"); scanned=$((scanned+sz))
  if grep -q FAKESECRET "$f"; then
    where=$f; [ -n "$FAKE_GITLEAKS_MEMBER" ] && where="$f!$FAKE_GITLEAKS_MEMBER"
    findings="$findings${findings:+,}{\"RuleID\":\"fake-rule\",\"File\":\"$where\",\"Match\":\"REDACTED\"}"
  fi
done
case "$FAKE_GITLEAKS_SCANNED" in
  none) ;;
  short) echo "INF scanned ~$((scanned-1)) bytes" >&2 ;;
  *) echo "INF scanned ~$scanned bytes" >&2 ;;
esac
case "${FAKE_GITLEAKS_REPORT:-write}" in
  write) echo "[$findings]" > "$report" ;;
  garbage) echo "{not json" > "$report" ;;
  null) echo "null" > "$report" ;;
esac
[ -n "$FAKE_GITLEAKS_RC" ] && exit "$FAKE_GITLEAKS_RC"
[ -n "$findings" ] && exit 1
exit 0
`

// fakeSecret is what the fake gitleaks reports; the real-gitleaks tests build theirs from
// split literals so this file carries no match for the repository's own scan.
const fakeSecret = "FAKESECRET"

var fakePAT = strings.Join([]string{"ghp_", "Zq9XmW2pL7vR4tY8uN3bK6cJ1hF5dG0sA2eQ"}, "")

type fixture struct {
	t      *testing.T
	repo   string
	config string
	log    string
	stderr bytes.Buffer
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
}

func gitOut(t *testing.T, repo string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = gitEnv()
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return out.String()
}

// newFixture is a repo with a bare remote `ckpt`, the stubs for the bounding and (with fake)
// the fake gitleaks first on PATH, and an own gitleaks config.
func newFixture(t *testing.T, fake bool) *fixture {
	t.Helper()
	root := realTempDir(t)
	f := &fixture{t: t, repo: filepath.Join(root, "repo"), config: filepath.Join(root, "gitleaks.toml"), log: filepath.Join(root, "gitleaks.log")}
	for _, d := range []string{f.repo, filepath.Join(root, "ckpt.git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitOut(t, f.repo, "", "init", "-q")
	gitOut(t, root, "", "init", "-q", "--bare", filepath.Join(root, "ckpt.git"))
	gitOut(t, f.repo, "", "remote", "add", "ckpt", filepath.Join(root, "ckpt.git"))
	if err := os.WriteFile(f.config, []byte("title = \"test\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubs := boundedStubs()
	if fake {
		stubs["gitleaks"] = fakeGitleaksScript
		t.Setenv("FAKE_GITLEAKS_LOG", f.log)
	}
	stubPath(t, stubs)
	return f
}

// checkpoints makes one root commit per ref holding the files, through one fast-import.
func (f *fixture) checkpoints(trees map[string]map[string]string) map[string]string {
	f.t.Helper()
	refs := make([]string, 0, len(trees))
	for ref := range trees {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	var stream strings.Builder
	for mark, ref := range refs {
		fmt.Fprintf(&stream, "reset %s\ncommit %s\nmark :%d\ncommitter t <t@example.com> 0 +0000\ndata 10\ncheckpoint\n", ref, ref, mark+1)
		paths := make([]string, 0)
		for p := range trees[ref] {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(&stream, "M 100644 inline %s\ndata %d\n%s\n", p, len(trees[ref][p]), trees[ref][p])
		}
		stream.WriteString("\n")
	}
	marks := filepath.Join(f.repo, ".git", "fast-import.marks")
	gitOut(f.t, f.repo, stream.String(), "fast-import", "--quiet", "--export-marks="+marks)
	raw, err := os.ReadFile(marks)
	if err != nil {
		f.t.Fatal(err)
	}
	byMark := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.Split(line, " ")
		byMark[parts[0]] = parts[1]
	}
	out := map[string]string{}
	for mark, ref := range refs {
		out[ref] = byMark[fmt.Sprintf(":%d", mark+1)]
	}
	return out
}

// single is one checkpoint ref with one transcript.
func (f *fixture) single(ref, body string) map[string]string {
	return f.checkpoints(map[string]map[string]string{ref: {"0/full.jsonl": body}})
}

func (f *fixture) missing(refs map[string]string) map[string]Blob {
	f.t.Helper()
	blobs, err := MissingBlobs(f.repo, refs, "ckpt")
	noErr(f.t, err)
	return blobs
}

func (f *fixture) opts() ScanOptions {
	return ScanOptions{
		Ledger: filepath.Join(f.repo, ".git", "ledger.txt"), Config: f.config,
		Limits: Limits{MemoryMax: "2G", CPUQuota: "200%"}, Stderr: &f.stderr,
	}
}

func (f *fixture) scan(refs map[string]string, opts ScanOptions) ScanResult {
	f.t.Helper()
	res, err := ScanBlobs(f.repo, f.missing(refs), opts)
	noErr(f.t, err)
	return res
}

// logged is the files gitleaks got, sorted.
func (f *fixture) logged() []string {
	raw, err := os.ReadFile(f.log)
	if err != nil {
		return nil
	}
	out := strings.Fields(string(raw))
	sort.Strings(out)
	return out
}

func (f *fixture) calls() int {
	raw, err := os.ReadFile(f.log + ".calls")
	if err != nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscan(string(raw), &n)
	return n
}

func (f *fixture) ledger(opts ScanOptions) []string {
	set, err := LoadLedger(opts.Ledger)
	noErr(f.t, err)
	out := make([]string, 0, len(set))
	for oid := range set {
		out = append(out, oid)
	}
	sort.Strings(out)
	return out
}

func blobID(body string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(body), body)))) //nolint:gosec
}

func sorted(ids ...string) []string {
	sort.Strings(ids)
	return ids
}

var peakRSSLine = regexp.MustCompile(`secret-scan: batch \d/3 peak RSS (\d+|unknown) MB`)
