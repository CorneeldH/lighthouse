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

Herkenning: de prompt eindigt op `(issue #<nr>, controller-run)`, of `PAIA_CONTROLLER_RUN` staat
in de omgeving (`scripts/controller_launch.sh`); de tweede telt na een hervatting met een
antwoord, want die prompt draagt het achtervoegsel niet. Dan geldt, boven de stappen hieronder:

1. Geen `codex`: meteen *Fallback zonder Codex*, zonder vraag.
2. Stap 2: het plan komt als argument; de bevestiging vervalt.
3. Elk van de drie spec-vragen uit stap 6 wordt dezelfde `🤖 Vraag` in plaats van een
   interactieve: een fix waarmee het plan van de spec afwijkt, een `spec`-beslissing die meer is
   dan een verduidelijking, en twijfel of iets de spec tegenspreekt. De vraag volgt
   `scripts/controller_run_instructies.md` en geeft het punt met twee opties. *Spec aanpassen*: de
   hervatte run past de spec aan volgens het antwoord, commit, en post de handoff
   `volgende: /paia-spec-adversarial-review <absoluut specpad>` (anders stuurt `paia-handoff` het
   ongereviewde plan terug naar plan-review en draait de keten rond). *Door met het punt open*:
   de review loopt door en het punt komt onder `Let op:` in de handoff naar SDD. In de sidecar
   krijgt het punt `"decision": "reject"` met `"reason": "open op verzoek (🤖 Vraag)"`: plan-review
   kent geen `defer`, `accept`/`spec` laat de volgende verificatieronde een fix verwachten, en een
   punt zonder beslissing laat `write --outcome converged` falen (exit 4), zodat het record
   verloren gaat.
   De run past de spec nooit uit zichzelf materieel aan.
4. Bij `aborted` (stap 8): eerst het record (`--outcome aborted`, stap 9), dan een `🤖 Vraag` met
   de open punten en twee opties: *Door naar SDD met de open punten* (handoff
   `volgende: /superpowers:subagent-driven-development <absoluut planpad>`, open punten onder
   `Let op:`, geen tweede `write`, geen `accepteer-open`) of *Interactief verder* (handoff
   `volgende: /superpowers:executing-plans <absoluut planpad>`: de controller geeft het issue
   terug). Geen optie *nog een ronde*.
5. Achtergrondtaken: de alinea **Controller-run:** in *Ronde 1: Codex en Opus*.

## Wat hij deelt met de spec-review

Eén runner. `review.sh` hier is een wrapper om `../paia-spec-adversarial-review/review.sh`,
met de plan-prompt (`reviewer-prompt.md`) en het plan-schema (`findings.schema.json`, met een
verplicht `cat`-veld). Schone `CODEX_HOME` (`~/.codex-specreview/`), `--sandbox read-only`,
exitcodes 2/3/4/5 en de niveaukeuze (`high`; `max` per run als een gemiste blocker duur is,
nooit `xhigh`) staan in `paia-spec-adversarial-review/SKILL.md`, *Voorwaarden*. Lees die als
Codex hapert.

Vanaf ronde 2 verifieert een Opus-subagent, met de verificatieprompt (`verifier-prompt.md`) en
het verificatieschema (`verification.schema.json`). Die zoekt niet opnieuw naar problemen; hij
verifieert alleen of de fixes uit de vorige ronde standhouden (zie *Workflow*, stap 5).

