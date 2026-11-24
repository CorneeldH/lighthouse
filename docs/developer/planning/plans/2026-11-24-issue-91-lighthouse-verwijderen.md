# Lighthouse schoon verwijderen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse uninstall` draait terug wat `lighthouse init` schreef, toont eerst per bestand de diff en verwijdert pas na akkoord (issue #91).

**Architecture:** `init` en `--upgrade` leggen in een nieuw bestand `.lighthouse/created.toml` vast wat ze buiten de units aanmaakten. `initcmd.Uninstall` hergebruikt `reconcile` (met een lege gewenste stand), de conflictpoort, de diff, de vraag en `apply`; daarvoor valt `execute` uiteen in gedeelde stappen. Uninstall plant per pad een `planned` en past toe met manifest en `created.toml` als laatste twee paden.

**Tech Stack:** Go, cobra, BurntSushi/toml, `internal/orderedjson`.

**Spec:** [2026-11-24-issue-91-lighthouse-verwijderen-design.md](../specs/2026-11-24-issue-91-lighthouse-verwijderen-design.md)

## Global Constraints

- Code, namen, commentaar en meldingen van het commando in het Engels (`code_language = "en"`); commits, docs en dit plan in het Nederlands; het changelog-fragment in het Engels (`docs/releasing.md`).
- Nieuw bestand: `.lighthouse/created.toml`, nooit een nieuwe sectie in het manifest (`manifest.Read` weigert onbekende sleutels; een oudere binary leest het manifest in hooks).
- Sleutels van `created.toml` letterlijk: `files`, `gitattributes`, `settings_keys`, `settings_language`; kop `# Managed by lighthouse init. Do not edit by hand.`
- `created.toml` groeit alleen: geen run haalt er iets uit; een run die niets aanmaakt schrijft het niet.
- Conflictregel is die van `init --upgrade` (`reconcile`): gelijk aan de manifest-hash is veilig, anders gewijzigd. Vastgelegde dingen buiten de units vallen niet onder de conflictpoort (uitzondering `lighthouse.toml`: altijd weg als in `files`).
- Een geweigerde of afgewezen run schrijft niets. Uninstall commit niet.
- Uninstall raadt niet op patronen: alleen wat in manifest of `created.toml` staat. Enige uitzondering: `settings.Apply` haalt elke hookregistratie weg die naar `.claude/hooks/lighthouse/` wijst.
- Een permissieregel valt alleen weg als `packs.IsLighthouseRule` hem herkent.
- Bestaat `.claude/settings.json` niet, dan raakt uninstall het niet aan en maakt het niet aan.
- Vraag letterlijk `Remove lighthouse? [y/N] `; geweigerd: `refused: N locally modified unit(s)`; meldingen achteraf op stderr, één regel per geval: `kept: <pad> (project file after seeding)`, `kept: <pad> (runtime state, not installed by init)`, `left: <pad> — init may have written it; remove it yourself if so`, `not touched: ...`.
- Laatste twee paden bij toepassen: `.lighthouse/manifest.toml`, dan `.lighthouse/created.toml`, niet in gesorteerde volgorde.
- Lege mappen opruimen van elk verwijderd bestand omhoog tot de repo-root; stop bij een map met inhoud (`__pycache__` telt als leeg). `removeEmptyParents` (upgrade) blijft ongewijzigd.
- Fixtures eindigen op precies één `\n`; `settings.json`-fixtures in de canonieke `orderedjson`-opmaak.
- Dogfood: raakt dit wat `init` schrijft, dan `go run ./cmd/lighthouse init --upgrade` en de installatie committen; verwacht `No changes.`. Elke PR een fragment in `changelog.d/`.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Nooit `--no-verify`.

## Review Focus

