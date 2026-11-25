# Versiewissel: afgeleide wissellijst en één voorstel per wissel

Issue: #380. Milestone: *Telemetrie: wat voegt elk onderdeel toe* (#12). Bouwt op
[zelflerende lighthouse](2026-11-16-zelflerende-lighthouse-design.md) (T7),
[explore/exploit](2026-11-24-issue-388-explore-exploit-design.md) (*Catalogus*, *Wanneer explore*)
en de inzet per run uit #373 en #375 (tabel `setups`, migratie 0013).

## Aanleiding

Na een nieuwe superpowers-versie, een ander model of een nieuwe harnessversie kan de waarde van een
onderdeel verschuiven. Lighthouse moet zo'n wissel zien en een meting voorstellen. Starten blijft een
menselijke stap: de spec van #388 maakt van een versiewissel een *trigger*, die een vergelijking als
issue voorstelt.

## Doelen

- Uit de inzet per run of sessie volgt een lijst wissels per dimensie (superpowers, model, harness),
  met oude en nieuwe waarde.
- Bij een wissel die een onderdeel uit de catalogus raakt, schrijft lighthouse precies één
  voorstel-issue, met de reden. Een volgende run met dezelfde versie stelt niets nieuws voor.

## Niet-doelen

- **De markering tekenen.** Dat doet #379 op zijn tijdlijn
  (spec #379, `2026-11-24-issue-379-telemetrie-ui-design.md`, nog op branch
  `issue-379-telemetrie-ui`; §6: het veld `markers`, leeg "tot
  #380 het vult", getekend als verticale lijn). #380 levert de wissels in die vorm (zie *Aansluiting
  op #379*). Daarmee wacht #380 niet meer op #379.
- **De meting zelf.** Protocol, budget en uitvoering horen bij #388 en de milestone *Explore/exploit*.
- **Versies installeren of bijwerken.** Lighthouse installeert of wijzigt zelf geen versie (no-go
  van de milestone; #325 levert pins en de achterstand-lane, die hier buiten blijven).

## Anti-doelen

- Een stroom voorstellen bij de eerste keer draaien, één per wissel uit de geschiedenis.
- Een voorstel per repo voor één hostbrede wissel.
- Ruis uit interactieve sessies waarin de mens zelf `/model` kiest.

## Randvoorwaarden

- Geen tweede opslag (no-go): de wissels worden afgeleid uit `setups`, niet opgeslagen. `setups`
  wordt bovendien ter plekke bijgewerkt (`internal/telemetry/record.go`, `RecordSetup`), dus een
  opgeslagen wissel kon achteraf onjuist blijken.
- Codex, roborev en kenn-forge zijn geen voorwaarde.
- Een fout in deze stap houdt de tick niet tegen.

## Aanpakken

1. **Afleiden bij het lezen, voorstel vanuit de tick (gekozen).** Een pure leesfunctie levert de
   wissels; een tickstap na `collectRuns` schrijft het voorstel. Geen migratie, geen nieuwe opslag,
   en #379 leest dezelfde functie.
2. **Wissels opslaan bij het verzamelen (verworpen).** Een tabel `version_changes` die de collector
   vult. Tweede opslag van wat al in `setups` staat, en onbetrouwbaar omdat `setups` ter plekke
   verandert.
3. **Losse opdracht plus geplande job (verworpen).** `lighthouse telemetry changes` met een job in
   `internal/scheduled`. Meer oppervlak (allow-list, `Register`, packcontract-tests) voor wat de
   tick elk uur al kan.

## Ontwerp

### De wissellijst

Een leesfunctie in `internal/telemetry`, bijvoorbeeld `VersionChanges(ctx, db)`, die per dimensie de
wissels teruggeeft. Een wissel draagt: dimensie, oude waarde, nieuwe waarde, en de eerste run of
sessie met de nieuwe waarde (id en tijd).

**Een wissel is de eerste keer dat een waarde opduikt.** Loop de rijen van een reeks af op tijd; een
waarde die in die reeks nog niet eerder voorkwam, is een wissel. *Oud* is de laatst geziene bekende
waarde vlak daarvoor; de allereerste waarde van een reeks is geen wissel. Zo geven bronnen die naast
elkaar draaien met verschillende versies (desktop, server, andere nodes) geen heen-en-weer:
A→B→A→B levert één wissel, A→B. Een terugval naar een oude versie is daarmee geen wissel.

| Dimensie | Kolom in `setups` | Reeks | Over welke rijen |
|---|---|---|---|
| `superpowers` | `superpowers_version` | één reeks | runs en sessies |
| `model` | `model` | één reeks per `setups.lane` (`sdd`, `native`; een rij zonder lane is een eigen reeks) | alleen dispatcher-runs (`run_id` gezet) |
| `harness` | `harness_version` | één reeks per `harness` | runs en sessies |

- **Volgorde.** Op `started_at` van de run of sessie zelf, niet op `setups.recorded_at`: dat moment
  schuift mee wanneer de collector een rij bijwerkt. `started_at` heeft secondeprecisie; bij gelijke
  tijd beslist `setups.id` oplopend, zodat *oud* en nieuw deterministisch zijn
  *(eigen keuze na spec-review, r1-f5)*.
- **Onbekend telt niet.** Een `NULL` wordt overgeslagen. `superpowers_version` is alleen bekend als
  in die sessie een superpowers-skill laadde (`internal/telemetry/transcript/setup.go`), dus gaten
  zijn normaal.
- **Hostbreed.** De lijst loopt over alle repo's in `lighthouse.db` samen: een superpowers-update
  raakt paia en lighthouse tegelijk.
- **Alleen indeling `standard`.** Een run met een andere `runs.assignment` (`shadow:*`,
  `variant:*=*`) telt niet mee, en een sessie met een andere `sessions.assignment` evenmin
  (`internal/store/migrations/0010_telemetry.sql:24-25`; nu zet geen code hem anders dan `standard`):
  een vergelijking van #388 draait juist met opzet op een andere keuze, en zou anders zelf een nieuw
  voorstel uitlokken.
- **Model alleen uit dispatcher-runs.** In een interactieve sessie kiest de mens het model; dat zou
  wissels geven die niets met de stand te maken hebben.
- **Model per lane.** De dispatcher start een Native-run met `--model sonnet`, een SDD-run zonder
  `--model` (`internal/workplace/carrier.go:112-114`). Eén reeks over beide lanes zou de laneskeuze
  als modelwissel zien en de *oud*-waarde van een echte update verkeerd noemen; vandaar een reeks
  per `setups.lane`.
- **Harness per harness.** Versienummers van verschillende harnesses (na #389 ook Codex en OpenCode)
  zijn onvergelijkbaar; een andere `harness`-naam zelf is geen wissel.
- **Een nooit eerder geziene oudere versie.** Een node met een verouderde installatie die voor het
  eerst rijen levert, geeft `nieuw→ouder`. Die wissel staat wel in de lijst (en als markering),
  maar krijgt geen voorstel wanneer de nieuwe waarde voor `superpowers` of `harness` als semver lager
  is dan de hoogste eerder geziene waarde in die reeks; een waarde die niet als semver te lezen is,
  telt als gewone wissel, en `model` kent geen rangorde *(eigen keuze na spec-review, r1-f4)*.

### Aansluiting op #379

De tijdlijn van #379 heeft per ISO-week (maandag, UTC) een lijst `markers` met week, soort en tekst
(spec #379, §6). #380 levert daarvoor een functie naast `VersionChanges`, bijvoorbeeld
`VersionMarkers`, die elke wissel omzet naar:

| Veld | Waarde |
|---|---|
| week | de ISO-week van de eerste run of sessie met de nieuwe waarde |
| soort | de dimensie: `superpowers`, `model` of `harness` |
| tekst | oud en nieuw, bijvoorbeeld `superpowers 7.0.1→7.0.2`; bij harness met de harnessnaam erbij, bij model met de lane erbij |

- **Leesbaar via elke readonly-verbinding**, ook de live-variant van #379 (`store.OpenReadOnlyLive`,
  spec #379 §2; `OpenReadOnly` is daar juist afgewezen vanwege `immutable=1`): beide functies doen
  alleen `SELECT`.
- **Ongemigreerde database.** Ontbreekt `setups`, dan geeft de functie een herkenbare fout, zodat het
  endpoint van #379 een dekkingsmelding toont in plaats van een 500 (spec #379 §2).
- **Wie koppelt.** Het vullen van `markers` in de tijdlijnreader is één aanroep. Wie als laatste
  merget, #379 of #380, legt die aanroep en de test dat een wissel als markering in de juiste week
  verschijnt *(eigen keuze: geen van beide wacht op de ander)*.
- **Repo-filter.** De tijdlijn van #379 filtert op `repo`; een wissel is hostbreed en verschijnt in
  elke repo-weergave.

### Koppeling aan onderdelen

Een vaste tabel in code, voor nu grof:

| Dimensie | Onderdelen (namen uit de catalogus van #388) |
|---|---|
| `superpowers` | `task_review`, `final_review` (de SDD-skills komen uit superpowers) |
| `model` | `model` |
| `harness` | `harness` |

Een wissel waarvan de dimensie geen onderdeel heeft, staat wel in de lijst (en dus op de tijdlijn van
#379), maar krijgt geen voorstel. Een fijnere koppeling per skill (per-skill hashes in `setups`) is
een later issue.

### Het voorstel

Een stap in de tick, direct na `collectRuns` (`internal/cli/tick.go`), zodat hij de setups van deze
tick al ziet.

- **Hooguit eens per uur.** De tick loopt elke 3 minuten en `FindByMarker` bladert door alle issues
  en PR's van de repo. De stap draait daarom op een eigen uurpoort, met een tijdsbestand naast
  `last-collect` (bijvoorbeeld `<state>/telemetry/last-version-check`). Dat bestand houdt alleen een
  tijdstip bij, geen gegevens, en is dus geen tweede opslag.
- **Alleen de laatste wissel per reeks** krijgt een voorstel. Oudere wissels uit de geschiedenis
  zijn alleen een markering. Zo vuurt de eerste keer draaien geen stroom af.
- **Waar.** In één repo uit `host.toml`: `[telemetry] proposals_repo` (een `owner/naam`). Dat moet
  een van de repo's zijn die de dispatcher bedient: de stap hergebruikt de forge en de
  `[project] language` die de tick voor die repo al heeft geladen. Ontbreekt de sleutel, of is de
  repo geen dispatch-repo, dan schrijft de stap niets en logt één regel met de reden. Logisch is de
  lighthouse-repo: daar staan de catalogus en de vergelijkingen van #388.
- **Precies één.** Een verborgen marker in de body, naar het patroon van de pin-lane
  (`internal/pins/lane/body.go`):

  ```text
  <!-- lighthouse-version-change: superpowers →7.0.2 -->
  <!-- lighthouse-version-change: harness codex →0.6 -->
  <!-- lighthouse-version-change: model native →claude-sonnet-5-5 -->
  ```

  De marker draagt de dimensie, bij harness de harnessnaam, bij model de lane, en de *nieuwe*
  waarde, maar niet de oude: *oud* is afgeleid en kan verschuiven wanneer een rij laat binnenkomt of
  ter plekke wordt bijgewerkt, of wanneer twee dispatcherhosts elk een andere reeks zien
  *(eigen keuze na spec-review, r1-f2)*. Oud→nieuw staat alleen in titel, tekst en markering. De
  stap zoekt met `forge.FindByMarker` (open én gesloten) altijd naar de volledige comment tot en
  met ` -->`: `FindByMarker` is een kale substring-match (`internal/forge/write.go:215`), en zonder
  afsluiter vindt `→7.0.2` ook `→7.0.20`. Bestaat er al een item met die marker, dan schrijft hij
  niets. Een gesloten voorstel komt dus niet terug.
- **Inhoud** (in `[project] language` van de doelrepo, `nl` of `en`; de repoconfig valt terug op `en`): de wissel met oude en nieuwe
  waarde, de eerste run of sessie met de nieuwe waarde, de geraakte onderdelen, de reden ("een
  versiewissel is een trigger volgens #388") en een verwijzing naar het protocol per vergelijking in
  de spec van #388. Lighthouse start de meting niet.
- **Fouten.** Een forge-fout of een onleesbare `setups` geeft één logregel met reden; de tick gaat
  door en meldt de stap als mislukt, zoals `collectRuns` dat doet. Een ontbrekende `proposals_repo`
  is geen mislukking: één logregel, de stap telt als geslaagd. Het tijdsbestand wordt na elke poging
  geschreven, ook na een mislukte, zodat een forge-storing niet elke 3 minuten alle issues laat
  doorbladeren *(eigen keuze na spec-review, r1-f8)*.

## Wat het bewijst (AC)

| AC | Bewijs |
|---|---|
| Een versiewissel verschijnt als markering, met oude en nieuwe versie | `VersionMarkers` geeft de wissel met week, soort en oud→nieuw; #379 tekent hem in `markers` |
| Precies één voorstel, met reden; een tweede run met dezelfde versie stelt niets nieuws voor | test: twee stappen, de tweede vindt de marker en schrijft niets; een derde run op dezelfde versie levert geen nieuwe wissel |
| Lighthouse installeert of wijzigt zelf geen versie | de stap leest `setups` en schrijft alleen een issue |
| Twee runs op verschillende superpowers-versies tonen één markering | test op `VersionChanges` met twee runs: precies één wissel; daarnaast A, B, A, B: nog steeds één |
| Een laat binnenkomende rij levert geen tweede voorstel | test: marker `superpowers →B` bestaat; een rij met eerdere `started_at` en waarde X komt binnen; de stap schrijft niets |

## Mijn keuzes (corrigeerbaar)

- Volgorde op `started_at` van run of sessie, niet op `recorded_at`.
- De sleutelnaam `[telemetry] proposals_repo`, beperkt tot een dispatch-repo.
- Alleen de laatste wissel per reeks krijgt een voorstel.
- Alleen runs met indeling `standard` tellen mee.
- De stap draait op een eigen uurpoort met een tijdsbestand.
- Wie van #379 en #380 als laatste merget, koppelt `VersionMarkers` aan de tijdlijn.
- Model: een reeks per lane; marker op dimensie en nieuwe waarde, zonder oude.
- Een wissel verschijnt in elke repo-weergave van de tijdlijn.

## Aan de bouwer

- Naam en signatuur van de leesfunctie en van de tickstap.
- Exacte tekst van het voorstel-issue en de titel, binnen de inhoud hierboven.
- Testopzet, naar het voorbeeld van `internal/pins/lane/*_test.go` (een nep-forge) en
  `internal/telemetry/record_test.go` (een echte `lighthouse.db` in een tijdelijke map).
