# Prioritering: zes statussen, één vormentaal, een statustab — ontwerp

Mockup: [2026-11-23-prioritering-zes-statussen-mockup.html](2026-11-23-prioritering-zes-statussen-mockup.html).
Open het bestand in een browser. De statussen van #540 en #380 in de mockup zijn verzonnen, als
voorbeeld.

Bouwt voort op het [Prioritering-tabblad](2026-11-16-prioritering-tabblad-design.md) en het
[statusmodel](2026-11-20-statusmodel-en-statusschema-design.md). Gaat uit van #445
(gemerged in #549: sortering op prio-gewicht, schakelaar en hover) en verplaatst de plek van #439.

## Waarom

De Prioritering-tab moet in één oogopslag laten zien in welke status elk issue is. Dat lukt nu niet:

- **De legenda** zet milestonekleuren, statussen en "not shaped yet" op één regel door elkaar.
- **"needs a human" en "not shaped yet"** tekenen allebei een stippelrand. Het verschil is niet te
  zien: het eerste hoort bij een issue, het tweede bij een hele milestone.
- **"done" en "ready"** zijn in de legenda allebei grijs. "Ready" leest in het Nederlands als
  "klaar", dus als done.
- **De graaf** gebruikt drie signalen door elkaar: een stippelrand om de kaart, strepen in de streep
  links en een kruis vóór het nummer. De streep links is tegelijk ook de milestonekleur.
- **Er is geen overzicht per status.** Wat moet geshaped worden, wat wacht op een hand, wat kan
  iemand oppakken en wat loopt er: dat staat nergens bij elkaar.

## Doelen

- Elk open issue heeft precies één status, in het hele model: `lighthouse state show`, de balk, de
  graaf en de statustab.
- Zes statussen, in een vaste volgorde: **blocked · needs shaping · to do · active · needs human ·
  done**.
- Eén vormentaal voor de zes statussen, op elke plek gelijk: legenda, balk, graafkaart en kanban.
- Een onderstreept nummer betekent overal: **een mens moet iets doen**.
- Een legenda met een eigen regel per soort, elk met een kopje.
- Een tab *Status* als kanban: een kolom per status, met een filter op milestone.
- *Without milestone* wordt het laatste blok van de tab *Milestones*. Daarmee zijn er drie tabs:
  **Dependencies · Milestones · Status**.

## Niet-doelen

- De modelsuggesties van #439 zelf. Dit ontwerp geeft ze alleen een plek.
- Slepen tussen kolommen op de statustab. De kanban leest alleen; status verander je op de forge
  of met `lighthouse state`.
- Een eigen vorm voor controller of interactive. Dat onderscheid is een label op de kaart, geen
  zevende vorm.
- Wijzigingen in de dispatcher of de tick. Die lezen `issuestate.Of` niet.

## Anti-doelen

- **Twee regels voor de status.** Als de web en `lighthouse state show` voor hetzelfde issue een
  andere status geven, is het werk mislukt, ook als het er goed uitziet.
- **Meer dan één signaal voor de status op een kaart.** De streep links is alleen de
  milestonekleur. De status staat in het blokje vóór het nummer, en nergens anders.
- **Een legenda-item voor de shaping van een milestone.** De stippelrand om de balk spreekt voor
  zich: het is dezelfde vorm als *needs shaping*.

## Beperkingen

- De statusregel staat in Go (`internal/issuestate.Of`) en in de web (`issueState()` in
  `web/src/lib/priorities/derive.ts`). Beide draaien op dezelfde gevallen in
  `tests/pack/fixtures/issue-state/cases.json`. Dat blijft zo.
- `/api/priorities` levert open issues met `label`, `status`, `needs_label`, `open_question`,
  `blocked_by`, `milestone`, `factor` en `prio_weight`, plus de runs. Per milestone levert de API
  de gesloten issues (`closed`: nummer, titel en url, zonder sluitdatum).
- Gele, grijze en navy kleuren zijn voorbehouden aan status
  (`web/src/lib/priorities/palette.ts`). Dit ontwerp gebruikt geen geel.
- Alle UI-tekst is Engels (#504). Artefacten zijn Nederlands.

## Aanpakken

1. **Zes statussen in het gedeelde model** — *gekozen.* Go `issuestate.Of`, `cases.json` en de web
   krijgen samen `needs_shaping` als eigen status. *Active* met een needs-label of een open vraag
   wordt een mensstatus. Board, CLI en balk zeggen hetzelfde, en daar gaat de milestone over: "één
   toestand per issue, van board tot balk". Wat het kost: de uitvoer van `lighthouse state show`
   verandert. Geen skill of hook leest die uitvoer.
2. **Alleen in de UI.** Go houdt vijf statussen en de web splitst bij het tekenen. Dat is
   goedkoper, maar dan geven de web en de CLI een andere status. Dat breekt de afspraak "één
   regel" in de packagecomment van `issuestate`. Afgewezen.

## Ontwerp

### 1. Statusmodel: zes statussen

De regel vervangt de tabel *Toestanden* van het
[statusmodel](2026-11-20-statusmodel-en-statusschema-design.md#toestanden). De eerste rij die past,
wint:

| # | Status | Wanneer |
|---|---|---|
| 1 | `done` | het issue is gesloten |
| 2 | `needs_shaping` | **actief** (rij 3), met het label `needs_shaping` |
| 3 | `needs_human` | **actief**: een run met `open` of `parked`, of Status `In progress` of `Controller`; én een ander needs-label (`needs_decision`, `needs_hands`, `needs_testing`) of een open 🤖 Vraag |
| 4 | `active` | **actief**, zonder rij 2 of 3 |
| 5 | `needs_shaping` | het label `needs_shaping` |
| 6 | `needs_human` | het label `needs_decision` |
| 7 | `blocked` | `blocked_issue` of `blocked_external`, of een open `blocked_by` |
| 8 | `needs_human` | het label `needs_hands` of `needs_testing` |
| 9 | `ready` | de rest |

- **De gebruiker koos:** zes statussen; `needs_shaping` krijgt een eigen status los van de andere
  drie needs-labels; één status per issue.
- *Mijn keuze:* `needs_decision` valt onder `needs_human`. Een besluit is kort werk voor een mens,
  shaping is lang werk.
- *Mijn keuze:* een actief issue met `needs_shaping` wordt `needs_shaping`, niet `needs_human`. Wie
  het interactief shapet, ziet het zo in de kolom waar het hoort.
- De rangvolgorde van rij 5 tot 8 blijft die van [labels.md](../../labels.md). Een open voorganger
  wint van hands en testing, niet van shaping en decision.
- De vlag `NeedsHuman` in `issuestate.Result` vervalt. De status zegt het al.
- De interne waarde blijft `ready`. Alleen de UI-tekst wordt "to do" (zie 3).
- `lighthouse state show <nr>` print `state: #<nr> <status>`, zonder `, needs human`.
- `cases.json` krijgt de nieuwe rijen. Elke rij heeft minstens één geval, ook "actief met
  `needs_shaping`" en "actief met een open vraag".

### 2. Vormentaal

Eén component tekent een status, overal: legenda, balk, graafkaart, kanbankolom.

| Status | Vorm | Nummer onderstreept |
|---|---|---|
| blocked | lichtgrijs (`--lh-seg-todo`) met een kruis van hoek tot hoek, als vector | nee |
| needs shaping | achtergrond van de kaart, met een stippelrand (`1.5px dashed` in `--foreground`) | **ja** |
| to do | lichtgrijs (`--lh-seg-todo`) | nee |
| active | strepen in de milestonekleur (`.lh-stripe`) | nee |
| needs human | strepen in de milestonekleur, met een dichte rand (`inset 0 0 0 2px` in `--foreground`) | **ja** |
| done | vol in de milestonekleur | nee |

- **Kruis als vector.** Het kruis is een SVG-achtergrond met `preserveAspectRatio="none"` en
  `vector-effect="non-scaling-stroke"`, in plaats van de twee `linear-gradient`s van `.lh-cross`.
  Zo blijven de lijnen glad in brede blokken. De gebruiker koos dit na de vergelijking in de
  mockup.
- **Onderstreping.** Het nummer krijgt `text-decoration: underline`, 2px dik, 2px onder de tekst.
  Dat geldt in het chipje in de balk, op de graafkaart, op de kanbankaart en in het label van de
  legenda. Een smal blok zonder nummer herken je aan de vorm.
- **Balkvolgorde.** De blokken staan in de omgekeerde volgorde van de legenda: done · needs human ·
  active · to do · needs shaping · blocked. Zo staat *needs human*, vaak bijna af, naast *done*.
  Dit vervangt `GROUP_ORDER` in `derive.ts`.
- **Milestone in shaping.** De stippelrand om de hele balk blijft (`Status: shaping` in de
  beschrijving, zie `isShaping`). Het is dezelfde vorm als *needs shaping*, zonder eigen
  legenda-item. "provisional" bij het percentage blijft.
- In de donkere modus volgen rand, kruis en onderstreping `--foreground` en `--lh-cross`, net als nu.

### 3. Legenda

Op elk tabblad twee regels, elk met een kopje in kleine hoofdletters:

- **Milestones**: de kleur per milestone die het tabblad toont, met *Without milestone* in grijs
  zodra issues zonder milestone in beeld zijn.
- **Status**: de zes statussen, in de volgorde uit 1, met de vorm uit 2. Het label van *needs
  shaping* en *needs human* is onderstreept. De labels zijn `blocked`, `needs shaping`, `to do`,
  `active`, `needs human` en `done`.

"not shaped yet" en "needs a human" verdwijnen als legenda-items. Het blokje van *done* en *active*
in de legenda gebruikt grijs (`--lh-grey`), want het staat los van een milestone.

### 4. Dependencies: de graafkaart

De kaart heeft één signaal voor de status.

- De streep links is de milestonekleur, altijd effen, zonder strepen.
- Vóór het nummer staat een statusblokje van 14 × 14 px met de vorm uit 2.
- Het nummer staat zonder `#` en is onderstreept bij *needs shaping* en *needs human*.
- De stippelrand om de kaart en het losse kruis vervallen. Een externe kaart (andere repo of buiten
  het filter) houdt haar grijze rand: die zegt iets over de herkomst, niet over de status.
- *Mijn keuze:* een externe kaart heeft **geen** statusblokje. Haar status is onbekend
  (`buildGraph` maakt haar met `blank()`), en een gegokt *to do* zou kunnen afwijken van
  `lighthouse state show`. Daarmee is er ook geen zevende vorm "onbekend" nodig.
- Rechts blijft het pilletje met het aantal wachtende issues.
- Hover-titels gebruiken geen `#` meer (`540 Live-verificatie …`).

Sortering per kolom, de schakelaar (`depsOrder`) en de hover van het pilletje komen uit #445 en
blijven ongewijzigd. Dit ontwerp verandert alleen de kaart zelf.

### 5. Milestones, met onderaan *Without milestone*

- De tab *Without milestone* vervalt. Zijn inhoud wordt het laatste blok van *Milestones*.
- **De rij *Without milestone*** heeft dezelfde opbouw als een milestonerij. Links staan een grijs
  vlakje, de titel en het aantal open issues. Daarnaast een balk met een blok per issue, in de
  balkvolgorde uit 2.
- Rechts staat geen percentage maar een streepje. Er zijn geen knoppen om te verplaatsen, en de rij
  staat altijd onderaan, in beide volgordes (*%* en *Priority*).
- Onder de balk staat de lijst van nu (`UnassignedView`): blokje, nummer, titel, de relatie
  (`blocks 12` of `waits on 12`, zonder `#`) en bij een gesloten milestone `<titel> · closed`.
  Wat er in die lijst staat, blijft gelijk: open issues zonder milestone én open issues in een
  gesloten milestone. De lijst toont de eerste 5 regels en klapt de rest in.
  *Mijn keuze:* het getal 5 sluit aan bij de done-kolom van de kanban.
- *Mijn keuze:* de rij krijgt een gewone, dichte rand. In de verkenningsmockup was die gestippeld,
  maar een stippelrand betekent hier *needs shaping*.
- Zonder issues zonder milestone verdwijnt de rij.
- **#439** zet zijn suggesties (groepen, passende milestone, nieuwe milestone, mogelijk achterhaald)
  in dit blok, onder de lijst. De tekst van #439 wordt daarop aangepast. De plek is in dit ontwerp
  al gereserveerd, maar blijft leeg tot #439 klaar is.

### 6. Status: de kanban

- **Kolommen:** zes, in de volgorde van de legenda: blocked · needs shaping · to do · active ·
  needs human · done. Elke kolom heeft een kopje met het statusblokje, de naam en het aantal.
- **Kaart:** links een streep in de milestonekleur (grijs zonder milestone), dan het nummer
  (onderstreept bij een mensstatus) en de titel op één afgekapte regel. De kaart linkt naar het
  issue.
- **controller of interactive.** Een kaart waarvan het issue een run met `open` of `parked` heeft,
  of Status `Controller`, krijgt het label `controller`. Status `In progress` zonder zo'n run geeft
  `interactive`. *De gebruiker koos* dit label op *active*. *Mijn keuze:* het label staat ook op een
  kaart in *needs shaping* of *needs human* die uit rij 2 of 3 van de tabel komt. Je ziet dan of
  de controller of je eigen sessie op je wacht.
- **Volgorde per kolom:** `prio_weight` van hoog naar laag, daarna het nummer van laag naar hoog.
- **done:** de gesloten issues van de open milestones in beeld (`Milestone.closed`). Ze staan op
  nummer van hoog naar laag, want de API kent geen sluitdatum. De kolom toont de eerste 5 en klapt
  de rest in. *Mijn keuze:* meer gesloten issues levert de API niet, en een done-kolom zonder grens
  groeit eindeloos.
- **Filter op milestone:** een chip per open milestone in beeld, plus *Without milestone*. Een chip
  zet je aan of uit, en standaard staan ze allemaal aan. Het filter op repo blijft daarnaast
  bestaan. De keuze blijft per browser bewaard, net als het filter op repo
  (`lighthouse.priorities` in `localStorage`).
- *Mijn keuze:* het filter op milestone staat **alleen op de tab Status**. De gebruiker vroeg het
  voor de kanban; *Dependencies* en *Milestones* blijven zoals ze zijn.
- *Mijn keuze:* een open issue in een gesloten milestone telt op de kanban als *Without
  milestone*: grijze streep, onder de chip *Without milestone*. Dat sluit aan bij 5, waar zo'n
  issue ook in het blok *Without milestone* staat.
- Het getal op de tab *Status* is het aantal open issues in beeld.
- Geen slepen (zie Niet-doelen).

### 7. Voorkeuren

- `ViewId` wordt `'deps' | 'milestones' | 'status'`.
- Een bewaarde `'unassigned'` valt terug op `'milestones'`, want daar staat die inhoud nu.
- Het filter op milestone komt erbij in `Prefs` en geldt alleen voor de tab Status. Een bewaarde
  milestone die niet meer bestaat, telt niet mee, zoals bij het filter op repo.

## Opdeling

Drie nieuwe issues in milestone *Statusmodel: één toestand per issue, van board tot balk*, elk met
een eigen PR, in deze volgorde:

| | Inhoud |
|---|---|
| a | 1 tot en met 4: zes statussen in Go, `cases.json` en de web; de vormentaal met het vectorkruis en de onderstreping; de legenda met kopjes; de balkvolgorde; de graafkaart met één signaal. |
| b | 5 en 7: *Without milestone* onder *Milestones*, de tab vervalt; de tekst van #439 aangepast. |
| c | 6 en 7: de tab *Status*. |

b en c hangen allebei af van a, en niet van elkaar. Beide raken `prefs.ts` en `ViewId`; wie als
tweede merget, voegt samen.

## Aan de bouwer

- Hoe het statusblokje als component wordt gedeeld (`Block` uitbreiden of een nieuwe component).
- De exacte afmetingen en tussenruimte van de kanbankaarten en -kolommen, en hoe de kanban zich
  op een smal scherm gedraagt. Horizontaal scrollen binnen het paneel mag.
- De tekst van de metaregel onder een milestonetitel (`7 done · 1 active · 2 not yet`), zolang de
  telling klopt met de zes statussen.
- Hoe het inklappen eruitziet (`+ 6 more ▾` of een andere vorm).

## Keuzes die ik maakte

Neem deze door bij het lezen. Alle andere keuzes komen van de gebruiker.

1. `needs_decision` valt onder `needs_human`.
2. Een actief issue met `needs_shaping` wordt `needs_shaping`. Met een ander needs-label of een
   open vraag wordt het `needs_human`.
3. Het label `controller` of `interactive` staat ook op een kaart in een mensstatus die uit een
   actief issue komt.
4. De done-kolom toont alleen gesloten issues van open milestones, op nummer, ingeklapt na 5.
5. *Without milestone* heeft een dichte rand, geen percentage en geen knoppen om te verplaatsen,
   en staat altijd onderaan.
6. Kanbankaarten staan per kolom op `prio_weight`, daarna op nummer.
7. Een externe graafkaart houdt haar grijze rand en heeft geen statusblokje, want haar status is
   onbekend.
8. Het filter op milestone staat alleen op de tab Status.
9. Een open issue in een gesloten milestone telt op de kanban als *Without milestone*.
10. De nieuwe graafkaart valt onder deel a, zodat de graaf nooit twee mensstatussen gelijk toont.
11. De lijst onder *Without milestone* houdt de relatie `blocks 12` of `waits on 12`.
