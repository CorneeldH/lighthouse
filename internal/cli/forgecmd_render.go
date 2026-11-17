package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

func renderTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// renderMilestoneRef is a milestone as it nests in an issue or PR: nil for none.
func renderMilestoneRef(m *forge.Milestone) any {
	if m == nil {
		return nil
	}
	return map[string]any{"number": m.Number, "title": m.Title, "description": m.Description}
}

func renderLabels(labels []string) []any {
	out := make([]any, len(labels))
	for i, l := range labels {
		out[i] = l
	}
	return out
}

// renderIssue is the JSON shape of an issue.
func renderIssue(i forge.Issue) map[string]any {
	return map[string]any{
		"number": i.Number, "title": i.Title, "body": i.Body, "state": i.State,
		"labels": renderLabels(i.Labels), "milestone": renderMilestoneRef(i.Milestone),
		"url": i.URL, "created_at": renderTime(i.CreatedAt), "author": i.Author,
	}
}

// renderChange is the JSON shape of a pull request or merge request; state is open, closed or
// merged.
func renderChange(c forge.Change) map[string]any {
	return map[string]any{
		"number": c.Number, "title": c.Title, "body": c.Body, "state": c.State,
		"labels": renderLabels(c.Labels), "milestone": renderMilestoneRef(c.Milestone),
		"url": c.URL, "created_at": renderTime(c.CreatedAt), "author": c.Author,
	}
}

func renderComments(cs []protocol.Comment) []any {
	out := make([]any, len(cs))
	for i, c := range cs {
		out[i] = map[string]any{"author": c.Author, "body": c.Body, "created_at": c.CreatedAt}
	}
	return out
}

// renderBlocker is one blocker of an issue: state is open or closed.
func renderBlocker(b forge.Blocker) map[string]any {
	return map[string]any{"number": b.Issue, "state": strings.ToLower(b.State), "repo": b.Repo, "merged_pr": b.MergedPR}
}

func renderMilestone(m forge.Milestone) map[string]any {
	return map[string]any{"number": m.Number, "title": m.Title, "description": m.Description, "state": m.State}
}

// pickField follows path (names joined by dots) into v. A scalar comes back as text, anything
// else as JSON. A null on the way, or at the end, is the empty string; a name that is not
// there is an error.
func pickField(v any, path string) (string, error) {
	cur := v
	for _, name := range strings.Split(path, ".") {
		if cur == nil {
			return "", nil
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("unknown field %q", path)
		}
		next, ok := m[name]
		if !ok {
			return "", fmt.Errorf("unknown field %q", path)
		}
		cur = next
	}
	switch s := cur.(type) {
	case nil:
		return "", nil
	case string:
		return s, nil
	case int:
		return strconv.Itoa(s), nil
	}
	b, err := json.Marshal(cur)
	return string(b), err
}

// forgeResult is what a read command prints.
type forgeResult struct {
	value any    // a map, or a []any of maps for a list
	field string // "" prints JSON
}

// render prints the result: JSON, or with a field the field of the value, or of every item of a
// list, one per line.
func (r forgeResult) render() (string, error) {
	if r.field == "" {
		b, err := json.MarshalIndent(r.value, "", "  ")
		return string(b) + "\n", err
	}
	items := []any{r.value}
	if list, ok := r.value.([]any); ok {
		items = list
	}
	var sb strings.Builder
	for _, it := range items {
		s, err := pickField(it, r.field)
		if err != nil {
			return "", err
		}
		sb.WriteString(s + "\n")
	}
	return sb.String(), nil
}
