# Getting started

Lighthouse is a set of skills, hooks and instructions that `lighthouse init` installs into your
repository, so that an AI coding agent works in the same chain from issue to merged PR every
time. It is for a developer with a GitHub repository and Claude Code. Without the controller
(`lighthouse serve`, early development) you already get worktrees, a test gate before every
commit, PR bodies with evidence and, with `codex`, adversarial reviews.

## Requirements

`lighthouse init` picks packages by what it finds in your repository: `core` always, and `go`,
`python`, `shell`, `node` or `expo` when they apply. Each package names the tools it needs.

| Requirement | Package | Needed for | Without it |
|---|---|---|---|
| Claude Code | all | running the skills and the hooks | The hooks do not run. Codex and OpenCode read `AGENTS.md`, but nothing enforces it: run the test gate yourself. |
| Python 3.11+ (`python3`, `tomllib`) | all | every hook, the helper scripts of the skills, reading `lighthouse.toml` | The core hooks report `COULD NOT CHECK`, and the blocking ones block: no commit passes `main-commit-guard` or `pre-commit-tests`. |
| `git` | core, expo, go, shell | worktrees, the commit guards, every skill | Nothing works: the hooks report `COULD NOT CHECK`; the blocking ones block, `worktree-guard` only warns. |
| `jq` | core, go, python, shell | reading the payload of a hook | The hooks report `COULD NOT CHECK`; `main-commit-guard`, `pre-commit-tests`, `pr-create-gate` and `worktree-linkcheck` block. |
| `gh` | core, python | reading and writing issues and PRs | The issue and PR skills cannot reach GitHub. `pr-create-gate` passes without counting the acceptance criteria, and says so. |
| `superpowers` (Claude Code plugin) | core, expo | the steps the skills hand off: worktrees, brainstorming, plans, debugging | The skills cannot hand off; `/lighthouse-start-issue` loses its worktree, spec and plan steps. |
| `codex` (optional) | core | adversarial reviews by another model family; `lighthouse usage codex` | `/lighthouse-spec-adversarial-review` and `/lighthouse-plan-adversarial-review` stop with `CODEX_MISSING`. |
| `uv` | python | `ruff-format`, `ty-check`, the test gate `uv run pytest -q` | `ty-check` reports `COULD NOT CHECK`; `ruff-format` falls back to `ruff` on the `PATH`, else blocks with `COULD NOT CHECK`. The test gate `uv run pytest -q` cannot start: `pre-commit-tests` reports `COULD NOT CHECK` and blocks the commit. |
| `go` | go | `go-check` | `go-check` reports `COULD NOT CHECK` at Stop; nothing was linted. |
| `gofmt` | go | `gofmt` after every edit | `gofmt` blocks with `COULD NOT CHECK`. |
| `golangci-lint` | go | `go-check` | `go-check` reports `COULD NOT CHECK` at Stop; nothing was linted. |
| `node` | node | the test gate `gate_node.py` | `gate_node.py` reports `COULD NOT CHECK` (exit 3); with 3 in `unchecked_exit_codes` the gate reports the commit as not tested. |
| `npm` | node | the web checks of `gate_node.py` (`npm run …`) | As for `node`. |
| `curl` | shell | the first download of the pinned `shellcheck` | No download. Without a `shellcheck` on your `PATH`, the `shellcheck` hook blocks with `COULD NOT CHECK` on every shell script you edit. |
| `sha256sum` | shell | verifying that download | As for `curl`. |
| `tar` | shell | unpacking that download | As for `curl`. |
| `xz` | shell | unpacking that download | As for `curl`. |

`init` reports a missing tool as `missing dependency: <tool> (needed by: …)` and goes on.

## Install

Download the binary for your platform from the latest release and check it against
`SHA256SUMS` before it lands in `~/.local/bin`. For Linux on amd64, paste this block in one go;
it stops at the first step that fails:

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
sha256sum --check --ignore-missing SHA256SUMS &&
chmod +x lighthouse_linux_amd64 &&
mkdir -p ~/.local/bin && mv lighthouse_linux_amd64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

