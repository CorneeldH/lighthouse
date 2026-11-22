package mapplanned

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/protocol"
)

const jobLogin = "lh-bot"

var placeQuestion = Question{
	Issue:       7,
	Topic:       TopicPlace,
	Text:        "Waar hoort #7 op de kaart?",
	Options:     []string{"onder intake", "nieuw onderdeel op de kaart", "niet op de kaart"},
	Recommended: "a",
	Off:         "c",
}

var wishQuestion = Question{
	Issue:       7,
	Topic:       TopicWish,
	Text:        "Willen we #7?",
	Options:     []string{"ja, op de kaart", "nee, niet op de kaart"},
	Recommended: "a",
	Off:         "b",
}

// trustedLogins trusts only owner and maintainer; any other login is untrusted.
func trustedLogins(_ context.Context, login string) (bool, error) {
	return login == "owner" || login == "maintainer", nil
}

func jobComment(body string) protocol.Comment {
	return protocol.Comment{Author: jobLogin, Body: body}
}

// Classification: pure function. Fails if a job comment starts to parse as a protocol header
// (it would hide a Handoff in the chain) or counts as the answer to an open 🤖 Vraag.
func TestJobCommentsAreNoProtocolComments(t *testing.T) {
	for name, body := range map[string]string{
		"question": RenderQuestion(placeQuestion, "nl"),
		"decision": RenderDecision(7, "nl"),
	} {
		if _, ok := protocol.ParseHeader(body); ok {
			t.Errorf("%s: ParseHeader accepted %q", name, body)
		}
		if protocol.IsAnswer(body) {
			t.Errorf("%s: IsAnswer accepted %q", name, body)
		}
	}
}

// Classification: pure function. Fails if the heading, the machine line or the option letters
// change.
func TestRenderQuestionCarriesTheMachineLine(t *testing.T) {
	nl := RenderQuestion(placeQuestion, "nl")
	if !strings.HasPrefix(nl, "🗺 Kaart · map-planned\n") {
		t.Errorf("nl heading: %q", nl)
	}
	for _, want := range []string{
		"<!-- lighthouse:map-planned kind=question topic=place off=c -->",
		"Waar hoort #7 op de kaart?",
		"- **a)** onder intake (aanbevolen)",
		"- **b)** nieuw onderdeel op de kaart\n",
		"- **c)** niet op de kaart\n",
	} {
		if !strings.Contains(nl, want) {
			t.Errorf("nl body lacks %q:\n%s", want, nl)
		}
	}
	en := RenderQuestion(wishQuestion, "en")
	for _, want := range []string{
		"🗺 Map · map-planned\n",
		"<!-- lighthouse:map-planned kind=question topic=wish off=b -->",
		"- **a)** ja, op de kaart (recommended)",
	} {
		if !strings.Contains(en, want) {
			t.Errorf("en body lacks %q:\n%s", want, en)
		}
	}
}

// Classification: pure function. Fails if a decision loses its heading, machine line or the
// issue number in its text.
func TestRenderDecision(t *testing.T) {
	nl := RenderDecision(7, "nl")
	for _, want := range []string{
		"🗺 Kaart · map-planned\n<!-- lighthouse:map-planned kind=decision topic=place -->",
		"#7 krijgt geen item op de kaart",
	} {
		if !strings.Contains(nl, want) {
			t.Errorf("nl decision lacks %q:\n%s", want, nl)
		}
	}
	if en := RenderDecision(7, "en"); !strings.Contains(en, "🗺 Map · map-planned") || !strings.Contains(en, "#7 gets no item on the map") {
		t.Errorf("en decision: %q", en)
	}
}

