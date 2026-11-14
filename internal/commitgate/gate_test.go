package commitgate

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/deadline"
	"github.com/CorneeldH/lighthouse/internal/flock"
)

const (
	minimalTOML = "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n"
	hostBase    = "[host]\nname = \"h\"\ntailscale_ip = \"100.64.0.1\"\n"
	// testGate is the usual [test_gate]: a command and no slot wait.
	testGate = "[test_gate]\ncommand = \"uv run pytest -q\"\nslot_wait_seconds = 0\n"
	slice    = "gate_slice = \"x.slice\"\n"
	// The two notes of a host without thresholds and without slice.
	plainNotes = "commit-test-gate: no gate memory threshold in host.toml, host memory not checked\n" +
		"commit-test-gate: no gate_slice in host.toml, tests run without memory brake\n"
	preTail = "This command does contain a 'git commit'. 'Could not test' is not a\n" +
		"green light, so this does not pass silently.\n"
	startEpoch = 1_000_000
)

type runCall struct {
	deadline time.Time
	argv     []string
	opts     deadline.Options
}

type runReply struct {
	code int
	out  string
	err  error
}

// harness is a Gate on a temporary lighthouse.toml, host.toml, slot dir and cgroup root, with
// a fake test run and a fake clock.
type harness struct {
	t        *testing.T
	g        *Gate
	dir      string
	root     string
	toml     string
	slots    string
	cgroup   string
	calls    []runCall
	replies  []runReply
	during   func(n int, c runCall)
	nowCalls int
}

func newHarness(t *testing.T, mode, gate, host string) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{
		t: t, dir: dir,
		root:   filepath.Join(dir, "project"),
		toml:   filepath.Join(dir, "lighthouse.toml"),
		slots:  filepath.Join(dir, "slots"),
		cgroup: filepath.Join(dir, "cgroup"),
	}
	for _, d := range []string{h.root, h.slots} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, h.toml, minimalTOML+"\n"+gate)
	hostPath := filepath.Join(dir, "host.toml")
	write(t, hostPath, hostBase+host)
	t.Setenv("LIGHTHOUSE_TOML", h.toml)
	t.Setenv("LIGHTHOUSE_HOST_TOML", hostPath)
	h.g = &Gate{
		Mode: mode,
		Root: h.root,
		Env: []string{
			"PATH=/usr/bin:/bin",
			"LIGHTHOUSE_GATE_SLOT_DIR=" + h.slots,
			"LIGHTHOUSE_GATE_CGROUP_ROOT=" + h.cgroup,
			"XDG_RUNTIME_DIR=/run/user/test",
			"LIGHTHOUSE_GATE_BASE=outer", "LIGHTHOUSE_GATE_HEAD=outer", "LIGHTHOUSE_GATE_FILES=outer",
		},
		Bash: "/fake/bash",
		Now: func() time.Time {
			h.nowCalls++
			return time.Unix(startEpoch, 0)
		},
		LookPath: lookOnly(),
		Run:      h.run,
	}
	return h
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) run(dl time.Time, argv []string, o deadline.Options) (int, []byte, error) {
	c := runCall{dl, argv, o}
	h.calls = append(h.calls, c)
	n := len(h.calls)
	if h.during != nil {
		h.during(n, c)
	}
	if n > len(h.replies) {
		h.t.Fatalf("run %d without a reply", n)
	}
	r := h.replies[n-1]
	return r.code, []byte(r.out), r.err
}

func (h *harness) reply(r ...runReply) *harness { h.replies = append(h.replies, r...); return h }

// rootRow is a pre row on the harness root.
func (h *harness) rootRow() []Row { return []Row{{Root: h.root}} }

func (h *harness) hint() string {
	return "The command is [test_gate] command in " + h.toml + ": uv run pytest -q. Set command = \"\" there to turn the test gate off.\n"
}

// message is the systemMessage of a pass, after checking the JSON shape.
func message(t *testing.T, res Result, event string) string {
	t.Helper()
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("want a pass, got %d\n%s", res.Code, res.Stderr)
	}
	m, _ := res.Output["systemMessage"].(string)
	spec, _ := res.Output["hookSpecificOutput"].(map[string]any)
	if spec["hookEventName"] != event || spec["additionalContext"] != m {
		t.Fatalf("output = %#v, want event %s and the same text twice", res.Output, event)
	}
	return m
}

