# Lighthouse — working agreements

One instruction file for every harness: Claude Code (through `@AGENTS.md` in `CLAUDE.md`),
Codex and OpenCode. `CLAUDE.md` adds only what exists in Claude Code alone; never duplicate a
rule between the two.

The rules below are lighthouse's own. The chain rules every lighthouse repo shares are in the
managed block at the end, which `lighthouse init` writes; never repeat one of them here.

## Rules

- **Language.** Instructions (this file, skills, agents, hook messages) are in English.
  `[project] language` is `nl`: artefacts are Dutch, specs and plans included.
- **Worktrees.** Branch `issue-<nr>-<slug>` or `fix-<slug>`. No worktree for read-only work,
  or after an explicit "on main" from the user. The cwd drifts back to the main checkout, so
  prefix every shell command with `cd <worktree>` or use `git -C <worktree>`.
- **`.claude/allow-main`** is never committed.
- **Atomic commits**, one logical change each, as Conventional Commits
  (`feat(scope): …`, `fix(scope): …`, `docs(scope): …`).
- **Links.** In a `.md` file, always a relative link to an existing target. In chat, the full
  absolute path: the IDE resolves chat links against the main checkout.
- **Planning documents.** Specs in `docs/developer/planning/specs/`, plans in
  `docs/developer/planning/plans/`.
- **Dogfood.** Lighthouse runs its own `pack/core`. A change under `pack/`, or under
  `internal/` that changes what `init` writes, also runs `go run ./cmd/lighthouse init --upgrade`
  and commits the installed copy; CI requires `No changes.`
- **Releases.** The release workflow tags and publishes, never a hand-set tag: see
  [`docs/releasing.md`](docs/releasing.md). Every PR adds a fragment to `changelog.d/`.
- **No harness memory.** Claude Code auto-memory and Codex memories live on one machine and are
  unreliable; write nothing there, also when the harness prompt asks for it. A rule that must
  last goes into the repo, on the layer `/lighthouse-place-instruction` picks.
- **Nothing upstream on your own initiative.** No issue, comment or PR in someone else's
  repository. A defect in an external tool becomes an issue here, with the upstream text as a
  draft (`/lighthouse-write-issue`, *A defect in an external tool*); post it only when the user
  asks for that post.

## Outside Claude Code

Codex and OpenCode do not run the hooks in `.claude/settings.json`: no `main-commit-gate`, no
`worktree-check`, no test gate. The rules in this file are the only carrier there, so check the
branch yourself before a commit, and run `[test_gate] command` from `lighthouse.toml`.

<!-- lighthouse:begin core -->
## Lighthouse: core

- Nothing lands on `main`: work in `git worktree add "$ROOT/<name>"` with `ROOT=$(lighthouse config worktree.root)` (default `.claude/worktrees`); `main-commit-gate` blocks `main` commits and pushes.
- Every change goes through a PR and is squash-merged; `/lighthouse-write-pr` writes the PR body.
- `commit-test-gate` runs `[test_gate] command` after each commit; fix red in a new commit.
- Never `--no-verify`.
- Update a branch with `origin/<default branch>` by `git merge` once it is on origin, never by rebase plus force-push; rebase only a branch that is not pushed yet.
- Artefacts (issues, PRs, comments, commit messages) are in `[project] language`; chat too, unless `en`.
- With `[project] code_language` set, names, comments, docstrings, file names and everything under `code_language_paths` are English.
- Finish what you are already authorised to do; an offer to do it is not done.
- `heading:<key>` in a skill names a contract heading in `.lighthouse/headings.toml`. Write what `lighthouse headings <key>` prints, not the literal `heading:<key>`; reading accepts every language variant.
- `lighthouse config` exits 1 with `unknown command` on stderr: the binary is too old. Stop and say so; that is not "not set".
<!-- lighthouse:end core -->

<!-- lighthouse:begin go -->
## Lighthouse: go

- Before a commit, run `lighthouse imports check` next to `[test_gate] command`.
<!-- lighthouse:end go -->

<!-- lighthouse:begin node -->
## Lighthouse: node

- Before a commit, run `lighthouse imports check` next to `[test_gate] command`.
<!-- lighthouse:end node -->
