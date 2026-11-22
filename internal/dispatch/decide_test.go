package dispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/runs"
)

// fixture is one file of testdata/decide: an input and the actions Decide must return.
type fixture struct {
	Input Input    `json:"input"`
	Want  []Action `json:"want"`
}

// decideFixtures reads every file of testdata/decide, by name without .json.
func decideFixtures(t *testing.T) map[string]fixture {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "decide", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixtures in testdata/decide")
	}
	out := make(map[string]fixture, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f fixture
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if f.Want == nil {
			t.Fatalf("%s has no want", path)
		}
		out[strings.TrimSuffix(filepath.Base(path), ".json")] = f
	}
	return out
}

func TestDecideFixtures(t *testing.T) {
	for name, f := range decideFixtures(t) {
		t.Run(name, func(t *testing.T) {
			got := Decide(f.Input)
			if !reflect.DeepEqual(got, f.Want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(f.Want, "", "  ")
				t.Errorf("Decide:\ngot  %s\nwant %s", gotJSON, wantJSON)
			}
		})
	}
}

// Apply posts Reason and Skip through protocol.Heading, which panics on an unknown key. Every
// key a fixture expects must be in the headings table, in nl and en. A defer carries the
// literal night, which apply never looks up.
func TestDecideKeysAreHeadings(t *testing.T) {
	keys := map[string]bool{}
	for _, f := range decideFixtures(t) {
		for _, a := range f.Want {
			if a.Reason != "" && a.Kind != KindDefer {
				keys[a.Reason] = true
			}
			if a.Skip != "" {
				keys[a.Skip] = true
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no Reason or Skip in any fixture")
	}
	for key := range keys {
		for _, language := range []string{"nl", "en"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Heading(%q, %q) panics: %v", key, language, r)
					}
				}()
				if protocol.Heading(key, language) == "" {
					t.Errorf("Heading(%q, %q) is empty", key, language)
				}
			}()
		}
	}
}

