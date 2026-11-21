package review

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var executabilityCats = []string{
	"spec_mismatch", "dependency_order", "repo_falsehood", "missing_artifact",
	"verification_gap", "unverified_external_assumption", "likely_runtime_failure",
}

// schemaItem is properties.findings.items of a findings or verification schema.
type schemaItem struct {
	Required   []string                  `json:"required"`
	Properties map[string]map[string]any `json:"properties"`
}

type reviewSchema struct {
	Required   []string `json:"required"`
	Properties struct {
		Findings struct {
			Items schemaItem `json:"items"`
		} `json:"findings"`
	} `json:"properties"`
}

func loadSchema(t *testing.T, skill, name string) reviewSchema {
	t.Helper()
	var s reviewSchema
	if err := json.Unmarshal(readFile(t, skillsDir, skill, name), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func enumOf(t *testing.T, item schemaItem, prop string) []string {
	t.Helper()
	raw, ok := item.Properties[prop]["enum"].([]any)
	if !ok {
		t.Fatalf("%s has no enum", prop)
	}
	var out []string
	for _, v := range raw {
		str, ok := v.(string)
		if !ok {
			t.Fatalf("%s enum holds %v, not a string", prop, v)
		}
		out = append(out, str)
	}
	return out
}

func requireAll(t *testing.T, have []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("required = %q, lacks %q", have, w)
		}
	}
}

// promptBody is the text between the markers of a template, which is what the reviewer gets.
func promptBody(t *testing.T, skill string) string {
	t.Helper()
	_, rest, _ := strings.Cut(string(readFile(t, skillsDir, skill, "reviewer-prompt.md")), beginMarker)
	body, _, _ := strings.Cut(rest, endMarker)
	return body
}

const noScenarioRule = "no concrete failure scenario, it is not a BLOCKER or MAJOR"

// pytest: test_plan_schema_requires_one_of_seven_categories
func TestPlanSchemaHasSevenExecutabilityCategories(t *testing.T) {
	item := loadSchema(t, "review-plan", "findings.schema.json").Properties.Findings.Items
	requireAll(t, item.Required, "cat", "plan_section", "failure_scenario")
	if _, ok := item.Properties["spec_section"]; ok {
		t.Error("the plan schema still has spec_section")
	}
	if got := enumOf(t, item, "cat"); !reflect.DeepEqual(got, executabilityCats) {
		t.Errorf("cat enum = %q, want %q", got, executabilityCats)
	}
}

// pytest: test_plan_prompt_asks_executability_not_improvement
func TestPlanPromptAsksExecutabilityNotImprovement(t *testing.T) {
	body := promptBody(t, "review-plan")
	for _, cat := range executabilityCats {
		if !strings.Contains(body, "`"+cat+"`") {
			t.Errorf("the plan prompt does not name `%s`", cat)
		}
	}
	for _, gone := range []string{"open_vraag", "operationeel", "tegenspraak"} {
		if strings.Contains(body, gone) {
			t.Errorf("the plan prompt still has %q", gone)
		}
	}
	for _, rule := range []string{"prove that executing this plan will fail", noScenarioRule} {
		if !strings.Contains(body, rule) {
			t.Errorf("the plan prompt lacks %q", rule)
		}
	}
}

// pytest: test_plan_verification_schema_is_scoped
func TestPlanVerificationSchemaIsScoped(t *testing.T) {
	s := loadSchema(t, "review-plan", "verification.schema.json")
	requireAll(t, s.Required, "verified")
	item := s.Properties.Findings.Items
	requireAll(t, item.Required, "verifies", "failure_scenario", "cat")
	if got, want := enumOf(t, item, "severity"), []string{"BLOCKER", "MAJOR"}; !reflect.DeepEqual(got, want) {
		t.Errorf("severity enum = %q, want %q", got, want)
	}
	want := append(append([]string{}, executabilityCats...), "fix_incomplete")
	if got := enumOf(t, item, "cat"); !reflect.DeepEqual(got, want) {
		t.Errorf("cat enum = %q, want %q", got, want)
	}
}

// pytest: test_spec_blocker_needs_a_failure_scenario
func TestSpecBlockerNeedsAFailureScenario(t *testing.T) {
	item := loadSchema(t, "review-spec", "findings.schema.json").Properties.Findings.Items
	requireAll(t, item.Required, "failure_scenario")
	if _, ok := item.Properties["cat"]; ok {
		t.Error("the spec schema has a cat")
	}
	body := promptBody(t, "review-spec")
	if !strings.Contains(body, noScenarioRule) {
		t.Errorf("the spec prompt lacks %q", noScenarioRule)
	}
	if strings.Contains(body, "{{CONTEXT_PATHS}}") {
		t.Error("the spec prompt has {{CONTEXT_PATHS}}")
	}
}

// pytest: test_runner_is_allowlisted
// It lives here because the runner is the review skills' entry point, and its permission is part of that contract.
func TestPackAllowsTheRunner(t *testing.T) {
	var perms struct {
		Allow []string `json:"allow"`
	}
	if err := json.Unmarshal(readFile(t, "../../pack/core/permissions.json"), &perms); err != nil {
		t.Fatal(err)
	}
	requireAll(t, perms.Allow, "Bash(lighthouse review-run:*)")
}
