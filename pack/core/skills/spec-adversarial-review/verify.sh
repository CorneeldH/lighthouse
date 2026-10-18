#!/usr/bin/env bash
# Draai één verificatieronde: dezelfde runner als review.sh, met de verifier-prompt
# en het verificatieschema van deze map (vanaf ronde 2, #1066).
#
#   verify.sh <SPEC> <ROUND> <OUT> [PRIOR] [LEVEL] [--context <diff>]...
#
# Eén runner, geen tweede kopie van de Codex-aanroep (spec #972, sectie 1).
# Exitcodes: die van review.sh (in deze map).
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SKILL_DIR/review.sh" \
  --prompt "$SKILL_DIR/verifier-prompt.md" \
  --schema "$SKILL_DIR/verification.schema.json" \
  "$@"
