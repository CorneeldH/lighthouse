package lane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

// countingForge counts MergePR calls on top of any Forge.
type countingForge struct {
	forge.Forge
	merges atomic.Int32
}

func (c *countingForge) MergePR(ctx context.Context, n int, sha string) error {
	c.merges.Add(1)
	return c.Forge.MergePR(ctx, n, sha)
}

func newFake(t *testing.T) (*forgetest.Server, *countingForge) {
	t.Helper()
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	c := forge.New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return srv, &countingForge{Forge: c}
}

// keyMarker is the substring that finds every item of key, whatever its kind or target.
func keyMarker(key string) string { return markerPrefix + escape(key) + " " }

func content(key string, kind ItemKind, target string) ItemContent {
	body := Marker(key, kind, target) + "\n\nbody"
	if kind == KindPR {
		body = "No issue: automatisch.\n\n" + body
	}
	return ItemContent{Title: "pin " + key + " " + target, Body: body, Branch: "pins/" + key, Base: "main"}
}

// run is one lane run for key: find, decide, apply.
func run(t *testing.T, a Applier, key string, out Outcome, c ItemContent) error {
	t.Helper()
	found, err := a.F.FindByMarker(context.Background(), keyMarker(key))
	if err != nil {
		t.Fatal(err)
	}
	return a.Apply(context.Background(), key, Decide(key, out, found), c)
}

