---
name: paia-fix-pr
description: 'Use when an existing pull request in CorneeldH/personal_ai_agent is red or has merge conflicts, or when a green PR carries an unprocessed roborev review comment (findings without a `roborev-verwerkt` marker) — "fix pr 596", "fix 595", "612 heeft conflicts, fix die", "627 pr heeft errors", "PR 585 is rood". Opent met één vaste diagnose (status, checks, faallog, worktree) en werkt in de worktree van de head-branch. LET OP — deze skill stopt zodra de PR groen is (en een roborev-review verwerkt is): het sluiten van het issue is paia-close-issue, een inhoudelijke review van de diff is caveman-review, en nieuw werk aan een issue beginnen is paia-start-issue. Niet gebruiken voor een PR die alleen nog gemerged hoeft te worden.'
metadata:
  binding: default
  origin: own
  domain: factory
---

# Fix PR Skill

Van "fix 596" naar een groene PR. De skill voegt geen kunde toe — hij vervangt de aanloop, die in
acht gemeten beurten steeds dezelfde vier vragen stelde in vijf tot acht losse calls.

Ontwerp en nulmeting: `docs/developer/planning/plans/2026-08-29-paia-fix-pr.md`.

## Domein

Hieronder valt: een **bestaande** PR in `CorneeldH/personal_ai_agent` die rood staat of
conflicteert — diagnose, de worktree van de head-branch, de fix, groen krijgen.

Tweede ingang: een **groene** PR met een onverwerkte roborev-review (sectie 6) — de controller
start de skill daarvoor, of jij zegt "verwerk de roborev-review op 1407".

Hier niet: het issue sluiten (`/paia-close-issue`), een inhoudelijke review van de diff
(`/caveman-review`), nieuw werk aan een issue beginnen (`/paia-start-issue`), een PR openen
(`/engineering-workflow-skills:pr`), en het mergen zelf — dat is een mensbeslissing.

## 1. Diagnose in één blok

Eén Bash-call, read-only, buiten de sandbox. Vervang `<nr>`.

```bash
set -uo pipefail
N=<nr>
R=$(uv run python -m src.controller.identity repo) || exit 2

PR=$(gh pr view "$N" --repo "$R" --json number,title,state,isDraft,mergeable,mergeStateStatus,headRefName,url 2>&1)
RC=$?
if [ "$RC" -ne 0 ]; then
  echo "geen PR #$N — is het een issue? gh zei: $PR"
  exit 1
fi
printf '%s' "$PR" | jq -r '"PR #\(.number) [\(.state)] draft=\(.isDraft) merge=\(.mergeable)/\(.mergeStateStatus)\nbranch \(.headRefName)\n\(.url)\n\(.title)"'
B=$(printf '%s' "$PR" | jq -r .headRefName)

echo "=== checks ==="
gh pr checks "$N" --repo "$R" || true

if [ -z "$B" ] || [ "$B" = "null" ]; then
  echo "geen headRefName in de PR-JSON — stop hier"
  exit 1
fi

echo "=== worktree ==="
git worktree list | grep -F -- "[$B]" || echo "GEEN worktree op $B"

echo "=== faallog ==="
RUNS=$(gh pr checks "$N" --repo "$R" --json bucket,link --jq '.[] | select(.bucket=="fail") | .link' 2>/dev/null | grep -oE 'runs/[0-9]+' | cut -d/ -f2 | sort -u)
if [ -z "$RUNS" ]; then
  echo "geen fail-check — rood is dan een conflict, een pending check of een niet-gestarte run"
fi
for r in $RUNS; do
  echo "--- run $r ---"
  gh run view "$r" --repo "$R" --log-failed 2>&1 \
    | grep -aE '##\[error\]|FAILED | E {2,}|Process completed with exit code|Error: ' \
    | tail -15 || echo "geen faalregel gevonden — zie 'Een rode job zonder gefaalde test'"
done

echo "=== roborev ==="
RB=$(gh api "repos/$R/issues/$N/comments" --paginate --jq '.[] | select((.user.login == "github-actions[bot]" or .user.login == "github-actions") and (.body | contains("roborev-pr-comment"))) | .body | @json' 2>/dev/null) \
  && RB=$(printf '%s\n' "$RB" | tail -1 | jq -r '. // empty' 2>/dev/null) || RB=""
VW=$(gh api "repos/$R/issues/$N/comments" --paginate --jq '.[] | .body | split("\n")[] | select(contains("<!-- roborev-verwerkt:"))' 2>/dev/null) || VW=""
[ -n "$RB" ] && printf '%s\n' "$RB" || echo "geen roborev-comment"
[ -n "$VW" ] && printf '%s\n' "$VW" || echo "geen roborev-verwerkt-marker"
```

