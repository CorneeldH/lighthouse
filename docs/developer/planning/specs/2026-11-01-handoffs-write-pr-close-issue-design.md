# Handoffs bouw→write-pr en merge→close-issue, met de regel voor een handoff

Issue: #147. Breidt *Decide* regel 5 en *Handoffs* uit van de
[M2-spec](2026-10-27-m2-dispatcher-design.md) (#114). Zie ook #119 (fix-pr) en #142 (naamgeving).

## Waarom

De dispatcher start elke volgende stap van de keten in een verse sessie, mits er een handoff
staat. De eigenaar legde op 2026-10-31 vast waar een handoff hoort:

> Een handoff staat waar de volgende stap een schone context nodig heeft: de vorige stap maakte
> veel context op, of de volgende stap moet onafhankelijk oordelen. Ook staat er een handoff waar
> de keten wacht op een externe gebeurtenis, zoals rode CI of een merge.

Twee plekken voldoen aan die regel en hebben nog geen handoff:

- **Bouw klaar → write-pr.** De bouwsessie heeft veel context opgemaakt, en het toetsen van de
  acceptatiecriteria hoort onafhankelijk te zijn van wie bouwde. Nu draait de bouwsessie
  `/lighthouse-write-pr` zelf (`internal/dispatch/instructions/run.md`, "Before push + PR").
- **Merge → close-issue.** De keten wacht op de merge, een externe gebeurtenis. Na een `Refs`-PR
  start de tick al `/lighthouse-close-issue N` (regel 5, *Nacontrole*). Na een `Closes`-PR niet:
  het issue is bij de merge al gesloten en het board zet het op `Done`, waar decide niet kijkt.
  Dat was besluit r1-f10 van de M2-spec: ingang C van close-issue is aan de mens.

Naamgeving: het issue noemt `/lighthouse-verify-pr` en `/lighthouse-instrument`. Na #142 heten die
`/lighthouse-write-pr` en `/lighthouse-place-instruction`. De handoff-skill staat in
`pack/core/skills/write-handoff/SKILL.md`. Op de kaart heten de stappen `commit-test-gate`,
`write-pr`, `pr-and-ci` en `close-issue`.

## Doelen

- Een bouwsessie die klaar is, met de laatste commit groen, post `next: /lighthouse-write-pr <nr>`.
  Decide start dat in een verse sessie.
- Write-pr schrijft altijd `Refs #N`, nooit `Closes`: in een controllerrun en interactief.
- Na de merge van een PR die de controller opende, start de tick `/lighthouse-close-issue N`.
- Interactief eindigt write-pr met een handoffblok met `/lighthouse-close-issue <nr>`, voor na de
  merge.
- Een PR die een mens zelf met `Closes` opende, start na de merge niets (zoals nu).
- De regel voor een handoff staat in de handoff-skill.
- De kaart, de routeringstabellen van de handoff-skill en de M2-spec zeggen hetzelfde.

## Niet-doelen

- De handoff pr-and-ci→fix-pr. Die hoort bij #119. Die staat nog als `planned` op de kaart; dat
  rechtzetten valt buiten dit issue.
- Een nacontrole voor een `Closes`-PR. Gesloten issues op `Done` blijven buiten gather en decide.
- Interactieve sessies dwingen om een handoff te posten. De skill biedt hem aan; de mens kiest.
- `Closes` weghalen uit `pr-create-gate` of ingang C uit close-issue. Een mens kan nog een
  `Closes`-PR openen; gate en ingang C blijven daarvoor.

## Anti-doelen

- Een issue dat de controller nooit aanraakte, krijgt na een merge toch een run.
- De dispatcher gaat de kolom `Done` lezen, met de kosten en de achterstand die daarbij horen.

## Randvoorwaarden

- Het board zet een gesloten issue zelf op `Done` (bevestigd door de eigenaar).
- Gather houdt alleen items op `Controller` en `On hold` over (`dispatchStatuses` in
  `internal/dispatch/gather.go`).
- Regel 5 bestaat al voor een `Refs`-PR: gemerged, issue open en op `Controller`, een 🤖 Gestart
  vóór de merge, sleutel `closecheck:<pr>` nog niet besteed (`closeCheck` in
  `internal/dispatch/decide.go`). Close-issue draait dan B7.
- Lighthouse merget nooit (No-Go van M2).
- Dogfood: na de skillwijziging geeft `go run ./cmd/lighthouse init --upgrade` `No changes.`, met
  de geïnstalleerde kopie gecommit.

## Aanpakken

Voor bouw→write-pr is er één zinnige vorm: de bestaande `handoff`-regel van decide, zoals
brainstorming→review-spec. Een eigen trigger, zoals `fixpr` in #119, is niet nodig: de bouwsessie
weet zelf wanneer ze klaar is en kan het zeggen.

Voor merge→close-issue zijn vier aanpakken overwogen:

| | Aanpak | Afweging |
|---|---|---|
| A | Gather leest ook `Done`; decide zet een gesloten issue met een gemergde `Closes`-PR terug op `Controller`, start de nacontrole en zet het daarna terug op `Done`. | Werkt met `Closes`, maar gather moet de grootste kolom lezen, er is een grens tegen achterstand nodig, en een heropening in ingang C laat de run open. |
| B | Starten op `Done`, zonder de status te wijzigen. | Zelfde gather-probleem als A, en breekt r1-f15: een vraag van die run valt buiten het overzicht. |
| C | De board-automatisering uitzetten. | Valt buiten lighthouse. |
| **D** | Write-pr schrijft altijd `Refs #N`, nooit `Closes`. Het issue blijft na de merge open (en in een controllerrun op `Controller`), en regel 5 start de nacontrole zoals nu. Interactief geeft write-pr een handoffblok voor close-issue. | Geen Go-wijziging voor dit deel. Het issue sluit pas na de nacontrole, niet bij de merge, en GitHub legt geen sluitkoppeling tussen PR en issue. |

**Gekozen: D** (voorstel van de eigenaar, 2026-11-01; ook interactief, op zijn aanwijzing). Het eerste ontwerp koos A; de builder-check
liet zien dat gather `Done` niet leest, en D maakt die hele tak overbodig.

## Ontwerp

### 1. Bouw klaar → write-pr

**Run-instructies** (`internal/dispatch/instructions/run.md`):

- Een bouwsessie opent zelf geen PR meer. De regel hangt aan de gebeurtenis, niet aan het
  routenummer: elke run die de bouw afrondt of bij `finishing-a-development-branch` komt, ook een
  `/superpowers:subagent-driven-development`- of `executing-plans`-run die zijn route niet kent.
  *(eigen keuze na spec-review, r1-f8)*
- Is de bouw klaar en de laatste commit groen, dan pusht de bouwsessie de branch zelf
  (`git push -u origin HEAD`) en post daarna met `/lighthouse-write-handoff`:
  `next: /lighthouse-write-pr <nr>`. De handoff-skill pusht niet betrouwbaar: Workflow stap 3
  pusht alleen bij een spec- of plananker, en alleen als GitHub de commit van dat artefact nog niet
  kent. Write-pr pusht alleen een branch die al op origin staat. *(eigen keuze na spec-review,
  r1-f1)*
- "Groen" betekent: `commit-test-gate` meldde na de laatste commit geen rood. Is de laatste
  commit rood, dan herstelt de sessie dat eerst in een nieuwe commit (bestaande regel).
- Bij `finishing-a-development-branch`: niet push + PR, maar push en de handoff hierboven. Die regel
  gaat boven de keuzes van die skill, zoals "Never wait for CI" nu boven "stop at green" gaat.
- V2b van write-pr (roborev-review en het verwerken van de bevindingen) zegt "the session that did
  the work". Na de splitsing draait V2b in de bouwsessie, vóór de push en de handoff. De
  write-pr-run oordeelt alleen over de acceptatiecriteria en verandert geen code; een al
  vastgelegde review van `HEAD` herkent hij zoals nu bij een herstart (`classify`). *(eigen keuze na
  spec-review, r1-f9)*
- Een run die met `/lighthouse-write-pr <nr>` start, toetst de acceptatiecriteria, opent de PR met
  de body via `--body-file`, en post `next: /lighthouse-close-issue <nr>`. Daarna stopt hij.
- Opent die run geen PR (rode tests, `evidence.py` exit 3 of 4, een blok van `pr-create-gate`, een
  PR die al bestaat), dan post hij geen handoff naar close-issue. Hij stelt een vraag met
  `lighthouse ask` als de mens moet kiezen, of eindigt zonder handoff; decide geeft het issue dan
  terug met `reason_no_handoff`. *(eigen keuze na spec-review, r1-f11)*

**Judge** (`internal/dispatch/judge.go`):

- `/lighthouse-write-pr` komt op de allowlist.
- Het argument is het issuenummer, gelijk aan het issue van de handoff, zoals bij
  `/lighthouse-start-issue`.
- `installed` moet `lighthouse-write-pr` bevatten, anders `VerdictSkillMissing` (bestaand gedrag).

De start gaat via de bestaande `handoff`-regel en trigger `handoff`. Er is geen nieuwe trigger en
geen migratie nodig.

### 2. Merge → close-issue

**Write-pr** (`pack/core/skills/write-pr/SKILL.md`), overal:

- De koppeling naar het issue is altijd `Refs #N — <reden>`, ook als elk criterium
  `demonstrably done` is. De reden zegt dat `/lighthouse-close-issue` na de merge toetst en sluit.
  De uitkomst *Closes* en het mengen van `Closes #5` met `Refs #6` verdwijnen uit de skill.
- De body krijgt onder `Refs #N — <reden>` een `### Evidence #N`-tabel met één rij per criterium
  dat `demonstrably done` is, en daaronder per open criterium (`partly`, `not`, `after merge`) het
  verdict en wat nog nodig is, zoals de `Refs`-vorm nu. Een rij voor een open criterium bestaat
  niet: `evidence.py run --before-pr` faalt op een rij die niet houdt. Zijn alle criteria open,
  dan is er geen tabel. *(eigen keuze na spec-review, r1-f2)*
- `pr_body_gate.py` valideert de rijen die er onder `Refs` staan (vorm en allowlist, zoals nu
  onder `Closes`), maar telt ze niet per criterium: die telling blijft bij `Closes`. De checks op
  de tabel (`tests/pack/test_evidence.py`, `test_pr_body_gate.py`) gaan daarin mee.
  *(eigen keuze na spec-review, r1-f2)*
- Het verdict `after PR` verdwijnt uit write-pr. Het mat `closedByPullRequestsReferences`, en daar
  staat een `Refs`-PR nooit in. Een criterium dat alleen na de PR te meten is, wordt
  `after merge`. (r1-f3)
- `pr-create-gate` accepteert `Refs #N — <reden>` al. De `Closes`-vorm blijft geldig voor een
  mens die zelf een PR opent.
- De PR-template (`pack/core/templates/pull_request_template.md`, geïnstalleerd als
  `.github/pull_request_template.md`) zegt nu `Closes #123` als elk criterium bewezen is. Die
  regel gaat naar `Refs`, met de geïnstalleerde kopie via `init --upgrade`; anders schrijft een
  sessie die de template volgt alsnog `Closes`. (r1-f7)
- **Interactief**, na `gh pr create`: write-pr eindigt met `/lighthouse-write-handoff`, eerste stap
  `/lighthouse-close-issue <nr>`, uit te voeren na de merge. Dat is de bestaande rij "work done,
  issue can be closed" van Workflow stap 4. Close-issue kiest dan ingang B, want het issue is open.
- **Controllerrun**: geen eigen blok; de run post `next: /lighthouse-close-issue <nr>` zoals in
  deel 1. Een controllerrun herkent write-pr aan de bestaande signalen: de prompt eindigt op
  `(issue #<nr>, controller-run)` of `LIGHTHOUSE_CONTROLLER_RUN` staat in de omgeving.

**Close-issue** (`pack/core/skills/close-issue/claude/SKILL.md` en `agents/SKILL.md`):

- **Step 0** stopt nu alleen op een open `Closes`-PR (`closedByPullRequestsReferences`). Een open
  `Refs`-PR staat daar niet in, dus een direct geplakt handoffblok zou in ingang B het issue
  kunnen sluiten vóór de merge. Step 0 stopt daarom ook op een open PR die het issue noemt: head
  `issue-<nr>-…` of `Refs #<nr>` in de body, met dezelfde melding `waits for merge of #<pr>`.
  *(eigen keuze na spec-review, r1-f5)*
- **Ingang B, en daarmee B7**, zoekt de gemergde `Refs`-PR's van het issue (head `issue-<nr>-…`
  of `Refs #<nr>` in de body). Heeft zo'n PR een `### Evidence #<nr>`-tabel, dan draait de skill
  die met `evidence.py run` op een verse `origin/<default branch>`, zoals ingang C nu. Een rij die
  houdt, is `demonstrably done`; een rij die niet houdt, is `not`. Criteria zonder rij gaan door
  B1–B4 zoals nu. Ingang C blijft voor een gesloten issue. (r1-f3, keuze van de eigenaar
  2026-11-02)
