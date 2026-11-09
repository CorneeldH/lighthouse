package review

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// The cases shared with tests/pack/fixtures/review_pair/: one JSON file per case, run through Run
// with the coin and the tree state as seams, exactly as pytest patches `_coin` and `_tree_state`.
const pairFixtureDir = "../../tests/pack/fixtures/review_pair"

type pairStep struct {
	Cmd          string                     `json:"cmd"`
	Exit         int                        `json:"exit"`
	Write        map[string]json.RawMessage `json:"write"`
	Coin         *bool                      `json:"coin"`
	Tree         *pairTree                  `json:"tree"`
	Files        map[string]json.RawMessage `json:"files"`
	Absent       []string                   `json:"absent"`
	Present      []string                   `json:"present"`
	Stdout       *string                    `json:"stdout"`
	StdoutStarts string                     `json:"stdout_starts"`
	StdoutHas    []string                   `json:"stdout_contains"`
	StdoutLacks  []string                   `json:"stdout_lacks_ci"`
	StdoutLines  []string                   `json:"stdout_lines"`
	StdoutIsFile string                     `json:"stdout_equals_file"`
	StderrHas    []string                   `json:"stderr_contains"`
	StderrLacks  []string                   `json:"stderr_lacks"`
	BlindIDs     []string                   `json:"blind_ids"`
	RowHas       map[string]string          `json:"row_contains"`
	CellOf       map[string][]any           `json:"cell_of"`
	Pipes        *int                       `json:"unescaped_pipes"`
	Merged       [][]string                 `json:"merged_findings"`
}

type pairTree struct {
	Head  string `json:"head"`
	Clean bool   `json:"clean"`
}

type pairCase struct {
	Kind    string                     `json:"kind"`
	Coin    bool                       `json:"coin"`
	Tree    pairTree                   `json:"tree"`
	Attempt map[string]json.RawMessage `json:"attempt"`
	Steps   []pairStep                 `json:"steps"`
}

// pairRoot is a repo root with the findings schemas where DefaultSkillDirs looks for them.
func pairRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	for kind, schema := range map[string]string{"plan": "plan-findings.schema.json", "spec": "spec-findings.schema.json"} {
		data, err := os.ReadFile(filepath.Join(pairFixtureDir, "..", schema))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, DefaultSkillDirs[kind], "findings.schema.json"), string(data))
	}
	return root
}

// writePairValue writes a step file: null removes it, {"text": ...} is verbatim, anything else is
// the JSON as the fixture holds it.
func writePairValue(t *testing.T, path string, raw json.RawMessage) {
	t.Helper()
	if string(raw) == "null" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	var text struct {
		Text *string `json:"text"`
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) == nil && len(probe) == 1 {
		if err := json.Unmarshal(raw, &text); err == nil && text.Text != nil {
			writeFile(t, path, *text.Text)
			return
		}
	}
	writeFile(t, path, string(raw))
}

func loadPairCase(t *testing.T, path string) pairCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c pairCase
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// runPairCase plays a case and returns the attempt directory.
func runPairCase(t *testing.T, c pairCase) string {
	t.Helper()
	root := pairRoot(t)
	attempt := filepath.Join(t.TempDir(), "attempt")
	coin, tree := c.Coin, c.Tree
	deps := PairDeps{
		Coin: func() bool { return coin },
		TreeState: func(got string) (string, bool, error) {
			if got != root {
				t.Errorf("TreeState root = %q, want the repo root %q", got, root)
			}
			return tree.Head, tree.Clean, nil
		},
	}
	meta, err := json.Marshal(map[string]string{"kind": c.Kind, "reviewed_sha": "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(attempt, "attempt.json"), string(meta))
	for name, raw := range c.Attempt {
		writePairValue(t, filepath.Join(attempt, name), raw)
	}
	for i, step := range c.Steps {
		if step.Coin != nil {
			coin = *step.Coin
		}
		if step.Tree != nil {
			tree = *step.Tree
		}
		for name, raw := range step.Write {
			writePairValue(t, filepath.Join(attempt, name), raw)
		}
		var out, errOut bytes.Buffer
		code := RunPair(step.Cmd, attempt, func() (string, error) { return root, nil }, deps, &out, &errOut)
		if code != step.Exit {
			t.Fatalf("step %d %s: exit = %d, want %d; stderr: %s", i, step.Cmd, code, step.Exit, errOut.String())
		}
		checkPairStep(t, i, step, attempt, out.String(), errOut.String())
	}
	return attempt
}

func parseOrdered(t *testing.T, data []byte) any {
	t.Helper()
	v, err := orderedjson.Parse(data)
	if err != nil {
		t.Fatalf("parse %q: %v", data, err)
	}
	return v
}

func pairTableRow(t *testing.T, table, blindID string) string {
	t.Helper()
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "| "+blindID+" |") {
			return line
		}
	}
	t.Fatalf("no row %s in %q", blindID, table)
	return ""
}