- Eigen `.gitattributes` zonder slotnewline of met een regel die op een lighthouse-regel lijkt maar niet vastgelegd is: blijft staan, byte-voor-byte afgezien van de aanvaarde slotnewline (Taak 5).
- `CLAUDE.md` die de gebruiker na `init` uitbreidde, of `"language"` met een andere waarde: blijft staan en wordt gemeld (Taak 5).
- Kapotte of niet-object `.claude/settings.json`, of een kapot `created.toml`: duidelijke fout, niets geschreven (Taak 4 en 5).
- Schrijffout halverwege `apply`: herhaalde uninstall ziet manifest en `created.toml` nog en maakt af (Taak 4).
- `.claude/hooks/lighthouse/` met een `__pycache__` of een eigen bestand erin: pycache alleen: map weg; eigen bestand ernaast: map blijft, pycache ook (Taak 4).

---

### Task 1: Pakket `internal/created`

**Files:**
- Create: `internal/created/created.go`, `internal/created/created_test.go`

**Interfaces:**
- Produces:
  ```go
  const Path = ".lighthouse/created.toml"
  type Created struct {
      Files            []string `toml:"files"`
      Gitattributes    []string `toml:"gitattributes"`
      SettingsKeys     []string `toml:"settings_keys"`
      SettingsLanguage string   `toml:"settings_language"`
  }
  func Read(root string) (c Created, found bool, err error) // fs.ErrNotExist -> found=false, nil; onbekende of kapotte TOML -> error met pad
  func Encode(c Created) []byte                              // kop + alleen niet-lege sleutels, lijsten in invoegvolgorde
  func (c Created) Merge(add Created) Created                // alleen groeien, geen dubbele items; SettingsLanguage = add's als die niet leeg is
  func (c Created) Empty() bool                              // geen enkel veld gevuld
  ```

- [ ] **Step 1: Schrijf de falende tests** in `created_test.go`: `TestMergeOnlyGrows` (lijst `a,b` + `b,c` geeft `a,b,c`; lege `add` laat `c` gelijk; `SettingsLanguage` van `add` wint, een lege laat de oude staan), `TestEncodeReadRoundTrip` (Encode → schrijf naar `t.TempDir()/.lighthouse/created.toml` → `Read` geeft gelijk struct; uitvoer begint met de kop en bevat geen lege sleutels), `TestReadMissing` (`found == false`, `err == nil`), `TestReadCorrupt` (geen TOML geeft een error die `.lighthouse/created.toml` noemt).
- [ ] **Step 2:** Run `go test ./internal/created/ -race`; verwacht FAIL (package ontbreekt).
- [ ] **Step 3: Implementeer** de vier functies uit Interfaces; `Encode` met `toml.NewEncoder` per sleutel of handmatig gebouwd, zolang de uitvoer de spec-vorm heeft.
- [ ] **Step 4:** Zelfde run; verwacht PASS.
- [ ] **Step 5: Commit** `feat(created): .lighthouse/created.toml lezen, schrijven en aanvullen (#91)`.

### Task 2: `init` en `--upgrade` leggen aan te maken vast

**Files:**
- Modify: `internal/initcmd/gitattributes.go`, `internal/initcmd/initcmd.go` (`buildPlan`), `internal/initcmd/initcmd_test.go` of een nieuw `internal/initcmd/created_test.go`
- Test: `internal/initcmd/created_test.go`

**Interfaces:**
- Consumes: `created.Read`, `created.Encode`, `Created.Merge`, `created.Path` (Taak 1); `planned.existed` uit `buildPlan`.
- Produces:
  ```go
  // missingGitattributes returns the lighthouse rules ensureGitattributes would add to old.
  func missingGitattributes(old []byte) []string
  // ensureGitattributes blijft bestaan en gebruikt missingGitattributes.
  ```
  `buildPlan` laadt `created.Path` via `load`, bepaalt een `created.Created` en zet `p.new = created.Encode(merged)` alleen als de samenvoeging anders is dan het bestaande.