- Hebben meerdere gemergde `Refs`-PR's een rij voor hetzelfde criterium (kolom `AC`), dan geldt
  per criterium alleen de rij van de laatst gemergde PR (`mergedAt`); oudere rijen voor dat
  criterium draait de skill niet. Zo maakt een rij die een latere PR verving, het issue niet
  blijvend `not`. *(eigen keuze na spec-review, r2-f1)*

**Decide.** Geen wijziging. Na de merge van de `Refs`-PR is het issue open en op `Controller`, en
staat er een 🤖 Gestart vóór de merge. `closeCheck` start `/lighthouse-close-issue N` met sleutel
`closecheck:<pr>`. Close-issue draait B7: het sluit met bewijs, of laat het issue open met wat
ontbreekt.

**Blockers.** `activeBlockers` laat een blocker vallen als die gesloten is of als een sluitende
PR ervan gemerged is (`MergedPR`, uit `closedByPullRequestsReferences`). Een `Refs`-PR telt daar
niet mee. Een issue dat op een controller-issue wacht, wacht dus tot B7 de blocker sluit, niet
meer tot de merge. Dat is bedoeld: B7 draait in de tick na de merge, en een blocker die B7 open
laat, is nog niet af. Geen Go-wijziging. (r1-f6, keuze van de eigenaar 2026-11-02)

