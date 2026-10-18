#!/usr/bin/env bash
# Draai één adversariële plan-reviewronde: de runner van de spec-review, met de
# plan-prompt en het plan-schema van deze map.
#
#   review.sh <PLAN> <ROUND> <OUT> [PRIOR] [LEVEL] [--context <SPEC>]...
#
# Eén runner, geen tweede kopie van de Codex-aanroep (spec #972, sectie 1). Het
# relatieve pad naar de buurskill werkt in .claude/skills/ én .agents/skills/.
# Exitcodes: die van ../paia-spec-adversarial-review/review.sh.
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SKILL_DIR/../paia-spec-adversarial-review/review.sh" \
  --prompt "$SKILL_DIR/reviewer-prompt.md" \
  --schema "$SKILL_DIR/findings.schema.json" \
  "$@"
