---
type: fixed
---
Git paths (`--show-toplevel`, `--git-common-dir`, `--git-dir`, `worktree list`) are converted to the operating system's native form in the hooks, `init`, the worktree audit and the other read sites, so they compare correctly on Windows and messages show native paths (#565).
