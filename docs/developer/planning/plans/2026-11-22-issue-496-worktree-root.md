# Plek van de worktrees instelbaar (#496) — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** `[worktree] root` in `lighthouse.toml` bepaalt waar worktrees komen (standaard `.claude/worktrees`); hooks, audit en skills gebruiken die plek, en `init` waarschuwt voor een sync-map.

**Architectuur:** Eén nieuwe config-sleutel `worktree.root` (`kStr`, standaard `.claude/worktrees`). Eén Go-helper `internal/worktreeroot` lost de waarde op (absoluut of relatief aan de repo-root) en detecteert sync-mappen. Skills en het managed block zijn statische tekst: ze noemen `lighthouse config worktree.root` en gebruiken die waarde **op runtime**. De standaardwaarde staat alleen in het schema en als verklarende zin in de tekst.

**Tech Stack:** Go (`internal/config`, `internal/hooks`, `internal/worktreeaudit`, `internal/initcmd`), pack-skills (Markdown), Windows-CI (`ci/windows-measure.sh`).

**Spec:** geen aparte spec; de acceptatiecriteria van [issue #496](https://github.com/CorneeldH/lighthouse/issues/496) zijn de spec. Milestone M3 (tweede omgeving).

## Ontwerpkeuze (vastgelegd)

Placeholder bij `init` versus `lighthouse config worktree.root` op runtime: **runtime**. Reden: `init --upgrade` herschrijft anders elke skill bij een wijziging van de sleutel; dogfood-diff blijft klein; de skills lezen al elders config zo (`lighthouse config forge.default_branch`). Gevolg: de pack-tekst bevat het letterlijke `.claude/worktrees` nog maar op één plek, als standaardwaarde-uitleg.

Relatief pad = relatief aan de repo-root (de hoofdcheckout, niet aan de worktree zelf). Een worktree-pad wordt daarmee `<root>/<name>`; bij een relatieve waarde `<repo>/<root>/<name>`. "Repo-root" is overal de hoofdcheckout, ook wanneer lighthouse of een skill vanuit een gelinkte worktree draait: `git rev-parse --show-toplevel` geeft dan de worktree zelf en is **niet** bruikbaar; gebruik de map boven `git rev-parse --path-format=absolute --git-common-dir` (zoals `pack/core/skills/fix-pr/agents/SKILL.md` al doet).

## Globale eisen

- Instructies, skills en hookberichten in het Engels; docs, tests-proza en changelog in het Nederlands (`[project] language = nl`).
- Geen nieuwe GitHub-aanname buiten `internal/forge`; de sync-waarschuwing is forge-neutraal.
- Dogfood: na elke wijziging onder `pack/` `go run ./cmd/lighthouse init --upgrade`, geïnstalleerde kopie committen (`No changes.` daarna). Nooit `.claude/skills`, `.agents/skills` of `.claude/hooks` met de hand.
- Voor elke commit: `[test_gate] command` en `lighthouse imports check`.
- `.md`: alleen relatieve links. Elke PR krijgt een fragment in `changelog.d/`.
- Test volgens `lighthouse-test-standard`: per test de productiewijziging noemen die hem rood maakt.

## Review Focus

- `root` met spaties (`C:\Users\A B\wt`, `/tmp/a b/wt`): hooks, audit en init moeten het pad heel houden.
- `root` die zelf in de repo valt maar niet `.claude/worktrees` is (bijv. `.wt`): audit-orphans en KPI-telling moeten meegaan.
- Lege of niet-bestaande `root`: orphan-scan geeft geen orphans, geen fout; `init` geeft geen crash.
- Relatieve `root` met `..` (`../wt`): oplossen tot één schoon absoluut pad.
- Een repo of `root` in `OneDrive - Bedrijf` (met spatie en streepje): waarschuwing moet nog afgaan.

---

### Taak 1: Sleutel `worktree.root` en resolutie-helper

**Bestanden:**
- Wijzig: `internal/config/config.go` (schema ~:112, defaults ~:166)
- Maak: `internal/worktreeroot/worktreeroot.go`, `internal/worktreeroot/worktreeroot_test.go`
- Wijzig: `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/defaults/expect.json`, `docs/site/data/config.json` (entry `worktree.root`, niveau advanced, niet verplicht)

**Interfaces:**
- Produceert: `config` kent `worktree.root` (`kStr`, standaard `".claude/worktrees"`).
- Produceert: `func Resolve(repoRoot, value string) string` — `value == ""` telt als de standaard; absoluut blijft absoluut; relatief wordt `filepath.Join(repoRoot, value)`; resultaat `filepath.Clean`.
- Produceert: `const Default = ".claude/worktrees"`.
- Produceert: `func MainCheckout(dir string) (string, error)` — de hoofdcheckout van de repo waarin `dir` ligt, via de map boven `git rev-parse --path-format=absolute --git-common-dir` (vanuit een gelinkte worktree dus de hoofdcheckout, niet `--show-toplevel`).
- Produceert: `func Contains(root, path string) bool` — `path` ligt in `root` volgens `filepath.Rel` (resultaat niet `.` en niet beginnend met `..`); géén `strings.HasPrefix` (`/tmp/wt-old` valt niet onder `/tmp/wt`).

- [ ] **Stap 1: schrijf falende tests** in `worktreeroot_test.go`: `TestResolve` met tabel (`""`→`<repo>/.claude/worktrees`; `".wt"`→`<repo>/.wt`; `"../wt"`→ bovenliggende map schoon; absoluut met spaties blijft gelijk; op Windows `C:\Users\A B\wt` blijft gelijk). Test in `config_test.go`-stijl dat `config.Default("worktree.root")` `".claude/worktrees"` geeft. `TestContains` met `/tmp/wt` tegen `/tmp/wt/a` (ja), `/tmp/wt-old/a` (nee), `/tmp/wt` zelf (nee). `TestMainCheckout` in een echte tijdelijke repo met gelinkte worktree: vanuit beide mappen dezelfde hoofdcheckout.
- [ ] **Stap 2: draai** `go test ./internal/worktreeroot ./internal/config` → FAIL (package/sleutel ontbreekt).
- [ ] **Stap 3: implementeer** de sleutel in schema en defaults, en `Resolve`/`Default`. Gebruik `filepath.IsAbs`.
- [ ] **Stap 4: werk de snapshots bij** (`config-schema.json`, `defaults/expect.json`, `docs/site/data/config.json`); `config_test.go:14` dwingt dit af. `docs/site/test/config.test.mjs` eist per sleutel op de kaart dat de lijst `readers` exact de bestanden zijn die de tekst `worktree.root` bevatten (`pack/`, `internal/`, `cmd/`) en dat een sleutel zonder lezer in `UNREAD` staat; de gate draait die test bij elke commit onder `internal/`, `pack/` of `docs/site/`. Elke taak die een lezer toevoegt of verplaatst werkt dus de `readers` van `worktree.root` in `docs/site/data/config.json` bij en draait `node --test docs/site/test/config.test.mjs` vóór de commit. In taak 1 (nog geen lezer buiten `internal/config`, die `SKIPPED_READERS` overslaat): voeg `worktree.root` alleen aan `UNREAD` toe als de test anders rood blijft, en haal hem er in taak 2 weer uit.
- [ ] **Stap 5: draai** `go test ./internal/config ./internal/worktreeroot` en `node --test docs/site/test/config.test.mjs` → PASS.
- [ ] **Stap 6: commit** `feat(config): [worktree] root met standaard .claude/worktrees (#496)`.

### Taak 2: Drie herkenners (hooks en audit)

**Bestanden:**
- Wijzig: `internal/hooks/worktreecheck.go:23,104` (bericht), `internal/hooks/maincommitgate.go:162` (bericht)
- Wijzig: `internal/worktreeaudit/collect.go:28,404-429`, `internal/worktreeaudit/render.go:271`
- Test: `internal/hooks/worktreecheck_test.go`, `internal/hooks/maincommitgate_test.go`, `internal/worktreeaudit/twins_test.go`

**Interfaces:**
- Verbruikt: `worktreeroot.Resolve`, `worktreeroot.Default`.
- Produceert: `findOrphans(root string, worktrees []WorktreeInfo)` leest de ingestelde `root` (via config, `Resolve`) in plaats van `const worktreeDir`; de KPI-telling matcht een worktree als zijn pad onder de opgeloste `root` ligt (`worktreeroot.Contains`, niet `strings.Contains` op de relatieve string en niet `strings.HasPrefix`). `View` krijgt een veld `Root string` (de opgeloste map; gezet door `internal/cli/worktreeaudit.go` uit `Collect`); de fixtures in `fixtures_test.go` en `tests/pack/fixtures/worktree_audit/render.json` zonder `Root` vallen terug op `<repo>/.claude/worktrees`, zodat de bestaande verwachtingen gelijk blijven.
- Produceert: `worktree-check` verwerkt een absoluut pad in Windows-vorm (`C:\Users\A B\wt\x.go`): een pure helper `isAbsPath(p string) bool` die ook `X:\` en `X:/` (laatste ook op Linux, via eigen controle op letter + `:` + `\` of `/`) als absoluut telt, en `canonMissing` start vanaf `filepath.VolumeName(path)+"/"` in plaats van vast `"/"` (`worktreecheck.go:138-139`), na `filepath.ToSlash`; zo wordt `C:/Users/A B/x` nooit `/C:/Users/...` en nooit aan de cwd geplakt (nu: alleen een leidende `/` telt, `worktreecheck.go:47`).

Detectie via git (`--git-dir` ≠ `--git-common-dir` in `worktreecheck`; `--show-toplevel` + branch in `main-commit-gate`) is padonafhankelijk voor POSIX-paden: de taak bewijst dat met tests en past daar alleen de berichten aan (noem de ingestelde plek, niet het vaste pad). De padverwerking van `worktree-check` is dat **niet** voor Windows-paden (zie Produceert hierboven) en wordt in deze taak gerepareerd.

- [ ] **Stap 1: schrijf falende tests.** Hooks: echte tijdelijke repo plus `git worktree add` naar een map **buiten** de repo met een spatie in het pad; `worktree-check` geeft geen waarschuwing voor een write daarin, wel voor een write in de hoofdcheckout op `main`; `main-commit-gate` blokkeert een commit in de hoofdcheckout op `main` en laat een commit in die worktree toe. Audit: `findOrphans` vindt een wees onder een `root` buiten de repo; de KPI telt een worktree onder die `root` en telt `<root>-old/x` niet mee. Windows-pad: een `worktree-check`-test met een `file_path` in Windows-vorm (alleen onder `runtime.GOOS == "windows"`), plus een tabeltest van `isAbsPath` die overal draait (`C:\x`, `C:/x`, `/x` ja; `x`, `./x`, `C:x` nee).
- [ ] **Stap 2: draai** `go test ./internal/hooks ./internal/worktreeaudit` → de audit-tests en de `isAbsPath`-tabeltest FAIL; de git-detectietests van de hooks (POSIX) PASS (vastleggen in de commit).
- [ ] **Stap 3: implementeer** in `worktreeaudit`: `worktreeDir`-const weg, `root` uit config opgelost met `Resolve`; `render.go` vergelijkt tegen de opgeloste map. Hookberichten: lees `worktree.root` zoals `internal/hooks/hooks.go:31-37` andere sleutels leest.
- [ ] **Stap 4: draai** dezelfde tests → PASS; draai ook de bestaande fixtures (`tests/pack/fixtures/worktree_audit/*.json`); pas alleen aan wat de standaard betreft als het gedrag per ongeluk veranderde.
- [ ] **Stap 5: werk `readers` van `worktree.root` bij** in `docs/site/data/config.json` (`internal/worktreeaudit/collect.go`, `internal/hooks/*.go` die de tekst noemen) en draai `node --test docs/site/test/config.test.mjs` → PASS.
- [ ] **Stap 6: commit** `feat(worktree): hooks en audit herkennen een root buiten de repo (#496)`.

### Taak 3: Pack-tekst gebruikt de ingestelde plek

**Bestanden:**
- Wijzig: `pack/core/instructions/AGENTS.md:3`; skills `fix-pr`, `close-issue`, `upgrade`, `start-issue`, `review-plan`, `write-handoff`, `audit-worktrees` (kernpaden uit het impact-pack, `claude/` en `agents/` varianten) en `pack/go/skills/go-standard/{claude,agents}/SKILL.md:44`
- Wijzig: `internal/repopins/instructions_test.go:59`, `internal/packcontract/auditworktrees_test.go`, `docs/workplace.md`, `docs/getting-started.md`
- Test: nieuwe pin in `internal/repopins` of `internal/packcontract`

**Interfaces:**
- Consumeert: `worktree.root` (taak 1).
- Woordenbudget: het managed block staat precies op het budget van 182 woorden (`TestCoreBlockWordBudget`, `internal/repopins/instructions_test.go:62`). De regel over worktrees in `pack/core/instructions/AGENTS.md:3` wordt **vervangen**, niet aangevuld, door een zin met evenveel of minder woorden die `lighthouse config worktree.root` noemt en `.claude/worktrees` één keer als standaard; het budget wordt niet verhoogd. De uitvoerder telt vóór de commit met `wc -w pack/core/instructions/AGENTS.md` (blok ≤ 182) en past de pin `requireTokens(... ".claude/worktrees/")` (`instructions_test.go:59`) aan naar `worktree.root`.

- [ ] **Stap 1: schrijf een falende pin-test**: geen `.claude/worktrees` in `pack/` behalve in een toegestane lijst (de ene standaardwaarde-zin in het managed block, en `internal/workplace/local` blijft buiten `pack/`). Assert met de exacte toegestane bestanden, zodat een nieuwe hardcode rood maakt.
  Tweede pin: elke skill die `lighthouse config worktree.root` gebruikt om een relatieve waarde op te lossen, bevat letterlijk bovenstaande `case "$ROOT" in /*|[A-Za-z]:*)`-regel en het `--git-common-dir`-anker; een skill met alleen `ROOT=$(lighthouse config worktree.root)` is rood, evenals een die het anker onvoorwaardelijk toepast (de pin eist de `case`-regel).
- [ ] **Stap 2: draai** de pin → FAIL met de lijst bestanden.
- [ ] **Stap 3: herschrijf** de plekken: managed block en skills zeggen `ROOT=$(lighthouse config worktree.root)` (relatief → onder de repo-root) en maken/zoeken `<root>/<name>`; `git worktree add "<root>/<name>" -b <branch>` met aanhalingstekens (spaties). De skills gebruiken één vaste shell-vorm, die absoluut (`/*`, `[A-Za-z]:*`) van relatief onderscheidt:
  ```bash
  ROOT=$(lighthouse config worktree.root)
  case "$ROOT" in /*|[A-Za-z]:*) ;; *) ROOT="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")/$ROOT" ;; esac
  ```
  Een relatieve waarde hangt zo aan de hoofdcheckout (nooit aan de cwd of `--show-toplevel`: geen geneste worktree vanuit een gelinkte worktree), een absolute blijft ongemoeid. Het managed block noemt `.claude/worktrees` één keer als standaard. Pas `review-plan` en `write-handoff` aan: padreductie gaat uit van de opgeloste root, niet van de letterlijke map.
- [ ] **Stap 4: draai** `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie in dezelfde commit; daarna `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Stap 5: draai** `go test ./internal/repopins ./internal/packcontract ./internal/checkinstr/...` en `node --test docs/site/test/config.test.mjs` → PASS (de `readers` van `worktree.root` in `docs/site/data/config.json` bevatten nu elke pack-skill/-bestand dat de tekst noemt, per `pack/<pack>/skills/<naam>`).
- [ ] **Stap 6: commit** `feat(pack): worktrees op de ingestelde plek, geen hardcoded pad (#496)`.

### Taak 4: Sync-map-waarschuwing in `init`

**Bestanden:**
- Wijzig: `internal/worktreeroot/worktreeroot.go` (nieuw), `internal/initcmd/initcmd.go` (`Run`, warnings in stijl van :489/:941)
- Test: `internal/worktreeroot/worktreeroot_test.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produceert: `func InSyncFolder(path string) (provider string, ok bool)` — herkent padsegmenten (hoofdletterongevoelig) `OneDrive` (ook `OneDrive - <naam>`), `Dropbox`, `iCloud Drive`, `iCloudDrive`, `Library/Mobile Documents/com~apple~CloudDocs`; scheidingsteken `/` en `\`.
- Consumeert: `Resolve`, `MainCheckout`. `initcmd.Run` leidt de hoofdcheckout af met `worktreeroot.MainCheckout(root)` en gebruikt `repoRoot` (`--show-toplevel`) niet voor het oplossen van `root` of de sync-controle van de repo.

- [ ] **Stap 1: schrijf falende tests**: tabel voor `InSyncFolder` (o.a. `C:\Users\A\OneDrive - Bedrijf\repo`, `/home/a/Dropbox/x`, `/Users/a/Library/Mobile Documents/com~apple~CloudDocs/x`, `/home/a/MyOneDriveNotes/x` → **niet**: alleen volledige segmenten). Init-test: repo of opgeloste `root` in een sync-map → `warning:` op `o.Err`, exitcode 0; geen sync-map → geen waarschuwing. Plus een init-test vanuit een **gelinkte worktree buiten de sync-map** waarvan de hoofdcheckout in een sync-map ligt (padsegment `OneDrive - Bedrijf`): de waarschuwing gaat af.
- [ ] **Stap 2: draai** → FAIL.
- [ ] **Stap 3: implementeer** `InSyncFolder` en roep het in `initcmd.Run` aan voor de repo-root en de opgeloste `worktree.root`; waarschuwing in het Engels, blokkeert niet, noemt de provider en wijst op `[worktree] root`.
- [ ] **Stap 4: draai** `go test ./internal/worktreeroot ./internal/initcmd ./internal/cli` → PASS.
- [ ] **Stap 5: werk `readers`** van `worktree.root` in `docs/site/data/config.json` bij (`internal/initcmd/initcmd.go`) en draai `node --test docs/site/test/config.test.mjs` → PASS.
- [ ] **Stap 6: commit** `feat(init): waarschuw voor een repo of worktree-root in een sync-map (#496)`.

### Taak 5: Windows-CI, docs en changelog

**Bestanden:**
- Wijzig: `ci/windows-measure.sh`, `.github/workflows/windows-measure.yml` (alleen als nodig)
- Wijzig: `docs/workplace.md`, `docs/getting-started.md`, `docs/site/data/config.json` (beschrijving van de sleutel)
- Maak: `changelog.d/issue-496-worktree-root.md`

- [ ] **Stap 1:** voeg aan `ci/windows-measure.sh` een stap toe: maak een tijdelijke repo, een `root` buiten de repo in een pad **met spaties**, `git worktree add` daarheen, en laat `worktree-check` en `main-commit-gate` daar draaien. De `hook`-helper kent nu alleen een Bash-payload (`tool_input.command`) en Bash-matchers; `worktree-check` staat op `Edit|Write` en leest `tool_input.file_path`. Voeg dus een payload-variant toe (`file_path` in Windows-vorm met spaties) en toets op de **tekst** `Worktree check:` in de `systemMessage` (aanwezig voor een write in de hoofdcheckout op `main`, afwezig voor een write in de externe worktree), niet op exit 0 alleen (de hook blokkeert nooit). Houd bestaande aanroepen (`WORK=$2`) intact. Het script en `windows-measure.yml` zijn een meting en blijven groen wat ze ook vinden (`ci/windows-measure.sh:153-156`, `continue-on-error`): de Go-tests op Windows-CI zijn ook `continue-on-error` en dus evenmin een gate. Ook die rode uitkomst telt: zie stap 4.
- [ ] **Stap 2:** documenteer `[worktree] root`, de standaard, de relatieve/absolute regel, de sync-waarschuwing en dat een relatieve root binnen de repo anders dan `.claude/worktrees` (bijv. `.wt`) zelf in `.gitignore` of `.git/info/exclude` moet, in `docs/getting-started.md`; corrigeer de verwijzingen in `docs/workplace.md`.
- [ ] **Stap 3:** schrijf het changelogfragment in het Nederlands.
- [ ] **Stap 4: eindcontrole Windows:** push de branch en lees (zonder te wachten op CI in de controller: het resultaat van de eerstvolgende run, bij een volgende fase) in de `windows-measure`-run zowel de meetrij van de nieuwe externe-root-controle als de uitkomst van `id: gotest` (`go-test`). Staat een van beide op FAIL, meld dat letterlijk in de PR-tekst en laat AC 5 onaangevinkt; de PR-schrijver (`/lighthouse-write-pr`) mag AC 5 alleen bewijzen met een groene uitkomst.
- [ ] **Stap 4b: eindcontrole:** `go test ./...`, `node --test docs/site/test/*.test.mjs`, `lighthouse imports check`, `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Stap 5: commit** `docs(worktree): [worktree] root, sync-waarschuwing en Windows-CI (#496)`.

## Buiten scope

`internal/workplace/local` (`excludeLine` in `info/exclude`) en `internal/kpi/startissue.go` (`worktreeMarker`) zijn adapter- respectievelijk KPI-code met eigen conventie; eventueel vervolgissue als de standaard daar knelt. `internal/dispatch/testdata` blijft ongewijzigd.

## Self-review

- AC 1 → taak 1; AC 2 → taak 3; AC 3 → taak 2; AC 4 → taak 4; AC 5 → taken 2 en 5 (root buiten de repo, spaties, Windows-CI).
- Typeconsistentie: `Resolve(repoRoot, value string) string`, `Default`, `InSyncFolder(path string) (string, bool)` worden in taken 2–4 gelijk gebruikt.
- Open risico: de skill-tekst voor `write-handoff`/`review-plan` bevat voorbeeldpaden; de uitvoerder controleert elke `.claude/worktrees`-treffer met `git grep` na taak 3.
