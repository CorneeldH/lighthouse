---
type: added
---
`lighthouse serve` has a Telemetry tab: per component (plan and spec review, lane, model, harness) a comparison with its label (*Observed, not caused* or *Assigned by lighthouse*), a weekly timeline of cost, questions and recovery, a sortable list of trajectories with a detail view, and a coverage block that names every gap as *not measured* with its reason. `review-index` now records every review run in `review_runs`, also without findings, and `measure-recovery` records the route, area and PR size of each trajectory (#379).
