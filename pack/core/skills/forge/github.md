# Forge reference: GitHub

`forge.kind = "github"`. The token is `GH_TOKEN` or `GITHUB_TOKEN` (or the variable in
`lighthouse config forge.token_env`), else `gh auth token`. In a `lighthouse forge api` path,
`{owner}` and `{repo}` are filled in from `forge.repo`.

## §board

The board is a GitHub Projects v2 project, configured in `[forge.board]`: `project_number`,
`project_id`, `status_field_id` and per status name the option ID in `status_options`. Without
`[forge.board]` there is no board: report `board: skipped — no [forge.board] configured`.

- Set a status with `lighthouse forge issue status <nr> "<status>"`; it adds the issue to the
  board first. `<status>` is a key of `[forge.board.status_options]`; another name is exit 2 and
  lists the valid ones.
- The token needs the `project` scope (`gh auth refresh -s project`); without it the status
  command fails with exit 1 and GitHub's message.
- A status outside `status_options` (an option added on the board only) reads back as
  `unknown:<option ID>`.
- Priority has no shared command. Set it in the board UI, or by hand with the item id, the field
  and the option from `[forge.board]`:

```bash
P=$(lighthouse config forge.board.project_id) || exit $?
F=$(lighthouse config forge.board.priority_field_id) || exit $?
S=$(lighthouse config forge.board.priority_options.<P0|P1|P2>) || exit $?
gh project item-edit --id <item-id> --project-id "$P" --field-id "$F" --single-select-option-id "$S"
```

The item id:
`N=$(lighthouse config forge.board.project_number) && O=$(lighthouse config forge.owner) && gh project item-list "$N" --owner "$O" --limit 1000 --format json --jq '.items[] | select(.content.number==<nr>) | .id'`.
`--limit` matters: without it `gh` reads only the first 30 items, and on a bigger board the item is
missing.

## §blockers

GitHub's issue dependencies (`dependencies/blocked_by`).

- `lighthouse forge issue block <nr> --by <blocker>` sets the relation; the wrapper looks up the
  database `id` of the blocker that GitHub wants.
- `lighthouse forge issue blockers <nr>` lists every blocker, open and closed, with `number`,
  `state`, `repo` and `merged_pr`. An empty list means there is no relation.
- The raw issue carries a count without an extra call:
  `lighthouse forge api repos/{owner}/{repo}/issues/<nr> | jq '.issue_dependencies_summary.blocked_by'`
  (open blockers). The JSON of `lighthouse forge issue view` does not.

## §sub-issues

GitHub sub-issues, through the REST API. `sub_issue_id` is the database `id` of the child, not
its number:

```bash
CHILD_ID=$(lighthouse forge api repos/{owner}/{repo}/issues/<child-nr> | jq '.id') || exit 1
lighthouse forge api -X POST repos/{owner}/{repo}/issues/<parent-nr>/sub_issues -F sub_issue_id="$CHILD_ID"
```

Verify with `lighthouse forge api repos/{owner}/{repo}/issues/<parent-nr>/sub_issues | jq '.[].number'`.

## §sluitwoorden <!-- lighthouse: allow-language — the section name chain skills cite -->

The closing keywords: a PR body closes an issue on merge with `close`, `closes`, `closed`, `fix`,
`fixes`, `fixed`, `resolve`, `resolves` or `resolved`, followed by `#N`, `<owner>/<repo>#N` or
the issue URL. Any other word (`implements`, `closing`) closes nothing; `Refs #N` is the
deliberate non-closing form.

## §proxy

`gh issue list --json` breaks under the rtk proxy (`Failed to parse gh issue list output`). Use
`lighthouse forge issue list` or `lighthouse forge issue search`, or `gh api` when you need a
`gh` call.
