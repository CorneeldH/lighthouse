package pusher

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/hostconfig"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

const prURL = "https://github.com/o/r/pull/12"

// fakeMerger records the merges it is asked for and the token of the client that asks. It answers
// with errs in turn, then with err.
type fakeMerger struct {
	mu     sync.Mutex
	tokens []string
	calls  []string // "<pr>@<sha>"
	errs   []error
	err    error
}

func (m *fakeMerger) MergePR(_ context.Context, n int, sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, strconv.Itoa(n)+"@"+sha)
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		return err
	}
	return m.err
}

// mergeFix is a runFix whose push row pushed and closed, as on the build route, with a fake
// merger for the workflow-token client.
type mergeFix struct {
	*runFix
	merger *fakeMerger
}

func newMergeFix(t *testing.T) *mergeFix {
	t.Helper()
	m := &mergeFix{runFix: newRunFix(t, hostconfig.ReviewEnforce, nil), merger: &fakeMerger{}}
	m.deps.Merger = func(token string) Merger {
		m.merger.mu.Lock()
		defer m.merger.mu.Unlock()
		m.merger.tokens = append(m.merger.tokens, token)
		return m.merger
	}
	return m
}

// recordPush puts sha on origin's branch and records it on the open push row, which closes as a
// handoff: what the pusher leaves behind on the build route.
func (m *mergeFix) recordPush(sha string) {
	m.t.Helper()
	m.originAt(sha)
	ctx := context.Background()
	if err := runs.SetPushed(ctx, m.db, m.rowID, sha); err != nil {
		m.t.Fatal(err)
	}
	if err := runs.Release(ctx, m.db, m.rowID, runs.Handoff, "", m.t0.Add(4*time.Minute)); err != nil {
		m.t.Fatal(err)
	}
}

// originAt moves origin's branch to sha, as a run's own push does.
func (m *mergeFix) originAt(sha string) {
	m.t.Helper()
	tgit(m.t, m.wt, "push", "-q", "-f", m.remote, sha+":refs/heads/"+branch)
}

// startMerge sets PR #12 open on head and claims the merge row for it.
func (m *mergeFix) startMerge(head string) {
	m.t.Helper()
	m.forge.prs = []forge.PR{{Number: 12, State: "OPEN", HeadRef: branch, HeadSHA: head, URL: prURL}}
	m.claim("handoff", MergeKey(12, head), time.Now())
	m.req.Worktree = ""
}

func (m *mergeFix) merge(head string) {
	m.t.Helper()
	if err := Merge(context.Background(), m.deps, m.req, 12, head); err != nil {
		m.t.Fatalf("Merge: %v", err)
	}
}

// refused checks that the merge pusher asked instead of merging: a Vraag without a session and
// without a SHA to permit, that names the step and contains want, and a row closed as question.
func (m *mergeFix) refused(step, want string) string {
	m.t.Helper()
	if len(m.merger.calls) != 0 || len(m.merger.tokens) != 0 {
		m.t.Errorf("merged %v (tokens read %d), want no merge", m.merger.calls, len(m.merger.tokens))
	}
	body := m.forge.last(m.t)
	h, ok := protocol.ParseHeader(body)
	if !ok || h.Kind != protocol.KindQuestion || h.Session != "" || h.Fields["sha"] != noSHA ||
		h.Fields["branch"] != branch || h.Fields["pr"] != "12" || h.Fields["step"] != step {
		m.t.Errorf("Vraag header %+v, want sha none, branch, pr 12, step %s:\n%s", h, step, body)
	}
	if !strings.Contains(body, want) {
		m.t.Errorf("Vraag lacks %q:\n%s", want, body)
	}
	if strings.Contains(body, "git push") || strings.Contains(strings.ToLower(body), "scope") {
		m.t.Errorf("a merge Vraag offers a push or names a token scope:\n%s", body)
	}
	wantClosed(m.t, m.row(), runs.Question)
	return body
}

