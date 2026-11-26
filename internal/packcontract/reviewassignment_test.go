package packcontract

import "testing"

// Classification: fails when a review skill stops applying the explore/exploit assignment in
// round 1 or stops passing it to the record: the variant literal of its component, the
// `--assignment-applied` flag, the rule that a variant of another component is `standard` for
// this review, and the one sentence on how a non-applied assignment shows: `assignment =
// standard` in a review run of the variant's component next to a trajectory with that variant,
// or `assignment_applied = 0` (the record has no reason field).
func TestReviewSkillsApplyAssignment(t *testing.T) {
	variantLiteral := map[string]string{
		"review-plan": "variant:plan_review=codex",
		"review-spec": "variant:spec_review=codex",
	}
	otherComponent := map[string]string{
		"review-plan": "A variant of another component (`variant:spec_review=…`) is `standard` for this review: record `--assignment standard`; `write` refuses it.",
		"review-spec": "A variant of another component (`variant:plan_review=…`) is `standard` for this review: record `--assignment standard`; `write` refuses it.",
	}
	forEachReviewVariant(t, func(t *testing.T, name, variant, body string) {
		wantAll(t, flat(body),
			variantLiteral[name],
			"--assignment-applied",
			otherComponent[name],
			"The record has no reason field: a non-applied assignment shows either as `assignment = standard` in a review run whose `review_runs.component` is the component of the trajectory's variant (join `review_runs.assignment` with `trajectories.assignment`; an interactive review without the prompt line) or as `assignment_applied = 0` (a variant that this skill cannot apply).",
		)
		if variant == "claude" {
			wantAll(t, flat(body), "`--review-mode fallback --reviewer-model <reported model>` with `--assignment-applied=true`", "No *Fallback without Codex* banner")
		}
	})
}