- [ ] **Step 1: Schrijf de falende tests** (helpers `newRepo`, `run`, `initYes`, `read`, `write` uit `initcmd_test.go`):
  - `TestInitRecordsCreatedInEmptyRepo`: lege repo, `init --yes`; `.lighthouse/created.toml` bevat `lighthouse.toml`, `AGENTS.md`, `CLAUDE.md`, `.claude/settings.json`, `.gitattributes` in `files`, beide regels `*.sh eol=lf` en `.claude/hooks/lighthouse/** eol=lf` in `gitattributes`, en `settings_keys` met `hooks`, `permissions`, `permissions.allow` (de testpack registreert hooks; controleer welke `permissions`-sleutels de testpack echt maakt en pas de verwachting daarop aan).
  - `TestInitRecordsNothingForExistingFiles`: repo met eigen `AGENTS.md`, `lighthouse.toml`, `.claude/settings.json` en een `.gitattributes` die beide regels al heeft: `files` noemt alleen wat `init` zelf aanmaakte, `gitattributes` is leeg of ontbreekt.
  - `TestUpgradeAddsLanguageToCreated`: bestaande `settings.json` zonder `language`, `init`, `[project] language = "nl"` zetten, `--upgrade --yes`: `created.toml` heeft `settings_keys` met `language` en `settings_language = "dutch"`; eerdere items blijven staan.
  - `TestUpgradeCreatedOnlyGrows`: `--upgrade` zonder iets aan te maken laat `created.toml` byte-gelijk; een repo zonder aangemaakte dingen krijgt geen `created.toml` (voorafgaand `init` in een repo waar alles al bestond).
  - `TestMissingGitattributes`: in `gitattributes_test.go`, tabel: leeg → beide; `*.sh eol=lf\n` → alleen de hookregel; beide aanwezig → leeg; `ensureGitattributes` geeft dezelfde bytes als voorheen (bestaande tests blijven groen).