// Classification: pure function. Fails if StatusOf misreads which job comment is the latest,
// whether an answer follows it, or whether its letter settles the issue.
func TestStatusOf(t *testing.T) {
	question := jobComment(RenderQuestion(placeQuestion, "nl"))
	decision := jobComment(RenderDecision(7, "nl"))
	owner := func(body string) protocol.Comment { return protocol.Comment{Author: "owner", Body: body} }
	tests := []struct {
		name       string
		cs         []protocol.Comment
		wantStatus Status
		wantAnswer Answer
	}{
		{"no comments", nil, StatusNone, Answer{}},
		{"no job comment", []protocol.Comment{owner("c")}, StatusNone, Answer{}},
		{"answer before the question", []protocol.Comment{owner("c"), question}, StatusOpen, Answer{Topic: TopicPlace}},
		{"only a question", []protocol.Comment{question}, StatusOpen, Answer{Topic: TopicPlace}},
		{"off letter", []protocol.Comment{question, owner("c")}, StatusSettled,
			Answer{Topic: TopicPlace, Letter: "c", Text: "c"}},
		{"place letter with a note", []protocol.Comment{question, owner("b, onder intake")}, StatusAnswered,
			Answer{Topic: TopicPlace, Letter: "b", Text: "b, onder intake"}},
		{"letter with a full stop", []protocol.Comment{question, owner(" a. ")}, StatusAnswered,
			Answer{Topic: TopicPlace, Letter: "a", Text: "a."}},
		{"free answer", []protocol.Comment{question, owner("Onder intake graag")}, StatusAnswered,
			Answer{Topic: TopicPlace, Text: "Onder intake graag"}},
		{"word starting with a letter", []protocol.Comment{question, owner("cool, onder intake")}, StatusAnswered,
			Answer{Topic: TopicPlace, Text: "cool, onder intake"}},
		{"letter beyond f", []protocol.Comment{question, owner("g")}, StatusAnswered,
			Answer{Topic: TopicPlace, Text: "g"}},
		{"first answer counts", []protocol.Comment{question, owner("a"), owner("c")}, StatusAnswered,
			Answer{Topic: TopicPlace, Letter: "a", Text: "a"}},
		{"protocol comment is no answer", []protocol.Comment{question, owner("🤖 Gestart\nsession: x")}, StatusOpen,
			Answer{Topic: TopicPlace}},
		{"decision", []protocol.Comment{decision}, StatusSettled, Answer{Topic: TopicPlace}},
		{"question after a decision", []protocol.Comment{decision, question}, StatusOpen, Answer{Topic: TopicPlace}},
		{"decision after an answered question", []protocol.Comment{question, owner("a"), decision}, StatusSettled,
			Answer{Topic: TopicPlace}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, answer, err := StatusOf(context.Background(), tt.cs, jobLogin, trustedLogins)
			if err != nil {
				t.Fatal(err)
			}
			if status != tt.wantStatus || answer != tt.wantAnswer {
				t.Errorf("got %v %+v, want %v %+v", status, answer, tt.wantStatus, tt.wantAnswer)
			}
		})
	}
}

// Classification: pure function (Review Focus 2). Fails if an answer from a login without an
// answer role settles or reopens the issue.
func TestStatusOfIgnoresUntrustedAnswers(t *testing.T) {
	cs := []protocol.Comment{
		jobComment(RenderQuestion(placeQuestion, "nl")),
		{Author: "stranger", Body: "c"},
	}
	status, answer, err := StatusOf(context.Background(), cs, jobLogin, trustedLogins)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusOpen || answer != (Answer{Topic: TopicPlace}) {
		t.Errorf("got %v %+v, want StatusOpen with no answer", status, answer)
	}
}

// Classification: pure function. Fails if StatusOf stops passing on an error of trusted.
func TestStatusOfPassesOnATrustError(t *testing.T) {
	boom := errors.New("boom")
	cs := []protocol.Comment{jobComment(RenderQuestion(placeQuestion, "nl")), {Author: "owner", Body: "a"}}
	_, _, err := StatusOf(context.Background(), cs, jobLogin,
		func(context.Context, string) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

// Classification: pure function. Fails if a well-formed machine line from another author than
// the job's login counts as a job comment.
func TestStatusOfIgnoresAForgedJobComment(t *testing.T) {
	cs := []protocol.Comment{{Author: "owner", Body: RenderDecision(7, "nl")}}
	status, _, err := StatusOf(context.Background(), cs, jobLogin, trustedLogins)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusNone {
		t.Errorf("got %v, want StatusNone", status)
	}
}

// Classification: pure function. Fails if a wish answer reads its off letter wrong: yes takes
// the issue back, no settles it.
func TestStatusOfAWishAnswer(t *testing.T) {
	tests := []struct {
		answer     string
		wantStatus Status
	}{
		{"a", StatusAnswered},
		{"b", StatusSettled},
	}
	for _, tt := range tests {
		t.Run(tt.answer, func(t *testing.T) {
			cs := []protocol.Comment{jobComment(RenderQuestion(wishQuestion, "nl")), {Author: "maintainer", Body: tt.answer}}
			status, answer, err := StatusOf(context.Background(), cs, jobLogin, trustedLogins)
			if err != nil {
				t.Fatal(err)
			}
			want := Answer{Topic: TopicWish, Letter: tt.answer, Text: tt.answer}
			if status != tt.wantStatus || answer != want {
				t.Errorf("got %v %+v, want %v %+v", status, answer, tt.wantStatus, want)
			}
		})
	}
}