func blocked(t *testing.T, res Result, want string) {
	t.Helper()
	if res.Code != 2 || res.Output != nil {
		t.Fatalf("want a block (2, no output), got %d %v\n%s", res.Code, res.Output, res.Stderr)
	}
	if res.Stderr != want {
		t.Fatalf("stderr:\n%s\nwant:\n%s", res.Stderr, want)
	}
}

func TestNoCommandPassesWithANote(t *testing.T) {
	for name, gate := range map[string]string{
		"not set": "",
		"empty":   "[test_gate]\ncommand = \"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "post", gate, "")
			res := h.g.RunRoots(h.rootRow(), "PostToolUseFailure")
			if got := message(t, res, "PostToolUseFailure"); got != "commit-test-gate: no test command configured, nothing was tested" {
				t.Fatalf("message = %q", got)
			}
			if len(h.calls) != 0 || h.nowCalls != 0 {
				t.Fatalf("ran %d, clock %d; want neither", len(h.calls), h.nowCalls)
			}
		})
	}
}

func TestUnreadableConfigBlocks(t *testing.T) {
	h := newHarness(t, "pre", "", "")
	write(t, h.toml, "[test_gate\n")
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the configuration cannot be read (test_gate.command): config: "+h.toml+
		": unreadable or not valid TOML: toml: line 2: expected '.' or ']' to end table name, but got '\\n' instead\n"+
		"\n"+preTail)
}

func TestRangesBlock(t *testing.T) {
	cases := []struct{ name, gate, host, want string }{
		{"deadline 0", "deadline_seconds = 0\n", "", "test_gate.deadline_seconds = 0; it must be between 1 and 280 (hook timeout 300 s)."},
		{"deadline 281", "deadline_seconds = 281\n", "", "test_gate.deadline_seconds = 281; it must be between 1 and 280 (hook timeout 300 s)."},
		{"slots 0", "slots = 0\n", "", "test_gate.slots = 0; it must be between 1 and 7."},
		{"slots 8", "slots = 8\n", "", "test_gate.slots = 8; it must be between 1 and 7."},
		{"host slots 8", "", "gate_slots = 8\n", "host.gate_slots = 8 in host.toml; it must be between 1 and 7."},
		{"negative wait", "slot_wait_seconds = -1\n", "", "test_gate.slot_wait_seconds = -1; it must not be negative."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gate := "[test_gate]\ncommand = \"uv run pytest -q\"\n" + c.gate
			h := newHarness(t, "pre", gate, c.host)
			res := h.g.RunRoots(h.rootRow(), "PreToolUse")
			blocked(t, res, "commit-test-gate: COULD NOT CHECK — "+c.want+"\n\n"+preTail)
			if h.nowCalls != 0 {
				t.Fatal("the clock was read before the ranges were checked")
			}
		})
	}
}

func TestHostCapsTheSlotsWithANote(t *testing.T) {
	h := newHarness(t, "pre", testGate+"slots = 3\n", "gate_slots = 1\n")
	hold(t, h.slots, 1)
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: test_gate.slots = 3 above host gate_slots = 1, the gate uses 1\n"+
		"commit-test-gate: COULD NOT CHECK — all gate slots busy after 0 s (slots: 1)\n"+
		"\n"+
		"Other gates are running already. Try the commit again shortly.\n"+
		"Not being able to test is not a green light.\n")
}

func TestAllSlotsBusyInPostNamesTheCommit(t *testing.T) {
	h := newHarness(t, "post", testGate+"slots = 1\n", "")
	hold(t, h.slots, 1)
	res := h.g.RunRoots([]Row{{Root: "/r/x", Base: "b1", Head: "h1"}}, "PostToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — all gate slots busy after 0 s (slots: 1)\n"+
		"\n"+
		"Other gates are running already. Not being able to test is not a green light.\n"+
		"\n"+
		"The commit is made but not tested. Do not repeat the commit to retry: that\n"+
		"gives 'nothing to commit' and tests nothing. Test it by hand instead:\n"+
		"\n"+
		"  Untested: b1..h1 in /r/x\n"+
		`  cd '/r/x' && f=$(mktemp) && t=$(mktemp -d) && git -c core.quotePath=false diff --name-only --no-renames b1 h1 > "$f" && LIGHTHOUSE_GATE_BASE=b1 LIGHTHOUSE_GATE_HEAD=h1 LIGHTHOUSE_GATE_FILES="$f" LIGHTHOUSE_GATE_TMP="$t" bash -c 'uv run pytest -q'`+"\n")
}

