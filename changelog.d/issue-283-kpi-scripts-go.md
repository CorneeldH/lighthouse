---
type: added
---
`lighthouse measure-start-issue-kpi` and `lighthouse measure-fix-pr-kpi` replace `.lighthouse/scripts/measure_start_issue_kpi.py` and `measure_fix_pr_kpi.py`, with the same metrics, flags and JSON output. `lighthouse init --upgrade` removes the two scripts; update the `lighthouse` binary on your PATH before you run the measurements in the `start-issue` and `fix-pr` skills.
