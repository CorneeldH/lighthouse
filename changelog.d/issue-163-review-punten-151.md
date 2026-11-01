---
type: changed
---
A pull request that touches only `CONTRIBUTING.md`, `SECURITY.md` or `CLA.md` no longer starts the Go jobs in CI, and each CI pytest part names itself in the pytest header (`LIGHTHOUSE_PYTEST_SHARD=2/3`), also under `-n auto`.
