package commitgate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const postTailNoRows = "'Could not test' is not a green light.\n" +
	"\n" +
	"A commit of this call may be untested. Do not repeat the commit to retry:\n" +
	"that gives 'nothing to commit' and tests nothing. Check 'git log', then run\n" +
	"[test_gate] command by hand on the files of that commit.\n"

func payload(command string) []byte {
	b, _ := json.Marshal(map[string]any{"session_id": "s1", "tool_use_id": "t1", "tool_input": map[string]any{"command": command}})
	return b
}

// Besluit 6: a lock that fails with another error than "held" blocks with that error.
func TestSlotErrorBlocks(t *testing.T) {
	h := newHarness(t, "pre", testGate, "")
	var closed []*os.File
	h.g.acquire = func(files []*os.File, _, _ time.Duration) (int, error) {
		closed = files
		return 0, errors.New("lock: not supported")
	}
	res := h.g.RunRoots(h.rootRow(), "PreToolUse")
	blocked(t, res, "commit-test-gate: COULD NOT CHECK — cannot take a gate slot: lock: not supported\n"+
		"\n"+
		"Without a slot this run would go unchecked beside the other gates.\n"+
		"Not being able to test is not a green light.\n")
	if len(h.calls) != 0 {
		t.Fatalf("the command ran %d times", len(h.calls))
	}
	if len(closed) != 2 {
		t.Fatalf("acquire got %d files, want 2", len(closed))
	}
	for _, f := range closed {
		if err := f.Close(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("slot file %s left open: %v", f.Name(), err)
		}
	}
}

func TestPreEarlyExits(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, command string
		code          int
		stderr        string
	}{
		{"empty command", "", 0, ""},
		{"no commit", "git status", 0, ""},
		{"open quote without a commit", "echo 'x", 0, ""},
		{"open quote with a commit", "git commit -m 'x", 2,
			"commit-test-gate: COULD NOT CHECK — the command cannot be parsed (open quote). Split the command.\n\n" + preTail},
		{"unresolved path", `git -C "$NOWHERE_XYZ" commit -m x`, 2,
			"commit-test-gate: COULD NOT CHECK — the path '$NOWHERE_XYZ' cannot be resolved. Spell the path out.\n\n" + preTail},
		{"new directory", "cd " + dir + "/new && git commit -m x", 2,
			"commit-test-gate: COULD NOT CHECK — '" + dir + "/new' does not exist yet. Commit in a separate call, once the directory exists.\n\n" + preTail},
		{"not a repository", "cd " + dir + " && git commit -m x", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := Pre(context.Background(), &Gate{Mode: "pre"}, payload(tc.command), dir)
			if res.Code != tc.code || res.Output != nil || res.Stderr != tc.stderr {
				t.Fatalf("got %d %v\n%s\nwant %d:\n%s", res.Code, res.Output, res.Stderr, tc.code, tc.stderr)
			}
		})
	}
}

// A marker that cannot be read blocks only when the text holds `git` and `commit`.
func TestPostUnreadableMarker(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("LIGHTHOUSE_TOML", filepath.Join(t.TempDir(), "absent.toml"))
	for _, tc := range []struct {
		command string
		code    int
		stderr  string
	}{
		{"ls", 0, ""},
		{"git commit -m x", 2, "commit-test-gate: COULD NOT CHECK — could not read the recorded HEAD: commit_base: marker is not a JSON object\n\n" + postTailNoRows},
	} {
		raw := payload(tc.command)
		marker, err := markerPath(raw, true)
		if err != nil {
			t.Fatal(err)
		}
		write(t, marker, "not json")
		res := Post(context.Background(), &Gate{Mode: "post"}, raw)
		if res.Code != tc.code || res.Output != nil || res.Stderr != tc.stderr {
			t.Fatalf("%s: got %d %v\n%s", tc.command, res.Code, res.Output, res.Stderr)
		}
	}
}

// The rows of post are the moved roots, answering the event of the payload.
func TestPostRunsTheMovedRoots(t *testing.T) {
	for _, tc := range []struct{ event, want string }{
		{"", "PostToolUse"},
		{"PostToolUse", "PostToolUse"},
		{"PostToolUseFailure", "PostToolUseFailure"},
	} {
		t.Run(tc.want+"/"+tc.event, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			root, base, head := gitRepo(t)
			h := newHarness(t, "post", testGate, "").reply(runReply{0, "3 passed\n", nil})
			raw := []byte(`{"session_id":"s1","tool_use_id":"t1","hook_event_name":"` + tc.event + `","tool_input":{"command":"git commit -m x"}}`)
			marker, err := markerPath(raw, true)
			if err != nil {
				t.Fatal(err)
			}
			write(t, marker, `{"heads": {"`+root+`": "`+base+`"}}`)
			m := message(t, Post(context.Background(), h.g, raw), tc.want)
			if len(h.calls) != 1 || h.calls[0].opts.Dir != root || envValue(h.calls[0].opts.Env, "LIGHTHOUSE_GATE_BASE") != base ||
				envValue(h.calls[0].opts.Env, "LIGHTHOUSE_GATE_HEAD") != head {
				t.Fatalf("calls %+v", h.calls)
			}
			if want := plainNotes + "commit-test-gate: " + root + " — 3 passed"; m != want {
				t.Fatalf("message:\n%s\nwant:\n%s", m, want)
			}
		})
	}
}

// With on = "pr" the commit moment does nothing, also on the default branch: no test, no record.
func TestPreOnPRSkipsDefaultBranchCommit(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"pr\"\n")
	res := h.pre("git commit -m x", repo)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got %d %v\n%s, want a silent pass", res.Code, res.Output, res.Stderr)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the test command ran in %q", h.ranIn())
	}
}

