## What and why

<!-- The change in a few sentences. Explain the "why" before the "what". -->

## Changes

<!-- Per file or area. Anything found and fixed along the way gets its own bullet. -->

## Related issues

<!-- Written by /lighthouse-write-pr. One of three forms:
     `Refs #123 — <reason>`, with a `### Evidence #123` table (| AC | Command | Result |) of one
       row per criterion proven on this branch, and below it each open criterion with what is
       still needed (a deploy, a measurement over days, a test that cannot run in CI);
       /lighthouse-close-issue runs the rows after the merge and closes the issue;
     `Closes #123` + a `### Evidence #123` table with one row per acceptance criterion, only
       when a person opens the PR without /lighthouse-write-pr: the merge closes the issue;
     `No issue: <reason>`.
     Give the PR the milestone of that issue (`gh pr create --repo <owner/repo> --milestone "<title>"`).
     No issue, no milestone. -->

## Open review points

<!-- Written by /lighthouse-write-pr from `lighthouse plan-review-record pr-open-points` when a spec or plan
     on this branch ends in an `Open after review` section (an adversarial review that hit its
     round limit). Delete this section otherwise. -->

## Before / After

<!-- Behavior a user or agent notices. Delete this section for a pure refactor or docs change. -->

## Verification

<!-- Commands you ran, with their raw result: test counts, linter exit codes, a spot check beyond
     the tests. Name what could not be measured from here. -->
