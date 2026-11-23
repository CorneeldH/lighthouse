package lintcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinned prefixes a step that installs the linter on a pinned version, so a row tests only what it
// names.
func pinned(linter string, steps ...Step) []Step {
	var install string
	switch linter {
	case "ruff":
		install = "pip install ruff==0.9.0"
	case "golangci-lint":
		install = "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"
	case "shellcheck":
		install = "curl -sSL https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.xz | tar xJ"
	case "eslint":
		install = "npm ci"
	}
	first := ghStep(install)
	out := []Step{first}
	for i, s := range steps {
		s.Index = i + 1
		out = append(out, s)
	}
	return out
}

func TestCallsRecognition(t *testing.T) {
	scripts := map[string]map[string]string{
		".":   {"lint": "eslint .", "build": "tsc", "version": "eslint --version", "lint:fix": "eslint . --fix", "masked": "eslint . || true"},
		"web": {"lint": "expo lint"},
	}
	cases := []struct {
		name   string
		linter string
		steps  []Step
		calls  int
		want   Verdict // of the last call; ignored when calls is 0
	}{
		{"ruff check", "ruff", pinned("ruff", ghStep("ruff check .")), 1, Valid},
		{"uvx ruff on a version", "ruff", []Step{ghStep("uvx ruff@0.9.0 check")}, 1, Valid},
		{"uv run ruff", "ruff", pinned("ruff", ghStep("uv run ruff check src/")), 1, Valid},
		{"ruff action", "ruff", []Step{{Uses: "astral-sh/ruff-action@v3", With: map[string]string{"version": "0.9.0"}}}, 1, Valid},
		{"ruff action with format args is no lint call", "ruff", []Step{{Uses: "astral-sh/ruff-action@v3", With: map[string]string{"version": "0.9.0", "args": "format --check"}}}, 0, NotFound},
		{"ruff format is no lint call", "ruff", pinned("ruff", ghStep("ruff format --check .")), 0, NotFound},
		{"golangci-lint run", "golangci-lint", pinned("golangci-lint", ghStep("golangci-lint run ./...")), 1, Valid},
		{"golangci-lint by path", "golangci-lint", pinned("golangci-lint", ghStep("$(go env GOPATH)/bin/golangci-lint run")), 1, Valid},
		{"golangci action", "golangci-lint", []Step{{Uses: "golangci/golangci-lint-action@v9", With: map[string]string{"version": "v2.15.0"}}}, 1, Valid},
		{"golangci action install-only is no call", "golangci-lint", []Step{{Uses: "golangci/golangci-lint-action@v9", With: map[string]string{"version": "v2.15.0", "install-only": "true"}}}, 0, NotFound},
		{"shellcheck x.sh", "shellcheck", pinned("shellcheck", ghStep("shellcheck x.sh")), 1, Valid},
		{"shellcheck-repo.sh", "shellcheck", []Step{ghStep(".lighthouse/scripts/shellcheck-repo.sh")}, 1, Valid},
		{"bash shellcheck-repo.sh", "shellcheck", []Step{ghStep("bash .lighthouse/scripts/shellcheck-repo.sh")}, 1, Valid},
		{"shellcheck --version is no call", "shellcheck", pinned("shellcheck", ghStep("shellcheck --version")), 0, NotFound},
		{"eslint .", "eslint", pinned("eslint", ghStep("eslint .")), 1, Valid},
		{"npx eslint", "eslint", pinned("eslint", ghStep("npx eslint src")), 1, Valid},
		{"expo lint", "eslint", pinned("eslint", ghStep("npx expo lint")), 1, Valid},
		{"npm run lint", "eslint", pinned("eslint", ghStep("npm run lint")), 1, Valid},
		{"npm run lint in working-directory", "eslint", func() []Step {
			s := ghStep("npm ci")
			s.WorkDir = "web"
			l := ghStep("npm run lint")
			l.WorkDir, l.Index = "web", 1
			return []Step{s, l}
		}(), 1, Valid},
		{"cd web && npm run lint", "eslint", []Step{ghStep("cd web && npm ci && npm run lint")}, 1, Valid},
		{"npm run of a script without eslint", "eslint", pinned("eslint", ghStep("npm run build")), 0, NotFound},
		{"npm run of a missing script", "eslint", pinned("eslint", ghStep("npm run nope")), 0, NotFound},
		{"npm script that masks the status", "eslint", pinned("eslint", ghStep("npm run masked")), 1, Unrecognised},
		{"npm script with --fix", "eslint", pinned("eslint", ghStep("npm run lint:fix")), 1, Unrecognised},
		{"make lint", "ruff", []Step{ghStep("make lint")}, 0, NotFound},
		{"just lint", "golangci-lint", []Step{ghStep("just lint")}, 0, NotFound},
		{"pre-commit run", "ruff", []Step{ghStep("pre-commit run --all-files")}, 0, NotFound},
		{"ratchet", "ruff", []Step{ghStep("python ci/ratchet.py")}, 0, NotFound},
		{"echo ruff check", "ruff", pinned("ruff", ghStep(`echo "ruff check"`)), 0, NotFound},
		{"echo unquoted", "ruff", pinned("ruff", ghStep(`echo ruff check .`)), 0, NotFound},
		{"comment", "ruff", pinned("ruff", ghStep("# ruff check\necho hi")), 0, NotFound},
		{"trailing comment", "ruff", pinned("ruff", ghStep("echo hi # ruff check")), 0, NotFound},
		{"backslash continuation", "ruff", pinned("ruff", ghStep("ruff \\\n  check \\\n  src/")), 1, Valid},
		{"multi-line run", "ruff", pinned("ruff", ghStep("echo one\n\nruff check src/\necho two\n")), 1, Valid},
		{"env assignment prefix", "ruff", pinned("ruff", ghStep("RUFF_OUTPUT_FORMAT=github ruff check")), 1, Valid},
		{"find -exec is not placed", "shellcheck", pinned("shellcheck", ghStep("find . -name '*.sh' -exec shellcheck {} +")), 1, Unrecognised},
		// commands that only look the tool up or print information lint nothing
		{"command -v shellcheck", "shellcheck", pinned("shellcheck", ghStep("command -v shellcheck")), 0, NotFound},
		{"command -V eslint", "eslint", pinned("eslint", ghStep("command -V eslint")), 0, NotFound},
		{"command -v ruff", "ruff", pinned("ruff", ghStep("command -v ruff check")), 0, NotFound},
		{"command runs the linter", "ruff", pinned("ruff", ghStep("command ruff check")), 1, Valid},
		{"type shellcheck", "shellcheck", pinned("shellcheck", ghStep("type shellcheck")), 0, NotFound},
		{"which eslint", "eslint", pinned("eslint", ghStep("which eslint")), 0, NotFound},
		{"hash shellcheck", "shellcheck", pinned("shellcheck", ghStep("hash shellcheck")), 0, NotFound},
		{"shellcheck --help", "shellcheck", pinned("shellcheck", ghStep("shellcheck --help")), 0, NotFound},
		{"shellcheck -V", "shellcheck", pinned("shellcheck", ghStep("shellcheck -V")), 0, NotFound},
		{"shellcheck-repo.sh --help", "shellcheck", []Step{ghStep(".lighthouse/scripts/shellcheck-repo.sh --help")}, 0, NotFound},
		{"eslint --version", "eslint", pinned("eslint", ghStep("eslint --version")), 0, NotFound},
		{"npx eslint --version", "eslint", pinned("eslint", ghStep("npx eslint --version")), 0, NotFound},
		{"eslint -v", "eslint", pinned("eslint", ghStep("eslint -v")), 0, NotFound},
		{"eslint --help", "eslint", pinned("eslint", ghStep("eslint --help")), 0, NotFound},
		{"eslint -h", "eslint", pinned("eslint", ghStep("eslint -h")), 0, NotFound},
		{"eslint --print-config", "eslint", pinned("eslint", ghStep("eslint --print-config src/a.js")), 0, NotFound},
		{"eslint --env-info", "eslint", pinned("eslint", ghStep("eslint --env-info")), 0, NotFound},
		{"eslint --init", "eslint", pinned("eslint", ghStep("eslint --init")), 0, NotFound},
		{"expo lint --help", "eslint", pinned("eslint", ghStep("npx expo lint --help")), 0, NotFound},
		{"npm script eslint --version", "eslint", pinned("eslint", ghStep("npm run version")), 0, NotFound},
		{"npm run lint -- --version", "eslint", pinned("eslint", ghStep("npm run lint -- --version")), 0, NotFound},
		{"ruff check --help", "ruff", pinned("ruff", ghStep("ruff check --help")), 0, NotFound},
		{"ruff check -h", "ruff", pinned("ruff", ghStep("ruff check -h")), 0, NotFound},
		{"ruff check --show-settings", "ruff", pinned("ruff", ghStep("ruff check --show-settings")), 0, NotFound},
		{"ruff check --show-files", "ruff", pinned("ruff", ghStep("ruff check --show-files")), 0, NotFound},
		{"golangci-lint run --help", "golangci-lint", pinned("golangci-lint", ghStep("golangci-lint run --help")), 0, NotFound},
		{"golangci-lint run -h", "golangci-lint", pinned("golangci-lint", ghStep("golangci-lint run -h")), 0, NotFound},
		{"ruff action args --help", "ruff", []Step{{Uses: "astral-sh/ruff-action@v3", With: map[string]string{"version": "0.9.0", "args": "check --help"}}}, 0, NotFound},
		{"golangci action args --help", "golangci-lint", []Step{{Uses: "golangci/golangci-lint-action@v9", With: map[string]string{"version": "v2.15.0", "args": "--help"}}}, 0, NotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Calls(c.steps, c.linter, func(dir string) map[string]string { return scripts[dir] },
				floorFor(c.linter), func(string, string) bool { return false })
			if len(got) != c.calls {
				t.Fatalf("want %d calls, got %+v", c.calls, got)
			}
			if c.calls > 0 && got[len(got)-1].Verdict != c.want {
				t.Fatalf("verdict %v (%s), want %v", got[len(got)-1].Verdict, got[len(got)-1].Reason, c.want)
			}
		})
	}
}

