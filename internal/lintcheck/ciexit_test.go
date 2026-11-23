package lintcheck

import (
	"testing"
)

var (
	ruffFloor  = []string{"E722", "B006", "F403", "F811", "B023", "PT012", "PGH003", "PGH004"}
	goFloor    = []string{"errcheck", "govet", "ineffassign", "staticcheck", "unused"}
	shellFloor = []string{"SC2164", "SC2046", "SC2115", "SC2155", "SC2034"}
)

// ghStep is a run step on a Linux runner without a shell key: GitHub runs it with bash -e {0}.
func ghStep(run string) Step {
	return Step{File: wf + "ci.yml", Job: "lint", Line: 10, Run: run, RunsOn: "ubuntu-latest"}
}

func floorFor(linter string) []string {
	switch linter {
	case "ruff":
		return ruffFloor
	case "golangci-lint":
		return goFloor
	case "shellcheck":
		return shellFloor
	}
	return nil
}

// callsOf runs Calls with no package.json scripts and a config check that never narrows.
func callsOf(steps []Step, linter string) []Call {
	return Calls(steps, linter, func(string) map[string]string { return nil }, floorFor(linter),
		func(string, string) bool { return false })
}

// onlyVerdict asserts there is exactly one call and returns its verdict.
func onlyVerdict(t *testing.T, steps []Step, linter string) Call {
	t.Helper()
	got := callsOf(steps, linter)
	if len(got) != 1 {
		t.Fatalf("want 1 call, got %d: %+v", len(got), got)
	}
	return got[0]
}

func TestEffectiveShell(t *testing.T) {
	cases := []struct {
		name    string
		step    Step
		shell   string
		errexit bool
		ok      bool
	}{
		{"no shell on ubuntu", Step{RunsOn: "ubuntu-latest"}, "bash -e {0}", true, true},
		{"no shell on macos", Step{RunsOn: "macos-latest"}, "bash -e {0}", true, true},
		{"no shell on ubuntu-24.04", Step{RunsOn: "ubuntu-24.04"}, "bash -e {0}", true, true},
		{"shell bash", Step{Shell: "bash", RunsOn: "ubuntu-latest"}, "bash", true, true},
		{"shell sh", Step{Shell: "sh", RunsOn: "ubuntu-latest"}, "sh", true, true},
		{"shell bash on windows", Step{Shell: "bash", RunsOn: "windows-latest"}, "bash", true, true},
		{"shell bash {0}", Step{Shell: "bash {0}", RunsOn: "ubuntu-latest"}, "bash {0}", false, true},
		{"shell sh {0}", Step{Shell: "sh {0}", RunsOn: "ubuntu-latest"}, "sh {0}", false, true},
		{"shell pwsh", Step{Shell: "pwsh", RunsOn: "ubuntu-latest"}, "pwsh", false, true},
		{"windows without shell", Step{RunsOn: "windows-latest"}, "", false, false},
		{"runs-on expression", Step{RunsOn: "${{ matrix.os }}"}, "", false, false},
		{"runs-on not a string", Step{}, "", false, false},
		{"shell expression", Step{Shell: "${{ matrix.shell }}", RunsOn: "ubuntu-latest"}, "", false, false},
		{"gitlab", Step{GitLab: true}, "gitlab", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, errexit, ok := effectiveShell(c.step)
			if name != c.shell || errexit != c.errexit || ok != c.ok {
				t.Fatalf("got (%q, %v, %v), want (%q, %v, %v)", name, errexit, ok, c.shell, c.errexit, c.ok)
			}
		})
	}
}

func TestExitStatusAllowlist(t *testing.T) {
	const call = "uvx ruff@0.9.0 check ."
	mod := func(f func(*Step)) []Step {
		s := ghStep(call)
		f(&s)
		return []Step{s}
	}
	run := func(script string) []Step { return []Step{ghStep(script)} }
	cases := []struct {
		name  string
		steps []Step
		want  Verdict
	}{
		{"own line", run(call), Valid},
		{"own line among others", run("echo start\n" + call + "\necho done"), Valid},
		{"and-chain before", run("cd src && " + call), Valid},
		{"and-chain after, last line", run(call + " && echo ok"), Valid},
		{"and-chain after, more lines follow", run(call + " && echo ok\necho more"), Unrecognised},
		{"shell bash with pipe (pipefail)", mod(func(s *Step) { s.Run = call + " | tee out.txt"; s.Shell = "bash" }), Valid},
		{"set -o pipefail with pipe", run("set -o pipefail\n" + call + " | tee out.txt"), Valid},
		{"set -euo pipefail with pipe", run("set -euo pipefail\n" + call + " | tee out.txt"), Valid},
		{"call last in pipe", run("echo x | " + call), Valid},
		{"macos without shell", mod(func(s *Step) { s.RunsOn = "macos-latest" }), Valid},
		{"continue-on-error false", mod(func(s *Step) { s.ContinueOnError = "false"; s.JobContinueOnError = "false" }), Valid},

		{"or true", run(call + " || true"), Unrecognised},
		{"or before", run("false || " + call), Unrecognised},
		{"tee without pipefail", run(call + " | tee out.txt"), Unrecognised},
		{"tee with shell sh", mod(func(s *Step) { s.Run = call + " | tee out.txt"; s.Shell = "sh" }), Unrecognised},
		{"semicolon after", run(call + "; echo done"), Unrecognised},
		{"background", run(call + " &\nwait"), Unrecognised},
		{"set +e before", run("set +e\n" + call), Unrecognised},
		{"set +o errexit before", run("set +o errexit\n" + call), Unrecognised},
		{"set +o pipefail undoes pipefail", run("set -o pipefail\nset +o pipefail\n" + call + " | tee x"), Unrecognised},
		{"inside if", run("if " + call + "; then echo ok; fi"), Unrecognised},
		{"inside for loop", run("for d in a b; do\n  " + call + "\ndone"), Unrecognised},
		{"negated", run("! " + call), Unrecognised},
		{"subshell", run("( " + call + " )"), Unrecognised},
		{"step continue-on-error true", mod(func(s *Step) { s.ContinueOnError = "true" }), Unrecognised},
		{"job continue-on-error true", mod(func(s *Step) { s.JobContinueOnError = "true" }), Unrecognised},
		{"continue-on-error expression", mod(func(s *Step) { s.ContinueOnError = "${{ matrix.experimental }}" }), Unrecognised},
		{"shell bash {0}", mod(func(s *Step) { s.Shell = "bash {0}" }), Unrecognised},
		{"shell pwsh", mod(func(s *Step) { s.Shell = "pwsh" }), Unrecognised},
		{"windows without shell", mod(func(s *Step) { s.RunsOn = "windows-latest" }), Unrecognised},
		{"runs-on matrix expression", mod(func(s *Step) { s.RunsOn = "${{ matrix.os }}" }), Unrecognised},
		{"exit-zero", run("uvx ruff@0.9.0 check --exit-zero ."), Unrecognised},
		{"exit-zero short", run("uvx ruff@0.9.0 check -e ."), Unrecognised},
		{"fix rewrites findings away", run("uvx ruff@0.9.0 check --fix ."), Unrecognised},
		{"unparseable heredoc", run("cat <<EOF\nx\nEOF\n" + call), Unrecognised},
		{"bash -c", run("bash -c '" + call + "'"), Unrecognised},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := onlyVerdict(t, c.steps, "ruff")
			if got.Verdict != c.want {
				t.Fatalf("verdict %v (%s), want %v", got.Verdict, got.Reason, c.want)
			}
			if c.want != Valid && got.Reason == "" {
				t.Fatal("a call that is not valid needs a reason")
			}
		})
	}
}