func TestSlotWaitIsClippedToTheDeadline(t *testing.T) {
	h := newHarness(t, "pre", "[test_gate]\ncommand = \"x\"\nslots = 1\nslot_wait_seconds = 50\ndeadline_seconds = 1\n", "")
	hold(t, h.slots, 1)
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, "commit-test-gate: COULD NOT CHECK — all gate slots busy after 1 s (slots: 1)\n") {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
}

func TestLockDirThatCannotBeMadeBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate, "")
	write(t, filepath.Join(h.dir, "file"), "")
	bad := filepath.Join(h.dir, "file", "slots")
	h.g.Env = append(h.g.Env, "LIGHTHOUSE_GATE_SLOT_DIR="+bad)
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — cannot take a gate slot: lock dir "+bad+
		" cannot be created (mkdir "+filepath.Join(h.dir, "file")+": not a directory)\n"+
		"\n"+
		"Without a slot this run would go unchecked beside the other gates.\n"+
		"Not being able to test is not a green light.\n")
}

func TestMemoryBelowThresholdBlocks(t *testing.T) {
	for _, mode := range []string{"pre", "post"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t, mode, testGate, "gate_min_mem_available_gb = 4\n")
			meminfo := filepath.Join(h.dir, "meminfo")
			writeMeminfo(t, meminfo, gibKB, 8*gibKB)
			h.g.Env = append(h.g.Env, "LIGHTHOUSE_GATE_MEMINFO="+meminfo)
			res := h.g.RunRoots([]Row{{Root: "/r/x", Base: "b1", Head: "h1"}}, "PreToolUse")
			head := "commit-test-gate: COULD NOT CHECK — host memory below threshold after 0 s (MemAvailable 1.0 GiB < 4 GiB)\n\n"
			want := head + "The host has too little free memory for a gate run. Try the commit again shortly.\n" +
				"Not being able to test is not a green light.\n"
			if mode == "post" {
				want = head + "The host has too little free memory for a gate run. Not being able to test is not a green light.\n" +
					"\n" +
					"The commit is made but not tested. Do not repeat the commit to retry: that\n" +
					"gives 'nothing to commit' and tests nothing. Test it by hand instead:\n" +
					"\n" +
					"  Untested: b1..h1 in /r/x\n" +
					`  cd '/r/x' && f=$(mktemp) && t=$(mktemp -d) && git -c core.quotePath=false diff --name-only --no-renames b1 h1 > "$f" && LIGHTHOUSE_GATE_BASE=b1 LIGHTHOUSE_GATE_HEAD=h1 LIGHTHOUSE_GATE_FILES="$f" LIGHTHOUSE_GATE_TMP="$t" bash -c 'uv run pytest -q'` + "\n"
			}
			blocked(t, res, want)
		})
	}
}

func TestMemoryNotMeasurableBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate, "gate_min_swap_free_gb = 1\n")
	missing := filepath.Join(h.dir, "nope")
	h.g.Env = append(h.g.Env, "LIGHTHOUSE_GATE_MEMINFO="+missing)
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — host memory not measurable: "+missing+": open "+missing+": no such file or directory\n"+
		"\n"+
		"Not being able to test is not a green light.\n")
}

