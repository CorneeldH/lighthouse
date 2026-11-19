package unblock

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/forge/gitlabtest"
)

// world is one forge on its fake server, seeded through neutral words. Each test runs on
// both forges unless it names one.
type world interface {
	forge() forge.Forge
	repo() string
	// addIssue seeds issue n; state is "open" or "closed", reason the GitHub close reason
	// ("" none; GitLab has none at all).
	addIssue(n int, state, reason string, labels ...string)
	// block makes blocker block issue blocked, both in this repo and already seeded.
	block(blocked, blocker int)
	// addBlocking makes issue nr block the open issue m of repo, spelled as given.
	addBlocking(nr int, repo string, m int)
	failIssue(n int)
	failBlocking(n int)
	failBlockedBy(n int)
	failComment(n int)
	// blockersRead reports whether the blockers of issue n were read.
	blockersRead(n int) bool
	// writes are the writes in order: "comment #n" and "unlabel #n".
	writes() []string
}

const label = "blocked-issue"

// ghWorld is GitHub on forgetest, repo Owner/repo.
type ghWorld struct {
	t   *testing.T
	srv *forgetest.Server
	f   forge.Forge
}

func newGitHub(t *testing.T) world {
	srv := forgetest.New(t)
	srv.Issues = map[int]map[string]any{}
	srv.BlockedBy = map[int][]map[string]any{}
	srv.BlockingExtra = map[int][]map[string]any{}
	srv.Fail = map[string]int{}
	srv.FailMessage = map[string]string{}
	f, err := forge.Open(forge.Config{Kind: "github", Repo: "Owner/repo", Token: "t", BaseURL: srv.URL()})
	if err != nil {
		t.Fatal(err)
	}
	return &ghWorld{t: t, srv: srv, f: f}
}

func (w *ghWorld) forge() forge.Forge { return w.f }
func (w *ghWorld) repo() string       { return "Owner/repo" }

func (w *ghWorld) addIssue(n int, state, reason string, labels ...string) {
	var ls []any
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	var r any
	if reason != "" {
		r = reason
	}
	w.srv.Issues[n] = map[string]any{"number": n, "title": fmt.Sprint("Issue ", n), "state": state,
		"state_reason": r, "labels": ls}
}

func (w *ghWorld) block(blocked, blocker int) {
	w.srv.BlockedBy[blocked] = append(w.srv.BlockedBy[blocked], map[string]any{"number": blocker,
		"state": w.srv.Issues[blocker]["state"], "repository_url": w.srv.URL() + "/repos/Owner/repo"})
}

func (w *ghWorld) addBlocking(nr int, repo string, m int) {
	w.srv.BlockingExtra[nr] = append(w.srv.BlockingExtra[nr], map[string]any{"number": m, "state": "open",
		"repository_url": w.srv.URL() + "/repos/" + repo})
}

func (w *ghWorld) fail(key string) {
	w.srv.Fail[key] = 403
	w.srv.FailMessage[key] = "nee"
}

func (w *ghWorld) failIssue(n int) { w.fail(fmt.Sprintf("GET /repos/Owner/repo/issues/%d", n)) }
func (w *ghWorld) failBlocking(n int) {
	w.fail(fmt.Sprintf("GET /repos/Owner/repo/issues/%d/dependencies/blocking", n))
}
func (w *ghWorld) failBlockedBy(n int) {
	w.fail(fmt.Sprintf("GET /repos/Owner/repo/issues/%d/dependencies/blocked_by", n))
}
func (w *ghWorld) failComment(n int) {
	w.fail(fmt.Sprintf("POST /repos/Owner/repo/issues/%d/comments", n))
}

func (w *ghWorld) blockersRead(n int) bool {
	return w.srv.Count(fmt.Sprintf("/repos/Owner/repo/issues/%d/dependencies/blocked_by", n)) > 0
}

func (w *ghWorld) writes() []string {
	var out []string
	for _, p := range w.srv.Posts {
		rest, _ := strings.CutPrefix(p.Path, "/repos/Owner/repo/issues/")
		n, tail, _ := strings.Cut(rest, "/")
		switch {
		case p.Method == "POST" && tail == "comments":
			out = append(out, "comment #"+n)
		case p.Method == "DELETE" && strings.HasPrefix(tail, "labels/"):
			out = append(out, "unlabel #"+n)
		default:
			out = append(out, p.Method+" "+p.Path)
		}
	}
	return out
}