func TestCallsArgsAndStep(t *testing.T) {
	steps := pinned("ruff", ghStep("ruff check --exclude migrations/ src/"))
	got := onlyVerdict(t, steps, "ruff")
	if got.Verdict != Valid || got.Linter != "ruff" || got.Step.Index != 1 || got.Step.Line != 10 {
		t.Fatalf("got %+v", got)
	}
	if strings.Join(got.Args, " ") != "--exclude migrations/ src/" {
		t.Fatalf("args %q", got.Args)
	}
}

func TestCallsExpoScriptInWorkDir(t *testing.T) {
	inst := ghStep("npm ci")
	inst.WorkDir = "./web/"
	s := ghStep("npm run lint -- --max-warnings 0")
	s.WorkDir, s.Index = "web", 1
	got := Calls([]Step{inst, s}, "eslint", func(dir string) map[string]string {
		if dir == "web" {
			return map[string]string{"lint": "expo lint"}
		}
		return nil
	}, nil, func(string, string) bool { return false })
	if len(got) != 1 || got[0].Verdict != Valid || strings.Join(got[0].Args, " ") != "--max-warnings 0" {
		t.Fatalf("got %+v", got)
	}
}

func TestCallsPin(t *testing.T) {
	two := func(install, call string) []Step {
		a, b := ghStep(install), ghStep(call)
		b.Index = 1
		return []Step{a, b}
	}
	cases := []struct {
		name   string
		linter string
		steps  []Step
		want   Verdict
	}{
		{"ruff bare", "ruff", []Step{ghStep("ruff check")}, Unpinned},
		{"ruff uvx without version", "ruff", []Step{ghStep("uvx ruff check")}, Unpinned},
		{"ruff pip install without version", "ruff", two("pip install ruff", "ruff check"), Unpinned},
		{"ruff pip install on the pin", "ruff", two("pip install ruff==0.9.0", "ruff check"), Valid},
		{"ruff uv tool install on the pin", "ruff", two("uv tool install ruff@0.9.0", "ruff check"), Valid},
		{"ruff version from the pin file", "ruff", []Step{ghStep(`uvx ruff@"$(cat .ruff-version)" check`)}, Valid},
		{"ruff at latest", "ruff", []Step{ghStep("uvx ruff@latest check")}, Unpinned},
		{"ruff install echoed only", "ruff", two(`echo "pip install ruff==0.9.0"`, "ruff check"), Unpinned},
		{"ruff action without version", "ruff", []Step{{Uses: "astral-sh/ruff-action@v3"}}, Unpinned},
		{"golangci bare", "golangci-lint", []Step{ghStep("golangci-lint run")}, Unpinned},
		{"golangci go install on the pin", "golangci-lint", two("go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0", "golangci-lint run"), Valid},
		{"golangci go install at latest", "golangci-lint", two("go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest", "golangci-lint run"), Unpinned},
		{"golangci install script on a version", "golangci-lint", two("curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.15.0", "golangci-lint run"), Valid},
		{"golangci action install-only on the pin", "golangci-lint", []Step{
			{File: wf + "ci.yml", Job: "lint", Uses: "golangci/golangci-lint-action@v9", With: map[string]string{"version": "v2.15.0", "install-only": "true"}},
			func() Step { s := ghStep("golangci-lint run"); s.Index = 1; return s }(),
		}, Valid},
		{"golangci action without version", "golangci-lint", []Step{{Uses: "golangci/golangci-lint-action@v9"}}, Unpinned},
		{"golangci action at latest", "golangci-lint", []Step{{Uses: "golangci/golangci-lint-action@v9", With: map[string]string{"version": "latest"}}}, Unpinned},
		{"shellcheck bare", "shellcheck", []Step{ghStep("shellcheck x.sh")}, Unpinned},
		{"shellcheck apt install", "shellcheck", two("sudo apt-get install -y shellcheck", "shellcheck x.sh"), Unpinned},
		{"shellcheck download on a version", "shellcheck", two("curl -sSL https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.xz | tar xJ", "shellcheck x.sh"), Valid},
		{"shellcheck-repo.sh pins itself", "shellcheck", []Step{ghStep(".lighthouse/scripts/shellcheck-repo.sh")}, Valid},
		{"eslint without lockfile install", "eslint", []Step{ghStep("npx eslint .")}, Unpinned},
		{"eslint after npm install", "eslint", two("npm install", "npx eslint ."), Unpinned},
		{"eslint after pnpm frozen install", "eslint", two("pnpm install --frozen-lockfile", "pnpm exec eslint ."), Valid},
		{"eslint after yarn immutable install", "eslint", two("yarn install --immutable", "yarn eslint ."), Valid},
		{"eslint after bun frozen install", "eslint", two("bun install --frozen-lockfile", "bunx eslint ."), Valid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := onlyVerdict(t, c.steps, c.linter); got.Verdict != c.want {
				t.Fatalf("verdict %v (%s), want %v", got.Verdict, got.Reason, c.want)
			}
		})
	}
}

