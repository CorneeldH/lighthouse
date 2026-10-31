## Lighthouse: python

- Python code follows `/lighthouse-python-standard`.
- Two hooks check it: `ruff-format-check` after every edit (it blocks on a finding), `ty-gate` when the session stops.
- Before a PR, when the repository has a `quality-baseline.toml`: `python3 .lighthouse/scripts/quality/ratchet.py ruff` and `python3 .lighthouse/scripts/quality/ratchet.py coverage`. A rising counter is red.
