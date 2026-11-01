---
type: internal
---
Lighthouse records its agent sessions with Entire; checkpoints go to a private repo, never to the public origin. A pre-push hook (`tools/hooks/install.sh`) scans every push's new commits with gitleaks and keeps Entire's refs off any other remote, and CI scans each PR's commits with the same `.gitleaks.toml`.
