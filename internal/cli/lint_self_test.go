package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Classification: these tests fail when lighthouse itself stops being wired for its own lint
// check (a dropped pin, a config that no longer covers the floor, a CI lint step that is gone or
// unrecognised), and when the `lint-check` job of ci.yml loses its step or gets a `needs:`, `if:`
// or `continue-on-error` that would let a PR pass without it. They read the real repository, not
// a temp repo.

// selfRoot is the root of this repository, two levels above internal/cli.
func selfRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLighthouseItselfIsWired(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := lintRun(selfRoot(t), nil, nil, "github", &out, &errOut)
	if rc != 0 {
		t.Fatalf("lint check on lighthouse itself: exit %d\nstdout:\n%s\nstderr:\n%s", rc, out.String(), errOut.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr not empty: %q", errOut.String())
	}
}

func TestCIRunsLintCheckJob(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(selfRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range lintCheckJobProblems(t, data) {
		t.Error(p)
	}
}

// Classification: guard-of-guard. Fails when lintCheckJobProblems lets through a lint-check job
// that can stay green on a red lint check: continue-on-error on job or step, needs, if, or no step.
func TestLintCheckJobProblemsCatchesEscapes(t *testing.T) {
	const step = "      - run: go run ./cmd/lighthouse lint check\n"
	head := "jobs:\n  lint-check:\n    runs-on: ubuntu-latest\n"
	cases := map[string]string{
		"job continue-on-error":  head + "    continue-on-error: true\n    steps:\n" + step,
		"step continue-on-error": head + "    steps:\n" + step + "        continue-on-error: true\n",
		"job needs":              head + "    needs: build\n    steps:\n" + step,
		"job if":                 head + "    if: false\n    steps:\n" + step,
		"step if":                head + "    steps:\n" + step + "        if: false\n",
		"no step":                head + "    steps:\n      - run: echo\n",
		"no job":                 "jobs:\n  other:\n    steps:\n" + step,
	}
	for name, yml := range cases {
		if got := lintCheckJobProblems(t, []byte(yml)); len(got) == 0 {
			t.Errorf("%s: no problem found in\n%s", name, yml)
		}
	}
	if got := lintCheckJobProblems(t, []byte(head+"    steps:\n"+step)); len(got) != 0 {
		t.Errorf("clean job: %v", got)
	}
}

// lintCheckJobProblems lists what lets the lint-check job of a ci.yml skip or ignore the check.
func lintCheckJobProblems(t *testing.T, data []byte) []string {
	t.Helper()
	var wf struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	job, ok := wf.Jobs["lint-check"]
	if !ok {
		return []string{"ci.yml has no job lint-check"}
	}
	var out []string
	for _, key := range []string{"needs", "if", "continue-on-error"} {
		if _, ok := job[key]; ok {
			out = append(out, "job lint-check has "+key+": a PR could pass without the lint check")
		}
	}
	steps, _ := job["steps"].([]any)
	found := false
	for _, s := range steps {
		step, _ := s.(map[string]any)
		for _, key := range []string{"if", "continue-on-error"} {
			if _, ok := step[key]; ok {
				out = append(out, fmt.Sprintf("job lint-check has a step with %s: %v", key, step))
			}
		}
		if run, _ := step["run"].(string); strings.TrimSpace(run) == "go run ./cmd/lighthouse lint check" {
			found = true
		}
	}
	if !found {
		out = append(out, "job lint-check has no step `run: go run ./cmd/lighthouse lint check`")
	}
	return out
}