// A handoff branch reaches git as an argument: only a plain ref name passes.
func TestValidBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"issue-7-bouw-de-keten", true},
		{"feature/a.b_c-1", true},
		{"", false},
		{"-x", false},
		{"--upload-pack=x", false},
		{"a..b", false},
		{"a b", false},
		{"a;b", false},
		{"$(x)", false},
		{"a`b`", false},
		{"a\nb", false},
	}
	for _, tt := range tests {
		if got := validBranch(tt.branch); got != tt.want {
			t.Errorf("validBranch(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

// fixPRRuns counts the fix-pr runs of one PR towards the ceiling: an open run counts, a failed
// start does not, nor a run of another PR or another issue.
func TestFixPRRuns(t *testing.T) {
	fix := func(issue int, command string, open bool, o runs.Outcome) runs.Row {
		return runs.Row{Issue: issue, Command: command, Open: open, Outcome: o}
	}
	tests := []struct {
		name    string
		history []runs.Row
		want    int
	}{
		{"none", nil, 0},
		{"closed handoff and done", []runs.Row{
			fix(7, "/lighthouse-fix-pr 12", false, runs.Handoff), fix(7, "/lighthouse-fix-pr 12", false, runs.Done),
		}, 2},
		{"open row counts", []runs.Row{
			fix(7, "/lighthouse-fix-pr 12", false, runs.Returned), fix(7, "/lighthouse-fix-pr 12", true, ""),
		}, 2},
		{"start_failed does not count", []runs.Row{
			fix(7, "/lighthouse-fix-pr 12", false, runs.StartFailed), fix(7, "/lighthouse-fix-pr 12", false, runs.Done),
		}, 1},
		{"other PR does not count", []runs.Row{
			fix(7, "/lighthouse-fix-pr 13", false, runs.Done), fix(7, "/lighthouse-fix-pr 120", false, runs.Done),
		}, 0},
		{"other issue does not count", []runs.Row{fix(8, "/lighthouse-fix-pr 12", false, runs.Done)}, 0},
		{"other command does not count", []runs.Row{fix(7, "/lighthouse-start-issue 7", false, runs.Done)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixPRRuns(tt.history, 7, 12); got != tt.want {
				t.Errorf("fixPRRuns = %d, want %d", got, tt.want)
			}
		})
	}
}

// Only a fix-pr start of the same repo and issue keeps a fix-pr close a handoff; any other
// close passes untouched.
func TestSettleUnstartedFixPR(t *testing.T) {
	const session = "11111111-2222-4333-8444-555555555555"
	fixClose := Action{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff,
		SessionID: session, ReasonArg: "fixpr:12:abc123", Language: "nl"}
	returned := Action{Kind: KindReturn, Repo: "o/r", Issue: 7, RunID: 5, SessionID: session,
		Reason: "reason_pr_red", Language: "nl"}
	fixStart := Action{Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: "fixpr", TriggerKey: "fixpr:12:abc123"}
	tests := []struct {
		name string
		in   []Action
		want []Action
	}{
		{"own fix-pr start keeps the close", []Action{fixClose, fixStart}, []Action{fixClose, fixStart}},
		{"no start returns", []Action{fixClose}, []Action{returned}},
		{"other trigger returns",
			[]Action{fixClose, {Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: "handoff", TriggerKey: "handoff:50"}},
			[]Action{returned, {Kind: KindStart, Repo: "o/r", Issue: 7, Trigger: "handoff", TriggerKey: "handoff:50"}}},
		{"fix-pr start of another issue returns",
			[]Action{fixClose, {Kind: KindStart, Repo: "o/r", Issue: 8, Trigger: "fixpr"}},
			[]Action{returned, {Kind: KindStart, Repo: "o/r", Issue: 8, Trigger: "fixpr"}}},
		{"fix-pr start of another repo returns",
			[]Action{fixClose, {Kind: KindStart, Repo: "o/s", Issue: 7, Trigger: "fixpr"}},
			[]Action{returned, {Kind: KindStart, Repo: "o/s", Issue: 7, Trigger: "fixpr"}}},
		{"plain handoff close passes",
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff}},
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff}}},
		{"start_failed close with a fixpr arg passes",
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.StartFailed, ReasonArg: "fixpr:x"}},
			[]Action{{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.StartFailed, ReasonArg: "fixpr:x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps []planned
			for _, a := range tt.in {
				ps = append(ps, planned{Action: a})
			}
			var got []Action
			for _, p := range settleUnstartedFixPR(ps, nil) {
				got = append(got, p.Action)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// A fix-pr close without a planned start hands the issue back with the cause of the fix-pr
// start, not always "PR red".
func TestSettleUnstartedFixPRCarriesCause(t *testing.T) {
	fixClose := Action{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff,
		ReasonArg: "fixpr:12:abc123", Language: "nl"}
	ps := []planned{{Action: fixClose, fixReason: "reason_merge_failed", fixReasonArg: "x"}}
	got := settleUnstartedFixPR(ps, nil)
	want := Action{Kind: KindReturn, Repo: "o/r", Issue: 7, RunID: 5, Reason: "reason_merge_failed",
		ReasonArg: "x", Language: "nl"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Action, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// A fix-pr start decideIssue planned that the brake deferred or dropped leaves its run open:
// the close is dropped, no Returned follows, and the defer stays.
func TestSettleUnstartedFixPRKeepsRunOpenWhenBraked(t *testing.T) {
	fixClose := Action{Kind: KindClose, Repo: "o/r", Issue: 7, RunID: 5, Outcome: runs.Handoff,
		ReasonArg: "fixpr:12:abc123", Language: "nl"}
	deferred := Action{Kind: KindDefer, Repo: "o/r", Issue: 7, Reason: "slots: 4 of 4 in use"}
	other := Action{Kind: KindClose, Repo: "o/r", Issue: 8, RunID: 6, Outcome: runs.Handoff, ReasonArg: "fixpr:3:def"}
	wanted := map[issueKey]bool{{"o/r", 7}: true}
	ps := []planned{{Action: fixClose}, {Action: deferred}, {Action: other}}
	var got []Action
	for _, p := range settleUnstartedFixPR(ps, wanted) {
		got = append(got, p.Action)
	}
	want := []Action{deferred, {Kind: KindReturn, Repo: "o/r", Issue: 8, RunID: 6, Reason: "reason_pr_red"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const (
	pushWorktree = "/home/u/r/.claude/worktrees/issue-7-bouw-de-keten"
	pushSession  = "11111111-2222-4333-8444-555555555555"
	pushSHA      = "3f2a1bcd4e5f60718293a4b5c6d7e8f901234567"
)

// pushHandoff is the base of the push tests: handoff-start with comment 63 replaced by a
// `next: push-workflows` handoff of run 5, and the pusher configured.
func pushHandoff(t *testing.T, fields string) Input {
	t.Helper()
	in := loadInput(t, "handoff-start")
	item7(&in).Comments[1].Body = "🤖 Handoff\nissue: #7\nworktree: " + pushWorktree +
		"\nbranch: issue-7-bouw-de-keten\nnext: push-workflows\n" + fields +
		"\n\n<sub>session " + pushSession + "</sub>"
	in.Repos[0].WorkflowPush = true
	in.Repos[0].Installed["lighthouse-write-pr"] = true
	in.Repos[0].PushTimeout = 30 * time.Minute
	return in
}

// pushQuestion is a pusher 🤖 Vraag (no session, a sha field) followed by the answer.
func pushQuestion(t *testing.T, answer string) Input {
	t.Helper()
	in := pushHandoff(t, "when: now")
	item7(&in).Comments = append(item7(&in).Comments[:1],
		Comment{ID: 70, Author: "lh-bot", CreatedAt: "2026-10-31T11:10:00Z",
			Body: "🤖 Vraag\nsha: " + pushSHA + "\nworktree: " + pushWorktree + "\nbranch: issue-7-bouw-de-keten\n\n" +
				"Rode stap: actionlint\n\ncd " + pushWorktree + " && git push -u origin HEAD"},
		Comment{ID: 71, Author: "owner", CreatedAt: "2026-10-31T11:30:00Z", Body: answer})
	return in
}

func TestDecidePushHandoff(t *testing.T) {
	const pushCommand = "cd " + pushWorktree + " && git push -u origin HEAD"
	tests := []struct {
		name   string
		mutate func(*Input)
		fields string
		want   []string
	}{
		{"configured", func(*Input) {}, "when: now", []string{"push::"}},
		{"no config", func(in *Input) { in.Repos[0].WorkflowPush = false }, "when: now",
			[]string{"return:reason_pusher_off:" + pushCommand}},
		{"run on another node", func(in *Input) { row5(in).Node = "n1" }, "when: now",
			[]string{"return:reason_pusher_node:" + pushCommand}},
		{"run of an unknown session", func(in *Input) { in.Repos[0].History = nil }, "when: now", []string{"push::"}},
		{"after that starts", func(*Input) {}, "after: /lighthouse-write-pr 7\nwhen: now", []string{"push::"}},
		{"after that is rejected", func(*Input) {}, "after: rm -rf /\nwhen: now", []string{"return:reason_bad_handoff:rm -rf /"}},
		{"after that is a push", func(*Input) {}, "after: push-workflows\nwhen: now",
			[]string{"return:reason_bad_handoff:push-workflows"}},
		{"push to the default branch", func(in *Input) {
			item7(in).Comments[1].Body = strings.Replace(item7(in).Comments[1].Body, "branch: issue-7-bouw-de-keten", "branch: main", 1)
		}, "when: now", []string{"return:reason_bad_handoff:branch main"}},
		{"night outside the window", func(in *Input) { in.Night = "22:00-06:00" }, "when: night", []string{"defer:night:"}},
		{"active blocker parks", func(in *Input) {
			item7(in).BlockedBy = []forge.Blocker{{Repo: "o/r", Issue: 3, State: "OPEN"}}
		}, "when: now", []string{"park::"}},
		{"trigger spent", func(in *Input) {
			item7(in).Comments[1].Rocket = true
			addRows(in, runs.Row{ID: 9, Issue: 7, TriggerKey: "push:63", Command: "push-workflows", Outcome: runs.Handoff})
		}, "when: now", []string{}},
		{"handoff trigger spent", func(in *Input) {
			item7(in).Comments[1].Rocket = true
			addRows(in, runs.Row{ID: 9, Issue: 7, TriggerKey: "handoff:63", Command: "push-workflows", Outcome: runs.Handoff})
		}, "when: now", []string{}},
		{"paused brake", func(in *Input) { in.Brake = &Brake{Paused: true} }, "when: now", []string{}},
		{"usage brake closed", func(in *Input) { in.Brake = &Brake{} }, "when: now", []string{"defer:usage: not measured:"}},
		{"slots full does not close a push", func(in *Input) {
			in.Brake = &Brake{UsageMeasured: true, HaltPctSession: 100, HaltPctWeek: 100, Slots: 1, InUse: 1}
		}, "when: now", []string{"push::"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := pushHandoff(t, tt.fields)
			tt.mutate(&in)
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
		})
	}
}

func TestDecidePushActionFields(t *testing.T) {
	got := Decide(pushHandoff(t, "when: now"))
	if len(got) != 1 {
		t.Fatalf("got %d actions, want 1: %+v", len(got), got)
	}
	want := Action{Kind: KindPush, Repo: "o/r", Issue: 7, Trigger: "push", TriggerKey: "push:63", Attempt: 1,
		Command: "push-workflows", Worktree: pushWorktree, Branch: "issue-7-bouw-de-keten", CommentID: 63, Language: "nl"}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got  %+v\nwant %+v", got[0], want)
	}
}

// A push row whose handoff lost its 🚀 gets it back, like any handoff trigger.
func TestDecidePushRocketRepair(t *testing.T) {
	in := pushHandoff(t, "when: now")
	addRows(&in, runs.Row{ID: 9, Issue: 7, TriggerKey: "push:63", Command: "push-workflows", Outcome: runs.Handoff})
	got := Decide(in)
	if s := summary(got); !reflect.DeepEqual(s, []string{"rocket::"}) || got[0].CommentID != 63 {
		t.Errorf("got %+v, want a rocket on 63", got)
	}
}

// A push row that failed to start has no session to retry: retry must not start the literal
// push-workflows command.
func TestDecidePushRowIsNoSessionRetry(t *testing.T) {
	in := pushHandoff(t, "when: now")
	addRows(&in, runs.Row{ID: 9, Issue: 7, TriggerKey: "push:63", Command: "push-workflows", Outcome: runs.StartFailed})
	for _, a := range Decide(in) {
		if a.Kind == KindStart || a.Kind == KindResume {
			t.Errorf("got %+v, want no session start for a push row", a)
		}
	}
}

func TestDecidePushAnswer(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		mutate func(*Input)
		want   []string
	}{
		{"push", "push", func(*Input) {}, []string{"push::"}},
		{"push with case and a full stop", " Push. ", func(*Input) {}, []string{"push::"}},
		{"other answer", "nee, laat maar", func(*Input) {}, []string{}},
		{"no config", "push", func(in *Input) { in.Repos[0].WorkflowPush = false },
			[]string{"return:reason_pusher_off:cd " + pushWorktree + " && git push -u origin HEAD"}},
		{"answer spent", "push", func(in *Input) {
			item7(in).Comments[2].Rocket = true
			addRows(in, runs.Row{ID: 9, Issue: 7, TriggerKey: "pushanswer:71", Command: "push-workflows", Outcome: runs.Handoff})
		}, []string{}},
		{"unreadable sha", "push", func(in *Input) {
			c := &item7(in).Comments[1]
			c.Body = strings.Replace(c.Body, pushSHA, "not-a-sha", 1)
		}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := pushQuestion(t, tt.answer)
			tt.mutate(&in)
			// The mutation of "unreadable sha" targets the Vraag, which is Comments[1] here.
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
		})
	}
}

// Red when protocol.IsAnswer (used by Classify and answerTo) takes a map-planned job comment
// after an open 🤖 Vraag for the human's answer: the question would read as answered and the
// session would resume on the job's text.
func TestAJobCommentDoesNotAnswerAProtocolQuestion(t *testing.T) {
	const jobComment = "🗺 Kaart · map-planned\n" +
		"<!-- lighthouse:map-planned kind=question topic=place off=c -->\n\n" +
		"Waar hoort #7 op de kaart?\n\n- **a)** onder intake\n- **b)** nieuw onderdeel op de kaart\n- **c)** niet op de kaart"
	tests := []struct {
		name       string
		jobComment string
		wantState  protocol.State
		wantResume bool
	}{
		{"job comment", jobComment, protocol.StateQuestion, false},
		// The same position with a human answer resumes: the setup can produce a resume.
		{"human answer", "Links.", protocol.StateOther, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := decideFixtures(t)["answer-resume"].Input
			// Keep Gestart (62) and the Vraag (63); the only comment after the Vraag is ours.
			item7(&in).Comments = append(item7(&in).Comments[:2],
				Comment{ID: 64, Author: "lh-bot", CreatedAt: "2026-10-31T11:10:00Z", Body: tt.jobComment})
			if st := protocol.Classify(item7(&in).ProtocolComments()).State; st != tt.wantState {
				t.Errorf("Classify state = %v, want %v", st, tt.wantState)
			}
			resumed := false
			for _, a := range Decide(in) {
				resumed = resumed || a.Kind == KindResume
			}
			if resumed != tt.wantResume {
				t.Errorf("resume planned = %v, want %v", resumed, tt.wantResume)
			}
		})
	}
}

func TestDecidePushAnswerFields(t *testing.T) {
	got := Decide(pushQuestion(t, "push"))
	if len(got) != 1 {
		t.Fatalf("got %d actions, want 1: %+v", len(got), got)
	}
	want := Action{Kind: KindPush, Repo: "o/r", Issue: 7, Trigger: "pushanswer", TriggerKey: "pushanswer:71", Attempt: 1,
		Command: "push-workflows", Worktree: pushWorktree, Branch: "issue-7-bouw-de-keten", CommentID: 71,
		PermitSHA: pushSHA, Language: "nl"}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got  %+v\nwant %+v", got[0], want)
	}
}

// pushRow is the base of the push-row tests: settle-green-pr-done with run 5 turned into an
// open push row on the branch of PR #12, started age before now, without a session or a stand,
// pushed at pushed ("" while the push phase runs); run 4 is the closed run of the Gestart that
// handed over to it. PR #12 is green; the pusher is configured.
func pushRow(t *testing.T, pushed string, age time.Duration) Input {
	t.Helper()
	in := loadInput(t, "settle-green-pr-done")
	prev := *row5(&in)
	prev.ID, prev.Open, prev.Outcome = 4, false, runs.Handoff
	r := row5(&in)
	r.SessionID, r.Command, r.Prompt, r.TriggerKey, r.PushedSHA = "", "push-workflows", "", "push:63", pushed
	r.StartedAt = in.Now.Add(-age)
	in.Repos[0].Stands = nil
	in.Repos[0].WorkflowPush, in.Repos[0].PushTimeout = true, 30*time.Minute
	addRows(&in, prev)
	return in
}

// measuredNoSession makes the session rules ready to fire on run 5 if they were asked: max_wait
// and max_runtime long past, and the run measured without a workplace session.
func measuredNoSession(in *Input) {
	in.MaxWait, in.MaxRuntime = Duration(5*time.Minute), Duration(10*time.Minute)
	in.Repos[0].Runs = map[int64]RunState{5: {}}
}

// An open push row without a pushed SHA goes back at the push timeout as "pusher did not
// respond", and before it nothing closes it: no merge of the green PR, no rule on a session,
// transcript or workplace. A timeout of zero is the default of 30 minutes.
func TestPushRowTimesOutWithoutMarker(t *testing.T) {
	tests := []struct {
		name   string
		age    time.Duration
		mutate func(*Input)
		want   []string
	}{
		{"past the timeout", 31 * time.Minute, func(*Input) {}, []string{"return:reason_pusher_silent:"}},
		{"before the timeout", 29 * time.Minute, func(*Input) {}, []string{}},
		{"session rules do not hold", 29 * time.Minute, measuredNoSession, []string{}},
		{"zero timeout is the default, before", 29 * time.Minute, func(in *Input) { in.Repos[0].PushTimeout = 0 }, []string{}},
		{"zero timeout is the default, past", 31 * time.Minute, func(in *Input) { in.Repos[0].PushTimeout = 0 },
			[]string{"return:reason_pusher_silent:"}},
		{"own timeout", 11 * time.Minute, func(in *Input) { in.Repos[0].PushTimeout = 10 * time.Minute },
			[]string{"return:reason_pusher_silent:"}},
		{"the pusher's Vraag closes it", 31 * time.Minute, func(in *Input) {
			item7(in).Comments = append(item7(in).Comments, Comment{ID: 70, Author: "lh-bot", CreatedAt: "2026-10-31T11:50:00Z",
				Body: "🤖 Vraag\nsha: " + pushSHA + "\nworktree: " + pushWorktree + "\nbranch: issue-7-bouw-de-keten\n\nRode stap: actionlint"})
		}, []string{"close::"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := pushRow(t, "", tt.age)
			tt.mutate(&in)
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
			for _, a := range got {
				if a.RunID != 5 || a.Stop || a.End || a.Discard || a.SessionID != "" {
					t.Errorf("action %+v must close run 5 without touching a session", a)
				}
				if a.Kind == KindClose && a.Outcome != runs.Question {
					t.Errorf("close outcome = %q, want question", a.Outcome)
				}
			}
		})
	}
}

// pusherReturned is the pusher's 🤖 Teruggegeven for a red step without a SHA, posted at.
func pusherReturned(at time.Time) Comment {
	return Comment{ID: 70, Author: "lh-bot", CreatedAt: at.Format(time.RFC3339),
		Body: "🤖 Teruggegeven\nreason: workflow-pusher rood: policy\nworktree: " + pushWorktree +
			"\nbranch: issue-7-bouw-de-keten\nstep: policy\n\n**Rode stap:** policy"}
}

// The pusher's own Returned closes its open push row as returned, before the push timeout, when
// the pusher died between the post and the close: without a session to stop and without a
// workplace to discard. The tick after it gives the issue back, labelled by reason_pusher_red.
func TestPushRowClosesOnThePushersReturned(t *testing.T) {
	in := pushRow(t, "", 5*time.Minute)
	item7(&in).Comments = append(item7(&in).Comments, pusherReturned(in.Now.Add(-time.Minute)))
	got := Decide(in)
	if s := summary(got); !reflect.DeepEqual(s, []string{"close::"}) {
		t.Fatalf("summary = %q, want one close", s)
	}
	if a := got[0]; a.RunID != 5 || a.Outcome != runs.Returned || a.Stop || a.End || a.Discard || a.SessionID != "" {
		t.Errorf("action %+v, want run 5 closed as returned without touching a session or workplace", a)
	}

	for i := range in.Repos[0].History {
		if r := &in.Repos[0].History[i]; r.ID == 5 {
			r.Open, r.Outcome = false, runs.Returned
		}
	}
	if s := summary(Decide(in)); !reflect.DeepEqual(s, []string{"finish_return:reason_pusher_red:"}) {
		t.Errorf("summary after the close = %q, want the give-back of reason_pusher_red", s)
	}
}

// Once the pusher pushed, the push row owns the PR of its branch: a green PR merges through the
// merge pusher (the row's PushedSHA routes it there, with or without a push note), a red one
// gets a fix-pr start, a merged one closes the row as done. max_runtime, max_wait and the push
// timeout no longer hold, so a pending PR keeps the row open however old it is.
func TestPushRowAfterPushOwnsPR(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{"green merges through the merge pusher", func(*Input) {}, []string{"close::merge:12:abc123", "push::"}},
		{"red starts fix-pr", red, []string{fixClose12, "start:reason_pr_red:"}},
		{"merged closes done", func(in *Input) {
			pr12(in).State, pr12(in).MergedAt = "MERGED", in.Now.Add(-time.Minute)
		}, []string{"close::"}},
		{"pending stays open", func(in *Input) { pr12(in).Checks = "PENDING" }, []string{}},
		{"PR closed unmerged goes back", func(in *Input) {
			pr12(in).State, pr12(in).ClosedAt = "CLOSED", in.Now.Add(-time.Minute)
		}, []string{"return:reason_pr_closed:"}},
		{"issue closed by hand closes done", func(in *Input) {
			pr12(in).Checks = "PENDING"
			item7(in).State = "CLOSED"
		}, []string{"close::"}},
		{"no PR of the branch at the timeout goes back", func(in *Input) { item7(in).PRs = nil },
			[]string{"return:reason_pusher_no_handoff:" + pushSHA}},
		{"another branch's pending PR does not count", func(in *Input) { pr12(in).HeadRef, pr12(in).Checks = "issue-7-ander", "PENDING" },
			[]string{"return:reason_pusher_no_handoff:" + pushSHA}},
		{"red with full slots defers the fix-pr start", func(in *Input) {
			red(in)
			in.Brake = openBrake()
			in.Brake.InUse = 4
		}, []string{"defer:slots: 4 of 4 in use:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := pushRow(t, pushSHA, 2*time.Hour)
			measuredNoSession(&in)
			tt.mutate(&in)
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
			for _, a := range got {
				if a.Kind == KindStart || a.Kind == KindDefer || a.Kind == KindPush {
					continue
				}
				if a.RunID != 5 || a.Stop || a.End || a.Discard {
					t.Errorf("action %+v must close run 5 without touching a session", a)
				}
				if a.Kind == KindClose && a.ReasonArg == "" && a.Outcome != runs.Done {
					t.Errorf("close outcome = %q, want done", a.Outcome)
				}
			}
		})
	}
}

// The close of a push row frees no slot: the row was never counted (CountRun), so a waiting
// start of another repo still defers on full slots. Freeing it gives o/t#7 a slot nobody held.
// A pushed row without a PR waits for the push timeout from its start before it goes back.
func TestPushedRowWithoutPRWaitsForTheTimeout(t *testing.T) {
	in := pushRow(t, pushSHA, 29*time.Minute)
	item7(&in).PRs = nil
	if got := summary(Decide(in)); len(got) != 0 {
		t.Fatalf("summary = %q before the timeout, want nothing", got)
	}
}

func TestPushRowCloseFreesNoSlot(t *testing.T) {
	in := pushRow(t, "", 31*time.Minute)
	in.Brake = openBrake()
	in.Brake.InUse = 4
	withWaitingRepo(t, &in)
	want := []string{"return:reason_pusher_silent:", "defer:slots: 4 of 4 in use:"}
	if got := summary(Decide(in)); !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

// failedPush is a push row of trigger key that failed to start at started.
func failedPush(id int64, key string, started time.Time) runs.Row {
	return runs.Row{ID: id, Issue: 7, Trigger: "handoff", TriggerKey: key, Attempt: 1, Command: "push-workflows",
		Mode: "start", Branch: "issue-7-bouw-de-keten", StartedAt: started, Outcome: runs.StartFailed,
		Reason: "exec: lighthouse: not found"}
}

// A push that failed to start starts the pusher once more with the worktree and branch of its
// handoff; after the second failure the issue goes back with the push command, once.
func TestPushStartFailedGetsOneRetryThenQuestion(t *testing.T) {
	const back = "return:reason_start_failed:exec: lighthouse: not found; cd " + pushWorktree + " && git push -u origin HEAD"
	t1 := time.Date(2026, 10, 31, 11, 5, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 31, 11, 10, 0, 0, time.UTC)
	returned := func(id int64, at string) Comment {
		return Comment{ID: id, Author: "lh-bot", CreatedAt: at, Body: "🤖 Teruggegeven\n\nstart mislukt"}
	}
	// given is a Returned that stands with its give-back done: the label is on the issue and the
	// board says To do (a needs_* label on Controller would get a todo).
	given := func(in *Input, at string) {
		item7(in).Comments = append(item7(in).Comments, returned(80, at))
		item7(in).Labels, in.Repos[0].Labels = []string{"needs_hands"}, testLabels
		item7(in).Status = "To do"
	}
	tests := []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{"one failure retries", func(in *Input) { addRows(in, failedPush(9, "push:63", t1)) }, []string{"push::"}},
		{"two failures go back", func(in *Input) {
			addRows(in, failedPush(9, "push:63", t1), failedPush(10, "push:63", t2))
		}, []string{back}},
		{"a Returned after the second failure stands", func(in *Input) {
			addRows(in, failedPush(9, "push:63", t1), failedPush(10, "push:63", t2))
			given(in, "2026-10-31T11:11:00Z")
		}, []string{}},
		{"one failure without the pusher goes back", func(in *Input) {
			addRows(in, failedPush(9, "push:63", t1))
			in.Repos[0].WorkflowPush = false
		}, []string{"return:reason_pusher_off:cd " + pushWorktree + " && git push -u origin HEAD"}},
		{"one failure on an unreadable handoff goes back", func(in *Input) {
			addRows(in, failedPush(9, "push:63", t1))
			item7(in).Comments[1].Body = "🤖 Handoff\nkapot"
		}, []string{"return:reason_start_failed:exec: lighthouse: not found"}},
		{"a Returned after one failure stands", func(in *Input) {
			addRows(in, failedPush(9, "push:63", t1))
			in.Repos[0].WorkflowPush = false
			given(in, "2026-10-31T11:06:00Z")
		}, []string{}},
		{"an older Returned does not count", func(in *Input) {
			addRows(in, failedPush(9, "push:63", t1), failedPush(10, "push:63", t2))
			item7(in).Comments = append(item7(in).Comments, returned(80, "2026-10-31T11:06:00Z"))
		}, []string{back}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := pushHandoff(t, "when: now")
			item7(&in).Comments[1].Rocket = true
			tt.mutate(&in)
			got := Decide(in)
			if s := summary(got); !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("summary = %q, want %q", s, tt.want)
			}
			if len(got) == 1 && got[0].Kind == KindPush {
				want := Action{Kind: KindPush, Repo: "o/r", Issue: 7, Trigger: "push", TriggerKey: "push:63", Attempt: 2,
					Command: "push-workflows", Worktree: pushWorktree, Branch: "issue-7-bouw-de-keten", CommentID: 63, Language: "nl"}
				if !reflect.DeepEqual(got[0], want) {
					t.Errorf("got  %+v\nwant %+v", got[0], want)
				}
			}
		})
	}
}

