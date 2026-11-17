package pusher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// maxComment is GitHub's limit on the characters of one comment; a Question stays under it.
const maxComment = 65536

// The caps on reviewer and step text in a Question, so that only the diff can make it too long.
const (
	maxWhy      = 8000
	maxProse    = 1000
	maxFindings = 20
)

// hint is what an answer push does with a Question, as the Question tells the user.
type hint int

const (
	hintAuto   hint = iota // hintRetry when the Question carries a SHA, else hintManual
	hintPermit             // the review did not approve, or shadow: push lifts the review
	hintRetry              // push runs everything but the review again for this SHA
	hintMoved              // the worktree left the permitted SHA: push gives this Question again
	hintManual             // the Question carries no SHA: push starts nothing
)

// hintHeadings are the heading keys of the hints.
var hintHeadings = map[hint]string{
	hintPermit: "question_push_permit", hintRetry: "question_push_retry",
	hintMoved: "question_push_moved", hintManual: "question_push_manual",
}

// questionInput is what a pusher Question says: the red step, the SHA, worktree, branch and after
// decide reads back for the answer push, the workflow diff and the reviewers' verdict when they
// exist, and what an answer push does with it. An empty sha is the push's own (push.sha).
type questionInput struct {
	red                                            Red
	sha, worktree, branch, after, diff, attachment string
	verdict                                        *Verdict
	hint                                           hint
}

// postTimeout bounds posting a comment and closing the row, apart from the work's deadline.
const postTimeout = 2 * time.Minute

// ask posts the Question for q and closes the push row as question. The text is scrubbed of every
// token the pusher holds, and cut under maxComment with the full diff as an attachment.
func (p *push) ask(ctx context.Context, q questionInput) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	if q.sha == "" {
		q.sha = p.sha
	}
	q.worktree, q.branch, q.after = p.req.Worktree, p.req.Branch, p.after
	if q.hint == hintAuto {
		q.hint = hintManual
		if shaRe.MatchString(q.sha) {
			q.hint = hintRetry
		}
	}
	scrub := scrubList(p.scrub())
	q.red.Why = scrubText(q.red.Why, scrub)
	q.diff = scrubText(q.diff, scrub)
	body := fitQuestion(q, p.req.Language, scrub, func(diff string) (string, error) { return p.attach(q.sha, diff) })
	if _, err := p.deps.Forge.CreateComment(ctx, p.req.Issue, body); err != nil {
		return fmt.Errorf("pusher: post the Question on #%d: %w", p.req.Issue, err)
	}
	return runs.Release(ctx, p.deps.DB, p.req.RunID, runs.Question, "push: "+q.red.Step, p.deps.Now())
}

// pushed posts the outcome of a push. The row first records the pushed SHA, so a post that fails
// leaves a row decide knows has pushed. On the fix-pr route the row stays open as the owner of the
// PR, and a note names the range and the workflows. On the build route a 🤖 Handoff hands the
// chain to after and the row closes as handoff.
func (p *push) pushed(ctx context.Context, fixPR bool, rangeText string, workflows []string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postTimeout)
	defer cancel()
	quoted := make([]string, len(workflows))
	for i, w := range workflows {
		quoted[i] = headerValue(w)
	}
	list := strings.Join(quoted, ", ")
	if err := runs.SetPushed(ctx, p.deps.DB, p.req.RunID, p.sha); err != nil {
		return err
	}
	if fixPR {
		if _, err := p.deps.Forge.CreateComment(ctx, p.req.Issue, protocol.RenderPushNote(headerValue(p.req.Branch), headerValue(rangeText), list, p.req.Language)); err != nil {
			return fmt.Errorf("pusher: post the push note on #%d: %w", p.req.Issue, err)
		}
		return nil
	}
	body := protocol.RenderPushed(p.req.Issue, headerValue(p.req.Worktree), headerValue(p.req.Branch), headerValue(rangeText), list, headerValue(p.after), p.req.Language)
	if _, err := p.deps.Forge.CreateComment(ctx, p.req.Issue, body); err != nil {
		return fmt.Errorf("pusher: post the handoff on #%d: %w", p.req.Issue, err)
	}
	return runs.Release(ctx, p.deps.DB, p.req.RunID, runs.Handoff, "", p.deps.Now())
}

