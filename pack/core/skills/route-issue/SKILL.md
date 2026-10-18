---
name: paia-route-issue
description: 'Read-only triage van één of meer issues in CorneeldH/personal_ai_agent — vijf poorten (blokkade, bekend recept, bug, instructielaag, ontwerpdiepte), eerste treffer wint, en per issue precies één regel met de route. Gebruik bij "welke route voor #610", "hoe zou je dit oppakken", en bij de aanroep vanuit paia-start-issue en paia-backlog-stand. Voert de route nooit uit. LET OP — daadwerkelijk aan een issue beginnen (worktree, board, uitvoering) is paia-start-issue.'
metadata:
  binding: default
  origin: own
  source: docs/developer/planning/specs/2026-09-28-backlog-stand-en-blokkades-design.md
  domain: factory
---

# Route Issue Skill

Van issuenummer naar één regel: welke poort, welke route, waarom. Read-only: deze skill leest,
weegt en meldt. Hij zet geen worktree op, raakt het board niet, stuurt geen voorverkenning uit en
roept geen vervolgskill aan.

**`paia-route-issue` roept de route nooit aan; hij meldt de regel en stopt. Uitvoeren doet alleen
`paia-start-issue`.**

Ontwerp: `docs/developer/planning/specs/2026-09-28-backlog-stand-en-blokkades-design.md`
(sectie C). De poorten zelf stammen uit
`docs/developer/planning/specs/2026-08-23-start-issue-skill-design.md`.

## Domein

Hieronder valt: de route van een bestaand issue in `CorneeldH/personal_ai_agent` bepalen, voor één
nummer of een lijst. Aanroepers: `paia-start-issue` (stap 4, één nummer) en `paia-backlog-stand`
(de indeling van *Oppakbaar*, veel nummers).

Hier niet: aan het issue beginnen of de route uitvoeren (`/paia-start-issue`), een issue schrijven
of bijwerken (`/paia-write-issue`), het board auditen (`/paia-board-sweep`).

## Invoer

Eén of meer issuenummers. Per nummer:

- **Staat het issue al in context** — gelezen in stap 1 van `paia-start-issue`, of in de lijstcall
  van `paia-backlog-stand` — dan geen nieuwe call.
- Anders één uitleescall, met `dangerouslyDisableSandbox: true`:

  ```bash
  R=$(uv run python -m src.controller.identity repo) || exit 2
  gh api "repos/$R/issues/<nr>"
  ```

  Alleen als `comments` > 0 volgt
  `R=$(uv run python -m src.controller.identity repo) && gh api "repos/$R/issues/<nr>/comments"`.
  Nooit `gh issue list --json` — dat breekt onder de rtk-proxy.

## Uitvoer

Per issue precies één regel, in de volgorde van de invoer:

```text
#<nr> Poort <n>[<a|b|c>]: <route> — <reden>
```

Bij poort 4 altijd met de subletter: `#610 Poort 4b: writing-plans — de AC leggen het HOE vast`,
nooit kaal `Poort 4: ...`. Viel de triage door poort 3, dan komt de laag erachter:
`… — <reden> · laag: <laag> (<bestand>)`. Een meereizende skill uit poort 1 noem je in de reden.

De vorm is contract. `scripts/measure_start_issue_kpi.py` telt de poortmelding (`GATE_RE`) en de
subletter; `paia-backlog-stand` sorteert *Oppakbaar* op de poort uit deze regel.

## Triage: vijf poorten, eerste treffer wint

```
0. Draagt het issue 3-blocked-issue of 3-blocked-external, of heeft het een open
   blocked_by zonder 3-needs-shaping/3-needs-decision/3-blocked-issue?
                                          → route: stop, reden: waarop het wacht (of: label ontbreekt)
1. Sluit de description van een paia-skill aan op dit issue,
   én levert die skill het éindresultaat van het issue?
                                          → route: die skill (sluit af)
2. Beschrijft het issue een bug of regressie?
                                          → route: superpowers:systematic-debugging
3. Raakt het issue een instructielaag?
                                          → laag + bestand, val door ↓
4. Ligt het HOE vast?                     → c / b / a
```

Alleen poort 3 valt door. Het nummer in de regel is de poort waar de triage stopte; dat maakt een
verkeerd gekozen poort achteraf vindbaar.

### Poort 0 — geblokkeerd

Twee ingangen: het label, of een blokkade zonder label.