Het `=== roborev ===`-deel toont de **volledige** body van de laatste roborev-comment, niet alleen
kop en verdict: sectie 6 werkt per bevinding (pad, regel, severity), en een controller-run krijgt
alleen het PR-nummer mee. Elke comment gaat als één `@json`-regel door `--paginate`, zodat
`tail -1` over paginagrenzen heen de laatste pakt.

Vier dingen in dit blok zijn gemeten, geen smaak (het `=== roborev ===`-deel volgt dezelfde `|| echo`-regel):

- **Een guard direct na `gh pr view`.** Faalt die call — `<nr>` was een issue, geen PR — dan stopt
  het blok met een boodschap in plaats van door te lezen met een lege `$B`: zonder guard matcht een
  lege `grep -F -- ""` daarna elke regel, en het faallog-deel meldt ten onrechte "geen fail-check".
- **`|| true` achter `gh pr checks`.** Die geeft exit 1 bij falende en exit 8 bij pending checks.
  Zonder de guard breekt het blok af in precies het geval waarvoor het bedoeld is.
- **`grep` op de foutmarkeringen, niet `tail -60`.** Gemeten op run `33113954058`: het log is
  8.284 regels, de gefaalde test staat op regel 2.141, en de laatste 60 regels zijn
  `Post job cleanup`. De grep gaf op datzelfde log vier regels, met de oorzaak erin.
- **Elke zoekende stap heeft een `|| echo`-tak.** Een grep die niets vindt geeft exit 1, en met
  `pipefail` leest "geen output" dan als "gecrasht".

## 2. Werk in de worktree van de head-branch

Nooit in de hoofdcheckout, en niet in een verse worktree vanaf `main`: het werk hoort op de
head-branch van de PR. `EnterWorktree` en `superpowers:using-git-worktrees` maken een **nieuwe**
branch vanaf `origin/main` en zijn hier dus verkeerd.

```bash
B=<head-branch>
ROOT=$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")
WT="$ROOT/.claude/worktrees/$(printf '%s' "$B" | tr / -)"
if git worktree list --porcelain | grep -qxF "branch refs/heads/$B"; then
  git worktree list --porcelain | grep -B2 -xF "branch refs/heads/$B" | head -1   # bestaat al
elif git rev-parse --verify --quiet "$B" >/dev/null; then
  git worktree add "$WT" "$B"
else
  git fetch origin "$B" && git worktree add "$WT" --track -b "$B" "origin/$B" \
    || echo "kon $B niet vinden op origin — bestaat de branch nog wel op de remote?"
fi
```

`ROOT` is de hoofdcheckout, ook als dit blok binnen een andere worktree draait: `--git-common-dir`
wijst altijd naar de gedeelde `.git`, nooit naar die van de worktree zelf. Zonder die ankering
maakt `git worktree add` een geneste worktree onder de huidige worktree, waar opruimregels niet
kijken. De `-x` op beide `grep`'s matcht de hele regel, niet een substring: `refs/heads/issue-664`
matcht anders ook `refs/heads/issue-664-fix-pr-skill`.

Prefix daarna **elk** commando met `cd <worktree>` of `git -C <worktree>`. De cwd van de
persistente shell drift midden in een sessie terug naar de hoofdcheckout, en een `sed -i` raakt
dan zonder waarschuwing de verkeerde boom.

## 3. De gates, en hoe je ze lokaal reproduceert

Sinds #1016 is er één job, `ci`, op twee Beelink-runners; de rij-labels hieronder zijn de
stapnamen in die job (`.github/workflows/ci.yml`), niet losse jobs.

| Rode check | Wat er faalt | Lokaal reproduceren |
|---|---|---|
| `ci` — Unit tests | pytest, CI-selectie | `uv run pytest tests/unit/ -m 'not slow and not hardware_audio' -n 4 --tb=short` |
| `ci` — Quality ratchet (ruff) | een ruff-teller in `quality-baseline.toml` liep op | `uv run python scripts/quality/ratchet.py ruff` |
| `ci` — REUSE lint | licentiekop of `REUSE.toml` klopt niet | `uvx reuse@6.2.0 lint` |
| `ci` — Type-check src/ | `ty` op `src/` | `uv sync --extra all --group dev` daarna `uv run --no-sync ty check src/` |
| `ci` — Audit workflows | zizmor / ongepinde action in `.github/workflows/**` | `bash scripts/hooks/workflow-audit.sh` |
| `ci` — Scan working tree | gitleaks op de werkboom | `gitleaks detect --no-git --source . --config .gitleaks.toml --redact --no-banner` |
| `cla` | ondertekening ontbreekt | **niet lokaal** — de bijdrager moet zelf tekenen |
| `Expo apps` — `expo-doctor` | patch-drift in de Expo-lockfiles | `/paia-refresh-expo-lockfiles` |

