package lintcheck

import (
	"errors"
	"testing"
)

const wf = ".github/workflows/"

func stepsOf(t *testing.T, files map[string]string) []Step {
	t.Helper()
	got, err := Steps(writeRepo(t, files), "")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStepsIncludesPushAndPullRequestWorkflows(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "a.yml": "on:\n  push:\n    branches: [main]\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - run: ruff check .\n",
		wf + "b.yml": "on:\n  pull_request:\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go vet ./...\n",
		wf + "c.yml": "on:\n  schedule:\n    - cron: '0 0 * * *'\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo nightly\n",
	})
	if len(got) != 3 {
		t.Fatalf("want 3 steps, got %+v", got)
	}
	s := got[1]
	if s.File != wf+"a.yml" || s.Job != "lint" || s.Index != 1 || s.Run != "ruff check ." || s.Line != 9 || s.RunsOn != "ubuntu-latest" {
		t.Fatalf("unexpected step %+v", s)
	}
	if got[0].Uses != "actions/checkout@v4" || got[2].File != wf+"b.yml" {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestStepsOnAsListOrString(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "a.yml": "on: [push]\njobs:\n  j:\n    runs-on: x\n    steps:\n      - run: a\n",
		wf + "b.yml": "on: push\njobs:\n  j:\n    runs-on: x\n    steps:\n      - run: b\n",
	})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestStepsSkipsWorkflowDispatchOnly(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "a.yml": "on: workflow_dispatch\njobs:\n  j:\n    runs-on: x\n    steps:\n      - run: a\n",
		wf + "b.yml": "on: [workflow_dispatch, release]\njobs:\n  j:\n    runs-on: x\n    steps:\n      - run: b\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestStepsFollowsWorkflowCallOneLevel(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "ci.yml":   "on: push\njobs:\n  l:\n    uses: ./.github/workflows/lint.yml\n",
		wf + "lint.yml": "on: workflow_call\njobs:\n  ruff:\n    runs-on: x\n    steps:\n      - run: ruff check .\n  next:\n    uses: ./.github/workflows/deep.yml\n",
		wf + "deep.yml": "on: workflow_call\njobs:\n  d:\n    runs-on: x\n    steps:\n      - run: golangci-lint run\n",
	})
	if len(got) != 1 || got[0].File != wf+"lint.yml" || got[0].Job != "ruff" {
		t.Fatalf("got %+v", got)
	}
}

func TestStepsWorkingDirectoryFromDefaults(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "a.yml": `on: push
defaults:
  run:
    working-directory: wfdir
    shell: bash
jobs:
  one:
    runs-on: x
    steps:
      - run: a
  two:
    runs-on: x
    defaults:
      run:
        working-directory: jobdir
    steps:
      - run: b
      - run: c
        working-directory: stepdir
        shell: sh
`,
	})
	want := []string{"wfdir", "jobdir", "stepdir"}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].WorkDir != w {
			t.Errorf("step %d WorkDir %q, want %q", i, got[i].WorkDir, w)
		}
	}
	if got[0].Shell != "bash" || got[1].Shell != "bash" || got[2].Shell != "sh" {
		t.Errorf("shells %q %q %q", got[0].Shell, got[1].Shell, got[2].Shell)
	}
}

func TestStepsIfFalseLiteralIsMarked(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "a.yml": "on: push\njobs:\n  j:\n    runs-on: x\n    steps:\n      - run: a\n        if: false\n      - run: b\n        if: github.event_name == 'push'\n",
	})
	if len(got) != 2 || !got[0].IfFalse || got[1].IfFalse {
		t.Fatalf("got %+v", got)
	}
}

func TestStepsContinueOnErrorAndEnv(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "a.yml": "on: push\nenv:\n  A: w\n  B: w\njobs:\n  j:\n    runs-on: x\n    continue-on-error: true\n    env:\n      B: j\n    steps:\n      - run: a\n        continue-on-error: false\n        env:\n          C: s\n        with:\n          k: v\n",
	})
	s := got[0]
	if s.ContinueOnError != "false" || s.JobContinueOnError != "true" {
		t.Fatalf("%+v", s)
	}
	if s.Env["A"] != "w" || s.Env["B"] != "j" || s.Env["C"] != "s" || s.With["k"] != "v" {
		t.Fatalf("%+v", s)
	}
}

func TestStepsInvalidYAMLIsUnchecked(t *testing.T) {
	_, err := Steps(writeRepo(t, map[string]string{wf + "a.yml": "on: [push\njobs: {"}), "")
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Fatalf("want UncheckedError, got %v", err)
	}
}

func TestStepsMissingCIFilesIsEmptyNotError(t *testing.T) {
	root := writeRepo(t, map[string]string{"x": ""})
	got, err := Steps(root, "github")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	if ciFilesPresent(root, "") {
		t.Fatal("no CI files expected")
	}
}

func TestStepsReadsYamlExtension(t *testing.T) {
	root := writeRepo(t, map[string]string{
		wf + "a.yaml": "on: push\njobs:\n  j:\n    runs-on: x\n    steps:\n      - run: a\n",
	})
	if !ciFilesPresent(root, "") {
		t.Fatal("yaml extension must count")
	}
	got, err := Steps(root, "")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}
