package reflect

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// skipPatterns are lines that are no user prompt: system text, tool results, command expansions.
// Derived from should_include_message in scripts/lib/reflect_utils.py of
// BayramAnnakov/claude-reflect (MIT, (c) Bayram Annakov), commit
// 69056446e7d4fc877078cb2eb2dbac2d5e1c8845, via scripts/reflect_skills_run.py in personal_ai_agent.
var skipPatterns = regexp.MustCompile(`^<|^\[|^\{|tool_result|tool_use_id|<command-|<task-notification>|<system-reminder>|` +
	`This session is being continued|^Analysis:|^\*\*`)

// extractPrompts returns the user prompts of the session file, filtered and cut to maxPrompt
// runes. A line that cannot be read, is not valid JSON or is larger than maxLine bytes is
// skipped, never fatal: a session is read while it is written, and a cut line must not cost the
// prompts before it.
func extractPrompts(path string, maxPrompt, maxLine int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []string
	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) <= maxLine && len(bytes.TrimSpace(line)) > 0 {
			for _, text := range userTexts(line) {
				if strings.TrimSpace(text) == "" || skipPatterns.MatchString(text) {
					continue
				}
				if rs := []rune(text); len(rs) > maxPrompt {
					text = string(rs[:maxPrompt])
				}
				out = append(out, text)
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				return out // what is read stays
			}
			return out
		}
	}
}

// userTexts are the text blocks of one line when it is a user line that is not meta.
func userTexts(line []byte) []string {
	var e struct {
		Type    string `json:"type"`
		IsMeta  bool   `json:"isMeta"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "user" || e.IsMeta {
		return nil
	}
	var s string
	if json.Unmarshal(e.Message.Content, &s) == nil {
		return []string{s}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		if b.Type == "text" {
			out = append(out, b.Text)
		}
	}
	return out
}

// corpusSession is the prompts of one session, with its date (of its last write) and short id.
type corpusSession struct {
	Date    string
	ID      string
	Prompts []string
}

func (s corpusSession) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s · sessie %s\n", s.Date, s.ID)
	for _, p := range s.Prompts {
		fmt.Fprintf(&b, "- %s\n", oneLine(p))
	}
	b.WriteString("\n")
	return b.String()
}

func oneLine(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ") }

const (
	corpusTitle      = "# Sessiecorpus\n\n"
	headInteractive  = "## Interactief\n\n"
	headController   = "## Controller\n\n"
	repeatedTitle    = "### Herhaalde prompts\n"
	repeatedMinCount = 2
)

// buildCorpus renders the corpus: two sections, Interactief and Controller, each session under its
// kind. In Controller a prompt that occurs more than once (the fixed commands the controller
// repeats) is listed once with its count, so the repetition does not take the budget of the rest.
// The budget maxBytes is split half and half; what one section does not use goes to the other.
// Inside a section the newest sessions fill first (inter and ctrl are newest first); a session
// that no longer fits is skipped, a smaller and older one may still fit. The section is written
// oldest first. The result never exceeds maxBytes, unless the two headings alone do.
func buildCorpus(inter, ctrl []corpusSession, maxBytes int) []byte {
	repeated, ctrl := mergeRepeated(ctrl)
	base := len(corpusTitle) + len(headInteractive) + len(headController)
	budget := max(maxBytes-base, 0)
	iAll, cAll := size(inter), size(ctrl)+len(repeated)
	half := budget / 2
	bI, bC := half, budget-half
	switch {
	case iAll <= half:
		bI, bC = iAll, budget-iAll
	case cAll <= half:
		bC, bI = cAll, budget-cAll
	}
	var b strings.Builder
	b.WriteString(corpusTitle)
	b.WriteString(headInteractive)
	b.WriteString(fill(inter, bI))
	b.WriteString(headController)
	rest := bC
	if repeated != "" && len(repeated) <= rest {
		rest -= len(repeated)
		b.WriteString(repeated)
	}
	b.WriteString(fill(ctrl, rest))
	return []byte(b.String())
}

func size(ss []corpusSession) int {
	n := 0
	for _, s := range ss {
		n += len(s.render())
	}
	return n
}

// fill takes sessions newest first into budget bytes and returns them oldest first.
func fill(ss []corpusSession, budget int) string {
	var picked []string
	for _, s := range ss {
		r := s.render()
		if len(r) > budget {
			continue
		}
		budget -= len(r)
		picked = append(picked, r)
	}
	slices.Reverse(picked)
	return strings.Join(picked, "")
}

// mergeRepeated takes the prompts that occur at least twice over all of ss out of their sessions
// and returns them as one block of `- <prompt> ×<n>` lines, most frequent first; sessions left
// with no prompt drop out.
func mergeRepeated(ss []corpusSession) (block string, rest []corpusSession) {
	count := map[string]int{}
	for _, s := range ss {
		for _, p := range s.Prompts {
			count[oneLine(p)]++
		}
	}
	var rep []string
	for p, n := range count {
		if n >= repeatedMinCount {
			rep = append(rep, p)
		}
	}
	for _, s := range ss {
		var keep []string
		for _, p := range s.Prompts {
			if count[oneLine(p)] < repeatedMinCount {
				keep = append(keep, p)
			}
		}
		if len(keep) > 0 {
			s.Prompts = keep
			rest = append(rest, s)
		}
	}
	if len(rep) == 0 {
		return "", rest
	}
	sort.Slice(rep, func(i, j int) bool {
		if count[rep[i]] != count[rep[j]] {
			return count[rep[i]] > count[rep[j]]
		}
		return rep[i] < rep[j]
	})
	var b strings.Builder
	b.WriteString(repeatedTitle)
	for _, p := range rep {
		fmt.Fprintf(&b, "- %s ×%d\n", p, count[p])
	}
	b.WriteString("\n")
	return b.String(), rest
}
