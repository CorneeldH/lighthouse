#!/usr/bin/env bash
# Draai één verificatieronde: dezelfde runner als review.sh, met de verifier-prompt
# en het verificatieschema van deze map (vanaf ronde 2, #1066).
#
#   verify.sh <PLAN> <ROUND> <OUT> [PRIOR] [LEVEL] [--context <SPEC-of-diff>]...
#
# Eén runner, geen tweede kopie van de Codex-aanroep (spec #972, sectie 1). Het
# relatieve pad naar de buurskill werkt in .claude/skills/ én .agents/skills/.
# Exitcodes: die van ../paia-spec-adversarial-review/review.sh.
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SKILL_DIR/../paia-spec-adversarial-review/review.sh" \
  --prompt "$SKILL_DIR/verifier-prompt.md" \
  --schema "$SKILL_DIR/verification.schema.json" \
  "$@"
