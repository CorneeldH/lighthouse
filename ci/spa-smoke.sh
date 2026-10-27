#!/usr/bin/env bash
# Smoke test for a lighthouse binary with the real SPA embedded.
# Usage: ci/spa-smoke.sh <binary>
# Starts `<binary> serve` on a free port, waits up to 10 s for /api/health, then requires that
# `/` carries the SPA marker and that the first /assets/*.js of that page answers 200 with a JavaScript content type.
# On any failure the server log goes to stderr.
set -euo pipefail

fail() {
  [ -z "${tmp:-}" ] || [ ! -f "$tmp/serve.log" ] || cat "$tmp/serve.log" >&2
  echo "spa-smoke: $*" >&2
  exit 1
}

[ $# -eq 1 ] || fail "usage: ci/spa-smoke.sh <binary>"
bin=$1
[ -x "$bin" ] || fail "not an executable file: $bin"

tmp=$(mktemp -d)
pid=
cleanup() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

port=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
base="http://127.0.0.1:$port"

"$bin" serve --addr "127.0.0.1:$port" --db "$tmp/lighthouse.db" >"$tmp/serve.log" 2>&1 &
pid=$!

up=
for _ in $(seq 1 100); do
  if curl -fsS -o /dev/null "$base/api/health" 2>/dev/null; then
    up=1
    break
  fi
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.1
done
if [ -z "$up" ]; then
  fail "server did not answer /api/health within 10 s"
fi

page=$(curl -fsS "$base/") || fail "GET / failed"
case $page in
  *'name="lighthouse-ui" content="spa"'*) ;;
  *) fail 'page / lacks name="lighthouse-ui" content="spa": the binary embeds no real SPA build' ;;
esac

asset=$(printf '%s' "$page" | grep -oE '/assets/[^"]+\.js' | head -n 1 || true)
[ -n "$asset" ] || fail "page / references no /assets/*.js"
# The SPA fallback answers an unknown path with 200 text/html, so the status alone proves nothing.
res=$(curl -sS -o /dev/null -w '%{http_code} %{content_type}' "$base$asset") || fail "GET $asset failed"
case $res in
  '200 text/javascript'*) ;;
  *) fail "GET $asset answered '$res', want '200 text/javascript'" ;;
esac

echo "spa-smoke: ok ($asset)"
