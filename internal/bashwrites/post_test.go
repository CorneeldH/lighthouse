package bashwrites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/deadline"
	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// The twins of the run_post, dispatch and pool tests in tests/pack/test_bash_writes.py. Where the
// Python replaces run_gate, _now, _claim, diff_commit or run_until, these replace the package
// variable of the same role.

const badName = "bad\xff.sh"

func swap[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func swapGates(t *testing.T, ext string, gates []string) {
	t.Helper()
	old := Gates[ext]
	Gates[ext] = gates
	t.Cleanup(func() { Gates[ext] = old })
}

// pastThisSecond waits for the next whole second: source 2 walks the reflog since the pre start in
// whole seconds, so a setup commit from the same second would count too.
func pastThisSecond() {
	n := time.Now()
	time.Sleep(time.Second - time.Duration(n.Nanosecond()) + 20*time.Millisecond)
}

// gateRoot is a lighthouse root with a stub `exit 0` for every gate.
func gateRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "lighthouse-root")
	if err := os.MkdirAll(filepath.Join(root, hookDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for gate := range GatePacks {
		if err := os.WriteFile(GatePath(root, gate), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type postEnv struct {
	repo, xdg, gates, session, prefix string
}

func newPostEnv(t *testing.T, session, prefix string) *postEnv {
	t.Helper()
	repo := newRepo(t)
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("CLAUDE_PROJECT_DIR", repo)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	t.Setenv("LIGHTHOUSE_BASH_GATES_DEADLINE", "")
	t.Setenv("LIGHTHOUSE_BASH_GATES_START_NS", "")
	pastThisSecond()
	return &postEnv{repo: repo, xdg: xdg, gates: gateRoot(t), session: session, prefix: prefix}
}

func utf8Env(t *testing.T) *postEnv { return newPostEnv(t, "sess-utf8", "toolu_utf8_") }

func (e *postEnv) payload(t *testing.T, n int) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"session_id": e.session, "tool_use_id": e.prefix + string(rune('0'+n)),
		"hook_event_name": "PostToolUse", "cwd": e.repo,
		"tool_input": map[string]any{"command": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (e *postEnv) pre(t *testing.T, n int) {
	t.Helper()
	eq(t, "pre", Pre(e.payload(t, n), time.Now), 0)
}

type postResult struct {
	code   int
	output map[string]any
	stderr string
}

func (r postResult) out(t *testing.T) string {
	t.Helper()
	if r.output == nil {
		return ""
	}
	raw, err := json.Marshal(r.output)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (r postResult) silent() bool { return r.code == 0 && r.output == nil && r.stderr == "" }

func (r postResult) context(t *testing.T) string {
	t.Helper()
	hso, _ := r.output["hookSpecificOutput"].(map[string]any)
	text, ok := hso["additionalContext"].(string)
	if !ok {
		t.Fatalf("no additionalContext in %v", r.output)
	}
	return text
}

func (e *postEnv) post(t *testing.T, n int) postResult {
	t.Helper()
	code, output, stderr := Post(e.payload(t, n), e.gates)
	return postResult{code, output, stderr}
}

func (e *postEnv) write(t *testing.T, name, text string) {
	t.Helper()
	writeFile(t, filepath.Join(e.repo, name), text)
}

func (e *postEnv) git(t *testing.T, args ...string) {
	t.Helper()
	fixturesetup.Git(t, e.repo, args...)
}

// dirtyCalls is pre, then names written dirty: the next post has them as candidates.
func (e *postEnv) dirtyCalls(t *testing.T, names ...string) {
	t.Helper()
	e.pre(t, 0)
	for _, name := range names {
		e.write(t, name, "echo hoi\n")
	}
}

// ledgerRoot is the ledger row of the repo, as post reads it.
func (e *postEnv) ledgerRoot(t *testing.T) *record {
	t.Helper()
	l := readLedger(filepath.Join(e.xdg, "lighthouse-bash-gates", e.session, "ledger.json"))
	r := validRecord(l.Roots[e.repo])
	if r == nil {
		t.Fatalf("no valid ledger row for %s in %v", e.repo, l.Roots)
	}
	return r
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func gateFake(f func(rel, gate string) (string, []byte)) func(string, string, string, string, time.Time) (string, []byte, error) {
	return func(_, _, rel, gate string, _ time.Time) (string, []byte, error) {
		verdict, output := f(rel, gate)
		return verdict, output, nil
	}
}

// lateNow is a clock whose first call is now and every later call far past the deadline: only the
// dispatch misses it, the git reads keep their real time.
func lateNow() func() time.Time {
	start := time.Now()
	calls := 0
	return func() time.Time {
		calls++
		if calls == 1 {
			return start
		}
		return start.Add(1e6 * time.Second)
	}
}

func headSha(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestDeadlineMakesProgressOverManyFiles(t *testing.T) {
	e := newPostEnv(t, "sess-many", "toolu_many_")
	var judged [][2]string
	fired := false
	start := time.Now()
	swap(t, &runGateFunc, gateFake(func(rel, gate string) (string, []byte) {
		judged = append(judged, [2]string{rel, gate})
		fired = true
		return "finding", []byte("SC2034 " + rel)
	}))
	// Before the first gate of this post: time left. After it: far past the deadline. The deadline
	// is checked per file, so each post judges exactly one file, with all of its gates.
	swap(t, &now, func() time.Time {
		if fired {
			return start.Add(1e6 * time.Second)
		}
		return start
	})
	e.pre(t, 0)
	for _, name := range []string{"c.sh", "a.sh", "b.sh"} {
		e.write(t, name, "echo kapot\n")
	}
	var results []postResult
	for n := range 4 {
		fired = false
		if n > 0 {
			e.pre(t, n)
		}
		results = append(results, e.post(t, n))
	}
	eq(t, "judged", judged, [][2]string{
		{"a.sh", "shell-lint-check.sh"}, {"a.sh", "code-language-check.sh"},
		{"b.sh", "shell-lint-check.sh"}, {"b.sh", "code-language-check.sh"},
		{"c.sh", "shell-lint-check.sh"}, {"c.sh", "code-language-check.sh"},
	})
	var codes []int
	for _, r := range results {
		codes = append(codes, r.code)
	}
	eq(t, "codes", codes, []int{2, 2, 2, 0})
	first := results[0].stderr
	if !strings.Contains(first, "COULD NOT CHECK") || !strings.Contains(first, "b.sh") || !strings.Contains(first, "c.sh") {
		t.Fatalf("first stderr = %q", first)
	}
	if !results[3].silent() {
		t.Fatalf("last post = %+v", results[3])
	}
}

func writeBadName(t *testing.T, e *postEnv) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.repo, badName), []byte("echo hoi\n"), 0o644); err != nil {
		t.Skipf("the filesystem refuses a name that is not UTF-8: %v", err) // APFS (EILSEQ)
	}
}

func TestNonUTF8NameIsUncheckedNotDispatched(t *testing.T) {
	e := utf8Env(t)
	var dispatched []string
	swap(t, &runGateFunc, gateFake(func(rel, _ string) (string, []byte) {
		dispatched = append(dispatched, rel)
		return "ok", nil
	}))
	e.pre(t, 0)
	writeBadName(t, e)
	for n := range 2 {
		if n > 0 {
			e.pre(t, n)
		}
		r := e.post(t, n)
		if r.code != 2 || !strings.Contains(r.stderr, "COULD NOT CHECK") || !strings.Contains(r.stderr, `bad\xff.sh`) ||
			!strings.Contains(r.out(t), "COULD NOT CHECK") {
			t.Fatalf("post %d = %+v", n, r)
		}
	}
	eq(t, "dispatched", len(dispatched), 0)
	if _, ok := e.ledgerRoot(t).seen[badName]; ok {
		t.Fatal("the bad name is in seen")
	}
}

func TestNonUTF8NameFromACommitStaysPending(t *testing.T) {
	e := utf8Env(t)
	var dispatched []string
	swap(t, &runGateFunc, gateFake(func(rel, _ string) (string, []byte) {
		dispatched = append(dispatched, rel)
		return "ok", nil
	}))
	e.pre(t, 0)
	writeBadName(t, e)
	e.git(t, "add", "-A")
	e.git(t, "commit", "-qm", "bad name")
	r := e.post(t, 0)
	if r.code != 2 || !strings.Contains(r.stderr, `bad\xff.sh`) {
		t.Fatalf("post = %+v", r)
	}
	eq(t, "dispatched", len(dispatched), 0)
	rec := e.ledgerRoot(t)
	if !contains(rec.pendingStrings(), badName) {
		t.Fatalf("pending = %q", rec.pendingStrings())
	}
	if _, ok := rec.seen[badName]; ok {
		t.Fatal("the bad name is in seen")
	}
}

func TestFailingDiffTreeInABornRepoIsUnchecked(t *testing.T) {
	e := utf8Env(t)
	swap(t, &diffCommit, func(root, _ string, _ bool, _ time.Time) (map[string]bool, map[string]bool, error) {
		return nil, nil, &GitError{Root: root, Code: 128, Msg: "fatal: kapot"}
	})
	e.pre(t, 0)
	e.write(t, "x.sh", "echo hoi\n")
	e.git(t, "add", "-A")
	e.git(t, "commit", "-qm", "x")
	r := e.post(t, 0)
	if r.code != 2 || !strings.Contains(r.stderr, "COULD NOT CHECK: "+e.repo) || !strings.Contains(r.stderr, "fatal: kapot") {
		t.Fatalf("post = %+v", r)
	}
}

func TestMissedDeadlineAdvancesHeadAndKeepsPending(t *testing.T) {
	e := utf8Env(t)
	swap(t, &runGateFunc, gateFake(func(rel, _ string) (string, []byte) {
		if rel != "x.sh" {
			t.Fatalf("gate on %s", rel)
		}
		return "finding", []byte("SC2034 x.sh")
	}))
	e.pre(t, 0)
	e.write(t, "x.sh", "echo kapot\n")
	e.git(t, "add", "x.sh")
	e.git(t, "commit", "-qm", "x")
	head := headSha(t, e.repo)

	swap(t, &now, lateNow())
	r := e.post(t, 0)
	if r.code != 2 || !strings.Contains(r.stderr, "COULD NOT CHECK") || strings.Contains(r.stderr, "SC2034") {
		t.Fatalf("post 0 = %+v", r)
	}
	rec := e.ledgerRoot(t)
	var gotHead string
	_ = json.Unmarshal(rec.raw["head"], &gotHead)
	eq(t, "head", gotHead, head)
	eq(t, "pending", rec.pendingStrings(), []string{"x.sh"})

	now = time.Now
	e.pre(t, 1)
	r = e.post(t, 1)
	if r.code != 2 || !strings.Contains(r.stderr, "SC2034") {
		t.Fatalf("post 1 = %+v", r)
	}
	eq(t, "pending", e.ledgerRoot(t).pendingStrings(), []string{})
	e.pre(t, 2)
	if r := e.post(t, 2); !r.silent() {
		t.Fatalf("post 2 = %+v", r)
	}
}

func testRecord(t *testing.T, text string) *record {
	t.Helper()
	r := validRecord(json.RawMessage(text))
	if r == nil {
		t.Fatalf("invalid record %s", text)
	}
	return r
}

func testPool(startHead, head string, keep, settled []string) pool {
	return pool{view: rootView{root: "/r", seen: map[string]json.RawMessage{}, head: head, startHead: &startHead},
		keep: keep, settled: settled}
}

func headText(t *testing.T, r *record) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(r.raw["head"], &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAdvanceIsACompareAndSwapOnHead(t *testing.T) {
	r := testRecord(t, `{"seen": {}, "head": "H0", "pending": [], "claims": {}}`)
	advance(r, testPool("H0", "H1", nil, nil))
	eq(t, "head", headText(t, r), "H1")
	// A parallel post already moved on to H2: this post (began at H0) loses.
	r.raw["head"] = json.RawMessage(`"H2"`)
	advance(r, testPool("H0", "H1", nil, nil))
	eq(t, "head", headText(t, r), "H2")
}

func TestAdvanceMergesPendingAndKeepsWhatAnotherPostAdded(t *testing.T) {
	r := testRecord(t, `{"seen": {}, "head": "H0", "pending": ["b.sh", "gone.sh"], "claims": {}}`)
	advance(r, testPool("H0", "H1", []string{"a.sh"}, []string{"gone.sh"}))
	eq(t, "pending", r.pendingStrings(), []string{"a.sh", "b.sh"})
}

func TestPendingRenameSourceIsNotUnchecked(t *testing.T) {
	// a.sh gets into pending through a missed deadline, and is then staged renamed: the source is
	// deleted, not COULD NOT CHECK.
	e := utf8Env(t)
	swap(t, &runGateFunc, gateFake(func(string, string) (string, []byte) { return "ok", nil }))
	e.pre(t, 0)
	e.write(t, "a.sh", "echo a\n")
	e.git(t, "add", "a.sh")
	e.git(t, "commit", "-qm", "a")
	swap(t, &now, lateNow())
	if r := e.post(t, 0); r.code != 2 {
		t.Fatalf("post 0 = %+v", r)
	}
	eq(t, "pending", e.ledgerRoot(t).pendingStrings(), []string{"a.sh"})

	now = time.Now
	e.pre(t, 1)
	e.git(t, "mv", "a.sh", "b.sh")
	r := e.post(t, 1)
	if r.code != 0 || strings.Contains(r.stderr, "COULD NOT") || strings.Contains(r.out(t), "COULD NOT") {
		t.Fatalf("post 1 = %+v", r)
	}
	eq(t, "pending", e.ledgerRoot(t).pendingStrings(), []string{})
}

func TestPendingManagedFileLeavesPendingUndispatched(t *testing.T) {
	// a.py gets into pending through a missed deadline; then the manifest names it as a file unit:
	// lighthouse's own, so no gate, and out of pending.
	e := utf8Env(t)
	var dispatched []string
	swap(t, &runGateFunc, gateFake(func(rel, _ string) (string, []byte) {
		dispatched = append(dispatched, rel)
		return "ok", nil
	}))
	e.pre(t, 0)
	e.write(t, "a.py", "x = 1\n")
	e.git(t, "add", "a.py")
	e.git(t, "commit", "-qm", "a")
	swap(t, &now, lateNow())
	if r := e.post(t, 0); r.code != 2 {
		t.Fatalf("post 0 = %+v", r)
	}
	eq(t, "pending", e.ledgerRoot(t).pendingStrings(), []string{"a.py"})

	now = time.Now
	sum := sha256.Sum256([]byte("x = 1\n"))
	writeManifest(t, e.repo, "[[unit]]\nkind = \"file\"\npath = \"a.py\"\nsha256 = \""+hex.EncodeToString(sum[:])+"\"\n")
	e.git(t, "add", ".lighthouse/manifest.toml")
	e.git(t, "commit", "-qm", "manifest")
	e.pre(t, 1)
	if r := e.post(t, 1); r.code != 0 {
		t.Fatalf("post 1 = %+v", r)
	}
	eq(t, "dispatched", len(dispatched), 0)
	eq(t, "pending", e.ledgerRoot(t).pendingStrings(), []string{})
}

func TestVanishedCandidateIsNotDispatchedAndNotOK(t *testing.T) {
	// x.sh is in HEAD and vanishes between pool and gate: the gate would see `[ -f ]` fail and
	// give exit 0. That is no verdict.
	e := utf8Env(t)
	e.write(t, "x.sh", "echo a\n")
	e.git(t, "add", "x.sh")
	e.git(t, "commit", "-qm", "x")
	pastThisSecond()
	e.dirtyCalls(t, "x.sh", "y.sh")
	var dispatched []string
	real := claimFunc
	swap(t, &claimFunc, func(state, key string, pools []pool) ([]claimed, bool, error) {
		got, replaced, err := real(state, key, pools)
		_ = os.Remove(filepath.Join(e.repo, "x.sh"))
		_ = os.Remove(filepath.Join(e.repo, "y.sh"))
		return got, replaced, err
	})
	swap(t, &runGateFunc, gateFake(func(rel, _ string) (string, []byte) {
		dispatched = append(dispatched, rel)
		return "ok", nil
	}))
	r := e.post(t, 0)
	eq(t, "dispatched", len(dispatched), 0)
	if r.code != 2 || !strings.Contains(r.stderr, "COULD NOT CHECK") || !strings.Contains(r.stderr, "x.sh") {
		t.Fatalf("post = %+v", r)
	}
	// y.sh was never in HEAD: deleted, no notice.
	if strings.Contains(r.stderr, "y.sh") {
		t.Fatalf("y.sh reported: %q", r.stderr)
	}
	rec := e.ledgerRoot(t)
	if _, ok := rec.seen["x.sh"]; ok {
		t.Fatal("x.sh is in seen")
	}
	eq(t, "claims", len(rec.claims), 0)
}

func TestSignalDuringAGateStopsTheDispatchLoudly(t *testing.T) {
	e := utf8Env(t)
	e.dirtyCalls(t, "a.sh", "b.sh", "c.sh")
	var started []string
	swap(t, &runCapture, func(_ time.Time, _ []string, o deadline.Options) (int, []byte, error) {
		started = append(started, filepath.Base(o.Dir))
		return 0, nil, &deadline.Interrupted{Signal: syscall.SIGTERM}
	})
	r := e.post(t, 0)
	eq(t, "started", len(started), 1)
	if r.code != 2 || !strings.Contains(r.stderr, "COULD NOT CHECK") {
		t.Fatalf("post = %+v", r)
	}
	for _, name := range []string{"a.sh", "b.sh", "c.sh"} {
		if !strings.Contains(r.stderr, name) {
			t.Fatalf("%s missing from %q", name, r.stderr)
		}
	}
	rec := e.ledgerRoot(t)
	eq(t, "seen", len(rec.seen), 0)
	eq(t, "claims", len(rec.claims), 0)
}

func TestGateExit128PlusNDoesNotStopTheDispatch(t *testing.T) {
	// The gate itself ends with 128 + n (a killed shellcheck under `set -e`); the post received no
	// signal. That is an ordinary non-verdict exit for that one file.
	for _, rc := range []int{129, 130, 143} {
		t.Run(strconv.Itoa(rc), func(t *testing.T) {
			e := utf8Env(t)
			e.dirtyCalls(t, "a.sh", "b.sh", "c.sh")
			var started []string
			swap(t, &runCapture, func(_ time.Time, _ []string, o deadline.Options) (int, []byte, error) {
				var payload struct {
					ToolInput struct {
						FilePath string `json:"file_path"`
					} `json:"tool_input"`
				}
				if err := json.Unmarshal(o.Stdin, &payload); err != nil {
					t.Fatal(err)
				}
				started = append(started, filepath.Base(payload.ToolInput.FilePath))
				return rc, nil, nil
			})
			r := e.post(t, 0)
			eq(t, "started", started, []string{"a.sh", "a.sh", "b.sh", "b.sh", "c.sh", "c.sh"})
			if r.code != 2 || !strings.Contains(r.stderr, "COULD NOT CHECK") || strings.Contains(r.stderr, "signal") {
				t.Fatalf("post = %+v", r)
			}
		})
	}
}

func TestGatePacksNameEveryGate(t *testing.T) {
	eq(t, "GatePacks", GatePacks, map[string]string{
		"shell-lint-check.sh":    "shell",
		"ruff-format-check.sh":   "python",
		"gofmt-check.sh":         "go",
		"md-link-check":          "core",
		"code-language-check.sh": "core",
	})
	all := map[string]string{}
	for _, gates := range Gates {
		for _, g := range gates {
			all[g] = GatePacks[g]
		}
	}
	eq(t, "gates", all, GatePacks)
}

func TestRewritingGateRunsBeforeCodeLanguage(t *testing.T) {
	// code-language-check.sh must read the version after `ruff format`.
	for ext, gates := range Gates {
		for i, g := range gates {
			if g != "code-language-check.sh" {
				continue
			}
			for _, later := range gates[i+1:] {
				if signAfterGate[later] {
					t.Errorf("%s: rewriting gate %s after code-language-check.sh", ext, later)
				}
			}
		}
	}
}

func TestGofmtSignsAfterGate(t *testing.T) {
	if !signAfterGate["gofmt-check.sh"] || !signAfterGate["ruff-format-check.sh"] {
		t.Fatalf("signAfterGate = %v", signAfterGate)
	}
}

func TestRunShGateRunsThroughRunSh(t *testing.T) {
	// md-link-check has no script of its own: `run.sh md-link-check` runs it.
	gates := gateRoot(t)
	writeFile(t, filepath.Join(gates, hookDir, "run.sh"), "#!/bin/sh\necho \"args: $*\"\nexit 2\n")
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "x.md"), "[x](weg.md)\n")
	verdict, output, err := runGate(gates, repo, "x.md", "md-link-check", time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "verdict", verdict, "finding")
	eq(t, "output", string(output), "args: md-link-check\n")
}

func TestRunGateFindsTheGateUnderTheRepoRoot(t *testing.T) {
	gates := gateRoot(t)
	writeFile(t, filepath.Join(gates, hookDir, "shell-lint-check.sh"), "#!/bin/sh\necho stub-ran\nexit 2\n")
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "a.sh"), "echo a\n")
	verdict, output, err := runGate(gates, repo, "a.sh", "shell-lint-check.sh", time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "verdict", verdict, "finding")
	eq(t, "output", string(output), "stub-ran\n")
}

func TestMissingLanguagePackIsReportedNotBlocked(t *testing.T) {
	e := utf8Env(t)
	for _, gate := range []string{"ruff-format-check.sh", "shell-lint-check.sh"} {
		if err := os.Remove(GatePath(e.gates, gate)); err != nil {
			t.Fatal(err)
		}
	}
	verdict, output, err := runGate(e.gates, e.repo, "a.py", "ruff-format-check.sh", time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "verdict", verdict, "missing")
	eq(t, "output", string(output), "no python check installed")
	e.dirtyCalls(t, "a.py", "b.sh")
	r := e.post(t, 0)
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("post = %+v", r)
	}
	context := r.context(t)
	for _, want := range []string{"a.py not checked — no python check installed", "b.sh not checked — no shell check installed"} {
		if !strings.Contains(context, want) {
			t.Fatalf("%q missing from %q", want, context)
		}
	}
	// One notice per change: the next call does not report unchanged files again.
	e.pre(t, 1)
	if r := e.post(t, 1); !r.silent() {
		t.Fatalf("post 1 = %+v", r)
	}
}

func TestDispatchRunsEveryGateInOrder(t *testing.T) {
	e := utf8Env(t)
	swapGates(t, ".sh", []string{"first.sh", "second.sh"})
	var ran [][2]string
	swap(t, &runGateFunc, gateFake(func(rel, gate string) (string, []byte) {
		ran = append(ran, [2]string{rel, gate})
		return "finding", []byte(gate + " says no")
	}))
	e.dirtyCalls(t, "x.sh")
	r := e.post(t, 0)
	eq(t, "ran", ran, [][2]string{{"x.sh", "first.sh"}, {"x.sh", "second.sh"}})
	first := strings.Index(r.stderr, "== x.sh (first.sh) ==\nfirst.sh says no")
	second := strings.Index(r.stderr, "== x.sh (second.sh) ==\nsecond.sh says no")
	if r.code != 2 || first < 0 || second < 0 || first > second {
		t.Fatalf("post = %+v", r)
	}
}

func TestDispatchCombinedVerdict(t *testing.T) {
	// x.sh comes from a commit (source 2), so it is in pending until it has a real verdict from
	// every gate.
	cases := []struct {
		verdicts [2]string
		judged   bool
	}{
		{[2]string{"unchecked", "ok"}, false},
		{[2]string{"ok", "unchecked"}, false},
		{[2]string{"finding", "unchecked"}, false},
		{[2]string{"ok", "ok"}, true},
		{[2]string{"ok", "finding"}, true},
		{[2]string{"missing", "ok"}, true},
		{[2]string{"finding", "missing"}, true},
	}
	for _, c := range cases {
		t.Run(c.verdicts[0]+"-"+c.verdicts[1], func(t *testing.T) {
			e := utf8Env(t)
			swapGates(t, ".sh", []string{"first.sh", "second.sh"})
			byGate := map[string]string{"first.sh": c.verdicts[0], "second.sh": c.verdicts[1]}
			swap(t, &runGateFunc, gateFake(func(_, gate string) (string, []byte) { return byGate[gate], nil }))
			e.pre(t, 0)
			e.write(t, "x.sh", "echo x\n")
			e.git(t, "add", "x.sh")
			e.git(t, "commit", "-qm", "x")
			e.post(t, 0)
			rec := e.ledgerRoot(t)
			_, seen := rec.seen["x.sh"]
			if c.judged {
				eq(t, "pending", rec.pendingStrings(), []string{})
				eq(t, "seen", seen, true)
			} else {
				eq(t, "pending", rec.pendingStrings(), []string{"x.sh"})
				eq(t, "seen", seen, false)
			}
			eq(t, "claims", len(rec.claims), 0)
		})
	}
}

func TestDispatchSignatureAfterRewritingGate(t *testing.T) {
	cases := []struct {
		gates       []string
		signedAfter string
	}{
		// The rewriting gate's verdict holds for the version it wrote; a write during the
		// read-only gate after it stays new.
		{[]string{"ruff-format-check.sh", "code-language-check.sh"}, "ruff-format-check.sh"},
		// No rewriting gate: the version from just before the first gate.
		{[]string{"shell-lint-check.sh", "code-language-check.sh"}, ""},
	}
	for _, c := range cases {
		t.Run(c.gates[0], func(t *testing.T) {
			e := utf8Env(t)
			swapGates(t, ".sh", c.gates)
			path := filepath.Join(e.repo, "x.sh")
			sigs := map[string]Signature{}
			swap(t, &runGateFunc, gateFake(func(_, gate string) (string, []byte) {
				// Every gate writes the file (a growing size, so each version has its own
				// signature), as ruff format does, or a concurrent write would.
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.WriteString("# " + gate + "\n")
				_ = f.Close()
				sigs[gate], _ = signatureOf(path)
				return "ok", nil
			}))
			e.dirtyCalls(t, "x.sh")
			before, _ := signatureOf(path)
			if r := e.post(t, 0); !r.silent() {
				t.Fatalf("post = %+v", r)
			}
			var seen []int64
			if err := json.Unmarshal(e.ledgerRoot(t).seen["x.sh"], &seen); err != nil {
				t.Fatal(err)
			}
			want := before
			if c.signedAfter != "" {
				want = sigs[c.signedAfter]
			}
			eq(t, "seen", seen, want[:])
		})
	}
}

func TestRunGateSetsUnderGatesEnv(t *testing.T) {
	t.Setenv(underGatesEnv, "")
	_ = os.Unsetenv(underGatesEnv)
	gates := gateRoot(t)
	writeFile(t, filepath.Join(gates, hookDir, "code-language-check.sh"), "#!/bin/sh\necho \"$LIGHTHOUSE_UNDER_BASH_WRITE_CHECK\"\n")
	repo := newRepo(t)
	writeFile(t, filepath.Join(repo, "a.py"), "x = 1\n")
	verdict, output, err := runGate(gates, repo, "a.py", "code-language-check.sh", time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "verdict", verdict, "ok")
	eq(t, "output", string(output), "1\n")
	if _, ok := os.LookupEnv(underGatesEnv); ok {
		t.Fatal("the env var leaked into this process")
	}
}

// gateOutput makes code-language-check.sh end with rc 0 and output; every other gate is silent.
func gateOutput(t *testing.T, output string) {
	t.Helper()
	swap(t, &runGateFunc, gateFake(func(_, gate string) (string, []byte) {
		if gate == "code-language-check.sh" {
			return "ok", []byte(output)
		}
		return "ok", nil
	}))
}

func TestNoticeOnRC0IsPassedOn(t *testing.T) {
	e := utf8Env(t)
	gateOutput(t, "code-language-check: NOTICE \xe2\x80\x94 x\na.py:2: name: y")
	e.dirtyCalls(t, "x.py")
	r := e.post(t, 0)
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("post = %+v", r)
	}
	message, _ := r.output["systemMessage"].(string)
	want := "== x.py (code-language-check.sh) ==\ncode-language-check: NOTICE — x\na.py:2: name: y"
	for _, text := range []string{r.context(t), message} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q missing from %q", want, text)
		}
	}
}

