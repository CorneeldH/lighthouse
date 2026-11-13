package kpi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

const worktreeDirInfix = "--claude-worktrees-"

// Turn is a human prompt and every record that followed it up to the next human prompt.
type Turn struct {
	Prompt  string
	Records []map[string]any
}

type toolUse struct {
	Name  string
	Input map[string]any
	ID    string
}

// pySpace reports whether r is whitespace for Python's str.strip.
func pySpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pySplitLines splits like Python's str.splitlines().
func pySplitLines(s string) []string {
	var lines []string
	rs := []rune(s)
	var cur []rune
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		case '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
		default:
			cur = append(cur, r)
			continue
		}
		lines = append(lines, string(cur))
		cur = cur[:0]
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	return lines
}

// readSession reads one session file. Broken lines and lines that are no JSON object are
// skipped, not repaired; invalid UTF-8 becomes U+FFFD.
func readSession(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []map[string]any
	for _, line := range pySplitLines(strings.ToValidUTF8(string(data), "�")) {
		line = strings.TrimFunc(line, pySpace)
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec == nil {
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}

func jsonlIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) || errors.Is(err, syscall.ENOTDIR) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// sessionFiles lists the .jsonl files of dir and, with withSiblings, of every
// <dir>--claude-worktrees-* sibling, sorted like pathlib.
func sessionFiles(dir string, withSiblings bool) ([]string, error) {
	dir = pyPath(dir) // "a/" must have parent ".", as for pathlib
	dirs := []string{dir}
	if withSiblings {
		parent := filepath.Dir(dir)
		prefix := filepath.Base(dir) + worktreeDirInfix
		entries, err := os.ReadDir(parent)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), prefix) {
				continue
			}
			p := filepath.Join(parent, e.Name())
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				dirs = append(dirs, p)
			}
		}
	}
	var files []string
	for _, d := range dirs {
		f, err := jsonlIn(d)
		if err != nil {
			return nil, err
		}
		files = append(files, f...)
	}
	sortPaths(files)
	return files, nil
}

// after reports whether rec is on or after since (compared as the first 10 runes of
// its timestamp); a record without a string timestamp always counts.
func after(rec map[string]any, since *string) bool {
	if since == nil || *since == "" {
		return true
	}
	stamp, ok := rec["timestamp"].(string)
	if !ok {
		return true
	}
	if r := []rune(stamp); len(r) > 10 {
		stamp = string(r[:10])
	}
	return stamp >= *since
}

// truthy is Python's truthiness for a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// contentBlocks returns the object blocks of message.content when it is a list.
func contentBlocks(rec map[string]any) []map[string]any {
	list, ok := asMap(rec["message"])["content"].([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, b := range list {
		if m, ok := b.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// promptText joins the text blocks of a record; "" if there is none.
func promptText(rec map[string]any) string {
	switch c := asMap(rec["message"])["content"].(type) {
	case string:
		return c
	case []any:
		var texts []string
		for _, b := range contentBlocks(rec) {
			if b["type"] == "text" {
				t, _ := b["text"].(string)
				texts = append(texts, t)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

func isUserPrompt(rec map[string]any) bool {
	if rec["type"] != "user" || truthy(rec["isSidechain"]) || truthy(rec["isMeta"]) {
		return false
	}
	if ut, present := rec["userType"]; present && ut != nil && ut != "external" {
		return false
	}
	return strings.TrimFunc(promptText(rec), pySpace) != ""
}

// iterTurns groups records into turns; records before the first prompt are dropped.
func iterTurns(records []map[string]any) []Turn {
	var turns []Turn
	started := false
	var cur Turn
	for _, rec := range records {
		if isUserPrompt(rec) {
			if started {
				turns = append(turns, cur)
			}
			cur, started = Turn{Prompt: promptText(rec)}, true
		} else if started {
			cur.Records = append(cur.Records, rec)
		}
	}
	if started {
		turns = append(turns, cur)
	}
	return turns
}

// toolUses lists the tool_use blocks of an assistant record outside a sidechain.
func toolUses(rec map[string]any) []toolUse {
	if rec["type"] != "assistant" || truthy(rec["isSidechain"]) {
		return nil
	}
	var out []toolUse
	for _, b := range contentBlocks(rec) {
		if b["type"] != "tool_use" {
			continue
		}
		name, _ := b["name"].(string)
		id, _ := b["id"].(string)
		in := asMap(b["input"])
		if in == nil {
			in = map[string]any{}
		}
		out = append(out, toolUse{Name: name, Input: in, ID: id})
	}
	return out
}

// loadedSkills lists the skill names of every Skill call in the turn.
func loadedSkills(t Turn) []string {
	var out []string
	for _, rec := range t.Records {
		for _, u := range toolUses(rec) {
			if s, ok := u.Input["skill"].(string); ok && u.Name == "Skill" {
				out = append(out, s)
			}
		}
	}
	return out
}
