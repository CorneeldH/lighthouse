# Changelog

All notable changes to lighthouse. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); how versions are
chosen is in [docs/releasing.md](docs/releasing.md). The release workflow writes the sections below from the
fragments in `changelog.d/`; never edit a released section by hand.

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
