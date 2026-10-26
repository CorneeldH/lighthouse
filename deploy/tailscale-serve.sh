#!/usr/bin/env bash
# Expose the local lighthouse server on the tailnet over HTTPS.
#
# Usage: deploy/tailscale-serve.sh [port]    (default 8127)
#
# - Idempotent: running it again replaces the same serve entry.
# - --bg stores the entry in tailscaled, so it survives a reboot.
# - Never use `tailscale funnel`: it would publish the server on the public internet.
# - lighthouse has no authentication; the tailnet is the access boundary.
#
# One-time prerequisites:
#   sudo tailscale set --operator="$USER"    # lets your user run `tailscale serve`
#   enable HTTPS certificates in the tailnet admin console
#
# Turn it off: tailscale serve --https=<port> off
set -euo pipefail

port="${1:-8127}"

if [[ ! "$port" =~ ^[0-9]+$ ]]; then
  echo "usage: $0 [port]  (port must be digits only, got '$port')" >&2
  exit 2
fi

tailscale serve --bg --https="$port" "http://127.0.0.1:$port"
tailscale serve status
