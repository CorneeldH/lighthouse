# Releasebeleid: release per milestone, na een periode op dev — ontwerp

Issue: #582. Milestone: Product: eerste versie voor anderen. Grootte: project.

## Waarom

Lighthouse releaset nu zichzelf zodra er iets te releasen is: een merge met een `fixed`-fragment
releaset meteen, en elke maandag gaat de rest mee ([releasing.md](../../../releasing.md#when-a-release-happens)).
Zolang alleen de maintainer lighthouse gebruikt, is dat prima. Met echte gebruikers is het te
snel: een gebruiker krijgt halve milestones en wijzigingen die nog nooit in de praktijk gedraaid
hebben.

Het nieuwe beleid: een release volgt op een afgeronde milestone, en draait daarvoor een tijd als
dev-versie op de eigen repo's (lighthouse zelf en personal_ai_agent). Een kritieke fix kan los.

## Doelen, niet-doelen, anti-doelen

- **Doel:** een gebruiker op het releasekanaal krijgt alleen afgeronde milestones die minstens 7
  dagen op dev gedraaid hebben, plus losse kritieke fixes. Wat op dev draait, is `main`; dat bevat
  de rc (zie *Soak*).
- **Doel:** de eigen repo's draaien elke wijziging snel in de praktijk, via een devkanaal.
- **Doel:** een release wordt niet vergeten: de workflow opent zelf een PR die de maintainer
  alleen hoeft te mergen.
- **Niet-doel:** een signaal "het gaat goed" uit telemetrie of uit open issues. Het oordeel is
  de merge van de release-PR door de maintainer.
- **Niet-doel:** het releasemodel van personal_ai_agent (#347, een trein). Dit ontwerp gaat alleen
  over lighthouse. Dat personal_ai_agent het devkanaal volgt, is een instelling daar.
- **Anti-doel:** een release die werk meeneemt dat ná de afsplitsing op `main` landde. Ook een
  kritieke fix mag dat niet. Werk van andere, nog open milestones dat vóór de afsplitsing op
  `main` stond, gaat wél mee: dat heeft op dev gedraaid (besloten met de gebruiker, zie
  *Welke milestone*).
- **Anti-doel:** een gebruiker die zonder eigen keuze een prerelease krijgt.

## Randvoorwaarden

- Alles landt op `main` via een PR (AGENTS.md). De enige directe push blijft die van de bot bij
  een release ([releasing.md](../../../releasing.md#the-release-commit)).
- Een tag maakt altijd een workflow, nooit een mens.
- `build.yml` bouwt alleen een tag met de `dist`-commit erbij; `prerelease.yml` en `release.yml`
  maken die commit al (C2). Dat mechanisme blijft.
- Een PR die een workflow opent met `GITHUB_TOKEN` start geen andere workflows. De release-PR moet
  CI (en de macOS-gate) wel draaien, dus die PR opent de workflow met de GitHub App uit `[ci]` in
  `lighthouse.toml` (`bot_client_id_var`, `bot_private_key_secret`).
- `GET /releases/latest` slaat prereleases over; `go install …@latest` kiest ook geen prerelease.
  Het releasekanaal als standaard komt daarmee vanzelf.

## Aanpakken

Drie keuzes, elk in het gesprek gemaakt.

**Route voor een kritieke fix.**

| Aanpak | Afweging | Oordeel |
|---|---|---|
| A. Releasebranch vanaf de laatste release, fix erop cherry-picken, patch | alleen de fix gaat mee; `CHANGELOG.md` en fragmenten leven op twee lijnen | **gekozen** |
| B. Een fix releaset heel `main` na een korte dev-periode | simpel; neemt een half milestone mee | afgevallen (anti-doel) |
| C. Geen losse fix; wie haast heeft, gaat naar dev | simpelst; geen fix-route | afgevallen |

**Wanneer een dev-build ontstaat.**

| Aanpak | Afweging | Oordeel |
|---|---|---|
| D1. Na elke merge naar `main` met een releasebaar fragment | snelle praktijktest; veel prereleases en builds | **gekozen** |
| D2. Pas bij het sluiten van de milestone | weinig builds; half werk draait nooit in de praktijk | afgevallen |
| D3. Eén keer per dag als `main` veranderd is | tussenvorm | afgevallen |

**Welke milestone een release start.** De repo heeft veel milestones tegelijk open, als thema's
die parallel lopen; `main` bevat bij elke afsplitsing ook werk van open milestones.

| Aanpak | Afweging | Oordeel |
|---|---|---|
| Elke gesloten milestone splitst af | geen markering; elke thematische close wordt een release | afgevallen |
| Alleen het werk van de milestone, gecherry-pickt vanaf de vorige release | strikt; elke release een cherry-pick-klus met conflicten, en de combinatie draaide nooit op dev | afgevallen ("voor nu") |
| Alleen een gemarkeerde milestone splitst af; heel `main` gaat mee | simpel, past bij één `main`; werk van open milestones gaat mee, wel getest op dev | **gekozen** |

**Wanneer een rc een release wordt.**

| Aanpak | Afweging | Oordeel |
|---|---|---|
| R1. Automatisch na 7 dagen zonder nieuwe rc | geen handwerk; "goed" = "niemand vond iets" | afgevallen |
| R2. Met de hand (`workflow_dispatch`), de workflow weigert vóór 7 dagen | oordeel van de maintainer; kan vergeten worden | afgevallen |
| R3. Na 7 dagen opent de workflow een release-PR; de merge is de release | oordeel van de maintainer, en niet te vergeten | **gekozen** (voorstel van de gebruiker) |

## Ontwerp

### Kanalen en versies

| Kanaal | Tags | Wie |
|---|---|---|
| `release` (standaard) | `vX.Y.Z` | iedereen |
| `dev` | `vX.Y.Z-dev.N`, `vX.Y.Z-rc.N` en `vX.Y.Z` | de eigen repo's, wie wil meetesten |

- `X.Y.Z` in een dev-tag is de versie die de volgende milestone-release krijgt: de minor na de
  hoogste `release/vX.Y`-branch, of na de laatste release als er geen open releasebranch is.
  Voorbeeld: na `v0.27.0` zijn de dev-builds `v0.28.0-dev.1`, `-dev.2`, …; na de afsplitsing van
  `release/v0.28` worden ze `v0.29.0-dev.N`.
- `N` is één hoger dan de hoogste bestaande `N` voor die `X.Y.Z` en dat achtervoegsel. Alle
  dev-builds delen één concurrency-groep, en `N` wordt binnen die groep berekend: twee merges kort
  na elkaar geven geen botsende tag. *(eigen keuze na spec-review, r1-f9)*
- `-dev` en `-rc` zijn van de workflows. Een testbuild met de hand via `prerelease.yml` krijgt
  `-test.N` (`vX.Y.Z-test.N`), zodat hij de rc-nummering nooit raakt. *(eigen keuze na
  spec-review, r1-f13)*
- Semver-volgorde maakt het devkanaal vanzelf goed: `v0.28.0-dev.5 < v0.28.0-rc.1 < v0.28.0 <
  v0.29.0-dev.1`.
- Een milestone-release is een minor (onder 0.x, ook bij een brekende wijziging). Een major kiest
  de maintainer met de hand bij de afsplitsing (zie Afsplitsen).

### Stroom

```text
main ──merge──merge──merge──(milestone dicht)──merge──merge──…
        │      │      │            │
     dev.1  dev.2  dev.3           └─ release/v0.28 ── rc.1 ──(7 dagen)── release-PR ──merge── v0.28.0
                                                         │
                                       fix: PR op main, cherry-pick-PR naar release/v0.28 ── rc.2 (klok opnieuw)
```

1. **Dev-build (D1).** Een push naar `main` waarvan de commit minstens één fragment van een
   releasebaar type toevoegt (`added`, `changed`, `fixed`, `removed`), maakt een dev-tag via het mechanisme van `prerelease.yml` (UI-build, `dist`-commit,
   alleen de tag pushen, `build.yml` met `--prerelease`). Een merge met alleen `internal` maakt
   geen dev-build.
2. **Afsplitsen.** Een `milestone: closed`-event van een *releasemilestone* start de
   afsplitsing: `release/vX.Y` vanaf de `main`-commit van dat moment, en tag `vX.Y.0-rc.1`. Een
   releasemilestone heeft in zijn beschrijving een regel `Release: ja`; de sluiting van een
   andere milestone doet niets. Geen releasebaar fragment open dat nog niet in een
   releasebranch zit: geen afsplitsing, de run meldt dat. Een `workflow_dispatch` doet hetzelfde zonder milestone (een
   vergeten of milestoneloze release), met een keuze `minor`/`major`.
3. **Soak.** De eigen repo's draaien op dev. Het devkanaal kiest de hoogste versie, dus na de
   eerste merge na de afsplitsing draait daar `vX.Y+1.0-dev.N`, niet de rc zelf. Dat is bedoeld:
   `main` bevat de rc, dus wie `main` soakt, soakt de rc mee. Wat de soak niet ziet, is een fout
   die alleen in de rc bestaat omdat `main` hem al met ander werk verhielp; die kans is klein,
   en de soak meet dus `main`, niet de rc als losse build. *(r1-f6)* Een bug wordt gefixt via een PR op
   `main` (met `fixed`-fragment), daarna een cherry-pick-PR naar `release/vX.Y`. De merge daarvan
   tagt `rc.N+1`; de klok begint opnieuw.
4. **Release-PR (R3).** Een dagelijkse cron kijkt per open `release/vX.Y`: is de nieuwste rc 7
   dagen oud en staat er geen release-PR open, dan opent de workflow een PR op `release/vX.Y` met
   de nieuwe sectie in `CHANGELOG.md` en de verwijdering van de vrijgegeven fragmenten,
   `chore(release): vX.Y.0`. De workflow geeft de versie expliciet aan `ci/changelog` mee (uit de
   branchnaam, of de volgende patch op de lijn van de branch); `ci/changelog` leidt hem niet meer
   af uit de fragmenten of de hoogste tag. *(eigen keuze na spec-review, r1-f8)* Een nieuwe rc
   terwijl de PR open staat: de workflow sluit de PR. Sluit de maintainer de PR zelf zonder
   merge, dan opent de cron geen nieuwe tot er een nieuwe rc is. Fragmenten waarvan de
   bestandsnaam al in een gereleasede sectie van `CHANGELOG.md` staat, neemt de release-PR niet
   mee, los van de volgorde van de merges. *(eigen keuze na spec-review, r1-f10, r1-f12)*
5. **Release.** De merge van de release-PR start de release: macOS-gate, `dist`-commit (C2), tag
   `vX.Y.0`, `build.yml`. De workflow weigert de tag als de ouder van de releasecommit niet de
   commit van de nieuwste rc is: dan is er na de soak nog iets gemergd. *(r1-f12)*
6. **Sync naar `main`.** Een eigen stap, los van de tag, die je opnieuw kunt draaien. Hij leidt
   zijn werk af uit de tag: de sectie uit `CHANGELOG.md` op de tag, en de fragmentnamen die de
   releasecommit verwijderde. Hij zet die sectie op `main` en verwijdert daar de fragmenten met
   dezelfde naam. Is `main` al bij (sectie staat er, fragmenten weg), dan doet hij niets.
   Fragmenten van werk voor de volgende milestone blijven staan. Faalt de push, dan draai je
   alleen deze stap opnieuw; de tag blijft. *(r1-f2)*

**Herstel na een gemergde release-PR zonder tag** (macOS rood, de job stierf): een
`workflow_dispatch` "release" met als invoer de branch draait stap 5 en 6 opnieuw op de tip van
die branch. De cron slaat een branch over waarvan de tip een `chore(release)`-commit zonder tag
is; hij opent daar geen tweede release-PR. Een afsplitsing per dispatch is een andere invoer en
raakt zo'n branch niet. *(r1-f1)*

### Waar de logica draait

Alle releaselogica draait uit de workflows en de `ci/changelog`-code van `main`, ook voor wat op
een releasebranch gebeurt (rc na een cherry-pick, release na de release-PR, sync). Een event op
`release/*` mag de workflow van die branch niet gebruiken: die is bevroren bij de afsplitsing,
en een fix van de releaselogica zou dan een cherry-pick en een nieuwe rc vragen. De bouwer kiest
het mechanisme (bijvoorbeeld `workflow_run` of een cron/dispatch op `main` met de branch als
invoer), zolang de checkout van de logica `main` is en die van de inhoud de releasebranch.
*(r1-f7)*

### Kritieke fix (route A)

- De fix landt eerst via een PR op `main`, met een `fixed`-fragment. Zo raakt hij niet kwijt bij
  de volgende milestone-release.
- Een cherry-pick-PR naar de releasebranch van de laatste release (`release/vX.Y`) neemt de
  commit over, fragment inbegrepen.
- Na de merge op een al gereleasede branch opent de workflow meteen een patch-release-PR, zonder
  7 dagen wachten (`chore(release): vX.Y.Z+1`). Kritiek betekent: kan niet wachten.
- Na die release verwijdert de bot het fragment op `main` en zet de patch-sectie in `main`'s
  `CHANGELOG.md`. De volgende milestone-release noemt de fix dus niet nog eens.
- Staat er op dat moment ook een rc in soak voor de volgende minor, dan krijgt die de fix via
  dezelfde weg (cherry-pick-PR naar die branch).

### `self-update` en het kanaal

- `lighthouse self-update --channel release|dev`, standaard `release`.
- `release`: zoals nu, `GET /releases/latest`.
- `dev`: de lijst van releases (`GET /releases`, zoals `lighthouse releases` al doet), en daaruit
  de hoogste semver, prereleases inbegrepen.
- `lighthouse version compare <a> <b>` (nieuw) vergelijkt twee versies volgens semver en geeft
  `-1`, `0` of `1`; `self-update` en `/lighthouse-upgrade` gebruiken dezelfde vergelijking.
- De parser accepteert `vX.Y.Z` met een optioneel `-dev.N` of `-rc.N`, en vergelijkt volgens
  semver. Een binary op `v0.29.0-dev.3` is geen lokale `dev`-build: `version.String()` geeft de
  tag, en `self-update` slaat hem niet over.
- Geen downgrade: wie van `dev` naar `release` wisselt, blijft op zijn hogere dev-versie tot de
  release hem inhaalt.
- `deploy/systemd/lighthouse-upgrade.service` geeft het kanaal door in `ExecStart`, standaard
  `release`; de installatiegids beschrijft een drop-in voor `dev`.

### Wat verdwijnt

- De release op een `fixed`-merge en de maandag-cron in `release.yml`, en de event-tak `push` en
  `schedule` in `decideBump` (`ci/changelog/changelog.go`).
- De pins daarop in `internal/repopins/release_test.go`.
- De tekst in [releasing.md](../../../releasing.md) over "When a release happens" en de
  maandag-cron in de macOS-gate.

### macOS-gate

Een PR naar `release/*` draait `macos.yml`, zodat de maintainer het resultaat ziet vóór de merge.
De harde gate zit in de workflow na de merge: geen tag zonder een groene `macos`-run op de
gemergde commit. Rood: geen tag, de branch blijft staan; herstel met *Re-run failed jobs*, of met
de dispatch "release" (zie *Herstel na een gemergde release-PR zonder tag*). Dev- en rc-tags wachten niet op macOS, net als de huidige prereleases.

### Gevolg voor repo's op dev

Een repo op het devkanaal (personal_ai_agent) krijgt bij elke dev-build een andere binary.

- De versiemelding van de hooks ("run lighthouse init --upgrade") blijft zoals hij is: een repo
  op dev ziet hem vaker; dat is geaccepteerd (besloten met de gebruiker).
- `/lighthouse-upgrade` vergelijkt versies nu met `sort -V` en laat prereleases weg
  (`pack/core/skills/upgrade/claude/SKILL.md:36`, `:45-46`). `sort -V` zet `0.29.0` vóór
  `0.29.0-rc.2`; een repo op `v0.29.0-rc.2` met een binary op `v0.29.0` krijgt dan "the binary is
  older than the repo" en upgradet niet. De skill gaat over op `lighthouse version compare`, en
  kiest de nieuwste versie op het kanaal van de binary. Dat is een wijziging onder `pack/`: dogfood
  met `init --upgrade` (besloten met de gebruiker na de spec-review, r1-f3).
- Dev-releases worden nooit opgeruimd. Een repo zet zijn binary-versie in
  `.lighthouse/manifest.toml`, en `unblock-dependents.yml` downloadt precies die tag
  (`pack/core/workflows/unblock-dependents.yml:103`); een opgeruimde dev-tag breekt die workflow.
  *(r1-f4)*

### Documentatie

- `docs/releasing.md` beschrijft kanalen, de stroom, de kritieke fix, de release-PR en het
  herstel van elke stap.
- `docs/install.md`: het kanaal kiezen, ook voor de timer.
- `AGENTS.md`, regel **Releases**, blijft kloppen ("de release-workflow tagt").

## Keuzes van de ontwerper

Niet met de gebruiker besproken; te corrigeren bij het lezen.

- Een afsplitsing terwijl de vorige rc nog niet gereleased is (twee milestones kort na elkaar):
  de workflow merget `main` in die `release/vX.Y` en tagt `rc.N+1`, met dezelfde versie. Geen
  tweede releasebranch, geen overgeslagen versie, geen force-push. Alleen als `main` releasebare
  fragmenten heeft die nog niet op die branch staan; anders niets. Geeft de merge een conflict,
  dan faalt de run met een melding en merget de maintainer `main` via een PR naar de branch.
  *(eigen keuze na spec-review, r1-f11)*
- Een cherry-pick naar een releasebranch gaat via een PR, met de hand gemaakt. Geen automatische
  backport op een label.
- De soakduur van 7 dagen staat als constante in de workflow, niet in `lighthouse.toml`.
- De macOS-gate is een stap in de workflow, geen ruleset: de repo heeft geen rulesets, en een
  ruleset op `release/*` vraagt handwerk van een admin plus een bypass voor de bot.
- De markering van een releasemilestone is de regel `Release: ja` in de beschrijving, geen label
  (milestones hebben geen labels) en geen naamregel.
- Een releasebranch is open zolang er geen `vX.Y.0`-tag voor bestaat; de cron kijkt alleen
  daarnaar en naar gereleasede branches met een nieuwe cherry-pick.
- De release-PR is vrijgesteld van de CI-eis van een nieuw fragment.
- Een fragment dat al met een patch uit is, neemt een latere release-PR niet mee (stap 4).

## Open voor het plan

- Twee plannen: eerst `self-update --channel`, de semver-parser, `lighthouse version compare` en
  `/lighthouse-upgrade`; daarna de workflows. Het eerste hangt alleen af van de vorm van de tags.
  *(r1-f14)*

## Aan de bouwer

- De overgang van de huidige stroom: wat er met de open fragmenten op `main` gebeurt op het
  moment dat de nieuwe workflows actief worden.
- De verdeling over workflows: `release.yml` uitbreiden of splitsen (dev-build, afsplitsen,
  release-PR, release na merge). `prerelease.yml` hergebruiken als `workflow_call` ligt voor de
  hand.
- Hoe de cron de leeftijd van de nieuwste rc bepaalt (tagdatum of releasedatum).
- De exacte tekst van de release-PR-body.
- Hoe `ci/changelog` de releasebranch en `main` elk hun deel geeft (sectie schrijven, fragmenten
  op naam verwijderen).
