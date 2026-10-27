# Getting started

Lighthouse is a set of skills, hooks and instructions that `lighthouse init` installs into your
repository, so that an AI coding agent works in the same chain from issue to merged PR every
time. It is for a developer with a GitHub repository and Claude Code. Without the controller
(`lighthouse serve`, early development) you already get worktrees, a test gate before every
commit, PR bodies with evidence and adversarial reviews.

## Requirements

`lighthouse init` picks packages by what it finds in your repository: `core` always, and `go`,
`python`, `shell` or `expo` when they apply. Each package names the tools it needs.

| Requirement | Package | Needed for | Without it |
|---|---|---|---|
| Claude Code | all | running the skills and the hooks | The hooks do not run. Codex and OpenCode read `AGENTS.md`, but nothing enforces it: run the test gate yourself. |
| Python 3.11+ (`python3`, `tomllib`) | all | every hook, the helper scripts of the skills, reading `lighthouse.toml` | The core hooks report `COULD NOT CHECK`, and the blocking ones block: no commit passes `main-commit-guard` or `pre-commit-tests`. |
| `git` | core, expo, go, shell | worktrees, the commit guards, every skill | Nothing works: the guards report `COULD NOT CHECK` and block. |
| `jq` | core, go, python, shell | reading the payload of a hook | The hooks report `COULD NOT CHECK`; `main-commit-guard`, `pre-commit-tests`, `pr-create-gate` and `worktree-linkcheck` block. |
| `gh` | core, python | reading and writing issues and PRs | The issue and PR skills cannot reach GitHub. `pr-create-gate` passes without counting the acceptance criteria, and says so. |
| `superpowers` (Claude Code plugin) | core, expo | the steps the skills hand off: worktrees, brainstorming, plans, debugging | The skills cannot hand off; `/lighthouse-start-issue` loses its worktree, spec and plan steps. |
| `codex` (optional) | core | adversarial reviews by another model family; `lighthouse usage codex` | `/lighthouse-spec-adversarial-review` and `/lighthouse-plan-adversarial-review` stop with `CODEX_MISSING`. |
| `uv` | python | `ruff-format`, `ty-check`, the test gate `uv run pytest -q` | `ty-check` reports `COULD NOT CHECK`; `ruff-format` falls back to `ruff` on the `PATH`, else blocks with `COULD NOT CHECK`. The test gate fails red and blocks the commit. |
| `go` | go | `go-check` | `go-check` reports `COULD NOT CHECK` at Stop; nothing was linted. |
| `gofmt` | go | `gofmt` after every edit | `gofmt` blocks with `COULD NOT CHECK`. |
| `golangci-lint` | go | `go-check` | `go-check` reports `COULD NOT CHECK` at Stop; nothing was linted. |
| `curl` | shell | the first download of the pinned `shellcheck` | No download: `shellcheck` blocks with `COULD NOT CHECK` on every shell script you edit. |
| `sha256sum` | shell | verifying that download | As for `curl`. |
| `tar` | shell | unpacking that download | As for `curl`. |
| `xz` | shell | unpacking that download | As for `curl`. |

`init` reports a missing tool as `missing dependency: <tool> (needed by: …)` and goes on.