// glWorld is GitLab on gitlabtest, project group/project.
type glWorld struct {
	t      *testing.T
	srv    *gitlabtest.Server
	f      forge.Forge
	states map[int]string
	failed map[int]bool // issues that answer 404
}

func newGitLab(t *testing.T) world {
	srv := gitlabtest.New(t)
	f, err := forge.Open(forge.Config{Kind: "gitlab", Repo: "group/project", Token: "t", BaseURL: srv.URL()})
	if err != nil {
		t.Fatal(err)
	}
	return &glWorld{t: t, srv: srv, f: f, states: map[int]string{}, failed: map[int]bool{}}
}

func (w *glWorld) forge() forge.Forge { return w.f }
func (w *glWorld) repo() string       { return "group/project" }

func (w *glWorld) addIssue(n int, state, _ string, labels ...string) {
	w.states[n] = state
	if w.failed[n] {
		return
	}
	gl := map[string]bool{"open": true}[state]
	st := "closed"
	if gl {
		st = "opened"
	}
	w.srv.AddIssue(map[string]any{"iid": n, "title": fmt.Sprint("Issue ", n), "state": st,
		"labels": append([]string{}, labels...)})
}

func (w *glWorld) block(blocked, blocker int) {
	st := "opened"
	if w.states[blocker] == "closed" {
		st = "closed"
	}
	w.srv.AddLink(blocked, map[string]any{"iid": blocker, "state": st, "link_type": "is_blocked_by",
		"references": map[string]any{"full": fmt.Sprintf("group/project#%d", blocker)}})
}

func (w *glWorld) addBlocking(nr int, repo string, m int) {
	w.srv.AddLink(nr, map[string]any{"iid": m, "state": "opened", "link_type": "blocks",
		"references": map[string]any{"full": fmt.Sprintf("%s#%d", repo, m)}})
}

// failIssue leaves issue n out, so reading it answers 404; call it before addIssue.
func (w *glWorld) failIssue(n int)     { w.failed[n] = true }
func (w *glWorld) failBlocking(n int)  { w.srv.FailLinks(n) }
func (w *glWorld) failBlockedBy(n int) { w.srv.FailLinks(n) }
func (w *glWorld) failComment(n int)   { w.srv.FailNotes(n) }

func (w *glWorld) blockersRead(n int) bool {
	for _, r := range w.srv.Requests() {
		if strings.HasPrefix(r, fmt.Sprintf("GET /api/v4/projects/group%%2Fproject/issues/%d/links", n)) {
			return true
		}
	}
	return false
}

func (w *glWorld) writes() []string {
	var out []string
	for _, wr := range w.srv.Writes() {
		rest, _ := strings.CutPrefix(wr.Path, "/api/v4/projects/group%2Fproject/issues/")
		n, tail, _ := strings.Cut(rest, "/")
		switch {
		case wr.Method == "POST" && tail == "notes":
			out = append(out, "comment #"+n)
		case wr.Method == "PUT" && tail == "" && wr.Body["remove_labels"] != nil:
			out = append(out, "unlabel #"+n)
		default:
			out = append(out, wr.Method+" "+wr.Path)
		}
	}
	return out
}

var forges = []struct {
	name string
	new  func(t *testing.T) world
}{{"github", newGitHub}, {"gitlab", newGitLab}}

// onBoth runs body on GitHub and on GitLab.
func onBoth(t *testing.T, body func(t *testing.T, w world)) {
	for _, fc := range forges {
		t.Run(fc.name, func(t *testing.T) { body(t, fc.new(t)) })
	}
}

func run(t *testing.T, w world, nr int, lbl string, dryRun bool) (int, string) {
	t.Helper()
	code, lines := Run(t.Context(), w.forge(), w.repo(), nr, lbl, dryRun)
	return code, strings.Join(lines, "\n") + "\n"
}

