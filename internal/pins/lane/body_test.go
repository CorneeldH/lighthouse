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
	body := RenderPRBody("<!-- m -->", "core/skills/x", "def5678", commits, "a | 2 +-\n```", "https://github.com/o/r/compare/a...b")
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

var oddKeys = []string{
	"node_modules/@scope/pkg", "a#12", "with`tick", "two words", "x --> y", "ünï/çode", "100%", "",
}

func TestRender_oddValuesStayInertOutsideFences(t *testing.T) {
	link := "https://github.com/o/r/compare/a@b...c#12`x y"
	rels := []forge.Release{{Tag: "v1", Body: "b"}}
	for _, key := range oddKeys {
		bodies := map[string]string{
			"behind": RenderBehindBody(Marker(key, KindBehind, "t"), key, key, key, rels, link),
			"pr":     RenderPRBody(Marker(key, KindPR, "t"), key, "t", []pins.CommitInfo{{SHA: "a", Subject: "s"}}, "d", link),
			"error":  RenderErrorBody(Marker(key, KindError, "t"), key, "d"),
			"conflict": RenderConflictBody(Marker(key, KindConflict, "t"), key, "t", []string{key}, key,
				[]pins.CommitInfo{{SHA: "a", Subject: key}}, link),
		}
		for name, body := range bodies {
			outside := stripFences(t, body)
			if mentionRe.MatchString(outside) || closingRe.MatchString(outside) || strings.Contains(outside, "`") {
				t.Errorf("%s key %q: unsafe outside fence: %q", name, key, outside)
			}
			k, _, _, ok := ParseMarker(body)
			if !ok || k != key {
				t.Errorf("%s key %q: marker parsed %q %v", name, key, k, ok)
			}
		}
	}
}

func TestMarkerRoundTrip_oddValues(t *testing.T) {
	for _, key := range oddKeys {
		for _, target := range []string{"", "a b", "-->", "@x#1", "é"} {
			m := Marker(key, KindPR, target)
			if strings.Contains(m[len(markerPrefix):len(m)-len(markerSuffix)], "-->") || strings.ContainsAny(m, "@") {
				t.Fatalf("marker %q holds a hazard", m)
			}
			k, kind, tg, ok := ParseMarker("x\n" + m + "\ny")
			if !ok || k != key || kind != KindPR || tg != target {
				t.Fatalf("key %q target %q: got %q %q %q %v", key, target, k, kind, tg, ok)
			}
		}
	}
}

func TestRenderBehindBody_byteBudgetAlone(t *testing.T) {
	var rels []forge.Release
	for i := 0; i < maxReleases; i++ {
		rels = append(rels, forge.Release{Tag: "v", Body: strings.Repeat("😀", MaxExcerpt)})
	}
	body := RenderBehindBody("<!-- m -->", "k", "1", "2", rels, "u")
	if len(body) > MaxBody {
		t.Fatalf("body is %d bytes, over MaxBody", len(body))
	}
	if !strings.Contains(body, "nog ") {
		t.Fatalf("no truncation note")
	}
}

// Fails when the conflict issue loses the conflicting files or the reason, lets upstream text
// out of its fence, starts like a PR body, or passes the body budget with many long commits.
func TestRenderConflictBody_filesReasonAndCommitsFenced(t *testing.T) {
	var commits []pins.CommitInfo
	for i := 0; i < 200; i++ {
		commits = append(commits, pins.CommitInfo{SHA: "abc1234", Subject: "Fixes #12 @mallory", Body: strings.Repeat("x", 3*MaxExcerpt)})
	}
	body := RenderConflictBody("<!-- m -->", "core/skills/x", "abc1234",
		[]string{"pack/core/skills/x/SKILL.md", "pack/core/skills/x/@evil#3.md"}, "merge conflict ``` @x",
		commits, "https://github.com/o/r/compare/a...b")
	if !strings.HasPrefix(body, "<!-- m -->") {
		t.Fatalf("marker not first: %q", body[:30])
	}
	if strings.Contains(body, "No issue:") {
		t.Fatalf("a conflict issue carries the PR's No issue line")
	}
	for _, want := range []string{"pack/core/skills/x/SKILL.md", "pack/core/skills/x/@evil#3.md", "merge conflict", "abc1234"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	outside := stripFences(t, body)
	if closingRe.MatchString(outside) || mentionRe.MatchString(outside) {
		t.Fatalf("unsafe text outside fence: %q", outside)
	}
	if len(body) > MaxBody {
		t.Fatalf("body is %d bytes, over MaxBody", len(body))
	}
	if !strings.Contains(body, "nog ") {
		t.Fatalf("no truncation note for the commits that did not fit")
	}
}

func TestRender_carryRaiseInstructions(t *testing.T) {
	const target = "0123456789abcdef0123456789abcdef01234567"
	pr := RenderPRBody("<!-- m -->", "k", target, []pins.CommitInfo{{SHA: "a", Subject: "s"}}, "d", "u")
	for _, want := range []string{"docs/pins.md", "ready for review", "merge hem zelf", "upstream-commit", "source_sha", "nieuwste upstream-commit die het pad raakte", "stopt de lane met bijwerken"} {
		if !strings.Contains(stripFences(t, pr), want) {
			t.Errorf("PR body lacks %q outside fences", want)
		}
	}
	conflict := RenderConflictBody("<!-- m -->", "k", target, nil, "r", []pins.CommitInfo{{SHA: "a", Subject: "s"}}, "u")
	for _, want := range []string{"docs/pins.md", "upstream-commit", "source_sha", "nieuwste upstream-commit die het pad raakte"} {
		if !strings.Contains(stripFences(t, conflict), want) {
			t.Errorf("conflict body lacks %q outside fences", want)
		}
	}
	for name, body := range map[string]string{"PR": pr, "conflict": conflict} {
		if !strings.Contains(body, "Doelcommit: "+target) {
			t.Errorf("%s body does not name the target commit %s", name, target)
		}
	}
	behind := RenderBehindBody("<!-- m -->", "k", "1", "2", []forge.Release{{Tag: "v2"}}, "u")
	for _, want := range []string{"docs/pins.md", "lighthouse.toml", "lokaal", "sluit", "tussenversie", "Refs"} {
		if !strings.Contains(stripFences(t, behind), want) {
			t.Errorf("behind body lacks %q outside fences", want)
		}
	}
}
