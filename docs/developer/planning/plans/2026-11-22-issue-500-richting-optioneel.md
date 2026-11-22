# Richtingsdocumenten optioneel voor elke repo: implementatieplan

> **Voor uitvoerders:** verplichte sub-skill: gebruik `superpowers:subagent-driven-development` (aanbevolen) of `superpowers:executing-plans` om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** elke repo met lighthouse kan de richtingsdocumenten van #372 (strategie, uitgangspunten, een bestand per area) kiezen via `lighthouse init`/`--upgrade`, en wie ze niet kiest merkt niets.

**Architectuur:** de koppen die code en skills lezen worden `heading:<key>`-sleutels in `pack/core/headings.toml`; `internal/direction` leest ze daar (elke taalvariant) en wordt opt-out als geen enkele `paths.*`-sleutel gezet is; een nieuwe opdracht `lighthouse direction check` draait de vormcheck in elke repo. `init` zaait sjablonen uit `pack/core/templates/` alleen als het bestand ontbreekt, naar het voorbeeld van `Pack.LabelsTemplate`, buiten de beheerde eenheden.

**Techniek:** Go (cobra, `internal/packs`, `internal/initcmd`, `internal/direction`, `internal/config`), TOML, skills in markdown, Node-test voor `docs/site`.

**Spec:** [`2026-11-22-issue-372-richting-per-repo-design.md`](../specs/2026-11-22-issue-372-richting-per-repo-design.md). Dit plan werkt de punten uit die de spec naar #500 verwees: koppen als `heading:<key>` (§1 *Koppen*), sjablonen voor andere repo's (besluit 6, §7). Issue: #500, milestone *Product: eerste versie voor anderen*.

## Beslissingen die dit plan vastlegt

- **Opt-in = een sleutel of de vlag.** Een repo wil de documenten als `lighthouse init --direction` is meegegeven, of als `lighthouse.toml` onder `[paths]` al minstens één van `strategy`, `tenets`, `areas` zet. Anders doet `init` niets en zegt het niets. Dit maakt `init --upgrade` het pad uit het AC: een repo die de sleutels zet, krijgt bij `--upgrade` de ontbrekende bestanden.
- **Standaardpaden** voor `--direction` zonder gezette sleutels: `docs/developer/strategie.md`, `docs/developer/uitgangspunten.md`, `docs/developer/areas` (de paden van lighthouse zelf; `[paths]` krijgt ze via de bestaande `AppendMissing`-route).
- **Zaaien nooit overschrijven:** bestaat het bestand, dan `direction: <pad> exists; left as it is` op stderr (zoals `labels:`). De bestanden staan niet in het manifest.
- **Areas:** één bestand per `labels.area_prefix`-label uit de taxonomie (`internal/labels.ParseTaxonomy`), uit één sjabloon `area.md`. Zonder taxonomie of zonder area-labels worden er geen area-bestanden gezaaid, met één regel uitleg bij `--direction`.
- **Taal:** een sjabloon heeft markers `{{heading:<key>}}`; `init` vervangt ze door de variant van `[project] language` (val terug op `en`). Richtlijnen in het sjabloon staan als HTML-commentaar in het Engels.
- **De vormcheck** (`internal/direction.Check`) leest koppen via de headings-tabel, accepteert elke taalvariant, en geeft `nil` als géén van de drie sleutels gezet is. Is er minstens één gezet, dan is een ontbrekende of onleesbare andere een fout (uitgangspunt C, zoals nu).

## Globale randvoorwaarden

