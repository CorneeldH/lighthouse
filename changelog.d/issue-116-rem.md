---
type: added
---
`lighthouse tick` checks one shared brake on the host before every start or resume: the pause flag, the usage window, MemAvailable, slots and software runs, with the reason in the deferred line. `lighthouse pause [--until HH:MM]` and `lighthouse pause off` set and clear the pause flag, and a controller run the usage guard parked resumes with `ga verder` after the reset.
