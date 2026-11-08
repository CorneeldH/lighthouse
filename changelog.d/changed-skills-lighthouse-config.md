---
type: changed
---
Skills and `AGENTS.md` read `lighthouse.toml` and the headings through `lighthouse config` and `lighthouse headings` instead of `config.py` and `headings.py`, and the `python` pack's `ratchet.py` and `prcov_issues.py` call the binary too. Update the `lighthouse` binary on your PATH before you run a skill: an older binary does not know `config` and the skill reads every key as unset.
CI jobs that run `ratchet.py` (`pack/python`) must now install the `lighthouse` binary, since it reads its config through `lighthouse config --json`.