- Artefacten (specs, plannen, commits, issuetekst) Nederlands; skills, hookmeldingen en `docs/*.md` voor gebruikers Engels (zoals `docs/getting-started.md`).
- Niets in `pack/core` kent een lighthouse-specifiek pad of een areanaam (spec, anti-doelen).
- Nooit `AGENTS.md` of `CLAUDE.md` van een projectrepo schrijven (milestone, no-go).
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command` (`lighthouse.toml:47`).
- Onder `pack/` of een `internal/`-wijziging die verandert wat `init` schrijft: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie meecommitten; CI eist `No changes.`
- Eén fragment in `changelog.d/` per type (`docs/releasing.md`); zie Taak 6.
- Atomic commits, Conventional Commits, eindigend op de attributieregel van de sessie.

## Review Focus

Invoer- of faalklassen die de spec stil laat en een gebruiker raakt, het waarschijnlijkst eerst. Elke regel heeft een test in de genoemde taak.

1. Repo zonder `[paths]`-sleutels en zonder `--direction`: `init` en `init --upgrade` schrijven geen bestand en melden niets (Taak 3).
2. Sleutel gezet, bestand al door de gebruiker aangepast: `--upgrade` laat het byte-voor-byte staan en maakt geen conflict (Taak 3).
3. `--direction` zonder taxonomie of zonder `2-area:`-labels: geen areabestanden, geen fout, wel één uitlegregel (Taak 3).
4. Een repo in het Engels: gezaaide koppen zijn Engels en de check accepteert ze; een Nederlandse repo ziet Nederlandse koppen (Taak 1 en 3).
5. Een repo met alleen `paths.areas` gezet (geen strategie): de check meldt `paths.strategy is niet gezet` niet als opt-out, maar de lezers (`write-issue`, `map-planned`) blijven stil voor wat ontbreekt (Taak 2 en 4).

---

## Bestandsstructuur

| Bestand | Verantwoordelijkheid |
|---|---|
| `pack/core/headings.toml` | nieuwe sleutels voor de koppen van strategie, uitgangspunten en areas |
| `internal/config/headings.go` | leest de tabel ook vanuit een gegeven repo-root |
| `internal/direction/direction.go` | vormcheck, taalonafhankelijk en opt-out |
| `internal/cli/direction.go` (nieuw) | `lighthouse direction check` |
| `pack/core/templates/{strategie,uitgangspunten,area}.md` (nieuw) | sjablonen met `{{heading:<key>}}`-markers |
| `internal/packs/packs.go` | `Pack.DirectionTemplates` naast `LabelsTemplate` |
| `internal/initcmd/{initcmd,direction}.go` | opt-in, zaaien, `--direction`-vlag |
| `internal/cli/init.go` | vlag `--direction` |
| `pack/core/skills/{write-issue,close-issue,map-planned}` | koppen als `heading:<key>`, stil bij ontbrekende sleutel (bestaat al; pinnen) |
| `docs/richting.md` (nieuw), `docs/getting-started.md`, `docs/site/data/config.json` | gebruikersdocs |

---

### Taak 1: Koppen als `heading:<key>` en een taalonafhankelijke vormcheck

**Bestanden:**
- Wijzig: `pack/core/headings.toml`, `internal/config/headings.go`, `internal/direction/direction.go:30-31,244` en wat daar de Nederlandse literals leest
- Test: `internal/direction/direction_test.go`, `internal/config/headings_parity_test.go`, `internal/packs/core_test.go`
- Dogfood: `.lighthouse/headings.toml` via `init --upgrade`

**Interfaces:**
- Produceert: sleutels in `headings.toml`, elk met `nl` en `en`: `area_crux` (Crux/Crux), `policy` (Leidend beleid/Guiding policy), `not_now` (Bewust niet/Deliberately not), `strategy_diagnosis` (Diagnose/Diagnosis), `strategy_audience` (Voor wie/For whom), `strategy_roadmap` (Roadmap/Roadmap), `tenet_over` (boven/over), `tenet_evidence` (Bewijs/Evidence). `area_near_goal` bestaat al.
- Produceert: `func config.LoadHeadingsFile(path string) (*HeadingsTable, error)` (exporteer de tabel; `loadHeadings` wordt een dunne wrapper rond `HeadingsPath()` + deze functie, `LIGHTHOUSE_HEADINGS` blijft gelden voor de bestaande functies) en `func config.ParseHeadings(data []byte) (*HeadingsTable, error)` voor de ingebedde packtabel. `(*HeadingsTable).Variants(key) ([]string, error)` (alle varianten, `nl` eerst) en `(*HeadingsTable).Heading(key, language string) (string, error)` (taalspecifiek, terugval `en`). Geen `HeadingVariantsAt`: `Check` laadt de tabel één keer uit `<root>/.lighthouse/headings.toml`; `init` parst de tabel uit de pack (Taak 3).
- Produceert: `func direction.Check(root string) []string` houdt zijn handtekening.

- [ ] **Stap 1: schrijf de falende tests** in `direction_test.go`: `TestCheckAcceptsEnglishHeadings` (fixture met `lighthouse.toml` `[project] language = "en"` en een `.lighthouse/headings.toml`; een area met `## Crux`, `## Guiding policy`, `## Deliberately not` en een tenet `## A. X over Y` met de regel `*Evidence*: [a](a.md), [b](a.md)` (bestaande linkdoelen; de tweelinksregel blijft) geeft `len(Check) == 0`). `TestCheckAcceptsEveryVariantInOneRepo` bevat ook een Nederlandse titel met het gewone woord ` over ` (`## A. Praten over de klant boven aannames`) die geldig blijft: de titel moet precies één voorkomen van één variant van `tenet_over` bevatten; bij meer dan één voorkomen van de varianten samen kiest de check de variant van `[project] language`. De helper `fixture()` kopieert `pack/core/headings.toml` naar `.lighthouse/headings.toml`, zodat de bestaande tests groen blijven. en `TestCheckAcceptsEveryVariantInOneRepo` (Nederlandse en Engelse koppen door elkaar in één repo geven geen fout; lezen accepteert elke variant). Bestaande tests behouden de Nederlandse fixtures.
- [ ] **Stap 2: draai** `go test ./internal/direction/...` — verwacht FAIL (Engelse koppen onbekend).
- [ ] **Stap 3: voeg de sleutels toe** aan `pack/core/headings.toml` met de waarden hierboven en implementeer `config.LoadHeadingsFile`, `config.ParseHeadings` en de methoden van `HeadingsTable` in `internal/config/headings.go`.
- [ ] **Stap 4: vervang in `internal/direction`** `areaHeadings`, `strategyHeadings`, de kop `Bewust niet`, het scheidingswoord ` boven ` en `Bewijs` door opzoekingen in de tabel van `config.LoadHeadingsFile`; `Check` laadt de tabel één keer. Een ontbrekende of kapotte tabel is een bevinding `.lighthouse/headings.toml: kan niet laden: …`, nooit stil groen. Een kop is gelijk aan één van de varianten.
- [ ] **Stap 5: voeg pariteits- en kerntests toe** voor de nieuwe sleutels (patroon van `headings_parity_test.go`), draai `go run ./cmd/lighthouse init --upgrade`, en draai `go test ./internal/direction/... ./internal/config/... ./internal/packs/... ./internal/cli/...` — verwacht PASS, inclusief `TestDirectionDocumentsHaveTheirShape` op de eigen documenten.
- [ ] **Stap 6: commit** `feat(richting): koppen als heading-sleutels, vormcheck in elke taal (#500)` met `pack/core/headings.toml`, `.lighthouse/headings.toml` en de Go-bestanden.

