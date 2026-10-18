#!/usr/bin/env bash
# Draai één adversariële reviewronde via een externe Codex-sessie.
#
#   review.sh [--prompt F] [--schema F] [--context PAD]... <DOC> <ROUND> <OUT> [PRIOR] [LEVEL]
#
#     DOC    pad naar het te reviewen document (spec of plan), relatief aan de repo-root
#     ROUND  rondenummer (1, 2, 3)
#     OUT    pad waar het JSON-resultaat landt (.events en .err komen ernaast)
#     PRIOR  pad naar de vorige ronde, of "none" (default none). Ligt ernaast een
#            sidecar `<naam zonder .json>.decisions.json`, dan noemt het prior-blok
#            die; zonder sidecar is het blok byte-gelijk aan dat van vóór #972.
#     LEVEL  redeneerniveau; leeg = uit config.toml. Gebruik `max` alleen als een
#            gemiste blocker duur is. Nooit `xhigh` — gemeten duurder dan `high`
#            zonder één unieke bevinding (experiments/spec-review-levels/REPORT.md).
#
#     --prompt   promptsjabloon; default reviewer-prompt.md naast dit script
#     --schema   outputschema; default findings.schema.json naast dit script
#     --context  extra contextpad voor {{CONTEXT_PATHS}}, herhaalbaar; repo-relatief
#                of absoluut, moet bestaan
#
# Vlaggen mogen overal staan. Zonder vlaggen en zonder sidecar is de prompt
# byte-gelijk aan die van vóór #972 — de spec-review merkt niets van de plan-review
# (tests/unit/test_adversarial_review_sh.py).
#
# Eén vaste commandovorm, zodat hij te allowlisten is en een ronde op de
# achtergrond kan lopen zonder tussentijdse approval.
#
# Vóór Codex draait de runner `codex_usage.py budget` (#1093) en schrijft het besluit
# naar `<OUT>.budget.json`. Staat het budget vooraf op (besluit `fallback`), dan stopt
# de runner met exit 6 en stderr `CODEX_BUDGET: fallback — <reden>`, zonder `codex exec`.
# `PAIA_CODEX_BUDGET_CMD` vervangt dat commando (testnaad, op spaties gesplitst). Een
# check die faalt of geen JSON-object geeft telt als niet meetbaar: de ronde gaat door.
#
# Exit: 0 ok | 2 verkeerd gebruik | 3 codex ontbreekt | 4 niet ingelogd | 5 run mislukt | 6 budget vooraf op
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMPT_FILE="$SKILL_DIR/reviewer-prompt.md"
SCHEMA_FILE="$SKILL_DIR/findings.schema.json"
CONTEXTS=""
POS=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --prompt | --schema | --context)
      [ "$#" -ge 2 ] || { echo "$1 mist een waarde" >&2; exit 2; }
      case "$1" in
        --prompt) PROMPT_FILE="$2" ;;
        --schema) SCHEMA_FILE="$2" ;;
        --context) CONTEXTS="${CONTEXTS}${2}"$'\n' ;;
      esac
      shift 2
      ;;
    --*) echo "onbekende vlag: $1" >&2; exit 2 ;;
    *) POS+=("$1"); shift ;;
  esac
done
# `${POS[@]+...}`: bash 3.2 (de macOS-default) klapt onder `set -u` op een lege
# array-expansie.
set -- ${POS[@]+"${POS[@]}"}

SPEC="${1:-}"
ROUND="${2:-}"
OUT="${3:-}"
PRIOR="${4:-none}"
LEVEL="${5:-}"

[ -n "$SPEC" ] && [ -n "$ROUND" ] && [ -n "$OUT" ] || {
  echo "gebruik: review.sh [--prompt F] [--schema F] [--context PAD]... <DOC> <ROUND> <OUT> [PRIOR] [LEVEL]" >&2; exit 2; }