func TestNoticeBesideABlockAlsoReachesTheUser(t *testing.T) {
	e := utf8Env(t)
	verdicts := map[string][2]string{
		"a.py": {"ok", "code-language-check: NOTICE \xe2\x80\x94 x"},
		"b.sh": {"finding", "SC2034 b.sh"},
	}
	swap(t, &runGateFunc, gateFake(func(rel, gate string) (string, []byte) {
		if gate == "code-language-check.sh" {
			return verdicts[rel][0], []byte(verdicts[rel][1])
		}
		return "ok", nil
	}))
	e.dirtyCalls(t, "a.py", "b.sh")
	r := e.post(t, 0)
	message, _ := r.output["systemMessage"].(string)
	if r.code != 2 || !strings.Contains(message, "NOTICE — x") || strings.Contains(message, "SC2034") ||
		!strings.Contains(r.stderr, "SC2034") || !strings.Contains(r.stderr, "NOTICE — x") {
		t.Fatalf("post = %+v", r)
	}
}

func TestPlainNoteHasNoSystemMessage(t *testing.T) {
	e := utf8Env(t)
	swap(t, &runGateFunc, gateFake(func(_, gate string) (string, []byte) {
		if gate == "ruff-format-check.sh" {
			return verdictMissing, []byte("no python check installed")
		}
		return "ok", nil
	}))
	e.dirtyCalls(t, "a.py")
	r := e.post(t, 0)
	if r.code != 0 || !strings.Contains(r.context(t), "a.py not checked — no python check installed") {
		t.Fatalf("post = %+v", r)
	}
	if _, ok := r.output["systemMessage"]; ok {
		t.Fatalf("systemMessage in %v", r.output)
	}
}