// The second attempt of an answer `push` takes the worktree, the branch and the permitted SHA
// from the pusher's Vraag the answer replies to: the answer itself carries none of them. A later
// pusher Vraag, on another SHA, is not the one the user answered: the permit waives the review,
// so it must never move to a SHA the user did not see.
func TestPushAnswerRowRetryKeepsWorktreeAndPermit(t *testing.T) {
	const laterSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name   string
		mutate func(*Input)
	}{
		{"the Vraag before the answer", func(*Input) {}},
		{"a later Vraag does not count", func(in *Input) {
			item7(in).Comments = append(item7(in).Comments, Comment{ID: 72, Author: "lh-bot", CreatedAt: "2026-10-31T11:40:00Z",
				Body: "🤖 Vraag\nsha: " + laterSHA + "\nworktree: /elders\nbranch: issue-7-ander\n\nRode stap: zizmor"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := pushQuestion(t, "push")
			item7(&in).Comments[2].Rocket = true
			row := failedPush(9, "pushanswer:71", time.Date(2026, 10, 31, 11, 31, 0, 0, time.UTC))
			row.Trigger, row.Branch = "answer", ""
			addRows(&in, row)
			tt.mutate(&in)
			got := Decide(in)
			want := []Action{{Kind: KindPush, Repo: "o/r", Issue: 7, Trigger: "pushanswer", TriggerKey: "pushanswer:71",
				Attempt: 2, Command: "push-workflows", Worktree: pushWorktree, Branch: "issue-7-bouw-de-keten", CommentID: 71,
				PermitSHA: pushSHA, Language: "nl"}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

// Every reason_* heading has the label of the spec table; a reason added to headings.toml without
// a row here turns the test red, so its label is a decision. A dynamic key such as a
// cause+"_ceiling" that no heading names falls to needs_hands.
func TestReturnLabelKeyTable(t *testing.T) {
	want := map[string]string{
		"reason_needs_brainstorm": "needs_shaping", "reason_interactive": "needs_decision",
		"reason_skill_missing": "needs_hands", "reason_not_allowed": "needs_hands", "reason_bad_handoff": "needs_hands",
		"reason_state_lost": "needs_hands", "reason_start_failed": "needs_hands", "reason_own_work": "needs_hands",
		"reason_workplace_gone": "needs_hands", "reason_pr_closed": "needs_hands", "reason_pr_draft": "needs_hands",
		"reason_pr_red": "needs_hands", "reason_pr_red_ceiling": "needs_hands", "reason_pr_conflict": "needs_hands",
		"reason_pr_conflict_ceiling": "needs_hands", "reason_pr_roborev": "needs_hands",
		"reason_pr_roborev_ceiling": "needs_hands", "reason_merge_failed": "needs_hands",
		"reason_merge_failed_ceiling": "needs_hands", "reason_no_handoff": "needs_hands",
		"reason_post_failed": "needs_hands", "reason_post_failed_unreadable": "needs_hands",
		"reason_start_uncertain": "needs_hands", "reason_permission": "needs_hands", "reason_silent": "needs_hands",
		"reason_silent_forge": "needs_hands", "reason_timeout": "needs_hands", "reason_pusher_off": "needs_hands",
		"reason_pusher_node": "needs_hands", "reason_pusher_silent": "needs_hands",
		"reason_pusher_no_handoff": "needs_hands", "reason_pusher_red": "needs_hands", "reason_repeat_handoff": "needs_hands",
	}
	var headings map[string]map[string]string
	if _, err := toml.DecodeFile(filepath.Join("..", "..", "pack", "core", "headings.toml"), &headings); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for key := range headings {
		if !strings.HasPrefix(key, "reason_") {
			continue
		}
		seen++
		label, ok := want[key]
		if !ok {
			t.Errorf("%s has no row in the label table", key)
			continue
		}
		if got := ReturnLabelKey(key, false); got != label {
			t.Errorf("ReturnLabelKey(%q, false) = %q, want %q", key, got, label)
		}
	}
	if seen != len(want) {
		t.Errorf("headings.toml has %d reason keys, the table %d", seen, len(want))
	}
	if got := ReturnLabelKey("reason_pr_red", true); got != "blocked_issue" {
		t.Errorf("ReturnLabelKey(reason_pr_red, blocked) = %q, want blocked_issue", got)
	}
	if got := ReturnLabelKey("", false); got != "needs_hands" {
		t.Errorf("ReturnLabelKey(\"\", false) = %q, want needs_hands", got)
	}
}

// Rule 13 with an active blocker hands the run back as blocked (label blocked_issue); a fix-pr
// close that started nothing for another cause (the item is On hold) is not.
func TestSettleUnstartedFixPRMarksBlocked(t *testing.T) {
	for name, want := range map[string]bool{"fixpr-blocker": true, "fixpr-on-hold": false} {
		got := Decide(loadInput(t, name))
		if len(got) != 1 || got[0].Kind != KindReturn || got[0].Blocked != want {
			t.Errorf("%s: Decide = %+v, want one return with Blocked %v", name, got, want)
		}
	}
}

// needsHands marks item 7 of in with the needs_hands label and gives the repo its label names.
func needsHands(in *Input) {
	item7(in).Labels, in.Repos[0].Labels = []string{"needs_hands"}, testLabels
}

// A Controller issue with a needs_* label and no open run goes to To do and starts nothing, not
// even the start of a new issue or the fix-pr start of a red PR. Dropping the needs_* check in
// decideIssue gives the start back; dropping KindToDo gives no action.
func TestDecideNeedsLabelNoRunMovesToToDo(t *testing.T) {
	for _, label := range []string{"needs_shaping", "needs_decision", "needs_hands", "needs_testing"} {
		t.Run(label, func(t *testing.T) {
			in := loadInput(t, "new-issue-start")
			item7(&in).Labels, in.Repos[0].Labels = []string{label}, testLabels
			got := Decide(in)
			if len(got) != 1 || got[0].Kind != KindToDo || got[0].ItemID != "I7" || got[0].Issue != 7 || got[0].RunID != 0 ||
				got[0].Stop || got[0].End || got[0].Discard || got[0].Reason != "" {
				t.Fatalf("Decide = %+v, want one bare todo on item I7", got)
			}
		})
	}
}

// A needs_* label with a red PR and a closing run: the fix-pr start is not made. The label
// check before the fix-pr start in decideIssue is what turns this red (the start comes back).
func TestDecideNeedsLabelNoFixPRStart(t *testing.T) {
	in := loadInput(t, "settle-red-pr")
	needsHands(&in)
	for _, a := range Decide(in) {
		if a.Kind == KindStart || a.Kind == KindToDo {
			t.Errorf("action %+v: a needs_hands issue must not start a fix-pr", a)
		}
	}
}

// With an open run the run goes on: no todo, and the settle actions stay as they are. Emitting
// the todo before the run is looked at turns the first case red.
func TestDecideNeedsLabelOpenRunKeepsRunning(t *testing.T) {
	in := loadInput(t, "open-run-blocks-start")
	want := summary(Decide(in))
	needsHands(&in)
	if got := summary(Decide(in)); !reflect.DeepEqual(got, want) {
		t.Errorf("with needs_hands: %q, want the unlabelled %q", got, want)
	}
	for _, a := range Decide(in) {
		if a.Kind == KindToDo {
			t.Errorf("open run: got %+v, want no todo", a)
		}
	}
}

// A Handoff on a needs_* issue starts no run either.
func TestDecideNeedsLabelNoNewStart(t *testing.T) {
	in := loadInput(t, "handoff-start")
	if got := kinds(Decide(in)); !reflect.DeepEqual(got, []Kind{KindStart}) {
		t.Fatalf("unlabelled: %v, want a start (fixture premise)", got)
	}
	item7(&in).Labels, in.Repos[0].Labels = []string{"needs_shaping"}, testLabels
	if got := kinds(Decide(in)); !reflect.DeepEqual(got, []Kind{KindToDo}) {
		t.Errorf("needs_shaping: %v, want one todo and no start", got)
	}
}

// unparkInput is the fixture unpark (To do, own Geparkeerd last, blocker o/r#5 closed without a
// merged PR, no open run) with the label names of the repo.
func unparkInput(t *testing.T) Input {
	t.Helper()
	in := loadInput(t, "unpark")
	in.Repos[0].Labels = testLabels
	return in
}

// A To do item whose last protocol comment is the tick's own Geparkeerd and that has no active
// blocker is unparked: a blocker that closed, and a blocker in another repo that is still open
// but has a merged PR. Dropping the statusToDo branch in decideIssue turns every case red.
func TestDecideUnparksToDoWhenPredecessorGone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blocker forge.Blocker
	}{
		{"closed in this repo", forge.Blocker{Repo: "o/r", Issue: 5, State: "CLOSED"}},
		{"merged PR in another repo", forge.Blocker{Repo: "x/y", Issue: 9, State: "OPEN", MergedPR: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := unparkInput(t)
			item7(&in).BlockedBy = []forge.Blocker{tc.blocker}
			got := Decide(in)
			if len(got) != 1 || got[0].Kind != KindUnpark || got[0].ItemID != "I7" || got[0].Issue != 7 {
				t.Fatalf("Decide = %+v, want one unpark of item I7", got)
			}
		})
	}
	in := unparkInput(t)
	item7(&in).BlockedBy = []forge.Blocker{{Repo: "x/y", Issue: 9, State: "OPEN"}}
	if got := Decide(in); len(got) != 0 {
		t.Errorf("blocker still active: Decide = %+v, want nothing", got)
	}
}

// The unblock lane may take blocked_issue off before the tick looks: the item is unparked all
// the same, and with the label still on as well.
func TestDecideUnparkWithoutLabel(t *testing.T) {
	for _, labels := range [][]string{nil, {"blocked_issue"}} {
		in := unparkInput(t)
		item7(&in).Labels = labels
		if got := kinds(Decide(in)); !reflect.DeepEqual(got, []Kind{KindUnpark}) {
			t.Errorf("labels %q: Decide = %v, want one unpark", labels, got)
		}
	}
}

// A To do item a human holds with a needs_* or blocked_external label is never unparked: the
// todo of Task 4 would move it back, every tick. Dropping heldByHuman in unparkToDo turns this red.
func TestDecideUnparkSkipsNeedsLabel(t *testing.T) {
	for _, label := range []string{"needs_shaping", "needs_decision", "needs_hands", "needs_testing", "blocked_external"} {
		in := unparkInput(t)
		item7(&in).Labels = []string{"blocked_issue", label}
		if got := Decide(in); len(got) != 0 {
			t.Errorf("%s: Decide = %+v, want nothing", label, got)
		}
	}
}

// A To do item without the tick's own Geparkeerd as last protocol comment is left alone, also
// when its blocker is closed: a human's To do item, a Geparkeerd of another author, and an
// issue a human picked up interactively (a Started of a session no run knows). The last one
// shows that neither orphan nor repair runs for a To do item without an open run: orphan would
// hand it back with reason_state_lost.
func TestDecideToDoWithoutOwnParkedUntouched(t *testing.T) {
	started := "🤖 Gestart\nsession: 99999999-2222-4333-8444-555555555555\nworktree: /home/u/r/.claude/worktrees/issue-7-x\n" +
		"command: /lighthouse-start-issue 7\nstart: 2026-10-31T11:40:00Z\nbranch: issue-7-x\nrunner: forge\n"
	for _, tc := range []struct {
		name   string
		mutate func(it *Item)
	}{
		{"no Geparkeerd", func(it *Item) { it.Comments = it.Comments[:2] }},
		{"Geparkeerd of another author", func(it *Item) { it.Comments[2].Author = "owner" }},
		{"foreign Started after the Geparkeerd", func(it *Item) {
			it.Comments = append(it.Comments, Comment{ID: 65, Author: "owner", Body: started, CreatedAt: "2026-10-31T11:40:00Z"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := unparkInput(t)
			tc.mutate(item7(&in))
			if got := Decide(in); len(got) != 0 {
				t.Errorf("Decide = %+v, want nothing", got)
			}
			// The same item in Controller is not left alone: the case is not vacuous.
			item7(&in).Status = statusController
			if got := Decide(in); len(got) == 0 {
				t.Errorf("in Controller: Decide = [], want an action (fixture premise)")
			}
		})
	}
}

// The On hold remnant: an issue the old tick parked on On hold is still unparked to Controller,
// although no code sets On hold any more.
func TestDecideOnHoldRemnantStillUnparks(t *testing.T) {
	in := unparkInput(t)
	item7(&in).Status = statusOnHold
	if got := kinds(Decide(in)); !reflect.DeepEqual(got, []Kind{KindUnpark}) {
		t.Errorf("Decide = %v, want one unpark", got)
	}
}