// With on = "pr" a commit on a feature branch records nothing for Post.
func TestPreOnPRRecordsNothing(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"pr\"\n")
	gitIn(t, repo, "checkout", "-q", "-b", "issue-1-x")
	res := h.pre("git commit -m x", repo)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got %d %v\n%s, want a silent pass", res.Code, res.Output, res.Stderr)
	}
	if _, ok := markerHeads(t); ok {
		t.Fatal("a marker was recorded with on = pr")
	}
}

// --no-verify is refused whatever `on` says.
func TestPreOnPRStillBlocksNoVerify(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"pr\"\n")
	res := h.pre("git commit --no-verify -m x", repo)
	if res.Code != 2 || !strings.Contains(res.Stderr, "--no-verify is not allowed in this repo.") {
		t.Fatalf("got %d\n%s, want the --no-verify block", res.Code, res.Stderr)
	}
}

// on = "both" and the default (commit) keep the commit moment as it was.
func TestPreOnBothTestsCommit(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"both\"\n")
	h.reply(runReply{0, "3 passed\n", nil})
	res := h.pre("git commit -m x", repo)
	if got := message(t, res, "PreToolUse"); got != passLine(repo) {
		t.Fatalf("message:\n%s\nwant:\n%s", got, passLine(repo))
	}
	if got := h.ranIn(); len(got) != 1 || got[0] != repo {
		t.Fatalf("the test command ran in %q, want only %q", got, repo)
	}
}

func TestPreDefaultOnTestsCommit(t *testing.T) {
	h, repo := projectHarness(t, testGate)
	h.reply(runReply{0, "3 passed\n", nil})
	res := h.pre("git commit -m x", repo)
	if got := message(t, res, "PreToolUse"); got != passLine(repo) {
		t.Fatalf("message:\n%s\nwant:\n%s", got, passLine(repo))
	}
	if got := h.ranIn(); len(got) != 1 || got[0] != repo {
		t.Fatalf("the test command ran in %q, want only %q", got, repo)
	}
}

// An unreadable `on` blocks a commit instead of guessing a moment.
func TestPreBlocksOnAnInvalidOn(t *testing.T) {
	h, repo := projectHarness(t, testGate+"on = \"always\"\n")
	res := h.pre("git commit -m x", repo)
	if res.Code != 2 || !strings.Contains(res.Stderr, "test_gate.on = \"always\"; it must be commit, pr or both.") {
		t.Fatalf("got %d\n%s, want the invalid-on block", res.Code, res.Stderr)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the test command ran in %q", h.ranIn())
	}
}

// With on = "pr" Post runs nothing for a moved root.
func TestPostOnPRRunsNothing(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	root, base, _ := gitRepo(t)
	h := newHarness(t, "post", testGate+"on = \"pr\"\n", "")
	raw := payload("git commit -m x")
	marker, err := markerPath(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	write(t, marker, `{"heads": {"`+root+`": "`+base+`"}}`)
	res := Post(context.Background(), h.g, raw)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got %d %v\n%s, want a silent pass", res.Code, res.Output, res.Stderr)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the test command ran %d times", len(h.calls))
	}
}

// Without a moved root Post reads no configuration: an invalid `on` is not seen.
func TestPostWithoutAMovedRootReadsNoOn(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	h := newHarness(t, "post", testGate+"on = \"always\"\n", "")
	res := Post(context.Background(), h.g, payload("git commit -m x"))
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got %d\n%s, want a silent pass", res.Code, res.Stderr)
	}
}

// writeBlocking gives root a hooks/lighthouse/blocking with the given lines.
func writeBlocking(t *testing.T, root string, lines string) {
	t.Helper()
	d := filepath.Join(root, ".claude", "hooks", "lighthouse")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "blocking"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Red when Pre judges --no-verify in a repo whose pack lists no-verify-check: that base hook
// refuses it, so the test gate must not refuse twice (the pass is a commit on a non-repo path).
func TestPreLeavesNoVerifyToNoVerifyCheck(t *testing.T) {
	dir := t.TempDir()
	writeBlocking(t, dir, "main-commit-gate\ncommit-test-gate\nno-verify-check\n")
	res := Pre(context.Background(), &Gate{Mode: "pre", Root: dir}, payload("cd "+dir+" && git commit --no-verify -m x"), dir)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("got %d %q, want a pass", res.Code, res.Stderr)
	}
}

// Red when Pre stops refusing --no-verify in a repo on an older pack (no no-verify-check in
// blocking, or no readable blocking file): a newer binary must not open that gap.
func TestPreBlocksNoVerifyOnAnOlderPack(t *testing.T) {
	const want = "[commit-test-gate/no-verify] commit-test-gate: --no-verify is not allowed in this repo.\n" +
		"\n" +
		"The test gate is a Claude Code PreToolUse hook, not a git hook, so the\n" +
		"flag skips nothing today — but it will as soon as git hooks are added.\n" +
		"If something fails: fix it. If it really has to be skipped, ask.\n"
	for name, lines := range map[string]string{
		"old blocking": "main-commit-gate\ncommit-test-gate\npr-create-gate\n",
		"no blocking":  "",
		"similar name": "no-verify-check-x\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if name != "no blocking" {
				writeBlocking(t, dir, lines)
			}
			res := Pre(context.Background(), &Gate{Mode: "pre", Root: dir}, payload("cd "+dir+" && git commit --no-verify -m x"), dir)
			if res.Code != 2 || res.Stderr != want {
				t.Fatalf("got %d %q, want a block", res.Code, res.Stderr)
			}
		})
	}
}