func TestChatterOnRC0StaysSilent(t *testing.T) {
	e := utf8Env(t)
	gateOutput(t, "formatted 1 file")
	e.dirtyCalls(t, "x.py")
	if r := e.post(t, 0); !r.silent() {
		t.Fatalf("post = %+v", r)
	}
}

// --- Go-only: Review-focus 2, 3 and 5, decision 5, and the field-tolerant marker ---------------

func TestPrintable(t *testing.T) {
	cases := map[string]string{
		"bad\xff.sh":    `bad\xff.sh`,
		"café.sh":       "café.sh",
		"a\xe2\x80.sh":  `a\xe2\x80.sh`,
		"plain\n— text": "plain\n— text",
	}
	for in, want := range cases {
		eq(t, "printable("+in+")", printable(in), want)
	}
}

func TestDispatchStopsLoudlyOnASignal(t *testing.T) {
	e := utf8Env(t)
	e.dirtyCalls(t, "a.sh", "b.sh", "c.sh")
	calls := 0
	swap(t, &runGateFunc, gateFake(func(string, string) (string, []byte) {
		calls++
		return verdictStopped, nil
	}))
	r := e.post(t, 0)
	eq(t, "gate calls", calls, 1)
	want := "bash-write-check: COULD NOT CHECK: signal received, not checked:\n" +
		"  " + filepath.Join(e.repo, "a.sh") + "\n  " + filepath.Join(e.repo, "b.sh") + "\n  " + filepath.Join(e.repo, "c.sh")
	if r.code != 2 || r.stderr != want+"\n" {
		t.Fatalf("stderr = %q, want %q", r.stderr, want+"\n")
	}
	rec := e.ledgerRoot(t)
	eq(t, "seen", len(rec.seen), 0)
	eq(t, "claims", len(rec.claims), 0)
}