func TestCallsPinOnlyFromSameJobEarlierStep(t *testing.T) {
	install := ghStep("pip install ruff==0.9.0")
	call := ghStep("ruff check")
	call.Index = 1
	other := install
	other.Job = "setup"
	if got := onlyVerdict(t, []Step{other, call}, "ruff"); got.Verdict != Unpinned {
		t.Fatalf("install in another job counted: %+v", got)
	}
	after := install
	after.Index = 2
	if got := onlyVerdict(t, []Step{call, after}, "ruff"); got.Verdict != Unpinned {
		t.Fatalf("install after the call counted: %+v", got)
	}
	skipped := install
	skipped.IfFalse = true
	if got := onlyVerdict(t, []Step{skipped, call}, "ruff"); got.Verdict != Unpinned {
		t.Fatalf("install that never runs counted: %+v", got)
	}
	inDir := ghStep("npm ci")
	inDir.WorkDir = "other"
	lint := ghStep("npx eslint .")
	lint.Index = 1
	if got := onlyVerdict(t, []Step{inDir, lint}, "eslint"); got.Verdict != Unpinned {
		t.Fatalf("lockfile install in another directory counted: %+v", got)
	}
}

func TestCallsFlags(t *testing.T) {
	run := func(linter, script string) []Step { return pinned(linter, ghStep(script)) }
	act := func(uses string, with map[string]string) []Step {
		return []Step{{File: wf + "ci.yml", Job: "lint", Uses: uses, With: with}}
	}
	withOpts := func(linter, script, opts string) []Step {
		s := run(linter, script)
		s[1].Env = map[string]string{"SHELLCHECK_OPTS": opts}
		return s
	}
	cases := []struct {
		name   string
		linter string
		steps  []Step
		want   Verdict
	}{
		{"ruff exclude is scope", "ruff", run("ruff", "ruff check --exclude migrations/"), Valid},
		{"ruff extend-exclude is scope", "ruff", run("ruff", "ruff check --extend-exclude=tests/"), Valid},
		{"ruff select without floor", "ruff", run("ruff", "ruff check --select E9"), Narrowed},
		{"ruff select covering the floor", "ruff", run("ruff", "ruff check --select E,B,F,PT,PGH"), Valid},
		{"ruff select ALL", "ruff", run("ruff", "ruff check --select=ALL"), Valid},
		{"ruff extend-select", "ruff", run("ruff", "ruff check --extend-select D"), Valid},
		{"ruff ignore floor code", "ruff", run("ruff", "ruff check --ignore B006"), Narrowed},
		{"ruff ignore floor prefix", "ruff", run("ruff", "ruff check --ignore=PGH"), Narrowed},
		{"ruff ignore other code", "ruff", run("ruff", "ruff check --ignore E501"), Valid},
		{"ruff extend-ignore floor code", "ruff", run("ruff", "ruff check --extend-ignore F403"), Narrowed},
		{"ruff per-file-ignores floor code is scope", "ruff", run("ruff", "ruff check --per-file-ignores 'tests/*:B006' src/"), Valid},
		{"ruff extend-per-file-ignores floor code is scope", "ruff", run("ruff", "ruff check --extend-per-file-ignores 'tests/*:B006'"), Valid},
		{"ruff isolated drops the config", "ruff", run("ruff", "ruff check --isolated"), Narrowed},
		{"ruff inline config", "ruff", run("ruff", `ruff check --config "lint.ignore = ['B006']"`), Narrowed},
		{"ruff action args select", "ruff", act("astral-sh/ruff-action@v3", map[string]string{"version": "0.9.0", "args": "check --select E9"}), Narrowed},
		{"ruff action args exit-zero", "ruff", act("astral-sh/ruff-action@v3", map[string]string{"version": "0.9.0", "args": "check --exit-zero"}), Unrecognised},

		{"golangci disable floor linter", "golangci-lint", run("golangci-lint", "golangci-lint run --disable errcheck"), Narrowed},
		{"golangci -D floor linter", "golangci-lint", run("golangci-lint", "golangci-lint run -D unused"), Narrowed},
		{"golangci disable other linter", "golangci-lint", run("golangci-lint", "golangci-lint run --disable=lll"), Valid},
		{"golangci enable-only", "golangci-lint", run("golangci-lint", "golangci-lint run --enable-only errcheck"), Narrowed},
		{"golangci default none", "golangci-lint", run("golangci-lint", "golangci-lint run --default none"), Narrowed},
		{"golangci default=none", "golangci-lint", run("golangci-lint", "golangci-lint run --default=none"), Narrowed},
		{"golangci default standard", "golangci-lint", run("golangci-lint", "golangci-lint run --default standard"), Valid},
		{"golangci no-config", "golangci-lint", run("golangci-lint", "golangci-lint run --no-config"), Narrowed},
		{"golangci issues-exit-code 0", "golangci-lint", run("golangci-lint", "golangci-lint run --issues-exit-code=0"), Unrecognised},
		{"golangci issues-exit-code 0 spaced", "golangci-lint", run("golangci-lint", "golangci-lint run --issues-exit-code 0"), Unrecognised},
		{"golangci issues-exit-code 2", "golangci-lint", run("golangci-lint", "golangci-lint run --issues-exit-code=2"), Valid},
		{"golangci paths are scope", "golangci-lint", run("golangci-lint", "golangci-lint run ./internal/..."), Valid},
		{"golangci action args disable", "golangci-lint", act("golangci/golangci-lint-action@v9", map[string]string{"version": "v2.15.0", "args": "--disable errcheck"}), Narrowed},
		{"golangci action args exit code", "golangci-lint", act("golangci/golangci-lint-action@v9", map[string]string{"version": "v2.15.0", "args": "--issues-exit-code=0"}), Unrecognised},
		{"golangci action only-new-issues", "golangci-lint", act("golangci/golangci-lint-action@v9", map[string]string{"version": "v2.15.0", "only-new-issues": "true"}), Unrecognised},
		{"golangci action only-new-issues expression", "golangci-lint", act("golangci/golangci-lint-action@v9", map[string]string{"version": "v2.15.0", "only-new-issues": "${{ github.event_name == 'pull_request' }}"}), Unrecognised},
		{"golangci action only-new-issues false", "golangci-lint", act("golangci/golangci-lint-action@v9", map[string]string{"version": "v2.15.0", "only-new-issues": "false"}), Valid},
		{"golangci new", "golangci-lint", run("golangci-lint", "golangci-lint run --new"), Unrecognised},
		{"golangci -n", "golangci-lint", run("golangci-lint", "golangci-lint run -n"), Unrecognised},
		{"golangci new-from-rev", "golangci-lint", run("golangci-lint", "golangci-lint run --new-from-rev HEAD~1"), Unrecognised},
		{"golangci new-from-rev=", "golangci-lint", run("golangci-lint", "golangci-lint run --new-from-rev=origin/main"), Unrecognised},
		{"golangci new-from-merge-base", "golangci-lint", run("golangci-lint", "golangci-lint run --new-from-merge-base main"), Unrecognised},
		{"golangci new-from-patch", "golangci-lint", run("golangci-lint", "golangci-lint run --new-from-patch x.diff"), Unrecognised},
		{"golangci action args new-from-rev", "golangci-lint", act("golangci/golangci-lint-action@v9", map[string]string{"version": "v2.15.0", "args": "--new-from-rev=HEAD~1"}), Unrecognised},
		{"ruff diff", "ruff", run("ruff", "ruff check --diff ."), Unrecognised},
		{"ruff add-noqa", "ruff", run("ruff", "ruff check --add-noqa"), Unrecognised},
		{"ruff watch", "ruff", run("ruff", "ruff check --watch"), Unrecognised},
		{"ruff watch short", "ruff", run("ruff", "ruff check -w"), Unrecognised},

		{"shellcheck exclude floor code", "shellcheck", run("shellcheck", "shellcheck -e SC2164 x.sh"), Narrowed},
		{"shellcheck exclude floor code bare number", "shellcheck", run("shellcheck", "shellcheck --exclude=2164 x.sh"), Narrowed},
		{"shellcheck exclude other code", "shellcheck", run("shellcheck", "shellcheck -e SC1091 x.sh"), Valid},
		{"shellcheck include without floor code", "shellcheck", run("shellcheck", "shellcheck -i SC2086 x.sh"), Narrowed},
		{"shellcheck --include without floor code", "shellcheck", run("shellcheck", "shellcheck --include=SC2086 x.sh"), Narrowed},
		{"shellcheck include of the whole floor", "shellcheck", run("shellcheck", "shellcheck -i SC2164,SC2046,SC2115,SC2155,SC2034 x.sh"), Valid},
		{"shellcheck severity error above the floor", "shellcheck", run("shellcheck", "shellcheck --severity=error x.sh"), Narrowed},
		{"shellcheck -S error", "shellcheck", run("shellcheck", "shellcheck -S error x.sh"), Narrowed},
		{"shellcheck severity warning keeps the floor", "shellcheck", run("shellcheck", "shellcheck --severity=warning x.sh"), Valid},
		{"shellcheck-repo.sh severity error", "shellcheck", []Step{ghStep(".lighthouse/scripts/shellcheck-repo.sh --severity error")}, Narrowed},
		{"SHELLCHECK_OPTS exclude floor code", "shellcheck", withOpts("shellcheck", "shellcheck x.sh", "-e SC2164"), Narrowed},
		{"SHELLCHECK_OPTS with shellcheck-repo.sh", "shellcheck", func() []Step {
			s := ghStep(".lighthouse/scripts/shellcheck-repo.sh")
			s.Env = map[string]string{"SHELLCHECK_OPTS": "--exclude=SC2046"}
			return []Step{s}
		}(), Narrowed},
		{"SHELLCHECK_OPTS harmless", "shellcheck", withOpts("shellcheck", "shellcheck x.sh", "-x"), Valid},

		{"eslint no-config-lookup without config", "eslint", run("eslint", "eslint --no-config-lookup ."), Narrowed},
		{"eslint no-eslintrc without config", "eslint", run("eslint", "eslint --no-eslintrc ."), Narrowed},
		{"eslint ignore-pattern is scope", "eslint", run("eslint", "eslint --ignore-pattern dist ."), Valid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := onlyVerdict(t, c.steps, c.linter); got.Verdict != c.want {
				t.Fatalf("verdict %v (%s), want %v", got.Verdict, got.Reason, c.want)
			}
		})
	}
}

