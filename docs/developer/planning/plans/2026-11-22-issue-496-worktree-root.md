# Plek van de worktrees instelbaar (#496) — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** `[worktree] root` in `lighthouse.toml` bepaalt waar worktrees komen (standaard `.claude/worktrees`); hooks, audit en skills gebruiken die plek, en `init` waarschuwt voor een sync-map.

**Architectuur:** Eén nieuwe config-sleutel `worktree.root` (`kStr`, standaard `.claude/worktrees`). Eén Go-helper `internal/worktreeroot` lost de waarde op (absoluut of relatief aan de repo-root) en detecteert sync-mappen. Skills en het managed block zijn statische tekst: ze noemen `lighthouse config worktree.root` en gebruiken die waarde **op runtime**. De standaardwaarde staat alleen in het schema en als verklarende zin in de tekst.

**Tech Stack:** Go (`internal/config`, `internal/hooks`, `internal/worktreeaudit`, `internal/initcmd`), pack-skills (Markdown), Windows-CI (`ci/windows-measure.sh`).

**Spec:** geen aparte spec; de acceptatiecriteria van [issue #496](https://github.com/CorneeldH/lighthouse/issues/496) zijn de spec. Milestone M3 (tweede omgeving).

## Ontwerpkeuze (vastgelegd)

Placeholder bij `init` versus `lighthouse config worktree.root` op runtime: **runtime**. Reden: `init --upgrade` herschrijft anders elke skill bij een wijziging van de sleutel; dogfood-diff blijft klein; de skills lezen al elders config zo (`lighthouse config forge.default_branch`). Gevolg: de pack-tekst bevat het letterlijke `.claude/worktrees` nog maar op één plek, als standaardwaarde-uitleg.

Relatief pad = relatief aan de repo-root (de hoofdcheckout, niet aan de worktree zelf). Een worktree-pad wordt daarmee `<root>/<name>`; bij een relatieve waarde `<repo>/<root>/<name>`.

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
- Wijzig: `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/defaults/expect.json`, `docs/site/data/config.json`

**Interfaces:**
- Produceert: `config` kent `worktree.root` (`kStr`, standaard `".claude/worktrees"`).
- Produceert: `func Resolve(repoRoot, value string) string` — `value == ""` telt als de standaard; absoluut blijft absoluut; relatief wordt `filepath.Join(repoRoot, value)`; resultaat `filepath.Clean`.
- Produceert: `const Default = ".claude/worktrees"`.

- [ ] **Stap 1: schrijf falende tests** in `worktreeroot_test.go`: `TestResolve` met tabel (`""`→`<repo>/.claude/worktrees`; `".wt"`→`<repo>/.wt`; `"../wt"`→ bovenliggende map schoon; absoluut met spaties blijft gelijk; op Windows `C:\Users\A B\wt` blijft gelijk). Test in `config_test.go`-stijl dat `config.Default("worktree.root")` `".claude/worktrees"` geeft.
- [ ] **Stap 2: draai** `go test ./internal/worktreeroot ./internal/config` → FAIL (package/sleutel ontbreekt).
- [ ] **Stap 3: implementeer** de sleutel in schema en defaults, en `Resolve`/`Default`. Gebruik `filepath.IsAbs`.
- [ ] **Stap 4: werk de snapshots bij** (`config-schema.json`, `defaults/expect.json`, `docs/site/data/config.json`); `config_test.go:14` dwingt dit af.
- [ ] **Stap 5: draai** `go test ./internal/config ./internal/worktreeroot` → PASS.
- [ ] **Stap 6: commit** `feat(config): [worktree] root met standaard .claude/worktrees (#496)`.

### Taak 2: Drie herkenners (hooks en audit)

**Bestanden:**
- Wijzig: `internal/hooks/worktreecheck.go:23,104` (bericht), `internal/hooks/maincommitgate.go:162` (bericht)
- Wijzig: `internal/worktreeaudit/collect.go:28,404-429`, `internal/worktreeaudit/render.go:271`
- Test: `internal/hooks/worktreecheck_test.go`, `internal/hooks/maincommitgate_test.go`, `internal/worktreeaudit/twins_test.go`

**Interfaces:**
- Verbruikt: `worktreeroot.Resolve`, `worktreeroot.Default`.
- Produceert: `findOrphans(root string, worktrees []WorktreeInfo)` leest de ingestelde `root` (via config, `Resolve`) in plaats van `const worktreeDir`; de KPI-telling matcht een worktree als zijn pad onder de opgeloste `root` ligt (`strings.HasPrefix` op schoon pad, niet `Contains` op de relatieve string).

Detectie in `worktreecheck` (`--git-dir` ≠ `--git-common-dir`) en `main-commit-gate` (`--show-toplevel` + branch) is al padonafhankelijk: de taak bewijst dat met tests en past alleen de berichten aan (noem de ingestelde plek, niet het vaste pad).

- [ ] **Stap 1: schrijf falende tests.** Hooks: echte tijdelijke repo plus `git worktree add` naar een map **buiten** de repo met een spatie in het pad; `worktree-check` geeft geen waarschuwing voor een write daarin, wel voor een write in de hoofdcheckout op `main`; `main-commit-gate` blokkeert een commit in de hoofdcheckout op `main` en laat een commit in die worktree toe. Audit: `findOrphans` vindt een wees onder een `root` buiten de repo; de KPI telt een worktree onder die `root`.
- [ ] **Stap 2: draai** `go test ./internal/hooks ./internal/worktreeaudit` → de audit-tests FAIL, hook-detectietests PASS (vastleggen in de commit).
- [ ] **Stap 3: implementeer** in `worktreeaudit`: `worktreeDir`-const weg, `root` uit config opgelost met `Resolve`; `render.go` vergelijkt tegen de opgeloste map. Hookberichten: lees `worktree.root` zoals `internal/hooks/hooks.go:31-37` andere sleutels leest.
- [ ] **Stap 4: draai** dezelfde tests → PASS; draai ook de bestaande fixtures (`tests/pack/fixtures/worktree_audit/*.json`); pas alleen aan wat de standaard betreft als het gedrag per ongeluk veranderde.
- [ ] **Stap 5: commit** `feat(worktree): hooks en audit herkennen een root buiten de repo (#496)`.

### Taak 3: Pack-tekst gebruikt de ingestelde plek

**Bestanden:**
- Wijzig: `pack/core/instructions/AGENTS.md:3`; skills `fix-pr`, `close-issue`, `upgrade`, `start-issue`, `review-plan`, `write-handoff`, `audit-worktrees` (kernpaden uit het impact-pack, `claude/` en `agents/` varianten) en `pack/go/skills/go-standard/{claude,agents}/SKILL.md:44`
- Wijzig: `internal/repopins/instructions_test.go:59`, `internal/packcontract/auditworktrees_test.go`, `docs/workplace.md`, `docs/getting-started.md`
- Test: nieuwe pin in `internal/repopins` of `internal/packcontract`

**Interfaces:**
- Consumeert: `worktree.root` (taak 1).

- [ ] **Stap 1: schrijf een falende pin-test**: geen `.claude/worktrees` in `pack/` behalve in een toegestane lijst (de ene standaardwaarde-zin in het managed block, en `internal/workplace/local` blijft buiten `pack/`). Assert met de exacte toegestane bestanden, zodat een nieuwe hardcode rood maakt.
- [ ] **Stap 2: draai** de pin → FAIL met de lijst bestanden.
- [ ] **Stap 3: herschrijf** de plekken: managed block en skills zeggen `ROOT=$(lighthouse config worktree.root)` (relatief → onder de repo-root) en maken/zoeken `<root>/<name>`; `git worktree add "<root>/<name>" -b <branch>` met aanhalingstekens (spaties). Het managed block noemt `.claude/worktrees` één keer als standaard. Pas `review-plan` en `write-handoff` aan: padreductie gaat uit van de opgeloste root, niet van de letterlijke map.
- [ ] **Stap 4: draai** `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie in dezelfde commit; daarna `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Stap 5: draai** `go test ./internal/repopins ./internal/packcontract ./internal/checkinstr/...` → PASS.
- [ ] **Stap 6: commit** `feat(pack): worktrees op de ingestelde plek, geen hardcoded pad (#496)`.

### Taak 4: Sync-map-waarschuwing in `init`

**Bestanden:**
- Wijzig: `internal/worktreeroot/worktreeroot.go` (nieuw), `internal/initcmd/initcmd.go` (`Run`, warnings in stijl van :489/:941)
- Test: `internal/worktreeroot/worktreeroot_test.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produceert: `func InSyncFolder(path string) (provider string, ok bool)` — herkent padsegmenten (hoofdletterongevoelig) `OneDrive` (ook `OneDrive - <naam>`), `Dropbox`, `iCloud Drive`, `iCloudDrive`, `Library/Mobile Documents/com~apple~CloudDocs`; scheidingsteken `/` en `\`.
- Consumeert: `Resolve`.

- [ ] **Stap 1: schrijf falende tests**: tabel voor `InSyncFolder` (o.a. `C:\Users\A\OneDrive - Bedrijf\repo`, `/home/a/Dropbox/x`, `/Users/a/Library/Mobile Documents/com~apple~CloudDocs/x`, `/home/a/MyOneDriveNotes/x` → **niet**: alleen volledige segmenten). Init-test: repo of opgeloste `root` in een sync-map → `warning:` op `o.Err`, exitcode 0; geen sync-map → geen waarschuwing.
- [ ] **Stap 2: draai** → FAIL.
- [ ] **Stap 3: implementeer** `InSyncFolder` en roep het in `initcmd.Run` aan voor de repo-root en de opgeloste `worktree.root`; waarschuwing in het Engels, blokkeert niet, noemt de provider en wijst op `[worktree] root`.
- [ ] **Stap 4: draai** `go test ./internal/worktreeroot ./internal/initcmd ./internal/cli` → PASS.
- [ ] **Stap 5: commit** `feat(init): waarschuw voor een repo of worktree-root in een sync-map (#496)`.

### Taak 5: Windows-CI, docs en changelog

**Bestanden:**
- Wijzig: `ci/windows-measure.sh`, `.github/workflows/windows-measure.yml` (alleen als nodig)
- Wijzig: `docs/workplace.md`, `docs/getting-started.md`, `docs/site/data/config.json` (beschrijving van de sleutel)
- Maak: `changelog.d/issue-496-worktree-root.md`

- [ ] **Stap 1:** voeg aan `ci/windows-measure.sh` een stap toe: maak een tijdelijke repo, een `root` buiten de repo in een pad **met spaties**, `git worktree add` daarheen, en laat `worktree-check` en `main-commit-gate` daar draaien; verwacht exit 0 respectievelijk de blokkade op de hoofdcheckout. Houd bestaande aanroepen (`WORK=$2`) intact.
- [ ] **Stap 2:** documenteer `[worktree] root`, de standaard, de relatieve/absolute regel en de sync-waarschuwing in `docs/getting-started.md`; corrigeer de verwijzingen in `docs/workplace.md`.
- [ ] **Stap 3:** schrijf het changelogfragment in het Nederlands.
- [ ] **Stap 4: eindcontrole:** `go test ./...`, `lighthouse imports check`, `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Stap 5: commit** `docs(worktree): [worktree] root, sync-waarschuwing en Windows-CI (#496)`.

## Buiten scope

`internal/workplace/local` (`excludeLine` in `info/exclude`) en `internal/kpi/startissue.go` (`worktreeMarker`) zijn adapter- respectievelijk KPI-code met eigen conventie; eventueel vervolgissue als de standaard daar knelt. `internal/dispatch/testdata` blijft ongewijzigd.

## Self-review

- AC 1 → taak 1; AC 2 → taak 3; AC 3 → taak 2; AC 4 → taak 4; AC 5 → taken 2 en 5 (root buiten de repo, spaties, Windows-CI).
- Typeconsistentie: `Resolve(repoRoot, value string) string`, `Default`, `InSyncFolder(path string) (string, bool)` worden in taken 2–4 gelijk gebruikt.
- Open risico: de skill-tekst voor `write-handoff`/`review-plan` bevat voorbeeldpaden; de uitvoerder controleert elke `.claude/worktrees`-treffer met `git grep` na taak 3.
