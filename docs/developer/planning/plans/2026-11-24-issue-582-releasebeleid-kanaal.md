# Releasebeleid deel 1: kanaal, semver en upgrade-skill — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse self-update` kiest een kanaal (`release` of `dev`), de binary vergelijkt versies volgens semver, en `/lighthouse-upgrade` volgt het kanaal.

**Architecture:** Een klein tag-pakket in `internal/selfupdate` hergebruikt `pins.ParseVersion`/`Compare` voor `vX.Y.Z[-dev.N|-rc.N|-test.N]`. `selfupdate.Run` krijgt een kanaal; `release` leest `/releases/latest`, `dev` de lijst en kiest de hoogste. Het kanaal staat in een bestand naast `hostconfig.Path()`. De skill gaat over op `lighthouse version compare` en `lighthouse channel`.

**Tech Stack:** Go (cobra), `internal/pins`, `internal/forge`, de skill onder `pack/core/skills/upgrade/`.

**Spec:** [2026-11-24-issue-582-releasebeleid-design.md](../specs/2026-11-24-issue-582-releasebeleid-design.md), sectie *`self-update` en het kanaal* en *Gevolg voor repo's op dev*. Dit is plan 1 van 2; plan 2 is [2026-11-24-issue-582-releasebeleid-workflows.md](2026-11-24-issue-582-releasebeleid-workflows.md). Dit plan hangt alleen af van de vorm van de tags.

## Global Constraints

- Tags: `vX.Y.Z`, `vX.Y.Z-dev.N`, `vX.Y.Z-rc.N`, `vX.Y.Z-test.N`. Volgorde: `v0.28.0-dev.5 < v0.28.0-rc.1 < v0.28.0 < v0.29.0-dev.1`.
- Kanaalbestand: `~/.config/lighthouse/channel`, inhoud `release` of `dev`. Zonder bestand: `release`.
- `release` gebruikt `GET /releases/latest` (slaat prereleases over). `dev` gebruikt `GET /releases` en kiest de hoogste semver, prereleases inbegrepen, drafts niet.
- Geen downgrade: alleen vervangen als de doelversie strikt hoger is dan `Current`.
- Een binary op `v0.29.0-dev.3` is geen lokale `dev`-build; alleen de string `dev` wordt overgeslagen.
- Code, namen en commentaar in het Engels (`[project] code_language`). Fragment in `changelog.d/` in het Engels, type `added`.
- Een wijziging onder `pack/` vraagt `go run ./cmd/lighthouse init --upgrade` en een commit van de installed copy.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.

## Review Focus

- Een `-dev.10` is hoger dan `-dev.9` (numerieke, geen tekstuele vergelijking) — Task 1.
- Wisselen van `dev` naar `release` downgradet nooit: binary `v0.29.0-dev.3`, latest `v0.28.0` geeft `UpToDate` — Task 2.
- Een kapot of onbekend kanaalbestand (`stable`, leeg) geeft een duidelijke fout, geen stilzwijgende `release` — Task 3.
- `dev`-kanaal met een lijst zonder enige geldige tag, of alleen drafts: fout, geen panic of lege tag — Task 2.
- Een repo op `v0.29.0-rc.2` met een binary op `v0.29.0` krijgt geen "binary is older" — Task 5.

---

### Task 1: Tag-vergelijking

**Files:**
- Create: `internal/selfupdate/tag.go`
- Test: `internal/selfupdate/tag_test.go`
- Modify: `internal/selfupdate/selfupdate.go` (verwijder `tagRe`, `parseTag`, `newer`)

**Interfaces:**
- Produces: `func ParseTag(s string) (pins.Version, error)` — `vX.Y.Z` met optioneel `-dev.N`, `-rc.N` of `-test.N` (N is een getal zonder voorloopnul); een andere suffix of geen `v` is een fout. `func CompareTags(a, b string) (int, error)` geeft `-1`, `0`, `1`. `func HighestTag(tags []string) (string, error)` geeft de hoogste geldige tag; ongeldige tags worden overgeslagen; een lege uitkomst is een fout.

- [ ] **Step 1: Write the failing tests** in `tag_test.go`: `TestParseTagAcceptsChannelSuffixes` (`v0.28.0`, `v0.28.0-dev.1`, `v0.28.0-rc.2`, `v0.28.0-test.3` ok; `0.28.0`, `v0.28.0-beta.1`, `v0.28`, `v0.28.0-dev.01` fout), `TestCompareTagsOrder` (de keten uit Global Constraints, plus `v0.28.0-dev.10` > `v0.28.0-dev.9`), `TestHighestTagSkipsInvalid` (`[]string{"junk","v0.27.0","v0.28.0-dev.2"}` geeft `v0.28.0-dev.2`; `[]string{"junk"}` geeft een fout).
- [ ] **Step 2: Run** `go test ./internal/selfupdate -run 'Tag' -v` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement** de drie functies in `tag.go`: strip de `v`, valideer met een regexp op de drie suffixen, geef de rest aan `pins.ParseVersion`. Laat `selfupdate.go` nog `parseTag` gebruiken tot Task 2.
- [ ] **Step 4: Run** dezelfde test — Expected: PASS.
- [ ] **Step 5: Commit** `feat(selfupdate): tags met dev-, rc- en test-suffix vergelijken (#582)`.

