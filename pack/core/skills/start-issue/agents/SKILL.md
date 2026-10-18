---
name: paia-start-issue
description: Use when starting work on a GitHub issue in CorneeldH/personal_ai_agent — "pak 610 op", "begin aan issue 526", "kijk naar 623". LET OP — gaat het om een issue schrijven, gebruik paia-write-issue; om een issue sluiten, paia-close-issue; wordt een nummer alleen genoemd zonder de intentie het op te pakken, gebruik deze skill niet.
metadata:
  binding: default
  origin: own
  domain: factory
---

# Start Issue Skill

Van issuenummer naar gelezen issue, worktree en één gekozen vervolgroute. Vult het gat tussen
`/paia-write-issue` (opent de lane) en `/paia-close-issue` (sluit hem).

Ontwerp: `docs/developer/planning/specs/2026-08-23-start-issue-skill-design.md`.

## Domein

Hieronder valt: het *begin* van werk aan een bestaand issue in
`CorneeldH/personal_ai_agent` — uitlezen, de assen melden, de al-af-vraag stellen, de worktree opzetten, de route ophalen
bij `/paia-route-issue` en doorroeren.

Hier niet: een issue schrijven of bijwerken (`/paia-write-issue`), een issue sluiten
(`/paia-close-issue`), de staat van het board auditen (`/paia-board-sweep`), en het uitvoeren van
de gekozen route zelf — dat delegeert deze skill.

## Workflow

De volgorde ís het ontwerp: de worktree staat vóór de eerste schrijfactie, niet erna.

### 1. Eén uitleescall

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<nr>"
```

Levert `title`, `body`, `labels`, `milestone` en `comments` (het *aantal*) in één antwoord. Alleen
als dat aantal > 0 is volgt een tweede call:

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<nr>/comments"
```

Nooit `gh issue list --json` — dat breekt onder de rtk-proxy. Altijd met
`dangerouslyDisableSandbox: true`; Go's TLS is anders geblokkeerd.

### 2. De drie assen lezen en melden

Uit de labels:

| As | Label | Wat het zegt |
|---|---|---|
| waar pak je het op | `1-loc:mac` / `1-loc:beelink` / `1-loc:container` | de omgeving |
| waar hoort het bij | `2-area:*` | de eeuwige as |
| wat blokkeert | een `3-`-label, hoogstens één | de eerstvolgende stap |

Meld alle drie, ook als ze de route niet veranderen. Ontbreekt er één, meld dát — dan klopt het
issue niet en dat is zelf informatie.

Zit er een milestone aan, lees dan de `### Definition of Done`, de `### No-Gos` en de
`### Nog niet vastgelegd` uit de milestone-beschrijving mee. Past het issue daar volgens de eerste
twee niet in, zeg dat vóór je begint. Raakt het issue aanwijsbaar een open vraag uit de derde
(een bullet zonder `→ #<nr>`), meld dan één regel `fog: <vraag>`; in `paia-route-issue` valt het
dan in poort 4 onder c. Alleen een vraag die aantoonbaar over het werk van dit issue gaat telt. Ontbreekt de
kop of staat er "Geen.", dan geen melding.

**Al af?** Vraag dit vóór de worktree: een issue dat al af is kost dan geen branch. Het is een
gerichte zoekopdracht op dít issue, geen bulkscan. Kies een kernterm uit het issue (functie,
bestand, configsleutel) en draai dit als **één Bash-call** (niet vier losse; de KPI `leadin` telt
elke call vóór de eerste route-call):

```bash
git fetch origin main || echo 'FETCH FAALDE: origin/main kan verouderd zijn'
git log --oneline origin/main -E --grep '#<nr>([^0-9]|$)'
git log --oneline -S '<kernterm>' origin/main
git grep -l '<kernterm>' origin/main -- src scripts deploy .claude docs
```

`git grep` geeft exit 1 bij nul treffers; pas exit ≥ 2 (bijv. 128) is een fout. Precisie gaat voor
recall: alleen een treffer die de acceptatiecriteria aantoonbaar dekt telt, namelijk een commit met
`(#<nr>)` in het onderwerp die het werk zelf levert, of code/docs die de AC aanwijsbaar invullen.
Een vermelding in een commitbody, een spec-, plan- of "deel 1"-commit telt niet: dat is de
`nee`-regel hieronder, in zijn tweede vorm.

Meld precies één regel, in een van drie soorten:

- `al af? nee — gezocht op <termen>`: ook zonder treffer, nooit stilte. Door naar stap 3, zonder
  vraag. Tweede vorm, bij een niet-dekkende vermelding:
  `al af? nee — genoemd in <sha>, dekt de AC niet`; ook die gaat door naar stap 3.
