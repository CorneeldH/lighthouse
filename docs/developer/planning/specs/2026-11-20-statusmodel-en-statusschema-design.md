# Statusmodel van een issue en het statusschema in Prioritering — ontwerp

Vervolg op [het Prioritering-tabblad](2026-11-16-prioritering-tabblad-design.md) (#374). Dit
ontwerp trekt eerst het statusmodel van een issue recht en laat de UI daarna alleen dat model
tekenen.

## Waarom

De milestone-balk en de afhankelijkhedengraaf tonen de stand van een issue, maar die stand staat
op vier plekken die elkaar deels overlappen:

| Drager | Waarden | Wie schrijft nu |
|---|---|---|
| Blokkadelabel (`[labels]`, hoogstens één) | `needs_shaping`, `needs_decision`, `blocked_issue`, `blocked_external`, `needs_hands`, `needs_testing` | `lighthouse-write-issue` en de mens; `lighthouse-close-issue` haalt `blocked_issue` weg bij afhankelijke issues |
| Board-Status | `To do`, `In progress`, `Controller`, `On hold`, `Done` | `lighthouse-start-issue` (`In progress`), triage (`Controller`), de tick (`Controller` ↔ `On hold`), de mens |
| Run in de lighthouse-state | `open`, `parked`, plus een fase | de dispatcher |
| Triage-comment | route 0–4c, ontwerpvragen | triage, alleen als tekst |

Wat daardoor scheef zit:

- **`On hold` betekent twee dingen.** De tick parkeert er een uitvoerend issue met een actieve
  voorganger, wat inhoudelijk `blocked_issue` is. De mens zet er werk op dat bewust wacht, wat
  deels samenvalt met `blocked_external`. `lighthouse-write-issue` zegt intussen dat On hold
  *geen* blokkade is.
- **"Moet eerst gebrainstormd" staat nergens.** Route 4c (meerdere verdedigbare ontwerpen) komt
  alleen in een triage-comment. `needs_shaping` staat op 2 van de 55 open issues van deze repo.
- **Een `needs_*`-label zegt niet wie het oppakt.** Triage stuurt 4a/4b/2 naar `Controller`, ook
  met `needs_hands` of `needs_testing`.
- **De UI tekent "actief" op drie manieren** (geel effen, geel gestreept, navy stippelrand met een
  gele pil) en mist het bij een deel van de actieve issues. Op 20 november: `Controller` telt niet
  als actief (lighthouse #381, #383, #406, #407), en een issue zonder afhankelijkheden krijgt in de
  graaf geen markering (lighthouse #395, paia #1184, #1345, #1362, #1776).
- **Twee betekenissen voor "nog niet geshaped" in de milestone-balk:** een grijze streepbalk bij
  0 issues, en een stippelrand met witte waas bij `Status: shaping`.

## Doelen

1. Eén statusmodel: elk open issue heeft precies één toestand, af te leiden uit labels, Status,
   `blocked_by` en runs.
2. Een `needs_*`-label betekent: **dit moet interactief worden opgepakt.** Zonder zo'n label mag
   de controller het dragen.
3. De regels van het model staan op één plek in Go. Skills, tick en triage roepen die aan.
4. De milestone-balk en de afhankelijkhedengraaf tekenen dat model met één vormentaal.

## Niet-doelen

- Een nieuw label of een nieuwe Status. Het model past in de bestaande zes labels en vier
  statussen (`On hold` gaat eraf).
- Schrijven vanuit de web-UI. De UI blijft alleen-lezen.
- Milestone-suggesties door een model bij *Zonder milestone*. Dat is een eigen issue, geblokkeerd
  door #372; zie [Opdeling](#opdeling-in-issues).

## Anti-doelen

- **Een balk die iets anders zegt dan het board.** Als de UI een eigen afleiding maakt die afwijkt
  van wat tick en triage beslissen, is het model mislukt, ook als de balk mooi is.
- **Elke statuswijziging via een modelcall.** Deterministische overgangen lopen niet via een
  skill.
- **Kleur als enige drager.** Elke toestand is ook aan vorm te herkennen (effen, streep, stippel,
  kruis).

## Aanpakken

- **A. Eén spec, vijf issues (gekozen).** Gedeeld commando, statusmodel doorvoeren,
  milestone-balk, afhankelijkhedengraaf, milestone-suggesties. De balk leest alleen labels,
  `blocked_by`, Status en runs, dus de UI-issues kunnen parallel aan het model lopen en tonen de
  goede stand zodra de labels kloppen.
- **B. Eén issue voor alles.** Eén PR door Go-kern, skills, docs en UI: niet te reviewen, niet
  atomair.
- **C. Alleen de balk nu.** De balk tekent dan labels met hun oude betekenis (bijvoorbeeld
  `needs_hands` voor een stap aan het eind van het werk). Het beeld zou niet kloppen.

## Het statusmodel

### Toestanden

Elk open issue heeft één toestand. De eerste regel die past, wint:

| # | Toestand | Wanneer |
|---|---|---|
| 1 | **actief** | een run met `open` of `parked`, of Status `In progress` of `Controller` |
| 2 | **mens nodig** | `needs_shaping` of `needs_decision` |
| 3 | **geblokkeerd** | `blocked_issue` of `blocked_external`, of een open `blocked_by` |
| 4 | **mens nodig** | `needs_hands` of `needs_testing` |
| 5 | **oppakbaar** | de rest |

Een gesloten issue is **af**. De volgorde van 2 tot 4 is de rang uit [labels.md](../../labels.md):
een open voorganger wint van hands en testing, niet van shaping en decision. Zo werkt gate 0 nu
ook (`FixedGate0` in `internal/triage/rules.go`).

**Open `blocked_by`** is een voorganger met state `OPEN`. *Mijn keuze:* de tick kijkt ook of de
voorganger een gemergde PR heeft, de UI niet (de planning-query kent geen PR's). Het verschil duurt
alleen van de merge tot het sluiten van de voorganger, en `blocked_issue` als label is de drager
waar de UI op leunt.

Daarnaast een vlag **mens nodig** op een *actief* issue: het issue heeft een `needs_*`-label
(interactief opgepakt), of de laatste 🤖-comment is een open 🤖 Vraag. *Mijn keuze: een open
vraag telt als mens nodig, want het issue wacht op jou, ook al blijft het bij de controller.*

`On hold` verdwijnt als Status. Wat er nu op staat, krijgt een van de toestanden hierboven (zie
[Migratie](#migratie-van-on-hold)).

### Labels: wanneer en door wie

De regel uit [labels.md](../../labels.md) blijft: een blokkadelabel markeert wat de
**eerstvolgende** stap blokkeert, hoogstens één, laagste rang wint. Wat verandert:

- **Een `needs_*`-label betekent interactief.** Een issue dat pas aan het eind een hand of een oordeel
  nodig heeft, krijgt dat label niet vooraf. Het komt er pas op als de controller het daarvoor
  teruggeeft.
- **`needs_shaping` dekt ook route 4c.** "Wat/waarom onscherp" én "meerdere verdedigbare ontwerpen,
  eerst brainstormen" krijgen hetzelfde label. Brainstormen is in principe interactief.
- **`blocked_external` is "on hold".** Het enige label voor "wacht op iets buiten ons". Werkt ook
  in een repo zonder board.
- **Bewust uitgesteld werk** gaat uit de milestone (naar de backlog), niet op een Status.

Wie zet wat:

| Overgang | Wie | Hoe |
|---|---|---|
| `needs_shaping` bij route 4c | `lighthouse-write-issue` bij het schrijven; triage als vangnet | `lighthouse-write-issue` doet dezelfde route-beoordeling als `lighthouse-route-issue`; triage zet het label via het gedeelde commando |
| andere `needs_*` bij het schrijven | `lighthouse-write-issue` | alleen als de eerstvolgende stap van een mens is |
| `blocked_issue` | `lighthouse-write-issue` (relatie eerst) en de tick bij teruggeven | zie [Teruggeven](#teruggeven-door-de-controller) |
| `blocked_issue` eraf | `lighthouse-close-issue` en de unblock-lane (bestaan); de tick bij een eigen geparkeerd issue | zie [Parkeren](#parkeren-wordt-teruggeven-met-blocked_issue) |
| `needs_shaping` eraf | `lighthouse-start-issue` na een goedgekeurde spec | daarna kan de controller het plan dragen |
| `blocked_external` op en af | de mens, via `lighthouse-write-issue` | ongewijzigd |

### Statussen: wanneer en door wie

| Status | Betekenis | Wie zet het |
|---|---|---|
| `To do` | niet actief; toestand volgt uit labels en `blocked_by` | de mens, `lighthouse-write-issue`, de tick bij teruggeven |
| `Controller` | de controller draagt het | triage, alleen voor een issue **zonder** blokkadelabel en zonder open `blocked_by` |
| `In progress` | interactief opgepakt | `lighthouse-start-issue` (`lighthouse board-start`) |
| `Done` | gesloten | de forge |

Triage neemt een issue met een `needs_*`-label niet meer op in `Controller`. Nu doet het dat wel
bij 4a/4b/2 ([triage.md](../../../triage.md)).

Staat er toch een issue met een `needs_*`-label op `Controller` (het label kwam er later op, of het
stond er al), dan start de tick geen nieuwe run. Zonder open run zet hij het op `To do`; het label
blijft. Een lopende run maakt hij af zoals nu; de UI toont het als actief met de vlag mens nodig.
*Mijn keuze.*

### Teruggeven door de controller

De controller kent al twee wegen terug naar de mens ([tick.md](../../../tick.md)):

- **🤖 Vraag.** Een open vraag volstaat; het issue blijft op `Controller` en gaat verder na het
  antwoord. Ongewijzigd. In de UI: actief met de vlag mens nodig.
- **🤖 Teruggegeven.** Alleen de tick post die, met een reden `reason_*` uit `internal/dispatch`.
  Nu blijft de Status `Controller` en moet de mens het issue zelf verplaatsen. Nieuw: elke
  Teruggegeven zet het issue op `To do` met een label. De board-Priority blijft staan. *Mijn keuze
  voor het label:*

  | Reden | Label |
  |---|---|
  | `reason_needs_brainstorm` (de handoff wil brainstormen) | `needs_shaping` |
  | `reason_interactive` (de handoff wil een interactieve skill) | `needs_decision` |
  | regel 13 met een actieve blokkade (fix-pr kan niet starten) | `blocked_issue` |
  | alle andere, ook de tijdelijke (`silent`, `timeout`, `start_uncertain`, `state_lost`, `pusher_*`, `permission`) | `needs_hands` |

  Ook een tijdelijke reden gaat van `Controller` af. Een Teruggegeven betekent nu al dat de mens
  iets moet doen; dat blijft zo, en het label maakt het zichtbaar.

Welke van de twee de sessie kiest (een vraag stellen of een handoff die interactief verder moet),
is een oordeel en blijft in de skill die de sessie draait.

### Parkeren wordt teruggeven met `blocked_issue`

Nu parkeert de tick een uitvoerend issue in `Controller` met een actieve voorganger op `On hold`,
en zet het terug als de voorganger dicht is (`hold`, `unpark`). Nieuw:

- De tick zet zo'n issue op `To do` met `blocked_issue` (de relatie bestaat al) en post de
  🤖 Geparkeerd zoals nu.
- **De tick zet het ook terug**, net als nu bij `unpark`. Gather en de vingerafdruk lezen naast
  `Controller` ook de items op `To do` waarvan de laatste 🤖-comment een eigen 🤖 Geparkeerd is.
  Heeft zo'n issue geen actieve voorganger meer (de definitie van de tick: open en zonder gemergde
  PR, ook in een andere repo), dan haalt de tick `blocked_issue` weg en zet hij het op
  `Controller`. De volgende tick hervat de laatste Handoff. *Mijn keuze:* triage is er niet voor
  geschikt; die slaat een issue met een protocolcomment, een branch of een run over, en draait één
  keer per dag.
- Haalt de unblock-lane (`internal/unblock`) `blocked_issue` eerder weg, dan ziet de tick een
  geparkeerd issue op `To do` zonder label en zet het net zo terug.
- Een issue in de spec-, plan- of reviewfase met een actieve voorganger blijft staan, zoals nu.
- De run en de werkplek blijven bestaan zolang het issue geparkeerd is, zoals nu bij `On hold`.

### Migratie van `On hold`

Op 20 november staan 72 issues op `On hold` (lighthouse 7, personal_ai_agent 65). Eenmalig, per
repo, via een commando dat eerst een lijst met voorstel toont en pas na bevestiging schrijft:

- laatste 🤖-comment is een eigen 🤖 Geparkeerd → `To do` met `blocked_issue`;
- anders (door een mens op On hold gezet) → het voorstel is `To do` met `blocked_external`; de
  gebruiker kan per issue kiezen voor backlog (milestone eraf) of zonder label.

Dezelfde ronde toont een tweede lijst: de open issues met `needs_hands` of `needs_testing`. Die
labels droegen tot nu toe ook "een mens aan het eind". Per issue is het voorstel: label eraf, tenzij
de eerstvolgende stap echt van een mens is. Ook hier schrijft het commando pas na bevestiging.

`On hold` verdwijnt uit de verplichte `status_options` (`internal/config/config.go`,
`requiredStatusOptions`). Een board dat de optie nog heeft, blijft geldig; lighthouse zet hem
niet meer.

## Eén plek voor de regels

De deterministische regels komen in één Go-package met een `lighthouse`-subcommando ervoor:

- de toestand van een issue uit labels, Status, `blocked_by` en runs ([tabel](#toestanden));
- een blokkadelabel zetten met de rangregel (hoogstens één, laagste rang wint, de hogere eraf);
- teruggeven: Status `To do` plus label, Priority blijft.

Skills roepen het subcommando aan; tick en triage roepen dezelfde functies rechtstreeks aan.
`internal/triage/rules.go` (gate 0) gaat erop over. Oordelen (welke route, vraag of teruggeven)
blijven in de skills; de dispatcher roept daarvoor een skill aan via `claude -p`, zoals triage al
doet.

*De naam van het subcommando en de package laat ik aan de bouwer.*

## Het statusschema in Prioritering

### Data

- `GET /api/priorities` krijgt per issue het blokkadelabel als sleutel uit `[labels]`
  (`needs_shaping`, …, of leeg), zodat de UI geen labelnamen per repo hoeft te kennen. *Mijn
  keuze.*
- Per milestone ook de **gesloten** issues (nummer, titel, URL), zodat een af-blokje een issue is.
- Of de laatste 🤖-comment een open 🤖 Vraag is, voor de vlag mens nodig. De overview kent de
  open vragen al (`/api/questions`).
- `issueState()` in `web/src/lib/priorities/derive.ts` volgt de [tabel](#toestanden) en is de enige
  afleiding in de UI. De UI-tabel en de Go-regel krijgen dezelfde testgevallen.

### Vormentaal

| Toestand | Blokje (balk) | Kaart (graaf) |
|---|---|---|
| af | effen milestone-kleur | — (de graaf toont alleen open issues) |
| actief | streep van volle milestone-kleur en transparant | kleurstrook links gestreept |
| oppakbaar | effen grijs (`--lh-seg-todo`) | kleurstrook effen |
| mens nodig | als oppakbaar, plus stippelrand | stippelrand |
| geblokkeerd | grijs met een kruis | kruis bij het nummer |

- **Stippelrand = mens nodig**, overal: 1,5 px, gestippeld, `--foreground`, niet in
  milestone-kleur (onzichtbaar op een pastelkleur). Ook op een actief blokje met de vlag.
- **Milestone niet geshaped** (0 issues, of `Status: shaping` in de beschrijving): één
  markering, een grijze stippelrand om de hele balk; percentage grijs met `voorlopig`. Bij
  0 issues staat in de lege balk *nog geen issues*. De grijze streepbalk en de witte waas
  verdwijnen.
- Geel, de gele pil `picked up` en de navy stippelrand verdwijnen. De legenda in de toolbar volgt
  de tabel.

### Milestone-balk

```
[#371][#372][#360][#361][#362][#364][#365][#363]
 └─ af ─┘  └ actief ┘ └ oppakbaar ┘ ┊mens┊  ╳
```

- Volgorde: **gegroepeerd per toestand**, af, actief, oppakbaar, mens nodig, geblokkeerd. Binnen
  een groep de werkvolgorde (diepte in de blocker-graaf, dan component, dan nummer); af op nummer.
- In elk blokje het nummer, als het past. *Mijn keuze: onder 28 px breedte geen nummer; de hover
  blijft.* Hover: `#nr titel`. Klik opent het issue.
- Outside blockers houden hun eigen kolom vóór de balk, met dezelfde vormentaal in de kleur van hun
  eigen milestone. Hun huidige stippelrand wordt een doorgetrokken rand, omdat stippels nu mens
  nodig betekenen. *Mijn keuze.*
- De `to do:`-regel onder de balk blijft, in dezelfde volgorde als de blokjes.

### Afhankelijkhedengraaf

- Kaarten markeren de toestand volgens de tabel. Externe kaarten (andere repo of buiten het
  filter) krijgen een doorgetrokken grijze rand in plaats van een gestippelde.
- **Issues zonder afhankelijkheden** krijgen dezelfde kaart van 210×38 met nummer, titel en
  toestand, in een raster onder de graaf, in plaats van een chip met alleen het nummer.

## Opdeling in issues

| # | Issue | Hangt af van |
|---|---|---|
| 1 | Eén Go-package en subcommando voor toestand, blokkadelabel en teruggeven | — |
| 2 | Het statusmodel doorvoeren: `write-issue` en triage zetten `needs_shaping` bij 4c, triage laat `needs_*` uit `Controller`, Teruggegeven en parkeren gaan naar `To do` met label, `On hold` eruit plus de migratie, docs en skills mee | 1 |
| 3 | Milestone-balk: API (label, gesloten issues, open vraag), `issueState()`, vormentaal, nummers en hover | — |
| 4 | Afhankelijkhedengraaf: dezelfde vormentaal, kaarten voor losse issues | 3 |
| 5 | Modelsuggesties bij *Zonder milestone* (stap 2 van #374) | #372 |

Docs en skills die issue 2 raakt: [labels.md](../../labels.md), [tick.md](../../../tick.md),
[triage.md](../../../triage.md), [install.md](../../../install.md),
[getting-started.md](../../../getting-started.md), [workplace.md](../../../workplace.md), en in
`pack/core/skills/` `write-issue`, `route-issue`, `start-issue` en `review-spec`. Een wijziging
onder `pack/` draait ook `lighthouse init --upgrade` (zie `AGENTS.md`).

## Aan de bouwer

- Namen van package, subcommando en API-velden.
- Hoe de UI-tabel en de Go-regel dezelfde testgevallen delen (een gedeelde JSON-fixture of twee
  gespiegelde tabellen).
- De exacte maat van het kruis en de streep, binnen de vormentaal hierboven, in licht en donker.