func check(t *testing.T, w world, gotCode int, gotOut string, wantCode int, wantOut string, wantWrites ...string) {
	t.Helper()
	if gotCode != wantCode || gotOut != wantOut {
		t.Errorf("Run = %d,\n%s\nwant %d,\n%s", gotCode, gotOut, wantCode, wantOut)
	}
	if got := w.writes(); !slices.Equal(got, wantWrites) {
		t.Errorf("writes = %q, want %q", got, wantWrites)
	}
}

// comments are the bodies of the comments on issue n.
func comments(t *testing.T, w world, n int) []string {
	t.Helper()
	cs, err := w.forge().Comments(t.Context(), n)
	if err != nil {
		t.Fatalf("Comments(%d): %v", n, err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Body)
	}
	return out
}

func labels(t *testing.T, w world, n int) []string {
	t.Helper()
	is, err := w.forge().Issue(t.Context(), n)
	if err != nil {
		t.Fatalf("Issue(%d): %v", n, err)
	}
	return is.Labels
}

// closedWithDependent seeds the closed issue 1 that blocks the open issue 2 with lbl.
func closedWithDependent(w world, lbl string) {
	w.addIssue(1, "closed", "completed")
	w.addIssue(2, "open", "", "2-area:x", lbl)
	w.block(2, 1)
}

const released = "## Release lane for #1\n\n- #2: released, `blocked-issue` comes off\n"

// The last blocker closed: reply comment on #1 first, then the comment on #2, then the label.
// Ports test_last_blocker_closed_releases_with_comments, test_comment_first_then_label and
// test_release_line_token.
func TestLastBlockerClosedReleasesWithComments(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		closedWithDependent(w, label)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, released, "comment #1", "comment #2", "unlabel #2")
		if got, want := comments(t, w, 1), []string{"Released lane after this close:\n\n- #2 is released: `blocked-issue` comes off"}; !slices.Equal(got, want) {
			t.Errorf("comments #1 = %q, want %q", got, want)
		}
		if got, want := comments(t, w, 2), []string{"Released: the last open blocker #1 is closed; `blocked-issue` comes off."}; !slices.Equal(got, want) {
			t.Errorf("comments #2 = %q, want %q", got, want)
		}
		if got := labels(t, w, 2); !slices.Equal(got, []string{"2-area:x"}) {
			t.Errorf("labels #2 = %q, want [2-area:x]", got)
		}
	})
}

func TestReleaseLineIsWhatCloseIssueReads(t *testing.T) {
	if ReleaseLine != "Released lane after this close:" {
		t.Errorf("ReleaseLine = %q", ReleaseLine)
	}
	skill, err := os.ReadFile("../../pack/core/skills/close-issue/claude/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skill), ReleaseLine) {
		t.Errorf("close-issue no longer names %q", ReleaseLine)
	}
}

// test_another_open_blocker_keeps_the_label_and_reports_back.
func TestAnotherOpenBlockerKeepsTheLabelAndReportsBack(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.addIssue(1, "closed", "completed")
		w.addIssue(7, "open", "")
		w.addIssue(2, "open", "", label)
		w.block(2, 1)
		w.block(2, 7)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, "## Release lane for #1\n\n- #2: still waits for #7\n", "comment #1")
		if got, want := comments(t, w, 1), []string{"Released lane after this close:\n\n- #2 still waits for #7"}; !slices.Equal(got, want) {
			t.Errorf("comments #1 = %q, want %q", got, want)
		}
		if got := labels(t, w, 2); !slices.Equal(got, []string{label}) {
			t.Errorf("labels #2 = %q, want [%s]", got, label)
		}
	})
}

// test_without_label_not_read_and_no_write: neither its blockers read, nor a write, nor a
// mention in a reply comment.
func TestWithoutLabelNotReadAndNoWrite(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.addIssue(1, "closed", "completed")
		w.addIssue(2, "open", "")
		w.block(2, 1)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, "## Release lane for #1\n\n- #2: does not carry `blocked-issue`, nothing to do\n")
		if w.blockersRead(2) {
			t.Error("the blockers of #2 were read; a dependent without the label is not read")
		}
	})
}

// Next to a released dependent, one without the label stays out of the reply comment.
func TestWithoutLabelStaysOutOfTheReply(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		closedWithDependent(w, label)
		w.addIssue(3, "open", "")
		w.block(3, 1)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, released+"- #3: does not carry `blocked-issue`, nothing to do\n",
			"comment #1", "comment #2", "unlabel #2")
		if got, want := comments(t, w, 1), []string{"Released lane after this close:\n\n- #2 is released: `blocked-issue` comes off"}; !slices.Equal(got, want) {
			t.Errorf("comments #1 = %q, want %q", got, want)
		}
	})
}

