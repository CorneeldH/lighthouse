# Changelog

All notable changes to lighthouse. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); how versions are
chosen is in [docs/releasing.md](docs/releasing.md). The release workflow writes the sections below from the
fragments in `changelog.d/`; never edit a released section by hand.

## [v0.8.7] - 2026-11-06

### Fixed

- In a clone of a fork `gh pr create` opened the PR in the upstream repo, because `lighthouse-write-pr`
  passed no `--repo`. The skill now passes `--repo` with `forge.repo`, as does every other writing
  `gh` command in the skills, and `pr-create-gate` blocks a `gh pr create` without `--repo`, `-R` or
  `GH_REPO` and names the exact `--repo <forge.repo>` to add.

## [v0.8.6] - 2026-11-06

### Fixed

- `lighthouse init` and `init --upgrade` now warn when git ignores a file they install, for example `.lighthouse/lib/` under a `lib/` rule, with the rule that ignores it. Such a file works locally but never reaches a commit.

## [v0.8.5] - 2026-11-02

### Fixed

- The dispatcher starts `/lighthouse-review-spec` and `/lighthouse-review-plan` again: its allowlist
  still held the names from before the rename and returned every review handoff. A handoff with an
  old name now starts the review under its new name.

## [v0.8.4] - 2026-11-02

### Fixed

- The checkpoint secret scan no longer holds checkpoints back on Entire's `turnAttributionKey` or on an Anthropic beta header that gitleaks cut off at a chunk boundary.

## [v0.8.3] - 2026-11-02

### Fixed

- The tick now removes the forge workspace of an issue that left Controller once it has no open run, no live foreign session, no local changes and no unpushed commits. A workspace it must keep is listed under *Workspaces to clean up*.

## [v0.8.2] - 2026-11-02

### Fixed

- The install guide for agents now has the agent ask to add `@AGENTS.md` to an existing `CLAUDE.md` and carry on, instead of leaving it open and stopping before the check.

## [v0.8.1] - 2026-11-01

### Fixed

- `bash-write-check` no longer hands a file that lighthouse itself installed (a file unit in `.lighthouse/manifest.toml`) to its gates, so `ruff format` or `gofmt` no longer rewrites it when `lighthouse init` runs in an agent's Bash call, and `lighthouse init --upgrade` afterwards says `No changes.`

## [v0.8.0] - 2026-11-01

### Changed

- The `lighthouse-triage.service` example in `docs/triage.md` now loads the token from an `EnvironmentFile`, sets a `PATH` that finds `claude`, and runs the binary from `~/go/bin`, where the install guide puts it.
- The *De pauzevlag* section of `docs/usage-gate.md` now says how to set the pause flag: `lighthouse pause` and `lighthouse pause off`, with a link to `tick.md#pauze`.

### Fixed

- A controller run that ended on a question, a handoff or Done no longer leaves its `claude` running in the workplace, so resuming it after an answer no longer fails with `submit initial message failed`. The tick ends only that session's process, never the unit, so an interactive session in the same worktree runs on.

## [v0.7.3] - 2026-11-01

### Fixed

- `docs/tick.md` (*Pauze*) no longer says a running run carries on during a pause. The tick starts nothing, and a running controller run parks at its next tool call; the text now says so and links to the pause flag in `usage-gate.md`.

## [v0.7.2] - 2026-11-01

### Fixed

- `lighthouse triage` no longer forwards an issue to `Controller` when a commit under another issue number already did the work. The judgment now weighs the code it reads, and reports such an issue as already done.

## [v0.7.1] - 2026-11-01

### Fixed

- A controller run that asks a question with `lighthouse ask` now stops without posting a handoff. The run instructions and the handoff skill both say so, so the question stays the last turn on the issue and the answer resumes the session.

## [v0.7.0] - 2026-11-01

### Changed

- `docs/tick.md` now says in the *Pauze* section that a parked run shows as `parked` in `GET /api/runs` and on the *Running runs* card.

### Fixed

- A controller run now writes the text of a `lighthouse ask` question in the `[project] language` of `lighthouse.toml`, so a question no longer comes out half in English.

## [v0.6.0] - 2026-11-01

### Added

- `/lighthouse-write-pr` runs a local roborev (Codex) review before the PR is opened when `[review] roborev = "local"` is set, processes the findings and records them in a PR comment. The dispatcher starts `/lighthouse-fix-pr` on a red PR (trigger `fixpr`), at most 3 runs per PR; after that the run returns with "PR red after 3 fix-pr runs".

