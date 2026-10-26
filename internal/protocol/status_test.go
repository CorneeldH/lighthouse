package protocol

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	type c = Comment
	tests := []struct {
		name     string
		comments []Comment
		want     State
	}{
		{"no header", []c{{Body: "hallo"}}, StateNone},
		{"question unanswered", []c{{Body: "🤖 Vraag"}}, StateQuestion},
		{"question answered", []c{{Body: "🤖 Vraag"}, {Body: "1a"}}, StateOther},
		{"answer before question", []c{{Body: "1a"}, {Body: "🤖 Vraag"}}, StateQuestion},
		{"answer with rocket", []c{{Body: "🤖 Vraag"}, {Body: "1a", Rockets: 1}}, StateQuestion},
		{"artefact after question", []c{{Body: "🤖 Vraag"}, {Body: "<!-- lighthouse:plan -->\nplan"}}, StateQuestion},
		{"legacy artefact after question", []c{{Body: "🤖 Vraag"}, {Body: "<!-- paia:plan -->"}}, StateQuestion},
		{"rebased after question", []c{{Body: "🤖 Vraag"}, {Body: "🤖 Herbaseerd"}}, StateQuestion},
		{"returned", []c{{Body: "🤖 Teruggegeven\nreden: x"}}, StateReturned},
		{"returned then started", []c{{Body: "🤖 Teruggegeven"}, {Body: "🤖 Gestart"}}, StateOther},
		{"unreadable header after question", []c{{Body: "🤖 Vraag"}, {Body: "🤖 Onbekend"}}, StateQuestion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.comments).State; got != tt.want {
				t.Fatalf("State = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyCarriesLastHeader(t *testing.T) {
	got := Classify([]Comment{{Body: "🤖 Gestart"}, {Body: "🤖 Teruggegeven\nreden: x", URL: "u"}, {Body: "🤖 Herbaseerd"}})
	if got.State != StateReturned || got.Comment.URL != "u" || got.Header.Fields["reason"] != "x" {
		t.Fatalf("got %+v", got)
	}
	q := Classify([]Comment{{Body: "🤖 Vraag\n\ntekst"}})
	if q.Comment.Body != "🤖 Vraag\n\ntekst" {
		t.Fatalf("question body = %q", q.Comment.Body)
	}
}

const questionIn = "🤖 Vraag · fase plan\n\n  **Vraag:** Welke?\n\n- **a)** dit\n\n**Zelf draaien:**\n```bash\n  echo  x\n\n```\n\n<sub>session …</sub>\n"

const questionOut = "**Vraag:** Welke?\n- **a)** dit\n**Zelf draaien:**\n```bash\n  echo  x\n\n```"

func TestQuestionText(t *testing.T) {
	inputs := map[string]string{
		"lf":   questionIn,
		"crlf": strings.ReplaceAll(questionIn, "\n", "\r\n"),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			if got := QuestionText(in); got != questionOut {
				t.Fatalf("got %q, want %q", got, questionOut)
			}
		})
	}
}