### Task 2: Kanaal in `selfupdate.Run`

**Files:**
- Modify: `internal/selfupdate/selfupdate.go`, `internal/selfupdate/selfupdate_test.go`

**Interfaces:**
- Consumes: `ParseTag`, `CompareTags`, `HighestTag` (Task 1).
- Produces: `type Channel string` met `ChannelRelease = "release"` en `ChannelDev = "dev"`; `Options.Channel Channel` (leeg = `release`); `func ParseChannel(s string) (Channel, error)` (alleen `release` en `dev`, hoofdlettergevoelig).

- [ ] **Step 1: Write the failing tests** met de bestaande fake-server van `selfupdate_test.go`: `TestRunDevChannelPicksHighestIncludingPrerelease` (lijst met `v0.27.0`, `v0.28.0-rc.1`, `v0.28.0-dev.5`, Current `v0.27.0` geeft `Replaced` met `To == "v0.28.0-rc.1"`; de fake serveert `/releases` met `tag_name` en `draft`), `TestRunDevChannelIgnoresDrafts`, `TestRunReleaseChannelStaysOnLatest` (endpoint `/releases/latest`, niet `/releases`), `TestRunNeverDowngrades` (Current `v0.29.0-dev.3`, latest `v0.28.0` geeft `UpToDate`, `To == "v0.28.0"`), `TestRunDevBinaryTagIsNotSkipped` (Current `v0.29.0-dev.3` loopt door naar het netwerk; alleen `"dev"` geeft `SkippedDev`), `TestRunDevChannelWithoutValidTagFails`, `TestParseChannelRejectsUnknown` (`"stable"`, `""`, `"Dev"`).
- [ ] **Step 2: Run** `go test ./internal/selfupdate -v` — Expected: de nieuwe tests FAIL.
- [ ] **Step 3: Implement** `ParseChannel`, `Options.Channel` en de kanaalkeuze in `latestTag` (`dev`: `GET <APIBase>/repos/<Repo>/releases?per_page=100`, drafts weg, `HighestTag`). Vervang `parseTag`/`newer` in `Run` door `ParseTag`/`CompareTags`.
- [ ] **Step 4: Run** `go test ./internal/selfupdate -v` — Expected: PASS, ook de bestaande tests.
- [ ] **Step 5: Commit** `feat(selfupdate): kanaal release of dev kiezen (#582)`.

### Task 3: Bewaard kanaal en `--channel`

**Files:**
- Create: `internal/selfupdate/channelfile.go`, `internal/selfupdate/channelfile_test.go`
- Modify: `internal/cli/selfupdate.go`, `internal/cli/selfupdate_test.go`, `internal/cli/root.go` (registreer `channel`)

**Interfaces:**
- Consumes: `Channel`, `ParseChannel` (Task 2); `hostconfig.Path()` voor de map.
- Produces: `func ChannelPath() string` (naast `hostconfig.Path()`, bestandsnaam `channel`); `func ReadChannel(path string) (Channel, error)` (ontbrekend bestand geeft `ChannelRelease`, een onbekende inhoud een fout met het pad erin); `func WriteChannel(path string, c Channel) error` (maakt de map aan, mode 0644). CLI: `lighthouse self-update --channel release|dev` bewaart het kanaal en gebruikt het; zonder vlag het bewaarde; `lighthouse channel` drukt het af (`release` of `dev`, één regel).