func TestPostPayloadThatIsNotAnObject(t *testing.T) {
	want := "bash-write-check: COULD NOT CHECK: the hook payload on stdin is not a JSON object"
	for _, raw := range []string{"[1]", "null", "nope", ""} {
		code, output, stderr := Post([]byte(raw), t.TempDir())
		eq(t, "code", code, 2)
		eq(t, "stderr", stderr, want+"\n")
		eq(t, "output", output, map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PostToolUse", "additionalContext": want,
		}})
	}
}

func TestPostPayloadShapes(t *testing.T) {
	// Review-focus 5: a missing tool_input runs on with "" as command; a command that is a number
	// runs on with a tool_use_id, and without one is COULD NOT CHECK (MarkerKey).
	e := utf8Env(t)
	run := func(payload map[string]any) postResult {
		payload["session_id"] = e.session
		payload["cwd"] = e.repo
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		code, output, stderr := Post(raw, e.gates)
		return postResult{code, output, stderr}
	}
	// Without a pre, post notes that source 2 is skipped: exit 0, nothing on stderr.
	if r := run(map[string]any{"tool_use_id": "toolu_shape_1"}); r.code != 0 || r.stderr != "" {
		t.Fatalf("missing tool_input: post = %+v", r)
	}
	if r := run(map[string]any{"tool_use_id": "toolu_shape_2", "tool_input": map[string]any{"command": 5}}); r.code != 0 || r.stderr != "" {
		t.Fatalf("number command with tool_use_id: post = %+v", r)
	}
	r := run(map[string]any{"tool_input": map[string]any{"command": 5}})
	if r.code != 2 || !strings.HasPrefix(r.stderr, "bash-write-check: COULD NOT CHECK: ") {
		t.Fatalf("number command without tool_use_id: post = %+v", r)
	}
}