func TestMemoryNotesAndAGreenRun(t *testing.T) {
	h := newHarness(t, "pre", testGate, "gate_min_swap_free_gb = 1\n").reply(runReply{0, "...\n3 passed in 0.1s\n", nil})
	meminfo := filepath.Join(h.dir, "meminfo")
	write(t, meminfo, "MemAvailable: 9999999 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	h.g.Env = append(h.g.Env, "LIGHTHOUSE_GATE_MEMINFO="+meminfo)
	got := message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	want := "commit-test-gate: no swap on this host, SwapFree not checked\n" +
		"commit-test-gate: no gate_slice in host.toml, tests run without memory brake\n" +
		"commit-test-gate: " + h.root + " — 3 passed in 0.1s"
	if got != want {
		t.Fatalf("message:\n%s\nwant:\n%s", got, want)
	}
}

// The command runs as <bash> -c <command> in the root, under the one deadline, with an empty
// LIGHTHOUSE_GATE_TMP per run and without the BASE, HEAD and FILES of an outer gate.
func TestPreRowRunsTheCommandInTheRoot(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "ok\n", nil}, runReply{0, "ok\n", nil})
	second := filepath.Join(h.dir, "second")
	var tmps []string
	h.during = func(_ int, c runCall) {
		tmp := envValue(c.opts.Env, "LIGHTHOUSE_GATE_TMP")
		entries, err := os.ReadDir(tmp)
		if err != nil || len(entries) != 0 {
			t.Errorf("GATE_TMP %q: %v, %d entries; want an empty dir", tmp, err, len(entries))
		}
		tmps = append(tmps, tmp)
	}
	res := h.g.RunRoots([]Row{{Root: h.root}, {Root: ""}, {Root: second}}, "PreToolUse")
	message(t, res, "PreToolUse")
	if len(h.calls) != 2 {
		t.Fatalf("%d runs, want 2 (the empty root is skipped)", len(h.calls))
	}
	for i, root := range []string{h.root, second} {
		c := h.calls[i]
		if !reflect.DeepEqual(c.argv, []string{"/fake/bash", "-c", "uv run pytest -q"}) || c.opts.Dir != root {
			t.Errorf("run %d: argv %q in %q", i, c.argv, c.opts.Dir)
		}
		for _, key := range []string{"LIGHTHOUSE_GATE_BASE", "LIGHTHOUSE_GATE_HEAD", "LIGHTHOUSE_GATE_FILES"} {
			if v, ok := lookupEnv(c.opts.Env, key); ok {
				t.Errorf("run %d: %s=%q inherited", i, key, v)
			}
		}
		if envValue(c.opts.Env, "PATH") != "/usr/bin:/bin" {
			t.Errorf("run %d: env lost PATH", i)
		}
	}
	if tmps[0] == tmps[1] || filepath.Base(tmps[0]) != "run-1" || filepath.Base(tmps[1]) != "run-2" {
		t.Fatalf("GATE_TMP = %q", tmps)
	}
	if _, err := os.Stat(filepath.Dir(tmps[0])); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate tmp dir left behind: %v", err)
	}
}

func envValue(env []string, key string) string { v, _ := lookupEnv(env, key); return v }

func lookupEnv(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			return v, true
		}
	}
	return "", false
}

func gitRepo(t *testing.T) (root, base, head string) {
	t.Helper()
	root = t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	write(t, filepath.Join(root, "a.txt"), "a")
	git("add", ".")
	git("commit", "-qm", "a")
	base = git("rev-parse", "HEAD")
	write(t, filepath.Join(root, "z.txt"), "z")
	write(t, filepath.Join(root, "b é.txt"), "b")
	git("add", ".")
	git("commit", "-qm", "b")
	return root, base, git("rev-parse", "HEAD")
}

func TestPostRowGetsBaseHeadAndFiles(t *testing.T) {
	root, base, head := gitRepo(t)
	h := newHarness(t, "post", testGate, "").reply(runReply{0, "1 passed\n", nil})
	var files string
	h.during = func(_ int, c runCall) {
		path := envValue(c.opts.Env, "LIGHTHOUSE_GATE_FILES")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Error(err)
		}
		files = string(data)
		if filepath.Dir(path) != filepath.Dir(envValue(c.opts.Env, "LIGHTHOUSE_GATE_TMP")) || filepath.Base(path) != "run-1.files" {
			t.Errorf("FILES %q not beside GATE_TMP", path)
		}
		if envValue(c.opts.Env, "LIGHTHOUSE_GATE_BASE") != base || envValue(c.opts.Env, "LIGHTHOUSE_GATE_HEAD") != head {
			t.Errorf("BASE/HEAD = %q", c.opts.Env)
		}
	}
	res := h.g.RunRoots([]Row{{Root: root, Base: base, Head: head}}, "PostToolUse")
	message(t, res, "PostToolUse")
	if files != "b é.txt\nz.txt\n" {
		t.Fatalf("files = %q", files)
	}
}

func TestRowWithoutHeadBlocks(t *testing.T) {
	h := newHarness(t, "post", testGate, "")
	res := h.g.RunRoots([]Row{{Root: "/r/x", Base: "b1"}}, "PostToolUse")
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, plainNotes+"commit-test-gate: COULD NOT CHECK — the recorded row for /r/x has no new HEAD.\n\n'Could not test' is not a green light.\n") {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
}

