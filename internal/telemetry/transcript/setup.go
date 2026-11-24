package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SkillLoad is one skill loaded into the session: its name, a hash of its
// normalised text and the superpowers version in its base directory. The text
// itself is never kept.
type SkillLoad struct {
	Name               string
	Hash               string
	SuperpowersVersion string
}

// Setup is the working set-up of a session or a window of it; nil is unknown.
type Setup struct {
	Model, Effort, Harness, HarnessVersion, SuperpowersVersion, SkillsHash, Lane *string
}

const skillMarker = "Base directory for this skill:"

var (
	commandName = regexp.MustCompile(`<command-name>\s*/?([^<\s]+)\s*</command-name>`)
	spVersion   = regexp.MustCompile(`/superpowers/([^/]+)/skills/`)
)

type blockJSON struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input struct {
		Skill string `json:"skill"`
	} `json:"input"`
}

// contentBlocks returns the blocks of a message; a plain string is one text block.
func contentBlocks(raw json.RawMessage) []blockJSON {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []blockJSON{{Type: "text", Text: s}}
	}
	var bs []blockJSON
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

// skillOf records skill names announced by a command tag or a Skill tool use,
// and returns the load when the line carries skill text.
func (r *Reading) skillOf(typ string, content json.RawMessage) *SkillLoad {
	var load *SkillLoad
	for _, b := range contentBlocks(content) {
		switch {
		case typ == "assistant" && b.Type == "tool_use" && b.Name == "Skill" && b.Input.Skill != "":
			r.announce(strings.TrimPrefix(b.Input.Skill, "/"))
		case typ == "user" && b.Type == "text" && strings.Contains(b.Text, skillMarker):
			load = r.loadOf(b.Text)
		case typ == "user" && b.Type == "text":
			if m := commandName.FindStringSubmatch(b.Text); m != nil {
				r.announce(m[1])
			}
		}
	}
	return load
}

// announce queues a skill name once; the queue is per file.
func (r *Reading) announce(name string) {
	for _, p := range r.pending {
		if p == name {
			return
		}
	}
	r.pending = append(r.pending, name)
}

func (r *Reading) loadOf(text string) *SkillLoad {
	i := strings.Index(text, skillMarker)
	lineEnd := strings.IndexByte(text[i:], '\n')
	base := text[i+len(skillMarker):]
	if lineEnd >= 0 {
		base = text[i+len(skillMarker) : i+lineEnd]
	}
	base = strings.TrimSpace(base)
	name := ""
	slash := strings.LastIndexByte(base, '/')
	if slash >= 0 {
		name = base[slash+1:]
	}
	m := spVersion.FindStringSubmatch(base)
	// Pair strictly: an announced name counts only when its part after ':' is the
	// skill directory. Stale announcements (/clear, /model) before it are dropped.
	for i, p := range r.pending {
		if p[strings.LastIndexByte(p, ':')+1:] == name {
			name, r.pending = p, r.pending[i+1:]
			break
		}
	}
	if m != nil && !strings.Contains(name, ":") {
		name = "superpowers:" + name
	}
	l := &SkillLoad{Name: name, Hash: normalisedHash(text)}
	if m != nil {
		l.SuperpowersVersion = m[1]
	}
	return l
}

// normalisedHash hashes the skill text without the base directory line and
// without everything from a line that starts with ARGUMENTS:.
func normalisedHash(text string) string {
	var keep []string
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, "ARGUMENTS:") {
			break
		}
		if strings.HasPrefix(ln, skillMarker) {
			continue
		}
		keep = append(keep, ln)
	}
	sum := sha256.Sum256([]byte(strings.Join(keep, "\n")))
	return hex.EncodeToString(sum[:])
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SetupBetween derives the set-up of the window [from, to): model, effort and
// harness version from the lines inside it; lane, skills hash and superpowers
// version from everything up to `to`. A zero bound is open.
func (r Reading) SetupBetween(from, to time.Time) Setup {
	var s Setup
	var effort, version, sp string
	inWindow := false
	skills := map[string]string{}
	lane := ""
	for _, e := range r.Entries {
		if !to.IsZero() && !e.At.IsZero() && !e.At.Before(to) {
			continue
		}
		if e.Skill != nil {
			skills[e.Skill.Name] = e.Skill.Hash
			if e.Skill.SuperpowersVersion != "" {
				sp = e.Skill.SuperpowersVersion
			}
			switch e.Skill.Name {
			case "superpowers:subagent-driven-development":
				lane = "sdd"
			case "superpowers:executing-plans":
				lane = "native"
			}
		}
		if !from.IsZero() && e.At.Before(from) {
			continue
		}
		inWindow = true
		if e.Version != "" {
			version = e.Version
		}
		if e.Effort != "" && !e.Sidechain {
			effort = e.Effort
		}
	}
	if inWindow {
		s.Harness = ptr("claude-code")
	}
	s.HarnessVersion, s.Effort = ptr(version), ptr(effort)
	s.SuperpowersVersion, s.Lane = ptr(sp), ptr(lane)
	s.Model = ptr(r.topModel(from, to))
	if len(skills) > 0 {
		names := make([]string, 0, len(skills))
		for n := range skills {
			names = append(names, n)
		}
		sort.Strings(names)
		h := sha256.New()
		for _, n := range names {
			inner := sha256.Sum256([]byte(skills[n]))
			h.Write([]byte(n))
			h.Write([]byte{0})
			h.Write(inner[:])
		}
		s.SkillsHash = ptr(hex.EncodeToString(h.Sum(nil)))
	}
	return s
}

// topModel is the model with the most output tokens on non-sidechain messages
// of the window; ties go to the lexically smaller name.
func (r Reading) topModel(from, to time.Time) string {
	sums := map[string]int64{}
	for _, m := range r.Messages {
		if m.Sidechain || (!from.IsZero() && m.At.Before(from)) || (!to.IsZero() && !m.At.Before(to)) {
			continue
		}
		sums[m.Model] += m.Tokens.Output
	}
	best, top := "", int64(-1)
	for name, n := range sums {
		if n > top || (n == top && name < best) {
			best, top = name, n
		}
	}
	return best
}