// The workflow and job env reach the step through Steps; SHELLCHECK_OPTS there narrows too.
func TestCallsShellcheckOptsFromWorkflowAndJobEnv(t *testing.T) {
	for _, yml := range []string{
		"on: push\nenv:\n  SHELLCHECK_OPTS: -e SC2164\njobs:\n  l:\n    runs-on: ubuntu-latest\n    steps:\n      - run: .lighthouse/scripts/shellcheck-repo.sh\n",
		"on: push\njobs:\n  l:\n    runs-on: ubuntu-latest\n    env:\n      SHELLCHECK_OPTS: -e SC2164\n    steps:\n      - run: .lighthouse/scripts/shellcheck-repo.sh\n",
		"on: push\njobs:\n  l:\n    runs-on: ubuntu-latest\n    steps:\n      - run: .lighthouse/scripts/shellcheck-repo.sh\n        env:\n          SHELLCHECK_OPTS: -e SC2164\n",
	} {
		got := onlyVerdict(t, stepsOf(t, map[string]string{wf + "ci.yml": yml}), "shellcheck")
		if got.Verdict != Narrowed {
			t.Errorf("verdict %v (%s) for\n%s", got.Verdict, got.Reason, yml)
		}
	}
}

func TestCallsConfigFlagAsksTheChecker(t *testing.T) {
	cases := []struct {
		linter, script, wantDir, wantCfg string
	}{
		{"ruff", "ruff check --config ci-lax.toml", ".", "ci-lax.toml"},
		{"golangci-lint", "golangci-lint run -c .golangci-ci.yml", ".", ".golangci-ci.yml"},
		{"golangci-lint", "golangci-lint run --config=.golangci-ci.yml", ".", ".golangci-ci.yml"},
		{"eslint", "eslint --no-config-lookup --config lax.config.js .", ".", "lax.config.js"},
		{"eslint", "cd web && npm ci && eslint -c lax.config.js .", "web", "lax.config.js"},
	}
	for _, c := range cases {
		t.Run(c.script, func(t *testing.T) {
			steps := pinned(c.linter, ghStep(c.script))
			var gotDir, gotCfg string
			narrows := true
			cfg := func(dir, p string) bool { gotDir, gotCfg = dir, p; return narrows }
			got := Calls(steps, c.linter, func(string) map[string]string { return nil }, floorFor(c.linter), cfg)
			if len(got) != 1 || got[0].Verdict != Narrowed {
				t.Fatalf("narrowing config: got %+v", got)
			}
			if gotDir != c.wantDir || gotCfg != c.wantCfg {
				t.Fatalf("cfgNarrows(%q, %q), want (%q, %q)", gotDir, gotCfg, c.wantDir, c.wantCfg)
			}
			narrows = false
			got = Calls(steps, c.linter, func(string) map[string]string { return nil }, floorFor(c.linter), cfg)
			if len(got) != 1 || got[0].Verdict != Valid {
				t.Fatalf("config that keeps the floor: got %+v", got)
			}
		})
	}
}

