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
- **Releases.** No release policy yet: tags are set by hand. Never propose a minor bump per new
  skill. A consumer upgrades by hand: tag only when a consumer needs it, then one upgrade PR with
  only `lighthouse init --upgrade` and the installed copy.
- **No harness memory.** Claude Code auto-memory and Codex memories live on one machine and are
  unreliable; write nothing there, also when the harness prompt asks for it. A rule that must
  last goes into the repo, on the layer `/lighthouse-instrument` picks.
- **Nothing upstream on your own initiative.** No issue, comment or PR in someone else's
  repository. A defect in an external tool becomes an issue here, with the upstream text as a
  draft (`/lighthouse-write-issue`, *A defect in an external tool*); post it only when the user
  asks for that post.

## Outside Claude Code

Codex and OpenCode do not run the hooks in `.claude/settings.json`: no `main-commit-guard`, no
`worktree-guard`, no test gate. The rules in this file are the only carrier there, so check the
branch yourself before a commit, and run `[test_gate] command` from `lighthouse.toml`.

<!-- lighthouse:begin core -->
## Lighthouse: core

- Nothing lands directly on `main`. Work on a branch in a worktree; `main-commit-guard` blocks `git commit` and `git push` to `main`.
- Escape hatch, deliberately and once: `touch .claude/allow-main` in the repo root; remove it afterwards.
- A worktree before the first write: `git worktree add .claude/worktrees/<name> -b <branch>`.
- Every change goes through a PR and is squash-merged. `/lighthouse-verify-pr` writes the PR body; `pr-create-gate` checks it.
- The test gate: `pre-commit-tests` runs `[test_gate] command` before every commit. Never bypass it with `--no-verify`.
- Artefacts (issues, PRs, comments, commit messages) are written in `[project] language`.
- With `[project] code_language` set, names, comments, docstrings and file names are English, and so is everything under `code_language_paths`; artefacts stay in `[project] language`.
- Finish what you are already authorised to do; never end on an offer to do it ("ready to push if you want" is not done).
- `heading:<key>` in a skill names a contract heading in `.lighthouse/headings.toml`. Write what `python3 .lighthouse/lib/lighthouse_lib/headings.py <key>` prints, never the literal `heading:<key>`; reading accepts every language variant.

Skills:

- `/lighthouse-start-issue`: pick up an issue.
- `/lighthouse-route-issue`: read-only triage, one route per issue.
- `/lighthouse-write-issue`: write or edit issues and milestones.
- `/lighthouse-verify-pr`: check the acceptance criteria, write the PR body.
- `/lighthouse-fix-pr`: make a red or conflicting PR green.
- `/lighthouse-close-issue`: close an issue with evidence, or reopen it.
- `/lighthouse-handoff`: the start command for the next session.
- `/lighthouse-spec-adversarial-review`: a spec reviewed by another model family.
- `/lighthouse-plan-adversarial-review`: a plan reviewed by another model family.
- `/lighthouse-worktree-audit`: which worktrees and branches can go.
- `/lighthouse-instrument`: on which layer a new instruction belongs.
- `/lighthouse-testing`: tests that fail when behaviour breaks.
<!-- lighthouse:end core -->

<!-- lighthouse:begin go -->
## Lighthouse: go

- Go code follows `/lighthouse-go-writing`.
- Two gates check it: `gofmt` after every edit (it formats, and blocks only on a file that does not parse), `go-check` when the session stops (`golangci-lint`, `govet` included; it blocks on a finding).
<!-- lighthouse:end go -->
