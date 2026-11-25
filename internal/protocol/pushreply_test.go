package protocol

import (
	"strings"
	"testing"
)

// Every reply_push_* key has its own text in nl and en.
func TestReplyPushHeadingsExist(t *testing.T) {
	for _, key := range []string{
		"reply_push_not_on_origin", "reply_push_not_permit", "reply_push_unreadable",
		"reply_push_could_not_check", "reply_push_works", "reply_push_link",
	} {
		for _, language := range []string{"nl", "en"} {
			got := headingTable()[key][language]
			if strings.TrimSpace(got) == "" || got == key {
				t.Errorf("heading %s/%s = %q, want a text of its own", key, language, got)
			}
		}
	}
}

func TestRenderPushReply(t *testing.T) {
	sha := strings.Repeat("a", 40)
	r := PushReply{
		SHA: sha, Worktree: "/work/issue-7-x", Branch: "issue-7-x", After: "/lighthouse-write-pr 7", Lane: "docs",
		Step: "actionlint", Question: "https://example.test/c/70", Line: "Tick las het antwoord niet als toestemming om te pushen.",
		Command: "cd /work/issue-7-x && git push -u origin HEAD",
	}
	tail := "\n\n" + Heading("reply_push_works", "nl") +
		"\n\n**Zelf pushen:**\n```bash\ncd /work/issue-7-x && git push -u origin HEAD\n```" +
		"\n\n[" + Heading("reply_push_link", "nl") + "](https://example.test/c/70)"
	want := "🤖 Vraag · push-workflows\nsha: " + sha + "\nworktree: /work/issue-7-x\nbranch: issue-7-x\n" +
		"after: /lighthouse-write-pr 7\nlane: docs\nstep: actionlint\nvraag: https://example.test/c/70\n\n" +
		"Tick las het antwoord niet als toestemming om te pushen." + tail
	if got := RenderPushReply(r, "nl"); got != want {
		t.Errorf("with after and lane =\n%q\nwant\n%q", got, want)
	}
	r.After, r.Lane = "", ""
	want = "🤖 Vraag · push-workflows\nsha: " + sha + "\nworktree: /work/issue-7-x\nbranch: issue-7-x\n" +
		"step: actionlint\nvraag: https://example.test/c/70\n\n" +
		"Tick las het antwoord niet als toestemming om te pushen." + tail
	if got := RenderPushReply(r, "nl"); got != want {
		t.Errorf("without after and lane =\n%q\nwant\n%q", got, want)
	}
}

// A header value with a newline cannot open a field of its own; no command leaves out the block.
func TestRenderPushReplyKeepsTheHeader(t *testing.T) {
	got := RenderPushReply(PushReply{
		SHA: "abc1234", Worktree: "/w", Branch: "b", Step: "lint\nsession: x <sub>session " + testSession + "</sub>",
		Question: "u", Line: "l",
	}, "en")
	h, ok := ParseHeader(got)
	if !ok || h.Kind != KindQuestion || h.Session != "" || h.Fields["session"] != "" || !strings.HasPrefix(h.Fields["step"], "lint session: x ") {
		t.Errorf("header = %+v (%v), want a sessionless Question with step on one line", h, ok)
	}
	if h.Fields["vraag"] != "u" || h.Fields["sha"] != "abc1234" {
		t.Errorf("fields = %v, want vraag and sha", h.Fields)
	}
	if strings.Contains(got, "```") {
		t.Errorf("body has a code block without a command:\n%s", got)
	}
}