// A changelog fragment after the push (the write-pr run's commit on the build route) leaves the
// workflows as pushed: the PR merges with the workflow token, on exactly its head, and the row
// closes done with a Done that names the PR.
func TestMergeAllowsChangelogFragmentAfterPush(t *testing.T) {
	m := newMergeFix(t)
	pushed := m.commitWorkflow("ci.yml", preMergeWF)
	m.recordPush(pushed)
	head := m.commit("fragments/396.md", "- fragment\n")
	m.originAt(head)
	m.startMerge(head)
	m.merge(head)
	if want := []string{"12@" + head}; strings.Join(m.merger.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("merges = %v, want %v", m.merger.calls, want)
	}
	if len(m.merger.tokens) != 1 || m.merger.tokens[0] != workflowTok {
		t.Errorf("merge client tokens = %q, want the workflow token once", m.merger.tokens)
	}
	body := m.forge.last(t)
	if h, ok := protocol.ParseHeader(body); !ok || h.Kind != protocol.KindDone || h.Session != "" || h.Fields["pr"] != prURL {
		t.Errorf("posted %q, want a Done for %s without a session", body, prURL)
	}
	wantClosed(t, m.row(), runs.Done)
}

// A workflow change after the pushed SHA was never reviewed: no merge, a Vraag that names it.
func TestMergeRefusesWorkflowChangeAfterPush(t *testing.T) {
	for name, change := range map[string]func(m *mergeFix) string{
		"changed": func(m *mergeFix) string { return m.commit(".github/workflows/ci.yml", preMergeWF+"# more\n") },
		"added":   func(m *mergeFix) string { return m.commit(".github/workflows/new.yml", preMergeWF) },
		"deleted": func(m *mergeFix) string {
			tgit(m.t, m.wt, "rm", "-q", ".github/workflows/ci.yml")
			tgit(m.t, m.wt, "commit", "-q", "-m", "rm")
			return m.head()
		},
		"renamed away": func(m *mergeFix) string {
			tgit(m.t, m.wt, "mv", ".github/workflows/ci.yml", "ci.yml")
			tgit(m.t, m.wt, "commit", "-q", "-m", "mv")
			return m.head()
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMergeFix(t)
			pushed := m.commitWorkflow("ci.yml", preMergeWF)
			m.recordPush(pushed)
			head := change(m)
			m.originAt(head)
			m.startMerge(head)
			m.merge(head)
			m.refused(StepAncestry, ".github/workflows/")
		})
	}
}

// The pushed SHA must be an ancestor of the head: a rewritten branch drops the reviewed commit,
// and a SHA the clone does not know is no proof either.
func TestMergeRefusesNonAncestor(t *testing.T) {
	t.Run("rewritten branch", func(t *testing.T) {
		m := newMergeFix(t)
		pushed := m.commitWorkflow("ci.yml", preMergeWF)
		m.recordPush(pushed)
		// The pusher's clone holds the pushed SHA from its push.
		tgit(t, m.dir, "init", "-q", "--bare", m.req.CloneDir)
		tgit(t, m.req.CloneDir, "fetch", "-q", m.wt, pushed+":refs/heads/pushed")
		tgit(t, m.wt, "reset", "-q", "--hard", "HEAD~1")
		head := m.commit("other.txt", "other\n")
		m.originAt(head)
		m.startMerge(head)
		m.merge(head)
		m.refused(StepAncestry, "not an ancestor")
	})
	t.Run("unknown SHA", func(t *testing.T) {
		m := newMergeFix(t)
		pushed := m.commitWorkflow("ci.yml", preMergeWF)
		m.recordPush(pushed)
		tgit(t, m.wt, "reset", "-q", "--hard", "HEAD~1")
		head := m.commit("other.txt", "other\n")
		m.originAt(head)
		m.startMerge(head)
		m.merge(head)
		m.refused(StepAncestry, pushed)
	})
}

