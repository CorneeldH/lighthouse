package packcontract

import "testing"

// Classification: fails when a review skill stops applying the explore/exploit assignment in
// round 1 or stops passing it to the record: the variant literal of its component, the
// `--assignment-applied` flag, and the sentence that a non-applied assignment shows in the
// record as `assignment = standard` next to a trajectory with a variant (the record has no
// reason field).
func TestReviewSkillsApplyAssignment(t *testing.T) {
	variantLiteral := map[string]string{
		"review-plan": "variant:plan_review=codex",
		"review-spec": "variant:spec_review=codex",
	}
	forEachReviewVariant(t, func(t *testing.T, name, _, body string) {
		wantAll(t, flat(body),
			variantLiteral[name],
			"--assignment-applied",
			"A non-applied assignment shows in the record as `assignment = standard` while the trajectory has a variant (join `review_runs.assignment` with `trajectories.assignment`); the record has no reason field",
		)
	})
}
