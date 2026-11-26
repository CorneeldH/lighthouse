# Installing the server

`lighthouse serve` runs the API and web UI of the development chain. One server per machine
serves all your repositories from one SQLite database.

## Requirements

- Linux with systemd (user units) for `lighthouse serve`; macOS is not supported there. The CLI
  runs on macOS, but the hooks are untested there.
  On Windows, `lighthouse serve` does not run natively; the CLI and the hooks do (see
  [getting-started.md](getting-started.md#windows)).
- Go, only to install with `go install` or to build from a clone.
- Node 22 with npm, only to build from a clone (the web UI). `web/.nvmrc` holds the exact version.
  A release binary needs neither Node nor a clone.
- Optional: Tailscale, to reach the server from other devices on your tailnet.

Every assumption about the host, with what another host does: [host-assumptions.md](host-assumptions.md).

## Download a release binary

Each release carries a binary per platform, plus `SHA256SUMS`: `lighthouse_linux_amd64`,
`lighthouse_linux_arm64`, `lighthouse_darwin_amd64`, `lighthouse_darwin_arm64` and
`lighthouse_windows_amd64.exe`. This example takes Linux on amd64:

```bash
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
chmod +x lighthouse_linux_amd64
mkdir -p ~/go/bin && mv lighthouse_linux_amd64 ~/go/bin/lighthouse
```

On macOS, download `lighthouse_darwin_arm64` (Apple silicon) or `lighthouse_darwin_amd64`, verify
with `shasum -a 256 -c --ignore-missing SHA256SUMS`, then `chmod +x` the file. On Windows,
download `lighthouse_windows_amd64.exe`, run `Get-FileHash -Algorithm SHA256
lighthouse_windows_amd64.exe` in PowerShell and compare the hash with its line in `SHA256SUMS`.

`~/go/bin/lighthouse` is where the systemd unit below looks for the binary; any directory on
your `PATH` works for the CLI. The release binary reports its tag (`lighthouse version`) and
carries the built web UI: `lighthouse serve` serves it at once, with no Node and no clone. To
upgrade, download the new binary the same way and follow the steps in
[Releasing](releasing.md#upgrading). Restart the service afterwards.

## Install with Go

`go install` builds a tagged version from source. Every release tag from the next release on
carries the built web UI, so `@<tag>` and `@latest` serve it. Older tags still serve the
placeholder page:

```bash
go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest
```

`go install` puts the binary in `~/go/bin`. `@main` is not a tag and carries no UI: it serves
a placeholder page. Install a tag, or use the release binary above, for the UI without a clone.

## Build from a clone

This route is for developers. To build the web UI yourself, clone the repository and build it.
The build writes the UI into `internal/web/dist`, and `go install` embeds it.

```bash
git clone https://github.com/CorneeldH/lighthouse.git
cd lighthouse
cd web && npm ci && npm run build && cd ..
go install ./cmd/lighthouse
```

A build from a clone reports version `dev`, and `lighthouse init` writes that version into
`.lighthouse/manifest.toml`. For a repository you run `init` in, use a release binary or a
tag instead.

All routes write `~/go/bin/lighthouse` (or your chosen path), so the last one wins: a release
binary or a `go install` of a tag (from the next release on) replaces a build from a clone and
still serves the UI. Only a `go install …@main`, an older tag, or a build from a clone without
`npm run build`, replaces it with the placeholder page, and the systemd unit then serves that.

## Start small

A repository does not have to take the whole chain at once. A first `lighthouse init` in a new
repository chooses the level `light`; the levels are `light`, `standard` and `strict`.

```bash
lighthouse init          # new repository: level light
lighthouse level         # the level, the stand per component, what the components on log counted
lighthouse init --upgrade --level standard
```

`lighthouse level` only reads. Scale up with `lighthouse init --upgrade --level standard`, later
`--level strict`. `--level` records the choice in `lighthouse.toml` and in
`.lighthouse/components.toml`. An existing repository without `level` in `lighthouse.toml`
behaves as `strict`. See [Setup levels](levels.md#what-each-level-skips-and-why) for what each
level skips and why.

- **Which level fits.** `lighthouse history` reads the Claude Code transcripts of the repository
  on this machine and prints one JSON report with a recommended level and a few proposals: where
  pushes go, whether work runs through issues and PRs, where worktrees live. It reads only,
  writes nothing, uses no network and prints no prompt, tool output or file content. Run it in
  the root of the repository before `init`; `--sources` adds the files and line numbers behind
  each finding. An agent that installs lighthouse for you does this as the History step of
  [the agent guide](site/agents/install.md).

Existing auto-memory? After `init`, run `/lighthouse-migrate-memory`: it sorts the Claude Code
memory files of this repo into instructions, issues, docs and `host.toml`, one group at a time,
and leaves the deleting to you.
- **When to scale up.** Move to `standard` once your first PRs hurt without tracking issues or
  reviews, or once you want `commit-test-gate` to run after each commit; first check that
  `lighthouse level` shows few reports for `md-link-check` and `code-language-check`, which
  `standard` turns on. Move to `strict` once you want the second-model-family reviews of spec
  and plan, which need Codex.
- **Overrides.** `[components]` in `lighthouse.toml` sets one component to `off`, `log` or `on`,
  whatever the level says. `log` exists only for the Go hooks of core: the hook runs, never blocks
  and never speaks, and writes what it would have reported to a logbook outside the repository,
  which `lighthouse level` counts. The base components (`run.sh`, `blocking`, `usage-gate`,
  `permission-check`, `workflow-push-gate`, `level-check`, `no-verify-check`) cannot be overridden.
  A skill that is on cannot hand over to a skill that is off; `init` refuses that.
- **Core gates.** `main-commit-gate`, `worktree-check` and `pr-create-gate` keep the repository
  in the chain. When one of them is not `on`, `init` warns, the session start (`level-check`)
  warns, and so does the managed block in `AGENTS.md`.

## Run it as a user service

Fetch the unit, [lighthouse.service](../deploy/systemd/lighthouse.service), into your user
unit directory. Use the tag of the binary you downloaded (`lighthouse version`) for `<tag>`. If
the binary is not at `~/go/bin/lighthouse`, edit the `ExecStart` path.

```bash
mkdir -p ~/.config/systemd/user
curl -fsSLo ~/.config/systemd/user/lighthouse.service https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/systemd/lighthouse.service
systemctl --user daemon-reload
systemctl --user enable --now lighthouse.service
loginctl enable-linger "$USER"
```

With a clone, copy the unit instead of the `curl` line:
`cp deploy/systemd/lighthouse.service ~/.config/systemd/user/`.

`enable-linger` keeps the service running when you are not logged in.

The server listens on `127.0.0.1:8127` and stores its database in
`${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/lighthouse.db`.

## Upgrade automatically

A timer can keep the binary current. [lighthouse-upgrade.service](../deploy/systemd/lighthouse-upgrade.service)
runs `lighthouse self-update --restart lighthouse.service`, and
[lighthouse-upgrade.timer](../deploy/systemd/lighthouse-upgrade.timer) starts it daily. The
binary is replaced only when the newest release on the channel is newer and its checksum matches, and the
server restarts only after a replacement. A local build without a tag (`dev`) is skipped.

The timer follows the channel stored in `~/.config/lighthouse/channel`; without that file it is
`release`. Choose once, by hand: `lighthouse self-update --channel dev` stores `dev` and
updates at once, `--channel release` goes back, and `lighthouse channel` prints the current
one.

```bash
cp deploy/systemd/lighthouse-upgrade.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now lighthouse-upgrade.timer
```

Without a clone, fetch both files with `curl` as for `lighthouse.service` above. The unit
reuses the forge token in `~/.config/lighthouse/forge.env` (`GH_TOKEN`) when present, and sends
it to `api.github.com` for the release lookup only; keep that file mode 0600.

The installed binary must already be a release that has `self-update`: upgrade once by hand
first. Under the unit's hardening `~/go/bin/lighthouse` must not be a symlink to a path outside
`~/go/bin`, because that directory is the only one the unit may write.

Every release on the channel goes live without a review. On `release` that is the newest
release; on `dev` it is the newest `dev` or `rc` prerelease of the next version too, never a
`-test` tag. To stop:
`systemctl --user disable --now lighthouse-upgrade.timer`.

Which version runs, and what the last run did:

```bash
lighthouse version
curl -s 127.0.0.1:8127/api/health
journalctl --user -u lighthouse-upgrade.service -n 5
```

## Check it

```bash
curl -s http://127.0.0.1:8127/api/health
curl -s http://127.0.0.1:8127/ | grep lighthouse-ui
journalctl --user -u lighthouse
```

The health endpoint answers `{"version":"…"}`. The page answers with
`<meta name="lighthouse-ui" content="spa" />`: the binary carries the built web UI.
`content="placeholder"` means the binary was built without the UI (`go install …@main`, or a
clone without `npm run build`). The log shows
`lighthouse: listening on 127.0.0.1:8127`.

The API is read-only. Its routes:

| Route | What it reads |
|---|---|
| `GET /api/health` | the version of the binary |
| `GET /api/usage` | the Claude and Codex usage windows |
| `GET /api/runs` | the open and parked runs and the deferred starts, from the tick state |
| `GET /api/questions` | the open questions, from the overview `serve` keeps in memory (see below) |
| `GET /api/returned` | the returned issues, from the same overview |
| `GET /api/cleanup` | the workspaces to clean up, from the database |
| `GET /api/latest-comment?repo=owner/name&issue=N` | the comments of one issue, from the forge; redirects (302) to the newest comment, or to the issue page when it has none or the read fails. A repo `serve` does not serve gets 404 |
| `GET /api/telemetry/components` | the comparison block per component, filtered by `repo`, `route`, `size`, `pr_size`, `area`, `model`, `harness`; from the database (see [`telemetry-ui.md`](telemetry-ui.md)) |
| `GET /api/telemetry/timeline?repo=owner/name` | cost, questions, closed and recovered trajectories per week, from the database |
| `GET /api/telemetry/trajectories` | the trajectories, with the filters of `components` plus `component`, from the database |
| `GET /api/telemetry/trajectories/{id}` | the detail of one trajectory, from the database; 404 when it does not exist |
| `GET /api/telemetry/coverage?repo=owner/name` | every gap in the measurement with its count or reason, from the database |

## Check the hooks

```bash
lighthouse check-hooks
```

Run it after `lighthouse init` (which runs it for you and prints the result on stderr), on a new
machine, and whenever a hook seems to do nothing. It reads `.claude/settings.json` and starts each
registered hook once in a smoketest. Every line is `ok` or `FAIL` with its remedy; the exit code is
0 when every hook can run, 1 on a `FAIL`, 2 when it could not check. The common `FAIL` lines:

| `FAIL` | Meaning | Fix |
|---|---|---|
| `no sh on the PATH` | Claude Code runs every hook with `sh -c`, so none runs | Put a POSIX `sh` on the `PATH` |
| `Git Bash not found` | Windows only: without Git Bash, Claude Code runs the hooks under PowerShell and they block nothing | Install Git for Windows, or set `CLAUDE_CODE_GIT_BASH_PATH` to its `bash.exe`, and start a new session |
| `has CRLF line endings` | The shell does not run the hook, or lets everything through | `lighthouse init --upgrade`: it writes the hook with LF and sets `.gitattributes` |
| `hook not registered` | A hook of the pack is missing from `.claude/settings.json` | `lighthouse init --upgrade` |
| `disableAllHooks is true` | No hook runs | Remove that line from the settings file it names |
| a hook line with a verdict | The hook failed its smoketest | Fix what the line says; `lighthouse init --upgrade` reinstalls the hooks |

Git Bash matters only on Windows. The Windows route is not proven yet: the WSL advice in
[the agent guide](site/agents/install.md) stays until `ci/windows-measure.sh` row 12 is green on
native Windows.

## Connect the forge

`GET /api/questions` and `GET /api/returned` read the open questions and returned issues of
the served GitHub repositories. Every item carries its `repo` (`owner/name`), and the UI shows
it as `owner/name#number`. Every `owner/name#number` in the UI links to
`/api/latest-comment`, so a click lands on the newest comment of the issue. Which repositories `serve` reads:

- With `[dispatch] repos` in `host.toml`: every repository listed there, in that order. A repo
  whose `lighthouse.toml` does not load is skipped with a line on stderr. `--repo` is then
  ignored, with `lighthouse: --repo <owner/name> ignored: serving the repos of [dispatch]` when
  it is not among them.
- Without `[dispatch] repos`: the one repository of `--repo owner/name` on `lighthouse serve`, or
  `LIGHTHOUSE_FORGE_REPO=owner/name`.

`/api/questions` and `/api/returned` do not call GitHub per request: they show one shared overview
that `serve` builds at start and then every 90 s (start to start, never two builds at once,
a time-out of 60 s per build, three times the 16–20 s of a live build). A build that fails is retried after 20 s, and until one succeeds
both endpoints answer 502 (503 without repositories or without a token) instead of old data. The first request after
a start waits for the first build. So `serve` reads GitHub also when nobody looks: 40 builds an
hour per set of repositories. The page re-reads that overview every 15 s; that reads memory, not GitHub. Its Refresh button
re-reads it too; it does not start a build.

A new question or return is on the page within one tick interval (3 minutes) of its comment. The
worst case is 90 s until the next build starts, 60 s for that build and 15 s until the page polls:
165 s. `TestNewCommentOnPageWithinTickInterval` in `internal/server` runs that case and fails
when the sum passes the `OnUnitActiveSec` of `lighthouse-tick.timer`.

A build costs, per repository, 1 REST call for the open issues (more when they span several pages
of 100) and 1 REST call per Controller issue that has comments (more for over 100 comments). The
board items (only with the Controller filter) are 1 GraphQL call, more for a large board, which
counts against GitHub's separate GraphQL limit of 5000 points an hour, not against the 5000 REST
requests an hour. One page of 100 board items costs 14 points (measured 2026-11-14 with
`rateLimit(dryRun: true)`), so two repositories on boards under 100 items cost 28 points a build
and 1120 an hour. At 40
builds an hour (the steady state; during an outage the 20 s retry can raise that to 180 an hour)
the REST calls must fit, together with the tick, in the 5000 requests an hour of one token. A repository without the Controller filter reads the comments of every open issue that
has comments, so it weighs most and reaches the budget first. That is a deliberately accepted
risk, with no code to guard it; when the budget runs out both endpoints answer 502 through the
existing `forgeError`.

A GitLab repository costs more REST calls per build and per tick. Reading the comments of an
issue is one call for its notes (more for over 100) plus one `award_emoji` call per note that is
not a system note, because the notes list carries no award emoji. `BlockedBy` costs one call for
the links plus one per closed blocker, to read whether a merge request that closes it is merged.
`BranchPRs` costs one call for the list plus one per open merge request on the branch, because
only a single merge request carries its pipeline. The board is two GraphQL queries of 100 work
items per page: all open issues, and the closed issues updated in the last 14 days. GitLab.com
allows about 2000 authenticated requests a minute per user; a self-hosted instance sets its own
limit.

Give it a token: `GH_TOKEN` (else `GITHUB_TOKEN`) in the environment, or `GITLAB_TOKEN` for a
GitLab repository. `[forge] token_env` in a repository's `lighthouse.toml` names another variable
instead. A repository without a token is skipped for questions and returned issues, with a line on
stderr, but `serve` still shows its runs. `--forge-url` (`LIGHTHOUSE_FORGE_URL`) sets the API base
URL for GitHub repositories only; the GitHub token is never sent to a GitLab host. Create a fine-grained
personal access token for those repositories with only the *Issues: read* permission. The
server only reads. With the Controller filter described below it also needs the `project` scope.

### A GitLab repository

Set `kind = "gitlab"` under `[forge]` in the repository's `lighthouse.toml`, with `repo` as
`group/name` or `group/subgroup/name`. A self-hosted GitLab adds `host = "gitlab.example.com"`
(host name only; without `host` it is `gitlab.com`). The client speaks the REST API v4 and
GraphQL. It is written for GitLab 19.4 and tested against a fake server only; the first run
against a real GitLab is part of the definition of done of the GitLab milestone (#260). The token (`GITLAB_TOKEN`, or the `token_env` variable) needs the
`api` scope to write, `read_api` to only read.

GitLab keeps the status of an issue in its built-in issue status (a work item widget), not in a
label. Add the status *Controller* to the top group of the lifecycle, under
*Settings → Work items → Statuses*. A status the lifecycle lacks stops the write with an error
that names the status and this place. Names are matched case-insensitively.

The `[forge.board]` of a GitLab repository has two tables only. `status_options` needs *To do*,
*In progress* and *Done*, and *Controller* as well once the file has a `[controller]`
table. Each lighthouse name needs a GitLab status of its own, and each priority a label of its
own: two names on one status or label are refused.

```toml
[forge]
kind = "gitlab"
repo = "group/sub/project"
host = "gitlab.example.com"

[forge.board.status_options]   # lighthouse name = GitLab status name
"To do" = "To do"
"In progress" = "In progress"
"Done" = "Done"
"Controller" = "Controller"

[forge.board.priority_options]  # lighthouse name = scoped label
"P1" = "priority::P1"
"P2" = "priority::P2"
```

`project_number`, `project_id`, `status_field_id`, `priority_field_id` and `project_owner` are
GitHub Projects keys and are refused for GitLab. `project_owner` is optional: set it when the
user or organisation that owns the project board differs from the repository owner. Limits of this first GitLab version:

- No script of `pack/core` is Python any more, so the old Python config reader, which
  required the GitHub board keys for every `[forge.board]`, is gone, and a host needs no `python3` for the core.
- The client refuses a redirect to another scheme or host, also a same-host `http` to `https`
  one, so the token is never sent anywhere else. Give `host` the address that answers directly.
- roborev notes on a GitLab merge request are only read once #260 lands.
- The status write (`workItemUpdate` with `statusWidget: {name}`) is not yet checked against a
  live GitLab. A real server may want the status ID, read from the lifecycle, instead of the
  name. How GitLab's GraphQL complexity limit and time-out treat the nested board query is not
  known either.
- `BranchPRs` reads each open merge request on the branch once more for its pipeline status:
  one extra call per open merge request.
- `--forge-url` does not apply to GitLab; use `host`.

Under systemd, put both in `~/.config/lighthouse/forge.env`, which the unit loads through
`EnvironmentFile=`. The token is a secret, so keep the file private:

```bash
mkdir -p ~/.config/lighthouse
install -m 0600 /dev/null ~/.config/lighthouse/forge.env
cat > ~/.config/lighthouse/forge.env <<'EOF'
LIGHTHOUSE_FORGE_REPO=owner/name
GH_TOKEN=<your fine-grained token>
EOF
systemctl --user restart lighthouse.service
```

A unit you copied before this change lacks the `EnvironmentFile=` line. Copy the unit again,
run `systemctl --user daemon-reload`, then restart the service.

Check it:

```bash
curl -s http://127.0.0.1:8127/api/questions
```

For a repository in `[dispatch] repos` whose `lighthouse.toml` has a `[forge.board]` with a
`Controller` option, the overview shows only the issues in Controller. Issues the dispatcher
parked (To do with the `blocked_issue` label, see [tick.md](tick.md)) then drop out. Otherwise it shows every open issue of
that repository. With the filter `serve` reads the project board, so the token needs the
`project` scope as well. A `host.toml` that does not load (an invalid value or an unknown key under
`[dispatch]`) stops `serve`; a missing one counts as no `[dispatch]`.

A `503` means no repository or no token is set. A `502` means GitHub could not be reached or
answered with an error for one of the repositories; the response body names it.

## Running runs

`GET /api/runs` and the page blocks *Running runs*, *Parked runs*, *Queue* and *Night* show the open
and parked runs and the deferred starts of the served repositories. A row of summary tiles above the
blocks counts each block and links to it; a tile shows `–` while its data is loading or has never
loaded (a failed refresh keeps the last number, like the panels). A deferred start with reason
`night` goes to *Night*, every other one to *Queue*. `serve` reads them from the tick state in its
database, the one `lighthouse tick` writes (see [tick.md](tick.md)): no forge call and no token, so
the blocks refresh every 30 seconds. Per run: `repo`, `issue`, `title`, `state` (`open` or
`parked`), `command`, `phase` (the skill the command starts, empty for an empty command), `task`,
`tasks` and `step` (the current plan task, the number of tasks and `build`, `review` or
`final-review` for a subagent-driven run; `0`, `0` and empty without progress), `trigger`,
`profile`, `workspace_id`, `started_at`, `stand` (`working`, `waiting` or `done`; empty when the
tick could not measure the session, shown as *not measured*) and `at`, the tick that wrote it. A
scheduled run (see *Scheduled runs*) also has `schedule`, its schedule name, with `issue` 0 and an
empty `title`, and `pr_at` once its pull request is open; the page shows it as
`<repo> · scheduled <name>`, without an issue link. Per deferred start: `repo`, `issue`, `title`,
`reason` and `at`, and `schedule` for a scheduled run. The rows do not show `at`; the Controller
section heading shows the age of the newest tick as *tick … ago*, so a row is at most one tick behind.
Without a tick both lists are empty. Next to `runs` and `deferred` the body carries two settings
of host.toml: `workspace_url`, the kenn-forge address a browser opens, without a trailing slash.
It is `[dispatch.forge] web_url`, else `[dispatch.forge] url` (`http://127.0.0.1:8091` without a
host.toml). Set `web_url` to the tailscale serve address when you open the page from another
machine; lighthouse itself keeps talking to the forge API on `url`. The page links the workspace id in
a run row and in *Workspaces to clean up* to `<workspace_url>/terminal/<id>`, in a new tab. The other setting is `night`,
the night window from `[dispatch] night` as `HH:MM-HH:MM`. A `500` means the database could not be read.

## Telemetry data model

The same `lighthouse.db` holds the telemetry of the work, migration `0010`, recovery after merge `0011`. A *trajectory* is one
issue of one repository (`trajectories`, unique on `repo` and `issue`, `repo` the configured slug
exactly as in `runs.repo`). Four more tables and two columns carry the rest: `sessions` (interactive
sessions only, never dispatcher runs), `setups` (the deployment of one run or session: model, effort,
harness and tool versions, skills hash), `usage` (tokens and cost per model, with `cost_kind`
`estimate`, `quota` or `actual`; an unknown cost is `NULL`) and `findings` (review findings with their
`decision`: `adopted`, `rejected` or `left_open`; the record files stay the source), plus
`runs.trajectory_id` and `runs.assignment` (`standard`, `shadow:<x>` or `variant:<k>=<v>`). `Take`
links every issue run to its trajectory when it claims the run.

Every telemetry row has the provenance columns `source`, `source_id` and `recorded_at`, unique on
`(source, source_id)`. The writers in `internal/telemetry` are idempotent: offering the same record
again never adds a row. A session and a finding update their derived fields (a session that grows can be
offered again, a finding gets its decision later); a setup and a usage row keep the first row.

An interactive session is linked to a trajectory in four steps, from the lines of its transcript
with their branch:

1. Lines on the default branch or with an empty branch do not count.
2. Of the other lines, the issue number comes from the branch name (`issue-<N>` or `issue-<N>-<slug>`);
   the issue with the most lines wins, ties go to the last one seen (basis `branch`).
3. Without an issue branch, the issue number the transcript names most often wins, here every line
   counts, also on the default branch (basis `transcript`).
4. Otherwise the session has no trajectory and one reason, each when the transcript also gave no issue
   number: `main_only` (all lines on the default branch), `no_issue_branch` (other branches, none an
   issue branch) or `no_issue_number` (no usable branch at all).

Scheduled runs (see *Scheduled runs*) have no issue and so no trajectory: `runs.trajectory_id` stays
empty for them.

### Herstel na merge

`lighthouse measure-recovery --repo-dir <pad> [--days N] [--db <pad>]` meet per gemergde PR of het
werk na de merge hersteld moest worden, en schrijft dat in `recoveries` (een rij per PR, uniek op
traject en PR: `merged_at`, `window_end`, `status`, `fixpr_rounds`) en `recovery_items` (een rij per
herstel: `kind` `fix`, `revert` of `bug_issue`, met `ref` en `at`). De opdracht draait niet vanzelf;
start haar met de hand, vanuit een eigen cron, of als geplande job `measure-recovery` (zie
[tick.md](tick.md#job-measure-recovery)). `--days` is standaard 14, `--db` standaard de
`lighthouse.db` van de statusmap. Repo en default branch komen uit de `lighthouse.toml` in
`--repo-dir`; de opdracht haalt eerst `origin/<default branch>` op (mislukt dat, dan is het exit 1 en
wordt niets geschreven) en meet op die ref.

- **Herstel** is een van drie dingen binnen het venster `(merged_at, merged_at + 7 dagen]`: een
  commit op de default branch waarvan het onderwerp met `fix:` of `fix(` begint en die minstens een
  bestand raakt dat ook de merge-commit raakte (`fix`), zo'n commit met `Revert "`, `revert:` of
  `revert(` (`revert`), of een issue met het label `bug` dat `#<pr>` in titel of body noemt en
  binnen het venster is aangemaakt (`bug_issue`). De merge-commit is de commit met `(#<pr>)` aan
  het eind van het onderwerp (squash-merge); een PR zonder zo'n commit wordt overgeslagen
  (`no_merge_commit`).
- **Status.** Zolang het venster loopt is de status `open`, ook als er al herstel is. Daarna is hij
  `recovered` (minstens een item) of `none`. Een rij die `recovered` of `none` is blijft zo; een
  `open`-rij wordt bij elke run vervangen, ook buiten `--days`, zodat hij sluit zodra zijn venster
  voorbij is.
- **Traject.** Het issuenummer komt uit de branchnaam `issue-<nr>-...` van de PR (een PR met een
  bestaande `open`-rij houdt het issue van die rij); een PR zonder
  zo'n branch wordt overgeslagen met reden `no_issue_branch`. Elke overgeslagen PR staat met reden in
  de uitvoer; de exitcode blijft 0. De uitvoer is `#<pr> <status> <n> items, <m> fix-pr rounds` per
  gemeten PR en `#<pr> skipped: <reason>` per overgeslagen PR.
- **`fixpr_rounds`** telt de `fix-pr`-runs van de dispatcher die voor de merge begonnen (`runs`,
  trigger `fixpr`, zonder `start_failed`). Alleen die tellen: een ronde in een interactieve sessie
  laat geen PR-record achter en blijft buiten de telling.
- **Forge.** Per run is er een zoekopdracht (`label:bug created:>=<oudste merge>`) vanwege de
  limiet van de zoek-API; een forgefout, ook een limiet, geeft exit 1 voordat er iets wordt
  geschreven. Alleen GitHub: GitLab kan gemergde PR's nog niet opsommen, dus daar is de opdracht een
  fout (exit 1).
- **Kenmerken.** Na het herstel legt een tweede ronde per traject de kenmerken vast die het
  tabblad Telemetry gebruikt ([`telemetry-ui.md`](telemetry-ui.md)): de route uit het laatste
  triagecomment vóór de eerste review, het gebied uit het enige label met `[labels] area_prefix`
  en de PR-grootte in coderegels. Die ronde raakt elk traject met een `recoveries`-rij dat nog
  een route, gebied of PR-grootte mist, of een PR binnen `--days` heeft; zonder `area_prefix`
  telt een leeg gebied niet als gat. Wat niet lukt (een forgefout, geen triagemarker, geen of
  meer dan één gebiedslabel) laat de oude waarde staan en geeft één regel
  `traits #<issue>: <reden>`; de exitcode blijft 0. Een traject zonder triagemarker blijft een
  doel: elke run leest zijn issue en comments opnieuw (twee forge-lezingen) en print de regel
  opnieuw.

### Inzet, kosten en tokens

`lighthouse telemetry collect [--repo-dir <pad>] [--db <pad>] [--since-last] [--dry-run]` leest de
Claude Code-transcripten van een repo en schrijft per transcript, in een transactie, de inzet
(`setups`: model, effort, harness- en toolversies, skillshash, `lane` `sdd` of `native`) en de
tokens met kosten (`usage`) in `lighthouse.db` (migratie `0013`: `setups.model` en de andere
velden mogen `NULL` zijn, plus `setups.lane`, `trajectories.plan_tasks` en
`trajectories.plan_path`). `--repo-dir` is standaard `.`, `--db` standaard de `lighthouse.db` van
de statusmap. Repo, default branch en `paths.plans` komen uit de `lighthouse.toml` in `--repo-dir`.

- **Vinden.** Onder `~/.claude/projects` hoort een projectmap bij de repo als haar eerste `cwd`
  in `--repo-dir` ligt, in `[worktree] root`, in een git-worktree van de repo of in een pad dat
  `<owner>/<repo>` bevat (in elke hoofdletterstand; dat vindt ook een al verwijderde worktree).
  `[worktree] root` telt alleen als zoekmap wanneer hij binnen de hoofdcheckout ligt (de
  standaard); een absolute root daarbuiten kan met andere repo's gedeeld zijn, en dan vinden de
  git-worktrees en het `<owner>/<repo>`-pad de worktrees van deze repo.
  De subagent-transcripten van een sessie horen bij haar hoofdtranscript.
- **Uitvoer en exitcodes.** Een regel per transcript: `<soort> <pad>`, of `skip <pad>: <reden>`
  voor een transcript dat blijft liggen (geen regels, geen leesrechten, de sessie houdt haar
  traject). Exit 0 is verzameld, ook als er transcripten zijn overgeslagen; 1 een fout in
  verwerking, config, git of database, ook een pas die deels mislukte (`fail <pad>: <reden>`);
  2 een verkeerd gebruik.
- **`--since-last`** laat een transcript staan dat sinds de vorige pas niet veranderde. Zonder de
  vlag wordt alles opnieuw verwerkt: zo herstel je na een nieuwe prijs of een nieuwe versie. Dat is
  veilig, want de schrijvers zijn idempotent (zie *Telemetry data model*).
- **`--dry-run`** opent de database alleen-lezen zonder te migreren, schrijft niets (ook geen
  zijbestand) en meldt wat er zou gebeuren (`would collect <pad> as <soort>`). Bestaat de
  database niet, of staat hij op een oudere schemaversie, dan meldt hij dat en toont alleen de
  gevonden transcripten.
- **Wat wordt gemeten.** Tokens per model en de inzet per run of sessie. De kosten zijn altijd een
  raming (`cost_kind` `estimate`), berekend uit `internal/telemetry/prices.toml`: per model de
  prijs per miljoen tokens met een `as_of` (geldig vanaf; de rij met de grootste `as_of` op of
  voor het berichtmoment telt). Alleen de letterlijke model-id uit het transcript past; een
  model zonder prijs, zoals de aliassen `sonnet`, `opus` en `haiku`, geeft `NULL` kosten, nooit
  een gok. Zijvragen komen niet altijd in het transcript terecht en worden dus
  ondergeteld. Er is geen quotum: `quota` blijft leeg.
- **Inzet van een run uit de reviewrecords** (migratie `0018`). Een run krijgt in
  `setups.controller_settings` de `[controller]` van de repo bij het verzamelen (JSON met
  `profile`, `spec_review`, `plan_review`, `at_round_limit`) en in `setups.prompt_hashes` de
  hashes van de plan- en specreviews die tijdens de run liepen (JSON, sleutel `review-plan` of
  `review-spec` voor `prompt_sha256`, met `:verifier_prompt`, `:schema` of
  `:verification_schema` voor de andere hashes). Een record hoort bij de run van zijn traject
  waarvan `[started_at, ended_at]` zijn `written_at` bevat; het traject komt uit `trajectory` of
  wordt herleid zoals `review-index` dat doet. Een record met `review_mode: fallback` geeft een
  rij in `setup_switches` (`reviewer`, `codex` naar `opus`, op `written_at`), uniek op
  `(setup_id, at, field)`, behalve onder `assignment: variant:<kind>_review=opus`: daar is de
  Opus-only ronde de indeling en geen uitval van Codex, dus geen wissel. Een record zonder run schrijft niets; een pas zonder records of zonder
  `[controller]` laat de bestaande waarden staan.
- **Vragen en antwoorden.** Van een afgesloten sessie (`ended_at` gezet) die aan een traject hangt,
  schrijft de collector per `AskUserQuestion` een `question`-gebeurtenis en, zodra het antwoord er
  is, een `answer`-gebeurtenis in `trajectory_events`, met alleen soort, `ref`
  (`<sessie-id>:<tool-use-id>`) en tijdstip, nooit de tekst. Een open sessie en een run schrijven
  er geen. Heeft een sessie al gebeurtenissen, dan blijft zij bij haar traject, ook als een latere
  pas een andere branch ziet (`skip <pad>: <reden>`).
- **De hook `telemetry-collect-check`** (SessionStart, een basishook zonder stand en zonder
  override) schrijft een waarneming (tijdstip, lighthouse- en packversie) in een zijbestand,
  `<state>/<sessie-id>.observations.json`, en start daarna `lighthouse telemetry collect
  --since-last` losgekoppeld. `<state>` is `LIGHTHOUSE_STATE_DIR`, anders dezelfde terugval als
  `tick` (`XDG_STATE_HOME` of `~/.local/state`, plus `lighthouse/dispatch`). De collector kiest per
  run de eerste waarneming binnen zijn venster. De hook blokkeert nooit en opent geen database;
  een fout is een regel in `<state>/telemetry/hook.log`. In een controller-run
  (`LIGHTHOUSE_CONTROLLER_RUN`) en met `LIGHTHOUSE_TELEMETRY_COLLECT=off` schrijft hij het
  zijbestand wel maar start hij geen collector.
- **`tick`** verzamelt de runs zelf, uit de ruwe transcriptbytes van de werkplek van de run
  (`workplace.RawTranscripts`): een open run hooguit eens per uur (het moment staat in
  `<state>/telemetry/last-collect`), een gesloten run elke tick tot zijn `setups`-rij na zijn
  einde is vastgelegd; een gesloten run waarvan een slag geen rij opleverde (geen regel in zijn
  venster, of geen transcript) hooguit eens per uur (`<state>/telemetry/closed-tried/<run-id>`).
  Dat gebeurt vóór het opruimen van werkplekken en mirrors, zodat een
  transcript niet verdwijnt voordat het is gelezen. Een transcript dat de werkplek niet kan tonen
  (weg, niet op deze host) geeft `unreachable, not processed` en is geen fout; een andere fout
  geeft een regel met het run-id en zet de tick op mislukt, waarna hij doorgaat.

### Menswerk en wachttijd per traject

`lighthouse trajectory <issue> [--repo-dir <pad>] [--db <pad>]` toont van het traject van een issue
wanneer elke vraag, elk antwoord en elke overname was, en hoe de tijd van het traject over de
oorzaken verdeeld is. Repo komt uit de `lighthouse.toml` in `--repo-dir` (standaard `.`), `--db`
is standaard de `lighthouse.db` van de statusmap. De opdracht leest het issue van de forge voor
het sluitmoment.

De uitvoer is eerst een regel per moment, `<moment> <tijdstip> [estimate] [unanswered] [reden]`:
`question`, `answer` of `takeover` met het tijdstip (UTC, RFC 3339). `estimate` staat achter een
moment dat niet is opgeslagen maar uit een run is afgeleid (een vraag uit het einde van haar run,
een antwoord uit de start van de run die het oppakte), `unanswered` achter een vraag zonder
antwoord, en een overname eindigt waar bekend op de reden. Daarna volgt een regel per oorzaak in
vaste volgorde: `active`, `human`, `usage`, `ci_review`, `pickup` en, als die er is,
`unexplained`. Elke oorzaakregel heeft de vorm `<oorzaak> <duur> <soort>`, met als soort
`measured`, `estimate` of `running` (het traject loopt nog). Een `active`-regel die CI- en
reviewtijd kan bevatten eindigt op `incl. ci_review`.

Exit: 0 uitvoer geprint | 1 een config-, forge- of databasefout, of `no trajectory for issue <N>`
| 2 verkeerd gebruik (geen of geen geldig issuenummer, onbekende vlag).

### Indeling in een experiment (`[[experiment]]`)

Een experiment deelt een aandeel van de trajecten in een variant in: voor één onderdeel krijgen die
trajecten een andere keuze dan de standaard (#572, spec
[explore/exploit](developer/planning/specs/2026-11-24-issue-388-explore-exploit-design.md)). Een mens
zet het aan en uit met een PR op de `lighthouse.toml` van elke meedoende repo:

```toml
[[experiment]]
id        = "plan-review-codex-2026-12"   # verplicht; uniek binnen de repo
component = "plan_review"                 # verplicht; plan_review of spec_review
choice    = "codex"                       # verplicht; codex of opus
share     = 0.10                          # verplicht; boven 0 en hooguit 0.20
sample    = 20                            # verplicht; geheel getal vanaf 1: variant-trajecten
start     = "2026-12-01"                  # verplicht; YYYY-MM-DD, middernacht lokale tijd
```

Een fout veld, een onbekende sleutel of een dubbel `id` maakt de config van de repo ongeldig, zoals
elke andere configfout. Er is geen `end`: een experiment stopt door zijn blok te verwijderen of door
de steekproef.

- **Wanneer een traject een variant krijgt.** De dispatcher deelt een traject in bij de eerste
  claim van zijn issue. Het wordt een variant (`variant:<component>=<choice>`) als de start is
  bereikt, het aantal variant-trajecten van het experiment onder de `sample` ligt en de hash van
  `repo` en `issue` onder `share` valt (een vaste waarde: dezelfde keuze bij elke herhaling).
  Anders blijft het `standard`. Alleen trajecten met een reviewrun van het onderdeel tellen mee voor
  de steekproef, ook bij nul bevindingen; een traject dat zijn variant nog niet gebruikte telt niet.
  Telling en analyse nemen alleen runs vanaf `start`.
- **Het traject houdt zijn indeling.** Elke latere run van het traject krijgt dezelfde indeling,
  ook als het experiment ondertussen stopt of verandert. Trajecten van vóór deze functie die al een
  run hebben, worden bij de migratie `standard` en blijven dat; een traject zonder run (de collector
  maakt trajecten ook vóór de eerste claim) blijft leeg tot zijn eerste claim. Een run krijgt zijn
  indeling als regel `assignment: <indeling>` in de prompt; bij `standard` ontbreekt de regel.
  Interactieve sessies delen niet in.
- **Reikwijdte over repo's.** De dispatcher bedient meerdere repo's. Hetzelfde `id` in meer repo's is
  één experiment en moet in elke repo in alle velden gelijk zijn, anders deelt hij niets in. Aandeel
  en steekproef tellen over die repo's samen. Hooguit één experiment tegelijk geldt over alle
  bediende repo's: declareren ze experimenten met verschillende `id`'s (op verschillende of op
  hetzelfde onderdeel), dan deelt hij niets in en logt hij
  `<repo>#<issue>: <reden>`. Alles blijft dan `standard`.
- **Toepassen en vastleggen.** De skills `lighthouse-review-plan` en `lighthouse-review-spec`
  lezen de indeling en passen de keuze toe: onder `variant:plan_review=codex` draait ronde 1 zonder de
  Opus-kant. Het reviewrecord legt de indeling vast en of die is toegepast (`assignment_applied`).
  Een variant van een ander onderdeel geldt voor die review als `standard`; `write` weigert hem.
  Een interactieve review zonder `assignment:`-regel schrijft `standard` met een lege
  `assignment_applied`; dat de indeling niet is toegepast blijkt pas uit de join van
  `review_runs.assignment` met `trajectories.assignment`, voor een reviewrun waarvan
  `review_runs.component` het onderdeel van de variant van het traject is.
  Valt een `codex`-variant door de budgetcheck terug op Opus, dan blijft het traject in zijn groep
  (intention to treat); de fallback staat in `review_mode` en in `setup_switches`.

**Nieuwe kolommen in `lighthouse.db`** (migraties `0015` tot en met `0018`):

| Tabel | Kolom | Inhoud |
| --- | --- | --- |
| `trajectories` | `assignment` | `standard`, `shadow:<x>` of `variant:<onderdeel>=<keuze>`; gezet bij de eerste claim, daarna vast |
| `trajectories` | `experiment_id` | het `id` van het experiment bij een variant, anders leeg |
| `review_runs` | `review_mode` | hoe de review draaide, bijvoorbeeld `fallback` |
| `review_runs` | `reviewers` | JSON-array van de reviewers, bijvoorbeeld `["codex","opus"]` |
| `review_runs` | `assignment` | de indeling uit het record, standaard `standard` |
| `review_runs` | `assignment_applied` | 1 toegepast, 0 niet toegepast, leeg zonder veld in het record |
| `findings` | `reviewer_source` | `codex`, `fallback` of `both` |
| `findings` | `severity_codex`, `severity_fallback` | de ernst per reviewer |
| `setups` | `prompt_hashes` | JSON met de hashes van de reviewprompts van de run |
| `setups` | `controller_settings` | JSON met de `[controller]` van de repo bij het verzamelen |
| `setup_switches` | (nieuwe tabel) | een wissel binnen een run: `setup_id`, `at`, `field` (`model` of `reviewer`), `from_value`, `to_value` |

Hoe de collector `setups.prompt_hashes`, `setups.controller_settings` en `setup_switches` vult staat
bij *Inzet, kosten en tokens*, de herleiding van het traject van een reviewrecord in
[`review-index.md`](review-index.md). De indeling van trajecten die nog geen experiment kennen blijft
`standard`; `runs.assignment` volgt het traject.

## Run the tick

`lighthouse tick` is the dispatcher. With `[dispatch] workplace = "local"` in `host.toml` it needs no
kenn-forge: each run gets a git worktree and a detached `claude -p` on this host (see
[workplace.md](workplace.md#lokaal) and [tick.md](tick.md)). The host needs Linux with `/proc`, `git`,
`setsid` from util-linux and `claude` on the `PATH` of the tick.

Fetch the unit, [lighthouse-tick.service](../deploy/systemd/lighthouse-tick.service), and its timer,
[lighthouse-tick.timer](../deploy/systemd/lighthouse-tick.timer), the way you fetched `lighthouse.service`:
the tag of your binary for `<tag>`, and an `ExecStart` path that matches where the binary lives.

```bash
curl -fsSLo ~/.config/systemd/user/lighthouse-tick.service https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/systemd/lighthouse-tick.service
curl -fsSLo ~/.config/systemd/user/lighthouse-tick.timer https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/systemd/lighthouse-tick.timer
systemctl --user daemon-reload
systemctl --user enable --now lighthouse-tick.timer
loginctl enable-linger "$USER"
```

With a clone, copy both files from `deploy/systemd/` instead. Enable the timer, not the service; it runs
the tick two minutes after boot and every three minutes after. `enable-linger` keeps the timer running
when you are not logged in.

Two choices in the unit are on purpose:

- `KillMode=process`. A detached run lives in the cgroup of the unit. With the default
  `KillMode=control-group` systemd kills every process of the cgroup when the tick ends, so each run
  would die with the tick that started it. While runs live, systemd logs `Found left-over process … in
  control group while starting unit. Ignoring.` on each tick; that is expected.
- No hardening (`ProtectHome=`, `ProtectSystem=`, `NoNewPrivileges=`). The runs inherit the unit's
  namespace and must write the worktree, `~/.claude` and the state directory, and run git, go and the
  test gate.

Put `GH_TOKEN` in `~/.config/lighthouse/forge.env` (mode `0600`), the file `lighthouse.service` also
reads (see *Connect the forge*). Issue runs inherit the token, so a run has the rights of the
dispatcher. A scheduled run starts without the token variables in its environment, but it still runs
as your user and can read what you can read, such as `gh`'s `hosts.yml`.
`claude` must be on the unit's `PATH`; the unit adds `%h/.local/bin`, where the claude installer puts it.
It also adds `/usr/local/go/bin`, where the official Go distribution puts `go`, which runs and the test
gate need.

Without systemd, run the tick from cron. Cron sets no `XDG_RUNTIME_DIR`, which the tick needs, so the
line sets it:

```text
*/5 * * * * export XDG_RUNTIME_DIR=/run/user/$(id -u) PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/bin:/usr/local/go/bin:/usr/bin:/bin"; set -a; . "$HOME/.config/lighthouse/forge.env"; set +a; lighthouse tick
```

### The workflow pusher

A run never pushes a commit that touches `.github/workflows/`: GitHub refuses it for a token without
`workflow` scope, and no run gets such a token. The tick starts the workflow pusher instead
(`lighthouse push-workflows`, see [tick.md](tick.md#wat-tick-doet)). It needs its own token and a few
tools on the tick host. Without `[dispatch.workflow_push]` in `host.toml` the route is off and a handoff
`next: push-workflows` is returned to you.

- **The token.** A fine-grained personal access token for the repositories of the controller only, with
  repository permissions *Contents* (read and write) and *Workflows* (read and write), and nothing else.
  It is not the `GH_TOKEN` of `forge.env`: that token stays without workflow rights, and the workflow token
  never goes into the environment of the tick, a run or a reviewer.
- **The file.** One token in a regular file, owned by you, with mode exactly `0600`; the pusher refuses
  `0400`, a symlink, a looser mode and a file with more than one token. Put it outside every worktree and outside every path a reviewer binds. A reviewer can never bind the
  token file, `~/.config/lighthouse`, `~/.config/gh` or `~/.kenn/forge`. Name it in `[dispatch.workflow_push] token_file`.
- **The tools.** `bwrap` (bubblewrap), `actionlint` and `zizmor` must be on the `PATH` of the tick; the
  pusher inherits it. A missing one is a red step with `COULD NOT CHECK`, never a green one: the Vraag on
  the issue says which tool is missing, and nothing is pushed. The real output formats of `actionlint`
  and `zizmor` are checked against fakes only: run the pusher once by hand on a real repository before
  you rely on it.
- **The reviewers.** Optional, two or three, set in `[[dispatch.workflow_push.reviewers]]`; see
  [tick.md](tick.md#dispatchworkflow_push). The review starts in `shadow` mode: it runs, but only you
  decide, by answering `push`.
- **The intent check.** Optional, one reviewer without a topic, set in
  `[dispatch.workflow_push.intent]` (`command`, `binds`, `env`, validated like a reviewer). For a
  pre-merge change that adds no event, action, pin, permission, secret, runner, environment or image
  to an existing workflow, it weighs the diff against the issue, spec and plan; its `approve` pushes
  without the security review, in `shadow` and in `enforce`, and its `reject` asks you. A question of the
  review says whether the reviewers or this table are missing, and `lighthouse level` names it too.

  ```toml
  [dispatch.workflow_push.intent]
  command = ["claude", "-p", "--tools", ""]
  ```

- **A push by hand.** When you push the branch yourself under an open pusher question, the next tick
  sees that the branch holds its `sha` and starts the pusher once, without a permit: it finds the SHA
  on origin and carries the chain on, as after its own push.
- **The merge.** Whether GitHub refuses an API merge of a workflow-touching PR for a token without
  `workflow` scope has not been verified. The merge by the pusher is therefore built, and the tick uses
  it only for a PR whose branch has a pusher comment; any other PR is merged by the tick as before.

### Scheduled runs

A repository can have runs on a fixed schedule, without an issue (#121, see
[tick.md](tick.md#geplande-runs)). Each `[[schedule]]` block in its `lighthouse.toml` is one schedule:

```toml
[[schedule]]
name = "codebase-map"   # required; [a-z0-9-]+, unique per repository: it goes into a branch name
job  = "map-refresh"    # required; map-refresh, reflect, map-planned, architecture-scan or measure-recovery
cron = "0 4 1 * *"      # required; five fields, in the local time of the host
# profile = "software"  # the profile of the run; default [controller] profile

[schedule.params]       # per job; every parameter has a default
publish_dir = "docs/developer/codebase"

[[schedule]]
name = "architecture-scan"
job  = "architecture-scan"
cron = "0 6 1 1,4,7,10 *"   # quarterly, 06:00 on the 1st

[[schedule]]
name   = "reflect"
job    = "reflect"
cron   = "0 1 * * MON"
weeks  = 2              # weeks and anchor go together: only the week of anchor and every
anchor = "2026-10-12"   # weeks-th week after it count (a week runs Monday to Sunday)
```

An invalid block makes the repository's config invalid, like any other config error. Jobs `map-refresh`,
`reflect`, `map-planned`, `architecture-scan` and `measure-recovery` are available (their params are in [tick.md](tick.md#job-map-refresh), [tick.md](tick.md#job-reflect), [tick.md](tick.md#job-architecture-scan) and [tick.md](tick.md#job-measure-recovery); `map-planned` takes none, see [tick.md](tick.md#job-map-planned)). An older binary refuses a `lighthouse.toml` with a `job` it does not know and skips the repo, so rebuild the host after the merge that adds such a block. A schedule
that names a job the binary does not know makes the tick report, at each gather of the repo,
`lighthouse: <repo> scheduled:<name>:<slot>: no job <job>` (a line, not an error: the tick still exits 0 and
saves its state) and start nothing.

A scheduled run ends in a pull request that the tick merges itself once it is green. The target
repository's CI must be able to get that PR green with only the paths the job may write: a
repository whose CI demands, say, a changelog fragment in every PR needs a configuration that lets
these PRs through. Otherwise every period closes a red PR as `failed`. A repository with no CI
checks at all never merges either: the tick requires the checks to be `SUCCESS`, so such a PR is
closed as `failed` after 60 minutes. The token needs *Contents:
write* and *Pull requests: write* to push the branch, open, merge and close the PR and delete the
branch.

## A node for another host

A kenn-forge node that runs content for a tick on another host (`[dispatch.nodes.<name>]` with
`serve_url`, see [tick.md](tick.md#nodes)) runs `lighthouse serve` too. Its `host.toml` needs:

```toml
[node]
token_file = "/abs/path/node.token"      # bearer token the tick sends; same content as serve_token_file there

[dispatch.forge]
url = "http://127.0.0.1:8091"
token_file = "/abs/path/forge.token"     # the node's own kenn-forge token; serve refuses to start without it
unit_prefix = "lighthouse-"

[dispatch.profiles.software]             # every profile name the tick uses: mandatory
```

Without `[node]` the routes under `/api/node/v1/` answer 404. A profile the tick uses but the node does
not list gets 400 on transcript, stop and kill. `lighthouse ask` must be on the `PATH` of the agent
(the claude the node's forge starts), not only of the service. The `mcp_config` of a profile with `node`
in the tick's `host.toml` is a path on that node: the tick sends it unchanged and never reads it.
The state files of the node (PAUSE, alarm, markers) are mode `0600` in a `0700` directory; when the
runs live in a container, its uid must match.

On macOS, run `serve` as a launchd agent: [com.lighthouse.serve.plist](../deploy/launchd/com.lighthouse.serve.plist).
`serve` binds loopback and has no TLS: put `tailscale serve` in front (see *Reach it from your tailnet*
below) and give the tick that https address as `serve_url`. Use the tailnet name of your own node; never
`tailscale funnel`.

## Reach it from your tailnet

[tailscale-serve.sh](../deploy/tailscale-serve.sh) exposes the local server over HTTPS on the
tailnet. It takes the port as an optional argument (default 8127).

One-time prerequisites:

```bash
sudo tailscale set --operator="$USER"
```

Also enable HTTPS certificates in the tailnet admin console. Then fetch the script for your
release tag (see `<tag>` above) and run it:

```bash
curl -fsSLO https://raw.githubusercontent.com/CorneeldH/lighthouse/<tag>/deploy/tailscale-serve.sh
chmod +x tailscale-serve.sh
./tailscale-serve.sh
```

With a clone, run `deploy/tailscale-serve.sh` instead.

Turn it off with `tailscale serve --https=<port> off`. Never use `tailscale funnel`: it
publishes the server on the public internet.

## Security

The server has no authentication, on purpose. The tailnet is the access boundary: any device on
the tailnet can do everything the API can. The server binds loopback, so nothing outside the
host and the tailnet sees it. Today the API is read-only; once lighthouse starts runs, any
device on the tailnet can start one.

`/api/questions` and `/api/returned` show the issue titles and question text of the served
repositories, private ones too (read with the server's token), to every device that can reach
the server. `/api/runs` shows their issue titles and the commands of their runs.
