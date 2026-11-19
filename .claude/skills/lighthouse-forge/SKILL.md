---
name: lighthouse-forge
description: 'Reference of what differs per forge (GitHub, GitLab): board status, blockers, sub-issues, closing keywords, the proxy quirk and what closing an issue needs, one file per forge with the same sections. Read only via another skill that names a section (lighthouse-start-issue, lighthouse-route-issue, lighthouse-write-issue, lighthouse-write-pr, lighthouse-close-issue); never a starting point of its own. NOTE — not for the operations both forges share: those are `lighthouse forge` subcommands, named in the calling skill.'
metadata:
  lighthouse: "core@dev"
  binding: default
  origin: own
---

# Forge reference

What a chain skill cannot say once for every forge. The calling skill names a section, for
example "§blockers"; this skill only picks the file:

```bash
KIND=$(lighthouse config forge.kind) || exit 2
```

| `forge.kind` | File |
|---|---|
| `github` | [github.md](github.md) |
| `gitlab` | [gitlab.md](gitlab.md) |

From another installed skill the path is `../lighthouse-forge/<kind>.md`, the same under
`.claude/skills/` and `.agents/skills/`. Read only the named section.

## What lives where

- **`lighthouse forge`** covers what both forges do the same: `issue view|list|search|create|edit|comment`,
  `issue close|reopen <nr> [--body-file F]` (the comment first, then the state), `issue blockers`,
  `issue blocking` (the issues it blocks), `issue changes` (its state, `closer` and the PRs around
  it), `issue block <nr> --by <blocker>`, `issue status <nr> <status>`,
  `pr create|view|comment` and `pr list [--state open|merged|closed|all]` (a merge request on
  GitLab), `milestone list|view|create|edit` (by title; `edit --state closed` closes one),
  `label list`. One JSON shape on both forges; `--field <path>` prints one value of a read
  command. Exit 0 is done, 1 a forge error with the API's message, 2 a usage error or missing
  config.
- **The references** carry only what differs: the board's status field, the tier limits of
  blockers, sub-issues, the closing keywords, the proxy quirk and what closing an issue needs
  (§close-issue). Where no shared command exists,
  they use `lighthouse forge api`.

## `lighthouse forge api`

The escape hatch, not an abstraction. The path and the JSON are the forge's own; the wrapper adds
only the token, the host and pagination (a GET that answers a list returns all pages as one
list).

- `-X <method>`: default GET without fields, POST with them.
- `-f name=text`: a text field. `-F name=value`: a typed field (a number, `true`, `false`, `null`,
  or `@<file>` for the contents of a file). Fields of a GET go in the query string, the others in
  the JSON body. No nested fields: GraphQL variables are written into the query itself.
- No other flag: no `--field`, `--jq`, `--method` or `--paginate`. Pipe the output to `jq`.
- GraphQL goes through the forge's own path (`graphql` on GitHub, `api/graphql` on GitLab), with
  the query in a file (`-F query=@<file>`): write it with your write tool, never as a shell
  argument.
- An Evidence row of `lighthouse evidence` allows `api` only as a GET without fields, never a
  `graphql` path, a `..` or a `%` in the path.
