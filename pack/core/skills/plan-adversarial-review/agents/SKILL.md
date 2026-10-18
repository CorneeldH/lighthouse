---
name: paia-plan-adversarial-review
description: Laat een implementatieplan adversarieel reviewen door een agent uit een ándere modelfamilie (Codex, read-only, schone context) tegen de spec en de repo — ronde 1 een executability-review, daarna alleen scoped verificatie van de fixes. Gebruik na het committen van een plan en vóór subagent-driven-development. Triggers - /paia-plan-adversarial-review, "review dit plan", "tweede paar ogen op het plan". LET OP — een spec reviewen is paia-spec-adversarial-review; een diff reviewen is caveman-review.
metadata:
  domain: factory
---

# Plan Adversarial Review

Tweede paar ogen op een plan vóór SDD het zonder jou uitvoert. Jij blijft operator: de
reviewer levert bevindingen, jíj beslist wat er echt is; de gebruiker alleen waar de spec
in het geding is (stap 6).

## Waarom deze skill bestaat

De lekmeting (#970, `experiments/lekmeting-keten/REPORT.md`): van de 28 lekken hoorden er 21
in het plan gezien te worden. Sinds superpowers 6.3.0 beslist de SDD-controller conflicten
zelf en logt hij ze als `Ruling:` in de ledger; een slecht plan wordt dan een stapel rulings
die de gebruiker pas bij de PR ziet. Deze skill zit tussen `writing-plans` en SDD. Ontwerp:
`docs/developer/planning/specs/2026-09-25-plan-adversarial-review-design.md`.

Discovery en verificatie deelden lange tijd één mechanisme: elke ronde was een volledige
review, dus elke ronde vond ook nieuwe kritiek in plaats van te convergeren (#1066). Sinds
dan is alleen ronde 1 discovery; ronde 2 en verder verifiëren uitsluitend of de fixes uit de
vorige ronde standhouden.

## Wanneer

Vaste naad (`AGENTS.md`, *Vaste naden*): plan gecommit in `docs/developer/planning/plans/` →
deze skill → `/superpowers:subagent-driven-development`. Voor élk plan, ook een plan zonder
aparte spec ("issue #N is de spec"), tenzij de gebruiker bij de plan-naad overslaan koos
(`paia-handoff`).

Niet hiervoor: een spec (`/paia-spec-adversarial-review`), een diff (`/caveman-review`).

## Controller-run

Controller-runs bestaan alleen in Claude Code (`scripts/controller_launch.sh` start `claude -p`); hier geldt ook headless *meld en stop*.

## Wat hij deelt met de spec-review

Eén runner. `review.sh` hier is een wrapper om `../paia-spec-adversarial-review/review.sh`,
met de plan-prompt (`reviewer-prompt.md`) en het plan-schema (`findings.schema.json`, met een
verplicht `cat`-veld). Schone `CODEX_HOME` (`~/.codex-specreview/`), `--sandbox read-only`,
exitcodes 2/3/4/5 en de niveaukeuze (`high`; `max` per run als een gemiste blocker duur is,
nooit `xhigh`) staan in `paia-spec-adversarial-review/SKILL.md`, *Voorwaarden*. Lees die als
Codex hapert.

Vanaf ronde 2 draait een tweede wrapper op dezelfde runner: `verify.sh`, met de
verificatieprompt (`verifier-prompt.md`) en het verificatieschema (`verification.schema.json`).
Die zoekt niet opnieuw naar problemen; hij verifieert alleen of de fixes uit de vorige ronde
standhouden (zie *Workflow*, stap 5).

De reviewprompt (ronde 1) stelt zeven categorieën, elk een `cat`:

| `cat` | Wanneer |
|---|---|
| `spec_mismatch` | het plan wijkt materieel af van de spec |
| `dependency_order` | de taakvolgorde of een afhankelijkheid maakt uitvoering onmogelijk of verkeerd |
| `repo_falsehood` | het plan neemt iets over de huidige codebase aan dat niet klopt |
| `missing_artifact` | een bestand, interface, schema of functie bestaat nog niet op het punt waar het plan hem gebruikt |
| `verification_gap` | de tests of verificatiestappen bewijzen niet wat het plan claimt |
| `unverified_external_assumption` | uitvoering hangt af van een externe aanname die het plan nooit checkt |
| `likely_runtime_failure` | het plan bevat een concreet faalpad dat waarschijnlijk verkeerd gedrag oplevert |

Elke BLOCKER of MAJOR draagt drie delen: de concrete stap of omissie, bewijs uit spec, plan of
repo, en het concrete faalscenario als het plan ongewijzigd draait — zonder faalscenario is het
hoogstens een MINOR. Een verificatieronde voegt daar één achtste `cat` aan toe:
`fix_incomplete` (zie stap 5 en 6).

## Workflow

Shellvariabelen overleven de volgende Bash-call niet: noteer de paden die een stap print
en vul ze letterlijk in.

### 1. Preflight

```bash
command -v codex || echo "codex CLI ontbreekt"
codex login status   # verwacht: "Logged in using ChatGPT"
```

Ontbreekt Codex, meld het hard en stop: er is geen fallback buiten Claude Code (zie *Fallback
zonder Codex*). Niet stil doorgaan.

Staat het Codex-budget vooraf op, dan merk je dat bij de ronde: de runner checkt het vóór
elke ronde (#1093) en stopt met exit 6 (`CODEX_BUDGET: fallback — <reden>`), zonder
`codex exec`. Meld `reden` en `reset_at` uit `<OUT>.budget.json` (een gepaarde ronde 1 kent
dat niet: buiten Claude Code is er geen paar) en stop **vóór** de ronde; de
convergentie-uitkomst is `aborted`, zoals bij de limiet.

### 2. Plan bepalen

Argument → dat pad, repo-relatief. Geen argument → het nieuwste bestand in
`docs/developer/planning/plans/` (niet `index.md`); bevestig welk.

### 3. Attempt starten

```bash
uv run python scripts/plan_review_record.py start <plan>
```

Print de attempt-map `${TMPDIR:-/tmp}/plan-review/<chain-key>/<attempt>/` (hierna
`<ATTEMPT>`) en legt de commit vast die ronde 1 leest; op de Beelink staat de attempt meteen
ook in de datamap, zodat een review die nooit bij stap 9 komt toch de nieuwste is. Exit 2 bij
een plan met ongecommitte wijzigingen: commit eerst. Elke nieuwe review van hetzelfde plan, ook na een terugkeer via
`spec`, begint hier opnieuw. Een oude attempt-map hergebruik je nooit.

### 4. Spec(s) bepalen — jij, niet de reviewer

Uit de `**Spec:**`-regel van het plan:

| De regel noemt | Jij doet |
|---|---|
| een repo-relatief pad | zo doorgeven |
| een absoluut worktree-pad | herleiden naar repo-relatief: alles na `.claude/worktrees/<naam>/` |
| meerdere specs | allemaal doorgeven |
| "issue #N is de spec" | body ophalen (hieronder), dat bestand doorgeven |
| niets | melden en vragen; niet raden |

Issue als spec — de reviewer heeft geen GitHub, dus de body gaat als bestand mee:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<N>" --jq .body > <ATTEMPT>/issue-<N>.md
date -u +%Y-%m-%dT%H:%M:%SZ
```

`gh` altijd met `dangerouslyDisableSandbox: true` (`AGENTS.md`). Noteer de tijd: die gaat als
`--issue-fetched-at` naar het record (stap 9).

### 5. Ronde draaien — direct, op de voorgrond

**Gepaarde ronde 1 (#988): alleen in Claude Code.** Daar draaien in ronde 1 standaard Codex en
een Opus-subagent naast elkaar, en verifieert Opus de rondes 2 en 3 (besluit 2026-10-04,
[`REPORT.md`](../../../experiments/codex-fallback-paren/REPORT.md)). Hier bestaat die subagent
niet; elke ronde loopt via Codex, zoals hieronder.

De `spec-adversary`-agent bestaat alleen in Claude Code (`.claude/agents/`). Hier roep je de
runner zelf aan. Vóór élke ronde een snapshot van het plan:

```bash
cp <plan> <ATTEMPT>/round-<n>.snapshot.md
```

**Alleen ronde 1 is een review:**

```bash
.agents/skills/paia-plan-adversarial-review/review.sh <plan> 1 <ATTEMPT>/round-1.json \
  none [max] --context <spec-pad> [--context <spec-pad-2>]
```

**Ronde n ≥ 2 draait alleen als ronde n-1 een BLOCKER/MAJOR had met `accept` of `spec`.**
Geen fixes, geen ronde. Bouw dan eerst de fixes-diff:

```bash
git diff --no-index <ATTEMPT>/round-<n-1>.snapshot.md <plan> > <ATTEMPT>/round-<n>.fixes.diff
```

`git diff --no-index` eindigt met exit 1 als er verschil is; dat is geen fout. Kreeg een
bevinding in de vorige ronde `spec`, haal dan vóór de dispatch de issue-body opnieuw op naar
`<ATTEMPT>/issue-<N>.md` (zelfde `gh api`-regel als stap 4). Draai daarna:

```bash
.agents/skills/paia-plan-adversarial-review/verify.sh <plan> <n> <ATTEMPT>/round-<n>.json \
  <ATTEMPT>/round-<n-1>.json [max] --context <spec-pad> [--context <spec-pad-2>] \
  --context <ATTEMPT>/round-<n>.fixes.diff
```

`max` alleen als een gemiste blocker duur is (default: weglaten). Een ronde kost ~7 minuten op
`high` (~18 op `max`). Draai de runner en het record-script uit stap 9 buiten de sandbox van je
harnas (of vraag escalatie): de runner schrijft onder `~/.codex-specreview/` en start een
`codex exec` met netwerk, het record-script schrijft naar `~/.local/share/`. Zet de timeout van
de shell-call op minstens 20 minuten. Toon daarna de bevindingen gesorteerd op severity, met het
ID berekend **vóór** het sorteren (`r<n>-f<index>`, de positie in `findings[]`):

```bash
jq -r --arg round <n> '
  .findings | to_entries
  | map(.value + {id: "r\($round)-f\(.key + 1)"})
  | sort_by({"BLOCKER": 0, "MAJOR": 1, "MINOR": 2}[.severity])
  | .[]
  | "| \(.id) | \(.severity) | \(.cat) | \(.verifies // "") | \(.plan_section) | \(.problem) | \(.failure_scenario) | \(.evidence) | \(.proposed_fix) |"
' <ATTEMPT>/round-<n>.json
```

Kop: `| ID | Sev | Cat | Verifies | Section | Problem | Failure | Evidence | Proposed fix |`.
Direct daaronder, bij een verificatieronde:

```bash
jq -r '.verified // empty | "VERIFIED: " + (if length == 0 then "none" else join(", ") end)' \
  <ATTEMPT>/round-<n>.json
```

Vat niets samen; lees `review_markdown` alleen als een bevinding onduidelijk is.

**Draai je zelf in Codex**, dan is de reviewer dezelfde modelfamilie als de auteur. Zet de
banner uit *Fallback zonder Codex* boven de tabel, en geef het record in stap 9
`--review-mode codex_same_family` mee in plaats van `codex`.

### 6. Gate — jij volgt je aanbeveling, de spec is de grens

**Verplichte sub-skill:** `superpowers:receiving-code-review`. Niet blind fixen.

Per bevinding vorm je een aanbeveling en voer je die uit, **zonder de gebruiker te vragen**.
De gebruiker koos in de praktijk steeds de aanbevolen optie (#977: zes vragen in twee rondes,
zes keer `accept (Recommended)`); een vraag per bevinding kostte alleen tijd.

Vraag de gebruiker **alleen** als je aanbeveling de spec tegenspreekt:

- de fix laat het plan afwijken van wat de spec voorschrijft of uitsluit, of
- de beslissing is `spec` en de wijziging is meer dan een verduidelijking (dan gaat de spec
  sowieso terug naar `/paia-spec-adversarial-review`; niet in ronde 3: zie de `spec`-rij
  hieronder).

Een fix die het plan juist mét de spec in lijn brengt, is geen tegenspraak: gewoon doen (#977
r2-f1 beperkte een invariant tot wat de spec al uitzonderde). Twijfel je of iets de spec
tegenspreekt, dan vraag je.

Toon na elke ronde één tabel `ID | Beslissing | Reden`, zodat de gebruiker kan ingrijpen vóór
de volgende ronde. De beslissingen:

| Beslissing | Betekenis |
|---|---|
| `accept` | terecht, plan aanpassen |
| `reject` | onterecht, **met reden**; die reden gaat mee naar de volgende ronde |
| `spec` | terecht, maar de fout zit in de spec: spec aanpassen plus één regel in het plan. Is de wijziging meer dan een verduidelijking, stop dan en meld dat de spec terug moet naar `/paia-spec-adversarial-review`. Uitzondering: in ronde 3 stopt een `spec`-beslissing de keten niet; het punt gaat via `accepteer-open` zoals elk ander open punt |

Controleer elke `file:line` van een BLOCKER vóór `accept`: een reviewer die de repo verkeerd
leest, produceert overtuigende onzin. Een `reject` is net zo goed een aanbeveling, mét reden.
MINORs beslis je ook zelf, per MINOR `minor_yes` of `minor_no`, zonder batchvraag.

In een verificatieronde geldt een extra check: Elk geaccepteerd ID staat in `verified` of in `verifies` van een finding
(geaccepteerd = `accept` of `spec`). Ontbreekt er een, dan draai je
díe verificatieronde opnieuw (zelfde `n`, zelfde `.fixes.diff`, `round-<n>.json` overschreven).
Ontbreekt het ID dan weer, dan is de uitkomst `aborted` en noemt de handoff dat ID als open punt
— nooit `converged`. MINORs starten nooit een ronde.

Schrijf na **élke** ronde, ook de laatste, de beslissingen naar de sidecar
`<ATTEMPT>/round-<n>.decisions.json`, **nooit** in de ronde-JSON (die moet schema-geldig
blijven). Eén regel per bevinding, MINORs inbegrepen: bij `converged` en `round_limit`
weigert stap 9 een record met een bevinding zonder beslissing (exit 4); alleen `aborted` mag
gaten hebben.

```json
[
  {"finding_id": "r2-f1", "decision": "accept", "reason": "Taak 3 noemde de verkeerde functie"},
  {"finding_id": "r2-f2", "decision": "reject", "reason": "de cron draait wél op de Beelink, zie Taak 5", "same_as": "r1-f4"},
  {"finding_id": "r2-f3", "decision": "minor_yes", "reason": "MINOR-batch"}
]
```

- `finding_id` alleen uit de `ID`-kolom, elk precies één keer. Nooit zelf tellen in de
  gesorteerde tabel: het ID is de positie vóór het sorteren. Een ID dat niet in die ronde
  bestaat of dubbel staat, is in stap 9 een validatiefout.
- `decision`: `accept`, `reject`, `spec`, `accepted_open` (alleen bij `round_limit`, gezet door
  `accepteer-open` in stap 8, nooit met de hand), `minor_yes`, `minor_no`.
- `same_as`: het ID van dezelfde bevinding in een eerdere ronde; anders weglaten.

`review.sh` noemt de sidecar in het prior-blok van de volgende ronde.

### 7. Fixen

Alleen de `accept`s in het plan en de `spec`-verduidelijkingen in de spec. Geen code, geen
belendende bestanden. Commit na elke ronde: `docs(plan): verwerk plan-review ronde <n> (#<issue>)`.
Geen fixes = geen commit; maak nooit een lege commit. Die commit gaat vóór de snapshot van de
volgende ronde (stap 5).

### 8. Convergentie — drie uitkomsten

| `outcome` | Wanneer | Handoff |
|---|---|---|
| `converged` | geen open BLOCKER/MAJOR in de laatste ronde (zie *Open* hieronder); is die ronde een verificatie, dan zijn daarmee alle geaccepteerde fixes geverifieerd; stap 9 weigert anders (exit 4) | `/superpowers:subagent-driven-development <plan>` |
| `round_limit` | ronde 3 gehaald met open BLOCKERs/MAJORs. Een derde ronde alleen als een fix uit ronde 2 niet klopt | `/superpowers:subagent-driven-development <plan>` — doorgaan, nooit een vraag: eerst `accepted_open` vastleggen (zie *Open*) |
| `aborted` | de gebruiker zegt stop, óf een geaccepteerd ID ontbreekt ook na een herhaalde verificatieronde (stap 6) | géén SDD; de handoff noemt de open punten |

Draagt dezelfde bevinding (via de `verifies`-keten) in ronde 2 én 3 `fix_incomplete` →
structureel oplossen, geen ronde 4.

**Open** = een BLOCKER/MAJOR uit de laatste ronde zonder `reject` — ook een die `accept` kreeg
en in stap 7 gefixt is, want die fix heeft geen reviewer meer gezien. `converged` betekent: na
de laatste ronde staat niets open. Bij `round_limit` leg je de open punten vast en ga je door,
in deze volgorde:

1. `uv run python scripts/plan_review_record.py accepteer-open <ATTEMPT> --doc <plan>`
   (buiten de sandbox, zoals bij `start` en `write`). Dit herschrijft `round-3.decisions.json`
   (elk open punt wordt `accepted_open`) en zet het blok `## Open na review` onderaan het plan;
   een bestaand blok wordt vervangen, nooit verdubbeld. Exit 2 met op stderr `geen open
   BLOCKER/MAJOR in de laatste ronde: niets te accepteren` = er staat niets open: de uitkomst is
   dan `converged`, geen `round_limit`. Exit 2 zonder die melding is een aanroepfout: herstel
   de aanroep. Exit 4 (`ACCEPTEER-OPEN MISLUKT: …`): meld de melding letterlijk, herstel de
   oorzaak (pad, sidecar) en draai opnieuw. Lukt het dan nog niet, zet de open punten in de
   handoff en ga door naar de volgende fase; nooit de sidecar of het document met de hand
   aanpassen, nooit een vraag.
2. Commit het plan als `git diff --quiet -- <plan>` een verschil meldt:
   `docs(plan): leg open review-punten vast (#<issue>)`. Geen verschil (hervatte run) = geen
   commit.
3. Pas dan stap 9 (`write --outcome round_limit`).

Een tweede regel voor hetzelfde ID is een validatiefout in stap 9. Vraag de gebruiker niets:
`round_limit` is geen vraag, ook niet als er `spec`-beslissingen open staan.
Het record (stap 9) komt altijd eerst. Na het record volgt in een controller-run de
run-instructie (`controller.bij_rondegrens` in `lighthouse.toml`); bij `doorgaan` is dat de gewone handoff.
Interactief is `round_limit` nooit een vraag.

### 9. Record — alleen op de Beelink

Ná de laatste fix-commit, bij elke uitkomst:

```bash
uv run python scripts/plan_review_record.py write <ATTEMPT> \
  --outcome <converged|round_limit|aborted> --review-mode codex \
  --spec <spec-pad> [--spec <spec-pad-2>] [--level max] \
  --skill-dir .agents/skills/paia-plan-adversarial-review
```

Issue als spec: `--issue <N> --issue-body <ATTEMPT>/issue-<N>.md --issue-fetched-at <tijd>`
in plaats van `--spec`. Vanuit Codex zelf: `--review-mode codex_same_family` (stap 5).

| exit | Betekenis | Jij doet |
|---|---|---|
| 0 | record geschreven; het pad staat op stdout | pad melden |
| 3 | niet op de Beelink: geen record | luid melden dat deze keten buiten de meting valt, door naar stap 10 |
| 4 | schrijf- of validatiefout: alleen de stagingmap `.<attempt>.tmp/` blijft | de melding letterlijk doorgeven; is het een ontbrekende of foute beslissing, sidecar herstellen en `write` opnieuw; anders door naar stap 10 |

Het record is meetmateriaal voor een latere meting, de review zelf is het product: een
mislukt record blokkeert de handoff nooit.

### 10. Afsluiten

Meld: plan-pad, aantal rondes, telling `accept`/`reject`/`spec`, eindverdict van de laatste
ronde, `outcome`, en het record-pad of de melding uit stap 9. Eindig met `/paia-handoff`
volgens de tabel in stap 8.

## Fallback zonder Codex

Buiten Claude Code is er geen `general-purpose`-subagent om op terug te vallen. Ontbreekt
Codex, of zit hij aan zijn limiet (exit 5, `grep -o 'try again at [^"]*' <OUT>.events`), of is
zijn budget vooraf op (exit 6, `reden` en `reset_at` uit `<OUT>.budget.json`), meld dat met de
resettijd en stop **vóór** de ronde: een review door jezelf is geen tweede paar ogen.

De banner voor een review binnen één modelfamilie (zie stap 5):

> ⚠️ SAME-FAMILY — zelfde modelfamilie als de auteur van het plan.
> Vangt grondfouten (code die niet bestaat), vangt géén gedeelde blinde vlekken.

## Bekende valkuilen

- **Het ID na het sorteren tellen.** Dan wijst elke beslissing naar de verkeerde bevinding en
  klopt het record niet. Neem het ID uit de tabel.
- **Beslissingen in de ronde-JSON schrijven.** Maakt hem schema-ongeldig; altijd de sidecar.
- **Een oude attempt hervatten.** Een nieuwe review is een nieuwe `start`; de meting telt
  alleen de nieuwste attempt per keten.
- **Een verificatieronde dispatchen met `review.sh`.** Dat start opnieuw discovery, en dat is
  precies de lus die #1066 sloot.
