package mapplanned

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

// Topic is what a question of the job asks about.
type Topic string

const (
	// TopicPlace asks where on the map an issue goes.
	TopicPlace Topic = "place"
	// TopicWish asks whether the issue is wanted at all.
	TopicWish Topic = "wish"
)

// Question is one question of the job on an issue.
type Question struct {
	Issue   int
	Topic   Topic
	Text    string
	Options []string // 2..6, letters a..f in order
	// Recommended is the letter of the recommended option.
	Recommended string
	// Off is the letter of "not on the map". A wish question has two options, a) yes, on the
	// map, b) no, not on the map, with Off "b".
	Off string
}

// Status is where the job's comments leave an issue.
type Status int

const (
	// StatusNone: no comment of the job.
	StatusNone Status = iota
	// StatusOpen: the latest job comment is a question nobody trusted has answered.
	StatusOpen
	// StatusAnswered: a trusted answer to the latest question, other than its Off letter.
	StatusAnswered
	// StatusSettled: the latest job comment is a decision, or its question was answered Off.
	StatusSettled
)

// Answer is the topic of the latest job question and the trusted answer to it, if any.
// Letter is empty for a free answer.
type Answer struct {
	Topic        Topic
	Letter, Text string
}

const (
	kindQuestion = "question"
	kindDecision = "decision"
)

// texts are the language-dependent parts of a job comment, in nl and in any other language.
type texts struct {
	heading, recommended, close, decision string
}

var textsNL = texts{
	heading:     "🗺 Kaart · map-planned",
	recommended: "(aanbevolen)",
	close:       "Antwoord met de letter, eventueel gevolgd door een toelichting.",
	decision:    "Beslissing: #%d krijgt geen item op de kaart.",
}

var textsEN = texts{
	heading:     "🗺 Map · map-planned",
	recommended: "(recommended)",
	close:       "Answer with the letter, optionally followed by a note.",
	decision:    "Decision: #%d gets no item on the map.",
}

func textsFor(language string) texts {
	if language == "nl" {
		return textsNL
	}
	return textsEN
}

// RenderQuestion writes the comment body of q: the heading, the machine line, the text and
// the lettered options, in language ("nl" or other).
func RenderQuestion(q Question, language string) string {
	t := textsFor(language)
	lines := []string{
		t.heading,
		fmt.Sprintf("%skind=%s topic=%s off=%s -->", protocol.MapPlannedMarker, kindQuestion, q.Topic, q.Off),
		"",
		q.Text,
		"",
	}
	for i, option := range q.Options[:min(len(q.Options), len(protocol.Letters))] {
		letter := string(protocol.Letters[i])
		line := "- **" + letter + ")** " + option
		if letter == q.Recommended {
			line += " " + t.recommended
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", t.close)
	return strings.Join(lines, "\n")
}

// RenderDecision writes the comment body of the decision that issue gets no item on the map.
func RenderDecision(issue int, language string) string {
	t := textsFor(language)
	return strings.Join([]string{
		t.heading,
		fmt.Sprintf("%skind=%s topic=%s -->", protocol.MapPlannedMarker, kindDecision, TopicPlace),
		"",
		fmt.Sprintf(t.decision, issue),
	}, "\n")
}

// machineLine is the parsed machine line of a job comment.
type machineLine struct {
	kind, off string
	topic     Topic
}

// parseMachineLine returns the fields of the first machine line in body.
func parseMachineLine(body string) (machineLine, bool) {
	for line := range strings.SplitSeq(body, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), protocol.MapPlannedMarker)
		if !ok {
			continue
		}
		rest, ok = strings.CutSuffix(rest, "-->")
		if !ok {
			continue
		}
		var m machineLine
		for field := range strings.FieldsSeq(rest) {
			key, value, _ := strings.Cut(field, "=")
			switch key {
			case "kind":
				m.kind = value
			case "topic":
				m.topic = Topic(value)
			case "off":
				m.off = value
			}
		}
		if m.kind == kindQuestion || m.kind == kindDecision {
			return m, true
		}
	}
	return machineLine{}, false
}

// answerLetter returns the option letter an answer starts with: a lowercase a-f followed by
// the end, a space or punctuation; "" for a free answer.
func answerLetter(text string) string {
	r, size := utf8.DecodeRuneInString(text)
	if r == utf8.RuneError || !strings.ContainsRune(protocol.Letters, r) {
		return ""
	}
	if next, _ := utf8.DecodeRuneInString(text[size:]); next != utf8.RuneError && !unicode.IsSpace(next) && !unicode.IsPunct(next) {
		return ""
	}
	return string(r)
}

// StatusOf reads the comments of one issue, oldest first: the latest question or decision of
// the job (a comment with the machine line whose Author is jobLogin, the login of the forge
// token; the machine line of any other author is ignored), and the answer to a question: of
// the comments after it by a trusted login that are not job comments, the first that starts
// with an option letter, or else the latest free one. A decision, or an answer whose letter is
// Off, is settled.
func StatusOf(ctx context.Context, cs []protocol.Comment, jobLogin string, trusted func(context.Context, string) (bool, error)) (Status, Answer, error) {
	var last *machineLine
	var lettered, free *Answer
	for _, c := range cs {
		if m, ok := parseMachineLine(c.Body); ok {
			if c.Author == jobLogin {
				last, lettered, free = &m, nil, nil
			}
			continue
		}
		if last == nil || last.kind != kindQuestion || lettered != nil || !protocol.IsAnswer(c.Body) {
			continue
		}
		ok, err := trusted(ctx, c.Author)
		if err != nil {
			return StatusNone, Answer{}, fmt.Errorf("trust of %s: %w", c.Author, err)
		}
		if !ok {
			continue
		}
		text := strings.TrimSpace(c.Body)
		a := &Answer{Topic: last.topic, Letter: answerLetter(text), Text: text}
		if a.Letter != "" {
			lettered = a
		} else {
			free = a
		}
	}
	answer := lettered
	if answer == nil {
		answer = free
	}
	switch {
	case last == nil:
		return StatusNone, Answer{}, nil
	case last.kind == kindDecision:
		return StatusSettled, Answer{Topic: last.topic}, nil
	case answer == nil:
		return StatusOpen, Answer{Topic: last.topic}, nil
	case answer.Letter != "" && answer.Letter == last.off:
		return StatusSettled, *answer, nil
	default:
		return StatusAnswered, *answer, nil
	}
}