**Een `Closes`-PR** van een mens op een controller-issue: het issue sluit bij de merge, het board
zet het op `Done`, decide ziet het niet meer. Er start niets, zoals nu. De bestaande fixture
`closecheck-closes-merged.json` (`"want": []`) pint dat al.

### 3. De regel in de handoff-skill

- De regel uit *Waarom* komt letterlijk in `pack/core/skills/write-handoff/SKILL.md`. De plek in
  de skill kiest `/lighthouse-place-instruction` tijdens de bouw. Verwachting: een korte sectie
  vóór Workflow stap 4, omdat stap 4 de tabel is die de regel toepast.
- **Workflow stap 4** krijgt een rij: "bouw klaar, laatste commit groen, nog geen PR" →
  `/lighthouse-write-pr <nr>`, kolom Controller `offer`.
- **Controller-modus stap 3** (`next:`-tabel) krijgt een rij: "bouw klaar, laatste commit groen"
  → `/lighthouse-write-pr <nr>`.
- De rij "PR open → `/lighthouse-close-issue <nr>`" zegt nu "the controller never starts this, it
  waits for green". Nieuwe tekst: de controller start het niet uit de handoff, maar na de merge
  van de `Refs`-PR als nacontrole.

### 4. Kaart en M2-spec

- `docs/site/data/chain.json`: `commit-test-gate`→`write-pr` en `pr-and-ci`→`close-issue` krijgen
  status `works`, zonder `issue`.