func TestFilesThatCannotBeListedBlock(t *testing.T) {
	h := newHarness(t, "pre", testGate, "")
	notRepo := t.TempDir()
	res := h.g.RunRoots([]Row{{Root: notRepo, Base: "b1", Head: "h1"}}, "PreToolUse")
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, plainNotes+"commit-test-gate: COULD NOT CHECK — could not list the files of the commit in "+notRepo+": commit_base: ") ||
		!strings.HasSuffix(res.Stderr, "\n\n"+preTail) {
		t.Fatalf("got %d\n%s", res.Code, res.Stderr)
	}
	if len(h.calls) != 0 {
		t.Fatal("the command ran without its files")
	}
}

// Review-focus 2: trailing blank and space-only lines are not the summary, and the block on
// stderr ends in exactly one newline.
func TestSummaryLine(t *testing.T) {
	cases := []struct{ name, out, want string }{
		{"last line", "a\n3 passed\n  \n\n", "commit-test-gate: ROOT — 3 passed"},
		{"tabs and CR", "3 passed\n\t\r\n", "commit-test-gate: ROOT — 3 passed"},
		{"selectors in order", "gate-go: skipped — no go files\nnoise\ngate: 4 passed\nlast\n",
			"commit-test-gate: ROOT — gate-go: skipped — no go files\ncommit-test-gate: ROOT — gate: 4 passed"},
		{"not a selector", "gate-: x\ngatey: y\nfinal\n", "commit-test-gate: ROOT — final"},
		{"no output", "", "commit-test-gate: ROOT — "},
		{"leading spaces kept", "  3 passed  \n", "commit-test-gate: ROOT —   3 passed  "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, "pre", testGate, "").reply(runReply{0, c.out, nil})
			got := message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
			want := plainNotes + strings.ReplaceAll(c.want, "ROOT", h.root)
			if got != want {
				t.Fatalf("message:\n%q\nwant:\n%q", got, want)
			}
		})
	}
	h := newHarness(t, "pre", testGate, "").reply(runReply{1, "3 passed\n  \n\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: tests failed. Fix the failing tests before committing.\n"+h.hint()+"\n3 passed\n  \n")
}

// Review-focus 1: bytes that are not UTF-8 reach stderr unchanged, and become U+FFFD in the JSON.
func TestOutputBytesPassThrough(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{1, "bad \xff byte\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: tests failed. Fix the failing tests before committing.\n"+h.hint()+"\nbad \xff byte\n")

	h = newHarness(t, "pre", testGate, "").reply(runReply{0, "gate-go: \xff ok\n", nil})
	res = h.g.RunRoots(h.rootRow(), "PreToolUse")
	data, err := json.Marshal(res.Output)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if want := plainNotes + "commit-test-gate: " + h.root + " — gate-go: \uFFFD ok"; back.SystemMessage != want {
		t.Fatalf("JSON message = %q, want %q", back.SystemMessage, want)
	}
}

func TestRedRunInPostSaysFixInANewCommit(t *testing.T) {
	h := newHarness(t, "post", testGate, "").reply(runReply{1, "FAILED x\n\n", nil})
	res := h.g.RunRoots([]Row{{Root: h.root}}, "PostToolUse")
	blocked(t, res, "commit-test-gate: tests failed on the commit you just made. Fix them in a new commit.\n"+h.hint()+"\nFAILED x\n")
}

func TestDeadlineBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{124, "slow\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the test command ran past the deadline of 270 s.\n"+
		"\n"+
		"Root: "+h.root+". An aborted run is not a green light.\n"+
		"\n"+
		"slow\n")
}

func TestNotFoundBlocksWithTheHint(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{127, "bash: uv: command not found\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the test command could not start (command not found?).\n"+
		"\n"+
		"Root: "+h.root+". Not being able to test is not a green light.\n"+
		h.hint()+
		"\n"+
		"bash: uv: command not found\n")
}

func TestUncheckedExitCodeBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate+"unchecked_exit_codes = [3, 5]\n", "").reply(runReply{5, "no tests ran\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the test command stopped with exit code 5, listed in test_gate.unchecked_exit_codes: nothing was tested.\n"+
		"\n"+
		"Root: "+h.root+". Not being able to test is not a green light.\n"+
		h.hint()+
		"\n"+
		"no tests ran\n")

	// 4 is not listed: tests failed.
	h = newHarness(t, "pre", testGate+"unchecked_exit_codes = [3, 5]\n", "").reply(runReply{4, "x\n", nil})
	res = h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: tests failed. Fix the failing tests before committing.\n"+h.hint()+"\nx\n")
}