- `al af? waarschijnlijk — <bewijs>`: stop vóór stap 3 en bied `/paia-close-issue <nr>` aan
  (ingang B, die verifieert). De gebruiker beslist. Zegt de gebruiker "toch door", ga dan verder
  met stap 3. Controller-run: zet de keuze (sluiten via `/paia-close-issue <nr>`, door de
  gebruiker, of "toch door") als laatste tekst en stop; zelf sluit je nooit.
- `al af? niet meetbaar — fetch faalde`: de output bevat `FETCH FAALDE` en er is geen dekkende
  treffer. Het recept zocht dan op een mogelijk verouderde `origin/main`, dus nul treffers is
  geen `nee`. Stop vóór stap 3; de gebruiker kiest tussen opnieuw proberen en "toch door".
  Controller-run: zet die keuze als laatste tekst en stop. Een dekkende treffer blijft
  `waarschijnlijk`: ook oude data bewijst dat het werk er al was.

Deze stap rapporteert alleen: ze sluit niets en zet geen label.

### 3. Worktree vóór de eerste schrijfactie

Via `superpowers:using-git-worktrees`, branch `issue-<nr>-<slug>`. Geen eigen implementatie: die
skill kiest zelf tussen `EnterWorktree` en de `git worktree add`-terugval.

Onvoorwaardelijk, ook bij poort 1 en poort 2 hieronder: een spec is een commit, een debugfix is
een commit. Uitzonderingen: read-only werk, en de gebruiker die expliciet "op main" zegt.

Zet daarna het board bij, vanuit de worktree en met `dangerouslyDisableSandbox: true`:

```bash
uv run python scripts/board_start.py <nr>
```

Meld de ene `board:`-regel die het script print. Alleen `To do` en `On hold` worden `In progress`;
`Controller` blijft staan, want de controller selecteert daarop en draait deze skill zelf.
Een `board: overgeslagen`-regel is geen stop: het oppakken gaat door. Sla de stap over bij een
`3-blocked-*`-label uit stap 2: poort 0 stopt dan, en het board zou `In progress` tonen voor werk
dat stilligt. In een controller-run geldt dat niet voor `3-blocked-issue`: poort 0 stopt daar niet
en het board blijft `Controller`, dus er is niets over te slaan.

### 4. Route via `paia-route-issue`, dan uitvoeren

Roep `paia-route-issue` aan met dit nummer. Het issue staat uit stap 1 al in context, dus hij doet
geen nieuwe uitleescall. De poorten (0–4, eerste treffer wint) en het `3-`-label als stuur staan
daar; hij levert één regel:

```text
#<nr> Poort <n>[<a|b|c>]: <route> — <reden>
```

Neem die regel **letterlijk** over in je antwoord, **vóór** de volgende `Skill`-call. Dat is wat
de KPI-rij `gate` telt (en `scout` de subletter); een regel die pas ná het doorroeren staat, telt
als stil gerouteerd.

Voer daarna de gemelde route uit:

| Regel | Uitvoering |
|---|---|
| `Poort 0` | stop, met de reden uit de regel; geen board, geen route |
| `Poort 1` | die skill aanroepen, klaar |
| `Poort 2` | `superpowers:systematic-debugging` |
| `Poort 4c` | `superpowers:brainstorming` → spec |
| `Poort 4b` | `superpowers:writing-plans` |
| `Poort 4a` | direct uitvoeren |

Draagt de regel `· laag: <laag> (<bestand>)` (poort 3) of noemt de reden een meereizende skill,
dan reist dat mee als context voor de uitvoering.

**Controller-run?** Eindigde de prompt op `(issue #<nr>, controller-run)`, dan roep je bij
uitgang **c** `superpowers:brainstorming` niet aan: headless kan niemand antwoorden. Post met
`/paia-handoff` (controller-modus) een handoff met `volgende: /superpowers:brainstorming` en
stop. De controller geeft het issue terug met "heeft brainstorm nodig".

**Geblokkeerd in een controller-run?** Draagt de regel `— blokkeerder #<x> open`:

- Poort `4b` of `4c`: voer de route uit zoals altijd; die eindigen al in een handoff.
- Poort `1`, `2` of `4a`: voer niet uit. Post met `/paia-handoff` (controller-modus) een handoff
  `volgende: /paia-start-issue <nr>` vanuit de eigen worktree, en stop. De controller start hem pas
  als de PR van de voorganger groen is.
- Uitzondering: draagt de laatste `🤖 Gestart` op het issue het veld `stapel:` of `blokkade:`, dan
  is de blokkade al verrekend en voer je de route uit (`blokkade: verrekend`: de voorganger is
  nog open, maar zijn PR is gemerged).

Meld de keuze in één regel: `— gestapeld op #<x>` of `— wacht op #<x>, handoff gepost`.

