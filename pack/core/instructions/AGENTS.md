## Lighthouse: core

- Nothing lands directly on `main`. Work on a branch in a worktree; `main-commit-gate` blocks `git commit` and `git push` to `main`.
- Escape hatch, deliberately and once: `touch .claude/allow-main` in the repo root; remove it afterwards.
- A worktree before the first write: `git worktree add .claude/worktrees/<name> -b <branch>`.
- Every change goes through a PR and is squash-merged. `/lighthouse-write-pr` writes the PR body; `pr-create-gate` checks it.
- Test gate: `commit-test-gate` runs `[test_gate] command` after each commit, on its files (default branch: before). Fix red in a new commit; never `--no-verify`. <!--level:commit-test-gate-->
- Update a branch with `origin/<default branch>` by `git merge` once it is on origin, never by rebase plus force-push; rebase only a branch that is not pushed yet. Every PR is squash-merged, so the merge commit never reaches the default branch.
- Artefacts (issues, PRs, comments, commit messages) are written in `[project] language`.
- With `[project] code_language` set, names, comments, docstrings, file names and everything under `code_language_paths` are English.
- Finish what you are already authorised to do; never end on an offer to do it ("ready to push if you want" is not done).
- `heading:<key>` in a skill names a contract heading in `.lighthouse/headings.toml`. Write what `lighthouse headings <key>` prints, never the literal `heading:<key>`; reading accepts every language variant.
- `lighthouse config` exits 1 with `unknown command` on stderr: the binary is too old. Stop and say so; that is not "not set".

Skills:

- `/lighthouse-start-issue`: pick up an issue. <!--level:skill:start-issue-->
- `/lighthouse-route-issue`: read-only triage, one route per issue. <!--level:skill:route-issue-->
- `/lighthouse-write-issue`: write or edit issues and milestones. <!--level:skill:write-issue-->
- `/lighthouse-write-pr`: check acceptance criteria, write the PR body. <!--level:skill:write-pr-->
- `/lighthouse-fix-pr`: make a red or conflicting PR green. <!--level:skill:fix-pr-->
- `/lighthouse-forge`: per-forge differences, read through another skill. <!--level:skill:forge-->
- `/lighthouse-close-issue`: close an issue with evidence, or reopen it. <!--level:skill:close-issue-->
- `/lighthouse-write-handoff`: start command for the next session. <!--level:skill:write-handoff-->
- `/lighthouse-review-spec`: a spec reviewed by another model family. <!--level:skill:review-spec-->
- `/lighthouse-review-plan`: a plan reviewed by another model family. <!--level:skill:review-plan-->
- `/lighthouse-audit-agent-config`: audit the agentic configuration. <!--level:skill:audit-agent-config-->
- `/lighthouse-audit-worktrees`: which worktrees and branches can go. <!--level:skill:audit-worktrees-->
- `/lighthouse-place-instruction`: on which layer a new instruction belongs. <!--level:skill:place-instruction-->
- `/lighthouse-test-standard`: tests that fail when behaviour breaks. <!--level:skill:test-standard-->
- `/lighthouse-upgrade`: move to the installed lighthouse version. <!--level:skill:upgrade-->
