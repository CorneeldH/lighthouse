---
name: lighthouse-go-standard
description: Use when writing or changing Go in this repository — packages, `_test.go` files, and the gofmt/vet/golangci-lint/race checks on them — and when planning a refactor, merging packages or choosing a test strategy, before a file is open. Carries a KPI table whose every row is a command that runs in this repository, plus the measurement rules (a zero without its GOOS, build tags, -race and GOWORK is not a zero). NOT for Python (use lighthouse-python-standard if present), shell that guards something (lighthouse-shell-standard if present), workflow YAML, or files outside the repository. In OpenCode the enforcing hooks do not run — run the KPI commands by hand.
---

# Writing Go in this repository

## Domain

Go code and the checks on it: `gofmt`, `go vet`, `golangci-lint`, `go test`. Also applies when no
file is open yet — while brainstorming or planning, when you decide *how* you will test something
and how you will trim it. That is why this is a skill and not a rule.

Out of scope: Python (`/lighthouse-python-standard`, if present), shell that guards something
(`/lighthouse-shell-standard`, if present) and workflow YAML.

This file carries the procedure, the measurement and the norms that apply before the first file.
A project that keeps norms per file in `.claude/rules/go.md` (if present) has them there as well.

What a good test is lives in `lighthouse-test-standard`; this file gives only its Go form (below).
Before the first edit, write the classification sentence from that skill in the chat; the classes
and their minimal checks are in its section *Classify first*:

```text
This is a <class> change; the likely regression surface is <observable behaviour>.
```

## KPIs

Every row is a command that runs in this repository.

| Norm | Command | Target |
|---|---|---|
| Formatted | `git ls-files -z '*.go' \| xargs -0r gofmt -l` | empty output |
| Modules tidy | `go mod tidy -diff` | empty output |
| Vet clean | `go vet ./...` | rc 0 |
| Lints at zero | `golangci-lint run ./...` | rc 0 |
| Tests green, shuffled and race-free | `go test -race -shuffle=on ./...` | rc 0, naming `-race` |
| The test gate is green | `bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"` | rc 0 |

Row 1 runs over `git ls-files` and not over `.`: from the main checkout, `gofmt -l .` also walks
into `.claude/worktrees/`. The `-r` keeps `xargs` from starting `gofmt` without files, where it
would wait on stdin. Row 2 needs Go 1.23 or later; on an older Go it is not measurable from here.

`-shuffle=on` is the default, so an order dependency between tests shows. A run with
`-shuffle=on` never comes from the test cache, so `(cached)` does not appear there. Use
`-count=1` only in a run without shuffle, when you suspect a stale cached result.

Row 6 is the command the test gate runs after every commit. The gate runs it on the files of that
commit; `python3 .lighthouse/scripts/gate_go.py -- go test -race` picks the packages of the commit.
A red run comes back to you: fix it in a new commit. The selector exits 3 when it could not
check (no `go`, a list it cannot read, a failing `go list`, a command that cannot start): list 3
in `[test_gate] unchecked_exit_codes`. When `config.py test_gate.command` gives rc 1 or an empty
line, the repository has no test gate: set one in `lighthouse.toml`, for a
Go repository `[test_gate] command = "python3 .lighthouse/scripts/gate_go.py -- go test -race"`.
The gate leaves out `-shuffle=on`: that turns off Go's test cache. CI keeps it.

Note: the test gate runs only in Claude Code. In OpenCode nothing local runs the suite before a
commit or push — run `bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"`
by hand.

## Non-negotiable

These two are about the measurement, not about the file.

- **A zero without its environment is not a zero.** Name GOOS, the build tags, `-race` on or off,
  and `GOWORK` on or off. ✅ "tests green, linux, -race, GOWORK=off" · ❌ "tests green".
- **Three outcomes, not two**: passed, failed, and *not measurable from here* — cgo for another
  OS, or `-race` without cgo. ✅ saying that the cgo files of another OS are not measurable here ·
  ❌ presenting the zero on this machine as if it covers them.

## Reporting done

While you work, run focused tests: `go test -run <Name> ./<pkg>/...`. Before you say done, run
every KPI row, also those you think you did not touch. Report per row:

- the command, as it ran;
- the outcome: passed, failed, or not measurable from here;
- the environment: GOOS, the build tags, `-race` on or off, `GOWORK` on or off;
- what was not measurable, and why.

A row left out of the report reads as passed. Leave none out.

## Merging or deepening: replace, don't layer

When you merge shallow packages into one deeper interface, or pull logic behind an existing
interface, the tests on the old, shallow packages are waste as soon as the new interface covers
the same behaviour. Remove them in the same change; do not leave them next to the new tests.

- ✅ `a_test.go` and `b_test.go` gone in the commit in which `ab_test.go` tests their behaviour
  through the new interface · ❌ keeping both sets "to be safe".
- **First covered, then gone.** An old test goes only once the behaviour it tests is tested
  through the new interface. Otherwise removing it is losing coverage, not cleaning up.
- **No interface with one implementation "for later".** Keep interfaces small and define them on
  the side of the code that uses them, at the moment a second implementation or a test fake needs
  one. An interface that only passes calls through is a *pass-through method* in another form.

Why here and not in a rule: the decision falls while planning ("we merge A and B"), before a
source or test file is open. Why no KPI row: whether an old test covers the same behaviour as a
new one is semantic; grep does not see the difference.

## The Go form of lighthouse-test-standard