Codex draait dus alleen in ronde 1, en daar altijd naast Opus. Dat is het besluit van
2026-10-04 (#988), met [`REPORT.md`](../../../experiments/codex-fallback-paren/REPORT.md) als
bron: de twee reviewers vullen elkaar in ronde 1 aan, en de verificatierondes op Opus sparen
het Codex-quotum. Buiten Claude Code is er geen Opus-subagent; daar verifieert `verify.sh`
(dezelfde runner) via Codex.

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

Ontbreekt Codex, meld het hard en vraag of de fallback mag (onderaan); niet in een controller-run. Niet stil doorgaan.

Staat het Codex-budget vooraf op, dan merk je dat pas bij ronde 1: de runner checkt het vóór
de ronde (#1093), schrijft het besluit naar `<OUT>.budget.json` en stopt met **exit 6**
zonder `codex exec`. Dat is geen preflight-fout om te melden en te stoppen: ronde 1 loopt dan
Opus-only, zie *Fallback zonder Codex*.

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

### 5. Ronde dispatchen — op de achtergrond

Vóór élke dispatch een snapshot van het plan: `cp <plan> <ATTEMPT>/round-<n>.snapshot.md`.

**Alleen ronde 1 is een review**, en die is altijd gepaard: Codex en Opus naast elkaar (*Ronde 1:
Codex en Opus*, hieronder). Alleen de herkansing uit de tabel daar dispatcht de
`spec-adversary`-agent zonder paar, met:

- `SPEC` = het plan (repo-relatief)
- `ROUND` = `1`, `OUT` = `<ATTEMPT>/round-1.json`
- `PRIOR` = `none`
- `SKILL_DIR` = `.claude/skills/paia-spec-adversarial-review`
- `REVIEW_SH` = `.claude/skills/paia-plan-adversarial-review/review.sh`, precies zo: de
  allowlist-regel in `.claude/settings.json` matcht op dat voorvoegsel
- `CONTEXT` = de spec(s) uit stap 4
- `LEVEL` alleen als je `max` wilt

**Ronde n ≥ 2 draait alleen als ronde n-1 een BLOCKER/MAJOR had met `accept` of `spec`.**
Geen fixes, geen ronde. Bouw dan eerst de fixes-diff:

```bash
git diff --no-index <ATTEMPT>/round-<n-1>.snapshot.md <plan> > <ATTEMPT>/round-<n>.fixes.diff
```

`git diff --no-index` eindigt met exit 1 als er verschil is; dat is geen fout. Kreeg een
bevinding in de vorige ronde `spec`, haal dan vóór de dispatch de issue-body opnieuw op naar
`<ATTEMPT>/issue-<N>.md` (zelfde `gh api`-regel als stap 4) — anders ziet de verificateur de
spec-kant van de fix niet.

Dispatch daarna de verificateur: de subagent uit *Opus-dispatch* (onderaan), met
`verifier-prompt.md`, `PRIOR` = `<ATTEMPT>/round-<n-1>.json`, als context de spec(s) uit stap 4
plus `<ATTEMPT>/round-<n>.fixes.diff`, en `OUT` = `<ATTEMPT>/round-<n>.json`. Altijd Opus, ook
als Codex nog budget heeft: Codex blijft gespaard voor ronde 1. Geen banner: dit is de regel,
geen fallback.

Laat hem op de achtergrond lopen (de default van de Agent-tool). De ID-tabel maak je zelf met
het jq-fragment uit *Opus-dispatch*: per bevinding een `ID` (`r<n>-f<index>`), een `Cat` en een
`Verifies`.

#### Ronde 1: Codex en Opus (#988)

In ronde 1 draaien Codex en een Opus-reviewer naast elkaar, op dezelfde commit; jij beslist
blind. Begonnen als tijdelijke meting, sinds 2026-10-04 de standaard bij plan én spec: in 15
plan-paren vond Codex 32 en Opus 29 terechte BLOCKER/MAJOR die de ander miste, tegen 26
gedeelde, en geen enkele gepaarde keten lekte (0/13). Cijfers in
[`REPORT.md`](../../../experiments/codex-fallback-paren/REPORT.md), opzet en grenzen in
[`RECEPT.md`](../../../experiments/codex-fallback-paren/RECEPT.md).

Controleer eerst dat `git status --porcelain` leeg is. Is hij dat niet, commit of ruim eerst
op: `start` toetst alleen het plan, maar `blind` weigert een werkboom die niet schoon is, en dan
is het paar verloren. Dispatch daarna in **één bericht** twee achtergrondreviewers, op dezelfde
commit en na de snapshot.

**Controller-run:** eindig geen antwoord zolang een reviewer loopt, ook niet de
verificateur vanaf ronde 2 of de herkansing; wacht op elke melding. Wat na het laatste antwoord
nog loopt, sterft met de sessie (`scripts/controller_run_instructies.md`). Headless bewezen op
2026-10-11 (`deploy/beelink/controller-tick-verification.md`, #1092).

1. **Codex**, via een Bash-call met `run_in_background: true` en
   `dangerouslyDisableSandbox: true`:

   ```bash
   .claude/skills/paia-plan-adversarial-review/review.sh <plan> 1 <ATTEMPT>/round-1.codex.json \
     none [max] --context <spec-pad> [--context <spec-pad-2>] > <ATTEMPT>/round-1.codex.log 2>&1; echo "exit=$?"
   ```

   `[max]` is het `LEVEL`-slot: laat het weg, of schrijf `max` als je dat wilt, net als `LEVEL`
   hierboven. Dan krijgt het record in stap 9 `--level max`.

   Geen `spec-adversary`: die geeft een tabel terug, en dan is beslissen niet meer blind. De
   omleiding is om dezelfde reden verplicht. Bij succes print `review.sh` het aantal bevindingen
   en de severity-verdeling (de `ok | reviewer: …`-regel). Die telling naast de A/B-tabel
   verraadt welke letter Codex is. Lees alleen de exitcode, nooit `round-1.codex.log`.
2. **Opus**: de subagent uit *Opus-dispatch*, met `OUT` = `<ATTEMPT>/round-1.fallback.json`.
   De prompt vraagt hem alleen `KLAAR <pad> model=<exact model-ID uit je eigen systeemprompt>`
   terug te geven: geen bevindingen, geen telling. `model: "opus"` is een alias. Neem de gemelde ID dus over als
   `--shadow-model` (stap 9) en schrijf hem nooit zelf in. Meldt hij een model waarvan
   `canonical_model` niet `SHADOW_MODEL` geeft, schrijf hem dan toch zo weg; `tally` telt dat
   paar niet mee. Draai voor deze dispatch geen jq-tabel en zet geen banner vóór `blind`: de
   jq-tabel uit *Opus-dispatch* leest het ruwe bestand, en dan is beslissen niet meer blind.
   `blind` maakt de tabel.

Tussen de dispatch en `blind` schrijf je niets in de worktree: geen edit, geen commit. Beide
reviewers lezen de live werkboom. `blind` weigert als `HEAD` verzet is of de werkboom niet
schoon is, ongevolgde bestanden inbegrepen. Een wijziging die je vóór `blind` terugdraait, ziet
hij niet. De regel geldt dus ook als je iets "even" terugzet.

Zijn beide klaar, draai dan `uv run python scripts/review_pair.py blind <ATTEMPT>`
(`dangerouslyDisableSandbox: true`) en beslis op de A/B-tabel. Lees `pair.json`, de ruwe
bestanden en `round-1.codex.log` niet. Stap 6 geldt ongewijzigd, met de blinde ID's.

Lukt het paar niet, dan is er geen paar: geen `merge`, geen `--shadow-model`. De ID-tabel komt
dan van het jq-fragment uit *Opus-dispatch*, op `round-1.json`; met de banner uit *Fallback
zonder Codex* als `round-1.json` van Opus komt, zonder als hij van Codex komt. Een budget dat
vooraf op was herken je aan exit 6 en `<ATTEMPT>/round-1.codex.json.budget.json` (`reden`,
`reset_at`); een limiet midden in de ronde aan exit 5 plus `grep -o 'try again at [^"]*' <ATTEMPT>/round-1.codex.json.events`
(niet `round-1.json.events`).

| Situatie | `round-1.json` wordt | `--review-mode` |
|---|---|---|
| Codex exit 6 (budget vooraf op) | `mv <ATTEMPT>/round-1.fallback.json <ATTEMPT>/round-1.json` | `fallback --reviewer-model <gemeld model>` |
| Codex exit 5 met resettijd | `mv <ATTEMPT>/round-1.fallback.json <ATTEMPT>/round-1.json` | `fallback --reviewer-model <gemeld model>` |
| Codex faalt anders (exit 2/3/4, of 5 zonder resettijd) | `mv <ATTEMPT>/round-1.fallback.json <ATTEMPT>/round-1.json` | `fallback --reviewer-model <gemeld model>` |
| Opus-subagent faalt of schrijft een ongeldig bestand | `mv <ATTEMPT>/round-1.codex.json <ATTEMPT>/round-1.json` | `codex` |
| Beide falen, of beide ruwe bestanden zijn ongeldig | geen; draai ronde 1 één keer opnieuw als gewone stap 5 (zonder paar) | volgens die herkansing; faalt ook die, dan stop je met `--outcome aborted` en meld je beide fouten |
| `blind` geeft exit 4 op een kapot ruw bestand | het andere, geldige bestand, zoals in de twee rijen hierboven | idem |
| `blind` geeft exit 4 met `werkboom veranderd` | `mv <ATTEMPT>/round-1.codex.json <ATTEMPT>/round-1.json`; de Opus-uitvoer vervalt | `codex` |
| `pair.json` of een ruw bestand gelezen vóór `merge` | `mv <ATTEMPT>/round-1.codex.json <ATTEMPT>/round-1.json`; de blinde beslissingen vervallen, beslis opnieuw op de r1-ID's | `codex` |

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

**Gepaarde ronde 1** (stap 5, *Ronde 1: Codex en Opus*):

- Schrijf `<ATTEMPT>/round-1.blind-decisions.json` in plaats van `round-1.decisions.json`, met
  de blinde ID's (`A-f<i>`, `B-f<i>`).
- Een dubbele bevinding volgens het
  [recept](../../../experiments/codex-fallback-paren/RECEPT.md) (*"Hetzelfde"*) is één rij
  met `also`: `{"finding_id": "A-f2", "also": "B-f1", "decision": "accept", "reason": "…"}`.
- Draai daarna `uv run python scripts/review_pair.py merge <ATTEMPT>`
  (`dangerouslyDisableSandbox: true`). Die schrijft `round-1.json`, `round-1.decisions.json`
  en `round-1.sources.json`, en toont de ontblinde tabel met de `r1-f<i>`-ID's.
- Exit 4 op een beslissing: herstel de sidecar en draai `merge` opnieuw. Exit 4 op een kapot
  ruw bestand: geen paar (de tabel in stap 5).
- Ronde 2 leest `round-1.json` als `PRIOR`, zoals altijd.

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
  --spec <spec-pad> [--spec <spec-pad-2>] [--level max]
```

`start` (stap 3) en `write` schrijven buiten de working directory
(`${TMPDIR:-/tmp}/plan-review/` resp. `~/.local/share/lekmeting-keten/`); draai ze onder een
actieve sandbox met `dangerouslyDisableSandbox: true`, anders eindigt `write` in exit 4 en is
het record kwijt.

Issue als spec: `--issue <N> --issue-body <ATTEMPT>/issue-<N>.md --issue-fetched-at <tijd>`
in plaats van `--spec`.

`--review-mode` zegt wie ronde 1 reviewde:

| Ronde 1 | Vlaggen |
|---|---|
| gepaard (er is een `round-1.sources.json`) | `--review-mode codex --shadow-model <model dat Opus in ronde 1 meldde>` |
| alleen Codex (Opus faalde, of `blind` weigerde) | `--review-mode codex` |
| alleen Opus (Codex faalde, *Fallback zonder Codex*) | `--review-mode fallback --reviewer-model <gemeld model>` |

De verificatierondes op Opus veranderen `--review-mode` niet: ze zijn de regel, geen fallback,
en `round_kinds` in het record markeert ze. Zonder paar geen `--shadow-model`; `write` weigert
beide verkeerde kanten met exit 4.

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

Alleen ronde 1 draait op Codex, dus alleen ronde 1 kent een fallback. Drie aanleidingen: geen
`codex` (vraag eerst, stap 1; niet in een controller-run), het budget vooraf op (exit 6), of een Codex-limiet midden in de
ronde (exit 5 + `try again at`). Ronde 1 loopt dan Opus-only: de Opus-kant van het paar wordt
`round-1.json` (de tabel in stap 5). Ontbrak `codex` al bij de preflight, dan is er geen paar
gedispatcht: één *Opus-dispatch* met `OUT` = `<ATTEMPT>/round-1.json`.

- **Exit 6** herken je aan `CODEX_BUDGET: fallback — <reden>` op stderr en het besluit in
  `<ATTEMPT>/round-1.codex.json.budget.json`. **Niet vragen en niet wachten.** Meld één regel
  met `reden` en `reset_at` uit dat bestand.
- **Exit 5** herken je aan een resettijd in de events
  (`grep -o 'try again at [^"]*' <ATTEMPT>/round-1.codex.json.events`; `.err` toont alleen
  `Reading additional input from stdin...`). Ook dan **niet vragen en niet wachten**. Meld de
  resettijd in één regel.

Noem in het eindverslag "vooraf (exit 6)" of "midden in de ronde (exit 5)".

Zet boven de tabel van een Opus-only ronde 1:

> ⚠️ SAME-FAMILY FALLBACK — zelfde modelfamilie als de auteur van het plan.
> Vangt grondfouten (code die niet bestaat), vangt géén gedeelde blinde vlekken.

Het record krijgt `--review-mode fallback --reviewer-model <modelnaam van de subagent>` (stap 9).

## Opus-dispatch

Eén dispatch, drie rollen: de Opus-kant van het paar in ronde 1, de fallback als Codex ronde 1
niet haalt, en de verificateur in elke ronde ≥ 2.

Dispatch een `general-purpose`-subagent met `model: "opus"` en de tekst tussen
`---BEGIN PROMPT---` en `---END PROMPT---` uit `reviewer-prompt.md` (ronde 1) of
`verifier-prompt.md` (ronde ≥ 2), de placeholders met de hand ingevuld: `{{CONTEXT_PATHS}}` als
regels `- <pad>`, en `{{PRIOR_REVIEW_BLOCK}}` leeg in ronde 1 en vanaf ronde 2 de alinea die
`review.sh` als `prior_block` bouwt, met `PRIOR` en zijn `.decisions.json`-sidecar ingevuld.
Geef hem de opdracht het JSON-antwoord volgens `findings.schema.json` resp.
`verification.schema.json` naar `OUT` te schrijven. Er is dan geen `spec-adversary` die de
`ID`-kolom maakt; bereken hem zelf, **vóór** het sorteren. In een gepaarde ronde 1 sla je deze
tabel over (stap 5): `blind` maakt de tabel.

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

## Bekende valkuilen

- **Het ID na het sorteren tellen.** Dan wijst elke beslissing naar de verkeerde bevinding en
  klopt het record niet. Neem het ID uit de tabel.
- **Beslissingen in de ronde-JSON schrijven.** Maakt hem schema-ongeldig; altijd de sidecar.
- **`REVIEW_SH` absoluut maken.** De allowlist matcht op het repo-relatieve voorvoegsel; een
  absoluut pad laat de achtergrondronde wachten op een approval.
- **Een oude attempt hervatten.** Een nieuwe review is een nieuwe `start`; de meting telt
  alleen de nieuwste attempt per keten.
- **Een verificatieronde dispatchen met `reviewer-prompt.md` of `review.sh`.** Dat start
  opnieuw discovery, en dat is precies de lus die #1066 sloot.
- **Een verificatieronde naar Codex sturen.** Codex is voor ronde 1; de verificatie draait op
  Opus, ook als het Codex-budget nog ruim is.
- **`pair.json` of een ruw bestand lezen vóór `merge`.** Dan beslis je niet meer blind en is het
  paar waardeloos; volg de laatste rij van de tabel in stap 5.
- **De uitvoer van de Codex-call in het gesprek halen.** Die hoort in `round-1.codex.log`; de
  telling in de `ok`-regel verraadt de letter.
