package dispatch

import (
	"reflect"
	"slices"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// idleSession is the session of the run idleInput keeps.
const idleSession = "11111111-2222-4333-8444-555555555555"

// idleInput is issue 7 in Controller with no next step: its one run (start:7, idleSession) closed
// as a handoff, no marker, no PR and no protocol comment, so newIssue finds start:7 spent and no
// other rule fires. ticks is the idle count the previous evaluations left for issue 7.
func idleInput(t *testing.T, ticks int) Input {
	t.Helper()
	in := loadInput(t, "usage-resume")
	r := &in.Repos[0]
	r.Markers = nil
	r.History[0].Outcome, r.History[0].SessionID = runs.Handoff, idleSession
	r.IdleTicks = map[int]int{7: ticks}
	return in
}

// started is the 🤖 Gestart of idleSession.
var started = Comment{ID: 61, Author: "lh-bot", CreatedAt: "2026-10-31T10:00:00Z",
	Body: "🤖 Gestart\nsession: " + idleSession + "\ncommand: /lighthouse-start-issue 7\n\n<sub>session " + idleSession + "</sub>"}

func TestIdleBelowLimit(t *testing.T) {
	for _, ticks := range []int{0, 1} {
		if got := summary(Decide(idleInput(t, ticks))); !reflect.DeepEqual(got, []string{"idle::"}) {
			t.Errorf("ticks %d: Decide = %q, want [idle::]", ticks, got)
		}
	}
}

func TestIdleAtLimitReturns(t *testing.T) {
	tests := []struct {
		name     string
		comments []Comment
		language string
		want     []string
	}{
		{"no protocol comment", nil, "nl", []string{"return:reason_no_rule:geen"}},
		{"no protocol comment en", nil, "en", []string{"return:reason_no_rule:none"}},
		{"last protocol comment", []Comment{started, {ID: 62, Author: "owner", Body: "Nog iets?"}}, "nl",
			[]string{"return:reason_no_rule:🤖 Gestart"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := idleInput(t, 2)
			in.Repos[0].Language = tt.language
			item7(&in).Comments = tt.comments
			if got := summary(Decide(in)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decide = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every evaluation counts, the current one too: from no count the third evaluation in a row
// gives the issue back.
func TestIdleThreeEvaluationsReturn(t *testing.T) {
	var got []string
	ticks := 0
	for range 3 {
		out := summary(Decide(idleInput(t, ticks)))
		got = append(got, out...)
		if slices.Equal(out, []string{"idle::"}) {
			ticks++
		}
	}
	want := []string{"idle::", "idle::", "return:reason_no_rule:geen"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("three evaluations = %q, want %q", got, want)
	}
}

// A run the usage guard parked waits for its marker, also when the marker's reset is still
// ahead, its file is unreadable or a node's markers could not be read: that is a pause, no stall.
func TestIdleLeavesPauseAlone(t *testing.T) {
	marker := park.Marker{SessionID: idleSession, ResetAt: "2026-10-31T13:00:00Z", CreatedAt: "2026-10-31T11:00:00Z",
		Source: "tool", Controller: true}
	tests := []struct {
		name   string
		mutate func(*RepoInput)
	}{
		{"marker not due", func(r *RepoInput) { r.History[0].Outcome, r.Markers = runs.Parked, []park.Marker{marker} }},
		{"marker unreadable", func(r *RepoInput) { r.History[0].Outcome, r.Unreadable = runs.Parked, []string{idleSession} }},
		{"markers incomplete", func(r *RepoInput) { r.MarkersIncomplete = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := idleInput(t, 2)
			tt.mutate(&in.Repos[0])
			if got := summary(Decide(in)); !reflect.DeepEqual(got, []string{}) {
				t.Errorf("Decide = %q, want []", got)
			}
		})
	}
}

// A start that placement defers is a next step: the defer stands, no idle and no return.
func TestIdleLeavesBusyWorkplaceAlone(t *testing.T) {
	in := idleInput(t, 2)
	r := &in.Repos[0]
	r.History = nil
	r.LocationPrefix, r.Controller.Locations = "loc:", map[string]string{"mac": "mac"}
	item7(&in).Labels = []string{"loc:pi"}
	if got := summary(Decide(in)); !reflect.DeepEqual(got, []string{"defer:locatie pi onbekend:"}) {
		t.Errorf("Decide = %q, want the placement defer", got)
	}
}

// An open PR waits for green and its merge, also without an end handoff.
func TestIdleLeavesOpenPRAlone(t *testing.T) {
	in := idleInput(t, 2)
	addPR(&in, forge.PR{Number: 12, State: "OPEN", HeadRef: "issue-7-bouw-de-keten"})
	if got := summary(Decide(in)); !reflect.DeepEqual(got, []string{}) {
		t.Errorf("Decide = %q, want []", got)
	}
}

// A PR merged after the end handoff waits for nothing once its close check ran (closecheck:12
// spent, done) and the issue stayed open: closeCheck declined, so idle counts it. Only an open
// PR keeps idle away.
func TestIdleEndHandoffMergedPRCloseCheckSpent(t *testing.T) {
	tests := []struct {
		name  string
		ticks int
		want  []string
	}{
		{"below limit", 0, []string{"idle::"}},
		{"at limit", 2, []string{"return:reason_no_rule:🤖 Handoff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := loadInput(t, "handoff-end-pr-merged-after")
			in.Repos[0].IdleTicks = map[int]int{7: tt.ticks}
			if got := summary(Decide(in)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decide = %q, want %q", got, tt.want)
			}
		})
	}
}

// A run that closes in this tick still had the issue: the next evaluation judges what it left.
// settle closes the run as done on its posted Done, and no start rule follows it. Without its PR
// nothing else holds the issue.
func TestIdleLeavesOpenRunAlone(t *testing.T) {
	in := loadInput(t, "settle-done-posted")
	in.Repos[0].IdleTicks = map[int]int{7: 2}
	item7(&in).PRs = nil
	if got := summary(Decide(in)); !reflect.DeepEqual(got, []string{"close::"}) {
		t.Errorf("Decide = %q, want [close::]", got)
	}
}

// A Question without an answer waits for a human, the pusher's too.
func TestIdleLeavesOpenQuestionAlone(t *testing.T) {
	tests := []struct {
		name     string
		question Comment
	}{
		{"session question", Comment{ID: 63, Author: "lh-bot", CreatedAt: "2026-10-31T11:00:00Z",
			Body: "🤖 Vraag\n\n**Vraag 1.** Links of rechts?\n\n<sub>session " + idleSession + " · issue #7</sub>"}},
		{"pusher question", Comment{ID: 70, Author: "lh-bot", CreatedAt: "2026-10-31T11:10:00Z",
			Body: "🤖 Vraag\nsha: " + pushSHA + "\nworktree: " + pushWorktree + "\nbranch: issue-7-bouw-de-keten\n\nRode stap: actionlint"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := idleInput(t, 2)
			item7(&in).Comments = []Comment{started, tt.question}
			if got := summary(Decide(in)); !reflect.DeepEqual(got, []string{}) {
				t.Errorf("Decide = %q, want []", got)
			}
		})
	}
}

// An issue the tick already gave back that is in Controller again without a new protocol comment
// is not given back again: its Returned stands, so no idle count and no second Returned. A
// blocked_external label keeps finishReturn out, which is where the loop would start.
func TestIdleAfterReturnNoLoop(t *testing.T) {
	in := idleInput(t, 2)
	in.Repos[0].Labels = testLabels
	item7(&in).Labels = []string{"blocked_external"}
	item7(&in).Comments = []Comment{started, {ID: 64, Author: "lh-bot", CreatedAt: "2026-10-31T11:00:00Z",
		Body: "🤖 Teruggegeven\nreason: geen vervolg na 3 evaluaties; laatste protocol-comment: 🤖 Gestart"}}
	if got := summary(Decide(in)); !reflect.DeepEqual(got, []string{}) {
		t.Errorf("Decide = %q, want []", got)
	}
}
