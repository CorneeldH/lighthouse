package protocol

import (
	"fmt"
	"regexp"
	"strings"
)

// Comment is one issue comment as the forge returns it.
type Comment struct {
	ID                   int64
	Author               string
	Body, CreatedAt, URL string
	Rockets              int
}

// State is what the comments of an issue say about it.
type State int

const (
	// StateNone: no protocol header in any comment.
	StateNone State = iota
	// StateQuestion: the last header is a question nobody has answered.
	StateQuestion
	// StateReturned: the last header is a returned issue.
	StateReturned
	// StateOther: the last header is anything else, or the question is answered.
	StateOther
)

// Status is the State plus the Comment and Header of the last header that counts.
type Status struct {
	State   State
	Comment Comment
	Header  Header
}

// artefactMarkers open an artefact comment (spec or plan as an issue comment), under the
// current and the legacy marker.
var artefactMarkers = []string{"<!-- lighthouse:", "<!-- paia:"}

// MapPlannedMarker opens the machine line of a comment of the scheduled job map-planned. The
// line follows the job's own heading, so it is not the first line of the body.
const MapPlannedMarker = "<!-- lighthouse:map-planned "

// IsAnswer reports whether a comment can answer a question: not empty, no protocol header,
// no artefact comment and no comment of the map-planned job.
func IsAnswer(body string) bool {
	text := strings.TrimSpace(body)
	if text == "" || strings.HasPrefix(text, Robot) {
		return false
	}
	for _, marker := range artefactMarkers {
		if strings.HasPrefix(text, marker) {
			return false
		}
	}
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), MapPlannedMarker) {
			return false
		}
	}
	return true
}

// Classify reads the comments, oldest first, like staat_van: the last parsed header wins, // lighthouse: allow-language — staat_van is the name of the paia function this ports
// a rebased note is skipped, and a question stays open unless a later comment without a
// rocket reaction answers it.
func Classify(comments []Comment) Status {
	var last *Status
	var after []Comment
	for _, c := range comments {
		header, ok := ParseHeader(c.Body)
		if ok && header.Kind == KindRebased {
			continue
		}
		if !ok {
			if last != nil {
				after = append(after, c)
			}
			continue
		}
		last = &Status{Comment: c, Header: header}
		after = nil
	}
	if last == nil {
		return Status{State: StateNone}
	}
	switch last.Header.Kind {
	case KindQuestion:
		last.State = StateQuestion
		for _, c := range after {
			if c.Rockets == 0 && IsAnswer(c.Body) {
				last.State = StateOther
				break
			}
		}
	case KindReturned:
		last.State = StateReturned
	default:
		last.State = StateOther
	}
	return *last
}

var fenceOpen = regexp.MustCompile("^(`{3,})bash$")

// QuestionText returns only the question of a `🤖 Question`: the text after the question label,
// numbered `1.`, `2.` in a bundle. Where, what, options and commands stay on the issue. A
// question without a question label, or with an empty one, falls back to the whole text
// without heading line and footer; outside a code block it drops blank lines and trims each
// line, inside one it keeps every line literally.
func QuestionText(body string) string {
	if fields, ok := ParseQuestion(body); ok && len(fields.Questions) > 0 {
		var out []string
		for i, q := range fields.Questions {
			if q.Question == "" {
				out = nil
				break
			}
			if len(fields.Questions) > 1 {
				out = append(out, fmt.Sprintf("%d. %s", i+1, q.Question))
			} else {
				out = append(out, q.Question)
			}
		}
		if out != nil {
			return strings.Join(out, "\n")
		}
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	var out []string
	fence := ""
	for _, line := range lines[1:] {
		if fence != "" {
			out = append(out, line)
			if line == fence {
				fence = ""
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			fence = m[1]
			out = append(out, line)
		} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "<sub>") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}