### Changed

- `docs/tick.md` now has a *Zicht* section that points to `GET /api/runs` and the *Running runs* card of `lighthouse serve`, with a link to `docs/install.md`.

### Fixed

- Triage now judges an issue in a temporary export of the tracked content of the default branch, so it no longer reads untracked files, ignored files or other worktrees. A free-text field that is too long, or a `redundancy_evidence` field with prose instead of references, now makes the judgment invalid.
  The judgment no longer loads Claude Code settings sources: only `env` and `modelSettings` are taken from the user settings file, so auth that lives only there (for example `apiKeyHelper`) no longer reaches it.

## [v0.5.0] - 2026-11-01

### Added

- A map of your repo after `lighthouse init` in `docs/site/repo.html` shows, as a tree, every place `init` writes, with a link to where each is explained. A switch shows what `init` adds and which of your files it edits. A test fails when `init` writes to a place that is not on the map.
- The web UI has a *Running runs* card, fed by the new read-only `GET /api/runs`: the open and parked runs with their command, trigger, profile, workspace and measured stand, and the deferred starts with their reason. It reads the state the tick writes every tick, so it costs no forge calls. With `[dispatch] repos` in `host.toml`, `serve` now shows every repo listed there instead of the one `--repo`, and every question, returned issue and run carries its `repo`, shown as `owner/name#number`.

### Changed

- The configuration page reads more easily. In `lighthouse.toml` and `host.toml` what a key does now stands as a comment behind the key, aligned per table, with a heading and a blank line per topic. Below the files each table has a title and a short explanation, and a key with a fixed set of values shows them as options with the default highlighted, such as `nl` and `en` for `[project] language`.

### Fixed

- A launch the workplace refuses now names its reason on the tick line and posts a 🤖 Returned *start failed: <reason>* under its 🤖 Started, so the issue shows why no agent runs. An uncertain launch names its cause on the tick line and posts nothing, since an agent may be running.

## [v0.4.1] - 2026-11-01

### Fixed

- A resumed run without a measurable transcript is no longer returned as silent on its first tick: the forge-silence rule now counts from the run's start at the earliest, as the transcript rule already did.

## [v0.4.0] - 2026-11-01

### Added

- A map of the configuration in `docs/site/config.html` lists every key of `lighthouse.toml` and `host.toml` with whether you set it, its default, and the skills, hooks and code that read it. A test fails when a key in `config.py` or the `[usage]` table of `host.toml` is missing from the map.
- `lighthouse tick` checks one shared brake on the host before every start or resume: the pause flag, the usage window, MemAvailable, slots and software runs, with the reason in the deferred line. `lighthouse pause [--until HH:MM]` and `lighthouse pause off` set and clear the pause flag, and a controller run the usage guard parked resumes with `ga verder` after the reset.
- `lighthouse tick` parks an executing issue that has an active blocker (a `🤖 Geparkeerd` comment with `blocked_by`, then the board to On hold), finishes a half-done park on the next tick, and puts the issue back in Controller once no blocker is active; an On hold set by a human stays untouched. The overview (`lighthouse serve`, `/api/questions`, `/api/returned`) shows only Controller issues when the repo is in `host.toml [dispatch]` with a `[forge.board]`.
- `lighthouse triage` routes the candidate issues of every repo with `[controller.triage]` through one read-only `claude -p` judgment each, writes a triage comment with the route line (and design questions at gate 4c), and moves an issue on gate 4a, 4b or 2 to `Controller` with a `🤖 Handoff` that waits for the night window. `--dry-run` judges without writing.

### Changed