func TestCallsFloorComesFromTheParameter(t *testing.T) {
	steps := pinned("ruff", ghStep("ruff check --ignore E501"))
	got := Calls(steps, "ruff", nil, []string{"E501"}, func(string, string) bool { return false })
	if len(got) != 1 || got[0].Verdict != Narrowed {
		t.Fatalf("a floor code from the parameter must count: %+v", got)
	}
}

func TestWiredCallAggregation(t *testing.T) {
	steps := pinned("ruff", ghStep("ruff check --select E9"), ghStep("ruff check"))
	calls := callsOf(steps, "ruff")
	if len(calls) != 2 {
		t.Fatalf("got %+v", calls)
	}
	if c, ok := wiredCall(calls, "ruff"); !ok || c.Verdict != Valid {
		t.Fatalf("one valid call wires the linter: %+v %v", c, ok)
	}
	c, ok := wiredCall(calls[:1], "ruff")
	if ok || c.Verdict != Narrowed {
		t.Fatalf("first non-valid call is reported: %+v %v", c, ok)
	}
	c, ok = wiredCall(nil, "ruff")
	if ok || c.Verdict != NotFound || c.Reason != "no CI step runs ruff check" {
		t.Fatalf("no call: %+v %v", c, ok)
	}
	if c, _ := wiredCall(nil, "golangci-lint"); c.Reason != "no CI step runs golangci-lint" {
		t.Fatalf("no call: %+v", c)
	}
}

