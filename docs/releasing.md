# Releasing

Lighthouse releases itself. A workflow picks the version, writes `CHANGELOG.md`, tags, and
builds the binaries. Nobody sets a tag by hand.

## Versions

Versions are `vMAJOR.MINOR.PATCH`. "User-visible" means the `lighthouse` binary and everything
`lighthouse init` writes into a repository (skills, agents, hooks, settings).

| Change | Bump |
| --- | --- |
| A fix only: a defect that no longer occurs | patch |
| New or changed user-visible behaviour, or something removed | minor |
| A breaking change under 0.x | minor |
| A breaking change from 1.0 on | major |

Only `workflow_dispatch` picks a major bump. A major bump to v2 or higher also needs the module
path in `go.mod` to end in `/v2` (Go semantic import versioning). Without it Go ignores the tag
when it stamps the build, and the binary reports `dev` instead of the version.

## When a release happens

- A merge to `main` whose open fragments contain a `fixed` releases at once: a patch, or a minor
  when another releasable type is open next to it.
- Every Monday at 06:00 UTC the workflow releases whatever is open: a patch for `fixed` only,
  otherwise a minor. A merge with only `added`, `changed` or `removed` waits for Monday.
- A maintainer can run the `release` workflow by hand (`workflow_dispatch`) with `bump`:
  `auto`, `patch`, `minor` or `major`. A forced bump lower than what the fragments need, or
  without a releasable fragment, fails instead of releasing.
- Fragments of type `internal` never start a release.
- Never create a tag by hand.

## Changelog fragments

Every PR adds a fragment to `changelog.d/`. A fragment is a markdown file with one `type:` line
between two `---` lines, then one to three sentences for a user, in English:

```markdown
---
type: fixed
---
`lighthouse init` no longer fails on a repository without a `docs/` directory.
```

The types:

- `added`: new behaviour a user can use.
- `changed`: existing behaviour that works differently now.
- `fixed`: a defect that no longer occurs.
- `removed`: behaviour that is gone.
- `internal`: what a user does not notice (developer docs, tests, CI). It stays out of the
  release notes and never starts a release.

The file is `changelog.d/<branch>.md`, a `/` in the branch name becoming `-`. A second fragment
on the same branch, or a name an open fragment already holds, takes `-2`, `-3` and so on:
`<branch>-2.md`. The marker `changelog.d/.lighthouse` switches the convention on; it is never
removed and never parsed. `/lighthouse-verify-pr` writes the fragment when the marker is there.

CI checks every PR: it needs at least one fragment added by the PR (an edited or renamed old
fragment does not count), and every fragment must parse. Windows line endings and a BOM are
accepted; a missing closing `---`, empty text, an unknown key or two `type:` lines fail with the
path in the message.

## The release commit

The workflow commits `CHANGELOG.md` and the removal of the released fragments to `main` as
`github-actions[bot]`, with message `chore(release): vX.Y.Z`. It tags that commit and pushes
commit and tag in one atomic push. This is the only exception to "nothing lands on `main`
directly". If `main` gets a ruleset, give the bot a bypass for it.

A refused push (someone merged in the meantime) lands nothing; the workflow fetches `main` and
retries, three times at most.

## Recovering a failed release

- The workflow failed before or during the push: nothing landed, or the fragments are still
  open. Run the `release` workflow again with `bump: auto`. The fragments are still there, so
  it picks them up.
- The tag exists but the build failed: run the `build` workflow by hand (`workflow_dispatch`)
  with that tag. It builds the five binaries again. If the GitHub release exists, it uploads the
  files with `--clobber`; otherwise it creates the release. The notes of an existing release
  stay as they are. To change them, edit the release notes on GitHub by hand. The run still
  needs a `CHANGELOG.md` section for the tag, and fails without one.

## Upgrading

1. Download the new binary (see [Installing the server](install.md#download-a-release-binary))
   and replace the old one.
2. In every repository that uses lighthouse, run `lighthouse init --upgrade`.
3. Commit the installed copy and send it as a PR; it contains only what `--upgrade` changed.