- The configuration page in `docs/site/config.html` reads as a reference now: first where `lighthouse.toml` and `host.toml` live, then both files in full with a comment and the default of every key, and per table one row per key with its level, default, what it does and what reads it. It replaces the key buttons, the level filter and the drawer. A test fails when the files on the page stop passing `config.py`.
- On the loop and the session map, a click on a chip or a handoff that the chosen route passes by now switches to the first route it is on, so the clicked step lights up with its neighbours.
- The loop shows auto-answer as its own planned step, next to triage: a controller run that takes a clear recommendation itself instead of asking.
- The loop map now lives at `loop.html`; `chain.html` forwards there, so older links keep working. The route buttons no longer show the gate number of `route-issue`, such as "(4c)".
- The GitHub section of the reference page now explains what lighthouse keeps on GitHub: issues, milestones, labels, the board and pull requests. It also says what `lighthouse init` puts in `.github/`: the pull request template and, with `[ci]` set, the unblock-dependents workflow, and no CI, release workflow or issue templates.
- The tick now decides the outcome of every open run: it hands the issue back with an honest reason when a session is silent, waits on a permission prompt, did not start, or leaves a draft, closed or red PR. It stops the session when needed and closes a run whose issue was closed. A new hook, `permission-check`, notes a permission prompt of a controller run.
- A pull request that touches only `CONTRIBUTING.md`, `SECURITY.md` or `CLA.md` no longer starts the Go jobs in CI, and each CI pytest part names itself in the pytest header (`LIGHTHOUSE_PYTEST_SHARD=2/3`), also under `-n auto`.

### Fixed

- The triage comment of `lighthouse triage` no longer turns `@name`, `#N` or `owner/repo#N` in the model's free text into a mention or a cross-reference: a zero-width joiner follows the `@` or `#`. The gate-0 blockers and the `#N` of a possible duplicate, which Go writes itself, stay clickable.

## [v0.3.0] - 2026-11-01

### Added

- A map of one session in `docs/site/session.html` follows a turn from prompt to stop: the writing skills that load before the model writes, and every hook on the event it runs on, with its matcher, package and behaviour. The index and the getting-started guide link to it.
- The system map (`system.html`) now shows lighthouse in three steps: install once on your workstation, `lighthouse init` in your repo, and run the loop, with every box linking to the details. A new reference page (`reference.html`) lists every skill, agent, hook and workflow per pack, each linking to its place in the repository. Every map now gives each sort of part one glyph from a single table, and the chain map is called "The loop".
- `lighthouse tick` runs as its own process next to `lighthouse serve`: it reads `[dispatch]` from `host.toml`, claims runs in the shared SQLite run state and reports every repository it skips, with the reason.
- `lighthouse tick` reads the board, the pull requests and the comments of trusted authors for every dispatchable repository, skips a repository whose state has not changed, and `lighthouse gather <repo-dir>` prints that snapshot as JSON.
- The dispatcher can prepare a kenn-forge workspace, start an agent in it through a carrier file, stop it and clean up leftovers, with rules for a busy workplace and for workspaces that must stay.
- `lighthouse tick` now decides and applies after the gather (start, resume, defer, close, restore the rocket), `lighthouse run <owner/name>#<n>` starts one issue at once, and `lighthouse ask` posts a question from the workplace.
- A new package `node` adds `gate_node.py`, the test-gate selector for a JavaScript project: when the commit touches the package directory it runs the npm scripts you name (lint, typecheck, test) after any drift check (`--drift gen=src/api`). Add it with `lighthouse init --upgrade --pack node` and put it in `[test_gate] command` yourself, for example `python3 .lighthouse/scripts/gate_node.py --dir web -- lint test`, and list exit code 3 (could not check) in `[test_gate] unchecked_exit_codes`.
- Install with your agent: paste one sentence from the install page, and the agent follows a guide written for it, asking before it installs or writes anything.
- A map site in `docs/site/` shows how an issue moves through lighthouse, from idea to close, with the skills, agents, hooks and GitHub steps at each phase and the packs `init` adds. Open it with `python3 -m http.server --directory docs/site`; it is also published on GitHub Pages.

### Changed

