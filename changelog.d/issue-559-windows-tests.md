---
type: fixed
---
On native Windows the state database and the forge cache open again: the SQLite `file:` URI put the drive letter in the authority (`invalid uri authority: C:%5CUsers…`), so `serve`, `tick`, `run` and every command that reads the store failed. The path now goes in as `/C:/…` (#559).

The usage gate's keys no longer hold a `:`, which Windows refuses in a file name: every `:` of the RFC 3339 time is a `-` (`venster-2026-09-06T12-30-00Z.tool.done`). A key written by an older version is not found, so the gate may speak once more in a window that was open during the upgrade.