- `tests/test_site_chain.py`, `test_works_handoffs_follow_the_handoff_table`: de verwachte set
  krijgt die twee paren. De test eist al dat `` `/lighthouse-write-pr `` en
  `` `/lighthouse-close-issue `` in de handoff-skill staan.
- #147 heeft het label `chain`. Zonder `issue` op die twee handoffs staat #147 niet meer op de
  kaart, en `ci/chain_roadmap.py` meldt dan `#147: labelled chain, but not on the map` zolang #147
  open is. Onder D blijft #147 na de merge open tot close-issue. Het label `chain` gaat daarom van
  #147 af bij het openen van de PR. (r1-f10)
- M2-spec, *Decide* regel 5 *Nacontrole*: een controllerrun opent altijd een `Refs`-PR, dus regel
  5 dekt elke PR van de controller. De zin over `Closes` blijft, met als reden dat alleen een mens
  nog een `Closes`-PR opent. r1-f10 staat als *herzien in #147*, met de oude keuze leesbaar erbij.

## Bewijs

De acceptatiecriteria 2 en 3 van #147 spreken over een `Closes`-PR. Met aanpak D worden ze, na
akkoord van de eigenaar, in het issue aangepast:

| AC | Bewijs |
|---|---|
| 1 | Snapshot-fixture: een 🤖 Handoff met `next: /lighthouse-write-pr 7` op een issue in `Controller` geeft een start van `/lighthouse-write-pr 7`. Plus judge-tests voor de allowlist en het argument (ander nummer → afgewezen). Plus `instructions_test.go`: `run.md` bevat de push en de handoff naar `/lighthouse-write-pr <nr>` na de bouw, en niet meer `Before push + PR`. *(eigen keuze na spec-review, r1-f4)* |
| 2 (nieuw) | Write-pr schrijft altijd `Refs #N`, ook als alles `demonstrably done` is, en eindigt interactief met een handoff naar `/lighthouse-close-issue <nr>`: een test op de skilltekst, en een snapshot-fixture waarin een gemergde `Refs`-PR van een controller-issue `/lighthouse-close-issue 7` start (bestaande `closecheck-refs-merged.json` volstaat als die dit dekt). Plus tests op de close-issue-tekst: step 0 stopt op een open `Refs`-PR, en ingang B draait de Evidence-tabel van een gemergde `Refs`-PR, bij twee PR's met een rij voor hetzelfde criterium alleen die van de laatst gemergde. |
| 3 (nieuw) | Een gemergde `Closes`-PR start niets: `closecheck-closes-merged.json`. |
| 4 | De M2-spec, regel 5 en r1-f10. |
| 5 | De regel in de handoff-skill. |
| 6 | `chain.json`, beide tabelrijen, en `test_works_handoffs_follow_the_handoff_table`. |
| 7 | `go run ./cmd/lighthouse init --upgrade` geeft `No changes.` |