### Taak 2: De vormcheck is opt-out en draait in elke repo

**Bestanden:**
- Wijzig: `internal/direction/direction.go` (rond `read(...)` en de area-tak, regels 125-170)
- Maak: `internal/cli/direction.go`; registreer in `internal/cli/root.go` (bij `AddCommand(newCheckInstructionsCommand())`)
- Test: `internal/direction/direction_test.go`, `internal/cli/direction_test.go`

**Interfaces:**
- Consumeert: `direction.Check(root string) []string` (Taak 1).
- Produceert: `lighthouse direction check` — leest de repo-root zoals `check-instructions` (`config.RepoRoot()`), drukt elke bevinding als `pad:regel: melding` en eindigt met exit 0 (geen bevinding of opt-out), 1 (minstens één bevinding), 2 (`COULD NOT CHECK`: geen repo-root of niet te parsen `lighthouse.toml`). Bij opt-out drukt hij niets. Daarvoor krijgt `direction` een eigen minimale lezer `readPaths(root)` (alleen TOML-decode van `[paths]` en `[labels]`, geen `[forge]`-eis); `repoconfig.Load` wordt niet meer gebruikt, zodat een repo zonder herkende remote (geen `forge.repo`) ook stil blijft. `Check(root) []string` houdt zijn handtekening; de CLI roept eerst `direction.CheckConfig(root) error` aan (parse-fout → exit 2) en dan `Check`.

