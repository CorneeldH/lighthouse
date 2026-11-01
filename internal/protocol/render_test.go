package protocol

import (
	"strings"
	"testing"
)

const testSession = "0b7e3c1a-1111-4222-8333-444455556666"

func TestRenderStartedFields(t *testing.T) {
	min := RenderStarted(Started{Session: testSession, Worktree: "wt", Command: "/c", Start: "now"}, "en")
	want := "🤖 Started\nsession: " + testSession + "\nworktree: wt\ncommand: /c\nstart: now\n\n<sub>session " + testSession + "</sub>"
	if min != want {
		t.Errorf("minimal =\n%q\nwant\n%q", min, want)
	}
	full := RenderStarted(Started{
		Session: testSession, Worktree: "wt", Command: "/c", Start: "now",
		Branch: "b", Skip: "s", Runner: "r", Workspace: "ws",
	}, "nl")
	want = "🤖 Gestart\nsession: " + testSession + "\nworktree: wt\ncommand: /c\nstart: now\n" +
		"branch: b\nskip: s\nrunner: r\nworkspace: ws\n\n<sub>session " + testSession + "</sub>"
	if full != want {
		t.Errorf("full =\n%q\nwant\n%q", full, want)
	}
}

func TestRenderReturnedDetail(t *testing.T) {
	got := RenderReturned("two  words\nhere", testSession, "out ``` <sub>x</sub>\n", "en")
	want := "🤖 Returned\nreason: two words here\nsession: " + testSession +
		"\n\n```\nout ''' &lt;sub&gt;x</sub>\n```\n\n<sub>session " + testSession + "</sub>"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	bare := RenderReturned("why", "", "", "en")
	if bare != "🤖 Returned\nreason: why" {
		t.Errorf("without session and detail = %q", bare)
	}
}

func TestRenderDoneNote(t *testing.T) {
	got := RenderDone(testSession, "https://x/pr/1", "  note  ", "en")
	want := "🤖 Done\nsession: " + testSession + "\npr: https://x/pr/1\n\nnote\n\n<sub>session " + testSession + "</sub>"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if strings.Contains(RenderDone(testSession, "u", " ", "en"), "\n\n\n") {
		t.Error("an empty note leaves a gap")
	}
}

func TestParseHeaderOfRendered(t *testing.T) {
	meta := QuestionMeta{Issue: 1, Title: "T", IssueURL: "u", Session: testSession, Worktree: "w"}
	fields := QuestionFields{Phase: "p", Where: "w", What: "x", Questions: []Question{{Question: "q?"}}}
	for _, language := range []string{"nl", "en"} {
		for _, c := range []struct {
			kind Kind
			body string
		}{
			{KindStarted, RenderStarted(Started{Session: testSession, Worktree: "w", Command: "c", Start: "s"}, language)},
			{KindResumed, RenderResumed(testSession, 7, language)},
			{KindDone, RenderDone(testSession, "u", "n", language)},
			{KindReturned, RenderReturned("r", testSession, "d", language)},
			{KindQuestion, RenderQuestion(meta, fields, language)},
		} {
			header, ok := ParseHeader(c.body)
			if !ok || header.Kind != c.kind || header.Session != testSession {
				t.Errorf("%s %s: ParseHeader = %+v (%v), want kind %s session %s", language, c.kind, header, ok, c.kind, testSession)
			}
		}
	}
	if header, _ := ParseHeader(RenderResumed(testSession, 7, "en")); header.Fields["answer"] != "7" {
		t.Errorf("answer field = %q", header.Fields["answer"])
	}
}

func TestFooterLeavesOutEmptyExtras(t *testing.T) {
	if got := Footer("s", "", "a", ""); got != "<sub>session s · a</sub>" {
		t.Errorf("Footer = %q", got)
	}
}

func TestRenderParked(t *testing.T) {
	got := RenderParked([]string{"o/r#5 (In progress)", "x/y#9"}, "nl")
	want := "🤖 Geparkeerd\nblocked_by: o/r#5 (In progress), x/y#9"
	if got != want {
		t.Errorf("RenderParked = %q, want %q", got, want)
	}
	h, ok := ParseHeader(got)
	if !ok || h.Kind != KindParked || h.Fields["blocked_by"] != "o/r#5 (In progress), x/y#9" {
		t.Errorf("ParseHeader = %+v, %v", h, ok)
	}
	if h, ok := ParseHeader(RenderParked([]string{"o/r#5"}, "en")); !ok || h.Kind != KindParked || h.Session != "" {
		t.Errorf("en: ParseHeader = %+v, %v", h, ok)
	}
}