// test_failing_reply_comment_touches_nothing_and_restart_repeats_everything.
func TestFailingReplyCommentTouchesNothing(t *testing.T) {
	for _, fc := range forges {
		t.Run(fc.name, func(t *testing.T) {
			w := fc.new(t)
			closedWithDependent(w, label)
			w.failComment(1)
			code, out := run(t, w, 1, label, false)
			if code != 1 || !strings.HasPrefix(out, released+"**Error:** comment on #1: ") || !strings.HasSuffix(out, "; nothing touched\n") {
				t.Errorf("Run = %d, %q; want 1, the release line and **Error:** comment on #1: …; nothing touched", code, out)
			}
			if got := w.writes(); !slices.Equal(got, []string{"comment #1"}) {
				t.Errorf("writes = %q, want only the failed comment #1", got)
			}
			if got := labels(t, w, 2); !slices.Contains(got, label) {
				t.Errorf("labels #2 = %q; the label must stay", got)
			}
		})
	}
	t.Run("github message", func(t *testing.T) {
		w := newGitHub(t)
		closedWithDependent(w, label)
		w.failComment(1)
		_, out := run(t, w, 1, label, false)
		if !strings.Contains(out, "status 403: nee; nothing touched") {
			t.Errorf("out = %q; want the forge's message", out)
		}
	})
}

// test_failing_comment_leaves_the_label.
func TestFailingCommentLeavesTheLabel(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		closedWithDependent(w, label)
		w.failComment(2)
		code, out := run(t, w, 1, label, false)
		if code != 1 || !strings.HasPrefix(out, released+"- #2: **Error:** ") {
			t.Errorf("Run = %d, %q; want 1, the release line and - #2: **Error:** …", code, out)
		}
		if got := w.writes(); !slices.Equal(got, []string{"comment #1", "comment #2"}) {
			t.Errorf("writes = %q, want comment #1, comment #2 and no unlabel", got)
		}
		if got := labels(t, w, 2); !slices.Contains(got, label) {
			t.Errorf("labels #2 = %q; the label must stay", got)
		}
	})
}

// test_label_404_is_already_gone: the label was removed by hand between the read and the
// write. GitLab has no 404 there: removing an absent label is a no-op.
func TestLabel404IsAlreadyGone(t *testing.T) {
	w := newGitHub(t).(*ghWorld)
	closedWithDependent(w, label)
	w.srv.Fail["DELETE /repos/Owner/repo/issues/2/labels/blocked-issue"] = 404
	w.srv.FailMessage["DELETE /repos/Owner/repo/issues/2/labels/blocked-issue"] = "Label does not exist"
	code, out := run(t, w, 1, label, false)
	check(t, w, code, out, 0, released, "comment #1", "comment #2", "unlabel #2")
}

// Only a 404 means "already gone": a label that cannot be removed is red.
func TestLabelRemovalRefusedIsRed(t *testing.T) {
	w := newGitHub(t).(*ghWorld)
	closedWithDependent(w, label)
	w.fail("DELETE /repos/Owner/repo/issues/2/labels/blocked-issue")
	code, out := run(t, w, 1, label, false)
	want := released + "- #2: **Error:** forge DELETE " + w.srv.URL() +
		"/repos/Owner/repo/issues/2/labels/blocked-issue: status 403: nee\n"
	check(t, w, code, out, 1, want, "comment #1", "comment #2", "unlabel #2")
}

// test_not_completed_releases_nothing[not_planned|duplicate]: GitHub only, GitLab has no
// close reason.
func TestNotCompletedReleasesNothing(t *testing.T) {
	for _, reason := range []string{"not_planned", "duplicate"} {
		t.Run(reason, func(t *testing.T) {
			w := newGitHub(t)
			closedWithDependent(w, label)
			w.addIssue(1, "closed", reason)
			code, out := run(t, w, 1, label, false)
			check(t, w, code, out, 0, "## Release lane for #1\n\n#1 closed as `"+reason+"`: blocks stay.\n")
		})
	}
}