- [ ] **Step 1: Write the failing tests**: `TestReadChannelMissingIsRelease`, `TestReadChannelRejectsGarbage` (inhoud `stable`: de fout noemt het pad), `TestWriteThenReadChannel` (in `t.TempDir()`); in `internal/cli`: `TestSelfUpdateChannelFlagIsStored` (draai met `--channel dev` onder een `HOME` in `t.TempDir()` en een dev-binary; daarna bestaat het bestand met `dev`), `TestSelfUpdateRejectsUnknownChannel` (exit 1, geen bestand), `TestChannelCommandPrintsStored`.
- [ ] **Step 2: Run** `go test ./internal/selfupdate ./internal/cli -run 'Channel' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** de drie functies en het cobra-commando `channel`; `self-update` bewaart pas ná een geldige `--channel`, en slaat dat op ook als de update zelf `SkippedDev` geeft.
- [ ] **Step 4: Run** dezelfde tests — Expected: PASS.
- [ ] **Step 5: Commit** `feat(cli): self-update --channel en lighthouse channel (#582)`.

### Task 4: `lighthouse version compare`

**Files:**
- Modify: `internal/cli/root.go` (het `version`-commando krijgt het subcommando `compare`), `internal/cli/root_test.go` of een nieuw `internal/cli/version_test.go`

**Interfaces:**
- Consumes: `selfupdate.CompareTags` (Task 1). Die eist een `v`; het commando accepteert `0.28.0` én `v0.28.0` en normaliseert.
- Produces: `lighthouse version compare <a> <b>` drukt `-1`, `0` of `1` af op stdout, exit 0; een ongeldige versie geeft exit 1 met de versie in de fout. `lighthouse version` zonder argumenten blijft `lighthouse <version>` afdrukken.

- [ ] **Step 1: Write the failing tests** in `version_test.go`: `TestVersionCompare` (tabel: `v0.28.0-rc.1 v0.28.0` geeft `-1`, `0.29.0 v0.29.0-rc.2` geeft `1`, gelijk geeft `0`), `TestVersionCompareRejectsInvalid` (`dev` en `x` geven exit 1), `TestVersionStillPrintsVersion`.
- [ ] **Step 2: Run** `go test ./internal/cli -run Version -v` — Expected: FAIL.
- [ ] **Step 3: Implement** het subcommando; laat `RunE` van `version` zelf staan en voeg `compare` als kind toe met `cobra.ExactArgs(2)`.
- [ ] **Step 4: Run** dezelfde tests — Expected: PASS.
- [ ] **Step 5: Commit** `feat(cli): lighthouse version compare (#582)`.

### Task 5: `/lighthouse-upgrade` volgt het kanaal

**Files:**
- Modify: `pack/core/skills/upgrade/claude/SKILL.md` (stap 1 en 2, regels rond 25–60), de `.agents`-variant als `pack/core/skills/upgrade/` er een heeft (check met `ls pack/core/skills/upgrade`), de contracttest `internal/packcontract/upgrade_test.go`
- Dogfood: `.claude/skills/lighthouse-upgrade/SKILL.md` en `.agents/skills/lighthouse-upgrade/SKILL.md` via `init --upgrade`

**Interfaces:**
- Consumes: `lighthouse version compare` (Task 4), `lighthouse channel` (Task 3).
- Produces: een skill waarin `sort -V` niet meer voorkomt.

- [ ] **Step 1: Write the failing test** in `internal/packcontract/upgrade_test.go`: `TestUpgradeSkillUsesVersionCompare` — de skill bevat `lighthouse version compare` en `lighthouse channel`, en bevat geen `sort -V`.
- [ ] **Step 2: Run** `go test ./internal/packcontract -run Upgrade -v` — Expected: FAIL.
- [ ] **Step 3: Edit the skill**: lees `C=$(lighthouse channel)`; kies `N` als de hoogste versie van de releases met `jq` (`prerelease` en `draft` alleen op `release` weg), vergelijk `M`, `B`, `N` met `lighthouse version compare`; op `dev` komen prereleases in de nieuwste versie en in de release notes. De regel "`M` > `B` ⇒ binary is older" gebruikt `compare` en blijft staan. `self-update` blijft zonder vlag.
- [ ] **Step 4: Run** `go test ./internal/packcontract -run Upgrade -v`, daarna `go run ./cmd/lighthouse init --upgrade` — Expected: PASS; de installed copies wijzigen.
- [ ] **Step 5: Commit** `feat(pack): /lighthouse-upgrade volgt het kanaal (#582)` met de pack-wijziging en de installed copies samen.

### Task 6: Documentatie en fragment

**Files:**
- Modify: `docs/install.md` (sectie *Upgrade automatically*, regels rond 134–154), `deploy/systemd/lighthouse-upgrade.service` (commentaar: de unit leest het bewaarde kanaal)
- Create: `changelog.d/issue-582-self-update-kanaal.md`

- [ ] **Step 1: Write** in `docs/install.md`: kies het kanaal één keer met `lighthouse self-update --channel dev`; de timer gebruikt het bewaarde kanaal; zonder keuze `release`. Pas de regel "Every release goes live here" aan voor dev.
- [ ] **Step 2: Write** het fragment, type `added`: "`lighthouse self-update --channel release|dev` chooses which releases the binary follows, `lighthouse channel` prints it, and `lighthouse version compare` orders two versions by semver."
- [ ] **Step 3: Run** de testgate en `go run ./ci/changelog check --base origin/main --head HEAD` — Expected: groen, exit 0.
- [ ] **Step 4: Commit** `docs(install): kanaal kiezen voor self-update en timer (#582)`.
