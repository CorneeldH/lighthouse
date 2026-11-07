---
type: added
---
`forge.kind = "gitlab"` is valid in `lighthouse.toml`, with repository paths that have subgroups (`group/subgroup/name`) and an optional `forge.host` for a self-hosted GitLab. `lighthouse init` recognises GitLab remotes (https, scp and `ssh://`, gitlab.com and self-hosted via `--forge-host`) and proposes `[forge]`. There is no GitLab client yet.
