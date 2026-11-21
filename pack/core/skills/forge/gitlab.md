# Forge reference: GitLab

`forge.kind = "gitlab"`, on gitlab.com unless `lighthouse config forge.host` names another
server. The token is `GITLAB_TOKEN` (or the variable in `lighthouse config forge.token_env`).
The wrapper talks to the GitLab API itself and does not depend on the glab CLI; nothing in the
chain needs it. A `lighthouse forge api` path is relative to `/api/v4`; a project in it is its
numeric id or its URL-encoded path (`group%2Fproject`). An Evidence row refuses a `%`, so pin
evidence with the shared read commands instead.

A pull request is a merge request here: `lighthouse forge pr …` creates and reads MRs, and
`--milestone` takes a title that may be a project or a group milestone. `milestone edit` refuses
a group milestone (exit 1); change that one with `lighthouse forge api` on
`groups/<id>/milestones/<id>`.

## §board

GitLab's built-in work item status, the one the issue list and the boards show. There is no board
project and no status label; every issue is on the board.

- `[forge.board] status_options` maps each lighthouse status name to a GitLab status name. GitLab
  matches that name case-insensitively against the status lifecycle of the top-level group.
  `project_number`, `project_id` and `status_field_id` do not apply.
- Set a status with `lighthouse forge issue status <nr> "<status>"`. A name outside
  `status_options` is exit 2. A GitLab status missing from the lifecycle is exit 1; it is added on
  the top-level group under Settings → Work items → Statuses.
- The status widget comes with GitLab's paid tiers; where it is missing, the status command fails
  with exit 1 and GitLab's message. Report that line and go on.
- A GitLab status that `status_options` does not map reads back as `unknown:<GitLab status name>`.
- Priority is a label: `[forge.board] priority_options` maps `P0`, `P1`, `P2` to label names. Set
  it with `lighthouse forge issue edit <nr> --add-label <label> --remove-label <other label>`.

## §blockers

Issue links of type `is_blocked_by`, available on Premium and Ultimate only.

- `lighthouse forge issue block <nr> --by <blocker>` sets the link. On Free GitLab refuses it: exit
  1 with GitLab's message. Then mark the block by hand instead: add `labels.blocked_issue` with
  `lighthouse forge issue edit <nr> --add-label <labels.blocked_issue>`, and put a line
  `Blocked by #<blocker>` in the body. `--body-file` replaces the whole body: read the current one
  (`lighthouse forge issue view <nr> --field body`), write it with that line added to the file,
  then `lighthouse forge issue edit <nr> --body-file <file>`.
- `lighthouse forge issue blockers <nr>` lists every blocker, open and closed, with `number`,
  `state`, `repo` (also another project) and `merged_pr`. On Free it gives an empty list without
  an error, so an empty list does not prove there is no blocker.
- Reading a block (route-issue): with `labels.blocked_issue` on the issue and an empty
  `issue blockers` list, read the blockers from the `Blocked by #<n>` lines in the body and check
  each with `lighthouse forge issue view <n> --field state`. Only with no such line is the label
  **unlinked**.
- The issue JSON carries no blocker count; `issue blockers` is the only source.

## §sub-issues

A sub-issue is a task (work item type Task) under an issue; an issue cannot be the child of an
issue. Epics, the level above an issue, need Premium. The hierarchy has no REST call, so it goes
through GraphQL: write each query with your write tool to a file, then
`lighthouse forge api api/graphql -F query=@<file>`, and check `.errors` and the mutation's own
`errors` in the answer. These queries follow GitLab's GraphQL reference but have not yet run
against a live GitLab: on an error, trust the message over the query below.

The global ids of the parent and a child, and the id of the Task type:

```graphql
query {
  project(fullPath: "<forge.repo>") {
    parent: workItems(iid: "<parent-nr>", first: 1) { nodes { id } }
    child: workItems(iid: "<child-nr>", first: 1) { nodes { id workItemType { name } } }
    workItemTypes(name: TASK) { nodes { id } }
  }
}
```

A new task under the issue:

```graphql
mutation {
  workItemCreate(input: {namespacePath: "<forge.repo>", title: "<title>",
    workItemTypeId: "<task type id>", hierarchyWidget: {parentId: "<parent id>"}}) {
    workItem { iid webUrl }
    errors
  }
}
```

An existing task under the issue:

```graphql
mutation {
  workItemUpdate(input: {id: "<child id>", hierarchyWidget: {parentId: "<parent id>"}}) {
    errors
  }
}
```

Verify:

```graphql
query {
  project(fullPath: "<forge.repo>") {
    workItems(iid: "<parent-nr>", first: 1) {
      nodes { widgets { ... on WorkItemWidgetHierarchy { children { nodes { iid title state } } } } }
    }
  }
}
```

## §sluitwoorden <!-- lighthouse: allow-language — the section name chain skills cite -->

The closing keywords: an MR body closes an issue when the MR merges into the default branch. The
words are the GitHub ones plus GitLab's own: `close`, `closes`, `closed`, `closing`, `fix`,
`fixes`, `fixed`, `fixing`, `resolve`, `resolves`, `resolved`, `resolving`, `implement`,
`implements`, `implemented`, `implementing`. The target is `#N`, `<group>/<project>#N` (subgroups
included) or `https://<host>/<group>/<project>/-/issues/N`. A list closes every issue in it:
`Closes #1, #2 and #3`. `Refs #N` is the deliberate non-closing form.

## §proxy

Not applicable: the rtk proxy breaks only the `gh` CLI's JSON output, and no step on GitLab calls
`gh`. Use `lighthouse forge`.

## §ci

CI actions that have no shared command. `<project>` is `forge.repo`, URL-encoded (`/` as `%2F`).
The jobs of a pipeline:

```bash
lighthouse forge api "projects/<project>/pipelines/<id>/jobs"
```

Jobs of a child pipeline (a bridge) are not listed by `pr checks`: when a pipeline is red but no job failed, check `lighthouse forge api "projects/<project>/pipelines/<id>/bridges"`.

Rerun one job: `lighthouse forge api -X POST "projects/<project>/jobs/<job>/retry"`. Roborev does
not exist on GitLab yet (it first needs a real run), so section 6 of `/lighthouse-fix-pr` does not apply.

## §workflows

No equivalent in this milestone. A GitLab repo does not run the workflows, and `lighthouse init`
does not write them.