The norm is in `lighthouse-test-standard`; these are the Go tools for it.

- **Literal expectations.** Table-driven tests with a literal `want` per case, never one computed
  in the test.
- **The real thing, not a mocked collaborator.** `t.TempDir()` for the filesystem, `httptest` for
  HTTP, `t.Setenv` for the environment.
- **`require` against `assert`, only with testify.** `require` where the rest of the test is
  meaningless without that condition; `assert` where the test can go on and report more. Without
  testify the same choice is `t.Fatalf` against `t.Errorf`.
- **Specific fakes.** A fake implements the interface of the code that uses it and checks the
  arguments that belong to the contract; never one that records every call without checking
  anything.

## Reaching a complexity limit: deepen, don't split

When a function grows too complex to follow, solve it by hiding a decision behind a type or a
function of its own, with its own test, not by chopping it into pieces that can only be
understood together. This section gives the norm, not a number: whether `gocyclo` or `gocognit`
measure it depends on the project's `.golangci.yml`.

- ✅ replacing a `switch` over message types with a map of handlers, or lifting the parsing into
  a pure function with its own table-driven test, so the caller reads one line · ❌
  `step1(a, b, c, d)` and `step2(a, b, c, d)` that are called once and pass the local state back
  and forth (*conjoined methods*), or a helper that only passes through (*pass-through method*).
- **Why an agent needs this.** It optimizes literally for the number, and splitting lowers the
  number without making anything simpler.

Source: Ousterhout, *A Philosophy of Software Design* (deep modules, *conjoined methods*,
*pass-through method*), the same thought as "replace, don't layer" above. Why here and not in a
rule: the decision falls while planning the refactor ("how do I trim this function"), before a
source file is open.

## The pairing

| Half | Carries | When |
|---|---|---|
| this skill | the procedure, the KPI table, the measurement rules | while planning and writing |
| `lighthouse-test-standard` | the test norms and the class table | for every test, before the first edit |
| `.claude/hooks/lighthouse/gofmt.sh` | formatting, per edit | right after the edit |
| `.claude/hooks/lighthouse/go-check.sh` | `golangci-lint` (`govet` included), once per turn, where the turn touched a Go path | at `Stop` |

`gofmt.sh` formats the file in place and blocks (exit 2) only on a file that does not parse.
`go-check.sh` blocks on a finding, and on a build that fails, except while a valid usage alarm is
up (see `docs/usage-guard.md`, *Andere Stop-gates*): `golangci-lint` reports both the
same way. It lints every checkout the turn wrote in, read from the transcript, and only when the turn
touched a Go path there (a `.go` file, `go.mod`, `go.sum`, `go.work`, or a `.golangci` file or pin:
an edit, a Bash write or a commit in the turn); any other checkout gets `not checked`. A worktree
you edit from a session in the main checkout is linted, not the main checkout. When a hook cannot
check — `gofmt.sh` needs `jq` and `gofmt`; `go-check.sh` needs `jq`, `git`, `python3`, `go` and
`golangci-lint`, at the version in `.golangci-version`, and a transcript that names the turn's
checkout — it says so with `COULD NOT CHECK` instead of passing silently. `gofmt.sh` then blocks:
an unchecked file must not read as a clean one. `go-check.sh` runs at `Stop`, where blocking would
keep the session from stopping, so it reports it as a message. A repository without a root
`go.mod` gets `not checked` from `go-check`: Go only in a subdirectory is never linted at Stop.

> **OpenCode does not run these hooks.** Claude Code hooks do not fire here, and
> `.claude/rules/go.md` (if present) is not loaded by itself, so both the enforcement and the
> norm half are missing. After every Go change, run by hand: `gofmt -w <file>`, and before you
> stop, `golangci-lint run ./...`. Open the norms yourself, as `AGENTS.md` prescribes. This is a
> deliberate asymmetry, not an omission: do not "fix" it by removing the hook reference.

## Known pitfalls

- **`go.work` locally, not on CI.** Locally you build against a local module. Verify with
  `GOWORK=off`.
- **`-race` needs cgo.** With `CGO_ENABLED=0`, `-race` fails or does not run. A green run without
  `-race` says nothing about concurrency.
- **Cross-OS vet skips cgo files**, because cross-compiling turns cgo off by default. Those files
  are not measurable from here.
- **`t.Setenv` with `t.Parallel()` panics.** Package-level `var` overrides in tests are shared
  state: with `t.Parallel()` they are a race.
- **Loop variable semantics depend on the `go` directive.** Below `go 1.22` a closure in a `for`
  loop shares the variable. Check the `go` directive in `go.mod` before you rely on it.
- **A `toolchain` directive with `GOTOOLCHAIN=auto`** can run another Go than you think.
  `go version` in the repository root says which one runs.
- **Build tags fall outside `go vet ./...` and `go test ./...`.** A file behind a tag such as `e2e`
  is only built with `-tags e2e`; `golangci-lint` takes the tags from `.golangci.yml`.
- **Lint debt.** A repository with old findings sets `issues.new-from-rev` in its own
  `.golangci.yml`; then the hook and the KPI row `golangci-lint run ./...` measure only new
  findings, without a config of lighthouse's own. The KPI row `go vet ./...` does not read
  `.golangci.yml` and still measures the old vet debt: report that row with that caveat.
- **`go generate`.** When the repository has `//go:generate` lines, generated code can go stale;
  run `go generate ./...` and check that `git diff --exit-code` stays empty.