func find(t *testing.T, a Applier, key string) []forge.Item {
	t.Helper()
	items, err := a.F.FindByMarker(context.Background(), keyMarker(key))
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestApply_twoRunsOneIssue(t *testing.T) {
	_, f := newFake(t)
	a := Applier{F: f}
	out := Outcome{Kind: KindBehind, Target: "1.1", Measurable: true}
	for i := 0; i < 2; i++ {
		if err := run(t, a, testKey, out, content(testKey, KindBehind, "1.1")); err != nil {
			t.Fatal(err)
		}
	}
	if items := find(t, a, testKey); len(items) != 1 {
		t.Fatalf("after two identical runs: %d items, want 1", len(items))
	}

	out.Target = "1.2"
	if err := run(t, a, testKey, out, content(testKey, KindBehind, "1.2")); err != nil {
		t.Fatal(err)
	}
	items := find(t, a, testKey)
	if len(items) != 1 {
		t.Fatalf("after a newer release: %d items, want 1 (no second issue)", len(items))
	}
	if _, _, target, _ := ParseMarker(items[0].Body); target != "1.2" || items[0].State != "open" {
		t.Errorf("item = %+v, want open with target 1.2", items[0])
	}
}

func TestApply_closeWritesLaneMarkerInBody(t *testing.T) {
	_, f := newFake(t)
	a := Applier{F: f}
	if err := run(t, a, testKey, errOut, content(testKey, KindError, "boom")); err != nil {
		t.Fatal(err)
	}
	// The pin is measurable again: the error issue is closed by the lane.
	cur := Outcome{Measurable: true}
	if err := run(t, a, testKey, cur, ItemContent{}); err != nil {
		t.Fatal(err)
	}
	items := find(t, a, testKey)
	if len(items) != 1 || items[0].State != "closed" {
		t.Fatalf("items = %+v, want one closed", items)
	}
	if !strings.Contains(items[0].Body, LaneClosedMarker) {
		t.Errorf("body %q lacks the lane marker", items[0].Body)
	}
	if !strings.Contains(items[0].Body, Marker(testKey, KindError, "boom")) {
		t.Errorf("body %q lost the pin marker", items[0].Body)
	}
	// Decide must now see the lane closure: a second run does nothing.
	if err := run(t, a, testKey, cur, ItemContent{}); err != nil {
		t.Fatal(err)
	}
	if len(find(t, a, testKey)) != 1 {
		t.Error("a second run changed the items")
	}
}

func TestApply_prConflictPrCycleViaFindByMarker(t *testing.T) {
	_, f := newFake(t)
	a := Applier{F: f}
	const x = "abc123"
	step := func(kind ItemKind) {
		t.Helper()
		out := Outcome{Kind: kind, Target: x, Measurable: true}
		if err := run(t, a, testKey, out, content(testKey, kind, x)); err != nil {
			t.Fatal(err)
		}
	}
	open := func() (kinds []ItemKind, prs int) {
		for _, it := range find(t, a, testKey) {
			if it.State != "open" {
				continue
			}
			_, k, _, _ := ParseMarker(it.Body)
			kinds = append(kinds, k)
			if it.IsPR {
				prs++
			}
		}
		return
	}
	step(KindPR)
	if k, prs := open(); len(k) != 1 || k[0] != KindPR || prs != 1 {
		t.Fatalf("after pr: open %v prs %d", k, prs)
	}
	step(KindConflict)
	if k, prs := open(); len(k) != 1 || k[0] != KindConflict || prs != 0 {
		t.Fatalf("after conflict: open %v prs %d, want one conflict issue and the PR closed", k, prs)
	}
	step(KindPR)
	k, prs := open()
	if len(k) != 1 || k[0] != KindPR || prs != 1 {
		t.Fatalf("after second pr: open %v prs %d, want the PR created again", k, prs)
	}
	var lane int
	for _, it := range find(t, a, testKey) {
		if it.State == "closed" && strings.Contains(it.Body, LaneClosedMarker) {
			lane++
		}
	}
	if lane != 2 {
		t.Errorf("lane-closed items = %d, want 2 (first PR, conflict issue)", lane)
	}
}

func TestApply_failureForOnePinContinuesWithNext(t *testing.T) {
	srv, f := newFake(t)
	srv.FailFirst = map[string]int{"POST /repos/o/r/issues": 500}
	a := Applier{F: f}
	const keyA, keyB = "core/skills/a", "core/skills/b"
	out := Outcome{Kind: KindBehind, Target: "1", Measurable: true}

	errA := run(t, a, keyA, out, content(keyA, KindBehind, "1"))
	errB := run(t, a, keyB, out, content(keyB, KindBehind, "1"))
	if errA == nil || !strings.Contains(errA.Error(), keyA) {
		t.Errorf("error for A = %v, want one naming %s", errA, keyA)
	}
	if errB != nil {
		t.Errorf("error for B = %v", errB)
	}
	if len(find(t, a, keyA)) != 0 || len(find(t, a, keyB)) != 1 {
		t.Error("want no issue for A and one for B")
	}
}

func TestApply_failedActionDoesNotSkipTheRestOfOnePin(t *testing.T) {
	srv, f := newFake(t)
	a := Applier{F: f}
	if err := run(t, a, testKey, Outcome{Kind: KindPR, Target: "x", Measurable: true}, content(testKey, KindPR, "x")); err != nil {
		t.Fatal(err)
	}
	// Closing the PR fails; the new conflict issue must still be created, and the error returned.
	for n := range srv.Issues {
		srv.Fail = map[string]int{fmt.Sprintf("PATCH /repos/o/r/pulls/%d", n): 500}
	}
	err := run(t, a, testKey, Outcome{Kind: KindConflict, Target: "x", Measurable: true}, content(testKey, KindConflict, "x"))
	if err == nil {
		t.Fatal("want the close failure reported")
	}
	var conflicts int
	for _, it := range find(t, a, testKey) {
		if _, k, _, _ := ParseMarker(it.Body); k == KindConflict {
			conflicts++
		}
	}
	if conflicts != 1 {
		t.Errorf("conflict issues = %d, want 1 despite the failed close", conflicts)
	}
	for _, it := range find(t, a, testKey) {
		if it.IsPR && it.State != "open" {
			t.Error("the PR was closed although its body write failed")
		}
	}
}

func TestApply_neverCallsMergePR(t *testing.T) {
	_, f := newFake(t)
	a := Applier{F: f}
	steps := []Outcome{
		{Kind: KindPR, Target: "a", Measurable: true},
		{Kind: KindPR, Target: "b", Measurable: true},
		{Kind: KindConflict, Target: "b", Measurable: true},
		{Kind: KindPR, Target: "b", Measurable: true},
		errOut,
		{Measurable: true},
	}
	for _, out := range steps {
		kind := out.Kind
		if !out.Measurable {
			kind = KindError
		}
		if err := run(t, a, testKey, out, content(testKey, kind, out.Target)); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.merges.Load(); n != 0 {
		t.Errorf("MergePR called %d times, want 0", n)
	}
}

func TestApply_laneItemsHaveNoMilestoneNoBoardNoCloses(t *testing.T) {
	srv, f := newFake(t)
	a := Applier{F: f}
	if err := run(t, a, testKey, Outcome{Kind: KindPR, Target: "x", Measurable: true}, content(testKey, KindPR, "x")); err != nil {
		t.Fatal(err)
	}
	if err := run(t, a, "core/skills/y", Outcome{Kind: KindBehind, Target: "1", Measurable: true}, content("core/skills/y", KindBehind, "1")); err != nil {
		t.Fatal(err)
	}
	if len(srv.Pulls) != 1 || srv.Pulls[0]["draft"] != true {
		t.Fatalf("pulls = %+v, want one draft", srv.Pulls)
	}
	if head := srv.Pulls[0]["head"].(map[string]any)["ref"]; head != "pins/"+testKey {
		t.Errorf("head = %v", head)
	}
	if body, _ := srv.Pulls[0]["body"].(string); !strings.HasPrefix(body, "No issue:") {
		t.Errorf("PR body %q does not start with No issue:", body)
	}
	for n, it := range srv.Issues {
		if it["milestone"] != nil {
			t.Errorf("item %d has a milestone", n)
		}
		if body, _ := it["body"].(string); strings.Contains(strings.ToLower(body), "closes #") {
			t.Errorf("item %d body has a closing keyword", n)
		}
	}
	for _, p := range srv.Posts {
		if strings.Contains(p.Path, "projects") || strings.Contains(p.Path, "milestone") {
			t.Errorf("unexpected board/milestone request %s", p.Path)
		}
	}
	if srv.Count("/graphql") != 0 {
		t.Error("the lane used GraphQL, which only the board needs")
	}
}

// bodyFailForge fails every body write, so a close that writes the marker first cannot proceed.
type bodyFailForge struct{ forge.Forge }

func (bodyFailForge) UpdateIssueBody(context.Context, int, string) error {
	return errors.New("body write failed")
}
func (bodyFailForge) UpdatePRBody(context.Context, int, string) error {
	return errors.New("body write failed")
}

func TestApply_closeLeavesItemOpenWhenMarkerWriteFails(t *testing.T) {
	for kind, next := range map[ItemKind]ItemKind{KindPR: KindConflict, KindBehind: KindPR} {
		t.Run(string(kind), func(t *testing.T) {
			_, f := newFake(t)
			a := Applier{F: f}
			if err := run(t, a, testKey, Outcome{Kind: kind, Target: "x", Measurable: true}, content(testKey, kind, "x")); err != nil {
				t.Fatal(err)
			}
			broken := Applier{F: bodyFailForge{f}}
			err := run(t, broken, testKey, Outcome{Kind: next, Target: "x", Measurable: true}, content(testKey, next, "x"))
			if err == nil {
				t.Fatal("want the failed marker write reported")
			}
			for _, it := range find(t, a, testKey) {
				if _, k, _, _ := ParseMarker(it.Body); k == kind && it.State != "open" {
					t.Errorf("%s item closed without the lane marker in its body", kind)
				}
			}
		})
	}
}

func TestApply_closeKeepsOneLaneMarker(t *testing.T) {
	_, f := newFake(t)
	a := Applier{F: f}
	if err := run(t, a, testKey, errOut, content(testKey, KindError, "e")); err != nil {
		t.Fatal(err)
	}
	items := find(t, a, testKey)
	items[0].Body += "\n" + LaneClosedMarker
	if err := a.Apply(context.Background(), testKey, []Action{{Op: CloseByLane, Item: items[0]}}, ItemContent{}); err != nil {
		t.Fatal(err)
	}
	got := find(t, a, testKey)[0].Body
	if n := strings.Count(got, LaneClosedMarker); n != 1 {
		t.Errorf("lane marker count = %d, want 1", n)
	}
}

func TestApply_refusesBodyWithoutPinMarker(t *testing.T) {
	_, f := newFake(t)
	a := Applier{F: f}
	if err := run(t, a, testKey, errOut, content(testKey, KindError, "e")); err != nil {
		t.Fatal(err)
	}
	item := find(t, a, testKey)[0]
	cases := map[string]Action{
		"update": {Op: Update, Item: item, Kind: KindError, Target: "e2"},
		"create": {Op: Create, Kind: KindBehind, Target: "1"},
	}
	for name, act := range cases {
		for _, body := range []string{"no marker here", Marker("other/key", KindError, "e")} {
			err := a.Apply(context.Background(), testKey, []Action{act}, ItemContent{Title: "t", Body: body})
			if err == nil {
				t.Errorf("%s with body %q: want an error", name, body)
			}
		}
	}
	if got := find(t, a, testKey); len(got) != 1 || !strings.Contains(got[0].Body, Marker(testKey, KindError, "e")) {
		t.Errorf("items = %+v, want the original untouched", got)
	}
}
