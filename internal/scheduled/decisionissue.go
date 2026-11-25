package scheduled

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

const (
	markerPrefix = "<!-- lighthouse: "
	markerSuffix = " -->"
	// dateLayout is the layout of Marker.First.
	dateLayout = "2006-01-02"
)

// Marker is the machine line on line 1 of a decision issue: which job wrote it, for which run
// and base commit, when the first scan was, and how many scans the issue holds.
type Marker struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Run     string `json:"run"`
	Base    string `json:"base"`
	First   string `json:"first"`
	Scans   int    `json:"scans"`
}

// String is the HTML comment line, without a line break.
func (m Marker) String() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// A struct of strings and ints always encodes.
	_ = enc.Encode(m)
	return markerPrefix + strings.TrimSuffix(buf.String(), "\n") + markerSuffix
}

// ParseMarker reads the marker from line 1 of body, and only there.
func ParseMarker(body string) (Marker, bool) {
	line, _, _ := strings.Cut(body, "\n")
	line = strings.TrimSuffix(line, "\r")
	if !strings.HasPrefix(line, markerPrefix) || !strings.HasSuffix(line, markerSuffix) {
		return Marker{}, false
	}
	var m Marker
	raw := strings.TrimSuffix(strings.TrimPrefix(line, markerPrefix), markerSuffix)
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return Marker{}, false
	}
	return m, true
}

// FindDecisionIssue lists the issues in state (open or closed) that carry label, have a marker
// of kind on line 1 and were written by the token's user, oldest first. For closed, the caller
// picks the newest ClosedAt.
func FindDecisionIssue(ctx context.Context, f forge.Forge, kind, label, state string) ([]forge.Issue, error) {
	if state != "open" && state != "closed" {
		return nil, fmt.Errorf("decision issue state %q: want open or closed", state)
	}
	login, err := f.Login(ctx)
	_ = login
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	issues, err := f.ListIssues(ctx, forge.IssueFilter{State: state, Label: label})
	if err != nil {
		return nil, fmt.Errorf("list %s issues: %w", state, err)
	}
	var out []forge.Issue
	for _, is := range issues {
		if is.Author != login || !slices.Contains(is.Labels, label) {
			continue
		}
		if m, ok := ParseMarker(is.Body); ok && m.Kind == kind {
			out = append(out, is)
		}
	}
	slices.SortStableFunc(out, func(a, b forge.Issue) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.Number, b.Number))
	})
	return out, nil
}

// Publish describes one decision issue write.
type Publish struct {
	Kind, Title, Label string
	ExtraLabels        []string
	// Warning is line 2 of the body, given the date of the first scan and the scan count.
	Warning func(first string, scans int) string
	Run     string
	Base    string
	Now     time.Time
	// Body is the content below the header, for a new issue.
	Body    string
	MaxBody int // limit in characters of the whole body, header included
	// Merge, when set, builds the content of an update from the live content (the body
	// without its two header lines); nil uses Body.
	Merge func(live string) string
	// CommentTitle heads the comment that keeps the previous body.
	CommentTitle string
}

// header is line 1, line 2 and a blank line.
func (p Publish) header(m Marker) string {
	warning := ""
	if p.Warning != nil {
		warning = p.Warning(m.First, m.Scans)
	}
	return m.String() + "\n" + warning + "\n\n"
}

// stripHeader drops the marker line, the warning line and one blank line after them.
func stripHeader(body string) string {
	if _, ok := ParseMarker(body); !ok {
		return body
	}
	parts := strings.SplitN(body, "\n", 3)
	if len(parts) < 3 {
		return ""
	}
	return strings.TrimPrefix(parts[2], "\n")
}

func (p Publish) check(body string) error {
	if n := utf8.RuneCountInString(body); n > p.MaxBody {
		return fmt.Errorf("decision issue body is %d characters, the limit is %d", n, p.MaxBody)
	}
	return nil
}

// PublishDecisionIssue creates the decision issue, or updates the oldest open one: the live
// body goes into a comment first, then the body is replaced with scans+1. The same run on an
// open issue does nothing. When the comment fails the body stays; when the body update fails
// after the comment, the error is returned and exactly one comment exists.
func PublishDecisionIssue(ctx context.Context, f forge.Forge, p Publish) (forge.Issue, error) {
	open, err := FindDecisionIssue(ctx, f, p.Kind, p.Label, "open")
	if err != nil {
		return forge.Issue{}, err
	}
	if len(open) == 0 {
		m := Marker{Kind: p.Kind, Version: 1, Run: p.Run, Base: p.Base, First: p.Now.Format(dateLayout), Scans: 1}
		body := p.header(m) + p.Body
		if err := p.check(body); err != nil {
			return forge.Issue{}, err
		}
		labels := append([]string{p.Label}, p.ExtraLabels...)
		is, err := f.CreateIssue(ctx, forge.NewIssue{Title: p.Title, Body: body, Labels: labels})
		if err != nil {
			return forge.Issue{}, fmt.Errorf("create decision issue: %w", err)
		}
		return is, nil
	}
	listed := open[0]
	if m, _ := ParseMarker(listed.Body); m.Run == p.Run {
		return listed, nil
	}
	live, err := f.Issue(ctx, listed.Number)
	if err != nil {
		return forge.Issue{}, fmt.Errorf("read decision issue #%d: %w", listed.Number, err)
	}
	prev, ok := ParseMarker(live.Body)
	if !ok {
		prev, _ = ParseMarker(listed.Body)
	}
	if prev.Run == p.Run {
		return live, nil
	}
	content := p.Body
	if p.Merge != nil {
		content = p.Merge(stripHeader(live.Body))
	}
	m := Marker{Kind: p.Kind, Version: 1, Run: p.Run, Base: p.Base, First: prev.First, Scans: prev.Scans + 1}
	body := p.header(m) + content
	if err := p.check(body); err != nil {
		return forge.Issue{}, err
	}
	comment := live.Body
	if p.CommentTitle != "" {
		comment = p.CommentTitle + "\n\n" + live.Body
	}
	if _, err := f.CreateComment(ctx, live.Number, comment); err != nil {
		return forge.Issue{}, fmt.Errorf("keep the previous body of #%d as a comment: %w", live.Number, err)
	}
	if err := f.UpdateIssueBody(ctx, live.Number, body); err != nil {
		return forge.Issue{}, fmt.Errorf("update the body of #%d after its comment: %w", live.Number, err)
	}
	live.Body = body
	return live, nil
}