// The pushed SHA comes from the pusher's own push rows of this branch, never from a comment: the
// newest one counts, a push of another branch does not, and without any the merge is refused.
func TestMergeTakesNewestPushOfTheBranch(t *testing.T) {
	t.Run("a second workflow push is the one that counts", func(t *testing.T) {
		m := newMergeFix(t)
		first := m.commitWorkflow("ci.yml", preMergeWF)
		m.recordPush(first)
		m.claim("handoff", "push:99", m.t0.Add(4*time.Minute+time.Second))
		second := m.commitWorkflow("ci.yml", preMergeWF+"# second\n")
		m.recordPush(second)
		head := m.commit("fragments/396.md", "- fragment\n")
		m.originAt(head)
		m.startMerge(head)
		m.merge(head)
		if len(m.merger.calls) != 1 {
			t.Fatalf("merges = %v, want one: the newest push covers the second workflow change", m.merger.calls)
		}
	})
	t.Run("a stale push of the branch refuses a later workflow change", func(t *testing.T) {
		m := newMergeFix(t)
		first := m.commitWorkflow("ci.yml", preMergeWF)
		m.recordPush(first)
		head := m.commit(".github/workflows/ci.yml", preMergeWF+"# by hand\n")
		m.originAt(head)
		// A forged note of a later push changes nothing: the merge never reads comments for it.
		m.forge.add(protocol.RenderPushNote(branch, first+".."+head, ".github/workflows/ci.yml", "nl"), m.t0.Add(4*time.Minute+time.Second))
		m.startMerge(head)
		m.merge(head)
		m.refused(StepAncestry, ".github/workflows/ci.yml")
	})
	t.Run("a push of another branch does not count", func(t *testing.T) {
		m := newMergeFix(t)
		pushed := m.commitWorkflow("ci.yml", preMergeWF)
		m.originAt(pushed)
		ctx := context.Background()
		if _, err := m.db.ExecContext(ctx, `UPDATE runs SET branch = 'issue-1-other' WHERE id = ?`, m.rowID); err != nil {
			t.Fatal(err)
		}
		if err := runs.SetPushed(ctx, m.db, m.rowID, pushed); err != nil {
			t.Fatal(err)
		}
		if err := runs.Release(ctx, m.db, m.rowID, runs.Handoff, "", m.t0.Add(4*time.Minute)); err != nil {
			t.Fatal(err)
		}
		m.startMerge(pushed)
		m.merge(pushed)
		m.refused(StepAncestry, "no push")
	})
}

// The head is the one decide measured: a PR or origin branch that moved, a PR of a fork, a closed
// PR or a merge row of another head is refused before the token is read.
func TestMergeRefusesAMovedOrForeignHead(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m *mergeFix, head string)
		step   string
		want   string
	}{
		{"PR head moved", func(m *mergeFix, _ string) { m.forge.prs[0].HeadSHA = strings.Repeat("d", 40) }, StepRoute, "moved"},
		{"origin moved", func(m *mergeFix, _ string) { m.originAt(m.commit("late.txt", "late\n")) }, StepRoute, "moved"},
		{"fork", func(m *mergeFix, _ string) { m.forge.prs[0].CrossRepo = true }, StepRoute, "#12"},
		{"closed", func(m *mergeFix, _ string) { m.forge.prs[0].State = "CLOSED" }, StepRoute, "#12"},
		{"row of another head", func(*mergeFix, string) {}, StepTrigger, "merge:12:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMergeFix(t)
			pushed := m.commitWorkflow("ci.yml", preMergeWF)
			m.recordPush(pushed)
			head := m.commit("fragments/396.md", "- fragment\n")
			m.originAt(head)
			m.startMerge(head)
			tt.mutate(m, head)
			arg := head
			if tt.name == "row of another head" {
				arg = pushed
			}
			if err := Merge(context.Background(), m.deps, m.req, 12, arg); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			m.refused(tt.step, tt.want)
		})
	}
}

