package dispatch

import (
	"context"
	"regexp"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

var (
	// reviewHeading is the heading of a roborev comment, same pattern as the heading regex in internal/roborev/classify.go; keep the two in step.
	reviewHeading = regexp.MustCompile("(?m)^## roborev: (.+?) \\(`([^`]+)`\\)\\s*$")
	// processedMarker is the marker a processed review leaves behind.
	processedMarker = regexp.MustCompile(`<!-- roborev-verwerkt: ([^ >]*) -->`)
	hexSHA          = regexp.MustCompile(`^[0-9a-f]{7,}$`)
)

// mergeCandidate reports whether pr is open, not a draft, mergeable and green.
func mergeCandidate(pr forge.PR) bool {
	return pr.State == "OPEN" && !pr.Draft && pr.Mergeable == "MERGEABLE" && pr.Checks == "SUCCESS"
}

// sameSHA reports whether a and b are hex SHAs of at least 7 characters and one is a prefix of
// the other.
func sameSHA(a, b string) bool {
	if !hexSHA.MatchString(a) || !hexSHA.MatchString(b) {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// isRoborevBot reports whether login is the account that posts roborev reviews.
func isRoborevBot(login string) bool {
	return login == "github-actions[bot]" || login == "github-actions"
}

// roborevState reads the comments of a PR: "" without a review that asks for fixes,
// forge.RoborevProcessed when a trusted author left a marker for the review's SHA, else
// forge.RoborevOpen. Only the last review comment counts. The role of an author is asked only
// for a comment with a marker that matches the review.
func roborevState(ctx context.Context, comments []protocol.Comment, trust *Trust) (string, error) {
	var review *protocol.Comment
	for i := range comments {
		if isRoborevBot(comments[i].Author) && strings.Contains(comments[i].Body, "roborev-pr-comment") {
			review = &comments[i]
		}
	}
	if review == nil {
		return "", nil
	}
	m := reviewHeading.FindStringSubmatch(review.Body)
	if m == nil || (m[1] != "Review Complete" && m[1] != "Combined Review") ||
		!strings.Contains(review.Body, "Changes require fixes") {
		return "", nil
	}
	sha := m[2]
	for _, cm := range comments {
		for _, mk := range processedMarker.FindAllStringSubmatch(cm.Body, -1) {
			if !sameSHA(mk[1], sha) {
				continue
			}
			ok, err := trust.Trusted(ctx, cm.Author)
			if err != nil {
				return "", err
			}
			if ok {
				return forge.RoborevProcessed, nil
			}
			break
		}
	}
	return forge.RoborevOpen, nil
}
