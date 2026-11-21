package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/priorities"
)

func prioDeps(read func(context.Context) ([]priorities.Repo, error), runs func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error)) Deps {
	d := testDeps(populated(), nil)
	d.Priorities = read
	d.Runs = runs
	return d
}

func TestPrioritiesReturnsReposAndRuns(t *testing.T) {
	repos := []priorities.Repo{{
		Slug: "o/r",
		Issues: []priorities.Issue{{
			Number: 3, Title: "three", URL: "https://x/3", Status: "Ready", Label: "needs_shaping", OpenQuestion: true,
			Milestone: &priorities.MilestoneRef{Number: 1, Title: "M1", State: "open"},
			BlockedBy: []priorities.Blocker{{Repo: "o/s", Issue: 9}},
		}},
		Milestones: []priorities.Milestone{
			{Number: 1, Title: "M1", Description: "d", Open: 2, Done: 1, Closed: []priorities.ClosedIssue{{Number: 4, Title: "four", URL: "https://x/4"}}},
			{Number: 2, Title: "M2", Closed: []priorities.ClosedIssue{}},
		},
	}}
	rec := get(New(testSite(), prioDeps(
		func(context.Context) ([]priorities.Repo, error) { return repos, nil }, noRuns)), "/api/priorities")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	body := decode(t, rec.Body.Bytes())
	if string(body["runs"]) != "[]" {
		t.Errorf("runs = %s, want []", body["runs"])
	}
	var got []priorities.Repo
	if err := json.Unmarshal(body["repos"], &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, repos) {
		t.Errorf("repos = %+v, want %+v", got, repos)
	}
	var raw []map[string]json.RawMessage
	_ = json.Unmarshal(body["repos"], &raw)
	if _, ok := raw[0]["milestones"]; !ok {
		t.Errorf("repo has no snake_case key milestones: %s", body["repos"])
	}
	var issues, milestones []map[string]json.RawMessage
	_ = json.Unmarshal(raw[0]["issues"], &issues)
	_ = json.Unmarshal(raw[0]["milestones"], &milestones)
	if string(issues[0]["label"]) != `"needs_shaping"` || string(issues[0]["open_question"]) != "true" {
		t.Errorf("issue label/open_question = %s / %s, want \"needs_shaping\" / true", issues[0]["label"], issues[0]["open_question"])
	}
	if string(milestones[0]["closed"]) != `[{"number":4,"title":"four","url":"https://x/4"}]` {
		t.Errorf("closed = %s", milestones[0]["closed"])
	}
	if string(milestones[1]["closed"]) != "[]" {
		t.Errorf("empty closed = %s, want [] and never null", milestones[1]["closed"])
	}
}

func TestPrioritiesEmptyListsAreArrays(t *testing.T) {
	rec := get(New(testSite(), prioDeps(
		func(context.Context) ([]priorities.Repo, error) { return nil, nil }, noRuns)), "/api/priorities")
	body := decode(t, rec.Body.Bytes())
	if string(body["repos"]) != "[]" || string(body["runs"]) != "[]" {
		t.Errorf("body = %s, want both lists as []", rec.Body)
	}
}

func TestPrioritiesNotConfiguredIs503(t *testing.T) {
	rec := get(New(testSite(), prioDeps(
		func(context.Context) ([]priorities.Repo, error) {
			return nil, fmt.Errorf("x: %w", forge.ErrNotConfigured)
		}, noRuns)), "/api/priorities")
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503, body %s", rec.Code, rec.Body)
	}
}

func TestPrioritiesBuildFailureIs502AndOthersStillServe(t *testing.T) {
	h := New(testSite(), prioDeps(
		func(context.Context) ([]priorities.Repo, error) { return nil, errors.New("github: 500") }, noRuns))
	if rec := get(h, "/api/priorities"); rec.Code != 502 {
		t.Fatalf("priorities status = %d, want 502, body %s", rec.Code, rec.Body)
	}
	if rec := get(h, "/api/questions"); rec.Code != 200 {
		t.Errorf("questions status = %d, want 200, body %s", rec.Code, rec.Body)
	}
}

func TestPrioritiesRunsComeFromRunsDep(t *testing.T) {
	runs := func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error) {
		return []dispatch.RunView{
			{Repo: "o/r", Issue: 3, State: "open", Phase: "build", Title: "ignored"},
			{Repo: "o/s", Issue: 4, State: "parked", Phase: "plan"},
		}, nil, nil
	}
	rec := get(New(testSite(), prioDeps(
		func(context.Context) ([]priorities.Repo, error) { return nil, nil }, runs)), "/api/priorities")
	var body struct {
		Runs []priorities.Run `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []priorities.Run{
		{Repo: "o/r", Issue: 3, State: "open", Phase: "build"},
		{Repo: "o/s", Issue: 4, State: "parked", Phase: "plan"},
	}
	if !reflect.DeepEqual(body.Runs, want) {
		t.Errorf("runs = %+v, want %+v", body.Runs, want)
	}
}

func TestPrioritiesRunsErrorIs500(t *testing.T) {
	runs := func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error) {
		return nil, nil, errors.New("db closed")
	}
	rec := get(New(testSite(), prioDeps(
		func(context.Context) ([]priorities.Repo, error) { return nil, nil }, runs)), "/api/priorities")
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500, body %s", rec.Code, rec.Body)
	}
}