// When GitHub refuses the merge despite the checks, the Vraag says so neutrally: GitHub's own
// advice about the token is dropped, the token never appears, and the row closes as question.
func TestMergeRefusedByForgeAsksNeutrally(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		want  string
		tries int
	}{
		{"workflow refusal", &forge.MergeRefused{Message: "refusing to allow a Personal Access Token to create or update workflow `.github/workflows/ci.yml` without `workflow` scope"},
			protocol.Heading("question_push_token_refused", "nl"), 1},
		{"other refusal", &forge.MergeRefused{Message: "Required status check \"ci\" is expected. token " + workflowTok},
			"Required status check", 1},
		// The PR on the forge is still on head: one more try, then the Vraag.
		{"head moved", forge.ErrHeadMoved, "moved", 2},
		{"transport", errors.New("dial tcp: i/o timeout"), "i/o timeout", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMergeFix(t)
			m.merger.err = tt.err
			pushed := m.commitWorkflow("ci.yml", preMergeWF)
			m.recordPush(pushed)
			head := m.commit("fragments/396.md", "- fragment\n")
			m.originAt(head)
			m.startMerge(head)
			m.merge(head)
			if len(m.merger.calls) != tt.tries {
				t.Fatalf("merges = %v, want %d attempts", m.merger.calls, tt.tries)
			}
			body := m.forge.last(t)
			h, _ := protocol.ParseHeader(body)
			if h.Kind != protocol.KindQuestion || h.Fields["step"] != StepMerge || h.Fields["sha"] != noSHA {
				t.Errorf("posted %q, want a merge Vraag without a SHA", body)
			}
			for _, bad := range []string{workflowTok, "refusing to allow", "scope", "git push"} {
				if strings.Contains(strings.ToLower(body), strings.ToLower(bad)) {
					t.Errorf("Vraag contains %q:\n%s", bad, body)
				}
			}
			if !strings.Contains(body, tt.want) {
				t.Errorf("Vraag lacks %q:\n%s", tt.want, body)
			}
			wantClosed(t, m.row(), runs.Question)
		})
	}
}

// Without [dispatch.workflow_push], or on a forge the pusher does not support, the merge pusher
// asks with the reason and reads no token.
func TestMergeRouteOffAsks(t *testing.T) {
	for name, off := range map[string]func(m *mergeFix){
		"no config": func(m *mergeFix) { m.req.Config = nil },
		"off":       func(m *mergeFix) { m.req.Off = "the workflow pusher supports only github.com" },
	} {
		t.Run(name, func(t *testing.T) {
			m := newMergeFix(t)
			pushed := m.commitWorkflow("ci.yml", preMergeWF)
			m.recordPush(pushed)
			m.startMerge(pushed)
			off(m)
			m.merge(pushed)
			m.refused(StepConfig, "")
		})
	}
}

// The workflow token goes only to the merge client: no git command of the merge pusher sees it,
// in argv or env, the forge token never travels in argv, and the process environment lacks it.
func TestMergeTokenOnlyToTheMergeClient(t *testing.T) {
	m := newMergeFix(t)
	pushed := m.commitWorkflow("ci.yml", preMergeWF)
	m.recordPush(pushed)
	head := m.commit("fragments/396.md", "- fragment\n")
	m.originAt(head)
	m.startMerge(head)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(m.dir, "git.log")
	write(t, filepath.Join(m.bin, "git"), "#!/bin/sh\n{ printf 'ARGV'; printf ' %s' \"$@\"; printf '\\n'; env | sed 's/^/ENV /'; } >> "+log+"\nexec "+real+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(m.bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_TOKEN", forgeTok)
	m.merge(head)
	if len(m.merger.calls) != 1 {
		t.Fatalf("merges = %v, want one", m.merger.calls)
	}
	b64 := func(tok string) string { return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok)) }
	text, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	pusherGit := regexp.MustCompile(`^ -C \S+ -c core\.hooksPath=/dev/null -c core\.fsmonitor=false `)
	commands := 0
	for _, block := range strings.Split(string(text), "ARGV")[1:] {
		argv, env, _ := strings.Cut(block, "\n")
		if !pusherGit.MatchString(argv) {
			continue
		}
		commands++
		for _, secret := range []string{workflowTok, forgeTok, b64(workflowTok), b64(forgeTok)} {
			if strings.Contains(argv, secret) {
				t.Errorf("a token in argv: %s", argv)
			}
		}
		if strings.Contains(env, workflowTok) || strings.Contains(env, b64(workflowTok)) || strings.Contains(env, "ENV GH_TOKEN") {
			t.Errorf("git%s sees the workflow token or GH_TOKEN in its env", argv)
		}
	}
	if commands < 4 {
		t.Errorf("%d git commands of the merge pusher, want the fetch and the checks", commands)
	}
	if strings.Contains(strings.Join(os.Environ(), "\n"), workflowTok) {
		t.Error("the workflow token is in the pusher's own environment")
	}
	for _, body := range m.forge.posted {
		if strings.Contains(body, workflowTok) {
			t.Errorf("posted text holds the workflow token: %s", body)
		}
	}
}