// A `severity=` line in .shellcheckrc is not a key shellcheck reads: the pinned binary still
// reports a warning with `severity=error` in the rc file. So the selection check of the rc file
// (Task 5) has no severity rule; only the --severity flag and SHELLCHECK_OPTS narrow by severity.
// Skips only when the pinned binary is not on this machine; the canary job fetches it.
func TestShellcheckrcSeverityIsNotAKey(t *testing.T) {
	bin := pinnedShellcheck(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t.sh"), []byte("#!/bin/bash\ncd /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".shellcheckrc"), []byte("severity=error\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "t.sh")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "SC2164 (warning)") {
		t.Fatalf("severity=error in .shellcheckrc hid a warning; the rc check needs a severity rule:\n%s", out)
	}
}

func pinnedShellcheck(t *testing.T) string {
	t.Helper()
	var cands []string
	if out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		cands = append(cands, filepath.Join(strings.TrimSpace(string(out)), "lighthouse-tools", "shellcheck"))
	}
	if p, err := exec.LookPath("shellcheck"); err == nil {
		cands = append(cands, p)
	}
	for _, p := range cands {
		out, err := exec.Command(p, "--version").Output()
		if err == nil && strings.Contains(string(out), "version: 0.11.0") {
			return p
		}
	}
	t.Skip("pinned shellcheck 0.11.0 not on this machine")
	return ""
}