- [ ] **Stap 1: schrijf de falende tests.** `TestCheckSkipsRepoWithoutDirectionKeys`: fixture waarin `strategy`, `tenets` en `areas` alle drie ontbreken geeft `Check(...) == nil`. `TestCheckStillFailsOnPartialKeys`: alleen `paths.areas` gezet geeft precies de fouten voor `paths.strategy` en `paths.tenets` (de bestaande zinnen `… is niet gezet`). `TestDirectionCheckCommand` in `internal/cli`: exit 0 met stilte op een repo zonder sleutels, exit 1 met `pad:regel:` op een kapotte areakop, exit 2 buiten een repo en exit 2 met een syntactisch kapotte `lighthouse.toml`; `TestCheckSkipsRepoWithoutForgeRepo`: een repo zonder `[forge] repo` en zonder richtingssleutels geeft `nil`.
- [ ] **Stap 2: draai** `go test ./internal/direction/... ./internal/cli/ -run 'Direction|Skips|Partial'` — verwacht FAIL.
- [ ] **Stap 3: implementeer** de opt-out aan het begin van `Check` (alle drie paden leeg → `return nil`), `CheckConfig`, en `newDirectionCommand()` met subopdracht `check`. **Geen taxonomie is geen fout:** is `paths.labels` of `labels.area_prefix` niet gezet en staan er geen `.md`-bestanden in de areamap, dan slaat `Check` de arealabel-vergelijking stil over (geen `paths.labels is niet gezet`); staan er wel area-bestanden, dan blijft die eis zoals nu. Een gezette `paths.areas` waarvan de map ontbreekt blijft een fout (spec, uitgangspunt C); `init --direction` zorgt dat de map bestaat (Taak 3). Test: `TestCheckAcceptsEmptyAreasWithoutTaxonomy`.
- [ ] **Stap 4: draai dezelfde tests plus** `TestDirectionDocumentsHaveTheirShape` — verwacht PASS.
- [ ] **Stap 5: commit** `feat(richting): lighthouse direction check, stil zonder sleutels (#500)`.

### Taak 3: Sjablonen en zaaien via init

**Bestanden:**
- Maak: `pack/core/templates/{strategie,uitgangspunten,area}.md`, `internal/initcmd/direction.go`
- Wijzig: `internal/packs/packs.go:109-118,328-335` (`DirectionTemplates map[string][]byte`, sleutels `strategie.md`, `uitgangspunten.md`, `area.md`; ook uit de lijst `templates/` toegestaan), `internal/initcmd/initcmd.go` (`Options.Direction bool`; roep het zaaien aan naast het zaaien van `labels.md`, `buildPlan` rond regel 481), `internal/cli/init.go:81` (`--direction`)
- Test: `internal/initcmd/direction_test.go` (nieuw), `internal/packs/packs_test.go`, `internal/initcmd/testdata/places.golden`

