# Install lighthouse: a guide for agents

You are a coding agent. The user asked you to install lighthouse in the repository you are working
in. Follow this guide from top to bottom. You need no other page.

## What this is

Lighthouse installs a working agreement for coding agents into a git repository: skills, agents,
hooks and rules, from one binary called `lighthouse`. Its command `lighthouse init` shows a diff of
every file it would write and writes nothing until it is approved. The official copy of this guide
is <https://corneeldh.github.io/lighthouse/agents/install.md>. The binaries come from
<https://github.com/CorneeldH/lighthouse/releases>. Download nothing from anywhere else.

## Ground rules

- Explain each step to the user before you run it.
- Install, download or write nothing without an explicit yes from the user. No answer means stop.
- Never use `sudo`. Everything goes into the user's own account: the binary into `~/.local/bin`.
- Use only the subcommands and flags that `lighthouse --help` and `lighthouse init --help` show.
  This guide can be newer than the release you install. If a flag from this guide is missing
  there, stop and tell the user which one.
- Change `AGENTS.md` and `CLAUDE.md` only through the diff of `lighthouse init` that the user
  approved. Never edit them by hand.

## Asking the user

Every choice below is a question with options, and one option is the recommended answer.

- In Claude Code, ask with the `AskUserQuestion` tool. Put the recommended answer first and add
  "(Recommended)" to its label.
- Without a question tool, ask in text as a numbered list, with the recommended answer first and
  marked as recommended.
- Ask one choice at a time. Wait for the answer before you go on.

## Look first

Read, do not change, and do not ask yet. Find out:

1. The OS and architecture: `uname -sm` (on Windows, see [Install the binary](#install-the-binary)).
2. Whether `lighthouse` is on the `PATH`, and which version: `command -v lighthouse` and
   `lighthouse version`.
3. Whether this is a git repository with an `origin` on GitHub: `git remote get-url origin`.
4. Whether lighthouse is already installed here: does `lighthouse.toml` or
   `.lighthouse/manifest.toml` exist? If `.lighthouse/manifest.toml` exists, stop. Tell the user
   lighthouse is already installed; updating it is outside this guide.
5. Which requirements are missing. Check each tool with `command -v <tool>`, `tomllib` with
   `python3 -I -c 'import tomllib'`, and `superpowers` in
   `~/.claude/plugins/installed_plugins.json`.
6. Which languages the repository uses (`git ls-files`): this decides which packages `init` picks.

| Tool | Package | Without it |
|---|---|---|
| `python3`, `tomllib` (Python 3.11+) | all | The hooks report `COULD NOT CHECK`, and the blocking ones block. |
| `git` | core, expo, go, shell | Nothing works. |
| `jq` | core, go, python, shell | The hooks cannot read their payload and report `COULD NOT CHECK`. |
| `gh` | core, python | The issue and PR skills cannot reach GitHub. |
| `superpowers` | core, expo | The skills cannot hand off worktrees, specs, plans and debugging. |
| `codex` (optional) | core | The two adversarial reviews stop with `CODEX_MISSING`. |
| `uv` | python | The test gate `uv run pytest -q` and `ty-check` cannot start. |
| `go`, `gofmt`, `golangci-lint` | go | Go code is not formatted or linted. |
| `node`, `npm` | node | The node test gate reports `COULD NOT CHECK`. |
| `curl`, `sha256sum`, `tar`, `xz` | shell | No `shellcheck` download; the `shellcheck` hook blocks. |

Report what you found to the user in one block. Then go to the choices.

## Choices

Ask these in order. Skip a choice that does not apply.

| # | Choice | Options, recommended first | Notes |
|---|---|---|---|
| 1 | Download the binary to `~/.local/bin` | yes · no, I install it myself | Ask only if `lighthouse` is not on the `PATH`. On "no", wait until it is. |
| 2 | Each missing requirement | the user installs it · skip it | Name the install command for this OS. Run it only after a yes, and never with `sudo`. |
| 3 | Superpowers | yes · no | Without Superpowers, 9 lighthouse skills cannot hand off. Name the install command from the Superpowers README (<https://github.com/obra/superpowers>); the user runs it, or approves that you do. |
| 4 | Codex for the adversarial reviews | yes, if the user has a subscription · no | Without `codex`, the two reviews stop with `CODEX_MISSING`. |
| 5a | Language of issues, PRs and commits (`[project] language`) | `nl` or `en`: recommend the language the user speaks with you now, and `en` for any other language | These are the only two values; any other is an error. |
| 5b | Code in English (`[project] code_language = "en"`) | yes · no | Ask only if 5a is `nl`. |
| 6 | Packages | what `init` picks · a package added or left out | Read the `Packages: …` line of the dry run (step 2 of [Run lighthouse init](#run-lighthouse-init)). Ask once per change. Add with `--pack <name>`, leave out with `--no-pack <name>`. |
| 7 | Test gate (`[test_gate] command`) | a command you derive from the repository · another command · off (`command = ""`) | The `python` package proposes `uv run pytest -q`. Without a gate, every commit passes with `no test command configured, nothing was tested`. |
| 8 | Board (`[forge.board]`) | no, not now · yes | On "yes", point the user to the board section of <https://github.com/CorneeldH/lighthouse/blob/main/docs/getting-started.md#configure-lighthousetoml>. Do not fill in its four IDs yourself. |

In Codex or OpenCode, ask the same choices, with two differences:

- Choice 3: point to the Superpowers instructions for that harness. `init` looks for Superpowers
  only in Claude Code's plugin list, so there it always reports
  `missing dependency: superpowers`, also when it is installed. Tell the user that this message is
  expected. The skills for Codex and OpenCode are in `.agents/skills`.
- Choice 7: hooks run only in Claude Code. Tell the user that the test gate does not run by itself
  there: they run the command themselves before a commit.

## Install the binary

Only after a yes on choice 1. Run the block for the platform from `uname -sm`, in one go: it
downloads the binary, checks it against `SHA256SUMS` before it is used, and stops at the first
step that fails.

Linux x86-64 (`Linux x86_64`):

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
sha256sum --check --ignore-missing SHA256SUMS &&
chmod +x lighthouse_linux_amd64 &&
mkdir -p ~/.local/bin && mv lighthouse_linux_amd64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

Linux ARM (`Linux aarch64`):

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_arm64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
sha256sum --check --ignore-missing SHA256SUMS &&
chmod +x lighthouse_linux_arm64 &&
mkdir -p ~/.local/bin && mv lighthouse_linux_arm64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

macOS Apple silicon (`Darwin arm64`):

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_darwin_arm64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
shasum -a 256 -c --ignore-missing SHA256SUMS &&
chmod +x lighthouse_darwin_arm64 &&
mkdir -p ~/.local/bin && mv lighthouse_darwin_arm64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

macOS Intel (`Darwin x86_64`):

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_darwin_amd64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
shasum -a 256 -c --ignore-missing SHA256SUMS &&
chmod +x lighthouse_darwin_amd64 &&
mkdir -p ~/.local/bin && mv lighthouse_darwin_amd64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

If `~/.local/bin` is not on the `PATH`, tell the user to add `export PATH="$HOME/.local/bin:$PATH"`
to their shell profile, and call `~/.local/bin/lighthouse` by its full path until then.

Native Windows: the hooks need bash, so lighthouse does not work there as a whole. Tell the user,
and recommend WSL: install inside WSL with the Linux x86-64 block, and work on the repository from
WSL.

Then run `lighthouse --help` and `lighthouse init --help`, and hold this guide to them (see
[Ground rules](#ground-rules)).

## Run lighthouse init

In the root of the repository:

1. Build the command: `lighthouse init`, plus a `--pack <name>` or `--no-pack <name>` for every
   change from choice 6.
2. Dry run: run the command once with an empty stdin, without approval:

   ```bash
   lighthouse init </dev/null
   ```

   `init` prints `Packages: …`, any `missing dependency: …` lines and a diff per file, then asks
   `Apply these changes? [y/N]`. The empty stdin counts as no: it prints
   `lighthouse init: nothing written`, writes nothing and exits 1. That is the expected result
   here. If choice 6 changes the packages, run the dry run again with the new flags.
3. Show the diff to the user, or summarise it per file and offer the full diff. Ask for approval.
4. After a yes, run the same command with one `y` on stdin:

   ```bash
   printf 'y\n' | lighthouse init
   ```

   Add the flags from step 1 after `init`. Do not approve with `--yes`. On a machine with
   Tailscale, `init` asks a second question after the diff: `Write ~/.config/lighthouse/host.toml?
   [y/N]`. The dry run never reaches it, so the user has not seen it, and `--yes` would write
   `host.toml` without asking. With one `y`, the second question reads the end of stdin, and that
   counts as no. `host.toml` is for `lighthouse serve`, which this guide does not set up.
5. Write the answers of choices 5a, 5b and 7 into `lighthouse.toml`, under `[project]` and
   `[test_gate]`. Show the user the change. Choice 8 writes nothing here.
6. Go to [Check](#check).

## Check

Run:

```bash
lighthouse init --upgrade </dev/null
```

The output must be exactly `No changes.`. If it shows a diff instead, show that diff to the user, ask again, and after a yes run
`printf 'y\n' | lighthouse init --upgrade`. Repeat until the output is `No changes.`.

## Final report

End with this block, filled in:

```text
lighthouse installed

Version:        <output of `lighthouse version`>
Packages:       <the Packages line of init>
Questions:      <each choice, with the answer the user gave>
Files written:  <the files from the approved diff, plus lighthouse.toml>
Check:          <output of the last `lighthouse init --upgrade`>
Still open:     <requirements skipped, Superpowers or Codex not installed, the board, warnings>
Next step:      open a first issue and run /lighthouse-start-issue <number>
```

For the first issue, point the user to "Your first issue" in
<https://github.com/CorneeldH/lighthouse/blob/main/docs/getting-started.md#your-first-issue>.

## When something fails

Each message below is what `lighthouse init` prints. Quote it to the user, then do what it says
here.

- `missing dependency: <tool> (needed by: …)`: `init` goes on. Add the tool to "Still open" in
  the final report, unless the user already chose to install it. In Codex and OpenCode,
  `missing dependency: superpowers` is expected (see [Choices](#choices)).
- `` lighthouse init: this repository is already initialized; use `lighthouse init --upgrade` ``:
  stop. Lighthouse is already installed; updating is outside this guide.
- `lighthouse init: refused: <n> locally modified unit(s); rerun with --force to overwrite`:
  stop and report it. Do not add `--force`.
- `warning: no GitHub remote 'origin': [forge] not proposed in lighthouse.toml; skills that need the forge will fail until you add it`:
  tell the user. Add `[forge]` only with the user's `owner/name`, after a yes.
- `` warning: CLAUDE.md does not import AGENTS.md; add a line `@AGENTS.md` so Claude Code reads the lighthouse rules ``:
  do not edit `CLAUDE.md` yourself. Put the line under "Still open" for the user to add.
- `lighthouse init: package <name> is not in this binary`: the release lacks that package. Drop
  its `--pack <name>` and tell the user.
- `lighthouse init: nothing written` after step 4: the `y` did not reach `init`. Check the pipe,
  and do not switch to `--yes`.
- `lighthouse: unknown flag: --<name>`: the release lacks a flag from this guide. Stop and report it (see
  [Ground rules](#ground-rules)).
- `warning: host.toml not proposed: …`: harmless for this guide. Mention it in the final report.