func TestShellcheckCodeSeverity(t *testing.T) {
	for _, c := range shellFloor {
		if got := shellcheckCodeSeverity(c); got != "warning" {
			t.Errorf("%s: %q, want warning", c, got)
		}
	}
	if got := shellcheckCodeSeverity("SC9999"); got != "style" {
		t.Errorf("unknown code: %q, want style (the lowest, so any raised severity narrows)", got)
	}
}

func TestGitLabSteps(t *testing.T) {
	const pin = "  before_script:\n    - pip install ruff==0.9.0\n"
	cases := []struct {
		name string
		job  string
		want Verdict
	}{
		{"lint job", "lint:\n  script:\n    - ruff check .\n" + pin, Valid},
		{"allow_failure true", "lint:\n  allow_failure: true\n  script:\n    - ruff check .\n" + pin, Unrecognised},
		{"allow_failure false", "lint:\n  allow_failure: false\n  script:\n    - ruff check .\n" + pin, Valid},
		{"allow_failure exit_codes", "lint:\n  allow_failure:\n    exit_codes: 1\n  script:\n    - ruff check .\n" + pin, Unrecognised},
		{"when manual", "lint:\n  when: manual\n  script:\n    - ruff check .\n" + pin, Unrecognised},
		{"when never", "lint:\n  when: never\n  script:\n    - ruff check .\n" + pin, Unrecognised},
		{"when on_success", "lint:\n  when: on_success\n  script:\n    - ruff check .\n" + pin, Valid},
		{"rules manual", "lint:\n  rules:\n    - if: $CI_COMMIT_BRANCH\n      when: manual\n  script:\n    - ruff check .\n" + pin, Unrecognised},
		{"rules never", "lint:\n  rules:\n    - if: $CI_COMMIT_TAG\n      when: never\n    - when: on_success\n  script:\n    - ruff check .\n" + pin, Unrecognised},
		{"rules changes", "lint:\n  rules:\n    - changes: ['**/*.py']\n  script:\n    - ruff check .\n" + pin, Valid},
		{"or true", "lint:\n  script:\n    - ruff check . || true\n" + pin, Unrecognised},
		{"script as one string", "lint:\n  script: ruff check .\n" + pin, Valid},
		{"pin from default before_script", "default:\n  before_script:\n    - pip install ruff==0.9.0\nlint:\n  script:\n    - ruff check .\n", Valid},
		{"no pin", "lint:\n  script:\n    - ruff check .\n", Unpinned},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := writeRepo(t, map[string]string{".gitlab-ci.yml": "stages: [test]\nvariables:\n  X: y\n.hidden:\n  script:\n    - ruff check .\n" + c.job})
			steps, err := Steps(root, "gitlab")
			if err != nil {
				t.Fatal(err)
			}
			got := onlyVerdict(t, steps, "ruff")
			if got.Verdict != c.want {
				t.Fatalf("verdict %v (%s), want %v", got.Verdict, got.Reason, c.want)
			}
			if got.Step.File != ".gitlab-ci.yml" || got.Step.Job != "lint" || got.Step.Line == 0 || !got.Step.GitLab {
				t.Fatalf("step %+v", got.Step)
			}
		})
	}
}

