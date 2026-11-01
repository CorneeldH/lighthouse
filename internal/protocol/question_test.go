package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type questionCase struct {
	Name     string          `json:"name"`
	Meta     QuestionMeta    `json:"meta"`
	Fields   QuestionFields  `json:"fields"`
	Language string          `json:"language"`
	Body     string          `json:"body"`
	Parsed   *QuestionFields `json:"parsed"`
}

// The fixture is shared with tests/pack/test_protocol.py: both twins must agree.
func TestQuestionSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/protocol/questions.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []questionCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := RenderQuestion(c.Meta, c.Fields, c.Language); got != c.Body {
				t.Fatalf("RenderQuestion =\n%s\nwant\n%s", got, c.Body)
			}
			want := c.Fields
			if c.Parsed != nil {
				want = *c.Parsed
			}
			if got := CleanFields(c.Fields); !reflect.DeepEqual(got, CleanFields(want)) {
				t.Errorf("CleanFields = %+v, want %+v", got, want)
			}
			got, ok := ParseQuestion(c.Body)
			if !ok {
				t.Fatal("ParseQuestion found no question")
			}
			if !reflect.DeepEqual(got, CleanFields(want)) {
				t.Errorf("ParseQuestion = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseQuestionNotAQuestion(t *testing.T) {
	for _, body := range []string{"", "plain", "🤖 Handoff\nissue: #1", "🤖 Klaar"} {
		if got, ok := ParseQuestion(body); ok {
			t.Errorf("ParseQuestion(%q) = %+v, want false", body, got)
		}
	}
}

// A command that looks like an option or holds backticks must come back literally.
func TestParseQuestionKeepsCommandsLiteral(t *testing.T) {
	f := QuestionFields{
		Phase: "plan", Where: "w", What: "x",
		Commands:  []string{"- **a)** not an option", "echo ````x````"},
		Questions: []Question{{Question: "q?", Options: []Option{{Text: "o"}}}},
	}
	got, ok := ParseQuestion(RenderQuestion(QuestionMeta{Issue: 1, Session: "s"}, f, "en"))
	if !ok || !reflect.DeepEqual(got, CleanFields(f)) {
		t.Fatalf("round trip = %+v (%v), want %+v", got, ok, CleanFields(f))
	}
}

func TestAnswerPromptDropsFooterAndClose(t *testing.T) {
	for _, language := range []string{"nl", "en"} {
		meta := QuestionMeta{Issue: 3, Title: "T", IssueURL: "u", Session: "s", Worktree: "w"}
		fields := QuestionFields{Phase: "plan", Where: "here", What: "there", Questions: []Question{
			{Question: "one?", Options: []Option{{Text: "x", Recommended: true}}},
			{Question: "two?", Options: []Option{{Text: "y"}}},
		}}
		posted := RenderQuestion(meta, fields, language)
		got := AnswerPrompt(posted, "  1a, 2a \n")
		if strings.Contains(got, "<sub>") {
			t.Errorf("%s: footer left in %q", language, got)
		}
		for _, drop := range append(Variants("question_close"), Variants("question_bundle_hint")...) {
			if strings.Contains(got, drop) {
				t.Errorf("%s: %q left in %q", language, drop, got)
			}
		}
		for _, keep := range []string{"**" + Heading("question_label", language) + " 1:** one?", "- **1a)** x"} {
			if !strings.Contains(got, keep) {
				t.Errorf("%s: %q missing from %q", language, keep, got)
			}
		}
		if !strings.HasSuffix(got, "\n\nThe user's answer:\n\n1a, 2a") {
			t.Errorf("%s: answer not literal at the end: %q", language, got)
		}
		if !strings.HasPrefix(got, "Your question stands on the issue like this; the controller added the option letters:\n\n"+Robot) {
			t.Errorf("%s: prefix wrong: %q", language, got)
		}
	}
}

// The block runs to the LAST close line: a close line quoted in free text does not cut it short.
func TestParseQuestionRunsToLastClose(t *testing.T) {
	f := QuestionFields{Phase: "p", Where: "w", What: "before\n" + Heading("question_close", "en") + "\nafter",
		Questions: []Question{{Question: "q?", Options: []Option{{Text: "o"}}}}}
	got, ok := ParseQuestion(RenderQuestion(QuestionMeta{Issue: 1, Session: "s"}, f, "en"))
	if !ok || !reflect.DeepEqual(got, CleanFields(f)) || got.Questions[0].Question != "q?" {
		t.Fatalf("got %+v (%v), want %+v", got, ok, CleanFields(f))
	}
}

func TestRenderQuestionLimits(t *testing.T) {
	var f QuestionFields
	for i := 0; i < MaxQuestions+1; i++ {
		q := Question{Question: "q?"}
		for j := 0; j < len(Letters)+1; j++ {
			q.Options = append(q.Options, Option{Text: "o"})
		}
		f.Questions = append(f.Questions, q)
	}
	body := RenderQuestion(QuestionMeta{Issue: 1, Session: "s"}, f, "en")
	if strings.Contains(body, "5:**") || strings.Contains(body, "1g)") {
		t.Errorf("limits not applied:\n%s", body)
	}
	got, _ := ParseQuestion(body)
	if len(got.Questions) != MaxQuestions || len(got.Questions[0].Options) != len(Letters) {
		t.Errorf("parsed %d questions, %d options", len(got.Questions), len(got.Questions[0].Options))
	}
}
