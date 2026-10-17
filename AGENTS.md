# Lighthouse — working agreements

One instruction file for every harness: Claude Code (through `@AGENTS.md` in `CLAUDE.md`),
Codex and OpenCode. `CLAUDE.md` adds only what exists in Claude Code alone; never duplicate a
rule between the two.

## Rules

- **Language.** Instructions (this file, skills, agents, hook messages) are in English.
  Artefacts (issues, PRs, specs, plans, commit messages) are in Dutch.
- **Worktree before the first write.** Work that produces a commit starts in a worktree on its
  own branch, never in the main checkout:
  `git worktree add .claude/worktrees/<name> -b <branch>`, with branch `issue-<nr>-<slug>` or
  `fix-<slug>`. Exceptions: read-only work, or an explicit "on main" from the user. The cwd
  drifts back to the main checkout, so prefix every shell command with `cd <worktree>` or use
  `git -C <worktree>`.
- **Nothing lands on `main` directly.** Every change goes through a PR and is squash-merged.
  `main-commit-guard` blocks `git commit` and `git push` on `main`. If something really has to
  go on `main`: `touch .claude/allow-main`, once, and remove it afterwards. Never commit it.
- **Atomic commits**, one logical change each, as Conventional Commits
  (`feat(scope): …`, `fix(scope): …`, `docs(scope): …`).
- **Links.** In a `.md` file, always a relative link to an existing target. In chat, the full
  absolute path: the IDE resolves chat links against the main checkout.
- **Planning documents.** Specs in `docs/developer/planning/specs/`, plans in
  `docs/developer/planning/plans/`.
- **Tests.** Run `uv run pytest` before a push. CI runs the same on every PR.

## Outside Claude Code

Codex and OpenCode do not run the hooks in `.claude/settings.json`: no `main-commit-guard`, no
`worktree-guard`. The rules above are the only carrier there, so check the branch yourself
before a commit.

## Bootstrap

The guards in `.claude/hooks/` are a manual copy from `personal_ai_agent@231f6e84`, translated
to English. `worktree-guard` no longer stays silent when it cannot check. Sub-issue M0 (b)
replaces this bootstrap through `lighthouse init` on this repo itself.

The chain skills (`/paia-*`, and the future `/lighthouse-*`) do not exist in this repo yet. Use
the Superpowers skills directly.
