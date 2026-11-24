# Setup levels

Lighthouse has three setup levels: `light`, `standard` and `strict`. A level sets the stand of
each component (`off`, `log` or `on`) and the value of the flags, such as `labels`. A first
`lighthouse init` in a new repository chooses `light`; see [Start small](install.md#start-small)
for the commands. `lighthouse level` reads the level and the stand per component;
`lighthouse init --upgrade --level <name>` switches to another level.

- `off`: the component is not installed.
- `log`: the hook runs, never blocks, says nothing in the chat and counts what it would have
  reported; `lighthouse level` shows the count.
- `on`: the full behaviour. A gate blocks; `worktree-check` warns.

A skill is only `off` or `on`.

## What each level skips and why

The stands come from the `levels.toml` file of each package under `pack/`.

| Component | `light` | `standard` | `strict` | Why it is off or logged on `light` |
|---|---|---|---|---|
| `main-commit-gate` | on | on | on | Core rule: nothing lands on `main`. |
| `worktree-check` | on | on | on | Core rule; it only warns. |
| `memory-gate` | log | on | on | Core rule: no harness memory; `log` counts the writes it would have blocked. |
| `pr-create-gate` | on | on | on | Core rule: every PR names its issue. |
| `bash-write-check` | on | on | on | Carrier: it applies the stands of the write checks below. |
| `commit-test-gate` | off | on | on | At first the PR and its CI carry the quality, not a test run after each commit. |
| `md-link-check` | log | on | on | Many `.md` writes, so much noise in the first step; `log` counts what it would have blocked. |
| `code-language-check` | log | on | on | Only acts with `[project] code_language` set; `log` counts what it would have reported. |
| Lint and format hooks of the language packs (`gofmt-check`, `go-lint-gate`, `ruff-format-check`, `ty-gate`, `shell-lint-check`) | off | on | on | Lint and format come after the way of working. |
| Imports gates of the language packs (`go-imports-gate`, `python-imports-gate`, `node-imports-gate`) | on | on | on | They guard the declared imports at every level. |
| Standard skills of the language packs (`go-standard`, `python-standard`, `shell-standard`) | off | on | on | They follow their hooks: their text describes lint and format hooks that are off on `light`. |
| Skills `start-issue`, `route-issue`, `forge`, `write-pr`, `close-issue`, `write-handoff`, `place-instruction`, `upgrade` | on | on | on | The chain from issue to PR to a closed issue, plus the way up. |
| Skills `write-issue`, `fix-pr`, `audit-worktrees`, `audit-agent-config`, `test-standard` | off | on | on | Maintenance of the chain. |
| Skill `map-planned` | off | on | on | Places planned issues on their map; part of issue management. |
| Skills `review-spec`, `review-plan` | off | off | on | They ask Codex for a review by a second model family. |
| Flag `labels` | off | on | on | Labels belong to the issue management of `standard`. |

The expo pack has one component, the skill `refresh-expo-lockfiles`, and it is on at every level.
The base components (`run.sh`, `blocking`, `usage-gate`, `permission-check`,
`workflow-push-gate`, `level-check`, `no-verify-check`) are never switched.

Permissions are the same at every level: no level adds to or removes from
`permissions.allow`, and no level answers a permission or security question. The core rules stay
on at every level. Only an override in `[components]` of `lighthouse.toml` turns a core gate off,
and then `init`, the session start and the managed block in `AGENTS.md` warn about it.