// A close without a reason counts as completed on GitLab, which has none, and not on GitHub.
func TestEmptyStateReason(t *testing.T) {
	t.Run("gitlab releases", func(t *testing.T) {
		w := newGitLab(t)
		closedWithDependent(w, label)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, released, "comment #1", "comment #2", "unlabel #2")
	})
	t.Run("github does not", func(t *testing.T) {
		w := newGitHub(t)
		closedWithDependent(w, label)
		w.addIssue(1, "closed", "")
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, "## Release lane for #1\n\n#1 closed as `none`: blocks stay.\n")
	})
}

// test_open_issue_releases_nothing and test_graphql_free_reply_with_trailing_newline.
func TestOpenIssueReleasesNothing(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.addIssue(1, "open", "")
		w.addIssue(2, "open", "", label)
		w.block(2, 1)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, "## Release lane for #1\n\n#1 is open: nothing to release.\n")
	})
}

// test_closed_dependent_does_not_count.
func TestClosedDependentDoesNotCount(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.addIssue(1, "closed", "completed")
		w.addIssue(2, "closed", "completed", label)
		w.block(2, 1)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, "## Release lane for #1\n\n#1 blocked no open issues.\n")
	})
}

// test_unreadable_blockers_is_red_and_leaves_the_label: the other dependent is still released.
func TestUnreadableBlockersIsRedAndLeavesTheLabel(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.addIssue(1, "closed", "completed")
		w.addIssue(2, "open", "", label)
		w.addIssue(3, "open", "", label)
		w.block(2, 1)
		w.block(3, 1)
		w.failBlockedBy(2)
		code, out := run(t, w, 1, label, false)
		first, rest, _ := strings.Cut(out, "\n- #3: ")
		if code != 1 || !strings.HasPrefix(first, "## Release lane for #1\n\n- #2: **Error:** ") ||
			rest != "released, `blocked-issue` comes off\n" {
			t.Errorf("Run = %d, %q; want 1, an error line for #2 and #3 released", code, out)
		}
		if got := w.writes(); !slices.Equal(got, []string{"comment #1", "comment #3", "unlabel #3"}) {
			t.Errorf("writes = %q, want comment #1, comment #3, unlabel #3", got)
		}
		if got := labels(t, w, 2); !slices.Contains(got, label) {
			t.Errorf("labels #2 = %q; the label must stay", got)
		}
	})
}

// test_closed_issue_unreadable_is_red, and the gh errors that are forge errors now:
// gh_error_without_stderr, gh_error_shows_the_first_line_of_stderr, reply_not_json,
// issue_without_state and gh_timeout.
func TestClosedIssueUnreadableIsRed(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.failIssue(1)
		closedWithDependent(w, label)
		code, out := run(t, w, 1, label, false)
		if code != 1 || !strings.HasPrefix(out, "## Release lane for #1\n\n**Error:** #1 not read: forge GET ") ||
			strings.Count(out, "\n") != 3 {
			t.Errorf("Run = %d, %q; want 1 and one line **Error:** #1 not read: <forge error>", code, out)
		}
		if got := w.writes(); len(got) != 0 {
			t.Errorf("writes = %q, want none", got)
		}
	})
	t.Run("github message", func(t *testing.T) {
		w := newGitHub(t)
		w.failIssue(1)
		_, out := run(t, w, 1, label, false)
		if !strings.HasSuffix(out, "/repos/Owner/repo/issues/1: status 403: nee\n") {
			t.Errorf("out = %q; want the forge's error", out)
		}
	})
}

// test_dependency_list_that_is_not_a_list_is_red: the blocking list that does not read.
func TestBlockingUnreadableIsRed(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		closedWithDependent(w, label)
		w.failBlocking(1)
		code, out := run(t, w, 1, label, false)
		if code != 1 || !strings.HasPrefix(out, "## Release lane for #1\n\n**Error:** #1 not read: forge GET ") {
			t.Errorf("Run = %d, %q; want 1 and **Error:** #1 not read: <forge error>", code, out)
		}
		if got := w.writes(); len(got) != 0 {
			t.Errorf("writes = %q, want none", got)
		}
	})
}

