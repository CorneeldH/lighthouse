package protocol

import "strings"

// pusherCommand names the workflow pusher in the header of its 🤖 Vraag.
const pusherCommand = "push-workflows"

// PushReply is what tick says to an answer on a pusher 🤖 Vraag that started nothing: the fields
// of the Vraag it answers, so the reply is the open pusher Vraag in its turn.
type PushReply struct {
	SHA, Worktree, Branch, After, Lane, Step string
	Question                                 string // comment URL of the original Vraag with the diff
	Line                                     string // what tick made of the answer (heading key resolved by caller)
	Command                                  string // the push command, "" when none
}

// RenderPushReply renders the reply as a sessionless 🤖 Vraag of the pusher: the header with the
// Vraag's fields and vraag: (the original Vraag), what tick made of the answer, the answers that
// work, the push command and a link to the Vraag. No diff: the link names where it stands.
func RenderPushReply(r PushReply, language string) string {
	h := func(key string) string { return Heading(key, language) }
	lines := []string{Robot + " " + h("kind_question") + " · " + pusherCommand,
		"sha: " + headerLine(r.SHA), "worktree: " + headerLine(r.Worktree), "branch: " + headerLine(r.Branch)}
	if r.After != "" {
		lines = append(lines, "after: "+headerLine(r.After))
	}
	if r.Lane != "" {
		lines = append(lines, "lane: "+headerLine(r.Lane))
	}
	lines = append(lines, "step: "+headerLine(r.Step))
	if r.Question != "" {
		lines = append(lines, "vraag: "+headerLine(r.Question))
	}
	lines = append(lines, "", clean(r.Line), "", h("reply_push_works"))
	if r.Command != "" {
		lines = append(lines, "", "**"+h("question_push_command")+":**")
		lines = append(lines, codeBlock(r.Command)...)
	}
	if r.Question != "" {
		lines = append(lines, "", "["+h("reply_push_link")+"]("+headerLine(r.Question)+")")
	}
	return strings.Join(lines, "\n")
}

// headerLine keeps a header value on its own line and out of a session footer.
func headerLine(value string) string {
	return strings.Join(strings.Fields(clean(value)), " ")
}
