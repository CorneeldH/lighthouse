# Rode policy of lint van de workflow-pusher gaat terug naar het werk

Issue: #505. Voorgangers: de workflow-pusher (#396, #410) en *Pusher-rood zonder SHA is een
Teruggegeven* (#510). Aanleiding: CorneeldH/personal_ai_agent#1806.

## Aanleiding

Een run die `.github/workflows/` raakt, pusht niet zelf. Hij schrijft een handoff
`next: push-workflows` en de tick start de workflow-pusher (`internal/pusher`). Die toetst de
worktree, de policy op de workflow-diff (`policy`), actionlint en zizmor (`lint`), en laat een
pre-merge-workflow reviewen voor hij pusht.

Is `policy` of `lint` rood, dan is er nog geen SHA die een antwoord `push` mag doorlaten. De pusher
post daarom een 🤖 Teruggegeven zonder sessie (`reason: workflow-pusher rood: <stap>`,
`internal/pusher/outcome.go`, `renderReturned`). De push-row sluit als `returned`. De volgende tick
geeft het issue terug met label en `To do` (`finishReturn` in `internal/dispatch/decide.go`). De
gebruiker moet dan zelf repareren of een reparatie starten.

Vaak is de fout klein en van het werk zelf. In personal_ai_agent#1806 was `policy` rood op twee
workflows zonder `permissions:` op workflowniveau. Die twee regels had de run zelf kunnen toevoegen.

## Doel

Een rode `policy` of `lint` die een fout van het werk is, gaat terug naar de run die de push-handoff
schreef. De controller hervat die sessie met de rode stap en de reden. De run repareert, commit en
schrijft een nieuwe handoff `next: push-workflows`. Daarna draait de pusher opnieuw, met alle
stappen en de review. Na een vast aantal rondes zonder groen gaat het issue terug naar de gebruiker.

## Niet-doelen

- **Geen reparatie voor andere stappen.** `worktree`, `ff`, `roborev`, `permit`, `review`, `token`
  en `push` blijven gaan zoals nu: een Vraag of een Teruggegeven.
- **Geen reparatie bij `COULD NOT CHECK`.** Is de diff, een workflow of de uitvoer van een linter
  niet leesbaar, of staat een linter niet op `PATH`, dan is dat geen fout van het werk. Dat blijft
  een Teruggegeven naar de gebruiker.
- **Geen wijziging aan de merge-pusher** (`internal/pusher/merge.go`). Zijn rood blijft naar de
  gebruiker gaan.
- **Geen verse run als terugval.** Is de sessie niet te hervatten, dan vangt de bestaande `retry`
  dat op (zie *Ontwerp*, punt 5).
- **Geen instelbare N.** Het aantal rondes is een constante in de code, zoals `maxFixPRRuns`.

## Anti-doelen

- Een reparatieronde die op het issue leest als "Teruggegeven" terwijl het werk doorloopt.
- Een reparatie die de review overslaat. Na elke reparatie draait de pusher volledig opnieuw.
- Een lus zonder eind. Elke Herstel telt mee, tot een groene doorgang of een hand-back.
- Decide die `COULD NOT CHECK` uit vrije tekst moet raden. De pusher beslist of een rood
  repareerbaar is.

## Aanpakken

| | Aanpak | Afweging |
|---|---|---|
| A | De pusher post zijn Teruggegeven zoals nu, met een veld `repair: yes`; decide hervat in plaats van terug te geven | Kleinste wijziging, maar op het issue staat "Teruggegeven" terwijl het werk doorloopt, en `finishReturn` moet een uitzondering krijgen |
| **B (gekozen)** | De pusher post een eigen 🤖-soort **Herstel** voor een repareerbaar rood; decide hervat daarop de sessie van de handoff | Raakt protocol en headings, maar het issue zegt wat er gebeurt; `finishReturn` blijft ongemoeid |
| C | De pusher blijft gelijk; decide leest stap en reden uit de body van de Teruggegeven | Decide moet `COULD NOT CHECK` uit vrije tekst halen: anti-doel |

Corneel koos B.

## Ontwerp

### 1. Nieuwe soort: 🤖 Herstel

Nieuwe `protocol.Kind` `repair`, met heading `kind_repair`: nl `Herstel`, en `Repair`. In
`pack/core/headings.toml` en de geïnstalleerde `.lighthouse/headings.toml` (dogfood).

Een Herstel komt alleen van de pusher, zonder sessiefooter. Header: `step:` en de velden van
`pushFields` (`worktree`, `branch`, `after`, `lane`). Body: de rode stap en de reden (zoals
`redLines`), de diff opgevouwen, de bijlage als de diff is afgekapt. Dezelfde scrub en
`fitQuestion`-grens als nu. Geen push-commando: het werk gaat verder, niet de gebruiker.

### 2. De pusher kiest Herstel of Teruggegeven

In `ask`: een rood zonder SHA (`hintManual`) waarvan de stap `policy` of `lint` is en dat geen
`COULD NOT CHECK` is, wordt een Herstel. De push-row sluit dan als `runs.Handoff`: het werk gaat
naar een andere run. Elk ander rood zonder SHA blijft een Teruggegeven die als `returned` sluit.

Ook `policy` rood op de workflow-diff (`pusher.go`, `workflowDiff`) is `COULD NOT CHECK` en blijft
dus een Teruggegeven.

Alleen het prefix `COULD NOT CHECK:` maakt een rood niet-repareerbaar. Een `RuleParse`-overtreding
van de policy (een workflow die een symlink is, te groot, of geen geldige YAML) is een fout in een
bestand van het werk, en dus wel repareerbaar. *(eigen keuze na spec-review, r1-f4)*

Hoe de pusher `COULD NOT CHECK` herkent, is aan de bouwer: een veld op `Red` of één functie op het
prefix. Eis: decide hoeft het niet te weten. Een unittest dekt de classificatie op het prefix.
End-to-end tests dekken de bereikbare plekken: een linter niet op `PATH`, linteruitvoer die geen
JSON is, en een fout van `workflowDiff` in `pusher.go`. *(eigen keuze na spec-review, r1-f5)*

### 3. Settle sluit de push-row op een Herstel

Sluit de pusher zijn row niet (de release faalde na de post), dan sluit `settle` hem: een Herstel
zonder sessie vanaf de start van een push-row sluit die row als `runs.Handoff`, naast de bestaande
regel voor `pushReturned`. `settle` erkent een Herstel alleen als de auteur
`Snapshot.TokenLogin` is, net als de regel `repair`. Een Herstel van een andere vertrouwde auteur
sluit niets.

### 4. Nieuwe regel `repair` in `next()`

Volgorde: `retry, finishReturn, usageResume, answer, repair, closeCheck, newIssue, handoff`.
`lastTurn` telt een Herstel als beurt.

De regel geldt als de laatste beurt een Herstel is van de token-login, en de trigger-sleutel
`repair:<comment-id>` nog door geen run is gebruikt. Dan:

1. **Rondes tellen.** Tel de Herstel-rondes zonder groen ertussen (zie punt 7). Is dat aantal,
   de nieuwe Herstel meegeteld, groter dan `maxRepairRuns` (constante, waarde `2`), dan volgt een
   Teruggegeven met reden `reason_pusher_red_ceiling`. De hand-back gaat via de bestaande
   `c.returned`, dus met label en `To do`. Het argument is `pushCommand(<worktree van de
   Herstel>)`, zoals bij `pushAgain`; de stap en de reden staan in de Herstel erboven.
   *(eigen keuze na spec-review, r1-f7)*
2. **Run zoeken.** Een Handoff-comment draagt geen sessie: de write-handoff-skill schrijft geen
   sessiefooter. Decide zoekt de run daarom in de historie, met dezelfde regel waarmee de pusher
   de worktree vindt (`runs.LastHandoffWorkspace`, `pusher.ResolveWorktree`). Neem de laatste
   push-row van dit issue die vóór de Herstel startte. Zijn sleutel `push:<handoff-comment-id>`
   wijst de Handoff-comment aan. Neem dan de laatste run van dit issue op deze host (`Node` leeg)
   die als `handoff` sloot, geen push-row is (`isPush`) en vóór die Handoff-comment startte. Die
   run levert `SessionID`, `Command` en `Branch`. *Keuze van de schrijver*: geen sessiefooter op
   de handoff, want dat raakt `pack/`, de fixtures en oudere handoffs.
3. **Geen run of geen sessie.** Is er zo'n run niet, of heeft hij geen sessie, dan een
   Teruggegeven met `reason_pusher_red` en `pushCommand(<worktree>)` als argument. De stap en de
   reden staan in de Herstel erboven.
4. **Anders hervatten.** `KindResume`, `modeResume`, trigger `repair`, sleutel
   `repair:<comment-id>`, `Attempt` 1, `SessionID`, `Command` en `Branch` van die run,
   `CommentID` van de Herstel. Plaatsing via `c.planned`, zoals `answer`. `triggerComment` kent
   het prefix `repair:`, zodat een tweede poging (`secondAttempt`) de comment-id houdt.

   De tabel `runs` kent geen trigger `repair`: de `CHECK` in
   `internal/store/migrations/0008_runs_schedule.sql` laat alleen een vaste lijst toe. Apply zet
   `repair` daarom bij de claim om naar `answer`, zoals `push` dat al doet voor de push-rows; de
   sleutel `repair:<id>` onderscheidt de row. Geen migratie. Een test claimt een repair-resume via
   `runs.Take` op een echte database. *(eigen keuze na spec-review, r1-f1)*

Op de fix-pr-route heeft de hervatte run het commando `/lighthouse-fix-pr <pr>`. Elke
reparatieronde telt daar dus ook mee voor `maxFixPRRuns`. Dat geldt nu al voor een hervatting
na een antwoord, en dat blijft zo.

**Prompt.** Nieuwe heading `prompt_pusher_repair`, met daarachter de rode stap, de reden (zoals
`redLines`) en de URL van de Herstel-comment. De diff gaat niet mee: die staat in de comment en
in de worktree, en een grote diff kost context en kan de argv-grens van de resume raken. Inhoud
nl: *De workflow-pusher vond deze stap rood. Repareer het in de worktree, commit, en schrijf een
nieuwe handoff met `next: push-workflows`.* Plus en-variant. *(eigen keuze na spec-review,
r1-f6)*

Zo staan de rode stap en de reden in de opdracht (AC 1).

### 5. Sessie niet te hervatten

Een resume die niet start, sluit als `start_failed`. De bestaande `retry` doet een tweede poging
op dezelfde sleutel en geeft daarna het issue terug met `reason_start_failed`. Er is geen aparte
terugval naar een verse run.

### 6. Na de reparatie

De hervatte run schrijft een nieuwe Handoff `next: push-workflows`. De bestaande regel `handoff`
start een nieuwe push-row met een nieuwe sleutel `push:<id>`, en de pusher draait volledig, met
review (AC 2). `repeatsHandoff` vangt een push-handoff niet: die gebruikt de sleutel `push:<id>`,
niet `handoff:<id>`. Een woord-voor-woord herhaling stopt dus pas bij `maxRepairRuns`. Dat
volstaat: met N = 2 zijn dat hooguit twee overbodige rondes. *Keuze van de schrijver*:
`repeatsHandoff` blijft zoals hij is.

Schrijft de hervatte run geen nieuwe handoff, dan gelden de gewone uitkomstregels van een run
(Klaar, Vraag, Teruggegeven). Zo'n ronde telt niet mee voor `maxRepairRuns`: zonder handoff draait
er geen pusher, komt er geen nieuwe Herstel en dus geen lus. *(eigen keuze na spec-review,
r2-f1)*

### 7. Teller: rondes zonder groen

AC 3 zegt "N reparatierondes zonder groene `policy` of `lint`". De teller telt daarom alleen de
aaneengesloten Herstel-rondes van dit issue. Loop de push-rows van het issue (sleutel `push:…`,
geen merge-pusher) van nieuw naar oud. Sla mislukte starts en de open row over. Tel elke row die
als `handoff` sloot zonder `PushedSHA`: die eindigde in een Herstel. Stop bij de eerste row die
anders afliep (gepusht, `question`, `returned`). Stop ook bij een row die startte vóór de laatste
Teruggegeven van de token-login: na een hand-back begint de telling opnieuw.

Met N = 2: de eerste en de tweede Herstel hervatten de run, de derde aaneengesloten Herstel geeft
het issue terug (AC 3). Een groene doorgang (een push, of een Vraag van de review) of een
hand-back zet de teller terug. *(eigen keuze na spec-review, r1-f3)*

### 8. Loop-kaart en documentatie

- `docs/site/data/loop.json`, chip `push-workflows`: een `how`-regel over de reparatieronde
  (Herstel, hervatting van de run, ten hoogste twee rondes, daarna naar de gebruiker), en de route
  `workflow-push` noemt in zijn `note` dat een rode policy of lint eerst naar het werk gaat (AC 5).
  Een nieuwe handoff-lijn is aan de bouwer; voegt hij er een toe, dan ook in `loop.test.mjs`
  (`PUSHER` of `DISPATCHED`).
- `docs/tick.md`: de uitkomst van de pusher (Herstel naast Vraag en Teruggegeven) en de regel
  `repair` in de volgorde van `next`.
- `changelog.d/`: een fragment.
- Raakt de wijziging `pack/`, dan `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde
  kopie mee committen (CI eist `No changes.`).

## Tests

- **Pusher:** rood `policy` met overtredingen en rood `lint` met bevindingen geven een Herstel en
  een row `handoff`. Elke `COULD NOT CHECK`-plek geeft een Teruggegeven en een row `returned`. Een
  rood van een andere stap blijft zoals nu.
- **Decide:**
  - Herstel als laatste beurt, sessie gevonden: `KindResume` met sleutel `repair:<id>`, de sessie
    van de handoff en een prompt met stap en reden.
  - Sleutel al gebruikt: niets.
  - De derde aaneengesloten Herstel: Teruggegeven `reason_pusher_red_ceiling` met het
    push-commando.
  - Een gepushte push-row, een `question`-row of een Teruggegeven tussen de Herstels zet de
    teller terug.
  - Een Herstel van een andere auteur dan de token-login: `settle` sluit niets, `repair` doet
    niets.
  - Apply claimt een repair-resume met trigger `answer` in een echte database.
  - De gevonden run is de handoff-run, niet de push-row (die sluit ook als `handoff`), en geen
    run van een andere host.
  - Geen handoff-run vóór de Handoff-comment, of een zonder sessie: Teruggegeven
    `reason_pusher_red`.
  - `settle` sluit een open push-row op een Herstel als `handoff`.
  - Een Teruggegeven van de pusher geeft nog steeds `finish_return:reason_pusher_red`
    (`decide_test.go`).
- **Golden fixtures** in `internal/dispatch/testdata/decide/` waar nodig bijwerken.
- **Loop-kaart:** `node --test docs/site/test/`.

Verificatie: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.

## Aan de bouwer

- Hoe de pusher `COULD NOT CHECK` herkent (veld of functie), binnen de eis van punt 2.
- De precieze tekst van de headings `kind_repair`, `prompt_pusher_repair` en
  `reason_pusher_red_ceiling`, binnen de bedoeling hierboven.
- De vorm van de Herstel-body, zolang stap en reden erin staan.
- Of de loop-kaart een eigen handoff-lijn krijgt.
