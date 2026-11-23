package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Classification: these tests fail when lighthouse itself stops being wired for its own lint
// check (a dropped pin, a config that no longer covers the floor, a CI lint step that is gone or
// unrecognised), and when the `lint-check` job of ci.yml loses its step or gets a `needs:` or
// `if:` that would let a PR skip it. They read the real repository, not a temp repo.

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
	var wf struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	job, ok := wf.Jobs["lint-check"]
	if !ok {
		t.Fatal("ci.yml has no job lint-check")
	}
	for _, key := range []string{"needs", "if"} {
		if _, ok := job[key]; ok {
			t.Errorf("job lint-check has %q: a PR could skip the lint check", key)
		}
	}
	steps, _ := job["steps"].([]any)
	found := false
	for _, s := range steps {
		step, _ := s.(map[string]any)
		if _, ok := step["if"]; ok {
			t.Errorf("job lint-check has a step with if: %v", step)
		}
		if run, _ := step["run"].(string); strings.TrimSpace(run) == "go run ./cmd/lighthouse lint check" {
			found = true
		}
	}
	if !found {
		t.Error("job lint-check has no step `run: go run ./cmd/lighthouse lint check`")
	}
}
