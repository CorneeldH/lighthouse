package lane

import (
	"regexp"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

var (
	mentionRe = regexp.MustCompile(`@|#\d`)
	closingRe = regexp.MustCompile(`(?i)\b(fix(es|ed)?|close[sd]?|resolve[sd]?)\b`)
)

// stripFences removes every fenced block, recognising the fence by its own length.
func stripFences(t *testing.T, body string) string {
	t.Helper()
	var out []string
	var fence string
	for _, line := range strings.Split(body, "\n") {
		if fence == "" {
			if strings.HasPrefix(line, "```") {
				fence = strings.TrimRight(line, "abcdefghijklmnopqrstuvwxyz")
				continue
			}
			out = append(out, line)
			continue
		}
		if strings.TrimSpace(line) == fence {
			fence = ""
		}
	}
	if fence != "" {
		t.Fatalf("unclosed fence %q in body", fence)
	}
	return strings.Join(out, "\n")
}

func longestRun(s string) int {
	longest, cur := 0, 0
	for _, r := range s {
		if r == '`' {
			cur++
			longest = max(longest, cur)
		} else {
			cur = 0
		}
	}
	return longest
}

func TestFence_longerThanAnyBacktickRun(t *testing.T) {
	cases := map[string]string{
		"none":   "plain text",
		"two":    "a `` b",
		"three":  "```go\ncode\n```",
		"seven":  "x ``````` y",
		"mixed":  "` `` ``` ````",
		"empty":  "",
		"no-eol": "end```",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			got := Fence(text)
			lines := strings.Split(got, "\n")
			open := lines[0]
			if strings.Trim(open, "`") != "" {
				t.Fatalf("opening line %q is not only backticks", open)
			}
			if len(open) < 3 || len(open) <= longestRun(text) {
				t.Fatalf("fence %q not longer than longest run %d in %q", open, longestRun(text), text)
			}
			if lines[len(lines)-1] != open {
				t.Fatalf("closing line %q != opening %q", lines[len(lines)-1], open)
			}
			if !strings.Contains(got, text) {
				t.Fatalf("fence lost the text: %q", got)
			}
		})
	}
}

func TestRenderBehindBody_noMentionsOutsideFence(t *testing.T) {
	rels := []forge.Release{{
		Tag: "v2.0.0", Name: "Release for @naam #7", URL: "https://github.com/o/r/releases/tag/v2.0.0",
		Body: "thanks @naam\nFixes #123\n```\nCloses #5\n```\nResolves #9 @team/x",
	}}
	body := RenderBehindBody("<!-- m -->", "tools/x", "1.0.0", "2.0.0", rels, "https://github.com/o/r/compare/v1.0.0...v2.0.0")
	outside := stripFences(t, body)
	if mentionRe.MatchString(outside) {
		t.Fatalf("mention outside fence: %q", outside)
	}
	if closingRe.MatchString(outside) {
		t.Fatalf("closing keyword outside fence: %q", outside)
	}
	if !strings.Contains(body, "Fixes #123") {
		t.Fatalf("upstream text missing from body")
	}
}

func TestRenderBehindBody_underLimitWithTwentyReleases(t *testing.T) {
	var rels []forge.Release
	for i := 0; i < 20; i++ {
		rels = append(rels, forge.Release{
			Tag: "v1." + string(rune('a'+i)), Body: strings.Repeat("x", 10000), URL: "https://example.test/r",
		})
	}
	const link = "https://github.com/o/r/compare/v1...v2"
	body := RenderBehindBody("<!-- m -->", "k", "1", "2", rels, link)
	if len(body) >= 65536 {
		t.Fatalf("body is %d bytes", len(body))
	}
	m := regexp.MustCompile(`nog (\d+) releases, zie (\S+)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no truncation note in body")
	}
	if m[2] != link {
		t.Fatalf("note links %q, want %q", m[2], link)
	}
	shown := strings.Count(body, "Tag: ")
	if shown+atoi(t, m[1]) != 20 {
		t.Fatalf("shown %d + omitted %s != 20", shown, m[1])
	}
}

func TestRenderBehindBody_excerptIsCut(t *testing.T) {
	rels := []forge.Release{{Tag: "v2", Body: strings.Repeat("é", MaxExcerpt+500)}}
	body := RenderBehindBody("<!-- m -->", "k", "1", "2", rels, "u")
	if n := strings.Count(body, "é"); n != MaxExcerpt {
		t.Fatalf("excerpt has %d runes, want %d", n, MaxExcerpt)
	}
}

func TestRenderBehindBody_budgetHoldsWithMultibyte(t *testing.T) {
	var rels []forge.Release
	for i := 0; i < 500; i++ {
		rels = append(rels, forge.Release{Tag: "v", Body: strings.Repeat("😀", 5000)})
	}
	body := RenderBehindBody("<!-- m -->", "k", "1", "2", rels, "u")
	if len(body) > MaxBody+2000 || len(body) >= 65536 {
		t.Fatalf("body is %d bytes", len(body))
	}
}

func TestRenderPRBody_startsWithNoIssueAndNoCloses(t *testing.T) {
	commits := []pins.CommitInfo{
		{SHA: "abc1234", Subject: "fix: thing, closes #4", Body: "Fixes #123 thanks @naam\n```\nx"},
		{SHA: "def5678", Subject: "feat: other"},
	}
	body := RenderPRBody("<!-- m -->", "core/skills/x", commits, "a | 2 +-\n```", "https://github.com/o/r/compare/a...b")
	if !strings.HasPrefix(body, "No issue: ") {
		t.Fatalf("body starts %q", body[:20])
	}
	outside := stripFences(t, body)
	if closingRe.MatchString(outside) || mentionRe.MatchString(outside) {
		t.Fatalf("unsafe text outside fence: %q", outside)
	}
	if regexp.MustCompile(`(?m)^Closes #`).MatchString(body) {
		t.Fatalf("body carries Closes")
	}
	if !strings.Contains(body, "abc1234") || !strings.Contains(body, "feat: other") {
		t.Fatalf("commits missing: %q", body)
	}
}

func TestRenderErrorBody_detailInFence(t *testing.T) {
	body := RenderErrorBody("<!-- m -->", "k", "boom @x #12 ```")
	if outside := stripFences(t, body); mentionRe.MatchString(outside) {
		t.Fatalf("mention outside fence: %q", outside)
	}
	if !strings.HasPrefix(body, "<!-- m -->") {
		t.Fatalf("marker not first: %q", body)
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	for _, kind := range []ItemKind{KindPR, KindConflict, KindBehind, KindError} {
		m := Marker("core/skills/test-standard", kind, "abc123")
		if want := "<!-- lighthouse-pin: core/skills/test-standard " + string(kind) + " abc123 -->"; m != want {
			t.Fatalf("marker %q, want %q", m, want)
		}
		key, k, target, ok := ParseMarker("intro\n" + m + "\nrest")
		if !ok || key != "core/skills/test-standard" || k != kind || target != "abc123" {
			t.Fatalf("round trip: %q %q %q %v", key, k, target, ok)
		}
	}
	if _, _, _, ok := ParseMarker("no marker here"); ok {
		t.Fatalf("parsed a body without marker")
	}
	if _, _, _, ok := ParseMarker("<!-- lighthouse-pin: k bogus t -->"); ok {
		t.Fatalf("parsed an unknown kind")
	}
	if LaneClosedMarker != "<!-- lighthouse-pin-closed-by-lane -->" {
		t.Fatalf("LaneClosedMarker changed: %q", LaneClosedMarker)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