- [ ] **Step 2:** Run `go test ./internal/initcmd/ -run 'Created|MissingGitattributes' -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** `missingGitattributes` en laat `ensureGitattributes` het gebruiken. In `buildPlan`: `files` = elk van `lighthouse.toml`, `AGENTS.md`, `CLAUDE.md`, `.claude/settings.json`, `.gitattributes` waarvoor de `planned` `!existed && new != nil` is (na alle planstappen, vóór het manifest); `gitattributes` = `missingGitattributes(ga.old)`; `settings_keys` = sleutels (`language`, `hooks`, `permissions`, `permissions.allow`) die in `doc` ontbraken vóór `Apply`/`ApplyAllow`/`ApplyLanguage` en er daarna zijn (een niet-bestaande `settings.json` telt als leeg object); `settings_language` = waarde van `language` als de sleutel nu is toegevoegd. Lees het bestaande `created.toml` met `created.Read`, `Merge`, en schrijf alleen bij verschil. Als `CLAUDE.md` onleesbaar was (`unreadable`), komt het niet in `files`.
- [ ] **Step 4:** Zelfde run plus `go test ./internal/initcmd/ -race`; verwacht PASS, geen bestaande test breekt (zo nodig bijwerken waar een snapshot nu `created.toml` bevat).
- [ ] **Step 5: Commit** `feat(initcmd): init en upgrade leggen vast wat ze buiten de units aanmaken (#91)`.

### Task 3: `execute` in gedeelde stappen splitsen

**Files:**
- Modify: `internal/initcmd/initcmd.go`

**Interfaces:**
- Produces (in `initcmd.go`, namen aan de bouwer toegestaan zolang de drie stappen apart staan):
  ```go
  func showDiffs(o Options, plan map[string]*planned, paths []string)
  func confirm(o Options, in *bufio.Reader, ask func() bool) bool // o.Yes, anders ask()
  func applyAll(root string, plan map[string]*planned, paths []string, apply func(root, rel string, p *planned) error) error
  ```
  De bestaande `apply` blijft de standaard voor init; uninstall geeft een eigen `apply` mee.
  `execute` houdt de letterlijke aanroep `ask(o.Out, in, "Apply these changes? [y/N] ")` in `initcmd.go` (geef die als closure aan `confirm`): `TestInitPromptMatchesTheBinary` in `internal/repopins/docs_test.go` zoekt precies die aanroep met een regex.

- [ ] **Step 1:** Geen nieuwe test: gedrag moet gelijk blijven. Run eerst `go test ./internal/initcmd/ ./internal/cli/ ./internal/repopins/ -race`; verwacht PASS (basislijn).
- [ ] **Step 2: Refactor** het blok in `execute` (diff tonen, `reportBudget`, vragen, toepassen) naar de drie functies; `execute` roept ze aan in dezelfde volgorde (diffs, `reportBudget`, vraag, toepassen, `Wrote N file(s).`).
- [ ] **Step 3:** Zelfde run (incl. `./internal/repopins/`); verwacht PASS.
- [ ] **Step 4: Commit** `refactor(initcmd): diff, vraag en toepassen als gedeelde stappen (#91)`.

### Task 4: `Uninstall` voor de units van het manifest

**Files:**
- Create: `internal/initcmd/uninstall.go`, `internal/initcmd/uninstall_test.go`

**Interfaces:**
- Consumes: gedeelde stappen (Taak 3); `reconcile.Plan(nil, m, cur)`; `current(root, nil, m)`; `printConflict`; `agentsmd.Remove`; `settings.Apply(doc, nil)`; `settings.ApplyAllow(doc, nil, drop)`; `packs.IsLighthouseRule`; `orderedjson.Marshal`.
- Produces:
  ```go
  // Uninstall removes what init installed and recorded; returns 0 done, 1 refused, declined or failed.
  func Uninstall(o Options) int
  func removeEmptyDirs(root, rel string) error // omhoog tot root; stopt bij map met inhoud; __pycache__ telt als leeg
  ```
  Gebruikt `Options.Dir`, `Yes`, `Force`, `In`, `Out`, `Err`. Fouten als `lighthouse uninstall: <fout>` op `o.Err`.

- [ ] **Step 1: Schrijf de falende tests** (`uninstall_test.go`, helper `runUninstall(t, root, mods ...func(*Options))` naast `run`):
  - `TestUninstallNotInstalled`: zonder manifest rc 1, `err` bevat `not installed`, snapshot gelijk.
  - `TestUninstallRoundTripKeepsProjectContent`: repo met eigen tekst in `AGENTS.md`, eigen hook en eigen permissieregel in `settings.json`, eigen `.gitattributes`; `init --yes`, `uninstall --yes`: rc 0 en `.claude/hooks/lighthouse/`, `.lighthouse/manifest.toml` weg; eigen inhoud staat er; vergelijk de bestanden *en* mappen (nieuwe `treeSnapshot` die mappen ook telt). In deze taak mag de vergelijking `lighthouse.toml`, `created.toml`-gerelateerde bestanden en wat uit Taak 5 komt uitsluiten; Taak 5 verscherpt dit tot volledige gelijkheid.
  - `TestUninstallRefusesLocalEdit`: de gevallen van `TestUpgradeRefusesLocalEdit` waarbij een unit lokaal *gewijzigd* is (scriptregel, blokregel, hook-timeout); zonder `--force` rc 1, uitvoer bevat `refused: 1 locally modified unit(s)` en de diff, snapshot ongewijzigd; met `--force` gaan ze weg. Het geval 'deleted script' is geen conflict: `reconcile.Plan(nil, …)` geeft `Remove` voor een afwezige unit; test daarvoor apart dat uninstall zonder `--force` slaagt en de rest verwijdert.
  - `TestUninstallDecline`: invoer `n\n`, rc 1, niets geschreven, uitvoer bevat `Remove lighthouse? [y/N] `.
  - `TestUninstallNoSettingsFile`: manifest met hookunits maar `.claude/settings.json` verwijderd: uninstall maakt het niet aan.
  - `TestUninstallLastTwoPathsAndResume`: dwing een schrijffout af (map met `chmod 0o500` rond `AGENTS.md` of een `apply`-hook in `Options`; kies wat op Windows en Linux werkt, anders `t.Skip` op Windows): na de fout staan `manifest.toml` en `created.toml` er nog; na herstel maakt een tweede `uninstall --yes` het af.
  - `TestUninstallPycacheAndOwnFileInHookDir`: twee fixtures. (a) alleen `__pycache__` in `.claude/hooks/lighthouse/`: verdwijnt met de map. (b) `__pycache__` plus een eigen bestand: de map blijft met alles erin (pycache blijft ernaast staan) en de bovenliggende `.claude` blijft.
  - `TestRemoveEmptyDirs`: tabel op een tempdir: leeg keten tot root; stop bij map met bestand; `__pycache__`-only telt leeg; verwijdert nooit de root.
- [ ] **Step 2:** Run `go test ./internal/initcmd/ -run 'Uninstall|RemoveEmptyDirs' -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** `Uninstall`: `repoRoot`; `manifest.Read` (`fs.ErrNotExist` → fout `lighthouse is not installed here (.lighthouse/manifest.toml not found)`); `current` + `reconcile.Plan(nil, m, cur)`; zonder `--force` conflicten tonen via `printConflict` en `refused: N locally modified unit(s)` teruggeven (geen `keptRetired`-uitzondering voor uninstall: elke conflictunit telt). Plan per `Change`: `KindFile` → `delete`; `KindBlock` → `agentsmd.Remove` op de `AGENTS.md`-planned; `KindHook` → `settings.Apply(doc, nil)` eenmaal; `KindPermission` met `IsLighthouseRule` → drop. Verwijder het manifest zelf als eigen `planned{delete}`, en `created.Path` altijd als eigen `planned{delete}` (ook als `created.toml` ontbreekt: dan overslaan). Pad-volgorde: alle andere paden gesorteerd, dan `manifest.Path`, dan `created.Path`. Toepassen met een `apply` die bij `delete` `os.Remove` + `removeEmptyDirs` doet. Succes: `Removed N file(s).` op `o.Out`.
- [ ] **Step 4:** Zelfde run; verwacht PASS. Run `go test ./internal/initcmd/ -race`.
- [ ] **Step 5: Commit** `feat(initcmd): uninstall verwijdert de units van het manifest (#91)`.

### Task 5: Het vastgelegde terugdraaien

**Files:**
- Modify: `internal/initcmd/uninstall.go`, `internal/initcmd/uninstall_test.go`

**Interfaces:**
- Consumes: `created.Read`, `Created` (Taak 1); uninstall-plan van Taak 4.
- Produces:
  ```go
  // revertCreated plans the undoing of what created.toml records, on top of the unit plan.
  // It returns lines for the report (kept / left).
  func revertCreated(root string, c created.Created, plan map[string]*planned, load func(string) (*planned, error)) (notes []string, err error)
  ```

- [ ] **Step 1: Schrijf de falende tests**:
  - `TestUninstallRoundTripEmptyRepo`: lege repo, `init --yes`, `uninstall --yes`: boom (bestanden en mappen) gelijk aan vooraf; geen `AGENTS.md`, `CLAUDE.md`, `settings.json`, `.gitattributes`, `lighthouse.toml`, `.lighthouse/`.
  - `TestUninstallRoundTripWithOwnContent`: de rondreis uit Taak 4, nu met volledige snapshotgelijkheid inclusief `lighthouse.toml`-afwezigheid en `.gitattributes` met alleen eigen regels (fixtures eindigen op één `\n`).
  - `TestUninstallRoundTripViaUpgrade`: bestaande `settings.json`, `init`, `[project] language = "nl"` zetten, `--upgrade`, `uninstall`: `"language"` weg, rest gelijk aan vooraf (JSON-inhoud).
  - `TestUninstallKeepsDeviatingCreated`: `"language"` na init gewijzigd naar `"english"` → blijft staan en de melding noemt het; `CLAUDE.md` uitgebreid naar `@AGENTS.md\nmore\n` → blijft staan, melding.
  - `TestUninstallWithoutCreatedToml`: manifest aanwezig, `created.toml` verwijderd: units weg, `lighthouse.toml`, `CLAUDE.md`, `.gitattributes`-regels en `"language"` blijven en elk krijgt een `left: <pad> — init may have written it; remove it yourself if so`.
  - `TestUninstallSettingsOddFormat`: `settings.json` met afwijkende opmaak: JSON-inhoud gelijk aan vooraf, geen bytevergelijking.
  - `TestUninstallCorruptCreatedToml`: kapotte `created.toml` geeft rc 1, niets geschreven.
- [ ] **Step 2:** Run `go test ./internal/initcmd/ -run Uninstall -race`; verwacht FAIL op de nieuwe gevallen.
- [ ] **Step 3: Implementeer** `revertCreated` volgens de tabel *Terugdraaien van het vastgelegde* in de spec: `lighthouse.toml` in `files` altijd weg; `AGENTS.md` in `files` weg bij alleen witruimte na `agentsmd.Remove`; `settings.json` in `files` weg bij leeg object (lege `hooks`, `permissions`, `allow` tellen leeg; `settings.Apply`/`ApplyAllow` laten die containers staan, dus verwijder ze hier als ze in `settings_keys` staan of `settings.json` in `files`); `CLAUDE.md` in `files` weg als de inhoud precies `@AGENTS.md\n` is én `AGENTS.md` weggaat of al afwezig is; `.gitattributes`-regels: per vastgelegde regel de laatste exacte treffer weg (CRLF-bewust), een ontbrekende regel overslaan, bestand weg bij alleen witruimte en in `files`; `"language"` weg als de waarde gelijk is aan `settings_language`. Afwijkend → blijft staan, regel in `notes`. Zonder `created.toml` (`found == false`): geen enkele terugdraai, wel `left:`-regels voor de aanwezige `lighthouse.toml`, `CLAUDE.md` met een `@AGENTS.md`-regel, `.gitattributes` met een van beide lighthouse-regels, `"language"` in `settings.json`. Roep `revertCreated` aan vóór het plannen van manifest en `created.toml`.
- [ ] **Step 4:** Zelfde run plus `go test ./internal/initcmd/ -race`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(initcmd): uninstall draait vastgelegde aanmaak terug (#91)`.

### Task 6: Meldingen achteraf

**Files:**
- Modify: `internal/initcmd/uninstall.go`, `internal/initcmd/uninstall_test.go`

**Interfaces:**
- Consumes: `notes` uit `revertCreated`; `[paths]` uit `lighthouse.toml`.
- Produces:
  ```go
  // seededPaths reads [paths] labels, strategy, tenets and areas from lighthouse.toml (before it goes).
  func seededPaths(root string) []string
  // report writes the closing lines of an uninstall to o.Err.
  func report(o Options, root string, seeded, notes []string)
  ```

- [ ] **Step 1: Schrijf de falende tests**:
  - `TestUninstallRoundTripStandardLevel`: opzet met `levelPacks()` en `flagLevelsToml` (`levels_test.go`) zodat het level iets zaait, en `Direction: true` voor de direction-docs (de standaard `testPacks()` zaait niets); `init --level standard --yes`, `uninstall --yes`: het labelsbestand en de direction-docs staan er nog, `o.Err` bevat per bestaand pad `kept: <pad> (project file after seeding)`; de rest van de boom gelijk aan vooraf.
  - `TestUninstallReportsRuntimeDirs`: `.lighthouse/run/x` en `.lighthouse/bin/y` aangemaakt voor uninstall: blijven staan, melding `kept: .lighthouse/run (runtime state, not installed by init)` en idem `bin`.
  - `TestUninstallReportsNotTouched`: `o.Err` bevat een regel `not touched:` die labels op de forge en `host.toml` noemt, en een regel dat een lopende Claude Code-sessie de hooks pas na een herstart kwijt is.
  - `TestUninstallLeavesNoReportOnRefusal`: geweigerde run schrijft geen `kept:`-regels.
- [ ] **Step 2:** Run `go test ./internal/initcmd/ -run Uninstall -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** `seededPaths` (lees vóór het plannen, `lighthouse.toml` gaat in dezelfde run weg; ontbrekend of onleesbaar → lege lijst, geen fout), de `kept:`-regels na `apply` voor wat nog bestaat, de scan van `.lighthouse/` op resterende mappen, en de vaste regels. Exacte tekst: zie Global Constraints.
- [ ] **Step 4:** Zelfde run plus `go test ./internal/initcmd/ -race`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(initcmd): uninstall meldt wat blijft staan (#91)`.

### Task 7: Commando, docs, changelog, dogfood

**Files:**
- Create: `internal/cli/uninstall.go`, `internal/cli/uninstall_test.go`, `changelog.d/issue-91-lighthouse-uninstall.md`
- Modify: `internal/cli/root.go` (naast `newInitCommand()`), `docs/getting-started.md`, `internal/repopins/docs_test.go`

**Interfaces:**
- Consumes: `initcmd.Uninstall`, `errSilent` uit `internal/cli/init.go`.
- Produces: `func newUninstallCommand() *cobra.Command` met `Use: "uninstall"`, `Args: cobra.NoArgs`, flags `--yes` (`Options.Yes`) en `--force` (`Options.Force`).

- [ ] **Step 1: Schrijf de falende tests**:
  - `internal/cli/uninstall_test.go`: `TestUninstallCommandRegistered` (`NewRoot(...)` kent `uninstall`; `--help` noemt `--yes` en `--force`); `TestUninstallCommandNotInstalled` in een tempdir-repo: exitcode 1 en de fout op stderr (volg het patroon van `init_test.go`).
  - `internal/repopins/docs_test.go`: `TestGettingStartedNamesUninstall`: `docs/getting-started.md` bevat `lighthouse uninstall` en de heading `## Remove lighthouse` (of de Nederlandse sectietitel die het document gebruikt; lees de koppen eerst).
- [ ] **Step 2:** Run `go test ./internal/cli/ ./internal/repopins/ -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** `newUninstallCommand` (zelfde opbouw als `newInitCommand`: `os.Getwd`, `o.In/Out/Err` van cmd, `initcmd.Run`-patroon met `errSilent` bij code ≠ 0) en registreer het in `root.go`. Schrijf in `docs/getting-started.md` een sectie na de upgrade-bullets: het commando, de vraag `Remove lighthouse? [y/N]`, `--yes`, `--force`, wat blijft staan (labelsbestand en direction-docs, `.lighthouse/run`, labels op de forge, `host.toml`, de binary), dat installaties van vóór deze versie `lighthouse.toml` en de rest zelf opruimen, dat `settings.json` herschreven wordt in canonieke opmaak, en dat een bestand zonder slotnewline of met een lege slotregel niet byte-gelijk terugkomt. Schrijf het changelog-fragment (`type: added`, Engels, met de compatibiliteitsnoot: een oudere binary negeert `created.toml`; wat een `--upgrade` met een oudere binary aanmaakt blijft bij uninstall staan en wordt gemeld).
- [ ] **Step 4:** Run `go test ./... -race`, `lighthouse imports check` en `go run ./cmd/lighthouse init --upgrade`; verwacht PASS en `No changes.` (zo niet: commit de geïnstalleerde kopie mee).
- [ ] **Step 5: Commit** `feat(cli): lighthouse uninstall met startgids en changelog (#91)`.
