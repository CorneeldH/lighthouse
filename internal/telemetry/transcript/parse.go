// Package transcript parses Claude Code session transcripts (JSONL) into
// de-duplicated per-message token usage. It is pure: no files, no database.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
)

// estimate prices one message; tests replace it with a fixed table.
var estimate = telemetry.Estimate

// File is one transcript file. Name is relative, with slashes;
// "<session>/subagents/..." is a sidechain.
type File struct {
	Name string
	R    io.Reader
}

// Entry is one parsed user or assistant line. It carries only what is needed
// later; no transcript text is kept.
type Entry struct {
	At        time.Time
	Type      string
	Sidechain bool
	SessionID string
	Effort    string // top-level "effort" of an assistant line
	Version   string // harness version of the line
	Skill     *SkillLoad
}

// Message is one unique API message (by message.id, else requestId, else line).
type Message struct {
	At        time.Time // first timestamp seen for the message
	Model     string
	Tokens    telemetry.Tokens
	Sidechain bool
}

// Reading is the parsed content of one session's files.
type Reading struct {
	Entries  []Entry
	Messages []Message
	Started  time.Time
	Ended    time.Time
	session  string
	pending  []string // skill names announced but not yet loaded
}

type usageJSON struct {
	Input         int64 `json:"input_tokens"`
	Output        int64 `json:"output_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	Split         *struct {
		M5 int64 `json:"ephemeral_5m_input_tokens"`
		H1 int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

type lineJSON struct {
	Type      string   `json:"type"`
	Timestamp string   `json:"timestamp"`
	SessionID string   `json:"sessionId"`
	RequestID string   `json:"requestId"`
	Sidechain bool     `json:"isSidechain"`
	Effort    string   `json:"effort"`
	Version   string   `json:"version"`
	Message   *msgJSON `json:"message"`
}

type msgJSON struct {
	ID    string     `json:"id"`
	Model string     `json:"model"`
	Usage *usageJSON `json:"usage"`
	// Content is a string (user) or a list of blocks; see contentBlocks.
	Content json.RawMessage `json:"content"`
}

func (u usageJSON) tokens() telemetry.Tokens {
	t := telemetry.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead}
	if u.Split != nil && (u.Split.M5 != 0 || u.Split.H1 != 0) {
		t.CacheWrite5m, t.CacheWrite1h = u.Split.M5, u.Split.H1
	} else {
		t.CacheWrite5m = u.CacheCreation
	}
	return t
}

func maxTokens(a, b telemetry.Tokens) telemetry.Tokens {
	m := func(x, y int64) int64 {
		if y > x {
			return y
		}
		return x
	}
	// The cache-write pair is taken whole from the line with the larger total, so a
	// split and an unsplit duplicate never add up to more than one line's total.
	cw := a
	if b.CacheWrite() > a.CacheWrite() {
		cw = b
	}
	return telemetry.Tokens{
		Input: m(a.Input, b.Input), Output: m(a.Output, b.Output), CacheRead: m(a.CacheRead, b.CacheRead),
		CacheWrite5m: cw.CacheWrite5m, CacheWrite1h: cw.CacheWrite1h,
	}
}

// Parse reads all files, skips lines that are not a JSON object (also a
// half-written last line) and counts only user and assistant lines. A message
// seen on several lines or files counts once; its tokens are the per-field
// maximum, since streamed lines of one message may carry partial counts.
func Parse(files []File) (Reading, error) {
	var r Reading
	index := map[string]int{} // dedupe key -> index in r.Messages
	lineNo := 0
	for _, f := range files {
		sidechain := strings.Contains(f.Name, "/subagents/")
		r.pending = nil // announcements never pair across files
		br := bufio.NewReader(f.R)
		for {
			raw, err := br.ReadString('\n')
			if line := strings.TrimSpace(raw); line != "" {
				lineNo++
				r.addLine(line, f.Name, sidechain, lineNo, index)
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return Reading{}, err
			}
		}
	}
	return r, nil
}

func (r *Reading) addLine(line, name string, sidechain bool, lineNo int, index map[string]int) {
	if !strings.HasPrefix(line, "{") {
		return
	}
	var l lineJSON
	if json.Unmarshal([]byte(line), &l) != nil {
		return
	}
	if l.Type != "user" && l.Type != "assistant" {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	if !at.IsZero() {
		if r.Started.IsZero() || at.Before(r.Started) {
			r.Started = at
		}
		if at.After(r.Ended) {
			r.Ended = at
		}
	}
	if r.session == "" && !sidechain {
		if l.SessionID != "" {
			r.session = l.SessionID
		} else if !strings.Contains(name, "/") {
			r.session = strings.TrimSuffix(name, ".jsonl")
		}
	}
	e := Entry{At: at, Type: l.Type, Sidechain: sidechain || l.Sidechain, SessionID: l.SessionID, Effort: l.Effort, Version: l.Version}
	if l.Message != nil {
		e.Skill = r.skillOf(l.Type, l.Message.Content)
	}
	r.Entries = append(r.Entries, e)
	if l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return
	}
	m := l.Message
	if m.Model == "" || strings.HasPrefix(m.Model, "<") { // "<synthetic>"
		return
	}
	key := "line:" + name + ":" + strconv.Itoa(lineNo)
	if m.ID != "" {
		key = "id:" + m.ID
	} else if l.RequestID != "" {
		key = "req:" + l.RequestID
	}
	tok := m.Usage.tokens()
	if i, ok := index[key]; ok {
		prev := &r.Messages[i]
		prev.Tokens = maxTokens(prev.Tokens, tok)
		if !at.IsZero() && (prev.At.IsZero() || at.Before(prev.At)) {
			prev.At = at
		}
		return
	}
	index[key] = len(r.Messages)
	r.Messages = append(r.Messages, Message{At: at, Model: m.Model, Tokens: tok, Sidechain: sidechain || l.Sidechain})
}

// SessionKey is the session id of the main transcript ("" when unknown).
func (r Reading) SessionKey() string { return r.session }

// Bounds are the first and last timestamp, also for unordered lines.
func (r Reading) Bounds() (started, ended time.Time) { return r.Started, r.Ended }

// ModelUsage is the token sum and estimated cost of one model.
type ModelUsage struct {
	Tokens telemetry.Tokens
	Cost   *float64 // nil: model unknown in the price table
}

// A message with a zero At (unparseable timestamp) falls outside any window with a
// non-zero from.
// UsageBetween sums messages with from <= At < to per model; a zero bound is
// open. Cost is the sum of per-message estimates at each message's own
// timestamp; one unpriced message makes Cost nil for that model.
func (r Reading) UsageBetween(from, to time.Time) map[string]ModelUsage {
	out := map[string]ModelUsage{}
	unknown := map[string]bool{}
	sums := map[string]float64{}
	for _, m := range r.Messages {
		if !from.IsZero() && m.At.Before(from) {
			continue
		}
		if !to.IsZero() && !m.At.Before(to) {
			continue
		}
		u := out[m.Model]
		u.Tokens.Input += m.Tokens.Input
		u.Tokens.Output += m.Tokens.Output
		u.Tokens.CacheRead += m.Tokens.CacheRead
		u.Tokens.CacheWrite5m += m.Tokens.CacheWrite5m
		u.Tokens.CacheWrite1h += m.Tokens.CacheWrite1h
		out[m.Model] = u
		if c, ok := estimate(m.Model, m.At, m.Tokens); ok {
			sums[m.Model] += c
		} else {
			unknown[m.Model] = true
		}
	}
	models := make([]string, 0, len(out))
	for k := range out {
		models = append(models, k)
	}
	sort.Strings(models)
	for _, k := range models {
		u := out[k]
		if !unknown[k] {
			c := sums[k]
			u.Cost = &c
		}
		out[k] = u
	}
	return out
}