func TestGatePathFromRepoRoot(t *testing.T) {
	// Twin of test_hook_dir_from_repo_root.
	root := filepath.Join("/x", "root")
	hooks := filepath.Join(root, ".claude", "hooks", "lighthouse")
	eq(t, "shell-lint-check.sh", GatePath(root, "shell-lint-check.sh"), filepath.Join(hooks, "shell-lint-check.sh"))
	eq(t, "code-language-check.sh", GatePath(root, "code-language-check.sh"), filepath.Join(hooks, "code-language-check.sh"))
}

func TestPostWithoutAProjectDirCannotCheck(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	code, _, stderr := Post([]byte(`{"hook_event_name": "PostToolUseFailure"}`), t.TempDir())
	eq(t, "code", code, 2)
	eq(t, "stderr", stderr, "bash-write-check: COULD NOT CHECK: CLAUDE_PROJECT_DIR is missing, the project roots are unknown\n")
}

func TestPostTurnsAPanicIntoCouldNotCheck(t *testing.T) {
	e := utf8Env(t)
	e.dirtyCalls(t, "a.sh")
	swap(t, &runGateFunc, gateFake(func(string, string) (string, []byte) { panic("boom") }))
	r := e.post(t, 0)
	if r.code != 2 || !strings.Contains(r.stderr, "bash-write-check: COULD NOT CHECK: internal error in the dispatcher (panic: boom)") {
		t.Fatalf("post = %+v", r)
	}
	eq(t, "claims", len(e.ledgerRoot(t).claims), 0)
	if _, ok := readMarker(stateOf(t, e.session), e.prefix+"0"); ok {
		t.Fatal("the marker survived the panic")
	}
}

