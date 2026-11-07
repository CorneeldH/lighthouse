---
type: fixed
---
The shell package verifies its pinned `shellcheck` download with `shasum -a 256` when `sha256sum` is missing, as on macOS. CI now runs the Go and Python tests on macOS as well.
