#!/bin/bash
# lighthouse: go@dev
# PostToolUse hook: run `gofmt -w` on edited Go files.
# Matcher: Edit|Write — runs after every file edit.
# Formatting is silent and non-blocking. A file that does not parse blocks (exit 2):
# gofmt cannot format it, and every later check would run on broken code.
#
# NOT fail-open: without jq, with a payload that is not JSON, or without gofmt,
# nothing was checked; a silent exit 0 would read as "the file is clean". Every such
# exit is exit 2 with `gofmt-check: COULD NOT CHECK — <reason>` on the first line (the
# token bash_writes.classify reads). No file path, another extension or a file that
# does not exist leave nothing to check: exit 0, without looking for gofmt.

HOOK_NAME="gofmt-check"

cannot_check() {
  {
    echo "$HOOK_NAME: COULD NOT CHECK — $1"
    echo ""
    echo "This hook formats the Go file and blocks on a file that does not parse."
    echo "Nothing was checked; that is not a green light."
  } >&2
  exit 2
}

if ! command -v jq >/dev/null 2>&1; then
  cannot_check "jq is missing."
fi

INPUT=$(cat)
# jq reads an empty stdin as no input at all and exits 0: that is no payload.
[ -n "${INPUT//[[:space:]]/}" ] || cannot_check "the payload is empty."
if ! FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null); then
  cannot_check "the payload is not valid JSON."
fi

if [ -z "$FILE_PATH" ]; then
  exit 0
fi

# Only process Go files
case "$FILE_PATH" in
  *.go) ;;
  *) exit 0 ;;
esac

if [ ! -f "$FILE_PATH" ]; then
  exit 0
fi

if ! command -v gofmt >/dev/null 2>&1; then
  cannot_check "gofmt is not on the PATH."
fi

# gofmt exits 2 when the file does not parse; the parse error is on its output.
if ! out=$(gofmt -w "$FILE_PATH" 2>&1); then
  {
    echo "$HOOK_NAME: $FILE_PATH does not parse — fix the syntax."
    echo ""
    echo "$out"
  } >&2
  exit 2
fi

exit 0