Not on your `PATH` yet? Add `export PATH="$HOME/.local/bin:$PATH"` to your shell profile. The
other platforms are on the site, <https://corneeldh.github.io/lighthouse/#download>; the details
are in [install.md](install.md#download-a-release-binary). Or install with Go:

```bash
go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest
```

`@latest` resolves to the newest tag; put a tag such as `@v0.1.6` there to pin one. The binary
lands in `~/go/bin`, so make sure that is on your `PATH`.

Install a tag, not `main`. A build from `main` or from a clone reports version `dev`, and
`lighthouse init` writes that version into `.lighthouse/manifest.toml`.

## Run lighthouse init

In the root of your own repository:

```bash
cd path/to/your-repo
lighthouse init
```

`init` names the packages it picked, prints a diff per file it would write, and asks
`Apply these changes? [y/N]`. Nothing is written until you answer `y`. It reports a missing tool
and goes on (see [Requirements](#requirements)).

- `lighthouse init --yes` approves every change without asking, for CI or an agent. Without a
  terminal the question reads empty stdin, which counts as no: nothing is written and `init`
  exits 1.
- `lighthouse init --upgrade` replaces what an earlier `init` installed. A second `init`
  without `--upgrade` refuses. A unit you changed locally makes the whole run refuse, and nothing
  is written, unless you add `--force`.
- `lighthouse init --pack python` adds a package, `lighthouse init --no-pack shell` leaves one
  out. Both work later with `--upgrade`, and both repeat.

What `init` writes, all of it on the diff first:

- `.claude/skills/lighthouse-<name>/` and `.agents/skills/lighthouse-<name>/`: the skills, for
  Claude Code and for Codex and OpenCode.
- `.claude/agents/lighthouse-<name>.md`: the agents the skills dispatch.
- `.claude/hooks/lighthouse/`: the hooks, registered in `.claude/settings.json` next to your own
  entries, with the permissions they need.
- `.lighthouse/lib/lighthouse_lib/` and `.lighthouse/scripts/`: the helpers of the hooks and
  skills; `.lighthouse/headings.toml`: the headings the skills write and read.
- `.lighthouse/manifest.toml`: what was installed, at which version.
- `AGENTS.md`: a managed block with the chain rules, one per package that has rules. Your own
  text stays. `CLAUDE.md` gets the line `@AGENTS.md` only when neither file exists yet. Claude
  Code reads the rules through that line: if you already have a `CLAUDE.md`, or only an
  `AGENTS.md`, add `@AGENTS.md` to `CLAUDE.md` yourself.
- `.github/pull_request_template.md` and `.github/workflows/lighthouse-<name>.yml`.
- `lighthouse.toml`: the keys that are missing, see below. Existing keys stay as they are.

On a machine with Tailscale, the first `init` also offers to write
`~/.config/lighthouse/host.toml`; `--yes` writes it without asking. Without Tailscale you see
`warning: host.toml not proposed: tailscale ip unavailable; skills that check the host will
report it`.

## Configure lighthouse.toml

`init` proposes `[project] language`, and `[forge]` from your `origin` remote when it is on
GitHub. Only the `python` package proposes a test gate. Until you set `[test_gate] command`,
`pre-commit-tests` passes every commit with `no test command configured, nothing was tested`.
Check the file, and set your test gate:

```toml
[project]
language = "en"

[forge]
kind = "github"
repo = "your-name/your-repo"

[test_gate]
command = "<your test command>"
```

| Key | What it does |
|---|---|
| `project.language` | The language of your artefacts: issues, PRs, comments, commit messages. `nl` or `en`; default `en`. |
| `project.code_language` | Set it to `en` to keep names, comments and file names in English while artefacts follow `project.language`. |
| `project.code_language_paths` | The globs that `project.code_language` covers. |
| `forge.kind` | `github`, the only forge so far. |
| `forge.repo` | `owner/name` of the repository on GitHub. |
| `forge.default_branch` | The branch nothing lands on directly; default `main`. |
| `test_gate.command` | What `pre-commit-tests` runs before every commit. An empty `command = ""` turns the gate off. The `python` package sets `uv run pytest -q`. |
| `paths.specs`, `paths.plans` | Where specs and plans go; default `docs/developer/planning/specs` and `docs/developer/planning/plans`. |
| `forge.board` | Optional: a GitHub Project board the skills move issues on. |

`[forge.board]` is not in the example. When you add it, it needs `project_number`, `project_id`,
`status_field_id`, and `status_options` with an option ID for each of `To do`, `In progress`,
`Done` and `On hold`.

Every other key, with its type and default, is in `SCHEMA` and `DEFAULTS` of
`.lighthouse/lib/lighthouse_lib/config.py`. A key with a wrong type or value is an error that
names the file and the key.

## Your first issue

The chain runs from an issue to a merged PR. Each step names what you type, what you see, and
which hook can step in. Run the skills in Claude Code, in your repository.
To see the chain as a map, open [the chain map](https://corneeldh.github.io/lighthouse/chain.html).
In a clone of the lighthouse repository, run `python3 -m http.server --directory docs/site` from its
root and open [the local copy](site/chain.html) at `http://localhost:8000/chain.html`.
What happens within one turn, which skill joins in and which hook steps in, is on
[the session map](https://corneeldh.github.io/lighthouse/session.html), and locally at
[site/session.html](site/session.html).

1. **Write the issue.** Type `/lighthouse-write-issue` and describe the change. You get an issue
   on GitHub with acceptance criteria, in `project.language`.
2. **Start on it.** Type `/lighthouse-start-issue <nr>`. You see a worktree under
   `.claude/worktrees/` on a branch `issue-<nr>-<slug>`, and one route line:
   `#<nr> Gate <n>[a|b|c]: <route> — <reason>`. Gate `4a` builds directly. Gate `4b` writes a
   plan first and gate `4c` a spec first; both end in a handoff, a start command for a fresh
   session.
3. **Build in the worktree.** `pre-commit-tests` runs your `[test_gate] command` before every
   `git commit`; a red run blocks the commit. `main-commit-guard` blocks a commit or push on
   `main`, also through `git -C <path>` or a preceding `cd`. `worktree-guard` warns when you
   write into the main checkout on `main`.
4. **Open the PR.** Type `/lighthouse-verify-pr`. It checks each acceptance criterion on the
   branch and writes the PR body with an evidence table. `pr-create-gate` blocks a
   `gh pr create` whose body lacks `Closes #N` with evidence, `Refs #N — <reason>` or
   `No issue: <reason>`.
5. **After the merge.** Type `/lighthouse-close-issue <nr>`. A `Closes #N` PR closed the issue on
   merge; the skill checks the evidence and reopens the issue when it does not hold.

The hooks run only in Claude Code. In Codex and OpenCode the skills and `AGENTS.md` are there,
but no hook is: check the branch yourself before a commit, and run your `[test_gate] command`.
