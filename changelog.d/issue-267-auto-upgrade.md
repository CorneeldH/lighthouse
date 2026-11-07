---
type: added
---
`lighthouse self-update` replaces the binary with the newest release when it is newer and its checksum matches; `deploy/systemd/lighthouse-upgrade.timer` runs it daily and restarts `lighthouse.service`.
