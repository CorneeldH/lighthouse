package history

import (
	"fmt"
	"regexp"
	"strings"
)

// Thresholds of the level rule; this is the only file that holds them.
const (
	minPRs               = 3
	maxTrunkPushRatio    = 0.20
	minTestedCommitRatio = 0.5
)

var (
	testCmdRe  = regexp.MustCompile(`\b(?:go\s+test|pytest|npm\s+test|cargo\s+test|lighthouse\s+gate)\b`)
	codexCmdRe = regexp.MustCompile(`(?:^|[;&|(])\s*(?:\w+=\S*\s+)*codex(?:\s|$)`)
)

// chainCounts are the per-session chain counts that Analyze keeps over the merged records.
type chainCounts struct {
	Commits, CommitsAfterTest, Codex int
}

// Counts is what the level rule looks at.
type Counts struct {
	TrunkPushes, KnownPushes int
	PRs                      int
	Chain                    chainCounts
	Sessions                 int
}

// chainTracker counts commits, tested commits and codex calls, per session.
type chainTracker struct {
	chainCounts
	tested map[string]bool
}

func newChainTracker() *chainTracker { return &chainTracker{tested: map[string]bool{}} }

func (c *chainTracker) add(r Record, sess string) {
	for _, cmd := range r.Commands {
		if testCmdRe.MatchString(cmd) {
			c.tested[sess] = true
		}
		if gitCommitRe.MatchString(cmd) {
			c.Commits++
			if c.tested[sess] {
				c.CommitsAfterTest++
			}
		}
		if codexCmdRe.MatchString(cmd) {
			c.Codex++
		}
	}
}

func decideLevel(c Counts) Level {
	if c.Sessions == 0 {
		return Level{Recommended: "light", Reason: "no history"}
	}
	var missing []string
	if c.PRs < minPRs {
		missing = append(missing, fmt.Sprintf("%d PRs (need at least %d)", c.PRs, minPRs))
	}
	if c.KnownPushes == 0 || float64(c.TrunkPushes)/float64(c.KnownPushes) >= maxTrunkPushRatio {
		if c.KnownPushes == 0 {
			missing = append(missing, "no trunk ratio: no pushes with a known branch")
		} else {
			missing = append(missing, fmt.Sprintf("%d%% of pushes on the trunk (need under %d%%)",
				pct(c.TrunkPushes, c.KnownPushes), int(maxTrunkPushRatio*100)))
		}
	}
	if c.Chain.Commits == 0 || float64(c.Chain.CommitsAfterTest)/float64(c.Chain.Commits) < minTestedCommitRatio {
		if c.Chain.Commits == 0 {
			missing = append(missing, "no commits to check for tests")
		} else {
			missing = append(missing, fmt.Sprintf("%d%% of commits after a test command (need at least %d%%)",
				pct(c.Chain.CommitsAfterTest, c.Chain.Commits), int(minTestedCommitRatio*100)))
		}
	}
	if len(missing) > 0 {
		return Level{Recommended: "light", Reason: "not standard: " + strings.Join(missing, "; ")}
	}
	if c.Chain.Codex == 0 {
		return Level{Recommended: "standard", Reason: "not strict: no codex call found"}
	}
	return Level{Recommended: "strict", Reason: fmt.Sprintf("%d PRs, %d codex calls, tests before commits, few trunk pushes", c.PRs, c.Chain.Codex)}
}

func pct(a, b int) int { return a * 100 / b }