**Interfaces:**
- Consumeert: `config.ParseHeadings` en `(*HeadingsTable).Heading` (Taak 1), toegepast op de ingebedde `headings.toml` van de pack (de inhoud die dezelfde run installeert), nooit op de schijf: een eerste `init --direction` of een `--upgrade` vanaf een oudere versie heeft de nieuwe sleutels daar nog niet; `internal/labels.ParseTaxonomy` en `labels.area_prefix`; `projectconfig` voor het bijwerken van `[paths]`.
- Produceert: `func directionPlan(root string, o Options, tmpl map[string][]byte, load func(string) (*planned, error)) error` — zet `p.new, p.mode = …, 0o644` voor elk ontbrekend bestand; bestaat het, dan stderr `direction: <pad> exists; left as it is` en geen wijziging. `wantsDirection(root string, o Options) bool` = `o.Direction` of minstens één van `paths.{strategy,tenets,areas}` gezet in `lighthouse.toml`.
- **Veilige paden:** `paths.strategy`, `paths.tenets`, `paths.areas` en elke afgeleide areabestandsnaam (label zonder prefix + `.md`) gaan door dezelfde controle als `labelsTarget` (schoon, relatief, geen `..`, binnen de repo); een afwijking is een fout vóór er iets geschreven wordt. Hergebruik die controle als gedeelde helper.
- **Sleutels in `[paths]`:** `--direction` zet de drie sleutels via `configTables` (zoals `LabelTables`), vóór `planConfig`, niet in `directionPlan`. Zonder taxonomie maakt `directionPlan` de areamap met een `.gitkeep` (zodat `direction check` groen blijft) en schrijft de uitlegregel.
- Sjabloon: koppen als `## {{heading:policy}}` enz. Markers worden vervangen door de variant van `[project] language` (val terug op `en`); het sjabloon `area.md` krijgt voor elk area-label `<paths.areas>/<label zonder prefix>.md`. De `[paths]`-sleutels komen via `AppendMissing` (nooit overschrijven).

- [ ] **Stap 1: schrijf de falende tests** in `internal/initcmd/direction_test.go`, elk met een tijdelijke git-repo:
  - `TestInitWithoutDirectionWantsNothing`: `init` en `init --upgrade` op een repo zonder sleutels en zonder vlag: geen bestand onder `docs/developer/`, geen regel met `direction` op stdout of stderr, `lighthouse.toml` zonder `strategy`/`tenets`/`areas`. **Dit is het bewijs van AC 1.**
  - `TestInitDirectionSeedsMissingTemplates`: met `--direction` en een taxonomie met `2-area:core` en `2-area:init` bestaan `strategie.md`, `uitgangspunten.md`, `areas/core.md`, `areas/init.md`, de koppen zijn Nederlands bij `language = "nl"` en Engels bij `en`, `lighthouse.toml` bevat daarna `strategy`, `tenets` en `areas` onder `[paths]`, en `direction.Check(root)` geeft `nil` (de test controleert eerst de drie sleutels, want zonder sleutels is `Check` een opt-out; een tweede run met één kapotte gezaaide kop geeft een bevinding). Een `--upgrade` vanaf een oudere `.lighthouse/headings.toml` zaait ook goed.
  - `TestUpgradeSeedsFilesForKeysAlreadySet`: sleutels gezet, bestanden weg: `--upgrade` zaait ze; sleutels gezet, bestanden aanwezig: ongewijzigd.
  - `TestUpgradeNeverOverwritesLocalEdits`: een door de gebruiker aangepast `strategie.md` blijft byte-voor-byte, de uitvoer bevat `exists; left as it is`, en het manifest noemt het bestand niet.
  - `TestDirectionWithoutTaxonomySeedsNoAreas`: geen `2-area:`-labels: geen area-bestanden, een areamap met alleen `.gitkeep`, exit 0, één uitlegregel, en `direction.Check(root)` is daarna `nil`. `TestDirectionRejectsPathsOutsideRepo`: `paths.strategy = "../x.md"` of een label `area:../y` schrijft niets buiten de repo en geeft een fout.