func TestGateThatCannotStartIsAnErrorNotAPanic(t *testing.T) {
	// A gate that exists but has no execute bit: the start error goes up as an error, like
	// Python's `(PermissionError: …)`, not through the panic path.
	e := utf8Env(t)
	gate := GatePath(e.gates, "shell-lint-check.sh")
	if err := os.Chmod(gate, 0o644); err != nil {
		t.Fatal(err)
	}
	e.dirtyCalls(t, "a.sh")
	r := e.post(t, 0)
	eq(t, "code", r.code, 2)
	eq(t, "stderr", r.stderr, "bash-write-check: COULD NOT CHECK: internal error in the dispatcher (fork/exec "+gate+": permission denied)\n")
	eq(t, "claims", len(e.ledgerRoot(t).claims), 0)
}

func TestOutcomeRender(t *testing.T) {
	var o Outcome
	code, output, stderr := o.Render("PostToolUse")
	if code != 0 || output != nil || stderr != "" {
		t.Fatalf("empty outcome = %d %v %q", code, output, stderr)
	}
	o.note("plain", false)
	o.note("loud \xff", true)
	code, output, stderr = o.Render("E")
	eq(t, "code", code, 0)
	eq(t, "stderr", stderr, "")
	eq(t, "output", output, map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": "E", "additionalContext": "plain\n\nloud \\xff"},
		"systemMessage":      "loud \\xff",
	})
	o.cannot("x")
	code, _, stderr = o.Render("E")
	eq(t, "code", code, 2)
	eq(t, "stderr", stderr, "plain\n\nloud \\xff\n\nbash-write-check: COULD NOT CHECK: x\n")
}

