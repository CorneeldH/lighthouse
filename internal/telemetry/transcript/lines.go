package transcript

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

var (
	commandArgsRe = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	leadingNum    = regexp.MustCompile(`^\s*#?(\d+)(?:\s|$)`)
)

// leadingIssue returns the number at the very start of args, 0 when there is none.
func leadingIssue(args string) int {
	m := leadingNum.FindStringSubmatch(args)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// issueOf reads the issue number from the ARGUMENTS tail of a loaded skill or
// from the <command-args> of a slash command. A number in free text is ignored.
func issueOf(typ string, content json.RawMessage) int {
	if typ != "user" {
		return 0
	}
	for _, b := range contentBlocks(content) {
		if b.Type != "text" {
			continue
		}
		if strings.Contains(b.Text, skillMarker) {
			for _, ln := range strings.Split(b.Text, "\n") {
				if rest, ok := strings.CutPrefix(ln, "ARGUMENTS:"); ok {
					if n := leadingIssue(rest); n > 0 {
						return n
					}
				}
			}
		}
		if m := commandArgsRe.FindStringSubmatch(b.Text); m != nil {
			if n := leadingIssue(m[1]); n > 0 {
				return n
			}
		}
	}
	return 0
}

// toolTraffic collects the paths of Write and Edit tool uses, the ids of
// AskUserQuestion tool uses and the ids of tool results. Nothing else is kept.
func toolTraffic(content json.RawMessage) (writes, asks, results []string) {
	for _, b := range contentBlocks(content) {
		switch {
		case b.Type == "tool_use" && (b.Name == "Write" || b.Name == "Edit") && b.Input.FilePath != "":
			writes = append(writes, b.Input.FilePath)
		case b.Type == "tool_use" && b.Name == "AskUserQuestion" && b.ID != "":
			asks = append(asks, b.ID)
		case b.Type == "tool_result" && b.ToolUseID != "":
			results = append(results, b.ToolUseID)
		}
	}
	return writes, asks, results
}

// Lines gives the branch and issue number of every parsed line.
func (r Reading) Lines() []telemetry.Line {
	out := make([]telemetry.Line, len(r.Entries))
	for i, e := range r.Entries {
		out[i] = telemetry.Line{Branch: e.Branch, Issue: e.Issue}
	}
	return out
}

// WrittenFile is a file a Write or Edit tool use targeted; no content.
type WrittenFile struct {
	Path string
	At   time.Time
}

// WrittenFiles lists the Write and Edit targets in chronological order.
func (r Reading) WrittenFiles() []WrittenFile {
	var out []WrittenFile
	for _, e := range r.Entries {
		for _, p := range e.Writes {
			out = append(out, WrittenFile{Path: p, At: e.At})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Question is one AskUserQuestion tool use; Answered is nil until a tool
// result with the same id arrives.
type Question struct {
	ToolUseID string
	Asked     time.Time
	Answered  *time.Time
}

// Questions lists the AskUserQuestion tool uses in order of appearance, each
// paired with the first tool result that carries its id.
func (r Reading) Questions() []Question {
	var out []Question
	at := map[string]int{}
	for _, e := range r.Entries {
		for _, id := range e.Asks {
			if _, dup := at[id]; !dup {
				at[id] = len(out)
				out = append(out, Question{ToolUseID: id, Asked: e.At})
			}
		}
		for _, id := range e.Results {
			if i, ok := at[id]; ok && out[i].Answered == nil {
				t := e.At
				out[i].Answered = &t
			}
		}
	}
	return out
}
