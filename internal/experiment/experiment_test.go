package experiment

import (
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

var start = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

func exp(share float64) *repoconfig.Experiment {
	return &repoconfig.Experiment{ID: "e1", Component: "plan_review", Choice: "opus", Share: share, Sample: 10, Start: start}
}

func TestHashStable(t *testing.T) {
	a := Hash("owner/repo", 572)
	if a != Hash("owner/repo", 572) {
		t.Fatal("same input must give same hash")
	}
	if a == Hash("owner/repo", 573) {
		t.Fatal("other issue must give other hash")
	}
	if a == Hash("owner/other", 572) {
		t.Fatal("other repo must give other hash")
	}
	if a < 0 || a >= 1 {
		t.Fatalf("hash %v outside [0,1)", a)
	}
	const want = 0.35051173283061354 // sha256("owner/repo#572"), top 53 bits of first 8 bytes / 2^53 (checked with python)
	if got := Hash("owner/repo", 572); got != want {
		t.Fatalf("Hash(owner/repo, 572) = %v, want pinned vector %v", got, want)
	}
}

func TestDecideShare(t *testing.T) {
	h := Hash("owner/repo", 572)
	now := start.Add(time.Hour)
	if d := Decide(exp(h+0.01), "", "owner/repo", 572, 0, now); d.Assignment != "variant:plan_review=opus" || d.ExperimentID != "e1" {
		t.Fatalf("hash below share: %+v", d)
	}
	if d := Decide(exp(h/2), "", "owner/repo", 572, 0, now); d.Assignment != "standard" || d.ExperimentID != "" {
		t.Fatalf("hash above share: %+v", d)
	}
	if d := Decide(exp(h), "", "owner/repo", 572, 0, now); d.Assignment != "standard" {
		t.Fatalf("hash == share must be standard: %+v", d)
	}
}

func TestDecideSampleReached(t *testing.T) {
	now := start.Add(time.Hour)
	if d := Decide(exp(0.99), "", "r", 1, 10, now); d.Assignment != "standard" {
		t.Fatalf("sample reached: %+v", d)
	}
	if d := Decide(exp(0.99), "", "r", 1, 9, now); d.Assignment == "standard" {
		t.Fatalf("sample not reached: %+v", d)
	}
}

func TestDecideBeforeStart(t *testing.T) {
	if d := Decide(exp(0.99), "", "r", 1, 0, start.Add(-time.Second)); d.Assignment != "standard" {
		t.Fatalf("before start: %+v", d)
	}
	if d := Decide(exp(0.99), "", "r", 1, 0, start); d.Assignment == "standard" {
		t.Fatalf("at start: %+v", d)
	}
}

func TestDecideNilKeepsNote(t *testing.T) {
	d := Decide(nil, "conflict", "r", 1, 0, start)
	if d.Assignment != "standard" || d.Note != "conflict" {
		t.Fatalf("%+v", d)
	}
}

func TestMergeSameIDAcrossRepos(t *testing.T) {
	e := *exp(0.1)
	got, repos, note := Merge([]Declared{{"a/x", e}, {"b/y", e}})
	if note != "" || got == nil || *got != e {
		t.Fatalf("got %v, note %q", got, note)
	}
	if !repos["a/x"] || !repos["b/y"] || len(repos) != 2 {
		t.Errorf("repos = %v, want a/x and b/y", repos)
	}
}

func TestMergeEmpty(t *testing.T) {
	if got, repos, note := Merge(nil); got != nil || len(repos) != 0 || note != "" {
		t.Fatalf("got %v, note %q", got, note)
	}
}

func TestMergeDifferentComponents(t *testing.T) {
	a, b := *exp(0.1), *exp(0.1)
	b.ID, b.Component = "e2", "spec_review"
	got, _, note := Merge([]Declared{{"a/x", a}, {"b/y", b}})
	if got != nil || note != "experiments on different components: no assignment" {
		t.Fatalf("got %v, note %q", got, note)
	}
}

func TestMergeSameComponentDifferentIDs(t *testing.T) {
	a, b := *exp(0.1), *exp(0.1)
	b.ID = "e2"
	if got, _, note := Merge([]Declared{{"a/x", a}, {"b/y", b}}); got != nil || note == "" {
		t.Fatalf("got %v, note %q", got, note)
	}
}

func TestMergeSameIDDifferentFields(t *testing.T) {
	a, b := *exp(0.1), *exp(0.2)
	if got, _, note := Merge([]Declared{{"a/x", a}, {"b/y", b}}); got != nil || note == "" {
		t.Fatalf("got %v, note %q", got, note)
	}
}

// Only the repos that declare the active experiment take part: a repo that declares nothing is
// not in the set.
func TestMergeReposOnlyDeclaring(t *testing.T) {
	e := *exp(0.1)
	_, repos, _ := Merge([]Declared{{"a/x", e}})
	if !repos["a/x"] || repos["b/y"] {
		t.Errorf("repos = %v, want only a/x", repos)
	}
}
