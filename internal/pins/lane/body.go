// Package lane renders and recognises the issues and pull requests the upstream-pins lane writes.
package lane

import (
	"fmt"
	"net/url"
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

// escape makes a marker field safe: it holds no space, no '>' (so " -->" cannot occur), no '@'
// or '#', and decodes back with unescape. Slashes stay readable.
func escape(s string) string {
	e := url.PathEscape(s)
	e = strings.ReplaceAll(e, "%2F", "/")
	return strings.ReplaceAll(e, "@", "%40")
}

func unescape(s string) (string, bool) {
	v, err := url.PathUnescape(s)
	return v, err == nil
}

// Marker is the hidden line that lets the lane find its own item again. Key and target are
// percent-encoded, so any value (spaces, "-->", unicode, empty) round-trips through ParseMarker.
func Marker(key string, kind ItemKind, target string) string {
	return markerPrefix + escape(key) + " " + string(kind) + " " + escape(target) + markerSuffix
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
	fields := strings.Split(rest[:end], " ")
	if len(fields) != 3 {
		return "", "", "", false
	}
	k := ItemKind(fields[1])
	switch k {
	case KindPR, KindConflict, KindBehind, KindError:
	default:
		return "", "", "", false
	}
	key, ok1 := unescape(fields[0])
	target, ok2 := unescape(fields[2])
	if !ok1 || !ok2 {
		return "", "", "", false
	}
	return key, k, target, true
}

// safeURL makes a link harmless outside a fence: no mention, issue reference, backtick, markup
// or whitespace survives, each becomes a percent escape.
func safeURL(u string) string {
	return strings.NewReplacer("@", "%40", "#", "%23", "`", "%60", "<", "%3C", ">", "%3E",
		" ", "%20", "\t", "%09", "\n", "%0A", "\r", "%0D").Replace(u)
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

// prSteps is the short version of "Een pin ophogen, skill of bestand" in docs/pins.md; keep them
// in agreement.
const prSteps = `Zo hoog je deze pin op (uitleg: docs/pins.md, sectie "Een pin ophogen"):

1. Lees de diffstat, de upstream-commits en de diff zelf.
2. Pas zo nodig aan op de branch van deze PR, met nieuwe commits.
3. Zet de PR op ready for review, laat CI groen worden en merge hem zelf; de lane merget nooit.
4. Bij een conflict: neem de wijziging met de hand over en zet upstream-commit (skill) of source_sha (bestand) op de doelcommit hieronder: de nieuwste upstream-commit die het pad raakte, niet de HEAD van upstream.
`

// conflictSteps is the short version of the conflict paragraph of "Een pin ophogen" in
// docs/pins.md.
const conflictSteps = `Zo hoog je deze pin op (uitleg: docs/pins.md, sectie "Een pin ophogen"):

1. Neem de wijziging met de hand over in een gewone branch en PR.
2. Zet upstream-commit (skill) of source_sha (bestand) op de doelcommit hieronder: de nieuwste upstream-commit die het pad raakte, niet de HEAD van upstream.
3. Werk pack/SOURCES.toml bij als er bestanden bijkwamen of verdwenen, draai init --upgrade en voeg een fragment toe aan changelog.d.
`

// behindSteps is the short version of "Een pin ophogen, plugin of tool" in docs/pins.md.
const behindSteps = `Zo hoog je deze pin op (uitleg: docs/pins.md, sectie "Een pin ophogen"):

1. Lees de releasenotes hieronder; de oudste ontbrekende staan achter de vergelijklink.
2. Probeer de nieuwe versie lokaal uit.
3. Verhoog version in de pin in lighthouse.toml, in een PR die dit issue sluit met bewijs.
4. Past de versie niet, sluit dan dit issue met een reden.
`

// RenderBehindBody is the body of the issue that reports a pin behind its upstream. The releases
// (newest first) appear only inside fences, each cut to MaxExcerpt; once the body would pass
// MaxBody or maxReleases, the rest becomes a note that points at compareURL.
func RenderBehindBody(marker string, key string, from, to string, rels []forge.Release, compareURL string) string {
	var b strings.Builder
	b.WriteString(marker + "\n\n")
	link := safeURL(compareURL)
	b.WriteString("Een pin staat achter upstream.\n\n" + behindSteps + "\n")
	b.WriteString(Fence(fmt.Sprintf("Pin: %s\nHuidig: %s\nUpstream: %s", cut(key, 200), cut(from, 200), cut(to, 200))) + "\n\n")
	fmt.Fprintf(&b, "Vergelijk: %s\n\n", link)
	shown := 0
	for _, r := range rels {
		text := fmt.Sprintf("Tag: %s\nNaam: %s\nURL: %s\n\n%s", cut(r.Tag, 200), cut(r.Name, 200), cut(r.URL, 200), excerpt(r.Body))
		block := Fence(text) + "\n\n"
		if shown >= maxReleases || b.Len()+len(block) > MaxBody-reserve {
			break
		}
		b.WriteString(block)
		shown++
	}
	if omitted := len(rels) - shown; omitted > 0 {
		fmt.Fprintf(&b, "nog %d releases, zie %s\n", omitted, link)
	}
	return b.String()
}

// RenderPRBody is the body of the draft pull request that moves a pinned path to upstream. It
// begins with `No issue:` (the shape pr-create-gate wants) and never carries a closing keyword.
// target is the upstream commit the pin is raised to.
func RenderPRBody(marker, key, target string, commits []pins.CommitInfo, statDiff, compareURL string) string {
	var b strings.Builder
	link := safeURL(compareURL)
	b.WriteString("No issue: automatische upstream-update van een pin, geen issue.\n\n")
	b.WriteString(marker + "\n\n")
	b.WriteString(prSteps + "\n")
	b.WriteString(Fence(pinLines(key, target)) + "\n\n")
	fmt.Fprintf(&b, "Vergelijk: %s\n\n", link)
	b.WriteString("Wijzigingen:\n\n")
	b.WriteString(Fence(cut(statDiff, 10*MaxExcerpt)) + "\n\n")
	writeCommits(&b, commits, link)
	return b.String()
}

// RenderConflictBody is the body of the issue for a pin the lane cannot move itself: a merge
// that conflicts, or one that reaches .github/workflows/. reason, the conflicting files and the
// upstream commits appear only inside fences. target is the upstream commit to raise the pin to.
func RenderConflictBody(marker, key, target string, conflicts []string, reason string, commits []pins.CommitInfo, compareURL string) string {
	var b strings.Builder
	link := safeURL(compareURL)
	b.WriteString(marker + "\n\n")
	b.WriteString("Een pin staat achter upstream, maar de lane kan de wijziging niet zelf overnemen.\n\n")
	b.WriteString(conflictSteps + "\n")
	b.WriteString(Fence(pinLines(key, target)) + "\n\n")
	b.WriteString("Reden:\n\n")
	b.WriteString(Fence(excerpt(reason)) + "\n\n")
	if len(conflicts) > 0 {
		b.WriteString("Conflicterende bestanden:\n\n")
		b.WriteString(Fence(cut(strings.Join(conflicts, "\n"), 10*MaxExcerpt)) + "\n\n")
	}
	fmt.Fprintf(&b, "Vergelijk: %s\n\n", link)
	writeCommits(&b, commits, link)
	return b.String()
}

// pinLines names the pin and the upstream commit it is raised to.
func pinLines(key, target string) string {
	return "Pin: " + cut(key, 200) + "\nDoelcommit: " + cut(target, 200)
}

// writeCommits adds the upstream commits, each in its own fence and cut to MaxExcerpt, until
// the body budget is spent; the rest becomes a note that points at link.
func writeCommits(b *strings.Builder, commits []pins.CommitInfo, link string) {
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
		fmt.Fprintf(b, "nog %d commits, zie %s\n", omitted, link)
	}
}

// RenderErrorBody is the body of the issue that reports a pin the lane could not measure.
func RenderErrorBody(marker, key, detail string) string {
	return marker + "\n\n" +
		"Een pin is niet te meten.\n\n" +
		Fence("Pin: "+cut(key, 200)) + "\n\n" +
		Fence(excerpt(detail)) + "\n"
}
