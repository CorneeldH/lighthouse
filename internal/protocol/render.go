package protocol

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The renderers write the same text for the same input. Headings come from the table, never from
// a literal here.

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

// RenderPusherDone renders the `🤖 Done` of the merge pusher after its merge of the PR at prURL:
// no session, since no session ran.
func RenderPusherDone(prURL, note, language string) string {
	text := Robot + " " + Heading("kind_done", language) + "\npr: " + prURL
	if strings.TrimSpace(note) != "" {
		text += "\n\n" + strings.TrimSpace(note)
	}
	return text
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

// RenderParked renders a `🤖 Parked`: the header and one `blocked_by:` line with the open
// predecessors. It has no session and no footer. Only the tick posts one.
func RenderParked(blockers []string, language string) string {
	return Robot + " " + Heading("kind_parked", language) + "\nblocked_by: " + strings.Join(blockers, ", ")
}

// RenderPushed renders the `🤖 Handoff` the pusher posts on the build route (no PR yet): the
// five fields ParseHandoff demands, plus the pushed range and the workflow files. next is
// the after command of the push handoff.
func RenderPushed(issue int, worktree, branch, repoRange, workflows, next, language string) string {
	return strings.Join([]string{
		Robot + " " + Heading("kind_handoff", language),
		"issue: #" + strconv.Itoa(issue),
		"worktree: " + worktree,
		"branch: " + branch,
		"next: " + next,
		"when: now",
		"range: " + repoRange,
		"workflows: " + workflows,
	}, "\n")
}

// RenderPushNote renders the comment the pusher posts on the fix-pr route, where the PR
// already exists: the pushed branch, range and workflow files, no handoff. The branch scopes the
// push to the PR of that head (ParsePushed).
func RenderPushNote(branch, rangeStr, workflows, language string) string {
	return strings.Join([]string{
		Robot + " " + Heading("note_pushed", language),
		"branch: " + branch,
		"range: " + rangeStr,
		"workflows: " + workflows,
	}, "\n")
}

// Pushed is what a pusher comment says about one push: the branch and the SHA it pushed.
type Pushed struct{ Branch, SHA string }

// ParsePushed reads a comment of the pusher after a push: the handoff of the build route
// (RenderPushed, a Handoff with range and workflows and no session footer) or the note of the
// fix-pr route (RenderPushNote). The SHA is the end of range and must be a full SHA; the branch
// must be set. false for any other comment.
func ParsePushed(body string) (Pushed, bool) {
	var fields map[string]string
	if h, ok := ParseHeader(body); ok {
		if h.Kind != KindHandoff || h.Session != "" || h.Fields["workflows"] == "" {
			return Pushed{}, false
		}
		fields = h.Fields
	} else if fields, ok = pushNoteFields(body); !ok {
		return Pushed{}, false
	}
	_, sha, ok := strings.Cut(fields["range"], "..")
	if !ok || !fullSHA.MatchString(sha) || fields["branch"] == "" {
		return Pushed{}, false
	}
	return Pushed{Branch: fields["branch"], SHA: sha}, true
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// pushNoteFields reads the fields of a RenderPushNote in any language, false for another body.
func pushNoteFields(body string) (map[string]string, bool) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	title, ok := strings.CutPrefix(lines[0], Robot+" ")
	if !ok || !slices.Contains(Variants("note_pushed"), strings.TrimSpace(title)) {
		return nil, false
	}
	fields := map[string]string{}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if m := fieldLine.FindStringSubmatch(line); m != nil {
			fields[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return fields, true
}