// brakeHarness has a slice whose brake is available, and memory.events at the given counts.
func brakeHarness(t *testing.T, mode string) *harness {
	h := newHarness(t, mode, testGate, slice)
	h.g.LookPath = lookOnly("systemctl", "systemd-run", "choom")
	h.g.showRun = showReply(t, "", "FragmentPath=/etc/x.slice\nMemoryMax=1073741824\n", nil)
	h.events("oom_kill 1\nhigh 5\n")
	return h
}

func (h *harness) events(text string) {
	write(h.t, filepath.Join(h.cgroup, "x.slice", "memory.events"), text)
}

func TestSignalBlocksWithAKillHint(t *testing.T) {
	cases := []struct {
		name   string
		host   string
		look   func(string) (string, error)
		status int
		hint   string
	}{
		{"no slice", "", lookOnly(), 137, "gate killed by signal 9 (no gate_slice in host.toml)."},
		{"deadline.py style", "", lookOnly(), 247, "gate killed by signal 9 (no gate_slice in host.toml)."},
		{"edge 192", "", lookOnly(), 192, "gate killed by signal 64 (no gate_slice in host.toml)."},
		{"edge 193", "", lookOnly(), 193, "gate killed by signal 63 (no gate_slice in host.toml)."},
		{"brake missing", slice, lookOnly(), 143, "gate killed by signal 15 (memory brake not available: no systemctl)."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, "pre", testGate, c.host).reply(runReply{c.status, "killed\n", nil})
			h.g.LookPath = c.look
			res := h.g.RunRoots(h.rootRow(), "PreToolUse")
			blocked(t, res, "commit-test-gate: COULD NOT CHECK — "+c.hint+"\n"+
				"\n"+
				"Root: "+h.root+". Not being able to test is not a green light.\n"+
				"\n"+
				"killed\n")
		})
	}
	h := brakeHarness(t, "pre").reply(runReply{137, "killed\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — gate killed by signal 9 (systemd-oomd in x.slice?).\n"+
		"\n"+
		"Root: "+h.root+". Not being able to test is not a green light.\n"+
		"\n"+
		"killed\n")
}

func TestBrakeWrapsTheCommandAndNotesNothing(t *testing.T) {
	h := brakeHarness(t, "pre").reply(runReply{0, "ok\n", nil})
	got := message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	want := []string{"systemd-run", "--user", "--scope", "--slice=x.slice", "--quiet", "--", "choom", "-n", "800", "--", "/fake/bash", "-c", "uv run pytest -q"}
	if !reflect.DeepEqual(h.calls[0].argv, want) {
		t.Fatalf("argv = %q", h.calls[0].argv)
	}
	if got != "commit-test-gate: no gate memory threshold in host.toml, host memory not checked\ncommit-test-gate: "+h.root+" — ok" {
		t.Fatalf("message = %q", got)
	}
}

func TestBrakeNotAvailableIsANote(t *testing.T) {
	h := newHarness(t, "pre", testGate, slice).reply(runReply{0, "ok\n", nil})
	got := message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	if got != "commit-test-gate: no gate memory threshold in host.toml, host memory not checked\n"+
		"commit-test-gate: memory brake not available: no systemctl\n"+
		"commit-test-gate: "+h.root+" — ok" {
		t.Fatalf("message = %q", got)
	}
	if !reflect.DeepEqual(h.calls[0].argv, []string{"/fake/bash", "-c", "uv run pytest -q"}) {
		t.Fatalf("argv = %q", h.calls[0].argv)
	}
}

// An OOM kill in the slice wins over every other reading of the exit code, 124 included.
func TestOOMKillBlocks(t *testing.T) {
	for _, status := range []int{1, 124, 137} {
		h := brakeHarness(t, "pre").reply(runReply{status, "Killed\n", nil})
		h.during = func(int, runCall) { h.events("oom_kill 2\nhigh 9\n") }
		res := h.g.RunRoots(h.rootRow(), "PreToolUse")
		blocked(t, res, "commit-test-gate: COULD NOT CHECK — gate throttled (oom-kill in x.slice during this run).\n"+
			"\n"+
			"Root: "+h.root+". Not being able to test is not a green light.\n"+
			"\n"+
			"Killed\n")
	}
}

func TestGreenRunStaysGreenWithAnOOMDelta(t *testing.T) {
	h := brakeHarness(t, "pre").reply(runReply{0, "3 passed\n", nil})
	h.during = func(int, runCall) { h.events("oom_kill 3\n") }
	message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
}