func TestPendingNameThatIsNotUTF8RoundTrips(t *testing.T) {
	// Python writes a surrogate-escaped name as \udcff; Go must read and write it the same way.
	eq(t, "encode", string(pyJSONString(badName)), `"bad\udcff.sh"`)
	eq(t, "encode utf8", string(pyJSONString("café.sh")), `"café.sh"`)
	for raw, want := range map[string]string{`"bad\udcff.sh"`: badName, `"caf\u00e9.sh"`: "café.sh", `"a\"b"`: `a"b`} {
		got, ok := pyJSONDecode(json.RawMessage(raw))
		if !ok || got != want {
			t.Fatalf("decode %s = %q %v, want %q", raw, got, ok, want)
		}
	}
	if _, ok := pyJSONDecode(json.RawMessage(`5`)); ok {
		t.Fatal("a number decoded as a name")
	}
}

func TestReadMarkerIsFieldTolerant(t *testing.T) {
	// Python's _marker_roots keeps the string entries of a mixed list; an invalid start_ns is
	// ignored while the roots still count.
	state := newState(t)
	writeFile(t, filepath.Join(state, "mixed.json"), `{"start_ns": "x", "roots": ["/a", 5, "/b"]}`)
	m, ok := readMarker(state, "mixed")
	if !ok {
		t.Fatal("marker not read")
	}
	eq(t, "roots", m.Roots(), []string{"/a", "/b"})
	if m.since() != nil {
		t.Fatalf("since = %v, want nil", *m.since())
	}
	writeFile(t, filepath.Join(state, "noroots.json"), `{"start_ns": 2500000000, "roots": "nope"}`)
	m, _ = readMarker(state, "noroots")
	eq(t, "roots", len(m.Roots()), 0)
	eq(t, "since", *m.since(), int64(2))
	writeFile(t, filepath.Join(state, "neg.json"), `{"start_ns": -1}`)
	m, _ = readMarker(state, "neg")
	eq(t, "since", *m.since(), int64(-1)) // floor division, like Python's //
	writeFile(t, filepath.Join(state, "bool.json"), `{"start_ns": true}`)
	m, _ = readMarker(state, "bool")
	if m.since() != nil {
		t.Fatal("a bool start_ns counts")
	}
	var none marker
	if none.Roots() != nil || none.since() != nil {
		t.Fatal("a missing marker has roots or since")
	}
}
