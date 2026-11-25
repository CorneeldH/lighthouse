# Releasebeleid deel 2: workflows en `ci/changelog` — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dev-builds na elke releasebare merge, een releasebranch per afgeronde releasemilestone, een release-PR na 7 dagen soak, patch-releases voor kritieke fixes, en een sync terug naar `main`.

**Architecture:** Alle beslislogica staat in Go (`ci/changelog`, getest met tabeltests en tijdelijke git-repo's); de workflows zijn dun en roepen die subcommando's aan. Eén reconcile-workflow op `main` (cron, dispatch, milestone-event) kijkt naar de staat van elke `release/vX.Y` en doet wat die staat vraagt. Dat is het mechanisme van *Waar de logica draait*: de workflow en de `ci/changelog`-code komen altijd van `main`, de inhoud van de releasebranch wordt uitgecheckt in een tweede map.

**Tech Stack:** Go (`ci/changelog`), GitHub Actions, `gh`, de GitHub App uit `[ci]` in `lighthouse.toml`.

**Spec:** [2026-11-24-issue-582-releasebeleid-design.md](../specs/2026-11-24-issue-582-releasebeleid-design.md), sectie *Stroom*, *Waar de logica draait*, *Kritieke fix*, *Wat verdwijnt*, *macOS-gate*. Dit is plan 2 van 2; plan 1 is [2026-11-24-issue-582-releasebeleid-kanaal.md](2026-11-24-issue-582-releasebeleid-kanaal.md) en levert `internal/selfupdate.ParseTag` voor de tagvorm. Plan 2 kan beginnen zodra de tagvorm vastligt; het hangt niet van plan 1 af.

## Global Constraints

- Soak: `7` dagen, een constante in de workflow, niet in `lighthouse.toml`.
- Dev-tag `vX.Y.Z-dev.N`, rc-tag `vX.Y.Z-rc.N`, handmatige testbuild `vX.Y.Z-test.N`; `N` = hoogste bestaande `N` voor die `X.Y.Z` en dat achtervoegsel plus één, berekend binnen één concurrency-groep.
- `X.Y.Z` van een dev-tag: de minor na de hoogste `release/vX.Y`-branch, of na de laatste release als er geen open releasebranch is. Een releasebranch is open zolang er geen `vX.Y.0`-tag voor bestaat.
- Een releasemilestone heeft de regel `Release: ja` in de beschrijving; de sluiting van een andere milestone doet niets.
- Releasebare fragmenttypes: `added`, `changed`, `fixed`, `removed`. `internal` start nooit een build.
- Commit van een release: `chore(release): vX.Y.Z`; de enige directe push van de bot is de sync naar `main` en de tags.
- De release-PR opent met de GitHub App uit `[ci]` (`bot_client_id_var`, `bot_private_key_secret`), anders start CI niet.
- Een tag maakt altijd een workflow. `build.yml` blijft ongewijzigd; `dist`-commit (C2) via `ci/tag-with-ui.sh`.
- Een release landt nooit werk dat ná de afsplitsing op `main` kwam; de milestone-release weigert de tag als de ouder van de releasecommit niet de ouder van de `dist`-commit van de nieuwste rc is (`vX.Y.0-rc.N^`).
- Acties in workflows staan op SHA gepind, zoals in de bestaande bestanden. Een wijziging aan `.github/workflows/` wordt door de workflow-pusher gepusht (`workflow-push-gate`), niet door deze run.
- Code in het Engels, fragment in het Engels. Voor elke commit: `[test_gate] command` en `lighthouse imports check`.

## Review Focus

- Twee dev-merges kort na elkaar geven geen botsende of dubbele tag — Task 3 (nummering) en Task 7 (concurrency-groep).
- Een fragment dat al in een release uit ging, komt niet in een latere release-PR, ook niet bij een andere merge-volgorde — Task 2.
- Een nieuwe rc terwijl de release-PR open staat sluit die PR; een door de maintainer gesloten PR komt pas terug na een nieuwe rc — Task 5.
- Een branch waarvan de tip een `chore(release)`-commit zonder tag is, krijgt geen tweede release-PR maar een herstel (stap 5 en 6 opnieuw) — Task 5 en Task 8.
- De sync is idempotent: een tweede run op een `main` die al bij is, wijzigt niets en faalt niet — Task 4.
- Een milestone zonder `Release: ja`, of zonder releasebaar fragment, splitst niets af en de run meldt waarom — Task 6.

---

### Task 1: `ci/changelog release` met expliciete versie

**Files:**
- Modify: `ci/changelog/changelog.go`, `ci/changelog/main.go`, `ci/changelog/changelog_test.go`, `ci/changelog/main_test.go`, `ci/changelog/release_fixture_test.go` en de `meta`-bestanden onder `ci/changelog/testdata/releases/*`

**Interfaces:**
- Produces: `release --version vX.Y.Z [--date D] [--root R]` schrijft de sectie en verwijdert de open fragmenten; het drukt de tag af. `--event` en `--bump` vervallen, evenals `decideBump` en de `push`/`schedule`-tak. Zonder releasebaar fragment: exit 1 met `nothing to release`. Een versie die al een tag heeft: exit 1. Signatuur: `func release(root, version, date string, tags []string) (string, error)`.

- [ ] **Step 1: Write the failing tests**: `TestReleaseUsesGivenVersion` (fragmenten `added` en `fixed`, `--version v0.30.0` geeft die sectie, ook als de hoogste tag `v0.27.0` is), `TestReleaseRefusesExistingTag`, `TestReleaseNothingReleasable` (alleen `internal`: exit 1), `TestReleaseRejectsMalformedVersion` (`0.30`, `v0.30.0-rc.1`: exit 2 usage). Pas de fixtures aan: `meta` noemt de versie.
- [ ] **Step 2: Run** `go test ./ci/changelog -v` — Expected: de nieuwe tests FAIL.
- [ ] **Step 3: Implement** de nieuwe signatuur; verwijder `decideBump`, `nextVersion`, `latestTag` en de `event`- en `bump`-vlaggen.
- [ ] **Step 4: Run** `go test ./ci/changelog -v` — Expected: PASS.
- [ ] **Step 5: Commit** `refactor(changelog): release met expliciete versie (#582)`.

### Task 2: Vrijgegeven fragmenten uit git

**Files:**
- Create: `ci/changelog/released.go`, `ci/changelog/released_test.go`
- Modify: `ci/changelog/changelog.go` (`release` slaat vrijgegeven fragmenten over), `ci/changelog/main.go`

**Interfaces:**
- Produces: `func releasedFragments(root string) (map[string]bool, error)` — de bestandsnamen (basename onder `changelog.d/`) die een commit `chore(release): vX.Y.Z` verwijderde, bereikbaar vanaf een `vX.Y.Z`-tag over alle `release/*`-branches en `main`. Subcommando `changelog released --root R` drukt ze af, één per regel, gesorteerd. `release` verwijdert een open fragment dat in die set staat zonder het in de sectie te zetten, en meldt het op stderr.

- [ ] **Step 1: Write the failing tests** met een tijdelijke git-repo (`gitenv_test.go` zet de omgeving): `TestReleasedFragmentsFromReleaseCommit`, `TestReleasedFragmentIsNotRereleased` (fragment `a.md` met een patch uit op de releasebranch, nog open op `main`: de volgende release laat `a.md` buiten de sectie), `TestReleasedFragmentsIgnoresUntaggedReleaseCommit` (een `chore(release)`-commit zonder tag telt niet), `TestReleasedFragmentsOrderIndependent` (de merges in omgekeerde volgorde geven dezelfde set).
- [ ] **Step 2: Run** `go test ./ci/changelog -run Released -v` — Expected: FAIL.
- [ ] **Step 3: Implement** met `git log --diff-filter=D --name-only` per `vX.Y.Z`-tag over het bereik `<tag>^..<tag>`; de bron is git, nooit `CHANGELOG.md`.
- [ ] **Step 4: Run** `go test ./ci/changelog -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(changelog): vrijgegeven fragmenten uit git afleiden (#582)`.

### Task 3: Volgende versie en tagnummer

**Files:**
- Create: `ci/changelog/nextver.go`, `ci/changelog/nextver_test.go`
- Modify: `ci/changelog/main.go`

**Interfaces:**
- Produces: `changelog next --kind dev|rc|test|minor|major|patch [--branch release/vX.Y] [--root R]` drukt de tag af. Semantiek: `dev` = `vX.Y.Z-dev.N` met `X.Y.Z` volgens de Global Constraints; `rc` (met `--branch`) = `vX.Y.0-rc.N` voor de branch; `test` = `vX.Y.Z-test.N` met dezelfde `X.Y.Z` als `dev`; `minor`, `major` = de versie van een nieuwe afsplitsing (`major` alleen via dispatch); `patch` (met `--branch`) = de volgende patch op die lijn. Signatuur: `func nextTag(kind string, branch string, tags, branches []string) (string, error)`.

- [ ] **Step 1: Write the failing tests** (tabel): na `v0.27.0` geeft `dev` `v0.28.0-dev.1`; met `-dev.1` en `-dev.2` bestaand `-dev.3`; met branch `release/v0.28` (geen `v0.28.0`-tag) geeft `dev` `v0.29.0-dev.1`; `-dev.10` is hoger dan `-dev.9`; `rc` voor `release/v0.28` met `v0.28.0-rc.1` geeft `-rc.2`; `test` raakt de rc-nummering niet; `patch` voor `release/v0.28` met `v0.28.0` en `v0.28.1` geeft `v0.28.2`; `rc` voor een branch met al een `vX.Y.0`-tag is een fout.
- [ ] **Step 2: Run** `go test ./ci/changelog -run Next -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `nextTag` en het subcommando; sorteer numeriek, niet tekstueel.
- [ ] **Step 4: Run** `go test ./ci/changelog -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(changelog): volgende dev-, rc- en patchtag berekenen (#582)`.

### Task 4: Sync naar `main`

**Files:**
- Create: `ci/changelog/sync.go`, `ci/changelog/sync_test.go`
- Modify: `ci/changelog/main.go`

**Interfaces:**
- Produces: `changelog sync --tag vX.Y.Z --source <dir> [--root R]`. `--source` is een checkout van de tag; `--root` is `main`. Het zet de sectie van `vX.Y.Z` uit `<source>/CHANGELOG.md` in `<root>/CHANGELOG.md` op de plek die `insertSection` kiest, en verwijdert in `<root>/changelog.d/` de fragmenten met dezelfde naam als die de releasecommit van de tag verwijderde. Staat de sectie er al en zijn de fragmenten weg: exit 0, geen wijziging. Fragmenten die niet in die set zitten, blijven.

- [ ] **Step 1: Write the failing tests**: `TestSyncAddsSectionAndRemovesFragments`, `TestSyncIsIdempotent` (tweede run: byte-gelijk, exit 0), `TestSyncKeepsFragmentsForNextMilestone`, `TestSyncPatchSectionOrder` (een `v0.28.1`-sectie komt boven `v0.28.0` en onder een al aanwezige `v0.29.0`).
- [ ] **Step 2: Run** `go test ./ci/changelog -run Sync -v` — Expected: FAIL.
- [ ] **Step 3: Implement** met `extractSection`, `insertSection` en de verwijderde namen uit `releasedFragments` voor die ene tag.
- [ ] **Step 4: Run** `go test ./ci/changelog -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(changelog): sync van een release naar main (#582)`.

### Task 5: Reconcile-beslissing per releasebranch

**Files:**
- Create: `ci/changelog/reconcile.go`, `ci/changelog/reconcile_test.go`
- Modify: `ci/changelog/main.go`

**Interfaces:**
- Produces: `type BranchState struct { Branch, Tip, TipSubject string; LatestRC string; RCCommit string; RCTime time.Time; Released bool; ReleaseTag string; ReleasePROpen bool; ReleasePRClosedByHand bool; NewCommitsSinceLatestTag bool }` en `func decide(s BranchState, now time.Time) Action` met `Action` uit `none`, `tag-rc`, `open-release-pr`, `close-release-pr`, `open-patch-pr`, `release-tip` (tip is een `chore(release)`-commit zonder tag), `recover`. Subcommando `changelog reconcile --branch B --now T --pr-open BOOL --pr-closed-by-hand BOOL` leest de rest uit git en drukt de actie af.
- Regels: een tip die geen releasecommit is en nieuwer dan de nieuwste rc op een open branch ⇒ `tag-rc` (en `close-release-pr` als er een PR open staat); een nieuwste rc van ≥ 7 dagen, geen PR open, niet door de hand gesloten ⇒ `open-release-pr`; een door de hand gesloten PR ⇒ `none` tot er een nieuwe rc is; een gereleasede branch met een nieuwe commit na `vX.Y.Z` en geen PR ⇒ `open-patch-pr`; tip `chore(release)` zonder tag ⇒ `release-tip`.

- [ ] **Step 1: Write the failing tests** (tabel met alle regels hierboven, plus `TestDecideRCSixDaysWaits`, `TestDecideRCSevenDaysOpensPR`, `TestDecideClosedByHandWaitsForNewRC`, `TestDecideUntaggedReleaseCommitNeverOpensSecondPR`).
- [ ] **Step 2: Run** `go test ./ci/changelog -run Decide -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `decide` als pure functie; de git-lezing in een aparte functie `loadState`.
- [ ] **Step 4: Run** `go test ./ci/changelog -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(changelog): beslissing per releasebranch (#582)`.

### Task 6: Afsplitsen

**Files:**
- Create: `ci/changelog/split.go`, `ci/changelog/split_test.go`
- Modify: `ci/changelog/main.go`

**Interfaces:**
- Produces: `changelog split --kind minor|major --root R` beslist of er afgesplitst wordt en drukt `split <branch> <rc-tag>`, `merge <branch> <rc-tag>` (vorige rc nog niet gereleased: `main` in de bestaande branch mergen, tag `rc.N+1`) of `skip <reden>`. Exit 0 bij alle drie, exit 1 bij een verdict dat de run rood moet maken (merge-conflict wordt door de workflow gemeld, niet hier). `skip` bij: geen releasebaar fragment dat nog niet in een releasebranch zit, of bij `merge` geen nieuwe releasebare fragmenten op `main`. Daarnaast `changelog milestone-release --description <tekst>` drukt `yes` of `no` (regel `Release: ja`, hoofdletterongevoelig, op een eigen regel).

- [ ] **Step 1: Write the failing tests**: `TestSplitFirstTime`, `TestSplitSkipsWithoutReleasableFragment`, `TestSplitWhilePreviousRCOpenMerges`, `TestSplitWhilePreviousRCOpenWithoutNewFragmentsSkips`, `TestMilestoneReleaseMarker` (`Release: ja` yes; `release: JA` yes; `Release: nee`, lege tekst, `Release: ja, misschien` no).
- [ ] **Step 2: Run** `go test ./ci/changelog -run 'Split|Milestone' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** met `next --kind` (Task 3) en `releasedFragments` (Task 2) voor "nog niet in een releasebranch".
- [ ] **Step 4: Run** `go test ./ci/changelog -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(changelog): afsplitsen van een releasebranch (#582)`.

### Task 7: Dev-build en `prerelease.yml` als `workflow_call`

**Files:**
- Modify: `.github/workflows/prerelease.yml` (trigger `workflow_call` met input `tag`, naast `workflow_dispatch`; een dispatch zonder tag berekent `vX.Y.Z-test.N` met `changelog next --kind test`), `internal/repopins/release_test.go` (`TestPrereleaseTriggerAndInput`, `TestPrereleaseRejectsNonPrereleaseTags`, `TestPrereleaseTagJobPushesOnlyTheTag`)
- Create: `.github/workflows/dev-build.yml`, test `internal/repopins/devbuild_test.go`

**Interfaces:**
- Consumes: `changelog next --kind dev`, `changelog released`.
- Produces: `dev-build.yml` met trigger `push: branches: [main]` en concurrency-groep `dev-build` (`cancel-in-progress: false`). Een job `decide` stopt zonder build als de pushed commit geen fragment van een releasebaar type toevoegt (`git diff-tree` op `changelog.d/`, type uit de front matter); anders roept de job `build` `prerelease.yml` aan met de tag van `next --kind dev`, binnen dezelfde groep.

- [ ] **Step 1: Write the failing tests** in `devbuild_test.go` (zelfde stijl als `release_test.go`: lees de YAML en pin): `TestDevBuildTriggersOnMainPushOnly`, `TestDevBuildSharesOneConcurrencyGroup` (groep `dev-build`, geen cancel), `TestDevBuildSkipsInternalOnly`, `TestDevBuildCallsPrerelease`; pas de prerelease-pins aan op de nieuwe triggers en op `-test.N`.
- [ ] **Step 2: Run** `go test ./internal/repopins -run 'DevBuild|Prerelease' -v` — Expected: FAIL.
- [ ] **Step 3: Write** de workflows; geen `${{ inputs.* }}` in `run:`-scripts (zie `TestNoInputsInRunScripts`). Een tag met het `-test`-achtervoegsel mag de handmatige dispatch maken; `-dev` en `-rc` alleen via `workflow_call`.
- [ ] **Step 4: Run** `go test ./internal/repopins -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(release): dev-build na elke releasebare merge (#582)`. Het pushen van de workflowbestanden doet de workflow-pusher.

### Task 8: Release-trein: afsplitsen, reconcile, release-PR, release en sync

**Files:**
- Create: `.github/workflows/release-train.yml`, test `internal/repopins/releasetrain_test.go`
- Modify: `.github/workflows/release.yml` (wordt `workflow_call` en `workflow_dispatch` "release" met input `branch`; verliest `push` en `schedule`), `internal/repopins/release_test.go` (verwijder pins op `push`, `schedule` en `decideBump`: `TestReleaseTriggersAndConcurrency`, `TestReleaseOnlyReleasesTheTestedCommit`, `TestOvertakenDispatchOrRerunFails`), `.github/workflows/macos.yml` (kop: ook PR's naar `release/*`)

**Interfaces:**
- Consumes: `changelog split`, `changelog reconcile`, `changelog release --version`, `changelog sync`, `changelog next`, `changelog milestone-release`.
- Produces: `release-train.yml` met triggers `milestone: [closed]`, cron elk uur, `workflow_dispatch` (input `action`: `split`, `reconcile`, `release`; input `kind`: `minor`/`major`; input `branch`). Jobs: `split` (milestone met `Release: ja`, of dispatch), `reconcile` (matrix per open `release/vX.Y` en per gereleasede branch met nieuwe commit; voert de actie van `changelog reconcile` uit), `release` (roept `release.yml` aan met de branch). Checkout van de logica is `main` in `./logic`, die van de inhoud de branch in `./branch`. De release-PR en de patch-PR openen met de App-token; titel `chore(release): vX.Y.Z`, body met de fragmentlijst en de rc-datum (tekst door de bouwer). `release.yml` doet na de merge van een release-PR: macOS-gate op de gemergde commit, de ouder-controle van de Global Constraints bij `vX.Y.0`, `dist`-commit via `ci/tag-with-ui.sh`, tag, `build.yml`, dan `changelog sync` met een PR-loze push naar `main` die bij een weigering van de push alleen die stap herhaalt.

- [ ] **Step 1: Write the failing tests** in `releasetrain_test.go`: `TestTrainTriggers` (geen `push`-trigger op `release/**`), `TestTrainChecksOutLogicFromMain`, `TestTrainOpensPRsWithBotToken`, `TestTrainMilestoneJobOnlyOnClosed`, `TestReleaseHasNoPushOrScheduleTrigger`, `TestReleaseRefusesWhenParentIsNotTheRCSource`, `TestReleaseSyncIsSeparateStep` (de sync-stap staat ná de tag en heeft eigen retry), `TestReleaseRecoveryDispatchTakesBranch`.
- [ ] **Step 2: Run** `go test ./internal/repopins -run 'Train|Release' -v` — Expected: FAIL.
- [ ] **Step 3: Write** de workflows volgens de Interfaces; de macOS-gate is een stap, geen ruleset; een rode gate laat geen tag achter en de branch staat. Houd C1/C2 en de atomaire push van het bestaande `release.yml` als mechanisme voor de tag.
- [ ] **Step 4: Run** `go test ./internal/repopins -v` en `actionlint` als `[test_gate] command` dat draait — Expected: PASS.
- [ ] **Step 5: Commit** `feat(release): releasetrein met afsplitsing, release-PR en sync (#582)`. Het pushen van de workflowbestanden doet de workflow-pusher.

### Task 9: Documentatie, overgang en fragment

**Files:**
- Modify: `docs/releasing.md` (herschrijf *When a release happens* en *The macOS gate*; nieuwe secties *Channels*, *The flow*, *A critical fix*, *Recovering a step*), `docs/install.md` indien Task 6 van plan 1 iets mist
- Create: `changelog.d/issue-582-releasebeleid.md` (type `changed`)

- [ ] **Step 1: Write** de documentatie in het Engels volgens de spec: kanalen en tagvormen, de stroom van dev-build tot sync, de kritieke-fixroute (PR op `main`, cherry-pick-PR, patch-PR zonder wachttijd), de herstelstappen (*Re-run failed jobs*, dispatch `release` met `branch`, sync opnieuw), `Release: ja` op een milestone. Voeg de overgang toe: bij het actief worden staan de open fragmenten op `main` en worden de eerste dev-tag `vX.Y.Z-dev.1` na de laatste release; ze gaan mee in de eerste releasemilestone.
- [ ] **Step 2: Write** het fragment: "Releases now follow a finished milestone after seven days on the dev channel; a critical fix can ship on its own as a patch."
- [ ] **Step 3: Run** de testgate, `lighthouse imports check` en `go run ./ci/changelog check --base origin/main --head HEAD` — Expected: groen.
- [ ] **Step 4: Commit** `docs(releasing): releasebeleid per milestone met devkanaal (#582)`.
