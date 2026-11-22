# Nacontrole met vervolgstap, en een spec voor een geblokkeerd issue (#394)

Issue: [#394](https://github.com/CorneeldH/lighthouse/issues/394). Milestone 18, *Controller:
betrouwbaar onbemand draaien*.

## Waarom

Na de merge van een `Refs`-PR start de controller één nacontrole (`/lighthouse-close-issue <nr>`,
trigger `closecheck`). Zijn niet alle criteria aantoonbaar klaar, dan moet de run eindigen in een
vervolgstap. De DoD van de milestone noemt er drie: een handoff, een vraag, of wachten op een
issue. Zonder vervolgstap valt het issue stil, en de reden *geen handoff of vraag* wijst dan de
verkeerde kant op.

PR #412 leverde de vraag: bij open criteria stelt B7 één vraag via `lighthouse ask`, met de opties
*nieuw werk*, *afsplitsen* en *sluiten*. Op antwoord *nieuw werk* volgt een handoff
`next: /lighthouse-start-issue <nr>`. Open bleven: het wachten op een ander issue, de reden bij
teruggeven, de decide-tests en `docs/tick.md`.

Daarnaast, op verzoek van de gebruiker in dezelfde sessie: een geblokkeerd issue mag een spec
krijgen, maar geen plan. Een spec hangt zelden af van de code van de voorganger, een plan wel.
Vandaag geldt interactief het omgekeerde (poort 0 stopt alles) en in de controller het ruimere
(spec én plan mogen).

## Doelen

1. **Wachten op een issue zonder mensbeurt.** Wacht elk open criterium van een nacontrole op een
   ander issue, dan legt B7 de relatie en post een handoff terug naar de nacontrole. De controller
   parkeert het issue en start de nacontrole opnieuw zodra de voorganger sluit.
2. **Een eerlijke reden.** Geeft de controller een nacontrole toch terug zonder handoff of vraag,
   dan noemt de reden de nacontrole.
3. **Spec ja, plan nee bij een blokkade.** Interactief en in de controller: een issue met een
   actieve voorganger of een blokkadelabel mag door naar de spec, en stopt vóór het plan.

## Niet-doelen

- Geen nieuwe controllerfunctie (No-Go van de milestone). Het wachten gebruikt de bestaande
  parkeer- en terugzetregels.
- AC1 zoals #412 het leverde blijft staan: de handoff naar `start-issue` komt na antwoord *a* op
  de vraag, niet zonder vraag. Codewerk vraagt een keuze van de gebruiker (*nieuw werk* of
  *afsplitsen*); die keuze blijft.
- Een voorganger in een andere repo: `lighthouse forge issue block` legt alleen een relatie binnen
  de repo. Een criterium dat op een issue elders wacht, valt onder de vraag.
- Geen wijziging aan de unblock-lane.

## Anti-doelen

- Een geparkeerd issue dat na het sluiten van de voorganger niet terugkomt: dan is het wachten
  stiller dan de oude *geen handoff of vraag*.
- Een lus: een nacontrole die zichzelf steeds opnieuw start zonder dat er iets veranderde.

## Benaderingen

**Wachten op een issue (doel 1).** Gekozen: **A**.

- **A — zonder vraag** (gekozen). Wacht elk open criterium op een ander issue in deze repo, dan
  legt B7 per voorganger de relatie en post een handoff `next: /lighthouse-close-issue <nr>`. Geen
  mensbeurt; past op de DoD van de milestone (*wachten op een issue* als eigen vervolgstap).
- **B — vierde optie in de vraag** (*wacht op #x*). Altijd een mensbeurt, ook als het antwoord
  vaststaat. Afgewezen: het doel van de milestone is onbemand draaien.

**Waarom terug naar de nacontrole, niet naar `start-issue`.** Na het sluiten van de voorganger is
er meestal niets te bouwen: het criterium klopt dan vanzelf, of niet. De nacontrole toetst dat;
`start-issue` zou eerst moeten ontdekken dat er niets te doen is.

**Spec ja, plan nee (doel 3).** Eén zinnige weg: de bestaande controllerregel (doorrouteren met
`— blocker #<x> open`) wordt de regel voor beide modi, en de grens schuift van *vóór uitvoering*
naar *vóór het plan*.

## Ontwerp

### 1. B7: drie soorten open criteria

B7 in `pack/core/skills/close-issue/{claude,agents}/SKILL.md` deelt elk open criterium in:

| Soort | Kenmerk | Vervolgstap |
|---|---|---|
| wacht op een issue | het criterium kan pas kloppen als een ander open issue in deze repo sluit | relatie leggen, handoff terug naar de nacontrole |
| codewerk of docs | het ontbrekende is in deze repo te bouwen | vraag (#412), aanbeveling *a* |
| handwerk of een gebeurtenis | een commando van de gebruiker, een live systeem, een externe run | vraag (#412), elk commando als `--command` |

Uitkomst over alle open criteria:

- **Alle open criteria wachten op een issue in deze repo.** Per voorganger
  `lighthouse forge issue block <nr> --by <voorganger>` (de relatie komt vóór de handoff; zonder
  `--by` faalt het commando met exit 2), dan `/lighthouse-write-handoff` met
  `next: /lighthouse-close-issue <nr>`. Geen vraag. Het after-merge-comment noemt per criterium de
  voorganger. De handoff noemt zijn voorgangers ook zelf (veld State of Context), zodat twee
  rondes nooit dezelfde handoff-tekst geven *(eigen keus na spec review, r1-f4)*: `repeatsHandoff`
  (`internal/dispatch/decide.go`) geeft bij een identieke tekst `reason_repeat_handoff` in plaats
  van een derde nacontrole.
- **Anders** (een mix, of geen enkel wachtend criterium): de vraag van #412. Een wachtend criterium
  staat in `--what` met zijn voorganger. De aanbeveling volgt de bestaande regels.

Mislukt `lighthouse forge issue block`, dan valt B7 terug op de vraag: zonder relatie parkeert de
tick niet, en zou de handoff meteen een nieuwe nacontrole starten.

*Wacht op een issue* telt alleen voor een voorganger die `OPEN` is en geen gemergde PR heeft,
dezelfde maat als `activeBlockers` in de tick. `lighthouse forge issue blockers` geeft alleen
nummer en staat; de gemergde PR van een voorganger leest B7 met `lighthouse forge pr list` *(eigen
keus na spec review, r1-f5)*. Een voorganger met een gemergde PR parkeert niet;
de handoff zou meteen weer een nacontrole starten. Zo'n criterium valt onder de vraag. *(Eigen
keus, uit de builder-check.)*

### 2. De controller: parkeren en terugkomen (bestaand)

Geen nieuwe regel. Een handoff `next: /lighthouse-close-issue` krijgt verdict `VerdictEnd` en het
commando `/lighthouse-close-issue <nr>` (`internal/dispatch/decide.go`, het handoff-startpad).
`Executing` geeft daarvoor `true`, dus bij een actieve voorganger parkeert de tick het issue
(🤖 Geparkeerd, `blocked_issue`, To do). Sluit de voorganger, of mergede zijn PR, dan zet de tick
het terug naar Controller (`unpark`), en de volgende tick start de handoff: een nieuwe
nacontrole.

Te bewijzen met fixtures, niet aan te nemen:

- de run van de nacontrole die met die handoff eindigt, sluit als `handoff` (regel 13c) en niet
  via regel 14;
- de start van de handoff parkeert bij een actieve voorganger, ook binnen dezelfde tick als 13c;
- na `unpark` start de handoff (sleutel `handoff:<comment-id>`), op een werkplek die de
  nacontrole aankan. De branch in de handoff is `closecheck/issue-<N>-pr-<pr>`; die is nooit
  gepusht, en `FollowRemote` (`internal/workplace/remote.go`) laat een bestaande werkplek dan op
  zijn oude HEAD staan. Dat is wél een gat voor B2 (zet 2 en 3, en de commando's van de criteria): die lopen in de cwd,
  en alleen B2a meet in een wegwerp-worktree op `origin/<default branch>`. Daarom wijzigt B7 in de
  controllermodus de maat: `git fetch origin` en dan B2 (zetten 2 en 3) en elk criteriumcommando
  in een wegwerp-worktree `git worktree add --detach <scratch>/… origin/<default branch>`, zoals
  B2a en C2 al doen. De HEAD van de closecheck-werkplek doet er dan niet toe. Een proza-contract
  pint dat mechanisme, niet alleen de zin, en een fixture/test toont dat de maat op
  `origin/<default branch>` loopt *(eigen keus na spec review, r1-f3, r2-f1)*. Geen nieuwe functie.

Geen lus: de sleutel `handoff:<comment-id>` wordt één keer gebruikt. Een tweede nacontrole met
weer alleen wachtende criteria legt een nieuwe relatie of vindt er geen open voorganger meer;
zonder open voorganger valt hij onder de vraag.

### 3. Reden bij teruggeven (AC4)

Regel 14 (`noHandoff`) krijgt bij een nacontrole een eigen reden. Een nacontrole is een run
waarvan het commando `/lighthouse-close-issue` is, ongeacht de trigger: de eerste start heeft
trigger `closecheck`, een herstart na `unpark` trigger `handoff`.
Alleen regel 14 met dat commando krijgt de nieuwe reden, voor beide triggers; een 13c-einde
sluit als `handoff` en bereikt regel 14 niet *(eigen keus na spec review, r1-f8)*.

| Sleutel | nl | en | Label |
|---|---|---|---|
| `reason_closecheck_open` | nacontrole: criteria open | after-merge check: criteria open | `needs_hands` |

De sleutel komt in `pack/core/headings.toml` (en via `init --upgrade` in
`.lighthouse/headings.toml`), met een rij in `TestReturnLabelKeyTable`. Regel 14a gaat voor: een
mislukte post blijft *post mislukt*.

### 4. Spec ja, plan nee bij een blokkade

**Route-issue, poort 0** (`pack/core/skills/route-issue/SKILL.md`). Een issue met
`labels.blocked_issue`, `labels.blocked_external` of een open `blocked_by` stopt niet meer op
poort 0, maar loopt door poort 1–4. De regel krijgt `— blocker #<x> open` of
`— blocked: <label>` erachter. Voor de bestaande uitzonderingen *(eigen keus, uit de
builder-check)*:

- *unknown* (blokkadelijst onleesbaar) blijft een stop op poort 0: een onleesbare blokkade valt
  nooit stil door.
- *lapsed*, *unlinked* en *label missing* routeren door, met de melding en het herstelcommando
  achter de regel. Ze tellen als blokkade: alleen 4c gaat door.
- Zonder issuenummer (bij `blocked_external`, *lapsed* of *unlinked*) zegt de stopmelding
  *wacht op: \<label\>* in plaats van *wacht op #x*.

**Start-issue, stap 4** (`pack/core/skills/start-issue/{claude,agents}/SKILL.md`), in beide modi:

| Poort | Met blokkade |
|---|---|
| 4c | uitvoeren: brainstorming, spec |
| 4b, 4a, 1, 2 | niet uitvoeren: stoppen met *wacht op #x* (interactief), of een handoff `next: /lighthouse-start-issue <nr>` (controller) |

Bij poort 4b met een blokkade gaat de scout niet uit; start-issue meldt
`heading:scout_skipped — blocker #<x> open, het plan wacht op de merge.`
`lighthouse-write-issue` zegt over de stapelvolgorde voortaan: spec ja, plan en uitvoering
wachten op een gemergde PR van de voorganger.

In de controller parkeert de tick die handoff (bestaand: `start-issue` is uitvoerend). De
uitzondering voor een verrekende blokkade (`stack:` of `blocker:` in de laatste 🤖 Gestart) blijft.
Na een goedgekeurde spec wacht het plan op de voorganger *(eigen keus, uit de builder-check)*:

- **Interactief.** Start-issue roept `superpowers:writing-plans` zelf nooit aan: bij poort 4c geeft
  het brainstorming over, en brainstorming (extern) roept na de spec writing-plans aan, terwijl
  `lighthouse-review-spec` interactief eindigt met een handoff `next: /superpowers:writing-plans`.
  Daarom staat de stop op twee plekken *(eigen keus na spec review, r1-f2)*: (1) start-issue geeft
  brainstorming bij poort 4c met een blokkade de opdracht mee om na de goedgekeurde spec te
  eindigen met *wacht op #x*, zonder writing-plans; (2) de interactieve afsluiting van
  `lighthouse-review-spec` schrijft bij een actieve voorganger *wacht op #x* in plaats van
  `next: /superpowers:writing-plans`. De controllermodus van review-spec verandert niet.
- **Controller.** `lighthouse-review-spec` blijft in de controllermodus ongewijzigd en post `next: /superpowers:writing-plans`.
  De tick parkeert die handoff bij een actieve voorganger (de nieuwe parkeerregel hieronder), en
  start hem na `unpark`.

Het board blijft bij een blokkadelabel op To do (stap 3 slaat `board-start` over, zoals nu). Dat
geldt ook voor een open `blocked_by` zonder label (*label missing*): stap 3 slaat dan ook
`board-start` over *(eigen keus na spec review, r1-f7)*.

**Controller** (`internal/dispatch`). Vandaag parkeert de tick alleen een uitvoerende handoff;
`writing-plans`, `review-plan` en `review-spec` zijn vrij. Voortaan zijn alleen de specfase-
commando's vrij (`/lighthouse-review-spec` en zijn herschreven namen; brainstorming geeft het
issue al terug). Een handoff naar `writing-plans` of `review-plan` parkeert bij een actieve
voorganger.

Bron: de ongecommitte worktree `fix-blocker-parkeert-plan` (basis `b6d55f4a`, 2026-11-20). Die
voegt `SpecPhase(command)` toe (waar alleen de specreview `true` geeft), zet de parkeercheck in
het handoff-pad op `!SpecPhase(j.Command) && c.active()`, en heeft de fixtures
`handoff-blocker-writing-plans-park.json` en `handoff-blocker-plan-review-park.json`. Overnemen
met twee correcties voor de huidige main:

- `Executing` blijft bestaan: `repeatsHandoff` gebruikt het sinds die basis, en dat gedrag
  verandert niet. `SpecPhase` komt ernaast.
- De proza van die worktree spreekt van `On hold`; main parkeert naar `To do` met
  `blocked_issue`. De tekst volgt main.

`labels.blocked_external` verandert in de controller niet, ook niet in de proza van poort 0: de
zin *blocked_external always stops, here too* blijft staan *(eigen keus na spec review, r1-f6)*. Een issue met dat label houdt een mens
vast (`heldByHuman`) en de controller start het niet. Een spec bij `blocked_external` is dus
alleen interactief.

### 5. Documentatie

- `docs/tick.md`: regel 14 met de nieuwe reden en een zin over de nacontrole die op een issue
  wacht; *Blokkades*: "een issue in de spec-fase blijft staan" in plaats van "spec-, plan- of
  reviewfase".
- Fragment in `changelog.d/`.
- `go run ./cmd/lighthouse init --upgrade` gedraaid en het geïnstalleerde exemplaar gecommit.

## Tests

- **Decide-fixtures** (`internal/dispatch/testdata/decide/`), één per route:
  - nacontrole klaar zonder handoff of vraag → `reason_closecheck_open`, zowel met trigger
    `closecheck` als met trigger `handoff` (herstart);
  - nacontrole met handoff `next: /lighthouse-close-issue` en een actieve voorganger → Park;
  - geparkeerd issue, voorganger gesloten → Unpark, volgende tick start de nacontrole;
  - tweede nacontrole-handoff na `unpark` met andere tekst (andere voorganger) → start, geen `reason_repeat_handoff`;
  - nacontrole met een vraag → run sluit als vraag (bestaand gedrag, vastgelegd);
  - handoff `next: /superpowers:writing-plans` met actieve voorganger → Park;
  - handoff `next: /lighthouse-review-spec` met actieve voorganger → start.
- **Proza-contracten** (`internal/packcontract/`): B7 noemt de drie soorten,
  `lighthouse forge issue block` vóór de handoff en de terugval op de vraag; poort 0 stopt niet
  meer op een blokkade maar zet `— blocker #<x> open` achter de regel; start-issue voert bij een
  blokkade alleen 4c uit; de interactieve afsluiting van review-spec schrijft bij een actieve
  voorganger *wacht op #x*; B7 pint `git fetch origin` en meten tegen `origin/<default branch>`. De varianten `claude` en `agents` zeggen hetzelfde.
- Bestaande prozatests die letterlijke B7- en poort-0-zinnen vastpinnen, aanpassen waar de tekst
  verandert, niet verwijderen.

## Aan de bouwer

- De exacte vorm van het predicaat in `internal/dispatch` (§4, controller).
- De woordkeus in B7, mits de drie soorten en de terugval erin staan.
- De volgorde van commits: B7 en reden (#394) los van spec-ja-plan-nee.

Eén PR, `Refs #394`. Spec-ja-plan-nee gaat mee op verzoek van de gebruiker en staat in de PR-body
en het changelog-fragment als tweede wijziging. *(Eigen keus.)*