- [ ] **Stap 2: draai** `go test ./internal/initcmd/ -run Direction` — verwacht FAIL.
- [ ] **Stap 3: schrijf de drie sjablonen** met de koppen en de ID-structuur van de spec (§1): strategie `Diagnose`, `Voor wie`, `Leidend beleid`, `Bewust niet` met `**S1**`-voorbeeld, `Roadmap`; uitgangspunten met voorrangsregel en het voorbeeld uitsluitend binnen een omheind codeblok (```), dat `readDoc` wist: `## A. <X> {{heading:tenet_over}} <Y>` plus `*{{heading:tenet_evidence}}*: [..](..), [..](..)`; een vers gezaaid bestand heeft dus geen actieve tenet en slaagt voor `Check`. Richtlijnen als HTML-commentaar bevatten nooit een regel die met `## ` of `- ` begint; area met `Crux`, `Leidend beleid`, `Bewust niet` (`**N1**`). Geen lighthouse-specifieke tekst; richtlijnen als HTML-commentaar.
- [ ] **Stap 4: implementeer** `DirectionTemplates` in `packs.go` (toegestane namen, `TestLoadKeepsDirectionTemplatesOutOfUnits` naar het voorbeeld van `TestLoadKeepsLabelsTemplateOutOfUnits`), `wantsDirection`, `directionPlan` en de vlag.
- [ ] **Stap 5: werk** `places.golden` en `packs`-tests bij, draai `go test -race ./internal/initcmd/... ./internal/packs/... ./internal/direction/... ./internal/cli/...` — verwacht PASS; draai `go run ./cmd/lighthouse init --upgrade` en controleer dat de eigen repo geen sjabloon zaait (haar bestanden bestaan) en de rest `No changes.` blijft.
- [ ] **Stap 6: commit** `feat(init): richtingsdocumenten zaaien via init, alleen als ze ontbreken (#500)`.

### Taak 4: Lezers stil, koppen via sleutels in de skills

**Bestanden:**
- Wijzig: `pack/core/skills/write-issue/{claude,agents}/SKILL.md` (rond regels 266, 394-397), `pack/core/skills/map-planned/SKILL.md`, `pack/core/skills/close-issue/{claude,agents}/SKILL.md` (stap 5)
- Test: `internal/packcontract/writeissue_test.go:363-374` (behouden strings), `internal/packcontract/mapplanned_test.go`, `internal/packcontract/closeissue_test.go`, `internal/scheduled/mapplanned/job_test.go:185-211`
- Dogfood: `.claude/skills/…`, `.agents/skills/…` via `init --upgrade`

**Interfaces:**
- Consumeert: de sleutels uit Taak 1. Skills noemen een kop als `heading:area_crux`, `heading:policy`, `heading:not_now`, `heading:strategy_diagnosis` en schrijven wat `lighthouse headings <key>` geeft; lezen accepteert elke variant.

- [ ] **Stap 1: schrijf de falende contracttests**: `TestWriteIssueNamesDirectionHeadingKeys` en `TestMapPlannedNamesDirectionHeadingKeys` eisen de literale tekst `heading:policy` en `heading:not_now` in de skill en geen losse `Leidend beleid`/`Bewust niet` meer als leescontract; `TestSkillsStaySilentWithoutDirectionKeys` eist in `write-issue` (3f en de pitch) en `map-planned` (stap 4) de zin dat een niet-gezette sleutel stil wordt overgeslagen. De bestaande pins (`lighthouse config paths.strategy`, `paths.tenets`, `paths.areas`, `diagnosis`, `deliberate no`) blijven.
- [ ] **Stap 2: draai** `go test ./internal/packcontract/...` — verwacht FAIL voor de `heading:policy`/`heading:not_now`-pins; `TestSkillsStaySilentWithoutDirectionKeys` en de afwezigheidsassertie pinnen wat al zo is (de skills lezen al op betekenis en bevatten de stille-overslag-zin) en slagen vooraf.
- [ ] **Stap 3: pas de skilltekst aan**: voeg `heading:<key>` toe naast de bestaande Engelse leesinstructies (waar een Nederlandse kop als literal staat, vervang die), laat de stille-overslag-zin staan of voeg hem toe, wijzig niets aan gedrag. Geen lighthouse-pad of areanaam.
- [ ] **Stap 4: draai** `go run ./cmd/lighthouse init --upgrade`, daarna `go test ./internal/packcontract/... ./internal/scheduled/mapplanned/...` — verwacht PASS; `lighthouse check-instructions` — geen FAIL.
- [ ] **Stap 5: commit** `docs(skills): richtingskoppen als heading-sleutels, lezers stil zonder sleutel (#500)`.

### Taak 5: Gebruikersdocs

**Bestanden:**
- Maak: `docs/richting.md` (Engels)
- Wijzig: `docs/getting-started.md` (verwijzing naast het stuk over `--labels`, regel ~119), `docs/site/data/config.json` (`about` van `paths.strategy`, `paths.tenets`, `paths.areas`; `readers` volgens `docs/site/test/config.test.mjs`)
- Test: `docs/site/test/config.test.mjs`

- [ ] **Stap 1: schrijf `docs/richting.md`** met: wat de drie documenten zijn en dat ze zonder status zijn; dat ze optioneel zijn en dat een repo zonder ze niets merkt; hoe je ze aanzet (`lighthouse init --direction`, of de drie `[paths]`-sleutels zetten en `--upgrade` draaien); dat `init` alleen zaait wat ontbreekt en nooit overschrijft; hoe je ze vult (crux, beleid, *Bewust niet* met ID's `S`/`N`, uitgangspunten met voorrang en bewijs); welke skills ze lezen (`write-issue` stap 3f en de milestonepitch, `close-issue` stap 5, de job `map-planned`); `lighthouse direction check` en wat de koppen in jouw taal zijn (`lighthouse headings <key>`). Alleen relatieve links naar bestaande doelen.
- [ ] **Stap 2: werk** `config.json` en `getting-started.md` bij.
- [ ] **Stap 3: draai** `node --test "docs/site/test/*.test.mjs"` en `.claude/hooks/lighthouse/run.sh md-link-check` via de Write-hook — verwacht PASS, geen blokkade.
- [ ] **Stap 4: commit** `docs(richting): wat de richtingsdocumenten zijn en hoe je ze aanzet (#500)`.

### Taak 6: Afronding

**Bestanden:**
- Maak: `changelog.d/issue-500-richting-optioneel.md` (type `added`: `init --direction`, sjablonen, `lighthouse direction check`)
- Controleer: dogfood en gate

- [ ] **Stap 1: schrijf het fragment** in de vorm van `changelog.d/issue-372-richting-per-repo.md`.
- [ ] **Stap 2: draai** `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`), `lighthouse imports check` en de volledige `[test_gate] command` — verwacht PASS.
- [ ] **Stap 3: controleer elk AC** van #500 tegen de tests (AC 1: `TestInitWithoutDirectionWantsNothing`, `TestCheckSkipsRepoWithoutDirectionKeys`; AC 2: `TestUpgradeSeedsFilesForKeysAlreadySet`, `TestUpgradeNeverOverwritesLocalEdits`; AC 3: Taak 1 en 4; AC 4: `TestDirectionCheckCommand`; AC 5: `docs/richting.md`).
- [ ] **Stap 4: commit** `chore(richting): changelog-fragment voor #500`.

## Zelfreview

- **Spec/AC-dekking:** AC 1 Taak 2+3+4; AC 2 Taak 3; AC 3 Taak 1+4; AC 4 Taak 2; AC 5 Taak 5. Geen AC zonder taak.
- **Typeconsistentie:** `LoadHeadingsFile`, `ParseHeadings`, `Check`, `DirectionTemplates`, `wantsDirection`, `directionPlan` en `Options.Direction` hebben in elke taak dezelfde naam.
- **Onzeker, aan de uitvoerder:** `labelsTarget(root)` (hoe `init` de taxonomie vindt) niet gelezen; `directionPlan` gebruikt dezelfde bron voor de area-labels. Zoek of `lighthouse headings` een taal zonder `nl`-variant aankan voordat de terugval op `en` wordt vastgelegd.