Geen diff-coverage-rij meer: die oordeelt sinds #1016 's nachts per gemergde PR
(`nightly-tests.yml` → `ratchet.py nightly`), niet meer op de PR.

Zes gates zitten vóór CI en breken de push, niet de PR: de ruff-`BLOCKING_RULES` uit
`.claude/hooks/ruff-format.sh` (per edit), `.claude/hooks/shellcheck.sh` (per bewerkt `*.sh`), en
de vier consumenten van `scripts/hooks/pre-push.sh` — `pre-push-checkpoint-scan.sh`,
`pre-push-workflow-audit.sh`, `pre-push-quality.sh` en `pre-push-instructielagen.sh` (#686 taak 8).
`main-commit-guard.sh` blokkeert een commit die op `main` uitkomt.

**Een `ty`-nul zonder extras is geen nul.** Een module die `ty` niet kan resolven maakt élk symbool
eruit `Unknown`, en op `Unknown` is geen typefout mogelijk. Meld altijd mét welke extras je meet.

**Dependabot-PR's lopen anders.** Een `runs-on`-ternary stuurt dezelfde job `ci` naar
`ubuntu-latest`; de stappen `Install gitleaks`, `Type-check src/`, `Audit workflows` en
`Scan working tree` (en hun verify-gates) slaan over. Coverage staat sowieso niet meer op de
PR — dat oordeelt de nightly. Een `apps/**`-only PR draait de job nog wel, maar slaat de
Python-stappen over (de padstap zet `python=false`).

## 4. Een rode job zonder gefaalde test

Sloopt een host-brede OOM de Beelink-runner, dan faalt de job zonder dat één test faalde.
Herkenning: in
`R=$(uv run python -m src.controller.identity repo) && gh api "repos/$R/actions/runs/<id>/jobs"` staat de
pytest-stap op `"completed_at": null` terwijl de job `failure` is, en het log is leeg of 404't.
Bevestigen:

```bash
journalctl -k --since <tijd> | grep -i "Killed process"
```

Je zoekt `Killing process <pid> (Runner.Worker)` en `Out of memory: Killed process <pid> (pytest)`.
Draai dan de CI-selectie lokaal; groen daar plus een stap zonder `completed_at` betekent OOM. De
fix is `gh run rerun <id> --failed`, geen codewijziging.

## 5. Push in één keer, en stop bij groen

Werk door op de branch en push pas als het af is. Elke push naar een open PR start een volledige
nieuwe check-ronde (CI draait alléén op PR's), dus "even tussendoor bijwerken" kost een hele
runner-cyclus en maakt de checks-historie onleesbaar. Uitzondering: een fix op een *rode* check —
die hoort er wél meteen op.

Zodra `gh pr checks <nr>` groen is en `mergeable` op `MERGEABLE` staat: **melden en stoppen**.
Niet mergen, niet reviewen, niet het issue sluiten. Dat "stop bij groen" geldt niet zolang
sectie 6 openstaat: een groene PR met een onverwerkte roborev-review is nog niet af.

## 6. Roborev-review verwerken

Alleen als de checks groen zijn, het verdict in `=== roborev ===` `Changes require fixes` is en er
voor die SHA geen `roborev-verwerkt`-regel staat. Eén ronde, niet meer:

1. Werk in de worktree van de head-branch (sectie 2).
2. Volg per bevinding `superpowers:receiving-code-review`: fixen en committen, of weerleggen met
   een reden. Lees de severity kritisch: in #1015 vond de gebruiker 3 van de 4 getoetste
   `high`-bevindingen geen `high` (`experiments/roborev-retro/REPORT.md`, *Kalibratie*). Neem
   high en medium serieus. Fix een low alleen als dat triviaal is.
3. Push één keer (sectie 5) — alleen als er iets te pushen is. Weerleg je álle bevindingen, dan
   is er niets te pushen en sla je deze stap over.
4. Post **altijd** één antwoord-comment, ook als je niets pushte en ook als je elke bevinding
   weerlegde: de marker is het enige signaal waarmee de controller de ronde als af ziet. Zonder
   marker geeft hij het issue terug, of start hij na een push nog een fix-pr-run (maximaal drie).
   `R=$(uv run python -m src.controller.identity repo) && gh pr comment <nr> --repo "$R" --body-file <bestand>`. Bovenaan staat
   de marker `<!-- roborev-verwerkt: <sha> -->`, met `<sha>` letterlijk uit de kop
   `` ## roborev: Review Complete (`<sha>`) `` (of `Combined Review`, zelfde vorm). Daaronder een tabel met de kolommen *bevinding
   (pad:regel)*, *severity* en *uitkomst*. De uitkomst is `gefixt in <commit>`,
   `weerlegd: <reden>` of `gelaten: low, niet triviaal`.
5. Stop — de marker staat er dan, ook bij een ronde zonder push. Geen nieuwe review aanvragen
   (geen sluiten en heropenen) en geen `roborev fix` of `roborev refine`. Een nieuwe review vraagt
   de gebruiker zelf aan.

## Non-negotiable

- ✅ De diagnose is één Bash-call · ❌ vier losse verkennende calls
- ✅ Read-only diagnose, mutatie pas daarna · ❌ `git rebase` in hetzelfde blok als de diagnose
- ✅ Werken in de worktree van de head-branch · ❌ in de hoofdcheckout, of in een verse worktree vanaf `main`
- ✅ `git worktree add <pad> --track -b <b> origin/<b>` · ❌ `EnterWorktree` (maakt een nieuwe branch vanaf `main`)
- ✅ `gh` buiten de sandbox · ❌ zonder — Go's TLS ligt er dan uit
- ✅ Grep op de foutmarkeringen · ❌ `--log-failed | tail -60` (mist de fout)
- ✅ De gate lokaal reproduceren vóór je pusht · ❌ pushen en kijken wat CI zegt
- ✅ Stoppen zodra de PR groen is · ❌ doorlopen in review, merge of `close-issue`
- ✅ Faalt een gate op code: fix de code · ❌ de config versoepelen of `--no-verify`
- ✅ Eén verwerkronde met altijd een marker-comment · ❌ een re-review aanvragen of `roborev fix`/`roborev refine`

## KPIs

| Metric | Nu | Target | Meting |
|---|---:|---:|---|
| Tool-calls tussen fixprompt en de eerste muterende call | mediaan 4 (n=9) | ≤ 2 | `uv run python scripts/measure_fix_pr_kpi.py leadin` |
| Read-only `gh`/`git`-diagnosecalls vóór die mutatie | mediaan 3,0 (n=10) | ≤ 1 | `uv run python scripts/measure_fix_pr_kpi.py diagnose` |
| Beurten waarin deze skill laadde zonder fixverzoek in de prompt | — | 0 | `uv run python scripts/measure_fix_pr_kpi.py mistrigger` |
| Beurten waarin de skill doorliep in `paia-close-issue` of `caveman-review` | — | 0 | `uv run python scripts/measure_fix_pr_kpi.py scope` |

Rij 1 en 2 dragen de gemeten cijfers uit `scripts/measure_fix_pr_kpi.py` (n=9 resp. n=10), niet de
5-7 uit het issue: dat laatste was een schatting. Acht gemeten beurten lagen aan de basis van het
ontwerp; het script telde er bij de definitieve meting negen respectievelijk tien — het script is
de bron, niet de tekst.

Rij 3 is de rekening van de ambient-keuze. Zonder die rij is "scherpe negatieve grens" een
bewering; met die rij is het een getal dat bij de volgende meting oploopt of niet.

Rij 4 is AC 7 als getal. Hij ziet alleen een `Skill`-call; een review die met de hand in dezelfde
beurt gebeurt telt niet mee.

## Bekende valkuilen

- **`gh pr checks` geeft exit 8 bij pending checks**, niet 0. Een blok met `set -e` valt daarop om.
- **`gh issue view <nr>` en `gh pr view <nr>` delen de nummerruimte niet.** Een "fix 596" kan een
  issue of een PR bedoelen; `gh pr view` is de test. In de acht gemeten beurten was het steeds
  een PR.
- **`gh issue list --json` breekt onder de rtk-proxy.** Gebruik `gh api`.
- **Een merge-conflict geeft geen fail-check.** `mergeStateStatus: DIRTY` is het signaal; het
  faallog-deel van het diagnoseblok blijft dan leeg en dat is correct.
- **Links: in chat absoluut, in bestanden relatief.** In een chat-antwoord moet een link naar
  een bestand dat alleen op de branch bestaat het volledige absolute worktree-pad dragen: de
  IDE resolvet chatlinks tegen de hoofdcheckout. In een `.md`-bestand juist relatief;
  `worktree-linkcheck.sh` blokkeert daar een absoluut pad.
- **De vault heeft geen `.git`.** Raakt de PR een vaultbestand, dan is `git status` daar zinloos.
- **De permission-classifier weigert in auto-mode `git worktree unlock` en `git worktree remove`
  op een gelockte worktree.** Ruim dan niet op; meld het.
