---
type: fixed
---
A controller run in a container that mounts the controller state but not the host's `~/.cache/lighthouse` now parks when the usage window is full. The meter also writes the usage alarm next to the pause flag, and in a controller run the usage gate and `lighthouse usage alarm` read that copy when their own alarm is missing.
