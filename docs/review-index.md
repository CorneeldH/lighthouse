# review-index

`lighthouse review-index` links the findings of the adversarial plan and spec reviews, and the
rulings of the task reviews of an SDD run, to the trajectory (repo and issue) they belong to. It
writes rows to `findings` in `lighthouse.db`; it never copies the content of a record or ledger
(path, hash, component, key, severity and decision only).

```
lighthouse review-index [--ledger <path>]... [--issue <n>]
```

Output: `indexed: new=<n> updated=<n> unchanged=<n> removed=<n>`, then one
`skipped: <path>: <reason>` per item that was not indexed. Exit 0 (also with skipped items), 1 on a
database or read error, 2 on wrong usage. A missing `<data>/{plan,spec}-review` counts as empty.

## Review records

`plan-review-record write` runs the same indexing over the whole data directory after it has
written its record; a failure there is one `REVIEW-INDEX FAILED: <error>` line on stderr and never
changes the exit code, since the record is the product. `review-index` is the catch-up run, and
recognises a `record.json` that changed afterwards (same path, other hash: the rows are updated,
not doubled).

- **Trajectory.** The record (schema 6) carries `trajectory`: `null`, or `{repo, issue}`. The issue
  is `--issue` of `write` (the issue stands in for the spec), else the number in the current
  branch (`issue-<n>`); the repo is `forge.repo` from `lighthouse.toml`. A record with
  `trajectory: null`, or of schema 5 or lower, is not indexed and shows up as `skipped` with its
  reason.
- **Newest attempt per chain.** Per chain the attempt with the greatest name counts. If it has
  only `attempt.json` (still `.<attempt>.tmp/`, or a failed `write`), the chain counts as not
  succeeded: its older attempts lose their findings and none are added.
- **Decision.** `accept`, `spec`, `minor_yes` become `adopted`; `reject`, `minor_no` become
  `rejected`; `accepted_open`, `defer` become `left_open`; no decision (only `aborted`) stays empty.

## SDD ledger

`--ledger <path>` reads the `Task <n>: ... Ruling: ...` lines of an SDD ledger as `task_review`
findings (severity empty). `Task <n>: parked ...` is `rejected` (the code stays), any other ruling
is `left_open`; a `Ruling:` line without a `Task <n>:` prefix is skipped. The ledger belongs to
the issue of `--issue`, else the `issue-<n>` in the plan path on its first line
(`# SDD ledger — plan: <path>`). Without either, it is skipped with a reason.

The ledger has no fixed format in this repo (it comes from the external superpowers skill
`subagent-driven-development`) and is removed at the end of the SDD run: run `review-index
--ledger` before that cleanup. Calling it automatically from the skill is not part of this change.
