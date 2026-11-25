# Releasing

Lighthouse releases itself, but not on every merge. A release follows a finished milestone, after
its code has run for seven days on the dev channel. Workflows pick the versions, write
`CHANGELOG.md`, tag and build the binaries. Every tag is made by a workflow: dev and release
candidate tags by `dev-build.yml` and `release-train.yml` (through `prerelease.yml`), release tags
by `release.yml`, test tags by hand through `prerelease.yml` (see
[Testing the build](#testing-the-build)). Never create a `vX.Y.Z` tag by hand.

## Channels

| Channel | Tags | Who |
| --- | --- | --- |
| `release` (the default) | `vX.Y.Z` | everyone |
| `dev` | `vX.Y.Z-dev.N`, `vX.Y.Z-rc.N` and `vX.Y.Z` | the maintainer's own repositories, and anyone who wants to test along |

`lighthouse self-update --channel dev` stores the dev channel in `~/.config/lighthouse/channel`;
`--channel release` goes back, and `lighthouse channel` prints the current one. Without a stored
choice the channel is `release`. Switching from `dev` to `release` never downgrades: you stay on
your higher dev version until a release catches up. Dev releases are never deleted, because
`manifest.toml` of a repository names the binary version that its workflows download.

The tags sort by semver: `v0.28.0-dev.5 < v0.28.0-rc.1 < v0.28.0 < v0.29.0-dev.1`. `X.Y.Z` of a
dev tag is the version of the next milestone release: the minor after the highest open
`release/vX.Y` branch, or after the last release when no branch is open. A release branch is open
until its `vX.Y.0` tag exists. `N` is one above the highest existing `N` for that `X.Y.Z` and
suffix. A test build from `prerelease.yml` is `vX.Y.Z-test.N` and counts apart.

## Versions

Versions are `vMAJOR.MINOR.PATCH`. "User-visible" means the `lighthouse` binary and everything
`lighthouse init` writes into a repository (skills, agents, hooks, settings).

| Release | Version |
| --- | --- |
| A finished release milestone | minor (also for a breaking change under 0.x) |
| A critical fix on a released line | the next patch of that line |
| A breaking change from 1.0 on | major, chosen by hand when the branch is cut |

Only the dispatch of the split picks a major. A major bump to v2 or higher also needs the module
path in `go.mod` to end in `/v2` (Go semantic import versioning). Without it Go ignores the tag
when it stamps the build, and the binary reports `dev` instead of the version.

## The flow

```text
main ──merge──merge──merge──(milestone closed)──merge──merge──…
        │      │      │            │
     dev.1  dev.2  dev.3           └─ release/v0.28 ── rc.1 ──(7 days)── release PR ──merge── v0.28.0
                                                         │
                                       fix: PR on main, cherry-pick PR to release/v0.28 ── rc.2 (clock restarts)
```

1. **Dev build.** A push to `main` that adds at least one fragment of type `added`, `changed`,
   `fixed` or `removed` makes `vX.Y.Z-dev.N` (`dev-build.yml`, which calls `prerelease.yml`). A
   merge with only `internal` fragments makes none. All dev builds share one concurrency group, so
   two merges close together never get the same tag.
2. **Cutting the branch.** Closing a milestone whose description has a line `Release: ja` starts
   the `release-train` workflow, which cuts `release/vX.Y` off `main` and tags `vX.Y.0-rc.1`.
   Closing any other milestone does nothing. With no releasable fragment that is not in a release
   branch yet, nothing is cut and the run says so. While the previous rc has no release yet,
   `main` is merged into that branch and the next rc follows (`rc.N+1`). The `release-train`
   dispatch with `action: split` does the same without a milestone, with a choice of `minor` or
   `major`.
3. **Soak.** The maintainer's repositories run the dev channel. It picks the highest version, so
   after the first merge following the cut that is `vX.Y+1.0-dev.N`, not the rc itself: `main`
   holds the rc, so whoever soaks `main` soaks the rc too. A bug is fixed by a PR on `main` (with a
   `fixed` fragment) and then a cherry-pick PR to `release/vX.Y`. The merge of that PR makes the
   next rc and the seven days start again.
4. **Release PR.** Every hour the train looks at each release branch. When its newest rc is seven
   days old and no release PR is open, it opens one on `release/vX.Y`: `chore(release): vX.Y.0`,
   with the new `CHANGELOG.md` section and the removal of the released fragments. A new rc closes
   an open release PR. If you close it yourself without merging, no new one opens until there is a
   new rc. A fragment that an earlier release shipped is not taken again: `ci/changelog` reads
   which fragments the `chore(release): vX.Y.Z` commits behind the `vX.Y.Z` tags removed, from git.
5. **Release.** Merge the release PR. The next run of the train sees a tip that is a
   `chore(release)` commit without a tag and calls `release.yml`: the macOS gate on that commit,
   the `dist` commit, the tag `vX.Y.0` and `build.yml`. For `vX.Y.0` the workflow refuses the tag
   when the parent of the release commit is not the parent of the `dist` commit of the newest rc
   (`vX.Y.0-rc.N^`): then something was merged after the soak. A patch release has no rc and no
   such check.
6. **Sync to `main`.** A step of its own after the tag, with its own retry. It puts the section of
   the tag into `CHANGELOG.md` on `main` and removes there the fragments the release commit removed.
   Fragments for the next milestone stay. If `main` is already up to date it changes nothing.

The logic always runs from the workflows and the `ci/changelog` code of `main`, also for what
happens on a release branch. A push to `release/*` triggers nothing; the frozen copy of the
workflows on a release branch is never used.

## A critical fix

A fix that cannot wait for the next milestone ships alone:

1. The fix lands on `main` by a PR with a `fixed` fragment, so the next milestone release does not
   lose it.
2. A cherry-pick PR takes the commit, fragment included, to the release branch of the last release
   (`release/vX.Y`).
3. After that merge the train opens a patch release PR at once, `chore(release): vX.Y.Z+1`, with no
   seven days of waiting. Merge it, and the release follows as in step 5.
4. The sync then removes the fragment on `main` and puts the patch section into `CHANGELOG.md`, so
   the next milestone release does not name the fix again.
5. If another rc is in its soak for the next minor, the fix goes there by the same road: a
   cherry-pick PR to that branch.

## The macOS gate

A PR to `release/*` runs `macos.yml` (the pytest job of `pack/python`, the Go race tests and the
e2e tests on `macos-latest`), so you see the result before you merge a release PR. The hard gate is
in `release.yml`: no tag without a green `macos` run on the merged commit. Dev and rc builds do not
wait for macOS. A pull request to `main` runs no macOS job (#337).

- macOS is red: no tag; the branch stays as it is. Choose *Re-run failed jobs* on the run, or start
  the release again (see [Recovering a step](#recovering-a-step)).
- To test a branch on macOS before its merge, run `macos.yml` by hand (`workflow_dispatch`) on
  that branch.

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
  release notes and never starts a dev build.

The file is `changelog.d/<branch>.md`, a `/` in the branch name becoming `-`. A second fragment
on the same branch, or a name an open fragment already holds, takes `-2`, `-3` and so on:
`<branch>-2.md`. The marker `changelog.d/.lighthouse` switches the convention on; it is never
removed and never parsed. `/lighthouse-write-pr` writes the fragment when the marker is there.

CI checks every PR: it needs at least one fragment added by the PR (an edited or renamed old
fragment does not count), and every fragment must parse. Windows line endings and a BOM are
accepted; a missing closing `---`, empty text, an unknown key or two `type:` lines fail with the
path in the message.

## The release commit

The workflow makes two commits. The first commits `CHANGELOG.md` and the removal of the released
fragments to `main` as `github-actions[bot]`, with message `chore(release): vX.Y.Z`. The second
sits on top of it and adds the built web UI (`internal/web/dist`), so that `go install` of the
tag serves the UI. The tag points at the second commit, which `main` never gets: the tag is not
an ancestor of `main`. The workflow pushes `main` and the tag in one atomic push. This is the
only exception to "nothing lands on `main` directly". If `main` gets a ruleset, give the bot a
bypass for it.

A refused push lands nothing. If someone merged in the meantime, the run ends as in "main moved"
under [The macOS gate](#the-macos-gate): that newer commit has its own run. Otherwise (a network
error) the workflow fetches `main` and retries, three times at most.

## Recovering a failed release

- The workflow failed before or during the push: nothing landed, or the fragments are still
  open. If `main` is still at the run's commit, choose *Re-run failed jobs* on the failed run
  first: it keeps the event, so a failed push run follows the push rule again, and it runs the
  failed macOS jobs again too. Otherwise, or if that fails, dispatch the `release` workflow with
  `bump: auto` from `main`. A dispatch follows the schedule rule: with only `added` open it releases a minor
  at once, where the failed push run would have waited for Monday.
- The tag exists but the build failed: run the `build` workflow by hand (`workflow_dispatch`)
  with that tag. It builds the five binaries again. If the GitHub release exists, it uploads the
  files with `--clobber`; otherwise it creates the release. The notes of an existing release
  stay as they are. To change them, edit the release notes on GitHub by hand. The run still
  needs a `CHANGELOG.md` section for the tag, and for a non-prerelease tag it fails without one.
- The `web` check of `build` fails on a tag that is already pushed (its `dist` is missing or does
  not match `web/`): fix the cause, then release a new patch version. Never move or re-set the
  tag, also not when `build` later runs again and its output differs, for instance because of a
  newer runner image. Running `build` again only works for tags made with the `dist` commit.

## The release commit

The release PR is squash-merged, so the tip of the release branch is the commit
`chore(release): vX.Y.Z (#N)` (C1) with `CHANGELOG.md` and the removal of the released fragments.
`release.yml` adds a second commit on top of it with the built web UI (`internal/web/dist`), so
that `go install` of the tag serves the UI. The tag points at that second commit (C2), which no
branch ever gets. The workflow pushes the tag together with the unchanged tip of the branch in one
atomic push: if the branch moved in the meantime, nothing lands. A refused push is retried, three
times at most. The only direct pushes of the bot are the tags and the sync to `main`; if `main`
gets a ruleset, give the bot a bypass for the sync.

## Recovering a step

- **The merged release PR got no tag** (macOS red, the job died). Choose *Re-run failed jobs* on the
  run, or run the `release` workflow by hand (`workflow_dispatch`) from `main` with `branch:
  release/vX.Y`. It repeats steps 5 and 6 on the tip of that branch. The train skips a branch whose
  tip is a `chore(release)` commit without a tag; it never opens a second release PR there. An
  action `release` of `release-train` does the same.
- **The tag exists but the build failed.** Run the `build` workflow by hand (`workflow_dispatch`)
  with that tag. It builds the five binaries again. If the GitHub release exists, it uploads the
  files with `--clobber`; otherwise it creates the release. The notes of an existing release stay
  as they are; to change them, edit the release notes on GitHub by hand. The run still needs a
  `CHANGELOG.md` section for the tag, and for a non-prerelease tag it fails without one.
- **The sync to `main` failed.** The tag stays. Run the `release` workflow again with the branch:
  the tag exists, so it makes no new one and only the sync runs. A `main` that is already up to
  date stays as it is.
- **The `web` check of `build` fails on a tag that is already pushed** (its `dist` is missing or does
  not match `web/`): fix the cause, then release a new patch version. Never move or re-set the
  tag, also not when `build` later runs again and its output differs, for instance because of a
  newer runner image. Running `build` again only works for tags made with the `dist` commit.
- **Merging `main` into the release branch conflicts** when a new rc is cut. The run fails with the
  conflict named; merge `main` into the branch by hand, and the next run tags the rc.

## Moving to this policy

At the moment these workflows become active, the open fragments on `main` stay where they are. The
first dev tag is `vX.Y.Z-dev.1` with `X.Y.Z` the minor after the last release, and those fragments
go with the first release milestone.

## Testing the build

To test `build.yml` without a release, run the `prerelease` workflow (`workflow_dispatch`), from
the branch you want to test. Leave the tag empty: the workflow counts the next `vX.Y.Z-test.N`
itself, so the test build never touches the numbering of the dev builds or the release candidates.
A tag you type must be a `-test.N` tag too: `-dev` and `-rc` tags belong to the workflows. It
builds the UI, adds the `dist` commit and pushes the tag. The build needs no `CHANGELOG.md` section for it: the release gets a
fixed note and `--prerelease`. Delete it afterwards with
`gh release delete <tag> --cleanup-tag --yes`. A prerelease tag pushed by hand has no `dist`, so
`build.yml` fails on it.

## Upgrading

1. Download the new binary (see [Installing the server](install.md#download-a-release-binary))
   and replace the old one. `lighthouse-upgrade.timer` does this when it is on (see
   [Upgrade automatically](install.md#upgrade-automatically)).
2. If you run lighthouse as a user service, restart it: `systemctl --user restart lighthouse`.
   The timer restarts it after a replacement (see
   [Upgrade automatically](install.md#upgrade-automatically)).
3. In every repository that uses lighthouse, run `/lighthouse-upgrade`: it runs
   `lighthouse init --upgrade` in a worktree, checks it, and commits only what it changed. By hand:
   `lighthouse init --upgrade`, then commit the installed copy and send it as a PR.
