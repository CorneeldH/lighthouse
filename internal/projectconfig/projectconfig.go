// Package projectconfig fills in lighthouse.toml without rewriting it (spec, init punt 2).
//
// Missing tables and keys are added as text: a missing table at the end, a missing key
// after the last key line of its table. Comments, order and any unknown table stay
// exactly as they are. A key whose table is not a plain `[name]` section (inline, dotted
// or only implied by a subtable) is not placed but reported, to be added by hand.
package projectconfig

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// KV is one key with its value as a TOML literal, such as `"en"`.
type KV struct {
	Key   string
	Value string
}

// Table is a table with the keys it should have.
type Table struct {
	Name string
	Keys []KV
}

// header matches a table or array-of-tables header line.
var header = regexp.MustCompile(`^\s*\[\[?\s*[A-Za-z0-9_."' -]+\s*\]\]?\s*(#.*)?$`)

// AppendMissing adds to existing what want has and existing lacks. added lists the
// keys it inserted, manual the missing keys it could not place safely.
func AppendMissing(existing []byte, want []Table) (out []byte, added []string, manual []string, err error) {
	var parsed map[string]any
	md, err := toml.Decode(string(existing), &parsed)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lighthouse.toml: not valid TOML: %w", err)
	}
	text := string(existing)
	var tail strings.Builder
	for _, t := range want {
		path := strings.Split(t.Name, ".")
		var missing []KV
		for _, kv := range t.Keys {
			if !md.IsDefined(append(append([]string{}, path...), kv.Key)...) {
				missing = append(missing, kv)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if !md.IsDefined(path...) {
			tail.WriteString("[" + t.Name + "]\n")
			for _, kv := range missing {
				tail.WriteString(kv.Key + " = " + kv.Value + "\n")
				added = append(added, t.Name+"."+kv.Key)
			}
			tail.WriteString("\n")
			continue
		}
		var ok bool
		text, ok = insertInSection(text, t.Name, missing)
		for _, kv := range missing {
			if ok {
				added = append(added, t.Name+"."+kv.Key)
			} else {
				manual = append(manual, t.Name+"."+kv.Key)
			}
		}
	}
	if tail.Len() > 0 {
		if text != "" {
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += "\n"
		}
		text += strings.TrimSuffix(tail.String(), "\n")
	}
	var check map[string]any
	if _, err := toml.Decode(text, &check); err != nil {
		return nil, nil, nil, fmt.Errorf("lighthouse.toml: adding %s would break the file: %w", strings.Join(added, ", "), err)
	}
	if text == string(existing) {
		return existing, added, manual, nil
	}
	return []byte(text), added, manual, nil
}

// insertInSection puts keys after the last key line of the `[name]` section. It reports
// false when the file has no such header line.
func insertInSection(text, name string, keys []KV) (string, bool) {
	own := regexp.MustCompile(`^\s*\[\s*` + regexp.QuoteMeta(name) + `\s*\]\s*(#.*)?$`)
	lines := strings.SplitAfter(text, "\n")
	start := -1
	for i, l := range lines {
		if own.MatchString(strings.TrimRight(l, "\r\n")) {
			start = i
			break
		}
	}
	if start < 0 {
		return text, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if header.MatchString(strings.TrimRight(lines[i], "\r\n")) {
			end = i
			break
		}
	}
	last := start
	for i := start + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			last = i
		}
	}
	var ins bytes.Buffer
	if !strings.HasSuffix(lines[last], "\n") {
		ins.WriteString("\n")
	}
	for _, kv := range keys {
		ins.WriteString(kv.Key + " = " + kv.Value + "\n")
	}
	out := strings.Join(lines[:last+1], "") + ins.String() + strings.Join(lines[last+1:], "")
	return out, true
}

