package protocol

import (
	"regexp"
	"strconv"
	"strings"
)

// The renderers are the Go twins of render_* in pack/core/lib/protocol.py: the same text for
// the same input. Headings come from the table, never from a literal here.

// clean keeps free text from breaking open a footer or a code block. It works one way only.
func clean(text string) string {
	text = strings.ReplaceAll(text, "```", "'''")
	text = strings.ReplaceAll(text, "~~~", "'''")
	text = strings.ReplaceAll(text, "<sub>", "&lt;sub&gt;")
	return strings.TrimSpace(text)
}

var backtickRun = regexp.MustCompile("`+")

// codeBlock is one command literally as a fenced block; the fence is longer than every run of
// backticks in the command, so the block never closes early.
func codeBlock(command string) []string {
	longest := 0
	for _, run := range backtickRun.FindAllString(command, -1) {
		longest = max(longest, len(run))
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return []string{fence + "bash", command, fence}
}

// Footer is the `<sub>session <uuid> · …</sub>` line; empty extras are left out.
func Footer(session string, extra ...string) string {
	parts := []string{"session " + session}
	for _, e := range extra {
		if e != "" {
			parts = append(parts, e)
		}
	}
	return "<sub>" + strings.Join(parts, " · ") + "</sub>"
}

// Started holds the fields of a `🤖 Started`. An empty field is left out.
type Started struct {
	Session, Worktree, Command, Start, Branch, Skip, Runner, Workspace string
}

// RenderStarted renders a `🤖 Started`: header, fields, footer.
func RenderStarted(s Started, language string) string {
	lines := []string{
		Robot + " " + Heading("kind_started", language),
		"session: " + s.Session,
		"worktree: " + s.Worktree,
		"command: " + s.Command,
		"start: " + s.Start,
	}
	for _, f := range []struct{ key, value string }{
		{"branch", s.Branch}, {"skip", s.Skip}, {"runner", s.Runner}, {"workspace", s.Workspace},
	} {
		if f.value != "" {
			lines = append(lines, f.key+": "+f.value)
		}
	}
	return strings.Join(lines, "\n") + "\n\n" + Footer(s.Session)
}

// RenderResumed renders a `🤖 Resumed` for the answer with this comment id.
func RenderResumed(session string, answerID int64, language string) string {
	return Robot + " " + Heading("kind_resumed", language) + "\n" +
		"session: " + session + "\nanswer: " + strconv.FormatInt(answerID, 10) + "\n\n" + Footer(session)
}

// RenderDone renders a `🤖 Done`; the prose in between (note) comes from the caller.
func RenderDone(session, prURL, note, language string) string {
	text := Robot + " " + Heading("kind_done", language) + "\nsession: " + session + "\npr: " + prURL
	if strings.TrimSpace(note) != "" {
		text += "\n\n" + strings.TrimSpace(note)
	}
	return text + "\n\n" + Footer(session)
}

// RenderReturned renders a `🤖 Returned`. An empty session leaves out the session line and
// the footer; an empty detail leaves out the code block.
func RenderReturned(reason, session, detail, language string) string {
	lines := []string{
		Robot + " " + Heading("kind_returned", language),
		"reason: " + strings.Join(strings.Fields(reason), " "),
	}
	if session != "" {
		lines = append(lines, "session: "+session)
	}
	text := strings.Join(lines, "\n")
	if strings.TrimSpace(detail) != "" {
		text += "\n\n```\n" + clean(detail) + "\n```"
	}
	if session != "" {
		text += "\n\n" + Footer(session)
	}
	return text
}
