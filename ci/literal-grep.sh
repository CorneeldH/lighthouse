#!/bin/bash
# Fail when a literal from the deny list appears in a package (spec, Portabiliteit).
# Usage: ci/literal-grep.sh [dir ...]   (default: pack)
# Env: LITERAL_DENYLIST, LITERAL_ALLOWLIST; LITERAL_SKIP_TESTS=1 also skips *_test.go
# files and testdata/ directories (for the Go scan; without it a hit in a test counts).
# rc 0 clean; rc 1 hits, printed as path:line:text; rc 2 deny or allow list unusable.
#
# Two exceptions. `<dir>/SOURCES.toml` is skipped in every given dir: it names source paths and is never
# installed. A hit whose `<path>:<text>` (without the line number) stands exactly in
# the allow list passes; the path there is relative to the repository root (a path
# outside the repository stays absolute).
set -u

if [ "$#" -eq 0 ]; then set -- pack; fi
DIRS=()
SKIPS=""
for d in "$@"; do
  d="${d%/}"
  DIRS+=("$d")
  SKIPS="$SKIPS$d/SOURCES.toml"$'\n'
done
DENYLIST="${LITERAL_DENYLIST:-$(dirname "$0")/literal-denylist.txt}"
ALLOWLIST="${LITERAL_ALLOWLIST:-$(dirname "$0")/literal-allowlist.txt}"
ROOT=$(cd "$(dirname "$0")/.." && pwd) || exit 2

if [ ! -r "$DENYLIST" ]; then
  echo "literal-grep: deny list not readable: $DENYLIST" >&2
  exit 2
fi
if [ ! -r "$ALLOWLIST" ]; then
  echo "literal-grep: allow list not readable: $ALLOWLIST" >&2
  exit 2
fi

PATTERNS=$(mktemp) || exit 2
trap 'rm -f "$PATTERNS"' EXIT
# A blank line as a pattern matches everything; comments are not patterns.
grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "$DENYLIST" > "$PATTERNS"
if [ ! -s "$PATTERNS" ]; then
  echo "literal-grep: deny list has no patterns: $DENYLIST" >&2
  exit 2
fi

# Caches are never installed (the installer skips them) and never committed.
# Bash 3.2 (macOS) under `set -u`: never expand a possibly-empty array bare; DIRS is never empty.
if [ "${LITERAL_SKIP_TESTS:-}" = 1 ]; then
  HITS=$(grep -rniF --exclude-dir=__pycache__ --exclude-dir=.pytest_cache --exclude="*.pyc" \
    --exclude="*_test.go" --exclude-dir=testdata -f "$PATTERNS" -- "${DIRS[@]}")
else
  HITS=$(grep -rniF --exclude-dir=__pycache__ --exclude-dir=.pytest_cache --exclude="*.pyc" \
    -f "$PATTERNS" -- "${DIRS[@]}")
fi
case $? in
  0) ;;
  1) exit 0 ;;
  *) echo "literal-grep: grep failed" >&2; exit 2 ;;
esac

REMAINING=$(printf '%s\n' "$HITS" | awk -v skips="$SKIPS" -v allow="$ALLOWLIST" -v root="$ROOT/" -v cwd="$(pwd)" '
  BEGIN {
    n = split(skips, parts, "\n")
    for (k = 1; k <= n; k++) if (parts[k] != "") skip[parts[k]] = 1
    while ((getline line < allow) > 0) {
      if (line ~ /^[[:space:]]*#/ || line ~ /^[[:space:]]*$/) continue
      ok[line] = 1
    }
  }
  {
    i = index($0, ":"); path = substr($0, 1, i - 1); rest = substr($0, i + 1)
    j = index(rest, ":"); text = substr(rest, j + 1)
    if (path in skip) next
    rel = path
    if (substr(rel, 1, 1) != "/") rel = cwd "/" rel
    if (index(rel, root) == 1) rel = substr(rel, length(root) + 1)
    if ((rel ":" text) in ok) next
    print
  }')
if [ -n "$REMAINING" ]; then
  printf '%s\n' "$REMAINING"
  echo "literal-grep: literals from $DENYLIST found in ${DIRS[*]}" >&2
  exit 1
fi
exit 0