// A merge row tick hands back while the merge pusher checks (here: when it reads the workflow
// token) is never merged, and gets no Question under tick's Returned. A merge pusher that starts
// after its work deadline does nothing at all.
func TestMergeHandedBackOrLateDoesNotMerge(t *testing.T) {
	setup := func(t *testing.T) (*mergeFix, string) {
		m := newMergeFix(t)
		pushed := m.commitWorkflow("ci.yml", preMergeWF)
		m.recordPush(pushed)
		head := m.commit("fragments/396.md", "- fragment\n")
		m.originAt(head)
		m.startMerge(head)
		return m, head
	}
	t.Run("handed back", func(t *testing.T) {
		m, head := setup(t)
		m.deps.ReadToken = func(path string) (string, error) {
			if err := runs.Release(context.Background(), m.db, m.rowID, runs.Returned, "reason_pusher_silent", time.Now()); err != nil {
				t.Fatal(err)
			}
			return ReadTokenFile(path)
		}
		if err := Merge(context.Background(), m.deps, m.req, 12, head); err == nil || !strings.Contains(err.Error(), "handed back") {
			t.Errorf("Merge = %v, want the handed-back row", err)
		}
		if len(m.merger.calls) != 0 || len(m.forge.posted) != 0 {
			t.Errorf("merges %v, posted %q; want neither", m.merger.calls, m.forge.posted)
		}
	})
	t.Run("late", func(t *testing.T) {
		m, head := setup(t)
		m.lateBy(m.cfg.Timeout)
		if err := Merge(context.Background(), m.deps, m.req, 12, head); err == nil || !strings.Contains(err.Error(), "work deadline") {
			t.Errorf("Merge = %v, want the passed work deadline", err)
		}
		if len(m.merger.calls) != 0 || len(m.forge.posted) != 0 || !m.row().Open {
			t.Errorf("merges %v, posted %q, row open %v; want neither and the row open", m.merger.calls, m.forge.posted, m.row().Open)
		}
	})
}

// A forge that says the head or base moved (GitHub's "Base branch was modified" when another merge
// landed first) while the PR is still on head gets one more try: the second merges. A PR whose head
// really moved gets none.
func TestMergeRetriesOnceWhileStillOnHead(t *testing.T) {
	setup := func(t *testing.T) (*mergeFix, string) {
		m := newMergeFix(t)
		pushed := m.commitWorkflow("ci.yml", preMergeWF)
		m.recordPush(pushed)
		head := m.commit("fragments/396.md", "- fragment\n")
		m.originAt(head)
		m.startMerge(head)
		return m, head
	}
	t.Run("second try merges", func(t *testing.T) {
		m, head := setup(t)
		m.merger.errs = []error{forge.ErrHeadMoved}
		m.merge(head)
		if want := "12@" + head + ",12@" + head; strings.Join(m.merger.calls, ",") != want {
			t.Fatalf("merges = %v, want two on %s", m.merger.calls, head)
		}
		wantClosed(t, m.row(), runs.Done)
	})
	t.Run("head really moved", func(t *testing.T) {
		m, head := setup(t)
		m.merger.errs = []error{forge.ErrHeadMoved}
		m.forge.prs[0].HeadSHA = strings.Repeat("d", 40) // the forge's PR moves after the check
		m.deps.Forge = movingForge{fakeForge: m.forge, head: head}
		m.merge(head)
		if len(m.merger.calls) != 1 {
			t.Fatalf("merges = %v, want one attempt", m.merger.calls)
		}
		wantClosed(t, m.row(), runs.Question)
	})
}

// movingForge shows the PR on head at the first read and on its moved head after that.
type movingForge struct {
	*fakeForge
	head string
}

func (f movingForge) BranchPRs(ctx context.Context, b string) ([]forge.PR, error) {
	prs, err := f.fakeForge.BranchPRs(ctx, b)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads++; f.reads == 1 && len(prs) > 0 {
		first := append([]forge.PR{}, prs...)
		first[0].HeadSHA = f.head
		return first, err
	}
	return prs, err
}
