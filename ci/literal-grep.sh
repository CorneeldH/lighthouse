#!/bin/bash
# Fail when a literal from the deny list appears in a package (spec, Portabiliteit).
# Usage: ci/literal-grep.sh [dir]   (default: pack)
# rc 0 clean; rc 1 hits, printed as path:line:text; rc 2 deny list missing or empty.
set -u

DIR="${1:-pack}"
DENYLIST="${LITERAL_DENYLIST:-$(dirname "$0")/literal-denylist.txt}"

if [ ! -r "$DENYLIST" ]; then
  echo "literal-grep: deny list not readable: $DENYLIST" >&2
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
grep -rniF --exclude-dir=__pycache__ --exclude-dir=.pytest_cache --exclude="*.pyc" -f "$PATTERNS" -- "$DIR"
case $? in
  0) echo "literal-grep: literals from $DENYLIST found in $DIR" >&2; exit 1 ;;
  1) exit 0 ;;
  *) echo "literal-grep: grep failed" >&2; exit 2 ;;
esac
