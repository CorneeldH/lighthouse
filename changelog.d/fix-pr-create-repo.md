---
type: fixed
---
In a clone of a fork `gh pr create` opened the PR in the upstream repo, because `lighthouse-write-pr`
passed no `--repo`. The skill now passes `--repo` with `forge.repo`, as does every other writing
`gh` command in the skills, and `pr-create-gate` blocks a `gh pr create` without `--repo`, `-R` or
`GH_REPO` and names the exact `--repo <forge.repo>` to add.
