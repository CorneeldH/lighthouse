---
name: paia-spec-adversarial-review
description: Laat een spec adversarieel reviewen door een agent uit een ándere modelfamilie (Codex, read-only, schone context) tegen de repo — ronde 1 een review, daarna alleen scoped verificatie van de fixes. Gebruik na het schrijven van een spec en vóór writing-plans. Triggers - /paia-spec-adversarial-review, "review deze spec", "tweede paar ogen op de spec", "adversarial review".
metadata:
  domain: factory
---

# Spec Adversarial Review

Tweede paar ogen op een spec vóórdat er een plan uit rolt. Jij blijft operator:
de reviewer levert findings, jíj beslist wat er echt is.

## Waarom deze skill bestaat

Superpowers' `brainstorming` (7.0) dispatcht vóór de overdracht een **builder check**
(`skills/brainstorming/builder-check-prompt.md`): een verse subagent leest de design als
degene die hem moet bouwen en meldt alleen de vragen waar een verkeerde gok ertoe doet. Hij
vraagt *waar moet ik gokken?*, terwijl de gebruiker er nog bij is, en krijgt alleen het
document: hij toetst niets tegen de codebase.

Een eerste zeef, geen toets tegen de code. Deze skill is de tweede zeef: hij vraagt *klopt
dit met de échte code?*, en draait ná de commit.

| Laag | Wie | Vangt |
|---|---|---|
| 1 | superpowers builder check, verse subagent, vóór de overdracht | gokplekken die alleen de gebruiker kan beslissen |
| 2 | deze skill: Codex en Opus naast elkaar in ronde 1, schone context, read-only | klopt de spec met de échte code, wat breekt bij implementatie |

Laag 1 vervangen we niet: die loopt toch al in brainstorming en vangt iets anders.