func TestOOMDeltaOnlyCountsWithTheBrake(t *testing.T) {
	h := newHarness(t, "pre", testGate, slice).reply(runReply{1, "x\n", nil})
	h.events("oom_kill 0\n")
	h.during = func(int, runCall) { h.events("oom_kill 3\n") }
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: tests failed. Fix the failing tests before committing.\n"+h.hint()+"\nx\n")
}

func TestDeadlineUnderMemoryPressure(t *testing.T) {
	h := brakeHarness(t, "pre").reply(runReply{124, "slow\n", nil})
	h.during = func(int, runCall) { h.events("oom_kill 1\nhigh 6\n") }
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the test command ran past the deadline of 270 s.\n"+
		"x.slice was under memory pressure\n"+
		"\n"+
		"Root: "+h.root+". An aborted run is not a green light.\n"+
		"\n"+
		"slow\n")
}

func TestSystemdRunThatDidNotStartBlocks(t *testing.T) {
	for _, text := range []string{"Failed to start transient scope unit: x", "Failed to connect to bus: x", "Failed to create bus connection: x"} {
		h := brakeHarness(t, "pre").reply(runReply{1, text + "\n", nil})
		res := h.g.RunRoots(h.rootRow(), "PreToolUse")
		blocked(t, res, "commit-test-gate: COULD NOT CHECK — systemd-run did not start.\n"+
			"\n"+
			"Root: "+h.root+". Not being able to test is not a green light.\n"+
			"\n"+
			text+"\n")
	}
	// Without the brake the same text is just a red run.
	h := newHarness(t, "pre", testGate, "").reply(runReply{1, "Failed to connect to bus: x\n", nil})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: tests failed. Fix the failing tests before committing.\n"+h.hint()+"\nFailed to connect to bus: x\n")
}

func TestXDGRuntimeDirIsSetOnlyForTheChildren(t *testing.T) {
	uidDir := "/run/user/" + strconv.Itoa(os.Getuid())
	if info, err := os.Stat(uidDir); err != nil || !info.IsDir() {
		t.Skip("no " + uidDir)
	}
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "ok\n", nil}, runReply{0, "ok\n", nil})
	h.g.Env = withoutKey(h.g.Env, "XDG_RUNTIME_DIR")
	before := os.Getenv("XDG_RUNTIME_DIR")
	message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	if got := envValue(h.calls[0].opts.Env, "XDG_RUNTIME_DIR"); got != uidDir {
		t.Fatalf("child XDG_RUNTIME_DIR = %q, want %q", got, uidDir)
	}
	if os.Getenv("XDG_RUNTIME_DIR") != before {
		t.Fatal("the process environment changed")
	}

	// With a bus address the hook leaves it alone.
	h.g.Env = append(h.g.Env, "DBUS_SESSION_BUS_ADDRESS=unix:path=/x")
	message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	if v, ok := lookupEnv(h.calls[1].opts.Env, "XDG_RUNTIME_DIR"); ok {
		t.Fatalf("XDG_RUNTIME_DIR = %q set beside a bus address", v)
	}
}

func withoutKey(env []string, key string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// One deadline for the whole loop: the clock is read once, and both runs get the same deadline.
func TestOneDeadlineForAllRoots(t *testing.T) {
	h := newHarness(t, "pre", testGate+"deadline_seconds = 9\n", "").reply(runReply{0, "a\n", nil}, runReply{0, "b\n", nil})
	trace := filepath.Join(h.dir, "trace")
	h.g.Trace = trace
	message(t, h.g.RunRoots([]Row{{Root: h.root}, {Root: h.dir}}, "PreToolUse"), "PreToolUse")
	if h.nowCalls != 1 {
		t.Fatalf("Now called %d times, want 1", h.nowCalls)
	}
	want := time.Unix(startEpoch+9, 0)
	if !h.calls[0].deadline.Equal(want) || !h.calls[1].deadline.Equal(want) {
		t.Fatalf("deadlines %v, %v; want %v twice", h.calls[0].deadline, h.calls[1].deadline, want)
	}
	if data, err := os.ReadFile(trace); err != nil || string(data) != "deadline\n" {
		t.Fatalf("trace = %q, %v; want one deadline line", data, err)
	}
}

func TestStartReplacesTheClock(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "a\n", nil})
	start := int64(42)
	h.g.Start = &start
	message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	if h.nowCalls != 0 || !h.calls[0].deadline.Equal(time.Unix(42+270, 0)) {
		t.Fatalf("clock %d, deadline %v", h.nowCalls, h.calls[0].deadline)
	}
}

