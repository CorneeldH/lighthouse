package dispatch

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// Classification: pure logic. These tests fail if Decide stops ordering the starts and resumes
// of a tick by Priority band (P0; P1, empty or unknown; P2), then prio weight high to low, then
// trigger time.

// orderNow is the tick time of the start-order tests.
var orderNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// orderItem is a Controller item of issue n with priority and weight; without comments its start
// is ordered by its ItemUpdatedAt, a quarter before the tick.
func orderItem(n int, priority string, weight int) Item {
	at := orderNow.Add(-15 * time.Minute)
	return Item{Issue: n, Title: "Bouw", State: "OPEN", ItemID: "I", Status: statusController,
		Priority: priority, PrioWeight: weight, ItemUpdatedAt: at, IssueUpdatedAt: at}
}

// orderInput is o/r with items, no brake.
func orderInput(items ...Item) Input {
	return Input{Now: orderNow, Repos: []RepoInput{{
		Snapshot:   Snapshot{Version: SnapshotVersion, Repo: "o/r", GatheredAt: orderNow, Items: items},
		Controller: repoconfig.Controller{SpecReview: true, PlanReview: true},
		Language:   "nl", DefaultBranch: "main",
		Installed: map[string]bool{"lighthouse-start-issue": true},
	}}}
}

// startedIssues lists the issues of the starts and resumes in got, in order.
func startedIssues(got []Action) []int {
	out := []int{}
	for _, a := range got {
		if a.Kind == KindStart || a.Kind == KindResume {
			out = append(out, a.Issue)
		}
	}
	return out
}

func TestStartOrderP0BeforeHighWeight(t *testing.T) {
	got := startedIssues(Decide(orderInput(orderItem(1, "", 99), orderItem(2, "P0", 1))))
	if want := []int{2, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("starts = %v, want %v (P0 with weight 1 before empty with weight 99)", got, want)
	}
}

func TestStartOrderP2LastDespiteWeight(t *testing.T) {
	got := startedIssues(Decide(orderInput(orderItem(1, "P2", 99), orderItem(2, "", 0))))
	if want := []int{2, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("starts = %v, want %v (P2 with weight 99 after empty with weight 0)", got, want)
	}
}

func TestStartOrderWeightThenTime(t *testing.T) {
	earlier := orderItem(3, "", 2)
	earlier.ItemUpdatedAt = orderNow.Add(-time.Hour)
	got := startedIssues(Decide(orderInput(orderItem(1, "", 2), orderItem(2, "", 5), earlier)))
	// Weight 5 first; 1 and 3 weigh the same, so 3's earlier time wins.
	if want := []int{2, 3, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("starts = %v, want %v (higher weight first, then the earlier time)", got, want)
	}
}

func TestStartOrderUnknownPriorityIsP1Band(t *testing.T) {
	got := startedIssues(Decide(orderInput(orderItem(1, "P3", 1), orderItem(2, "", 3), orderItem(3, "P2", 9))))
	if want := []int{2, 1, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("starts = %v, want %v (P3 sorts as empty, by weight; P2 last)", got, want)
	}
}

// A resume orders as a start does: with equal weight its earlier answer (11:30) goes before the
// start (11:45), with a lower weight it goes after a start of a heavier issue in the same band.
func TestStartOrderResumeByWeightThenTime(t *testing.T) {
	cases := []struct {
		name          string
		resumeWeight  int
		startPriority string
		startWeight   int
		want          []int
	}{
		{"equal weight, earlier answer first", 3, "", 3, []int{7, 8}},
		{"heavier start first", 1, "", 4, []int{8, 7}},
		{"P2 start after a light resume", 0, "P2", 9, []int{7, 8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := decideFixtures(t)["answer-resume"].Input
			repo := &in.Repos[0]
			items := []Item{repo.Snapshot.Items[0], orderItem(8, tc.startPriority, tc.startWeight)}
			items[0].Priority, items[0].PrioWeight = "", tc.resumeWeight
			repo.Snapshot.Items = items
			got := Decide(in)
			if issues := startedIssues(got); !reflect.DeepEqual(issues, tc.want) {
				t.Errorf("starts = %v (kinds %v), want %v", issues, kinds(got), tc.want)
			}
		})
	}
}

// A push carries the weight of its item: with a higher weight it goes before an older start of a
// lighter issue, rather than falling behind it on weight 0.
func TestStartOrderPushCarriesWeight(t *testing.T) {
	in := pushHandoff(t, "when: now")
	item7(&in).Priority, item7(&in).PrioWeight = "", 5
	early := orderItem(8, "", 1)
	early.ItemUpdatedAt = time.Date(2026, 10, 31, 6, 0, 0, 0, time.UTC)
	in.Repos[0].Snapshot.Items = append(in.Repos[0].Snapshot.Items, early)
	var got []string
	for _, a := range Decide(in) {
		if a.Kind == KindPush || a.Kind == KindStart {
			got = append(got, string(a.Kind)+":"+strconv.Itoa(a.Issue))
		}
	}
	if want := []string{"push:7", "start:8"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
