## Lighthouse: core

- Nothing lands directly on `main`. Work on a branch in a worktree; `main-commit-guard` blocks `git commit` and `git push` to `main`.
- Escape hatch, deliberately and once: `touch .claude/allow-main` in the repo root, and remove it afterwards.
- A worktree before the first write: `git worktree add .claude/worktrees/<name> -b <branch>`.
- Every change goes through a PR and is squash-merged. `/lighthouse-verify-pr` writes the PR body; `pr-create-gate` checks it.
- The test gate: `pre-commit-tests` runs `[test_gate] command` before every commit. Never bypass it with `--no-verify`.
- Artefacts (issues, PRs, comments, commit messages) are written in `[project] language`.
- A skill names a contract heading as `heading:<key>`: the key in `.lighthouse/headings.toml`. Write the text `python3 .lighthouse/lib/lighthouse_lib/headings.py <key>` prints, never the literal `heading:<key>`; reading accepts every language variant.

Skills:

- `/lighthouse-start-issue`: pick up an issue.
- `/lighthouse-route-issue`: read-only triage, one route per issue.
- `/lighthouse-write-issue`: write or edit an issue or milestone.
- `/lighthouse-verify-pr`: check the acceptance criteria, write the PR body.
- `/lighthouse-fix-pr`: make a red or conflicting PR green.
- `/lighthouse-close-issue`: close an issue with evidence, or reopen it.
- `/lighthouse-handoff`: the start command for the next session.
- `/lighthouse-spec-adversarial-review`: a spec reviewed by another model family.
- `/lighthouse-plan-adversarial-review`: a plan reviewed by another model family.
- `/lighthouse-worktree-audit`: which worktrees and branches can go.
- `/lighthouse-instrument`: on which layer a new instruction belongs.
