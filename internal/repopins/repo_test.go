// Package repopins holds tests that pin files of this repository against each other: workflows,
// instruction files, the pack sources and the docs. It has only test files.
package repopins

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot returns the directory that holds go.mod, found by walking up from the package
// directory, so the tests do not depend on the working directory depth.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

// read returns the text of the repository file at rel and fails the test when it cannot be
// read, so a renamed or removed file never leaves a pin silently green.
func read(t *testing.T, rel ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{repoRoot(t)}, rel...)...)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

var jobKey = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):`)

// jobs splits a workflow into its jobs: the text under `jobs:`, one entry per key indented by
// two spaces. The map value is the job's own lines.
func jobs(text string) map[string]string {
	out := map[string]string{}
	inJobs := false
	var name string
	var body []string
	flush := func() {
		if name != "" {
			out[name] = strings.Join(body, "\n")
		}
		name, body = "", nil
	}
	for _, line := range strings.Split(text, "\n") {
		if !inJobs {
			inJobs = line == "jobs:"
			continue
		}
		if line != "" && line[0] != ' ' && line[0] != '#' {
			break // the next top-level key ends the jobs
		}
		if m := jobKey.FindStringSubmatch(line); m != nil {
			flush()
			name = m[1]
		}
		if name != "" {
			body = append(body, line)
		}
	}
	flush()
	return out
}

var blockScalar = regexp.MustCompile(`^[|>][+-]?$`)

var runLine = regexp.MustCompile(`^(\s*(?:- )?)run:\s*(.*?)\s*$`)

// runBlocks returns every `run:` value of a workflow: the inline value, or for `run: |` and
// `run: >` the lines indented deeper than the key.
func runBlocks(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := runLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if !blockScalar.MatchString(m[2]) {
			out = append(out, m[2])
			continue
		}
		keyIndent := len(m[1])
		var block []string
		for i+1 < len(lines) {
			next := lines[i+1]
			if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " ")) < keyIndent+1 {
				break
			}
			block = append(block, strings.TrimSpace(next))
			i++
		}
		out = append(out, strings.TrimSpace(strings.Join(block, "\n")))
	}
	return out
}

func TestRepoRootFindsGoMod(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repoRoot %s has no go.mod: %v", root, err)
	}
	t.Chdir(filepath.Join(root, "internal"))
	if got := repoRoot(t); got != root {
		t.Errorf("repoRoot from internal/ = %s, want %s", got, root)
	}
}

func TestJobsSplitsAWorkflowPerJob(t *testing.T) {
	text := "name: x\non: push\njobs:\n  a:\n    runs-on: u\n    steps:\n      - run: one\n\n  b:\n    # note\n    steps:\n      - run: two\nother: 1\n"
	got := jobs(text)
	if len(got) != 2 {
		t.Fatalf("got %d jobs %v, want 2", len(got), got)
	}
	if !strings.Contains(got["a"], "run: one") || strings.Contains(got["a"], "run: two") {
		t.Errorf("job a = %q", got["a"])
	}
	if !strings.Contains(got["b"], "run: two") || strings.Contains(got["b"], "other") {
		t.Errorf("job b = %q", got["b"])
	}
}

func TestRunBlocksReturnsInlineAndBlockValues(t *testing.T) {
	text := "    steps:\n      - run: go build ./...\n      - name: x\n        run: |\n          echo a\n\n          echo b\n      - uses: y\n"
	got := runBlocks(text)
	want := []string{"go build ./...", "echo a\n\necho b"}
	if len(got) != len(want) {
		t.Fatalf("runBlocks = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("runBlocks[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
