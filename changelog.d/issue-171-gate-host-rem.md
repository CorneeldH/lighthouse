---
type: changed
---
The test gate waits, within its deadline, while the host has less `MemAvailable` or free swap than `gate_min_mem_available_gb` and `gate_min_swap_free_gb` in `host.toml`, `gate_slots` caps the gate slots per host, and lighthouse's own gate passes `--basetemp` to pytest.