// In post, a failing second row names only itself as untested, with the command quoted.
func TestPostNamesOnlyTheRowsNotDone(t *testing.T) {
	gate := "[test_gate]\ncommand = \"echo 'x'\\nexit 3\"\nslot_wait_seconds = 0\n"
	h := newHarness(t, "post", gate, "").reply(runReply{0, "ok\n", nil}, runReply{124, "late\n", nil})
	res := h.g.RunRoots([]Row{{Root: h.root}, {Root: "/r/b"}}, "PostToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — the test command ran past the deadline of 270 s.\n"+
		"\n"+
		"Root: /r/b. An aborted run is not a green light.\n"+
		"\n"+
		"The commit is made but not tested. Do not repeat the commit to retry: that\n"+
		"gives 'nothing to commit' and tests nothing. Test it by hand instead:\n"+
		"\n"+
		"  Untested: .. in /r/b\n"+
		`  cd '/r/b' && f=$(mktemp) && t=$(mktemp -d) && git -c core.quotePath=false diff --name-only --no-renames   > "$f" && LIGHTHOUSE_GATE_BASE= LIGHTHOUSE_GATE_HEAD= LIGHTHOUSE_GATE_FILES="$f" LIGHTHOUSE_GATE_TMP="$t" bash -c 'echo '\''x'\''`+"\n"+`exit 3'`+"\n"+
		"\n"+
		"late\n")
}

// Review-focus 5 (Besluit 9): a signal during the run ends like bash did, silent with 128 + n,
// and the slot is free afterwards.
func TestInterruptedRunEndsLikeBash(t *testing.T) {
	h := newHarness(t, "post", testGate+"slots = 1\n", "").reply(runReply{0, "", &deadline.Interrupted{Signal: syscall.SIGTERM, Output: []byte("half")}})
	res := h.g.RunRoots([]Row{{Root: h.root}}, "PostToolUse")
	if res.Code != 143 || res.Output != nil || res.Stderr != "" {
		t.Fatalf("got %+v, want 143, no output, no stderr", res)
	}
	f, err := flock.TryLock(filepath.Join(h.slots, "slot-1"))
	if err != nil || f == nil {
		t.Fatalf("slot not free after the run: %v, %v", f, err)
	}
	_ = f.Close()
}

func TestRunErrorBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate, "").reply(runReply{0, "", errors.New("fork/exec /fake/bash: permission denied")})
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, plainNotes+"commit-test-gate: COULD NOT CHECK — the test command could not run in "+h.root+": fork/exec /fake/bash: permission denied\n\n"+preTail)
}

// The slot stays taken while the command runs and is free once RunRoots returns.
func TestSlotIsHeldDuringTheRun(t *testing.T) {
	h := newHarness(t, "pre", testGate+"slots = 1\n", "").reply(runReply{0, "ok\n", nil})
	h.during = func(int, runCall) {
		f, err := flock.TryLock(filepath.Join(h.slots, "slot-1"))
		if !errors.Is(err, flock.ErrHeld) {
			t.Errorf("slot free during the run: %v, %v", f, err)
			if f != nil {
				_ = f.Close()
			}
		}
	}
	message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	f, err := flock.TryLock(filepath.Join(h.slots, "slot-1"))
	if err != nil || f == nil {
		t.Fatalf("slot not free after the run: %v, %v", f, err)
	}
	_ = f.Close()
}

// With the real RunCapture: a grandchild that outlives the command does not keep the slot.
func TestSlotDoesNotLeakToTheCommand(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	h := newHarness(t, "pre", "[test_gate]\ncommand = \"sleep 3 >/dev/null 2>&1 &\"\nslots = 1\nslot_wait_seconds = 0\n", "")
	h.g.Bash = bash
	h.g.Run = deadline.RunCapture
	h.g.Now = time.Now
	h.g.Env = append(os.Environ(), "LIGHTHOUSE_GATE_SLOT_DIR="+h.slots)
	message(t, h.g.RunRoots(h.rootRow(), "PreToolUse"), "PreToolUse")
	f, err := flock.TryLock(filepath.Join(h.slots, "slot-1"))
	if err != nil || f == nil {
		t.Fatalf("slot still held by the background child: %v, %v", f, err)
	}
	_ = f.Close()
}
