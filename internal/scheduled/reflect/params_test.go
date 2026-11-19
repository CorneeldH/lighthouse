package reflect

import (
	"strings"
	"testing"
	"time"
)

// Classification: pure logic. These tests fail if a param loses its default, a typo in a key is
// silently ignored, or a value that would make the job write outside its directory is accepted.

func TestDefaultsAreTheSpecDefaults(t *testing.T) {
	c, err := parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.command != "/paia-reflect-skills" || c.outputDir != "docs/developer/reflect" || c.minSessions != 20 ||
		c.windowMin != 14 || c.windowMax != 90 || c.minLines != 20 || c.maxCorpus != 400000 || c.maxPrompt != 400 ||
		c.maxRuntime != 60*time.Minute {
		t.Errorf("defaults = %+v", c)
	}
	if got := strings.Join(c.tools, " "); got != "Read Glob Grep Write" {
		t.Errorf("tools = %q, want the toolset without Bash", got)
	}
}

func TestParseRejectsWhatWouldRunWrong(t *testing.T) {
	for name, params := range map[string]map[string]any{
		"unknown key":      {"min_session": int64(5)},
		"output_dir up":    {"output_dir": "../x"},
		"output_dir .git":  {"output_dir": ".git/hooks"},
		"output_dir abs":   {"output_dir": "/etc"},
		"window inverted":  {"window_min_days": int64(30), "window_max_days": int64(10)},
		"min_sessions 0":   {"min_sessions": int64(0)},
		"tiny corpus":      {"max_corpus_bytes": int64(10)},
		"empty command":    {"command": " "},
		"multiline cmd":    {"command": "/a\n/b"},
		"command not str":  {"command": int64(1)},
		"tools empty list": {"tools": []any{}},
	} {
		if _, err := parse(params); err == nil {
			t.Errorf("%s: parse accepted %v", name, params)
		}
	}
}

func TestParseTakesOverrides(t *testing.T) {
	c, err := parse(map[string]any{"output_dir": "docs/r", "min_sessions": int64(3), "tools": "Read Write", "max_runtime": "10m"})
	if err != nil {
		t.Fatal(err)
	}
	if c.outputDir != "docs/r" || c.minSessions != 3 || len(c.tools) != 2 || c.maxRuntime != 10*time.Minute {
		t.Errorf("config = %+v", c)
	}
}