- The chain map no longer has an "Always on" band: it shows the flow from idea to main. `usage-guard` is on the session map, and `instrument` and `worktree-audit`, which sit outside that flow, move to the overview map later.
- The chain map shows three planned handoffs: build to verify-pr and merge to close-issue (#147), and a red PR to fix-pr (#119).
- The chain map shows the pick-up steps in the order `start-issue` runs them, draws the chosen route as a solid yellow line, keeps the "Always on" band lit on every route, and opens the details of a chip in a drawer from the right, with the routes it is on and its neighbours on the chosen route.
- On the chain map every skill, agent and hook is on at least one route, with examples marked as such, and a new route shows work without an issue. The details say whether a step is a skill, an agent, a hook or a workflow and how it starts (user-invoked, invoked by a skill, model-invoked or a hook event), and list how it works as steps.
- On the chain map, agents have a colour of their own, the legend lists Superpowers first, and the details name what a step is and how it starts in one term each.
- The chain map explains its reasoning in a "Wait, but why?" section, hides planned steps behind a "Show roadmap" switch, and marks every agent with ◆ whatever starts it.
- The chain map's roadmap layer shows exactly the open issues labelled `chain` (now roborev and automatic triage), checked by a new `chain-roadmap` workflow. The chain page explains its routes and handoffs in a short "Wait, but why?", the overview page carries the broader "Why lighthouse?", and the details end with a link to the part's full definition.
- The chain map's details explain each step in a few sentences, with a "How it works" section on the mechanism and a "When, who and why" section for the facts. It draws only the chosen route, shows the commit and write hooks as examples on the small-job route, and places the link check at the spec.
- The system map (`system.html`) shows `in repo: $ lighthouse init` as the head of step 2, no longer lists `host.toml` in that step, and drops its legend. Rules and Config, and The loop and A session, now each have a colour of their own on every map.
- Skills and hooks now follow one naming convention: a skill is named after its task (`write-pr`) or ends on `-standard`; a hook ends on `-gate` (can stop the action) or `-check` (reports after the action). "Write gates" are now "write checks". Skills (all keep the `lighthouse-` prefix in the installed name): `verify-pr` is `write-pr`, `handoff` is `write-handoff`, `instrument` is `place-instruction`, `spec-adversarial-review` is `review-spec`, `plan-adversarial-review` is `review-plan`, `worktree-audit` is `audit-worktrees`, `testing` is `test-standard`, `go-writing` is `go-standard`, `python-writing` is `python-standard`, `shell-gates` is `shell-standard`. Hooks: `main-commit-guard` is `main-commit-gate`, `commit-tests` is `commit-test-gate`, `usage-guard` is `usage-gate`, `go-check` is `go-lint-gate`, `ty-check` is `ty-gate`, `worktree-guard` is `worktree-check`, `worktree-linkcheck` is `md-link-check`, `code-language` is `code-language-check`, `bash-write-gates` is `bash-write-check`, `gofmt` is `gofmt-check`, `ruff-format` is `ruff-format-check`, `shellcheck` is `shell-lint-check`. `lighthouse init --upgrade` removes the installed copies under the old names. If you changed such an old copy locally, the upgrade refuses and writes nothing: revert your change, or run it with `--force` to remove the copy anyway. The `[usage_guard]` and `[test_gate]` sections in `lighthouse.toml` keep their names.
- The `lighthouse-spec-adversary` agent is gone: the review skills retry a failed round 1 with Codex themselves.
- The site opens with what lighthouse is and why, and installs on a page of its own: your platform open, the others one click away.
- The `go-check` and `ty-check` Stop hooks now skip a checkout where the turn touched no Go or Python path, and say so in one line instead of linting it. An edit, a Bash write or a commit in the turn still runs the check.

### Fixed

- The session map no longer shows a separate "A skill loads" moment before the tool call. A skill loads through the Skill tool and an agent starts through the Agent tool, so the standard skills and `issue-impact-scout` now sit in the column "The tool or skill call", between the hooks before and after a tool runs. The loop no longer draws the agent, and its legend drops "lighthouse agent".

## [v0.2.0] - 2026-10-31

### Added

- Every GitHub release now carries prebuilt binaries for Linux, macOS and Windows (amd64 and arm64 where available) with a `SHA256SUMS` file, so you can install lighthouse without Go.
- Releases are automatic: a merged fix ships as a patch release, everything else in the weekly minor release; the policy is in `docs/releasing.md`. `/lighthouse-verify-pr` writes a changelog fragment in repositories that opt in with `changelog.d/.lighthouse`.

### Changed

- Since v0.1.6: lighthouse's own code is in English and a code-language hook checks it, blocking Stop gates stay quiet while a valid usage alarm is set, and the test gate now runs after the commit and only on what the commit touches. `/lighthouse-write-issue` gained a path for updating an existing issue, the repository has a getting-started guide, contribution guidelines and a CLA check, and work started on a web UI that shows open questions, returned issues and the usage window (release binaries still serve a placeholder page).

### Fixed

- lighthouse builds on Windows again; `lighthouse serve` reports that its database lock is not supported there.

## Earlier versions

v0.1.0–v0.1.6: internal pre-releases.