// A dependent whose labels do not read is an error line and stays as it is.
func TestDependentUnreadableIsRed(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.failIssue(2)
		closedWithDependent(w, label)
		code, out := run(t, w, 1, label, false)
		if code != 1 || strings.Count(out, "\n") != 3 || !strings.HasPrefix(out, "## Release lane for #1\n\n- #2: **Error:** forge GET ") {
			t.Errorf("Run = %d, %q; want 1 and one error line for #2", code, out)
		}
		if got := w.writes(); len(got) != 0 {
			t.Errorf("writes = %q, want none", got)
		}
	})
}

// test_dry_run_writes_nothing.
func TestDryRunWritesNothing(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		closedWithDependent(w, label)
		code, out := run(t, w, 1, label, true)
		check(t, w, code, out, 0, released+"\n_dry-run: nothing written._\n")
	})
}

// test_blocked_label_from_config, test_label_with_a_space_is_url_encoded and
// test_label_with_reserved_characters_is_fully_encoded: the label really comes off.
func TestOtherLabelsComeOff(t *testing.T) {
	for _, lbl := range []string{"x", "blocked / issue", "3: blocked/issue +x"} {
		t.Run(lbl, func(t *testing.T) {
			onBoth(t, func(t *testing.T, w world) {
				closedWithDependent(w, lbl)
				code, out := run(t, w, 1, lbl, false)
				check(t, w, code, out, 0, "## Release lane for #1\n\n- #2: released, `"+lbl+"` comes off\n",
					"comment #1", "comment #2", "unlabel #2")
				if got := labels(t, w, 2); !slices.Equal(got, []string{"2-area:x"}) {
					t.Errorf("labels #2 = %q, want [2-area:x]", got)
				}
				if got, want := comments(t, w, 2), []string{"Released: the last open blocker #1 is closed; `" + lbl + "` comes off."}; !slices.Equal(got, want) {
					t.Errorf("comments #2 = %q, want %q", got, want)
				}
			})
		})
	}
}

// The forge spells the repo canonically, lighthouse.toml as the user typed it: the same repo.
func TestRepoComparedWithoutCase(t *testing.T) {
	for _, fc := range []struct {
		name, spelling string
		new            func(t *testing.T) world
	}{{"github", "owner/REPO", newGitHub}, {"gitlab", "Group/Project", newGitLab}} {
		t.Run(fc.name, func(t *testing.T) {
			w := fc.new(t)
			w.addIssue(1, "closed", "completed")
			w.addIssue(2, "open", "", label)
			w.addBlocking(1, fc.spelling, 2)
			code, out := run(t, w, 1, label, false)
			check(t, w, code, out, 0, released, "comment #1", "comment #2", "unlabel #2")
		})
	}
}

// A dependent in another repository is neither read nor edited, and the reply leaves it out.
func TestDependentInAnotherRepository(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		closedWithDependent(w, label)
		w.addIssue(9, "open", "", label) // the same number here must stay untouched
		w.addBlocking(1, "other/repo", 9)
		code, out := run(t, w, 1, label, false)
		// GitLab lists the links it was given before the ones it derives.
		other := "- other/repo#9: in another repository, not released\n"
		want := released + other
		if w.forge().Kind() == "gitlab" {
			want = "## Release lane for #1\n\n" + other + "- #2: released, `blocked-issue` comes off\n"
		}
		check(t, w, code, out, 0, want, "comment #1", "comment #2", "unlabel #2")
		if got, want := comments(t, w, 1), []string{"Released lane after this close:\n\n- #2 is released: `blocked-issue` comes off"}; !slices.Equal(got, want) {
			t.Errorf("comments #1 = %q, want %q", got, want)
		}
		if w.blockersRead(9) {
			t.Error("the blockers of #9 were read; it lives in another repository")
		}
	})
}

// Only another repository alone: nothing to write.
func TestOnlyAnotherRepositoryWritesNothing(t *testing.T) {
	onBoth(t, func(t *testing.T, w world) {
		w.addIssue(1, "closed", "completed")
		w.addBlocking(1, "other/repo", 9)
		code, out := run(t, w, 1, label, false)
		check(t, w, code, out, 0, "## Release lane for #1\n\n- other/repo#9: in another repository, not released\n")
	})
}
