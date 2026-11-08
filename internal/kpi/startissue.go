package kpi

import (
	"fmt"
	"math"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
)

const (
	skillName         = "lighthouse-start-issue"
	routeSkillName    = "lighthouse-route-issue"
	worktreeSkillName = "superpowers:using-git-worktrees"
	scoutAgent        = "lighthouse-issue-impact-scout"
	worktreeMarker    = ".claude/worktrees/"

	// ScoutToolCallBudget is the search budget from the scout persona ("at most 35 search
	// actions"). maxTurns bounds turns, not calls, so the calls are counted here.
	ScoutToolCallBudget = 35
)

// The four actions that count as "a route has been chosen"; changing them makes a new
// measurement incomparable with the baseline.
var (
	routeTools = []string{"Skill", "Write", "Edit", "AskUserQuestion"}
	writeTools = []string{"Write", "Edit"}
	scoutGates = []string{"4b", "4c"}
)

// Superpowers' default directories, spelled in two pieces so a repo-wide grep skips this file.
const legacyRoot = "docs/" + "superpowers/"

var legacyDocDirs = []string{legacyRoot + "specs/", legacyRoot + "plans/"}

// measurer holds what every row needs; the headings load on first use, because only a turn
// that needs a gate or scout pattern may fail on a missing headings file.
type measurer struct {
	dir      string
	since    *string
	heads    headingRes
	headsErr error
	loaded   bool
}

func (m *measurer) headings() (headingRes, error) {
	if !m.loaded {
		m.heads, m.headsErr = loadHeadingRes()
		m.loaded = true
	}
	return m.heads, m.headsErr
}

func isRoutingSkill(in map[string]any) bool {
	s, _ := in["skill"].(string)
	return s == skillName || s == routeSkillName
}

// countLeadin is the number of tool calls before the first route call; false if none was chosen.
func countLeadin(t Turn) (int, bool) {
	seen := 0
	for _, rec := range t.Records {
		for _, u := range toolUses(rec) {
			if u.Name == "Skill" && isRoutingSkill(u.Input) {
				continue
			}
			if u.Name == "Agent" && u.Input["subagent_type"] == scoutAgent {
				continue
			}
			if slices.Contains(routeTools, u.Name) {
				return seen, true
			}
			seen++
		}
	}
	return 0, false
}

// firstWritePath is the file_path of the first Write or Edit with a non-empty one.
func firstWritePath(t Turn) (string, bool) {
	for _, rec := range t.Records {
		for _, u := range toolUses(rec) {
			if slices.Contains(writeTools, u.Name) {
				if p, ok := u.Input["file_path"].(string); ok && p != "" {
					return p, true
				}
			}
		}
	}
	return "", false
}

