## What and why

<!-- The change in a few sentences. Explain the "why" before the "what". -->

## Changes

<!-- Per file or area. Anything found and fixed along the way gets its own bullet. -->

## Related issues

<!-- Written by /paia-verify-pr. One of three forms (mixing is allowed):
     `Closes #123` + a `### Evidence #123` table with one row per acceptance criterion
       (| AC | Command | Result |), when every criterion is proven on this branch or can only be
       measured once the PR exists;
     `Refs #123 — <reason>`, when a criterion depends on something after the merge (a deploy,
       a measurement over days, a check on the Beelink);
     `No issue: <reason>`.
     Give the PR the milestone of that issue (`gh pr create --milestone "<title>"`).
     No issue, no milestone. -->

## Open review points

<!-- Written by /paia-verify-pr from `plan_review_record.py pr-open-punten` when a spec or plan
     on this branch ends in an `Open na review` section (an adversarial review that hit its
     round limit, #1352). Delete this section otherwise. -->

## Before / After

<!-- Behavior a user or agent notices. Delete this section for a pure refactor or docs change. -->

## Verification

<!-- Commands you ran, with their raw result: test counts, ty/ruff exit code, a spot check beyond
     pytest. Name what could not be measured from here. -->
