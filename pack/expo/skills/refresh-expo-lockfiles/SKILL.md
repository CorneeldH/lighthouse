---
name: lighthouse-refresh-expo-lockfiles
description: Refresh the dependencies of the Expo apps in this repository so the CI check on them goes green again. Use when `npx expo-doctor` fails with "Check that packages match versions required by installed Expo SDK", or when patch-level Expo drift blocks an unrelated PR. Stays on the pinned SDK; never upgrades it.
---

# Refresh Expo lockfiles

Brings every Expo app in this repository back in line with the version table of the **already
pinned** Expo SDK. Patch numbers move; the SDK major does not.

## When to Use

- `npx expo-doctor` fails in CI with *Check that packages match versions required by installed
  Expo SDK*. The workflow that runs it lives in `.github/workflows/`, if present.
- An unrelated PR is red because it touches an Expo app or the workflow that checks the apps.
- You do not want to wait for the next Dependabot PR (see `.github/dependabot.yml`, if present).

**Not** for a jump to a new SDK major. See *Stopping point* at the end.

## Why this keeps coming back

`expo-doctor` fetches its "expected" column **live from Expo**. Every Expo patch release makes the
lockfiles fall behind without any change in the repository: the same commit is red today and was
green last month.

## The trap: do not use `npm update`

It is the first idea, and it is wrong. Dozens of packages inside the Expo tree declare
`"expo": "*"` as a peer, and `npm update` reads `*` as *take the newest*. You end up with a newer
SDK nested inside `node_modules/expo/node_modules/` while the top level stays on the pinned one.
`expo-doctor` then reports duplicate native modules (`expo-font`, `expo-keep-awake`,
`expo-modules-core`): you break the check with the command that was meant to repair it.

Use `npx expo install --fix`. It knows the SDK: it reads the version table of the installed SDK
and installs exactly those versions, without pulling in the `*` peers.

## Workflow

### 1. Worktree

This produces commits, so start in a worktree (`superpowers:using-git-worktrees`). Branch name
`fix-expo-lockfile-drift` or `issue-<nr>-expo-drift`.

### 2. Find the apps and check the tools

The apps are the directories with a `package.json` that names `"expo"`, as git tracks them. Run
this from the repository root:

```bash
git ls-files -- ':(glob)**/package.json' | while read -r f; do
  d=$(dirname "$f")
  if grep -q '"expo"' "$f" && [ -f "$d/package-lock.json" ]; then echo "$d"; fi
done
```

A directory without its own `package-lock.json` is no app for this skill: a library that names
`expo` as a peer, or a member of an npm workspace. `npm ci` needs that lockfile, so leave such a
directory out and name it in your report.

Check that `node` and `npm` exist before anything else:

```bash
command -v node && command -v npm && node -v
```

If one is missing, stop and report `refresh-expo-lockfiles: node or npm not found`. Use the Node
version CI uses (the `node-version` in the workflow under `.github/workflows/`, if present): a
test script may pass options that an older Node rejects.

### 3. Establish the drift

For each app from step 2:

```bash
(cd "<app>" && npm ci --silent && npx expo-doctor)
```

`npm ci` first, not `npm install`: you start from the committed lockfile, not from whatever happens
to be installed. Note the `package / expected / found` table per app. You need it in step 7 for the
commit message, and at the *Stopping point* to decide whether this is still your task.

### 4. Repair

Per app:

```bash
(cd "<app>" && npx expo install --fix && npm dedupe)
```

`expo install --fix` updates `package.json` **and** `package-lock.json`: the ranges move up to what
the installed SDK expects (a `~X.Y.Z` range moves to a later patch of the same `X.Y`). That is
intended and stays within the SDK: `expo install` cannot lift you to the next SDK by construction,
because it resolves against the table of the SDK that is already installed.

`npm dedupe` afterwards is no ritual. `--fix` can leave a duplicate behind, two patch versions of
the same package under `expo/`. The version check is then green and the duplicate native module
check red: you would think the fix failed, while only the clean-up was missing. Always run it,
also when `--fix` seems to have done nothing; on an app without duplicates it is a no-op.

That the manifests move here is the difference with a Dependabot entry on
`versioning-strategy: lockfile-only` (in `.github/dependabot.yml`, if present), which leaves the
ranges alone. Both end on the same installed patch version and so on a green version check; this
route is only more explicit.

### 5. Verify: both, per app

```bash
(cd "<app>" && npx expo-doctor)          # must report that all checks passed
(cd "<app>" && npm test --if-present)    # what a CI check on the app usually runs
```

Claim nothing about green without having seen the output of both. Both must exit 0.

### 6. Check that the diff is what you think, before you commit

```bash
git diff --stat                                # only <app>/package.json and <app>/package-lock.json
git diff -- '*package.json' | grep '^+.*"'     # every bump must stay within the same major
```

If one of those lines looks like a major jump, step 4 did not run as intended: revert, and read
the *Stopping point*.

### 7. Commit

One commit per app keeps the diff readable and makes each app revertable on its own:

```
chore(deps): align <app> with the Expo SDK patch versions

expo <old> → <new>, jest-expo <old> → <new>. expo-doctor green,
tests green. No SDK change: only caught up with the patch table.
```

Then follow the commit flow from `AGENTS.md`.

## Stopping point: this has become an SDK upgrade

Compare the `expected` column from step 3 with the major in `package.json`:

| Situation | Meaning | Action |
|---|---|---|
| expected `~X.0.N`, manifest `~X.0.M` with `M < N` | patch drift | this skill fixes it |
| expected `~(X+1).x` or higher | the SDK pin itself is due | **stop** |

The second case is no lockfile refresh but an SDK migration: a new React Native, native module
churn, rebuilding the dev client. Write an issue for it with `/lighthouse-write-issue`. Give it the
area label of the apps when the project has an area prefix: read it with
`python3 .lighthouse/lib/lighthouse_lib/config.py labels.area_prefix` (rc 1: no prefix, leave the
label out). Use the `expo-upgrade` skill, if present. Do not smuggle the upgrade into this fix.

## Known exceptions

An app may exclude packages from the React Native Directory check
(`expo.doctor.reactNativeDirectoryCheck.exclude` in its `package.json`). Such an exclusion is
existing and intended: never add to it to get a refresh green. The reason for each exclusion is in
the instructions next to the app (an `AGENTS.md` or `CLAUDE.md`), if present.
