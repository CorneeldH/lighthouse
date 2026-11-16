// Package lane renders and recognises the issues and pull requests the upstream-pins lane writes.
package lane

import (
	"fmt"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

const (
	// MaxBody is the budget in bytes for one body; GitHub refuses 65536 characters or more.
	MaxBody = 60000
	// MaxExcerpt is the number of characters kept of one release or commit.
	MaxExcerpt = 1500

	// maxReleases bounds the releases shown, so a long backlog stays readable.
	maxReleases = 10
	// reserve is what the header, footer and truncation note may take on top of the excerpts.
	reserve = 2000
)

// LaneClosedMarker marks an item the lane itself closed, so it is not reopened as human work.
const LaneClosedMarker = "<!-- lighthouse-pin-closed-by-lane -->"

// ItemKind is the kind of item a marker names.
type ItemKind string

const (
	KindPR       ItemKind = "pr"
	KindConflict ItemKind = "conflict"
	KindBehind   ItemKind = "behind"
	KindError    ItemKind = "error"
)

const (
	markerPrefix = "<!-- lighthouse-pin: "
	markerSuffix = " -->"
)

// Marker is the hidden line that lets the lane find its own item again. Key, kind and target
// must not contain whitespace.
func Marker(key string, kind ItemKind, target string) string {
	return markerPrefix + key + " " + string(kind) + " " + target + markerSuffix
}

// ParseMarker finds a marker in body. ok is false without a marker, with a malformed one or
// with an unknown kind.
func ParseMarker(body string) (key string, kind ItemKind, target string, ok bool) {
	start := strings.Index(body, markerPrefix)
	if start < 0 {
		return "", "", "", false
	}
	rest := body[start+len(markerPrefix):]
	end := strings.Index(rest, markerSuffix)
	if end < 0 {
		return "", "", "", false
	}
	fields := strings.Fields(rest[:end])
	if len(fields) != 3 {
		return "", "", "", false
	}
	switch k := ItemKind(fields[1]); k {
	case KindPR, KindConflict, KindBehind, KindError:
		return fields[0], k, fields[2], true
	}
	return "", "", "", false
}

// Fence puts text in a fenced code block whose fence is longer than any backtick run in text
// (three at least), so upstream text can neither close the block nor trigger mentions or
// closing keywords.
func Fence(text string) string {
	n := max(3, longestBacktickRun(text)+1)
	fence := strings.Repeat("`", n)
	return fence + "\n" + strings.TrimSuffix(text, "\n") + "\n" + fence
}

func longestBacktickRun(s string) int {
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

// excerpt cuts s to MaxExcerpt characters.
func excerpt(s string) string { return cut(s, MaxExcerpt) }

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// RenderBehindBody is the body of the issue that reports a pin behind its upstream. The releases
// (newest first) appear only inside fences, each cut to MaxExcerpt; once the body would pass
// MaxBody or maxReleases, the rest becomes a note that points at compareURL.
func RenderBehindBody(marker string, key string, from, to string, rels []forge.Release, compareURL string) string {
	var b strings.Builder
	b.WriteString(marker + "\n\n")
	fmt.Fprintf(&b, "De pin `%s` staat op `%s`; upstream is `%s`. Vergelijk: %s\n\n", key, from, to, compareURL)
	shown := 0
	for _, r := range rels {
		text := fmt.Sprintf("Tag: %s\nNaam: %s\nURL: %s\n\n%s", r.Tag, r.Name, r.URL, excerpt(r.Body))
		block := Fence(text) + "\n\n"
		if shown >= maxReleases || b.Len()+len(block) > MaxBody-reserve {
			break
		}
		b.WriteString(block)
		shown++
	}
	if omitted := len(rels) - shown; omitted > 0 {
		fmt.Fprintf(&b, "nog %d releases, zie %s\n", omitted, compareURL)
	}
	return b.String()
}

// RenderPRBody is the body of the draft pull request that moves a pinned path to upstream. It
// begins with `No issue:` (the shape pr-create-gate wants) and never carries a closing keyword.
func RenderPRBody(marker, key string, commits []pins.CommitInfo, statDiff, compareURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "No issue: automatische upstream-update van pin `%s`, geen issue.\n\n", key)
	b.WriteString(marker + "\n\n")
	fmt.Fprintf(&b, "Vergelijk: %s\n\n", compareURL)
	b.WriteString("Wijzigingen:\n\n")
	b.WriteString(Fence(cut(statDiff, 10*MaxExcerpt)) + "\n\n")
	b.WriteString("Upstream-commits:\n\n")
	shown := 0
	for _, c := range commits {
		text := c.SHA + " " + c.Subject
		if c.Body != "" {
			text += "\n\n" + c.Body
		}
		block := Fence(excerpt(text)) + "\n\n"
		if b.Len()+len(block) > MaxBody-reserve {
			break
		}
		b.WriteString(block)
		shown++
	}
	if omitted := len(commits) - shown; omitted > 0 {
		fmt.Fprintf(&b, "nog %d commits, zie %s\n", omitted, compareURL)
	}
	return b.String()
}

// RenderErrorBody is the body of the issue that reports a pin the lane could not measure.
func RenderErrorBody(marker, key, detail string) string {
	return marker + "\n\n" +
		fmt.Sprintf("De pin `%s` is niet te meten:\n\n", key) +
		Fence(excerpt(detail)) + "\n"
}