#### Scout: voorverkenning bij 4b en 4c

Poort `4b` en `4c` sturen door naar een skill die de codebase alsnog breed moet leren kennen.
Die verkenning kost hoofdagentcontext en herhaalt zich tussen brainstorming, planning en
uitvoering. Daarom gaat er bij deze twee uitgangen — en alleen bij deze twee — eerst een
read-only scout langs.

**Pas ná de worktree.** De agent krijgt een absoluut pad; dat pad moet bestaan.

Eén Agent-call, `subagent_type: issue-impact-scout`. Mee in de opdracht: de volledige
issuebody met de acceptatiecriteria, de Definition of Done, no-gos en `Nog niet vastgelegd` van
de milestone als die er is, de gekozen route (`4b` of `4c`), het absolute worktreepad, en de
instructielaag plus meereizende skill als poort 3 die vaststelde. De agent doet zelf geen GitHub-call.

Meld de trigger op zijn eigen regel:

```text
Poort 4b: writing-plans
Scout: issue-impact-scout
```

Poort `0`, `1`, `2` en `4a` roepen hem nooit aan: die hebben respectievelijk een stop, een
compleet recept, een debugworkflow en een uitvoering die in één bestand past.

##### Overslaan mag, stil overslaan niet

Alleen wanneer de input die je al hebt — issue, spec, eerdere sessiecontext — deze drie
concreet bevat:

1. de exact geraakte bestanden of componenten;
2. het bestaande patroon dat gevolgd wordt;
3. het testpad en het verificatiecommando.

Dan meld je:

```text
Scout: overgeslagen — <concrete reden>.
```

`Lijkt eenvoudig`, `context is duidelijk` en elk ander oordeel zonder die drie zijn
**geen geldige reden** om hem over te slaan.

> De testmarker is de aaneengesloten string `geen geldige reden`. Breek hem niet over twee
> regels af bij het herformatteren.

##### Als hij mislukt, gaat de route door

Timeout, toolfout, of output die het contract niet volgt:

```text
Scout: mislukt — <fout>.
Route vervolgt met normale contextverkenning.
```

De scout is een versneller, geen gate. Brainstorming of planning doet dan zijn eigen
verkenning.

##### Het pack staat in context

Het `IMPACT PACK` is het antwoord van de Agent-call en staat daarmee al in de context van de
skill (`brainstorming` of `writing-plans`) die je hierna in deze sessie aanroept.

Herhaal het niet in de Skill-args en vat het niet samen; zet in de args alleen de vaste
verwijzing `IMPACT PACK: staat in context, niet herhalen`. Een samenvatting kost precies de
precisie waarvoor de scout betaald is, een kopie betaalt dezelfde tokens twee keer. De
ontvangende skill blijft zelf verantwoordelijk en leest een bron opnieuw wanneer een echte
beslissing erop rust.

De ontvanger leest vanaf de regel `IMPACT PACK`. Staat er denkwerk vóór die regel, dan is dat
een defect in de persona, die zo'n aanloop verbiedt, en geen reden de route te stoppen.

##### Buiten Claude Code

De persona staat alleen onder `.claude/agents/` en is hier **niet beschikbaar**. Meld dat
zoals je een fout meldt en volg het fail-open-pad: de route gaat door met de normale
contextverkenning van de volgende skill. Je **simuleert** de persona niet met een algemene
executor — dan krijg je de kosten van een subagent zonder de begrenzing die hem bruikbaar
maakt.

**b eindigt bij het gecommitte plan.** Plan geschreven en gecommit → `/paia-handoff` (vaste
naad, `AGENTS.md`). Die laat de gebruiker kiezen: plan-review, controller (SDD) of interactief (`/superpowers:executing-plans`). Uitvoeren volgt pas ná die keuze, in een eigen
venster of via de controller.

Noem bij het doorroeren **expliciet het projectpad**, want dat overruled de default van de
superpowers-skills: specs naar `docs/developer/planning/specs/`, plannen naar
`docs/developer/planning/plans/` — niet de eigen map van de superpowers-plugin.

## Non-negotiable

- ✅ Worktree vóór de eerste schrijfactie · ❌ eerst wat bestanden aanpassen en dan de branch bedenken
- ✅ `gh api …/issues/<nr>` · ❌ `gh issue list --json` (breekt onder de rtk-proxy)
- ✅ Alle drie de assen melden · ❌ alleen de as noemen die de route bepaalde
- ✅ De route bij `paia-route-issue` halen en zijn regel letterlijk overnemen vóór de volgende
  `Skill`-call · ❌ zelf routeren, de regel samenvatten, of hem pas ná het doorroeren noemen
