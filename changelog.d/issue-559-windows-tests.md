---
type: fixed
---
On native Windows the state database and the forge cache open again: the SQLite `file:` URI put the drive letter in the authority (`invalid uri authority: C:%5CUsers…`), so `serve`, `tick`, `run` and every command that reads the store failed. The path now goes in as `/C:/…` (#559).