// The shell of a step can come from defaults.run.shell on the job or the workflow (r3-f1); Steps
// resolves it and the verdict follows.
func TestExitShellFromDefaults(t *testing.T) {
	cases := map[string]Verdict{
		"on: push\ndefaults:\n  run:\n    shell: bash {0}\njobs:\n  l:\n    runs-on: ubuntu-latest\n    steps:\n      - run: uvx ruff@0.9.0 check\n":             Unrecognised,
		"on: push\njobs:\n  l:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: bash {0}\n    steps:\n      - run: uvx ruff@0.9.0 check\n": Unrecognised,
		"on: push\njobs:\n  l:\n    runs-on: windows-latest\n    defaults:\n      run:\n        shell: bash\n    steps:\n      - run: uvx ruff@0.9.0 check\n":    Valid,
	}
	for yml, want := range cases {
		got := onlyVerdict(t, stepsOf(t, map[string]string{wf + "ci.yml": yml}), "ruff")
		if got.Verdict != want {
			t.Errorf("verdict %v (%s), want %v for\n%s", got.Verdict, got.Reason, want, yml)
		}
	}
}

func TestExitActionWithoutContinueOnErrorIsValid(t *testing.T) {
	s := Step{File: wf + "ci.yml", Job: "go", Uses: "golangci/golangci-lint-action@abc123", With: map[string]string{"version": "v2.15.0"}}
	if got := onlyVerdict(t, []Step{s}, "golangci-lint"); got.Verdict != Valid {
		t.Fatalf("got %+v", got)
	}
	s.ContinueOnError = "true"
	if got := onlyVerdict(t, []Step{s}, "golangci-lint"); got.Verdict != Unrecognised {
		t.Fatalf("got %+v", got)
	}
	// Shell and runner do not matter for an action.
	s.ContinueOnError = ""
	s.RunsOn = "windows-latest"
	if got := onlyVerdict(t, []Step{s}, "golangci-lint"); got.Verdict != Valid {
		t.Fatalf("got %+v", got)
	}
}

func TestExitIfFalseNeverValid(t *testing.T) {
	// The pin comes from an earlier step, so only IfFalse can keep the call from being Valid.
	steps := pinned("ruff", ghStep("ruff check"))
	steps[1].IfFalse = true
	if got := callsOf(steps, "ruff"); len(got) != 0 {
		t.Fatalf("a step that never runs counted: %+v", got)
	}
	act := Step{Uses: "astral-sh/ruff-action@v3", With: map[string]string{"version": "0.9.0"}, IfFalse: true}
	if got := callsOf([]Step{act}, "ruff"); len(got) != 0 {
		t.Fatalf("an action that never runs counted: %+v", got)
	}
}

func TestExitJobIfFalseMarksEverySteps(t *testing.T) {
	got := stepsOf(t, map[string]string{
		wf + "ci.yml": "on: push\njobs:\n" +
			"  a:\n    if: false\n    runs-on: ubuntu-latest\n    steps:\n      - run: x\n      - run: y\n" +
			"  b:\n    if: ${{false}}\n    runs-on: ubuntu-latest\n    steps:\n      - run: x\n" +
			"  c:\n    if: \"  ${{   false }} \"\n    runs-on: ubuntu-latest\n    steps:\n      - run: x\n" +
			"  d:\n    if: github.ref == 'refs/heads/main'\n    runs-on: ubuntu-latest\n    steps:\n      - run: x\n        if: \" false \"\n      - run: y\n",
	})
	want := []bool{true, true, true, true, true, false}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].IfFalse != w {
			t.Errorf("step %d (%s/%d) IfFalse %v, want %v", i, got[i].Job, got[i].Index, got[i].IfFalse, w)
		}
	}
}
