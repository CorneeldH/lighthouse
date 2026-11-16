package dispatch

import (
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/runs"
)

// withClosingRepo adds repo o/s of done-no-handoff to in: issue 7 with open run 5, which settle
// closes in this tick as a return (reason_no_handoff). row adjusts run 5 first.
func withClosingRepo(t *testing.T, in *Input, row func(*runs.Row)) {
	t.Helper()
	src := loadInput(t, "done-no-handoff")
	repo := src.Repos[0]
	repo.Snapshot.Repo = "o/s"
	row(&repo.History[0])
	in.Repos = append(in.Repos, repo)
}

// withWaitingRepo adds repo o/t of brake-slots to in: issue 7 in Controller without a run, which
// wants a start. The brake of brake-slots is not used.
func withWaitingRepo(t *testing.T, in *Input) {
	t.Helper()
	src := loadInput(t, "brake-slots")
	repo := src.Repos[0]
	repo.Snapshot.Repo = "o/t"
	in.Repos = append(in.Repos, repo)
}

func kinds(got []Action) []Kind {
	out := []Kind{}
	for _, a := range got {
		out = append(out, a.Kind)
	}
	return out
}

// A run that settle closes in this tick gives its slot to a start of another issue in the same
// tick (#359): on the host, for the software limit and on its node. A run on a node outside Nodes,
// or a run that is not software, frees nothing the waiting start needs. Leaving frees, node or
// software unset on the close in decideIssue fails the matching case.
func TestClosingRunFreesSlotSameTick(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		row    func(*runs.Row)
		want   []Kind
		reason string // the defer text, when one is wanted
	}{
		{"host", "brake-slots", func(*runs.Row) {}, []Kind{KindReturn, KindStart}, ""},
		{"software", "brake-software", func(r *runs.Row) { r.Profile = ProfileSoftware },
			[]Kind{KindReturn, KindStart}, ""},
		{"node", "brake-node-full", func(r *runs.Row) { r.Node = "node-a" },
			[]Kind{KindReturn, KindStart, KindStart}, ""},
		{"node outside Nodes", "brake-slots", func(r *runs.Row) { r.Node = "node-x" },
			[]Kind{KindReturn, KindDefer}, "slots: 4 of 4 in use"},
		{"not software", "brake-software", func(r *runs.Row) { r.Profile = "" },
			[]Kind{KindReturn, KindDefer}, "software: 2 of 2 in use"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := loadInput(t, c.base)
			withClosingRepo(t, &in, c.row)
			got := Decide(in)
			if k := kinds(got); !reflect.DeepEqual(k, c.want) {
				t.Fatalf("kinds = %v, want %v: %+v", k, c.want, got)
			}
			if got[0].Repo != "o/s" || got[0].RunID != 5 {
				t.Errorf("the return must close run 5 of o/s: %+v", got[0])
			}
			for _, a := range got[1:] {
				if a.Kind == KindDefer && a.Reason != c.reason {
					t.Errorf("defer reason %q, want %q", a.Reason, c.reason)
				}
			}
		})
	}
}

// Which closes free the slot of their run (spec table "Welke sluiting"): a parked run stays
// counted until its usage resume, a merge GitHub refuses leaves the run open, and a return
// without a run closes none.
func TestFreesSlot(t *testing.T) {
	cases := []struct {
		name string
		a    Action
		want bool
	}{
		{"close parked", Action{Kind: KindClose, Outcome: runs.Parked}, false},
		{"close done", Action{Kind: KindClose, Outcome: runs.Done}, true},
		{"close handoff", Action{Kind: KindClose, Outcome: runs.Handoff}, true},
		{"close question", Action{Kind: KindClose, Outcome: runs.Question}, true},
		{"close start failed", Action{Kind: KindClose, Outcome: runs.StartFailed}, true},
		{"return with run", Action{Kind: KindReturn, RunID: 5}, true},
		{"return without run", Action{Kind: KindReturn}, false},
		{"done", Action{Kind: KindDone, RunID: 5}, true},
		{"merge", Action{Kind: KindMerge, RunID: 5}, false},
	}
	for _, c := range cases {
		if got := freesSlot(c.a); got != c.want {
			t.Errorf("%s: freesSlot = %v, want %v", c.name, got, c.want)
		}
	}
}

// The fix-pr close (rule 13) frees nothing: its fix-pr start inherits the slot (holdsSlot), so a
// waiting start of another issue still defers on full slots. Marking the fix-pr close as frees
// gives o/t#7 a second slot and fails this test.
func TestFixPRCloseFreesNothing(t *testing.T) {
	in := loadInput(t, "settle-green-pr-done")
	red(&in)
	in.Brake = openBrake()
	in.Brake.InUse = 4
	withWaitingRepo(t, &in)
	want := []string{fixClose12, "defer:slots: 4 of 4 in use:", "start:reason_pr_red:"}
	if got := summary(Decide(in)); !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