func TestGitLabVariablesReachShellcheckOpts(t *testing.T) {
	root := writeRepo(t, map[string]string{".gitlab-ci.yml": "variables:\n  SHELLCHECK_OPTS: -e SC2164\nlint:\n  script:\n    - .lighthouse/scripts/shellcheck-repo.sh\n"})
	steps, err := Steps(root, "gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if got := onlyVerdict(t, steps, "shellcheck"); got.Verdict != Narrowed {
		t.Fatalf("got %+v", got)
	}
}

func TestGitLabMissingAndInvalid(t *testing.T) {
	steps, err := Steps(writeRepo(t, map[string]string{"x": ""}), "gitlab")
	if err != nil || len(steps) != 0 {
		t.Fatalf("missing file: %v %v", steps, err)
	}
	_, err = Steps(writeRepo(t, map[string]string{".gitlab-ci.yml": "lint: [\n"}), "gitlab")
	if _, ok := err.(UncheckedError); !ok {
		t.Fatalf("invalid YAML: want UncheckedError, got %v", err)
	}
}

// Classification: ci-parse. Fails when GitLab's extends:, allow_failure in rules: or
// inherit: default: false is ignored: a job that may fail, or whose pin proof does not run, then
// counts as wired.
func TestGitLabExtendsRulesInherit(t *testing.T) {
	const script = "  script:\n    - pip install ruff==0.9.0\n    - ruff check\n"
	cases := []struct {
		name string
		yml  string
		want Verdict
	}{
		{"extends template with allow_failure", ".tmpl:\n  allow_failure: true\nlint:\n  extends: .tmpl\n" + script, Unrecognised},
		{"extends list, last template with allow_failure", ".a:\n  stage: test\n.b:\n  allow_failure: true\nlint:\n  extends: [.a, .b]\n" + script, Unrecognised},
		{"extends chain with when manual", ".a:\n  when: manual\n.b:\n  extends: .a\nlint:\n  extends: .b\n" + script, Unrecognised},
		{"extends template with rules allow_failure", ".a:\n  rules:\n    - if: $CI\n      allow_failure: true\nlint:\n  extends: .a\n" + script, Unrecognised},
		{"job overrides template allow_failure", ".tmpl:\n  allow_failure: true\nlint:\n  extends: .tmpl\n  allow_failure: false\n" + script, Valid},
		{"script from the template", ".tmpl:\n  script:\n    - pip install ruff==0.9.0\n    - ruff check\nlint:\n  extends: .tmpl\n", Valid},
		{"extends not in the file", "include: other.yml\nlint:\n  extends: .elsewhere\n" + script, Unrecognised},
		{"rules allow_failure", "lint:\n  rules:\n    - if: $CI\n      allow_failure: true\n" + script, Unrecognised},
		{"rules allow_failure false", "lint:\n  rules:\n    - if: $CI\n      allow_failure: false\n" + script, Valid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps, err := Steps(writeRepo(t, map[string]string{".gitlab-ci.yml": c.yml}), "gitlab")
			if err != nil {
				t.Fatal(err)
			}
			got := onlyVerdict(t, steps, "ruff")
			if got.Verdict != c.want {
				t.Fatalf("verdict %v (%s), want %v", got.Verdict, got.Reason, c.want)
			}
		})
	}
}

// Classification: ci-parse. Fails when the default before_script proves the pin for a job that
// does not inherit it.
func TestGitLabInheritDefault(t *testing.T) {
	const head = "default:\n  before_script:\n    - pip install ruff==0.9.0\nlint:\n  script:\n    - ruff check\n"
	cases := map[string]Verdict{
		"":                                   Valid,
		"  inherit:\n    default: true\n":    Valid,
		"  inherit:\n    default: false\n":   Unpinned,
		"  inherit:\n    default: [image]\n": Unpinned,
		"  inherit:\n    default: [before_script]\n": Valid,
	}
	for inherit, want := range cases {
		steps, err := Steps(writeRepo(t, map[string]string{".gitlab-ci.yml": head + inherit}), "gitlab")
		if err != nil {
			t.Fatal(err)
		}
		if got := onlyVerdict(t, steps, "ruff"); got.Verdict != want {
			t.Errorf("inherit %q: verdict %v (%s), want %v", inherit, got.Verdict, got.Reason, want)
		}
	}
}
