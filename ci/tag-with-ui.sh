#!/usr/bin/env bash
# Put the built SPA in a release commit and tag that commit.
# Usage: ci/tag-with-ui.sh <tag> <dist-source>
# Run from the root of the worktree. Copies <dist-source> (a directory outside the worktree that holds
# index.html) to internal/web/dist, force-adds it past .gitignore, commits it as
# `chore(release): <tag> web-UI` and creates the annotated tag <tag> on that commit, so
# `go install ...@<tag>` embeds the real SPA. Builds nothing and pushes nothing.
set -euo pipefail

fail() {
  echo "tag-with-ui: $*" >&2
  exit 1
}

[ $# -eq 2 ] || fail "usage: ci/tag-with-ui.sh <tag> <dist-source>"
tag=$1
src=$2
[ -f "$src/index.html" ] || fail "no index.html in $src: not a built SPA"

dist=internal/web/dist
rm -rf "$dist"
mkdir -p "$dist"
cp -R "$src"/. "$dist"/
# -f: the dist directory is ignored in the working tree.
git add -f "$dist"
git commit -q -m "chore(release): $tag web-UI"
git tag -a "$tag" -m "$tag"
echo "tag-with-ui: ok ($tag)"