[ -f "$PROMPT_FILE" ] || { echo "promptsjabloon niet gevonden: $PROMPT_FILE" >&2; exit 2; }
[ -f "$SCHEMA_FILE" ] || { echo "schema niet gevonden: $SCHEMA_FILE" >&2; exit 2; }

command -v codex >/dev/null || { echo "CODEX_MISSING: geen codex in PATH" >&2; exit 3; }

REPO="$(git rev-parse --show-toplevel)"
[ -f "$REPO/$SPEC" ] || { echo "spec niet gevonden onder de repo-root: $REPO/$SPEC" >&2; exit 2; }
while IFS= read -r ctx; do
  [ -z "$ctx" ] && continue
  case "$ctx" in
    /*) [ -f "$ctx" ] || { echo "contextpad niet gevonden: $ctx" >&2; exit 2; } ;;
    *) [ -f "$REPO/$ctx" ] || { echo "contextpad niet gevonden onder de repo-root: $REPO/$ctx" >&2; exit 2; } ;;
  esac
done <<<"$CONTEXTS"

# Schone CODEX_HOME: de reviewer erft geen marketplaces, plugins, MCP-servers,
# hooks of agents. Die kosten ~8k tokens en ondermijnen de schone context waar
# deze skill op rust. `--ignore-user-config` is niet genoeg — dat slaat alleen
# config.toml over en laadt $CODEX_HOME/agents/ alsnog.
CH="${CODEX_SPECREVIEW_HOME:-$HOME/.codex-specreview}"
mkdir -p "$CH"
[ -f "$HOME/.codex/auth.json" ] || { echo "CODEX_UNAUTHENTICATED: ~/.codex/auth.json ontbreekt" >&2; exit 4; }
cp -f "$HOME/.codex/auth.json" "$CH/auth.json"   # pakt een ververst token mee
[ -s "$CH/config.toml" ] || printf 'model = "gpt-5.6-sol"\nmodel_reasoning_effort = "high"\n' > "$CH/config.toml"

mkdir -p "$(dirname "$OUT")"

# Budget vooraf (#1093). Geen heredoc binnen `$(...)`: bash 3.2 (macOS) parset dat fout.
set +e
if [ -n "${PAIA_CODEX_BUDGET_CMD:-}" ]; then
  read -r -a BUDGET_ARGV <<<"$PAIA_CODEX_BUDGET_CMD"
  "${BUDGET_ARGV[@]}" < /dev/null > "$OUT.budget.json"
else
  uv run --project "$REPO" python "$REPO/scripts/codex_usage.py" budget \
    < /dev/null > "$OUT.budget.json"
fi
BUDGET_CODE=$?
python3 - "$OUT.budget.json" "$BUDGET_CODE" <<'PY'
import json
import pathlib
import sys

path, code = pathlib.Path(sys.argv[1]), int(sys.argv[2])
try:
    doc = json.loads(path.read_text(encoding="utf-8"))
except (OSError, ValueError):
    doc = None
if code in (0, 6) and isinstance(doc, dict):
    if code == 6:
        reden = doc.get("reden", "")
        sys.stderr.buffer.write(f"CODEX_BUDGET: fallback — {reden}\n".encode())
    raise SystemExit(code)
sleutels = (
    "besluit", "reden", "venster", "reset_at", "session_pct",
    "week_pct", "bron", "ondergrens", "drempels",
)
synth = dict.fromkeys(sleutels)
synth["besluit"] = "niet_meetbaar"
synth["reden"] = f"budgetcheck faalde: exit {code}"
path.write_text(json.dumps(synth, ensure_ascii=False) + "\n", encoding="utf-8")
PY
BUDGET_BESLUIT=$?
set -e
if [ "$BUDGET_BESLUIT" -eq 6 ]; then
  exit 6
fi

PROMPT_TMP="$(mktemp)"
trap 'rm -f "$PROMPT_TMP"' EXIT

python3 - "$PROMPT_FILE" "$SPEC" "$REPO" "$ROUND" "$PRIOR" "$CONTEXTS" > "$PROMPT_TMP" <<'PY'
import pathlib
import sys

tmpl, spec, repo, round_no, prior, contexts = sys.argv[1:7]
text = pathlib.Path(tmpl).read_text()
body = text.split("---BEGIN PROMPT---", 1)[1].split("---END PROMPT---", 1)[0].strip()
context_paths = [c for c in contexts.split("\n") if c]

prior_block = ""
if prior != "none":
    prior_path = pathlib.Path(prior)
    sidecar = prior_path.with_name(prior_path.name.removesuffix(".json") + ".decisions.json")
    if sidecar.is_file():
        prior_block = (
            f"Previous review: `{prior}`. The author's decisions are in `{sidecar}`:\n"
            "one entry per finding, keyed by `finding_id` = `r<round>-f<index>` (index =\n"
            "1-based position in that review's `findings` array), with `decision` and\n"
            "`reason`. Check specifically which accepted findings were answered in prose\n"
            "but not actually resolved in the document. Do not re-raise a finding the\n"
            "author rejected with a stated reason — instead say whether that reasoning is\n"
            "wrong, and why."
        )
    else:
        prior_block = (
            f"Previous review: `{prior}`. The author's responses are recorded there.\n"
            "Check specifically which findings were answered in prose but not actually\n"
            "resolved in the spec. Do not re-raise a finding the author rejected with a\n"
            "stated reason — instead say whether that reasoning is wrong, and why."
        )

if context_paths and "{{CONTEXT_PATHS}}" not in body:
    raise SystemExit("--context gegeven, maar het sjabloon heeft geen {{CONTEXT_PATHS}}")
context_block = "\n".join(f"- {c}" for c in context_paths) or "(none)"

body = (
    body.replace("{{SPEC_PATH}}", spec)
    .replace("{{DOC_PATH}}", spec)
    .replace("{{CONTEXT_PATHS}}", context_block)
    .replace("{{REPO}}", repo)
    .replace("{{ROUND}}", round_no)
    .replace("{{PRIOR_REVIEW_BLOCK}}", prior_block)
)
if "{{" in body:
    raise SystemExit("niet-gesubstitueerde placeholder in het promptsjabloon")
print(body)
PY

# De niveauvlag via positionele parameters, niet via een array: bash 3.2 (de
# macOS-default) klapt onder `set -u` op een lege array-expansie.
if [ -n "$LEVEL" ]; then
  set -- -c "model_reasoning_effort=$LEVEL"
else
  set --
fi

set +e
CODEX_HOME="$CH" codex exec \
  --sandbox read-only \
  "$@" \
  --cd "$REPO" \
  --output-schema "$SCHEMA_FILE" \
  -o "$OUT" \
  --json \
  "$(cat "$PROMPT_TMP")" \
  < /dev/null > "$OUT.events" 2> "$OUT.err"
CODE=$?
set -e

if [ "$CODE" -ne 0 ] || [ ! -s "$OUT" ]; then
  echo "CODEX_FAILED (exit $CODE). Laatste regels van $OUT.err:" >&2
  tail -15 "$OUT.err" >&2
  exit 5
fi

python3 - "$OUT" "$CH/config.toml" <<'PY'
import json
import pathlib
import sys

out, config = sys.argv[1], sys.argv[2]
doc = json.loads(pathlib.Path(out).read_text())
missing = {"review_markdown", "status", "findings"} - set(doc)
if missing:
    raise SystemExit(f"onvolledig resultaat, mist {sorted(missing)}")
model = next(
    (line.split('"')[1] for line in pathlib.Path(config).read_text().splitlines()
     if line.startswith("model =")),
    "onbekend",
)
sev = {s: sum(1 for f in doc["findings"] if f["severity"] == s) for s in ("BLOCKER", "MAJOR", "MINOR")}
print(f"ok | reviewer: {model} | status: {doc['status']} | "
      f"{len(doc['findings'])} findings ({sev['BLOCKER']}B/{sev['MAJOR']}M/{sev['MINOR']}m) | raw: {out}")
PY
