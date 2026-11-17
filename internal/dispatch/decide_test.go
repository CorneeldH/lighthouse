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

// Once the pusher pushed, the push row owns the PR of its branch: a green PR merges, a red one
// gets a fix-pr start, a merged one closes the row as done. max_runtime, max_wait and the push
// timeout no longer hold, so a pending PR keeps the row open however old it is.
func TestPushRowAfterPushOwnsPR(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{"green merges", func(*Input) {}, []string{"merge::"}},
		{"red starts fix-pr", red, []string{fixClose12, "start:reason_pr_red:"}},
		{"merged closes done", func(in *Input) {
			pr12(in).State, pr12(in).MergedAt = "MERGED", in.Now.Add(-time.Minute)
		}, []string{"close::"}},
		{"pending stays open", func(in *Input) { pr12(in).Checks = "PENDING" }, []string{}},
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
				if a.Kind == KindStart || a.Kind == KindDefer {
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
			item7(in).Comments = append(item7(in).Comments, returned(80, "2026-10-31T11:11:00Z"))
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
// from the pusher's Vraag the answer replies to: the answer itself carries none of them.
func TestPushAnswerRowRetryKeepsWorktreeAndPermit(t *testing.T) {
	in := pushQuestion(t, "push")
	item7(&in).Comments[2].Rocket = true
	row := failedPush(9, "pushanswer:71", time.Date(2026, 10, 31, 11, 31, 0, 0, time.UTC))
	row.Trigger, row.Branch = "answer", ""
	addRows(&in, row)
	got := Decide(in)
	want := []Action{{Kind: KindPush, Repo: "o/r", Issue: 7, Trigger: "pushanswer", TriggerKey: "pushanswer:71", Attempt: 2,
		Command: "push-workflows", Worktree: pushWorktree, Branch: "issue-7-bouw-de-keten", CommentID: 71,
		PermitSHA: pushSHA, Language: "nl"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}