Discovery en verificatie deelden lange tijd één mechanisme: elke ronde was een volledige
review, dus elke ronde vond ook nieuwe kritiek in plaats van te convergeren (#1066). Sinds dan
is alleen ronde 1 discovery; ronde 2 en verder verifiëren uitsluitend of de fixes uit de vorige
ronde standhouden.

Sinds 2026-10-04 (#988) draait ronde 1 altijd gepaard: Codex en Opus naast elkaar, blind
beslist. Bij de spec vond Opus meer (27 tegen 13 unieke terechte B/M in 8 paren). Toch had
ook de plan-review naar schatting zes van de 13 Codex-only vangsten niet opgevangen, waaronder
security. De verificatierondes draaien op Opus, zodat Codex gespaard blijft
voor ronde 1. Cijfers: [`REPORT.md`](../../../experiments/codex-fallback-paren/REPORT.md).

## Wanneer

- Spec geschreven (`docs/developer/planning/specs/`), vóór `superpowers:writing-plans`.
- Ook goed: een spec die van iemand anders komt en waar je op moet bouwen.

Niet hiervoor: code/diff reviewen (dat is `/caveman-review`), of een plan
reviewen (dat is `/paia-plan-adversarial-review`).

## Controller-run

Herkenning: de prompt eindigt op `(issue #<nr>, controller-run)`, of `PAIA_CONTROLLER_RUN` staat
in de omgeving (`scripts/controller_launch.sh`); de tweede telt na een hervatting met een
antwoord, want die prompt draagt het achtervoegsel niet. Dan geldt, boven de stappen hieronder:

1. Geen `codex`: meteen *Fallback zonder Codex*, zonder vraag (exit 5 en 6 doen dat al).
2. Stap 1: het pad komt als argument; de bevestiging vervalt.
3. Stap 3: je beslist de tabel en de stapels zelf, ook elke MINOR, en meldt de keuzes (tenet C). Een vondst
   buiten de scope wordt een regel onder *Open voor het plan* of een voorstel voor een eigen issue
   in de handoff, geen vraag (tenet B).
4. Een inhoudelijke vraag die geen tenet of areablok beslist: een genummerde lijst als laatste
   tekst, volgens `scripts/controller_run_instructies.md`; de controller post hem als `🤖 Vraag`
   en hervat de sessie met het antwoord.
5. Bij `converged` of `round_limit`: `/paia-handoff` met
   `volgende: /superpowers:writing-plans <absoluut specpad>`.
6. Bij `aborted`: eerst het record (`--outcome aborted`, stap 5), dan een `🤖 Vraag` met de open
   punten en twee opties. *Door naar writing-plans met de open punten*: handoff
   `volgende: /superpowers:writing-plans <absoluut specpad>`, open punten onder `Let op:`. Geen
   tweede `write`, geen `accepteer-open`. *Interactief verder*: handoff
   `volgende: /superpowers:executing-plans <absoluut specpad>` (het bestaande teruggeefpad: de controller post
   `🤖 Teruggegeven`, de Status blijft `Controller`, de gebruiker zet hem zelf af). Alleen
   `volgende:` draagt executing-plans, als teruggeefsignaal; de `Eerste stap:` van het blok is dan
   `/superpowers:writing-plans <absoluut specpad>` (of een interactieve spec-review), want er is
   nog geen plan om uit te voeren. *Nog een
   ronde* is geen optie: dat botst met de rondegrens en een oude attempt hergebruik je nooit.
7. Achtergrondtaken: de alinea **Controller-run:** in
   [`paia-plan-adversarial-review`](../paia-plan-adversarial-review/SKILL.md), *Ronde 1: Codex en
   Opus*.

## Voorwaarden

```bash
command -v codex || echo "codex CLI ontbreekt"
codex login status   # verwacht: "Logged in using ChatGPT"
```

**Zo staat hij op de Beelink geïnstalleerd.** `@openai/codex` globaal via npm met prefix
`~/.local` (binary `~/.local/bin/codex`, staat op PATH), ingelogd via **ChatGPT Plus**. Deze
codex-versie kent geen `--device-auth`-vlag, maar kaal `codex login` opent een
`localhost:1455`-callback die je op het scherm van de Beelink afmaakt. De auth landt in
`~/.codex/auth.json` (access + refresh token).

Dat een ChatGPT-abonnement de reviewer draait is geen toevalligheid maar de bedoeling: een
Claude-*abonnements*token gebruiken voor geautomatiseerde agent-review valt buiten wat Anthropic
sanctioneert, en het is bovendien dezelfde modelfamilie — precies wat deze skill wil vermijden.

De reviewer draait in een **eigen, schone `CODEX_HOME`** (`~/.codex-specreview/`),
niet in je gewone `~/.codex`. Twee redenen, en de tweede weegt het zwaarst:

- **Context.** Je gewone Codex-home laadt marketplaces, ~20 plugins, MCP-servers,
  hooks en agent-definities mee. Gemeten op dezelfde triviale prompt: 15.544
  tokens uit `~/.codex` tegen 7.502 uit de schone home — overhead vóór de spec
  ook maar in beeld is.
- **Schone context ís de claim van deze skill.** Een reviewer die begint met
  caveman-mode, jouw skill-catalogus en een handvol workflow-guards in zijn
  prompt oordeelt niet onbevangen. `--ignore-user-config` is níét genoeg: dat
  negeert alleen `config.toml` en laat `~/.codex/agents/` staan.

Model en reasoning-effort staan **niet** in deze skill — die horen bij jouw
Codex-abonnement. Ze staan in `~/.codex-specreview/config.toml`:

```toml
model = "gpt-5.6-sol"
model_reasoning_effort = "high"
```

### Waarom `high`, en wanneer `max`

Gemeten, niet gegokt — 27 runs over drie specs, zie
[`experiments/spec-review-levels/REPORT.md`](../../../experiments/spec-review-levels/REPORT.md):

| niveau | redeneerkosten t.o.v. `high` | unieke reproduceerbare claims |
|---|---|---|
| `high` | 1× | de basislijn |
| `xhigh` | 1,3× | **nul**, op geen van de drie specs |
| `max` | 2,6× (en ~2,5× doorlooptijd) | twee over drie specs, één overeind na oordeel |

`xhigh` is dominated: duurder dan `high` en vindt niets extra. Gebruik het niet.

`max` kan iets wat `high` niet kan, maar te weinig om elke review mee te betalen.
Zet het per run in als een gemiste blocker duur is — infra die je niet makkelijk
terugdraait, migraties, iets dat live gaat:

```bash
CODEX_HOME=~/.codex-specreview codex exec -c model_reasoning_effort=max ...
```

Raak `config.toml` daarvoor niet aan; de vlag overrulet het profiel per aanroep.

### Gestructureerde output

`findings.schema.json` (naast dit bestand) dwingt het eindantwoord af via
`codex exec --output-schema`. Het schema begint met een vrij `review_markdown`-veld
en pas daarna de gevalideerde velden — die volgorde is functioneel, niet cosmetisch:
gestructureerde generatie volgt de schemavolgorde, dus de reviewer redeneert eerst
vrij en structureert daarna. Formaatdwang verlaagt anders het redeneervermogen
(arXiv:2408.02442), en bij een adversarial reviewer is dat precies het verkeerde
om weg te bezuinigen.

Gevolg voor de agent: comprimeren wordt sorteren. Er komt geen tweede LLM-oordeel
meer tussen de reviewer en jou.

De agent maakt die home aan als hij ontbreekt en kopieert `auth.json` uit
`~/.codex` bij elke ronde, zodat een ververste token meekomt. Model wisselen =
alleen dat bestand aanpassen.

> Geen `[profiles.specreview]`-blok in `~/.codex/config.toml` zetten: dat is in
> Codex ≥ 0.148 een harde startfout. De v2-vorm (los `~/.codex/<naam>.config.toml`
> + `--profile`) werkt wél, maar erft de vervuiling van de basis-config — vandaar
> de aparte home in plaats van een profiel.

## Workflow

### 0. Preflight

Geen `codex` gevonden? Meld het hard en vraag of je door mag in
**fallback-modus** (zie onder); niet in een controller-run. Ga niet stilzwijgend door — dan verlies je
precies de eigenschap waar deze skill om draait.

Codex draait alleen in ronde 1, dus alleen ronde 1 kan op een Codex-limiet stuiten.

**Codex-budget vooraf op = exit 6, meteen fallback.** De runner checkt het Codex-budget vóór
de ronde (#1093) en schrijft het besluit naar `<OUT>.budget.json`. Staat het budget op
(`CODEX_BUDGET: fallback — <reden>` op stderr), dan stopt hij met exit 6 zonder `codex exec`.
Ronde 1 loopt dan Opus-only (zie *Fallback zonder Codex*): geen vraag aan de gebruiker, geen
wachten. Meld één regel met `reden` en `reset_at` uit `<OUT>.budget.json`. Een check die niet
meetbaar is laat de ronde gewoon door naar Codex.

**Codex-limiet midden in de ronde = ook meteen fallback, niet wachten.** Faalt ronde 1 met
exit 5, kijk dan in de events: `grep -o 'try again at [^"]*' <OUT>.events`. Staat daar een
resettijd, dan loopt ronde 1 Opus-only: geen vraag aan de gebruiker, geen timer tot de reset.
De ChatGPT Plus-limiet valt vaak midden in een reviewloop; wachten kost uren, de fallback
minuten. Meld de resettijd in één regel, en in het eindverslag "vooraf (exit 6)" of "midden in
de ronde (exit 5)".
(`.err` toont alleen `Reading additional input from stdin...`; de echte fout staat in
`.events`.)

### 1. Bepaal de spec

Argument gegeven → dat pad. Geen argument → nieuwste bestand in
`docs/developer/planning/specs/`, en bevestig welke je pakt.

Start dan een attempt (`dangerouslyDisableSandbox: true`, het script schrijft buiten de
working directory):

```bash
uv run python scripts/plan_review_record.py start --kind spec <spec>
```

Hij print de attempt-map (hierna `<ATTEMPT>`) en legt de commit vast die ronde 1 leest. Exit 2
bij een spec met ongecommitte wijzigingen: commit eerst. Shellvariabelen overleven de volgende
Bash-call niet, dus vul het pad letterlijk in. Elke nieuwe review begint hier opnieuw; een oude
attempt hergebruik je nooit.

Rondes landen in `<ATTEMPT>/round-<n>.json`, vanaf ronde 2 met `round-<n-1>.snapshot.md` en
`round-<n>.fixes.diff` (stap 2). Niet in de repo: de spec is het artefact, de review is
gereedschap. Tussen rondes hoeft de spec niet gecommit te zijn — de snapshot vergelijkt de
working tree, niet git.

### 2. Ronde draaien — op de achtergrond

Vóór élke dispatch een snapshot van de spec:

```bash
cp <spec> <ATTEMPT>/round-<n>.snapshot.md
```

**Alleen ronde 1 is een review**, en die is altijd gepaard (*Ronde 1: Codex en Opus*,
hieronder). Alleen de herkansing uit de tabel daar dispatcht de `spec-adversary` agent zonder
paar, met `SPEC`, `ROUND` = `1`, `OUT` = `<ATTEMPT>/round-1.json`, `PRIOR` = `none`,
`SKILL_DIR` = de map van dit bestand, en eventueel `LEVEL`.

**Ronde n ≥ 2 draait alleen als ronde n-1 een BLOCKER/MAJOR had met `accept`.** `defer` telt
niet. Geen fixes, geen ronde. Bouw dan eerst de fixes-diff:

```bash
git diff --no-index <ATTEMPT>/round-<n-1>.snapshot.md <spec> > <ATTEMPT>/round-<n>.fixes.diff
```

`git diff --no-index` eindigt met exit 1 als er verschil is; dat is geen fout.

Dispatch daarna de verificateur: de subagent uit *Opus-dispatch* (onderaan), met
`verifier-prompt.md`, `PRIOR` = `<ATTEMPT>/round-<n-1>.json`, als context
`<ATTEMPT>/round-<n>.fixes.diff`, en `OUT` = `<ATTEMPT>/round-<n>.json`. Altijd Opus, ook als
Codex nog budget heeft: Codex blijft gespaard voor ronde 1. Geen banner: dit is de regel, geen
fallback. De verificateur zoekt niet opnieuw naar problemen; hij verifieert alleen of de fixes
van de vorige ronde standhouden.

**Laat elke reviewer op de achtergrond lopen** (dat is de default van de Agent-tool): een
Codex-ronde kost ~7 minuten op `high` en ~18 op `max`. Je krijgt een melding zodra hij klaar
is; blijf ondertussen gewoon doorwerken.

De tabel draagt per bevinding een `ID`: `r<ronde>-f<index>`, de positie in `findings[]`
vóór het sorteren. Beslissingen verwijzen alleen naar dat ID (stap 3); een verificatieronde
draagt ook een `Verifies`.

Codex loopt via `review.sh`: één vaste commandovorm die de promptsubstitutie, de schone
`CODEX_HOME`, `--sandbox read-only` en `--output-schema` afhandelt. De ruwe review blijft in
`OUT`; lees dat alleen als een finding onduidelijk is. `verify.sh` (naast dit bestand) is een
dunne wrapper om `review.sh` met de verificatieprompt en het verificatieschema; die gebruikt
alleen de `.agents`-spiegel, waar geen Opus-subagent bestaat.

**Eenmalig instellen, anders valt de achtergrondronde stil op een prompt.** Codex
kan niet binnen de Claude-sandbox draaien (`failed to initialize in-process
app-server client: Operation not permitted`), dus de aanroep heeft toestemming
nodig. Zonder allowlist-regel wacht een onbeheerde ronde op jou in plaats van te
draaien. In `.claude/settings.json`:

```json
{ "permissions": { "allow": [
  "Bash(.claude/skills/paia-spec-adversarial-review/review.sh:*)",
  "Bash(.claude/skills/paia-spec-adversarial-review/verify.sh:*)"
] } }
```

Handmatig draaien kan ook, zonder agent — dan komt de hele review wél je context in:

```bash
.claude/skills/paia-spec-adversarial-review/review.sh \
  docs/developer/planning/specs/<spec>.md 1 <ATTEMPT>/round-1.json
```

#### Ronde 1: Codex en Opus (#988)

In ronde 1 draaien Codex en een Opus-reviewer naast elkaar; jij beslist blind. Opzet en
grenzen staan in [`RECEPT.md`](../../../experiments/codex-fallback-paren/RECEPT.md). De
mechaniek staat één keer, in de plan-skill:
[`paia-plan-adversarial-review`](../paia-plan-adversarial-review/SKILL.md), stap 5, *Ronde 1:
Codex en Opus*. Volg die: twee achtergrondreviewers in één bericht, dan
`uv run python scripts/review_pair.py blind <ATTEMPT>` en beslissen op de A/B-tabel (stap 3),
en bij een mislukt paar de tabel daar. `blind` draai je, net als `merge` en `write`, met
`dangerouslyDisableSandbox: true`. Waar die tabel "gewone stap 5 (zonder paar)" zegt, is dat
hier stap 2: de `spec-adversary` zonder paar. Hier verschilt alleen dit:

- **Codex** (Bash, `run_in_background: true`, `dangerouslyDisableSandbox: true`), zonder
  `--context`:

  ```bash
  .claude/skills/paia-spec-adversarial-review/review.sh <spec> 1 <ATTEMPT>/round-1.codex.json \
    none [max] > <ATTEMPT>/round-1.codex.log 2>&1; echo "exit=$?"
  ```

  `[max]` is het `LEVEL`-slot: laat het weg, of schrijf `max` als je dat wilt, net als `LEVEL`
  bij de gewone dispatch. Dan krijgt het record in stap 5 `--level max`. Een limiet staat in
  `<ATTEMPT>/round-1.codex.json.events`, niet in `round-1.json.events`.

- **Opus** is de subagent uit *Opus-dispatch* hieronder, met
  `OUT` = `<ATTEMPT>/round-1.fallback.json`.

Mis je een van deze drie regels, dan is het paar kwijt:

- `git status --porcelain` is leeg vóór de dispatch. Anders weigert `blind`. Tussen de dispatch
  en `blind` schrijf je niets in de worktree: geen edit, geen commit.
- De prompt vraagt de Opus-subagent alleen `KLAAR <pad> model=<exact model-ID uit je eigen
  systeemprompt>` terug te geven: geen bevindingen, geen telling. Draai geen jq-tabel en zet
  geen banner vóór `blind`. De gemelde ID gaat ongewijzigd als `--shadow-model` naar het
  record (stap 5).
- Lees `pair.json`, de ruwe bestanden en `round-1.codex.log` niet vóór `merge`. Lees van de
  Codex-call alleen de exitcode.

### 3. Gate — jij oordeelt, niet de reviewer

**Verplichte sub-skill:** `superpowers:receiving-code-review`. Niet blind fixen.

Toon de tabel en per BLOCKER/MAJOR een voorstel:

| Beslissing | Betekenis |
|---|---|
| accept | terecht, spec aanpassen |
| reject | onterecht — **met reden**, die reden gaat mee naar de volgende ronde |
| defer | terecht maar hoort in het plan, niet in de spec |

Spot-check elke `file:line` die een BLOCKER draagt vóórdat je 'm accepteert.
Een reviewer die de codebase verkeerd leest produceert overtuigende onzin.
Wat je overneemt (`accept` op een BLOCKER/MAJOR, `minor_yes` op een MINOR) sorteer je in drie
stapels, naar de builder check van superpowers (`brainstorming/SKILL.md`):

- **Al beantwoord** door spec of repo: het antwoord gaat in de spec.
- **Klein**: een redelijke bouwer beslist het zonder dat er iets anders gebouwd wordt. Beslis
  het zelf en markeer het in de spec met `*(eigen keuze na spec-review, <ID>)*`.
- **Van de gebruiker**: het antwoord verandert wat er gebouwd wordt. Alleen deze stapel wordt
  een vraag, gebundeld in één bericht volgens de vraagbundel-norm
  (`docs/developer/tenets.md`): hoogstens vier, belangrijkste eerst, elk met een aanbeveling.
  In Claude Code één `AskUserQuestion`; in een controller-run een genummerde lijst als laatste
  tekst (*Controller-run*).

MINORs beslis je zelf (`minor_yes`/`minor_no`); in de sidecar krijgt elke MINOR een eigen regel.
`reject` en `defer` krijgen geen stapel.

In een verificatieronde geldt een extra check: Elk geaccepteerd ID staat in `verified` of in `verifies` van een finding
(geaccepteerd = `accept`). Ontbreekt er een, dan draai je díe verificatieronde opnieuw
(zelfde `n`, zelfde `.fixes.diff`, `round-<n>.json` overschreven). Ontbreekt het ID dan weer,
dan stop je met `aborted` (stap 5) en meld je dat ID als open punt.
MINORs starten nooit een ronde.

Schrijf na **élke** ronde, ook de laatste, de beslissingen naar de sidecar
`<ATTEMPT>/round-<n>.decisions.json`, en **nooit** onderaan `OUT` zelf: dat maakt de JSON
ongeldig. Eén object per bevinding, MINORs inbegrepen. Bij `converged` en `round_limit` weigert
`write` (stap 5) een record met een bevinding zonder beslissing (exit 4); alleen `aborted` mag
gaten hebben.

```json
[
  {"finding_id": "r1-f2", "decision": "reject", "reason": "de migratie staat al in B3"},
  {"finding_id": "r1-f3", "decision": "minor_yes", "reason": "klein, eigen keuze"}
]
```

- `finding_id` alleen uit de `ID`-kolom, elk precies één keer.
- `decision`: `accept`, `reject`, `defer`, `accepted_open` (alleen bij `round_limit`, gezet door
  `accepteer-open` in stap 5, nooit met de hand), `minor_yes` of `minor_no`.
- `same_as`: het ID van dezelfde bevinding in een eerdere ronde; anders weglaten.

`review.sh` noemt de sidecar in het prior-blok van ronde N+1, zodat die ziet wat er is
afgewezen en waarom.

**Gepaarde ronde 1** (stap 2, *Ronde 1: Codex en Opus*): je beslist blind, zoals in stap 6 van de
plan-skill.

- Schrijf `<ATTEMPT>/round-1.blind-decisions.json` in plaats van `round-1.decisions.json`, met
  de blinde ID's (`A-f<i>`, `B-f<i>`).
- Een dubbele bevinding volgens het recept (*"Hetzelfde"*) is één rij met `also`:
  `{"finding_id": "A-f2", "also": "B-f1", "decision": "accept", "reason": "…"}`.
- Draai daarna `uv run python scripts/review_pair.py merge <ATTEMPT>`
  (`dangerouslyDisableSandbox: true`). Die schrijft `round-1.json`, `round-1.decisions.json`
  en `round-1.sources.json`, en toont de ontblinde tabel met de `r1-f<i>`-ID's.
- Exit 4 op een beslissing: herstel de sidecar en draai `merge` opnieuw. Exit 4 op een kapot
  ruw bestand: geen paar (de tabel in de plan-skill).

### 4. Fixen

Alleen de accepts, alleen in de spec. Geen code, geen belendende bestanden.
Deferred items: één regel in de spec onder *Open voor het plan*.

### 5. Convergentie

Stop bij het eerste dat waar is. Elke stopregel heeft een `outcome` voor het record:

| Stopregel | `outcome` |
|---|---|
| Ronde 1 zonder `accept` op een BLOCKER/MAJOR — er is niets te fixen, dus niets te verifiëren | `converged` |
| Een verificatieronde zonder open BLOCKER/MAJOR (zie stap 3) — alle geaccepteerde fixes staan overeind | `converged` |
| Ronde 3 gehaald met open BLOCKERs/MAJORs — leg ze vast met `accepteer-open` en ga door naar `writing-plans`, nooit een vraag | `round_limit` |
| Jij zegt stop, of een geaccepteerd ID ontbreekt ook na een herhaalde verificatieronde (stap 3) | `aborted` |

**Open** = een BLOCKER/MAJOR uit de laatste ronde zonder `reject` of `defer`, ook een `accept`
die je in stap 4 fixte: die fix zag geen reviewer meer. `write` weigert `converged` met een
open punt (exit 4).

**Alleen ronde 1 is een review.** Vanaf ronde 2 verifieert de reviewer alleen of de fixes van
de vorige ronde standhouden — geen nieuwe zoektocht naar problemen; dat was precies de lus die
niet convergeerde (#1066, zie *Waarom deze skill bestaat*). **Geen fixes, geen ronde**: zonder
een `accept` op een BLOCKER/MAJOR in de vorige ronde draai je geen volgende ronde.
**Een derde ronde alleen als een fix uit ronde 2 niet klopt** — dezelfde bevinding twee
rondes op rij is een structureel probleem, geen ronde-4-kandidaat.

**Commit eerst de spec-fixes**, vóór `accepteer-open` en het record:
`docs(spec): verwerk spec-review (#<issue>)`. `write` hasht de spec op `HEAD` als
`spec_sha256_final`; een ongecommitte fix valt buiten het record. Geen fixes = geen commit;
maak nooit een lege commit.

**Bij `round_limit` leg je de open punten vast en ga je door**, in deze volgorde:

1. `uv run python scripts/plan_review_record.py accepteer-open <ATTEMPT> --doc <spec>`
   (buiten de sandbox, zoals bij `start` en `write`). Dit herschrijft `round-3.decisions.json`
   (elk open punt wordt `accepted_open`) en zet het blok `## Open na review` onderaan de spec;
   een bestaand blok wordt vervangen, nooit verdubbeld. Exit 2 met op stderr `geen open
   BLOCKER/MAJOR in de laatste ronde: niets te accepteren` = er staat niets open: de uitkomst is
   dan `converged`, geen `round_limit`. Exit 2 zonder die melding is een aanroepfout: herstel
   de aanroep. Exit 4 (`ACCEPTEER-OPEN MISLUKT: …`): meld de melding letterlijk, herstel de
   oorzaak (pad, sidecar) en draai opnieuw. Lukt het dan nog niet, zet de open punten in de
   handoff en ga door naar de volgende fase; nooit de sidecar of het document met de hand
   aanpassen, nooit een vraag.
2. Commit de spec als `git diff --quiet -- <spec>` een verschil meldt:
   `docs(spec): leg open review-punten vast (#<issue>)`. Geen verschil (hervatte run) = geen
   commit.
3. Pas dan het record (hieronder, `--outcome round_limit`). Vraag de gebruiker niets:
   `round_limit` is geen vraag.
   Het record komt altijd eerst. Na het record volgt in een controller-run de run-instructie
   (`controller.bij_rondegrens` in `lighthouse.toml`); bij `doorgaan` is dat de gewone handoff.

Pas daarna het record, bij elke uitkomst (`dangerouslyDisableSandbox: true`, anders eindigt
`write` in exit 4 en is het record kwijt):

```bash
uv run python scripts/plan_review_record.py write <ATTEMPT> \
  --outcome <converged|round_limit|aborted> --review-mode <codex|fallback> \
  [--reviewer-model <model>] [--shadow-model <model>] [--level max]
```

Geen `--spec` of `--issue`: bij een spec-review is dat exit 2.

`--review-mode` zegt wie ronde 1 reviewde, zoals in stap 9 van de plan-skill:

- Gepaarde ronde 1 (er is een `round-1.sources.json`): `--review-mode codex --shadow-model
  <model dat Opus meldde>`.
- Alleen Codex haalde ronde 1: `--review-mode codex`.
- Alleen Opus haalde ronde 1 (*Fallback zonder Codex*): `--review-mode fallback
  --reviewer-model <gemeld model>`.

De verificatierondes op Opus veranderen `--review-mode` niet. Zonder paar geen
`--shadow-model`; `write` weigert beide verkeerde kanten met exit 4.

| exit | Betekenis | Jij doet |
|---|---|---|
| 0 | record geschreven; het pad staat op stdout | pad melden |
| 3 | niet op de Beelink: geen record | luid melden dat deze review buiten de meting valt, door naar afsluiten |
| 4 | schrijf- of validatiefout: alleen de stagingmap `.<attempt>.tmp/` blijft | de melding letterlijk doorgeven; is het een ontbrekende of foute beslissing, sidecar herstellen en `write` opnieuw; anders door naar afsluiten |

Het record is meetmateriaal, de review zelf is het product: een mislukt record blokkeert de
handoff nooit.

Sluit af met: spec-pad, aantal rondes, geaccepteerd/afgewezen/deferred, het eindverdict van
de laatste ronde, `outcome`, en het record-pad of de melding van `write`. Eindig met
`/paia-handoff`. Interactief draait `superpowers:writing-plans` pas in het volgende venster (vaste naad,
`AGENTS.md`); in een controller-run zie *Controller-run*.

## Fallback zonder Codex

Alleen ronde 1 draait op Codex, dus alleen ronde 1 kent een fallback. Drie aanleidingen: geen
`codex` (vraag eerst, zie *Preflight*; niet in een controller-run), het budget vooraf op (exit 6), of een Codex-limiet
midden in de ronde (exit 5 + `try again at`). Bij de laatste twee niet vragen, meteen doen.
Ronde 1 loopt dan Opus-only: de Opus-kant van het paar wordt `round-1.json` (de tabel in de
plan-skill). Ontbrak `codex` al bij de preflight, dan is er geen paar gedispatcht: één
*Opus-dispatch* met `OUT` = `<ATTEMPT>/round-1.json`.

Zet boven de tabel van een Opus-only ronde 1:

> ⚠️ SAME-FAMILY FALLBACK — zelfde modelfamilie als de auteur van de spec.
> Vangt grond-fouten (code die niet bestaat), vangt géén gedeelde blinde vlekken.

Schone context helpt nog steeds; andere blinde vlekken krijg je niet cadeau.

Het record krijgt `--review-mode fallback --reviewer-model <modelnaam van de subagent>` (stap 5).

## Opus-dispatch

Eén dispatch, drie rollen: de Opus-kant van het paar in ronde 1, de fallback als Codex ronde 1
niet haalt, en de verificateur in elke ronde ≥ 2.

Dispatch een `general-purpose` subagent met `model: "opus"` en de tekst tussen
`---BEGIN PROMPT---` en `---END PROMPT---` uit `reviewer-prompt.md` (ronde 1) of
`verifier-prompt.md` (ronde ≥ 2), placeholders met de hand ingevuld: `{{CONTEXT_PATHS}}` als
regels `- <pad>`, en `{{PRIOR_REVIEW_BLOCK}}` leeg in ronde 1 en vanaf ronde 2 de alinea die
`review.sh` als `prior_block` bouwt, met `PRIOR` en zijn `.decisions.json`-sidecar ingevuld.
Geef hem de opdracht het JSON-antwoord volgens `findings.schema.json` resp.
`verification.schema.json` naar `OUT` te schrijven. In een gepaarde ronde 1 sla je de tabel
hieronder over (stap 2): `blind` maakt de tabel. Anders maak je de `ID`-kolom zelf, **vóór**
het sorteren:

```bash
jq -r --arg round <n> '
  .findings | to_entries
  | map(.value + {id: "r\($round)-f\(.key + 1)"})
  | sort_by({"BLOCKER": 0, "MAJOR": 1, "MINOR": 2}[.severity])
  | .[]
  | "| \(.id) | \(.severity) | \(.spec_section) | \(.verifies // "") | \(.problem) | \(.failure_scenario) | \(.evidence) | \(.proposed_fix) |"
' <OUT>
```

Kop: `| ID | Sev | Section | Verifies | Problem | Failure | Evidence | Proposed fix |`. Direct
daaronder, bij een verificatieronde:

```bash
jq -r '.verified // empty | "VERIFIED: " + (if length == 0 then "none" else join(", ") end)' \
  <OUT>
```

## Uitbreidingen

- Plannen: `/paia-plan-adversarial-review`. Zelfde runner, met eigen prompt en schema via
  `--prompt`/`--schema` en de spec(s) via `--context` (zie de kop van `review.sh`).
- Verificatie: `verify.sh` (naast dit bestand) is een tweede wrapper op dezelfde runner, met
  `verifier-prompt.md` en `verification.schema.json` in plaats van de ronde-1-prompt/schema.
  Alleen de `.agents`-spiegel gebruikt hem; in Claude Code verifieert Opus (stap 2).
- Prompt tunen doe je in `reviewer-prompt.md` — deze SKILL.md leest de inhoud
  nooit, alleen de placeholders.
