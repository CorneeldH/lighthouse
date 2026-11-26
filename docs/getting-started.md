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
| Python 3.11+ (`python3`, `tomllib`) | python | `ty-gate`, `gate_python` and the `python-standard` skill | Those hooks and that skill cannot run. The other packages need no Python. |
| `git` | core, expo, go, powershell, shell | worktrees, the commit gates, every skill | Nothing works: the hooks report `COULD NOT CHECK`; the blocking ones block, `worktree-check` only warns. |
| `jq` | core, go, powershell, python, shell | the helper commands of the skills | Those skill steps fail. Every hook runs in the `lighthouse` binary and needs no `jq`. |
| `gh` | core, python | reading and writing issues and PRs in the skills that do not yet use `lighthouse forge` | Those skills cannot reach GitHub. `pr-create-gate` passes without counting the acceptance criteria, and says so. The chain skills `start-issue`, `route-issue`, `write-issue` and `write-pr` go through `lighthouse forge` and need `gh` only for the fallback `gh auth token`. |
| `superpowers` (Claude Code plugin) | core, expo | the steps the skills hand off: worktrees, brainstorming, plans, debugging | The skills cannot hand off; `/lighthouse-start-issue` loses its worktree, spec and plan steps. |
| `codex` (optional) | core | adversarial reviews by another model family; `lighthouse usage codex` | `/lighthouse-review-spec` and `/lighthouse-review-plan` stop with `CODEX_MISSING`. |
| `uv` | python | `ruff-format-check`, `ty-gate`, `python-imports-gate` (deptry), the test gate `uv run pytest -q` | `ty-gate` and `python-imports-gate` report `COULD NOT CHECK`; `ruff-format-check` falls back to `ruff` on the `PATH`, else blocks with `COULD NOT CHECK`. The test gate `uv run pytest -q` cannot start: `commit-test-gate` reports `COULD NOT CHECK` and blocks the commit. |
| `go` | go | `go-lint-gate`, `go-imports-gate` (`go mod tidy -diff`) | `go-lint-gate` and `go-imports-gate` report `COULD NOT CHECK` at Stop; nothing was checked. |
| `gofmt` | go | `gofmt-check` after every edit | `gofmt-check` blocks with `COULD NOT CHECK`. |
| `golangci-lint` | go | `go-lint-gate` | `go-lint-gate` reports `COULD NOT CHECK` at Stop; nothing was linted. |
| `npx` | node | `node-imports-gate` (knip) | `node-imports-gate` reports `COULD NOT CHECK` at Stop; nothing was checked. |
| `node` | node | the test gate `lighthouse gate node` | `lighthouse gate node` reports `COULD NOT CHECK` (exit 3); with 3 in `unchecked_exit_codes` the gate reports the commit as not tested. |
| `npm` | node | the web checks of `lighthouse gate node` (`npm run …`) | As for `node`. |
| `pwsh` | powershell | `powershell-lint-check` runs PSScriptAnalyzer in it | `powershell-lint-check` blocks with `COULD NOT CHECK` on every `.ps1` or `.psm1` file you edit. |
| `PSScriptAnalyzer` (PowerShell module) | powershell | the rules of `powershell-lint-check` | As for `pwsh`. Install it with `Install-Module PSScriptAnalyzer -Scope CurrentUser` (or `-Scope AllUsers`, which needs admin rights); the hook sees either. |
| `curl` | shell | the first download of the pinned `shellcheck`, verified with sha256sum or shasum | No download. Without a `shellcheck` on your `PATH`, the `shell-lint-check` hook blocks with `COULD NOT CHECK` on every shell script you edit. |
| `tar` | shell | unpacking that download | As for `curl`. |
| `xz` | shell | unpacking that download | As for `curl`. |

`init` reports a missing tool as `missing dependency: <tool> (needed by: …)` and goes on.

## Install

Rather have your agent do it? Paste this sentence into Claude Code, Codex or OpenCode, in your
repository:

```text
Read https://corneeldh.github.io/lighthouse/agents/install.md and install lighthouse in this repo.
```

