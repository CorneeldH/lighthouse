package history

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecideLevel(t *testing.T) {
	ok := Counts{TrunkPushes: 1, KnownPushes: 10, PRs: 3, Sessions: 1,
		Chain: chainCounts{Commits: 10, CommitsAfterTest: 6}}
	with := func(f func(*Counts)) Counts { c := ok; f(&c); return c }
	tests := []struct {
		name     string
		c        Counts
		level    string
		contains string
	}{
		{"standard", ok, "standard", "codex"},
		{"strict", with(func(c *Counts) { c.Chain.Codex = 1 }), "strict", ""},
		{"two PRs", with(func(c *Counts) { c.PRs = 2 }), "light", "PRs"},
		{"trunk 20.0 percent", with(func(c *Counts) { c.TrunkPushes, c.KnownPushes = 2, 10 }), "light", "trunk"},
		{"no known pushes", with(func(c *Counts) { c.TrunkPushes, c.KnownPushes = 0, 0 }), "light", "trunk"},
		{"no commits", with(func(c *Counts) { c.Chain = chainCounts{Codex: 2} }), "light", "test"},
		{"tested half", with(func(c *Counts) { c.Chain.CommitsAfterTest = 5 }), "standard", ""},
		{"tested below half", with(func(c *Counts) { c.Chain.CommitsAfterTest = 4 }), "light", "test"},
		{"no history", Counts{PRs: 9}, "light", "no history"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideLevel(tt.c)
			if got.Recommended != tt.level {
				t.Fatalf("level %q (%s), want %q", got.Recommended, got.Reason, tt.level)
			}
			if !strings.Contains(got.Reason, tt.contains) || got.Reason == "" {
				t.Fatalf("reason %q lacks %q", got.Reason, tt.contains)
			}
		})
	}
}

func TestAnalyze_TestInSubagentBeforeCommit(t *testing.T) {
	bash := func(ts, cmd string) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"gitBranch":"feat","cwd":"/r","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":%q}}]}}`, ts, cmd)
	}
	parent := writeLines(t,
		`{"type":"pr-link","prNumber":1}`, `{"type":"pr-link","prNumber":2}`, `{"type":"pr-link","prNumber":3}`,
		bash("2026-10-01T10:51:00Z", "git commit -m x"),
		bash("2026-10-01T10:52:00Z", "git push origin feat"))
	sub := writeLines(t, bash("2026-10-01T10:45:00Z", "go test ./..."))
	rep := Analyze([]SessionFiles{{ID: "s", Paths: []string{parent, sub}}}, Env{}, AnalyzeOptions{})
	if rep.Level.Recommended != "standard" {
		t.Fatalf("level %+v", rep.Level)
	}
	// the same commit without the subagent test is untested
	rep = Analyze([]SessionFiles{{ID: "s", Paths: []string{parent}}}, Env{}, AnalyzeOptions{})
	if rep.Level.Recommended != "light" {
		t.Fatalf("level %+v", rep.Level)
	}
}

func TestChainTracker_TestedCommits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cmds   []string
		tested int
	}{
		{"commit message names pytest", []string{`git commit -m "add pytest"`}, 0},
		{"pip install pytest", []string{"pip install pytest", "git commit -m x"}, 0},
		{"test then commit in one command", []string{"go test ./... && git commit -m x"}, 1},
		{"commit then test in one command", []string{"git commit -m x && go test ./..."}, 0},
		{"test in an earlier call", []string{"go test ./...", "git commit -m x"}, 1},
		{"commit before the first test", []string{"git commit -m x", "go test ./..."}, 0},
		{"cargo test", []string{"cd x && cargo test", "git commit -m x"}, 1},
		{"npm test", []string{"npm test", "git commit -m x"}, 1},
		{"uv run pytest", []string{"uv run pytest -q", "git commit -m x"}, 1},
		{"bare pytest", []string{"pytest", "git commit -m x"}, 1},
		{"lighthouse gate", []string{"lighthouse gate", "git commit -m x"}, 1},
		{"env prefix", []string{"CI=1 go test ./...", "git commit -m x"}, 1},
		{"echo go test is not a test", []string{"echo go test", "git commit -m x"}, 0},
	} {
		c := newChainTracker()
		for _, cmd := range tc.cmds {
			c.add(Record{Commands: []string{cmd}}, "s")
		}
		if c.CommitsAfterTest != tc.tested || c.Commits != 1 {
			t.Errorf("%s: commits %d tested %d, want 1/%d", tc.name, c.Commits, c.CommitsAfterTest, tc.tested)
		}
	}
}

func TestChainTracker_Codex(t *testing.T) {
	for cmd, want := range map[string]int{
		"codex exec foo":                  1,
		"cd x && codex review":            1,
		"FOO=1 codex exec x":              1,
		`git commit -m codex`:             0,
		"echo codex":                      0,
		`git commit -m "ask codex later"`: 0,
	} {
		c := newChainTracker()
		c.add(Record{Commands: []string{cmd}}, "s")
		if c.Codex != want {
			t.Errorf("%q: codex %d, want %d", cmd, c.Codex, want)
		}
	}
}