func unescapedPipes(line string) int {
	count, escaped := 0, false
	for _, ch := range line {
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '|':
			count++
		}
	}
	return count
}

func checkPairStep(t *testing.T, i int, s pairStep, attempt, out, errOut string) {
	t.Helper()
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(attempt, name))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		return data
	}
	for name, want := range s.Files {
		var text struct {
			Text *string `json:"text"`
		}
		if json.Unmarshal(want, &text) == nil && text.Text != nil {
			if string(read(name)) != *text.Text {
				t.Errorf("step %d: %s = %q, want %q", i, name, read(name), *text.Text)
			}
		} else if got := parseOrdered(t, read(name)); !reflect.DeepEqual(got, parseOrdered(t, want)) {
			t.Errorf("step %d: %s = %s, want %s", i, name, read(name), want)
		}
	}
	for _, name := range s.Absent {
		if _, err := os.Stat(filepath.Join(attempt, name)); err == nil {
			t.Errorf("step %d: %s exists, want it absent", i, name)
		}
	}
	for _, name := range s.Present {
		if _, err := os.Stat(filepath.Join(attempt, name)); err != nil {
			t.Errorf("step %d: %s missing", i, name)
		}
	}
	if s.Stdout != nil && out != *s.Stdout {
		t.Errorf("step %d: stdout = %q, want %q", i, out, *s.Stdout)
	}
	if !strings.HasPrefix(out, s.StdoutStarts) {
		t.Errorf("step %d: stdout = %q, want prefix %q", i, out, s.StdoutStarts)
	}
	for _, w := range s.StdoutHas {
		if !strings.Contains(out, w) {
			t.Errorf("step %d: stdout lacks %q", i, w)
		}
	}
	for _, w := range s.StdoutLacks {
		if strings.Contains(strings.ToLower(out), w) {
			t.Errorf("step %d: stdout contains %q", i, w)
		}
	}
	for _, w := range s.StdoutLines {
		if !slices.Contains(strings.Split(out, "\n"), w) {
			t.Errorf("step %d: stdout has no line %q", i, w)
		}
	}
	if s.StdoutIsFile != "" && string(read(s.StdoutIsFile)) != out {
		t.Errorf("step %d: %s differs from stdout", i, s.StdoutIsFile)
	}
	for _, w := range s.StderrHas {
		if !strings.Contains(errOut, w) {
			t.Errorf("step %d: stderr = %q, want it to contain %q", i, errOut, w)
		}
	}
	for _, w := range s.StderrLacks {
		if strings.Contains(errOut, w) {
			t.Errorf("step %d: stderr = %q, must not contain %q", i, errOut, w)
		}
	}
	checkPairTable(t, i, s, out)
	if s.Merged != nil {
		var merged struct {
			Findings []struct{ Problem, Severity string } `json:"findings"`
		}
		if err := json.Unmarshal(read("round-1.json"), &merged); err != nil {
			t.Fatal(err)
		}
		got := [][]string{}
		for _, f := range merged.Findings {
			got = append(got, []string{f.Problem, f.Severity})
		}
		if !reflect.DeepEqual(got, s.Merged) {
			t.Errorf("step %d: merged findings = %v, want %v", i, got, s.Merged)
		}
	}
}