- ✅ Na b: plan committen, dan `/paia-handoff` met de keuze review, controller of interactief · ❌ SDD in dezelfde sessie, of de plan-review overslaan zonder besluit van de gebruiker
- ✅ Het projectpad noemen bij het doorroeren · ❌ superpowers zijn eigen defaultmap laten kiezen
- ✅ Bij 4b/4c de scout aanroepen of hem met een concrete reden overslaan · ❌ hem stil overslaan
- ✅ Het pack in context laten en ernaar verwijzen met `IMPACT PACK: staat in context, niet herhalen` · ❌ het pack samenvatten, parafraseren of in de Skill-args herhalen
- ✅ Na de worktree `board_start.py` · ❌ een andere status dan `To do` of `On hold` overschrijven

## KPIs

| Metric | Nu | Target | Meting |
|---|---:|---:|---|
| Tool-calls tussen pickup-prompt en eerste `Skill`/`Write`/`Edit`/`AskUserQuestion` | mediaan 14 (n=11) | ≤ 2 | `uv run python scripts/measure_start_issue_kpi.py leadin` |
| Aandeel pickups waar de eerste zichtbare schrijfactie onder `.claude/worktrees/` viel | 78,6% | 100% | `uv run python scripts/measure_start_issue_kpi.py worktree-first` |
| Sessies waarin deze skill laadde zonder issuenummer in de prompt | — | 0 | `uv run python scripts/measure_start_issue_kpi.py mistrigger` |
| Nieuwe spec/plan-bestanden op de projectpaden in plaats van de superpowers-defaultmap | 100% | 100% | `uv run python scripts/measure_start_issue_kpi.py paths` |
| Pickups waar de skill de gekozen poort expliciet noemt | — | 100% | `uv run python scripts/measure_start_issue_kpi.py gate` |
| Pickups op poort 4b/4c met een scoutcall of een concrete skipreden | — | 100% | `uv run python scripts/measure_start_issue_kpi.py scout` |

Rij 1 draagt "mediaan 14" (n=11), niet de 5–7 uit het issue: dat laatste was een schatting, dit is
het gemeten cijfer over het cohort zonder handroutering. De "Al af?"-call uit stap 2 verhoogt de
lead-in bewust met 1: één Bash-call, vóór de eerste route-call. Target ≤ 2 blijft staan, maar is
nu alleen haalbaar voor een issue zonder comments (uitlezen + al-af = 2); met comments zijn het er 3.

Rij 3 is de rekening van de ambient-keuze. Zonder die rij is "scherpe negatieve grens" een
bewering; met die rij is het een getal dat bij de volgende meting oploopt of niet.

Rij 2 ziet alleen `Write`/`Edit`. Een bestand dat via een heredoc of `sed -i` ontstaat, telt niet
mee — 100% betekent "van de zichtbare schrijfacties".

Rij 4 telt vanaf 2026-08-02, de dag dat de conventie in `CLAUDE.md` kwam. Over de volle historie
staat hij op 62,6%, maar elk bestand in de superpowers-defaultmap dateert van vóór die dag; dat
verleden verandert nooit meer, dus over de volle historie is 100% per constructie onhaalbaar.

Rij 5 telt alleen pickups waarin deze skill daadwerkelijk laadde; een pickup zonder skill is de
fout die rij 1 en 3 meten, niet deze. De verdeling over de poorten is bijvangst en laat zien of
poort 1 en 2 überhaupt vuren of dood hout zijn.

## Bekende valkuilen

- **`gh issue list --json` breekt** onder de rtk-proxy (`Failed to parse gh issue list output`).
  Gebruik `gh api`. Een onbekend `--label`-filter wordt bovendien stil genegeerd: je krijgt het
  volledige, ongefilterde resultaat terug en telt onzin.
- **De cwd van de persistente shell drift** midden in een sessie terug naar de hoofdcheckout.
  Prefix elk commando met `cd <worktree>` of `git -C <worktree>`, anders raakt een `sed -i` de
  verkeerde boom.
- **Links: in chat absoluut, in bestanden relatief.** In een chat-antwoord moet een link naar
  een bestand dat alleen op de branch bestaat het volledige absolute worktree-pad dragen: de
  IDE resolvet chatlinks tegen de hoofdcheckout. In een `.md`-bestand juist relatief;
  `worktree-linkcheck.sh` blokkeert daar een absoluut pad.
- **Een milestone zonder DoD-tekst bestaat niet.** Kom je er een tegen, meld dat — het issue erin
  hangen is dan een gok.
- **Deze skill zet precies twee overgangen op het board: `To do` en `On hold` → `In progress`.**
  Geen labels, geen andere status. `On hold` is een bewuste parkering, en wie het issue
  expliciet oppakt heft die op. `Controller` blijft staan: een overdracht aan de controller.
  Voor de rest dezelfde lijn als `/paia-board-sweep`.
