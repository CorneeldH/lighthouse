## Lighthouse: python

- Python code follows `/lighthouse-python-writing`.
- Two gates check it: `ruff-format` after every edit (it blocks on a finding), `ty-check` when the session stops.
- Before a PR: `python3 .lighthouse/scripts/quality/ratchet.py ruff` and `python3 .lighthouse/scripts/quality/ratchet.py coverage`. A rising counter is red.