func checkPairTable(t *testing.T, i int, s pairStep, out string) {
	t.Helper()
	if s.BlindIDs != nil {
		var ids []string
		for _, line := range strings.Split(out, "\n")[2:] {
			if line != "" {
				ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
			}
		}
		if !reflect.DeepEqual(ids, s.BlindIDs) {
			t.Errorf("step %d: row order = %v, want %v", i, ids, s.BlindIDs)
		}
	}
	for id, want := range s.RowHas {
		if row := pairTableRow(t, out, id); !strings.Contains(row, want) {
			t.Errorf("step %d: row %s = %q, want it to contain %q", i, id, row, want)
		}
	}
	for id, spec := range s.CellOf {
		cells := strings.Split(pairTableRow(t, out, id), "|")
		if got := strings.TrimSpace(cells[int(spec[0].(float64))]); got != spec[1] {
			t.Errorf("step %d: cell %v of %s = %q, want %q", i, spec[0], id, got, spec[1])
		}
	}
	if s.Pipes != nil {
		for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
			if n := unescapedPipes(line); n != *s.Pipes {
				t.Errorf("step %d: %d unescaped pipes in %q, want %d", i, n, line, *s.Pipes)
			}
		}
	}
}

func TestPairFixtureCases(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(pairFixtureDir, "*.json"))
	if err != nil || len(paths) < 27 {
		t.Fatalf("fixtures = %d (%v), want at least 27", len(paths), err)
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			runPairCase(t, loadPairCase(t, path))
		})
	}
}

// The coin is tossed once: a second blind, even with another coin, reuses pair.json.
func TestPairIsDrawnOnce(t *testing.T) {
	c := loadPairCase(t, filepath.Join(pairFixtureDir, "05-blind-is-idempotent.json"))
	root := pairRoot(t)
	attempt := filepath.Join(t.TempDir(), "attempt")
	writeFile(t, filepath.Join(attempt, "attempt.json"), `{"kind":"plan","reviewed_sha":"abc123"}`)
	for name, raw := range c.Attempt {
		writePairValue(t, filepath.Join(attempt, name), raw)
	}
	tosses := 0
	deps := PairDeps{
		Coin:      func() bool { tosses++; return tosses == 1 },
		TreeState: func(string) (string, bool, error) { return "abc123", true, nil },
	}
	for range 3 {
		if _, err := Blind(attempt, func() (string, error) { return root, nil }, deps); err != nil {
			t.Fatal(err)
		}
	}
	if tosses != 1 {
		t.Errorf("coin tossed %d times, want 1", tosses)
	}
	data, err := os.ReadFile(filepath.Join(attempt, "pair.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"A": "codex", "B": "fallback"}`; strings.TrimSpace(string(data)) != want {
		t.Errorf("pair.json = %q, want %q", data, want)
	}
}

func TestPairCellEscapesBackslashPipeAndLineBreaks(t *testing.T) {
	if got, want := pairCell("x\\|y\r\nz\rq\nw"), `x\\\|y z q w`; got != want {
		t.Errorf("pairCell = %q, want %q", got, want)
	}
}

func TestPairTreeStateSeesUntrackedFiles(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git("add", "a.txt")
	git("commit", "-qm", "init")
	sha := git("rev-parse", "HEAD")
	if head, clean, err := gitTreeState(repo); err != nil || head != sha || !clean {
		t.Errorf("gitTreeState = %q, %v, %v; want %q, true, nil", head, clean, err, sha)
	}
	writeFile(t, filepath.Join(repo, "nieuw.txt"), "ongevolgd\n")
	if head, clean, err := gitTreeState(repo); err != nil || head != sha || clean {
		t.Errorf("gitTreeState = %q, %v, %v; want %q, false, nil", head, clean, err, sha)
	}
}

// An attempt without attempt.json, or outside a repo, fails as SHADOW-PAIR FAILED (exit 4).
func TestPairMissingAttemptIsExit4(t *testing.T) {
	var out, errOut bytes.Buffer
	deps := PairDeps{Coin: func() bool { return true }, TreeState: gitTreeState}
	code := RunPair("blind", t.TempDir(), func() (string, error) { return t.TempDir(), nil }, deps, &out, &errOut)
	if code != 4 || !strings.HasPrefix(errOut.String(), "SHADOW-PAIR FAILED: ") || out.Len() != 0 {
		t.Errorf("RunPair = %d, stdout %q, stderr %q; want 4, nothing, SHADOW-PAIR FAILED: ...", code, out.String(), errOut.String())
	}
}