// attach writes the full diff into the pusher's own state dir, at
// <state>/push/<owner>/<name>/<sha>-<run>.diff: never in the run's worktree or git-dir, whose
// paths the run controls. No directory below the state dir may be a symlink, and the file is new.
func (p *push) attach(sha, diff string) (string, error) {
	owner, name, ok := strings.Cut(p.req.Repo, "/")
	if !filepath.IsAbs(p.req.StateDir) || !ok || !slugPart(owner) || !slugPart(name) || !shaRe.MatchString(sha) && sha != noSHA {
		return "", fmt.Errorf("state dir %q, repo %q or sha %q is not usable for an attachment", p.req.StateDir, p.req.Repo, sha)
	}
	if err := os.MkdirAll(p.req.StateDir, 0o700); err != nil {
		return "", err
	}
	dir := p.req.StateDir
	for _, part := range []string{"push", owner, name} {
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory of the pusher", dir)
		}
	}
	path := filepath.Join(dir, sha+"-"+strconv.FormatInt(p.req.RunID, 10)+".diff")
	if strings.ContainsRune(path, '`') || strings.ContainsFunc(path, unicode.IsControl) {
		return "", fmt.Errorf("attachment path %q cannot be named in a comment", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|oNoFollow, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(diff); err != nil {
		_ = f.Close()
		return "", err
	}
	return path, f.Close()
}

// fitQuestion renders q, scrubbed. A Question of maxComment characters or more gets its diff cut
// at a line under the limit, with the full diff written by attach and named in the text.
func fitQuestion(q questionInput, language string, scrub []string, attach func(string) (string, error)) string {
	render := func() string { return scrubText(renderQuestion(q, language), scrub) }
	body := render()
	over := utf8.RuneCountInString(body) - (maxComment - 1)
	if over <= 0 {
		return body
	}
	full := q.diff
	if path, err := attach(full); err == nil {
		q.attachment = path
	} else {
		q.red.Why += "\n(the full diff could not be saved: " + err.Error() + ")"
	}
	n := len(full)
	for over > 0 && q.diff != "" {
		// A rune is at least one byte, so cutting the overflow in bytes always shortens enough.
		n -= over
		q.diff = cutAtLine(full, n)
		body = render()
		over = utf8.RuneCountInString(body) - (maxComment - 1)
	}
	if over > 0 {
		// Only the diff can be long, but stay under the limit whatever happens.
		r := []rune(body)
		body = string(r[:maxComment-1])
	}
	return body
}

// cutAtLine is the longest prefix of s of at most n bytes that ends with a newline.
func cutAtLine(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	i := strings.LastIndexByte(s[:n], '\n')
	return s[:i+1]
}

// renderQuestion renders the pusher's 🤖 Question. The header holds sha, worktree, branch and after
// for decide, and has no session footer: that is how decide tells it from a run's question.
func renderQuestion(q questionInput, language string) string {
	h := func(key string) string { return protocol.Heading(key, language) }
	lines := []string{
		protocol.Robot + " " + h("kind_question") + " · " + Command,
		"sha: " + headerValue(q.sha),
		"worktree: " + headerValue(q.worktree),
		"branch: " + headerValue(q.branch),
	}
	if q.after != "" {
		lines = append(lines, "after: "+headerValue(q.after))
	}
	lines = append(lines, "step: "+headerValue(q.red.Step), "",
		"**"+h("question_push_step")+":** "+prose(q.red.Step, maxProse))
	lines = append(lines, fenced("text", capRunes(strings.TrimSpace(q.red.Why), maxWhy))...)
	if q.verdict != nil {
		lines = append(lines, "", "**"+h("question_push_verdict")+":**")
		lines = append(lines, verdictLines(*q.verdict)...)
	}
	if q.diff != "" {
		lines = append(lines, "", "**"+h("question_push_diff")+":**")
		lines = append(lines, fenced("diff", strings.ReplaceAll(q.diff, "<sub>", "&lt;sub&gt;"))...)
	}
	if q.attachment != "" {
		lines = append(lines, "", h("question_push_attachment")+" `"+q.attachment+"`.")
	}
	if key, ok := hintHeadings[q.hint]; ok {
		lines = append(lines, "", h(key))
	}
	lines = append(lines, "", "**"+h("question_push_command")+":**")
	lines = append(lines, fenced("bash", "cd "+shellWord(q.worktree)+" && git push -u origin HEAD")...)
	return strings.Join(lines, "\n")
}

// verdictLines is one line per reviewer, with its findings below it.
func verdictLines(v Verdict) []string {
	var lines []string
	for _, r := range v.Results {
		line := "- " + prose(r.Topic, maxProse) + ": "
		if r.Approve {
			line += "approve"
		} else {
			line += "reject"
			if r.Why != "" {
				line += ": " + prose(r.Why, maxProse)
			}
		}
		lines = append(lines, line)
		for i, f := range r.Findings {
			if i == maxFindings {
				lines = append(lines, fmt.Sprintf("  - … %d more", len(r.Findings)-maxFindings))
				break
			}
			lines = append(lines, fmt.Sprintf("  - %s:%d: %s (%s)", prose(f.File, maxProse), f.Line,
				prose(f.Issue, maxProse), prose(f.Evidence, maxProse)))
		}
	}
	return lines
}

var backticks = regexp.MustCompile("`+")

// fenced is text as a code block whose fence is longer than every run of backticks in it, so
// the block never closes early.
func fenced(info, text string) []string {
	longest := 0
	for _, run := range backticks.FindAllString(text, -1) {
		longest = max(longest, len(run))
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return []string{fence + info, strings.TrimSuffix(text, "\n"), fence}
}

// headerValue is a header value on one line: a value with a control character, such as a file name
// with a newline, is quoted, so it can never add a field.
func headerValue(v string) string {
	if strings.ContainsFunc(v, unicode.IsControl) {
		return strconv.Quote(v)
	}
	return v
}

// prose is reviewer or step text inside a list line: on one line, without a code fence or a
// footer tag, and capped.
func prose(s string, n int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " ")
	s = strings.ReplaceAll(s, "```", "'''")
	s = strings.ReplaceAll(s, "<sub>", "&lt;sub&gt;")
	return capRunes(strings.TrimSpace(s), n)
}

func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + " …"
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./@%+=:,-]+$`)

// shellWord is s as one word for a POSIX shell.
func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