Elke test faalt als de bijbehorende wijziging weg is: zonder de allowlist-regel wijst judge de
handoff af (AC 1); zonder de controllerregel in write-pr verdwijnt de tekst waar de skilltest op
let (AC 2).

Dit verandert het gedrag van write-pr in elke repo die het pack installeert. Het
`changelog.d/`-fragment zegt dat.

## Aan de bouwer

- De precieze tekst in `run.md`, write-pr en de handoff-skill, binnen de regels hierboven. Taal:
  Engels (instructies), heading-sleutels via `.lighthouse/headings.toml`.
- Of de skilltest voor AC 2 in `tests/` (Python, zoals `test_site_chain.py`) of in
  `internal/packs/` (Go) hoort; volg waar de bestaande tests op skilltekst staan.
- Welke tests in `tests/pack/` (`test_write_pr_skill.py`, `test_evidence.py`,
  `test_pr_body_gate.py`, `test_pr_template.py`) een `Closes`-body als goede uitkomst van write-pr
  pinnen, en hoe ze omgaan naar `Refs`. Een test die de gate op een menselijke `Closes`-PR pint,
  blijft.
- Of `chain.json` elders zegt dat close-issue na een `Closes`-merge de rijen opnieuw meet (o.a. de
  write-pr-chip); die tekst gaat naar de nacontrole na een `Refs`-merge.
