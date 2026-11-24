---
name: lighthouse-upgrade
description: 'Use when this repo should move to the lighthouse version that is installed or newest — "upgrade lighthouse", "bring this repo to the new lighthouse", "update the lighthouse pack", /lighthouse-upgrade. Reads the repo version and the binary version, shows the changelog between them, runs `lighthouse init --upgrade` in a worktree, proves the result and commits it. NOTE — not for the binary itself (that is `lighthouse self-update`) and not for the lighthouse checkout itself, whose version is `dev`.'
metadata:
  lighthouse: "core@dev"
  binding: default
  origin: own
---

# Upgrade Skill

From "upgrade lighthouse" to one commit that holds the new pack, proven and ready for a PR. The
skill stops at the first red and never forces a conflict away.

## Domain

In scope: a repo that has `.lighthouse/manifest.toml` and a `lighthouse` binary on the PATH.

Not here: replacing the binary on its own (`lighthouse self-update`), the lighthouse checkout itself
(its version is `dev`; it dogfoods with `go run ./cmd/lighthouse init --upgrade`), opening the PR
(`/lighthouse-write-pr`).

## Workflow

### 1. Read the state

One Bash call, read-only, `dangerouslyDisableSandbox: true` (`lighthouse releases` needs the network).

```bash
set -uo pipefail
L=CorneeldH/lighthouse
M=$(sed -n 's/^version = "\(.*\)"$/\1/p' .lighthouse/manifest.toml | head -n 1)
B=$(lighthouse version | awk '{print $NF}')
C=$(lighthouse channel)   # release or dev
# skipped when the releases cannot be fetched: then self-update is not offered
T=$(mktemp -d)
lighthouse releases --repo "$L" > "$T/releases.json" || echo "releases: FETCH FAILED"
# draft never counts; prerelease only counts on dev, and there only vX.Y.Z, -dev.N and -rc.N
if [ "$C" = dev ]; then
  TAGS=$(jq -r '.[]|select(.draft|not)|.tag|select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+(-(dev|rc)\\.[0-9]+)?$"))' "$T/releases.json")
else
  TAGS=$(jq -r '.[]|select((.prerelease or .draft)|not)|.tag' "$T/releases.json")
fi
echo "$TAGS" > "$T/tags.txt"
# the highest tag, by lighthouse version compare
N=
for t in $TAGS; do
  if [ -z "$N" ] || [ "$(lighthouse version compare "$t" "$N")" = 1 ]; then N=$t; fi
done
M=${M#v}; B=${B#v}; N=${N#v}
echo "repo=$M binary=$B newest=$N channel=$C"
echo "releases=$T/releases.json tags=$T/tags.txt"
```

- `M` or `B` is empty (the manifest has no version, or `lighthouse version` printed nothing): say so
  and stop, before any comparison.
- `M` or `B` is `dev`: say so and stop. Never compare a `dev` build.
- `B` is older than the repo (`lighthouse version compare "$M" "$B"` prints `1`): say "the binary is older than the repo" and
  stop. Never run `init --upgrade` then; it would downgrade.

### 2. The binary

If `N` is newer than `B` (`lighthouse version compare "$N" "$B"` prints `1`), propose `lighthouse self-update` and warn that it replaces the
binary for the whole host. It needs the network: run it outside any sandbox that blocks the network. Run it only after the user agrees (a question in the chat), then read `B`
again.

If the user declines, or nothing newer exists, compare `B` with `M`:

- `B` = `M`: say "already current" and stop.
- `B` is ahead of `M`: continue with `B`, the binary the user has.

### 3. Release notes (a failed fetch is reported, never skipped silently)

The `lighthouse releases` call needs the network: run it outside any sandbox that blocks the network. If step 1 printed `releases: FETCH FAILED` or a fetch fails, tell the user "could not fetch release
notes" and why; do not go on as if there were none.

For every tag with `M < tag <= B`, show the notes. Strip the `v` with
`${x#v}`, and keep a tag when `lighthouse version compare` says `M < tag <= B`, never on the order `lighthouse releases` returns them. On the `dev` channel the prereleases of step 1 count too.

```bash
# the releases were fetched once in step 1: R is the `releases=` path it printed
R=<releases= path from step 1>
cat <tags= path from step 1>   # the candidate tags of this channel
jq -r --arg t "<tag>" '.[] | select(.tag == $t) | .body' "$R"
```

Show lines that ask to update the binary or `lighthouse.toml` first (for example "Update the binary
… before `lighthouse init --upgrade`") apart, at the top.

### 4. Worktree

From a fresh default branch (`lighthouse config forge.default_branch`), under the worktree root (`lighthouse config worktree.root`).
Branch by the repo's branch rule if it has one, otherwise `chore-lighthouse-<B>`. Work only there.

### 5. Upgrade

Show the diff first. Stdin is empty, so `init` prints the diff and then refuses at "Apply these
changes? [y/N]"; that is intended, not an error.

```bash
lighthouse init --upgrade </dev/null
```

Then ask the user to agree (a question in the chat). "Declined" is only the user's answer, never the
outcome of that first call. Only then:

```bash
lighthouse init --upgrade --yes
```

`refused:` on stderr (exit not 0), for example `refused: N locally modified unit(s)`: report it and
stop. Never `--force`.

### 6. Evidence

In this order; at the first red, stop, report what it says and do not commit.

1. The test gate. `lighthouse config test_gate.command` exits 1 when none is set, or prints nothing
   when it is empty: say so and skip it, it is no evidence; never count `bash -c ""` as green. Otherwise run it with the variable the hook would set:
   `LIGHTHOUSE_GATE_TMP=$(mktemp -d) bash -c "$(lighthouse config test_gate.command)"`.
2. `lighthouse check-instructions`.
3. If `[paths] mirror_exceptions` is set (the file: `lighthouse config paths.mirror_exceptions`): run `lighthouse check-instructions --print-mirror-exceptions` and
   write its output to that file.
4. `lighthouse init --upgrade </dev/null` once more must print exactly `No changes.`.

### 7. Commit and PR

`git status` must hold only what `init --upgrade` wrote, plus the mirror file. Anything else
(the test gate writes files too): stop and report it.

One commit, subject `chore(lighthouse): upgrade to <B>` written in `[project] language`. Push and
open the PR only after the user agrees, through `/lighthouse-write-pr`.

## Non-negotiable

- ✅ Compare versions without the `v`, with `lighthouse version compare` · ❌ compare strings, or a `dev` build
- ✅ Stop when the binary is older than the repo · ❌ `init --upgrade` with an older binary
- ✅ `lighthouse self-update`, after consent, warned as whole-host · ❌ any other way to get a binary
- ✅ `--yes` only after the user agreed to the shown diff · ❌ `--yes` on the first call
- ✅ Stop on `refused:` · ❌ never `--force`
- ✅ Stop at the first red evidence · ❌ commit a red upgrade