var (
	githubRepo = []*regexp.Regexp{
		regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`),
		regexp.MustCompile(`^ssh://git@github\.com(?::\d+)?/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	}
	repoPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// ForgeRepo returns owner/name for a GitHub remote URL.
func ForgeRepo(remoteURL string) (string, bool) {
	for _, re := range githubRepo {
		m := re.FindStringSubmatch(remoteURL)
		if m != nil && repoPart.MatchString(m[1]) && repoPart.MatchString(m[2]) {
			return m[1] + "/" + m[2], true
		}
	}
	return "", false
}

// CoreTables are the tables pack/core proposes: [project] always, [forge] when the
// repo has a GitHub remote.
func CoreTables(repo string, haveRepo bool) []Table {
	tables := []Table{{Name: "project", Keys: []KV{{Key: "language", Value: `"en"`}}}}
	if haveRepo {
		tables = append(tables, Table{Name: "forge", Keys: []KV{
			{Key: "kind", Value: `"github"`},
			{Key: "repo", Value: strconv.Quote(repo)},
		}})
	}
	return tables
}

var (
	fragmentHeader = regexp.MustCompile(`^\[([a-z_]+(?:\.[a-z_]+)*)\]$`)
	fragmentKey    = regexp.MustCompile(`^([a-z_]+)\s*=\s*(.*)$`)
)

// ParseFragment reads the lighthouse.toml.fragment of a package (plan M0 (c),
// design choice 4): `[table]` header lines and one-line `key = value` lines, plus
// comments and blank lines, which are dropped. Each value is kept as it is written, so
// AppendMissing can insert it without rewriting it. Every error names the line.
func ParseFragment(data []byte) ([]Table, error) {
	var tables []Table
	seen := map[string]bool{} // table names and table.key paths
	header := 0               // line of the current table's header
	closeTable := func() error {
		if len(tables) > 0 && len(tables[len(tables)-1].Keys) == 0 {
			return fmt.Errorf("line %d: table [%s] has no keys", header, tables[len(tables)-1].Name)
		}
		return nil
	}
	for i, raw := range strings.Split(string(data), "\n") {
		n := i + 1
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := fragmentHeader.FindStringSubmatch(line); m != nil {
			if err := closeTable(); err != nil {
				return nil, err
			}
			if seen[m[1]] {
				return nil, fmt.Errorf("line %d: [%s] is already defined", n, m[1])
			}
			seen[m[1]] = true
			tables = append(tables, Table{Name: m[1]})
			header = n
			continue
		}
		m := fragmentKey.FindStringSubmatch(line)
		switch {
		case m == nil:
			return nil, fmt.Errorf("line %d: not a [table] header or a key = value line", n)
		case len(tables) == 0:
			return nil, fmt.Errorf("line %d: key %s before the first [table]", n, m[1])
		}
		t := &tables[len(tables)-1]
		path := t.Name + "." + m[1]
		if seen[path] {
			return nil, fmt.Errorf("line %d: %s is already defined", n, path)
		}
		value := strings.TrimSpace(m[2])
		if !oneValue(value) {
			return nil, fmt.Errorf("line %d: %s: the value is not one TOML value on one line, without a comment", n, path)
		}
		seen[path] = true
		t.Keys = append(t.Keys, KV{Key: m[1], Value: value})
	}
	if err := closeTable(); err != nil {
		return nil, err
	}
	// The whole fragment must be TOML too: this catches a table that is also a key,
	// such as [t] a = 1 with [t.a].
	var v map[string]any
	if _, err := toml.Decode(string(data), &v); err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return nil, fmt.Errorf("line %d: %s", pe.Position.Line, pe.Message)
		}
		return nil, err
	}
	return tables, nil
}

// oneValue reports whether value is exactly one TOML value. Inside an array, a trailing
// comment would swallow the closing bracket, so `[value]` fails for one.
func oneValue(value string) bool {
	if value == "" {
		return false
	}
	var v map[string]any
	_, err := toml.Decode("v = ["+value+"]", &v)
	return err == nil
}
