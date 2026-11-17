package lane

import (
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

const testKey = "core/skills/x"

func openItem(n int, kind ItemKind, target string) forge.Item {
	return forge.Item{Number: n, IsPR: kind == KindPR, State: "open", Body: Marker(testKey, kind, target), HeadRef: ""}
}

func closedByHuman(n int, kind ItemKind, target string) forge.Item {
	it := openItem(n, kind, target)
	it.State = "closed"
	return it
}

func closedByLane(n int, kind ItemKind, target string) forge.Item {
	it := closedByHuman(n, kind, target)
	it.Body += "\n" + LaneClosedMarker
	return it
}

func behindOut(kind ItemKind, target string) Outcome {
	return Outcome{Kind: kind, Target: target, Measurable: true}
}

var errOut = Outcome{Measurable: false}

func TestDecide_tables(t *testing.T) {
	cases := []struct {
		name  string
		out   Outcome
		found []forge.Item
		want  []Action
	}{
		// Behind track.
		{"open same kind same target: nothing", behindOut(KindConflict, "b"),
			[]forge.Item{openItem(1, KindConflict, "b")}, nil},
		{"open same kind older target: update", behindOut(KindBehind, "0.2"),
			[]forge.Item{openItem(1, KindBehind, "0.1")},
			[]Action{{Op: Update, Item: openItem(1, KindBehind, "0.1"), Kind: KindBehind, Target: "0.2"}}},
		{"open pr older target: update", behindOut(KindPR, "b"),
			[]forge.Item{openItem(1, KindPR, "a")},
			[]Action{{Op: Update, Item: openItem(1, KindPR, "a"), Kind: KindPR, Target: "b"}}},
		{"open pr, outcome conflict: close old, create new", behindOut(KindConflict, "a"),
			[]forge.Item{openItem(1, KindPR, "a")},
			[]Action{{Op: CloseByLane, Item: openItem(1, KindPR, "a")}, {Op: Create, Kind: KindConflict, Target: "a"}}},
		{"open conflict, outcome pr: close old, create new", behindOut(KindPR, "a"),
			[]forge.Item{openItem(1, KindConflict, "a")},
			[]Action{{Op: CloseByLane, Item: openItem(1, KindConflict, "a")}, {Op: Create, Kind: KindPR, Target: "a"}}},
		{"human closed same target, any kind: nothing", behindOut(KindPR, "a"),
			[]forge.Item{closedByHuman(1, KindConflict, "a")}, nil},
		{"human closed newer target: create", behindOut(KindPR, "b"),
			[]forge.Item{closedByHuman(1, KindPR, "a")},
			[]Action{{Op: Create, Kind: KindPR, Target: "b"}}},
		{"nothing found: create", behindOut(KindBehind, "1"), nil,
			[]Action{{Op: Create, Kind: KindBehind, Target: "1"}}},
		{"lane closed same target does not count: create", behindOut(KindPR, "a"),
			[]forge.Item{closedByLane(1, KindConflict, "a")},
			[]Action{{Op: Create, Kind: KindPR, Target: "a"}}},
		{"current leaves open behind open", Outcome{Measurable: true},
			[]forge.Item{openItem(1, KindBehind, "1")}, nil},
		{"current leaves open pr open", Outcome{Measurable: true},
			[]forge.Item{openItem(1, KindPR, "a")}, nil},
		{"error leaves open behind alone", errOut,
			[]forge.Item{openItem(1, KindBehind, "1")},
			[]Action{{Op: Create, Kind: KindError, Target: ""}}},
		// Error track.
		{"error, open error: update body", Outcome{Target: "why"},
			[]forge.Item{openItem(1, KindError, "old")},
			[]Action{{Op: Update, Item: openItem(1, KindError, "old"), Kind: KindError, Target: "why"}}},
		{"error, no open error: create", errOut, nil,
			[]Action{{Op: Create, Kind: KindError, Target: ""}}},
		{"error, closed error: still create", errOut,
			[]forge.Item{closedByHuman(1, KindError, "")},
			[]Action{{Op: Create, Kind: KindError, Target: ""}}},
		{"measurable closes open error", behindOut(KindBehind, "1"),
			[]forge.Item{openItem(1, KindError, "")},
			[]Action{{Op: CloseByLane, Item: openItem(1, KindError, "")}, {Op: Create, Kind: KindBehind, Target: "1"}}},
		{"current closes open error", Outcome{Measurable: true},
			[]forge.Item{openItem(1, KindError, "")},
			[]Action{{Op: CloseByLane, Item: openItem(1, KindError, "")}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(testKey, c.out, c.found)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Decide = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestDecide_prToConflictClosesOldCreatesNew(t *testing.T) {
	got := Decide(testKey, behindOut(KindConflict, "b"), []forge.Item{openItem(4, KindPR, "a")})
	want := []Action{{Op: CloseByLane, Item: openItem(4, KindPR, "a")}, {Op: Create, Kind: KindConflict, Target: "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecide_conflictToPr(t *testing.T) {
	got := Decide(testKey, behindOut(KindPR, "b"), []forge.Item{openItem(4, KindConflict, "b")})
	want := []Action{{Op: CloseByLane, Item: openItem(4, KindConflict, "b")}, {Op: Create, Kind: KindPR, Target: "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// pr X, then conflict X (the lane closed the PR itself), then pr X again: a new PR.
func TestDecide_prThenConflictThenPrSameTargetCreatesPrAgain(t *testing.T) {
	found := []forge.Item{closedByLane(1, KindPR, "x"), openItem(2, KindConflict, "x")}
	got := Decide(testKey, behindOut(KindPR, "x"), found)
	want := []Action{{Op: CloseByLane, Item: openItem(2, KindConflict, "x")}, {Op: Create, Kind: KindPR, Target: "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// Once the conflict issue is closed by the lane too, a pr is still created.
	found = []forge.Item{closedByLane(1, KindPR, "x"), closedByLane(2, KindConflict, "x")}
	got = Decide(testKey, behindOut(KindPR, "x"), found)
	if len(got) != 1 || got[0].Op != Create {
		t.Fatalf("got %+v, want one Create", got)
	}
}

func TestDecide_humanClosedSameTargetNothing(t *testing.T) {
	for _, k := range []ItemKind{KindPR, KindConflict, KindBehind} {
		if got := Decide(testKey, behindOut(KindBehind, "a"), []forge.Item{closedByHuman(1, k, "a")}); got != nil {
			t.Fatalf("kind %s: got %+v, want nothing", k, got)
		}
	}
}

func TestDecide_humanClosedNewerTargetCreates(t *testing.T) {
	got := Decide(testKey, behindOut(KindBehind, "b"), []forge.Item{closedByHuman(1, KindBehind, "a")})
	if len(got) != 1 || got[0].Op != Create || got[0].Target != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecide_errorLeavesOpenBehindAlone(t *testing.T) {
	behind := openItem(1, KindBehind, "1")
	for _, a := range Decide(testKey, errOut, []forge.Item{behind}) {
		if a.Item.Number == 1 {
			t.Fatalf("touched the open behind item: %+v", a)
		}
	}
	pr := openItem(2, KindPR, "a")
	for _, a := range Decide(testKey, errOut, []forge.Item{pr}) {
		if a.Item.Number == 2 {
			t.Fatalf("touched the open pr: %+v", a)
		}
	}
}

func TestDecide_errorWithClosedErrorStillCreates(t *testing.T) {
	for _, closed := range []forge.Item{closedByHuman(1, KindError, ""), closedByLane(1, KindError, "")} {
		got := Decide(testKey, errOut, []forge.Item{closed})
		if len(got) != 1 || got[0].Op != Create || got[0].Kind != KindError {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestDecide_measurableClosesOpenErrorByLane(t *testing.T) {
	for _, out := range []Outcome{{Measurable: true}, behindOut(KindBehind, "1"), behindOut(KindPR, "a"), behindOut(KindConflict, "a")} {
		got := Decide(testKey, out, []forge.Item{openItem(7, KindError, "")})
		if len(got) == 0 || got[0].Op != CloseByLane || got[0].Item.Number != 7 {
			t.Fatalf("outcome %+v: got %+v", out, got)
		}
	}
}

func TestDecide_currentLeavesOpenBehindOpen(t *testing.T) {
	for _, k := range []ItemKind{KindPR, KindConflict, KindBehind} {
		if got := Decide(testKey, Outcome{Measurable: true}, []forge.Item{openItem(1, k, "1")}); got != nil {
			t.Fatalf("kind %s: got %+v", k, got)
		}
	}
}

func TestDecide_ignoresOtherKeys(t *testing.T) {
	other := forge.Item{Number: 9, State: "open", Body: Marker("other/key", KindBehind, "1")}
	plain := forge.Item{Number: 10, State: "open", Body: "no marker"}
	otherErr := forge.Item{Number: 11, State: "open", Body: Marker("other/key", KindError, "")}
	got := Decide(testKey, behindOut(KindBehind, "1"), []forge.Item{other, plain, otherErr})
	want := []Action{{Op: Create, Kind: KindBehind, Target: "1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	got = Decide(testKey, behindOut(KindBehind, "1"), []forge.Item{closedByHuman(12, KindBehind, "1"), other})
	if got != nil {
		t.Fatalf("got %+v", got)
	}
}