**Bij `3-blocked-issue` of `3-blocked-external`** komt de blocker uit de relatie, niet uit de
body:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<nr>/dependencies/blocked_by" \
  --jq '.[] | "#\(.number) \(.state)"'
```

Staat er geen open blocker, dan meldt de reden het oordeel — **vervallen** (alleen gesloten
relaties) of **ongekoppeld** (geen relatie) — met het herstelcommando uit `paia-write-issue` 3e.
De route blijft stop: het label weghalen of de relatie leggen is aan de gebruiker. Faalt deze GET,
of is de uitvoer niet leesbaar, dan is het oordeel **onbekend** met het uitleescommando erbij — de
route blijft stop, en een mislukte of lege GET is nooit **ongekoppeld** (dat zet de gebruiker op
het verkeerde spoor van een `--remove-label`).

**Zonder dat label, maar met `issue_dependencies_summary.blocked_by > 0`** (al in de payload van
stap 1, geen extra call): draagt het issue geen van `3-needs-shaping`, `3-needs-decision` of
`3-blocked-issue`, dan stopt ook dit bij poort 0. Bevestig met dezelfde lijst-GET hierboven en meld
**label ontbreekt** met het herstelcommando uit `paia-write-issue` 3e
(`gh issue edit <nr> --add-label 3-blocked-issue`). Faalt die GET, meld **onbekend** en stop — een
niet te lezen blokkade valt nooit stil door naar poort 4.

Draagt het issue wél `3-needs-shaping` of `3-needs-decision` met een open `blocked_by`, dan is dat
**geen** poort-0-stop: het lagere label wint (invariant `paia-write-issue` 3e, "klopt") en de route
loopt via dat label door — poort 4c.

**Controller-run** (de prompt eindigt op `(issue #<nr>, controller-run)`): een open blokkade via
`3-blocked-issue` of een kale `blocked_by` stopt dan niet. De controller laat alleen spec en plan
toe en stapelt de uitvoering zelf (#1099). Route gewoon door naar poort 1–4 en zet achter de
regel `— blokkeerder #<x> open`. `3-blocked-external` stopt altijd, ook hier. De router blijft
read-only: hij meldt alleen de route, het niet-uitvoeren staat in `paia-start-issue`.

### Poort 1 — bekend recept

De skill-catalogus staat bij sessiestart al in context, met description en al. Deze poort kost dus
**nul tool-calls**: issue lezen, tegen die descriptions houden. Niet greppen over
`.claude/skills/` — dat is de tool-call die het KPI-plafond raakt vóór er gerouteerd is.

**Scope**: alleen `paia-*` en `superpowers:*`. De catalogus bevat ook cedanl-skills
(`write-issue`, `check-style`, `init-repo`, `simplify-ceda`) die een *andere repo* targeten;
die komen nooit in de route.

**Toets op de uitkomst**: *levert deze skill het eindresultaat van het issue op, of alleen een norm
voor werk dat nog moet gebeuren?* Alleen het eerste sluit af.

| | Voorbeeld | Rol |
|---|---|---|
| afsluitend | `paia-refresh-expo-lockfiles`, `paia-board-sweep`, `paia-entity-manager` | de skill dóét het issue |
| meereizend | `paia-shell-gates`, `paia-obsidian-markdown`, `paia-mermaid-diagram` | de skill draagt een norm |

Die twee rijen zijn illustratie, geen lijst om te onderhouden. Een meereizende skill sluit niets
af: hij gaat mee naar poort 4 als context voor de uitvoering.

### Poort 2 — bug

De route is altijd `superpowers:systematic-debugging`. Geen subkeuze met `/gsd-debug`: die voegt
persistente debug-state toe, en dat weegt niet op tegen een bug die in vijf minuten om is. Blijkt
tijdens het debuggen dat de state nodig is, dan is dat een in-sessie-beslissing, geen routering
vooraf.

Herkenning zit in het issue-body, niet in een label — er is geen `3-bug`-label en dat komt er ook
niet: `3-` markeert wat blokkeert, niet welk soort werk het is.

### Poort 3 — instructielaag

Het issue vraagt een regel, norm, conventie of skill te wijzigen. Pas de tabel *Instructielagen*
uit `AGENTS.md` toe, bepaal welke laag en welk bestand de wijziging hoort te raken, en val door
naar poort 4 — meestal a of b. Geen extra beurt, geen `AskUserQuestion`; de laagkeuze reist mee
in de regel.

### Poort 4 — ligt het HOE vast?

| Uitgang | Wanneer | Route |
|---|---|---|
| **c** | het issue laat meerdere verdedigbare ontwerpen toe | `superpowers:brainstorming` → spec |
| **b** | de AC leggen het HOE vast; alleen volgorde en uitvoering resten | `superpowers:writing-plans` |
| **a** | één bestand, nul ontwerpkeuze, AC letterlijk uitvoerbaar | direct uitvoeren |

Onderzoeksissues ("onderzoek of X kan") vallen onder **c**.

Ook onder **c** valt een issue dat aanwijsbaar een open vraag raakt uit `### Nog niet vastgelegd`
van zijn milestone — de beschrijving zit al in context (`milestone.description` in de
issue-payload, of stap 1 van `paia-backlog-stand`), geen extra call. Hetzelfde gedrag als `3-needs-shaping`, maar zonder label. Open = een bullet zonder
`→ #<nr>`; alleen een vraag die aantoonbaar over het werk van dit issue gaat telt. Een milestone
zonder die kop, of met "Geen.", routeert gewoon. De reden noemt de vraag:
`— raakt open vraag "<vraag>" uit de milestone`. Na de spec volgt de uitstroom uit
`paia-write-issue`: een scherp genoeg geworden vraag wordt een eigen issue en de bullet krijgt
`→ #<nr>`.

Er is geen tiebreak-regel. De skill weegt en noemt één route; zit hij ernaast, dan corrigeert de
gebruiker in één zin. Een regel die bij twijfel altijd naar de duurdere kant leunt kost meer dan
die zin.

## Het `3-`-label stuurt de triage, het stopt hem niet

Poort 0 vangt de twee `blocked-`-labels. De andere vier zijn juist het signaal wélke route het
issue krijgt.

| Label | Route |
|---|---|
| `3-needs-shaping` | **poort 4, c** — dat ís precies de ontbrekende stap |
| `3-needs-decision` | **poort 4, c**, smal: de openstaande beslissing als eerste vraag voorleggen |
| `3-needs-hands` | **poort 4, b/a**, uitvoeren tot aan de handbeweging, met expliciete instructie wáár mensenhanden nodig zijn |
| `3-needs-testing` | **poort 4, b/a**, uitvoeren en de mens laten oordelen of het doet wat bedoeld is |
| `3-blocked-issue` | **poort 0** — stop, meld de blocker uit de `blocked_by`-relatie, niet uit de body; in een controller-run: doorrouteren, zie Poort 0 |
| `3-blocked-external` | **poort 0** — stop, meld waarop het wacht |

Alleen de twee `blocked-`-labels zijn een harde stop (behalve `3-blocked-issue` in een controller-run, zie Poort 0): die wachten op iets buiten de sessie. De
andere vier beschrijven wat er ín de sessie moet gebeuren.

## Non-negotiable

- ✅ De regel melden en stoppen · ❌ de route zelf aanroepen, een worktree opzetten of het board bijwerken
- ✅ De poorten in volgorde, eerste treffer wint · ❌ poort 4 als eerste vraag stellen
- ✅ Per issue één regel `#<nr> Poort <n>[<a|b|c>]: <route> — <reden>`, bij poort 4 met subletter
  · ❌ stil routeren, of kaal `Poort 4: ...` zonder subletter
- ✅ Eén route kiezen en die noemen · ❌ twee routes voorstellen en de gebruiker laten kiezen
- ✅ Poort 1 uit de catalogus in context · ❌ greppen over `.claude/skills/` (kost het KPI-plafond)
- ✅ `3-blocked-*` stopt (`3-blocked-issue` niet in een controller-run, zie Poort 0) · ❌ een `3-needs-*`-label als stop behandelen
- ✅ Een issue dat al in context staat niet opnieuw ophalen · ❌ per nummer een tweede uitleescall

## Bekende valkuilen

- **Een meereizende skill als poort 1 behandelen.** `paia-shell-gates` sluit een issue over een
  hook níét af — hij draagt de norm waarmee je het schrijft. Poort 1 stopt de routering; doe dat
  alleen als de skill het eindresultaat levert.
- **Cedanl-skills in de catalogus.** `write-issue`, `check-style`, `init-repo` en `simplify-ceda`
  targeten een andere repo en een ander board. Ze matchen soms overtuigend op een issue hier; ze
  horen nooit in poort 1.
