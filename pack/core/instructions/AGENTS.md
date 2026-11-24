## Lighthouse: core

- Nothing lands on `main`: work in `git worktree add "$ROOT/<name>"` with `ROOT=$(lighthouse config worktree.root)` (default `.claude/worktrees`); `main-commit-gate` blocks `main` commits and pushes.
- Every change goes through a PR and is squash-merged; `/lighthouse-write-pr` writes the PR body.
- `commit-test-gate` runs `[test_gate] command` per `[test_gate] on`; fix red in a new commit. <!--level:commit-test-gate-->
- Never `--no-verify`.
- Update a branch with `origin/<default branch>` by `git merge` once it is on origin, never by rebase plus force-push; rebase only a branch that is not pushed yet.
- Artefacts (issues, PRs, comments, commit messages) are in `[project] language`; chat too, unless `en`.
- With `[project] code_language` set, names, comments, docstrings, file names and everything under `code_language_paths` are English.
- Finish what you are already authorised to do; an offer to do it is not done.
- `heading:<key>` in a skill names a contract heading in `.lighthouse/headings.toml`. Write what `lighthouse headings <key>` prints, not the literal `heading:<key>`; reading accepts every language variant.
- `lighthouse config` exits 1 with `unknown command` on stderr: the binary is too old. Stop and say so; that is not "not set".
