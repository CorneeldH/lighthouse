---
type: internal
---
De Go-tests van `checkpointscan` en de clone-check in `cli` vergelijken met het opgeloste tijdelijke pad, zoals git het meldt. Daarmee slaagt `macos / go-test` in de release-workflow weer; `/var` is op macOS een symlink naar `/private/var`.