The agent follows [the agent guide](https://corneeldh.github.io/lighthouse/agents/install.md): it
asks before it installs or writes anything. To install by hand, read on.

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
other platforms are on the site, <https://corneeldh.github.io/lighthouse/install.html>; the details
are in [install.md](install.md#download-a-release-binary). Or install with Go:

```bash
go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest
```

`@latest` resolves to the newest tag; put a tag such as `@v0.1.6` there to pin one. The binary
lands in `~/go/bin`, so make sure that is on your `PATH`.

Install a tag, not `main`. A build from `main` or from a clone reports version `dev`, and
`lighthouse init` writes that version into `.lighthouse/manifest.toml`.

### Windows

Lighthouse runs natively on Windows: Claude Code runs the hooks in Git Bash, and every core hook
passes there, the test gate included (measured by `windows-measure.yml`). What you need:

- [Git for Windows](https://gitforwindows.org/). Without it, Claude Code runs the hooks in
  PowerShell, where they do not work.
- `lighthouse_windows_amd64.exe` from the Windows tab on the site, saved as `lighthouse.exe` in a
  folder on your `PATH`. Restart Claude Code after you change the `PATH`.
- Run `lighthouse init` in Git Bash.

`lighthouse serve` and the controller do not run natively on Windows; neither does the `shell`
package, whose pinned `shellcheck` download has no Windows build. WSL also works: install Claude
Code and lighthouse inside WSL, with the Linux block, and keep the repository in the WSL file
system.

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
- `lighthouse init --upgrade --audit` also shows the agent-audit summary that the first `init`
  shows after it wrote: conflicts between your hooks and permissions and lighthouse's, and
  project hooks that may do what a lighthouse hook does. It changes nothing; see
  [Agent audit](agent-audit.md).
- `lighthouse init --forge-host <host>` tells `init` that your `origin` remote is on a
  self-hosted GitLab, so it proposes `[forge]` with `kind = "gitlab"` and `host`.
- `lighthouse init --pack python` adds a package, `lighthouse init --no-pack shell` leaves one
  out. Both work later with `--upgrade`, and both repeat.
- `lighthouse init --labels` also proposes a label taxonomy, with `[labels]` and `[paths] labels`
  in `lighthouse.toml`, in the same diff and under the same approval. `--create-labels`, only
  with `--labels`, then creates the missing labels on the forge (GitHub or GitLab) and asks nothing more: the
  flag is the approval, also when there is nothing to apply (`No changes.`).
- `lighthouse init --direction` also seeds the optional direction documents (strategy, tenets, one
  file per area label) where they are missing; see [Direction documents](richting.md).

What `init` writes, all of it on the diff first:

- `.claude/skills/lighthouse-<name>/` and `.agents/skills/lighthouse-<name>/`: the skills, for
  Claude Code and for Codex and OpenCode.
- `.claude/agents/lighthouse-<name>.md`: the agents the skills dispatch.
- `.claude/hooks/lighthouse/`: the hooks, registered in `.claude/settings.json` next to your own
  entries, with the permissions they need.
- `.lighthouse/scripts/`: the scripts of a language package (`shell`, `python`) that its
  skills and the test gate run; `.lighthouse/headings.toml`: the headings the skills write
  and read.
- `.lighthouse/manifest.toml`: what was installed, at which version.
- `AGENTS.md`: a managed block with the chain rules, one per package that has rules. Your own
  text stays. `CLAUDE.md` gets the line `@AGENTS.md` only when neither file exists yet. Claude
  Code reads the rules through that line: if you already have a `CLAUDE.md`, or only an
  `AGENTS.md`, add `@AGENTS.md` to `CLAUDE.md` yourself.
- `.github/pull_request_template.md` and `.github/workflows/lighthouse-<name>.yml`.
- `.github/dependabot.yml`, one block per ecosystem found, only when the file does not exist; see
  [Dependabot](dependabot.md).
- `lighthouse.toml`: the keys that are missing, see below. Existing keys stay as they are.

Each of these places, and whether `init` adds it or edits a file of yours, is on [the repo map](site/repo.html) (also online: [Your repo after init](https://corneeldh.github.io/lighthouse/repo.html)).

On a machine with Tailscale, the first `init` also offers to write
`~/.config/lighthouse/host.toml`; `--yes` writes it without asking. Without Tailscale you see
`warning: host.toml not proposed: tailscale ip unavailable; lighthouse works without host.toml.
For the gate_* keys (memory brake, slots) write it by hand: see "A host.toml without Tailscale" in
the lighthouse getting-started guide`: the section after "Remove lighthouse".

## Remove lighthouse

```bash
lighthouse uninstall
```

`uninstall` removes what `init` installed, as listed in `.lighthouse/manifest.toml`. It prints a
diff per file and asks `Remove lighthouse? [y/N]`; nothing is removed until you answer `y`.

- `lighthouse uninstall --yes` approves without asking. Without a terminal the question reads
  empty stdin, which counts as no: nothing is removed and `uninstall` exits 1.
- A unit you changed locally makes the whole run refuse, and nothing is removed, unless you add
  `--force`.

What `init` created is undone: files it created are removed, and the sections and keys it added to
`AGENTS.md`, `.gitattributes` and `.claude/settings.json` are taken out again. `CLAUDE.md` is only
removed when `init` created it; `init` never edits an existing one. A `lighthouse.toml` that
already existed stays, with a `left:` line: `init` may have added keys to it, and `uninstall` does
not know which. A file that holds other content than what `init` put there stays, and the closing
report on stderr says so with a `kept:` or `left:` line.

What stays, each with a line in that report:

- the labels file and the direction documents `init --labels` and `--direction` seeded: they are
  your project files after seeding;
- `.lighthouse/run` and other runtime directories under `.lighthouse/`;
- the labels on the forge, `~/.config/lighthouse/host.toml` and the lighthouse binary
  (`not touched:`).

Two things to know:

- `.claude/settings.json` is written back in the canonical layout of `init`, so a hand-made
  layout (indentation, key order) does not return byte for byte; the JSON content is the same.
- A file without a final newline, or with an empty last line, does not return byte for byte
  either.

An install from before this version has no record of what `init` created. `uninstall` removes the
units of the manifest, and you remove `lighthouse.toml` and the rest yourself. The same holds for
what an `init --upgrade` with an older binary creates: that binary does not record it, so it stays.

The report ends with a `note:` line: a running Claude Code session keeps its hooks until it
restarts. The summary on stdout reads `Changed N file(s).`, which counts the files removed and the
files edited.

## A host.toml without Tailscale

The chain works without a `host.toml`. You only need one for the `gate_*` keys (memory brake,
slots) or `[usage]`. On a plain Linux server, this is enough:

```toml
[host]
name = "server"
gate_slots = 2
gate_min_mem_available_gb = 4
```

`gate_slice` asks for a systemd slice, and the `gate_min_*` keys only measure on Linux. All keys
are in [test-gate.md](test-gate.md).

First run `lighthouse init --upgrade` in every repository on this host, on a release with this
change. Only then write a `host.toml` without `tailscale_ip`: an older copy of `config.py`
rejects it, and then the test gate blocks every commit in that repository.

On a host without Tailscale that is the controller host, set `controller = true` (a TOML boolean) in
`[host]`: `host-check` then answers without measuring an address. `controller = false` says it is
not the controller host. Without the key the old rule holds: a `tailscale_ip` that is bound on this
machine. An older reader ignores `controller` and falls back to `tailscale_ip`.

## Configure lighthouse.toml

`init` proposes `[project] language`, and `[forge]` from your `origin` remote when it is on
GitHub or GitLab. Only the `python` package proposes a test gate. Until you set `[test_gate] command`,
`commit-test-gate` passes every commit with `no test command configured, nothing was tested`. The gate also runs `lighthouse imports check` on the files of the commit, with or without a test command; see [the test gate](test-gate.md#de-importcheck).
Every key and what reads it is on [the configuration map](site/config.html) (also online:
[The configuration](https://corneeldh.github.io/lighthouse/config.html)).
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
| `project.language` | The language of your artefacts: issues, PRs, comments, commit messages. `nl` or `en`; default `en`. With `nl`, `init` also sets `"language": "dutch"` in `.claude/settings.json`, so Claude Code replies in Dutch; a `language` already there stays, with a warning. |
| `project.code_language` | Set it to `en` to keep names, comments and file names in English while artefacts follow `project.language`. |
| `project.code_language_paths` | The globs that `project.code_language` covers. |
| `forge.kind` | `github` or `gitlab`; default `github`. |
| `forge.repo` | `owner/name` on GitHub, `group/subgroup/name` on GitLab. |
| `forge.host` | The hostname of a self-hosted GitLab. `lighthouse init --forge-host <host>` writes it. |
| `forge.default_branch` | The branch nothing lands on directly; default `main`. |
| `test_gate.command` | What `commit-test-gate` runs before every commit. An empty `command = ""` turns the gate off. The `python` package sets `uv run pytest -q`. |
| `paths.specs`, `paths.plans` | Where specs and plans go; default `docs/developer/planning/specs` and `docs/developer/planning/plans`. |
| `worktree.root` | The folder the worktrees live in; default `.claude/worktrees`. Relative, it is read from the main checkout; absolute (a leading `/`, a drive such as `C:/`), it may lie outside the repository, also in a path with spaces. See [Where the worktrees live](#where-the-worktrees-live). |
| `forge.board` | Optional: a GitHub Project board the skills move issues on. |

`[forge.board]` is not in the example. When you add it, it needs `project_number`, `project_id`,
`status_field_id`, and `status_options` with an option ID for each of `To do`, `In progress`
and `Done`. `On hold` is no longer needed: parking is `To do` plus the `blocked_issue` label.

### Where the worktrees live

`[worktree] root` sets the folder the hooks, the worktree audit and the skills use for worktrees;
without it, that is `.claude/worktrees` in the main checkout.

```toml
[worktree]
root = "../lighthouse-worktrees"   # relative to the main checkout, or absolute
```

- A relative `root` is resolved against the main checkout, never the cwd, so it reads the same from
  a worktree. An absolute `root` is used as it is and may lie outside the repository.
- A `root` outside the repository needs nothing in `.gitignore`. A relative `root` inside it other
  than `.claude/worktrees` (say `.wt`) must go into your `.gitignore` or `.git/info/exclude`
  yourself; lighthouse only knows the default.
- Keep the repository and the worktree root out of a folder a sync client manages (Dropbox,
  OneDrive, iCloud Drive, Google Drive): such a client corrupts a `.git` directory and the worktrees.
  `lighthouse init` warns when the main checkout or the resolved root lies in one; it never blocks.
- The local dispatch adapter ignores `worktree.root`; see [Lokaal](workplace.md#lokaal).

Every other key, with its type and default, is in `schema` and `defaults` of
[`internal/config/config.go`](../internal/config/config.go). A key with a wrong type or value is
an error that names the file and the key. `lighthouse config <key>` prints the value a skill
reads, its default included.

## Your first issue

The chain runs from an issue to a merged PR. Each step names what you type, what you see, and
which hook can step in. Run the skills in Claude Code, in your repository.
To see it as a map, open [The loop](https://corneeldh.github.io/lighthouse/loop.html).
In a clone of the lighthouse repository, run `go run ./tools/servedir docs/site` from its
root and open [the local copy](site/loop.html) at `http://localhost:8000/loop.html`.
What happens within one turn, which skill joins in and which hook steps in, is on
[the session map](https://corneeldh.github.io/lighthouse/session.html), and locally at
[site/session.html](site/session.html).

1. **Write the issue.** Type `/lighthouse-write-issue` and describe the change. You get an issue
   on GitHub with acceptance criteria, in `project.language`.
2. **Start on it.** Type `/lighthouse-start-issue <nr>`. You see a worktree under
   the worktree root (`.claude/worktrees/` unless `[worktree] root` says otherwise) on a branch `issue-<nr>-<slug>`, and one route line:
   `#<nr> Gate <n>[a|b|c]: <route> — <reason>`. Gate `4a` builds directly. Gate `4b` writes a
   plan first and gate `4c` a spec first; both end in a handoff, a start command for a fresh
   session.
3. **Build in the worktree.** `commit-test-gate` runs your `[test_gate] command` before every
   `git commit`; a red run blocks the commit. `main-commit-gate` blocks a commit or push on
   `main`, also through `git -C <path>` or a preceding `cd`. `worktree-check` warns when you
   write into the main checkout on `main`.
4. **Open the PR.** Type `/lighthouse-write-pr`. It checks each acceptance criterion on the
   branch and writes the PR body with `Refs #N` and an Evidence table of the proven criteria.
   `pr-create-gate` blocks a `gh pr create` whose body lacks `Closes #N` with evidence,
   `Refs #N — <reason>` or `No issue: <reason>`. `[pr_gate] issue` sets how strict it is:
   `optional` (default) allows `No issue:`, `required` demands an issue, `evidence` also demands
   an Evidence table for `Refs`.
5. **After the merge.** Type `/lighthouse-close-issue <nr>`. The issue is still open: the skill
   runs the Evidence table of the merged PR on the default branch and closes the issue with that
   evidence, or leaves it open with what is missing.

The hooks run only in Claude Code. In Codex and OpenCode the skills and `AGENTS.md` are there,
but no hook is: check the branch yourself before a commit, and run your `[test_gate] command`.
