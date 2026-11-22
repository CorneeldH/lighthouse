---
type: added
---
A component can now be set to the stand `log` in `[components]`: its Go hook runs but never blocks and never speaks, and writes one line per would-be report to a logbook outside the repository. `lighthouse level` shows the setup level of a repository, the stand per component, how often each component on `log` would have reported since the level was set, and which hooks of the next level are not measured yet. It never claims the next level is within reach.
