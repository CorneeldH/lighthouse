package protocol

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// MaxQuestions is the most questions in one comment; Letters are the option letters.
const (
	MaxQuestions = 4
	Letters      = "abcdef"
)

// Option is one answer a question offers.
type Option struct {
	Text        string `json:"text"`
	Recommended bool   `json:"recommended"`
}

// Question is one question with its options.
type Question struct {
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

// QuestionFields is what the run-session classifies: the content of a `🤖 Question`.
type QuestionFields struct {
	Phase     string     `json:"phase"`
	Task      string     `json:"task"`
	Where     string     `json:"where"`
	What      string     `json:"what"`
	Commands  []string   `json:"commands"`
	Questions []Question `json:"questions"`
}

// QuestionMeta is what the controller adds around the fields.
type QuestionMeta struct {
	Issue      int    `json:"issue"`
	Title      string `json:"title"`
	IssueURL   string `json:"issue_url"`
	Session    string `json:"session"`
	Worktree   string `json:"worktree"`
	HandoffURL string `json:"handoff_url"`
}

// CleanFields is f as RenderQuestion writes it and ParseQuestion reads it back: every text
// cleaned, an empty phase `?`, at most MaxQuestions questions and len(Letters) options each.
// The slices are never nil.
func CleanFields(f QuestionFields) QuestionFields {
	phase := f.Phase
	if phase == "" {
		phase = "?"
	}
	out := QuestionFields{
		Phase:     clean(phase),
		Task:      clean(f.Task),
		Where:     clean(f.Where),
		What:      clean(f.What),
		Commands:  []string{},
		Questions: []Question{},
	}
	for _, c := range f.Commands {
		if strings.TrimSpace(c) != "" {
			out.Commands = append(out.Commands, c)
		}
	}
	for _, q := range f.Questions[:min(len(f.Questions), MaxQuestions)] {
		cq := Question{Question: clean(q.Question), Options: []Option{}}
		for _, o := range q.Options[:min(len(q.Options), len(Letters))] {
			cq.Options = append(cq.Options, Option{Text: clean(o.Text), Recommended: o.Recommended})
		}
		out.Questions = append(out.Questions, cq)
	}
	return out
}

// runYourself is the run-yourself block: each command its own code block, empty without commands.
func runYourself(commands []string, language string) []string {
	var lines []string
	for _, command := range commands {
		if strings.TrimSpace(command) == "" {
			continue
		}
		if lines == nil {
			lines = []string{"", Heading("question_run_yourself", language)}
		}
		lines = append(lines, "")
		lines = append(lines, codeBlock(command)...)
	}
	if lines == nil {
		return nil
	}
	return append(lines, "")
}

// RenderQuestion renders a `🤖 Question`. Several questions make a bundle: numbered, with one
// run-yourself block before question 1.
func RenderQuestion(m QuestionMeta, f QuestionFields, language string) string {
	h := func(key string) string { return Heading(key, language) }
	phase := f.Phase
	if phase == "" {
		phase = "?"
	}
	top := fmt.Sprintf("%s %s · #%d %s ([link](%s)) · %s %s",
		Robot, h("kind_question"), m.Issue, m.Title, m.IssueURL, h("question_phase"), clean(phase))
	if f.Task != "" {
		top += " · " + h("question_task") + " " + clean(f.Task)
	}
	questions := f.Questions[:min(len(f.Questions), MaxQuestions)]
	if len(questions) == 0 {
		questions = []Question{{}}
	}
	bundle := len(questions) > 1
	lines := []string{
		top,
		"",
		"**" + h("question_where") + ":** " + clean(f.Where),
		"**" + h("question_what") + ":** " + clean(f.What),
	}
	// One run-yourself block for the whole comment: with one question below the question
	// line, with a bundle before question 1.
	block := runYourself(f.Commands, language)
	if bundle {
		lines = append(lines, block...)
	}
	for i, q := range questions {
		prefix, label := "", h("question_label")
		if bundle {
			prefix = fmt.Sprint(i + 1)
			label += " " + prefix
			if i > 0 {
				// Without a blank line, question n continues the previous option.
				lines = append(lines, "")
			}
		}
		lines = append(lines, "**"+label+":** "+clean(q.Question))
		if !bundle {
			lines = append(lines, block...)
		}
		for j, o := range q.Options[:min(len(q.Options), len(Letters))] {
			line := "- **" + prefix + string(Letters[j]) + ")** " + clean(o.Text)
			if o.Recommended {
				line += " " + h("question_recommended")
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, "", h("question_close"))
	if bundle {
		lines = append(lines, h("question_bundle_hint"))
	}
	handoff := ""
	if m.HandoffURL != "" {
		handoff = "handoff " + m.HandoffURL
	}
	lines = append(lines, Footer(m.Session, "worktree "+m.Worktree, handoff))
	return strings.Join(lines, "\n")
}

var questionOption = regexp.MustCompile(`^- \*\*[1-4]?[a-f]\)\*\*(?: (.*))?$`)

func alternatives(key string) string {
	var parts []string
	for _, v := range Variants(key) {
		parts = append(parts, regexp.QuoteMeta(v))
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

// fieldLine matches `**<where|what|question label>[ n]:** text`; groups 1 and 2 are where and what.
var questionFieldLine = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^\*\*(?:(` + alternatives("question_where") + `)|(` +
		alternatives("question_what") + `)|` + alternatives("question_label") +
		`(?: [1-4])?):\*\*(?: (.*))?$`)
})

var questionTop = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^.* · ` + alternatives("question_phase") + ` (.*?)(?: · ` +
		alternatives("question_task") + ` (.*))?$`)
})

type piece struct {
	name  string
	parts []string
}

// questionPieces cuts the lines between header and close into pieces per field or option, and
// apart the commands: the content of each code block under the run-yourself line, literally.
// Inside a code block no line starts a new piece, not even one that looks like an option.
func questionPieces(lines []string) ([]piece, []string) {
	runYourselfLines := Variants("question_run_yourself")
	pieces := []piece{}
	commands := []string{}
	inFence := false
	var fence string
	var block []string
	for _, line := range lines {
		if inFence {
			if line == fence {
				commands = append(commands, strings.Join(block, "\n"))
				inFence, block = false, nil
			} else {
				block = append(block, line)
			}
			continue
		}
		open := fenceOpen.FindStringSubmatch(line)
		field := questionFieldLine().FindStringSubmatch(line)
		option := questionOption.FindStringSubmatch(line)
		switch {
		case contains(runYourselfLines, line):
			pieces = append(pieces, piece{name: "commands"})
		case open != nil && len(pieces) > 0 && pieces[len(pieces)-1].name == "commands":
			inFence, fence = true, open[1]
		case field != nil:
			name := "question"
			if field[1] != "" {
				name = "where"
			} else if field[2] != "" {
				name = "what"
			}
			pieces = append(pieces, piece{name: name, parts: []string{field[3]}})
		case option != nil:
			pieces = append(pieces, piece{name: "option", parts: []string{option[1]}})
		case len(pieces) > 0:
			last := &pieces[len(pieces)-1]
			last.parts = append(last.parts, line)
		}
	}
	return pieces, commands
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// addOption adds an option to the last question; without one, to an empty question.
func addOption(questions *[]Question, text string) {
	if len(*questions) == 0 {
		*questions = append(*questions, Question{Options: []Option{}})
	}
	recommended := false
	for _, v := range Variants("question_recommended") {
		if text == v || strings.HasSuffix(text, " "+v) {
			text = strings.TrimSpace(strings.TrimSuffix(text, v))
			recommended = true
			break
		}
	}
	last := &(*questions)[len(*questions)-1]
	last.Options = append(last.Options, Option{Text: text, Recommended: recommended})
}

// ParseQuestion is the inverse of RenderQuestion: the fields as they went in, cleaned. The
// second result is false when the comment is no `🤖 Question`. The block runs from the blank
// line after the header to the LAST close line; a blank line in free text belongs to the
// running piece, only the opening lines of a field or option start a new piece.
func ParseQuestion(body string) (QuestionFields, bool) {
	header, ok := ParseHeader(body)
	if !ok || header.Kind != KindQuestion {
		return QuestionFields{}, false
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	fields := QuestionFields{Commands: []string{}, Questions: []Question{}}
	if top := questionTop().FindStringSubmatch(lines[0]); top != nil {
		fields.Phase, fields.Task = strings.TrimSpace(top[1]), strings.TrimSpace(top[2])
	}
	closeLines := Variants("question_close")
	end := len(lines)
	for i, line := range lines {
		if contains(closeLines, strings.TrimSpace(line)) {
			end = i
		}
	}
	pieces, commands := questionPieces(lines[1:end])
	fields.Commands = commands
	for _, p := range pieces {
		text := strings.TrimSpace(strings.Join(p.parts, "\n"))
		switch p.name {
		case "commands":
		case "option":
			addOption(&fields.Questions, text)
		case "question":
			fields.Questions = append(fields.Questions, Question{Question: text, Options: []Option{}})
		case "where":
			fields.Where = text
		case "what":
			fields.What = text
		}
	}
	return fields, true
}

// AnswerPrompt is the resume prompt: the question as posted, then the answer literally. The
// option letters are in the posted text; footer and close lines are for the human and stay out.
func AnswerPrompt(question, answer string) string {
	forHuman := append(Variants("question_close"), Variants("question_bundle_hint")...)
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(question), "\n") {
		if strings.HasPrefix(line, "<sub>") || contains(forHuman, strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}
	posted := strings.TrimSpace(strings.Join(kept, "\n"))
	return "Your question stands on the issue like this; the controller added the option letters:\n\n" +
		posted + "\n\nThe user's answer:\n\n" + strings.TrimSpace(answer)
}