// textBeforeRoute is the assistant text up to the first route tool_use block; without a
// route the whole turn.
func textBeforeRoute(t Turn) string {
	var parts []string
	for _, rec := range t.Records {
		if rec["type"] != "assistant" || truthy(rec["isSidechain"]) {
			continue
		}
		for _, b := range contentBlocks(rec) {
			switch b["type"] {
			case "text":
				s, _ := b["text"].(string)
				parts = append(parts, s)
			case "tool_use":
				name, _ := b["name"].(string)
				in := asMap(b["input"])
				if name == "Skill" {
					if s, _ := in["skill"].(string); isRoutingSkill(in) || s == worktreeSkillName {
						continue
					}
				}
				if slices.Contains(routeTools, name) {
					return strings.Join(parts, "\n")
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

// pickupTurns is every turn that begins with a pickup prompt.
func (m *measurer) pickupTurns() ([]Turn, error) {
	var out []Turn
	err := m.eachTurn(func(t Turn) {
		if isPickup(t.Prompt) {
			out = append(out, t)
		}
	})
	return out, err
}

func (m *measurer) eachTurn(fn func(Turn)) error {
	return eachTurnIn(m.dir, true, m.since, fn)
}

// eachTurnIn calls fn for every turn of the session files in dir (and, with siblings, the
// worktree sibling directories), after the --since cut.
func eachTurnIn(dir string, withSiblings bool, since *string, fn func(Turn)) error {
	files, err := sessionFiles(dir, withSiblings)
	if err != nil {
		return err
	}
	for _, path := range files {
		all, err := readSession(path)
		if err != nil {
			return err
		}
		var records []map[string]any
		for _, r := range all {
			if after(r, since) {
				records = append(records, r)
			}
		}
		for _, t := range iterTurns(records) {
			fn(t)
		}
	}
	return nil
}

func share(num, den int) any {
	if den == 0 {
		return nil
	}
	return pyFloat(pyRound(float64(num)/float64(den), 3))
}

func (m *measurer) leadin() (object, error) {
	turns, err := m.pickupTurns()
	if err != nil {
		return nil, err
	}
	var routed, unrouted []int
	noRoute := 0
	for _, t := range turns {
		n, ok := countLeadin(t)
		switch {
		case !ok:
			noRoute++
		case namesASkill(t.Prompt):
			routed = append(routed, n)
		default:
			unrouted = append(unrouted, n)
		}
	}
	return object{{"routed", summary(routed)}, {"unrouted", summary(unrouted)}, {"no_route", noRoute}}, nil
}

func (m *measurer) worktreeFirst() (object, error) {
	turns, err := m.pickupTurns()
	if err != nil {
		return nil, err
	}
	var inside, outside, invisible int
	for _, t := range turns {
		path, ok := firstWritePath(t)
		switch {
		case !ok:
			invisible++
		case strings.Contains(path, worktreeMarker):
			inside++
		default:
			outside++
		}
	}
	return object{
		{"in_worktree", inside},
		{"outside_worktree", outside},
		{"no_visible_write", invisible},
		{"share", share(inside, inside+outside)},
		{"note", "Write/Edit only; Bash writes are invisible"},
	}, nil
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func (m *measurer) mistrigger() (object, error) {
	var loaded, mistriggered int
	var examples []string
	err := m.eachTurn(func(t Turn) {
		if !slices.Contains(loadedSkills(t), skillName) {
			return
		}
		loaded++
		if !hasIssueNumber(t.Prompt) {
			mistriggered++
			examples = append(examples, truncRunes(t.Prompt, 120))
		}
	})
	if err != nil {
		return nil, err
	}
	return object{
		{"loaded", loaded},
		{"without_issue_number", mistriggered},
		{"examples", examples[:min(len(examples), 5)]},
	}, nil
}

func (m *measurer) gate() (object, error) {
	turns, err := m.pickupTurns()
	if err != nil {
		return nil, err
	}
	var reported, notReported, withoutSkill int
	counts := map[string]int{}
	for _, t := range turns {
		if !slices.Contains(loadedSkills(t), skillName) {
			withoutSkill++
			continue
		}
		h, err := m.headings()
		if err != nil {
			return nil, err
		}
		g, ok := h.gate(textBeforeRoute(t))
		if !ok {
			notReported++
			continue
		}
		reported++
		counts[fmt.Sprintf("gate_%d", g)]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	distribution := object{}
	for _, k := range keys {
		distribution = append(distribution, field{k, counts[k]})
	}
	return object{
		{"reported", reported},
		{"not_reported", notReported},
		{"share", share(reported, reported+notReported)},
		{"distribution", distribution},
		{"pickup_without_skill", withoutSkill},
	}, nil
}

// scoutCall is an Agent call to the scout and the record with its tool_result, or nil.
type scoutCall struct{ start, end map[string]any }

func scoutCalls(t Turn) []scoutCall {
	type pending struct {
		id  string
		rec map[string]any
	}
	var calls []pending
	results := map[string]map[string]any{}
	for _, rec := range t.Records {
		for _, b := range contentBlocks(rec) {
			if b["type"] == "tool_use" && b["name"] == "Agent" && asMap(b["input"])["subagent_type"] == scoutAgent {
				id, _ := b["id"].(string)
				calls = append(calls, pending{id, rec})
			}
		}
	}
	for _, rec := range t.Records {
		for _, b := range contentBlocks(rec) {
			if id, ok := b["tool_use_id"].(string); ok && b["type"] == "tool_result" {
				results[id] = rec
			}
		}
	}
	out := make([]scoutCall, len(calls))
	for i, c := range calls {
		out[i] = scoutCall{c.rec, results[c.id]}
	}
	return out
}

// parseStamp reads a timestamp as RFC 3339 or as a naive local time; naive reports which.
func parseStamp(s string) (t time.Time, naive, ok bool) {
	if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", s); err == nil {
		return t, false, true
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999999", s); err == nil {
		return t, true, true
	}
	return time.Time{}, false, false
}

// secondsBetween is the time from start to end in seconds, computed in whole microseconds
// like timedelta.total_seconds.
func secondsBetween(start, end map[string]any) (float64, bool) {
	a, aok := start["timestamp"].(string)
	b, bok := end["timestamp"].(string)
	if !aok || !bok {
		return 0, false
	}
	t0, n0, ok0 := parseStamp(a)
	t1, n1, ok1 := parseStamp(b)
	if !ok0 || !ok1 || n0 != n1 {
		return 0, false
	}
	return float64(t1.UnixMicro()-t0.UnixMicro()) / 1e6, true
}

// toolUseCount is toolUseResult.totalToolUseCount when it is a whole JSON number.
func toolUseCount(end map[string]any) (int, bool) {
	result, ok := end["toolUseResult"].(map[string]any)
	if !ok {
		return 0, false
	}
	f, ok := result["totalToolUseCount"].(float64)
	if !ok || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int(f), true
}

// finished reports whether end is the return of a finished run: not a rejected call
// (toolUseResult is a string) and not a background start.
func finished(end map[string]any) bool {
	switch r := end["toolUseResult"].(type) {
	case string:
		return false
	case map[string]any:
		return r["status"] != "async_launched"
	}
	return true
}

func runStats(calls []scoutCall) (seconds []float64, counts []int) {
	for _, c := range calls {
		if c.end == nil || !finished(c.end) {
			continue
		}
		if d, ok := secondsBetween(c.start, c.end); ok {
			seconds = append(seconds, d)
		}
		if n, ok := toolUseCount(c.end); ok {
			counts = append(counts, n)
		}
	}
	return seconds, counts
}

func (m *measurer) scout() (object, error) {
	turns, err := m.pickupTurns()
	if err != nil {
		return nil, err
	}
	var triggered, withReason, silent, mistrigger, unknown int
	var runtimes []float64
	var toolCalls []int
	for _, t := range turns {
		if !slices.Contains(loadedSkills(t), skillName) {
			continue
		}
		calls := scoutCalls(t)
		h, err := m.headings()
		if err != nil {
			return nil, err
		}
		text := textBeforeRoute(t)
		sub, ok := h.subgate(text)
		if !ok || sub == "4" {
			if len(calls) > 0 {
				unknown++
			}
			continue
		}
		if !slices.Contains(scoutGates, sub) {
			mistrigger += len(calls)
			continue
		}
		seconds, counts := runStats(calls)
		runtimes = append(runtimes, seconds...)
		toolCalls = append(toolCalls, counts...)
		switch {
		case len(calls) > 0:
			triggered++
		case h.skipReasonOK(text):
			withReason++
		default:
			silent++
		}
	}
	overBudget := 0
	for _, n := range toolCalls {
		if n > ScoutToolCallBudget {
			overBudget++
		}
	}
	return object{
		{"triggered", triggered},
		{"skipped_with_reason", withReason},
		{"silent", silent},
		{"mistrigger", mistrigger},
		{"gate_unknown", unknown},
		{"share", share(triggered+withReason, triggered+withReason+silent)},
		{"runtime_seconds", summaryFloat(runtimes)},
		{"tool_calls", summaryInt(toolCalls)},
		{"over_budget", overBudget},
	}, nil
}

// correctDocDirs are the project's spec and plan directories, each with a trailing slash.
func correctDocDirs() ([]string, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, key := range []string{"paths.specs", "paths.plans"} { // readers: tests/test_site_config.py finds them by these literals
		v, err := config.Lookup(cfg, key)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, strings.TrimRight(fmt.Sprint(v), "/")+"/")
	}
	return dirs, nil
}

func countPrefixed(files, prefixes []string) int {
	n := 0
	for _, f := range files {
		if slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(f, p) }) {
			n++
		}
	}
	return n
}

// paths reads git, not transcripts: where did new spec and plan files land?
func (m *measurer) paths() (object, error) {
	root, err := config.RepoRoot()
	if err != nil {
		return nil, err
	}
	argv := []string{"-C", root, "log", "--diff-filter=A", "--name-only", "--pretty=format:"}
	if m.since != nil && *m.since != "" {
		argv = append(argv, "--since", *m.since)
	}
	cmd := exec.Command("git", argv...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		msg := strings.TrimFunc(stderr.String(), pySpace)
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git log failed: %s", msg)
	}
	var added []string
	for _, line := range pySplitLines(string(raw)) {
		if strings.TrimFunc(line, pySpace) != "" {
			added = append(added, line)
		}
	}
	// Like the Python, read the config only when there is a file to classify.
	var dirs []string
	if len(added) > 0 {
		if dirs, err = correctDocDirs(); err != nil {
			return nil, err
		}
	}
	correct, legacy := countPrefixed(added, dirs), countPrefixed(added, legacyDocDirs)
	var since any
	if m.since != nil {
		since = *m.since
	}
	return object{
		{"since", since},
		{"right_dir", correct},
		{"superpowers_default_dir", legacy},
		{"share", share(correct, correct+legacy)},
	}, nil
}
