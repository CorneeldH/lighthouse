# Labeltaxonomie als vlag van `lighthouse init` — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse init --labels` zet een labeltaxonomie, `[labels]` en `[paths] labels` in een repo, en `--create-labels` maakt de ontbrekende labels op GitHub aan (#95).

**Architecture:** Het sjabloon zit in `pack/core/templates/labels.md`, maar wordt geen unit. Het is een seed-once-bestand zoals `lighthouse.toml`: `init` schrijft het alleen als het nog niet bestaat, het manifest kent het niet, en `--upgrade` raakt het dus nooit. De sleutels gaan via de bestaande `AppendMissing`-route. Na het toepassen roept `init` het geïnstalleerde script `.lighthouse/scripts/create_labels.py` aan. Dat script is de enige plek met `gh` en vraagt niets.

**Tech Stack:** Go (cobra, `internal/initcmd`, `internal/packs`, `internal/projectconfig`), Python 3 stdlib plus `lighthouse_lib` (script), pytest, `gh api`/`gh label create`.

**Spec:** [`2026-11-07-m3-tweede-omgeving-design.md`](../specs/2026-11-07-m3-tweede-omgeving-design.md), §4, plus de acceptatiecriteria van #95.

## Global Constraints

- Geen nieuwe vraag op stdin. `Apply these changes? [y/N]` en `Write ~/.config/lighthouse/host.toml? [y/N]` (`internal/initcmd/host.go:57`) blijven de enige twee. In de route met één `y` leest de tweede nog steeds EOF als nee.
- `--create-labels` werkt alleen samen met `--labels`. De vlag is het akkoord: geen vraag, ook niet met `--yes`.
- Geen `gh` vanuit `internal/initcmd`. Alle `gh`-aanroepen staan in `pack/core/scripts/create_labels.py`.
- Zonder `--labels` gedraagt `init` zich byte voor byte als nu.
- Bestaande labels op de remote blijven staan: niet hernoemen, geen nieuwe kleur, geen `--force`.
- Een mislukte labelstap (geen `gh`, geen rechten, geen `python3`) laat `init` niet falen: rc blijft 0.
- `pack/**`, `internal/**` en `tests/**` zijn Engels (`code_language`). Het sjabloon is ook Engels.
- Dogfood: na de nieuwe script commit je `go run ./cmd/lighthouse init --upgrade` met de geïnstalleerde kopie. Daarna moet de opdracht `No changes.` geven.
- Een `changelog.d/issue-95-labeltaxonomie-init.md`-fragment volgens `docs/releasing.md`.

## Beslissingen die dit plan neemt

1. **Seed-once, geen unit.** Een beheerde unit die lokaal is aangepast, wordt bij `--upgrade` een Conflict (`internal/reconcile/reconcile.go:90-103`). Dat botst met "de repo vult de area-as zelf in". Het sjabloon gaat daarom in `Pack.LabelsTemplate` en niet in `Pack.Units`. Het manifest en `IsInstallPath` veranderen niet.
2. **Doelpad.** Staat `[paths] labels` al in `lighthouse.toml`, dan is dat het doel. Anders is het `docs/developer/labels.md`, naast de standaard `docs/developer/planning/…`. Een pad dat absoluut is, `..` bevat of niet `path.Clean` is, maakt `init` rood (rc 1), en er wordt niets geschreven.
3. **`[labels]`-waarden** zijn de standaarden van `pack/core/lib/config.py:131-136` (`blocked-issue`, `blocked-external`, `needs-shaping`, `needs-decision`, `needs-hands`, `needs-testing`), plus `area_prefix = "area:"`. Er komt geen `location_prefix`: die as is optioneel, en een skill meldt dan `skipped`.
4. **Tabelvorm.** Het script leest elke Markdown-tabel met de kolommen `Label` en `Color` (hoofdletters tellen niet). Een label staat tussen backticks, een kleur is zes hex-tekens, ook tussen backticks. Regels die niet met `|` beginnen, slaat het script over. Daardoor telt een voorbeeldrij in een HTML-commentaar niet mee.

## Review Focus

- **`--upgrade --labels` op een repo die het bestand al heeft** (deze repo zelf, met `docs/developer/labels.md` in het Nederlands): de eerste regel van de stdout is `No changes.`, en het bestand blijft onaangeroerd. Daarna volgen alleen de regels van de labelstap: `labels: all <n> present on <R>`, of de ontbrekende labels met hun `gh label create`-regels (AC 4). Tests: `TestUpgradeKeepsEditedTaxonomy` (Taak 2, met een no-op labelstap exact `No changes.\n`) en `TestLabelsDefaultSyncRunsInstalledScript` (Taak 4, echte core, labelregels na `No changes.\n`). `--upgrade` zonder `--labels` blijft exact `No changes.\n`; daar hangt de dogfood-check van CI van af.
- **Labelnamen verschillen alleen in hoofdletters** (`Needs-Hands` op de remote): GitHub vergelijkt zonder hoofdletters, dus het label telt als aanwezig. Een `create` zou daar falen. Test in Taak 3: `test_existing_label_case_insensitive`.
- **Een repo zonder `origin`/`[forge] repo`**: het script kan de remote niet lezen. Het drukt dan alle regels af, zonder `--repo`, en eindigt met rc 0. Test in Taak 3: `test_no_forge_repo_prints_all_lines`.
- **Een remote met meer dan 30 labels** (de standaardpagina van GitHub): zonder `--paginate` lijken bestaande labels te ontbreken. Test in Taak 3: `test_reads_labels_paginated` controleert dat `--paginate` in de `gh`-aanroep staat.
- **Een dry run met `--create-labels`**: bij `</dev/null` mag er niets op de remote gebeuren. Test in Taak 4: `TestDeclinedLabelsDoesNotSync`.

---

### Taak 1: Sjabloon in `pack/core` en de loader

**Files:**
- Create: `pack/core/templates/labels.md`
- Modify: `pack/SOURCES.toml` (entry voor het sjabloon)
- Modify: `internal/packs/packs.go:107-113` (`Pack`), `internal/packs/packs.go:310-314` (`templates`-case)
- Test: `internal/packs/packs_test.go`, `internal/initcmd/labels_test.go` (nieuw)

**Interfaces:**
- Produces: veld `Pack.LabelsTemplate []byte` (nil zonder `templates/labels.md`).

- [ ] **Stap 1: Schrijf `pack/core/templates/labels.md`** (Engels). Daarin komen:
  - een kop `# Labels`;
  - een sectie **Blockers** met de regel "at most one; the lowest rank wins", en de tabel `| Rank | Label | Color | Binds when |` met zes rijen in de rangorde van `docs/developer/labels.md:104-111`: `needs-shaping` `FBCA04`, `needs-decision` `FBCA04`, `blocked-issue` `D93F0B`, `blocked-external` `D93F0B`, `needs-hands` `FBCA04`, `needs-testing` `FBCA04`;
  - een sectie **Areas** (`area:` prefix): uitleg dat de repo die invult, de tabel `| Label | Color | Rule of thumb |` zonder rijen, en daaronder een voorbeeldrij in een HTML-commentaar: `` <!-- | `area:docs` | `C4B5FD` | documentation for users | --> ``.
  - De tekst is generiek: geen persoonsnamen (schrijf "the maintainer"). `ci/literal-grep.sh` weigert namen uit `ci/literal-denylist.txt` in `pack/`, zonder op hoofdletters te letten; draai `ci/literal-grep.sh` na het schrijven.
  - Voeg in `pack/SOURCES.toml` een entry toe: `[[file]]`, `path = "pack/core/templates/labels.md"`, `new = true`. `tests/test_sources.py:27-31` eist er een per bestand onder `pack/`; de testgate selecteert die test niet, dus draai `uv run pytest -q tests/test_sources.py`.

- [ ] **Stap 2: Schrijf de falende tests**
  - `internal/packs/packs_test.go`, test `TestLoadKeepsLabelsTemplateOutOfUnits`: `m := basePack(); m["templates/labels.md"] = file(...)` (`packs.Load` krijgt de root van één pakket, zonder `core/`) geeft `p.LabelsTemplate` gelijk aan de inhoud. Geen enkele `u.Path` in `p.Units` eindigt op `labels.md`.
  - Dezelfde file, test `TestLoadRejectsOtherTemplate`: `templates/x.md` geeft nog steeds de fout `templates/ holds only`.

- [ ] **Stap 3: Draai** `go test ./internal/packs/ -run 'LabelsTemplate|OtherTemplate'`. Verwacht: FAIL. Het veld bestaat niet, en `labels.md` geeft nu de fout `templates/ holds only`.

- [ ] **Stap 4: Implementeer.** In de `templates`-case: `rest == "labels.md"` zet `p.LabelsTemplate = data` en `return nil`. De foutmelding noemt beide toegestane namen.

- [ ] **Stap 5: Draai** `go test -race ./internal/packs/... && uv run pytest -q tests/test_sources.py && ci/literal-grep.sh`. Verwacht: PASS.

- [ ] **Stap 6: Commit** `feat(packs): sjabloon voor de labeltaxonomie in core`.

### Taak 2: `--labels` — sleutels en seed-once-bestand in de diff

**Files:**
- Modify: `internal/projectconfig/projectconfig.go` (na `CoreTables`, `:156-165`)
- Modify: `internal/initcmd/initcmd.go` (`Options` `:48-62`, `execute` `:144`, `configTables` `:453`, `buildPlan` `:295`)
- Modify: `internal/cli/init.go:40-44`
- Test: `internal/initcmd/labels_test.go`, `internal/projectconfig/projectconfig_test.go`, `internal/initcmd/initcmd_test.go:24-32` (`testPacks` krijgt `core/templates/labels.md`)

**Interfaces:**
- Consumes: `Pack.LabelsTemplate` (Taak 1).
- Produces:
  - `projectconfig.DefaultLabelsPath = "docs/developer/labels.md"`
  - `func LabelTables(path string) []Table` geeft `[paths] labels = "<path>"` en `[labels]` met de zes waarden uit Beslissing 3 plus `area_prefix = "area:"`.
  - `Options.Labels bool`, cobra-vlag `--labels` ("propose a label taxonomy, [labels] and [paths] labels").

- [ ] **Stap 1: Schrijf de falende tests** in `internal/initcmd/labels_test.go`, met de bestaande helpers `run`, `newRepo`, `read`, `exists`, `write`, `snapshot` en een nieuwe `labels := func(o *Options) { o.Labels = true }`:
  - `TestLabelsDryRunShowsTaxonomy`: `run(root, "", labels)` geeft rc 1. `r.out` bevat `docs/developer/labels.md`, `[labels]`, `blocked_issue = "blocked-issue"` en `labels = "docs/developer/labels.md"`. `snapshot` is gelijk aan die van voor de run.
  - `TestLabelsOneYesApplies`: `run(root, "y\n", all(labels, withTailscale("100.64.0.1")))` geeft rc 0. De file is gelijk aan het `testPacks`-sjabloon, `lighthouse.toml` bevat beide tabellen en `host.toml` is niet geschreven.
  - `TestNoLabelsFlagNoTaxonomy`: `initYes` geeft geen `docs/developer/labels.md`, en `lighthouse.toml` bevat geen `[labels]`.
  - `TestUpgradeKeepsEditedTaxonomy`: init met `--labels --yes`, overschrijf de file met `"# mine\n"`, en draai dan `--upgrade` met en zonder `labels`. Beide geven rc 0, `r.out == "No changes.\n"`, en de file blijft `"# mine\n"`.
  - `TestLabelsUsesConfiguredPath`: begin met `lighthouse.toml` met `[paths]\nlabels = "meta/labels.md"\n`. Na de run met `labels`, `yes` staat `meta/labels.md` er, en `docs/developer/labels.md` niet.
  - `TestLabelsRejectsPathOutsideRepo`: `labels = "../x.md"` geeft rc 1, `r.err` bevat `paths.labels`, en `snapshot` is onveranderd.
  - `TestLabelTemplateHasEveryConfiguredLabel` (AC 7): laad de echte core (`packs.Load` op `pack.Files`, sub `core`). Voor elke key in de `[labels]`-tabel van `projectconfig.LabelTables(projectconfig.DefaultLabelsPath)` waarvan de naam niet op `_prefix` eindigt, geldt dat het sjabloon `` "| `"+value+"` |" `` bevat. Faalt als je een rij uit het sjabloon haalt of een waarde in `LabelTables` wijzigt.
  - `internal/projectconfig/projectconfig_test.go`, test `TestLabelTables`: `LabelTables("a/b.md")` heeft precies de tabellen `paths` (1 key) en `labels` (7 keys), en de waarden staan gequote.

- [ ] **Stap 2: Draai** `go test ./internal/initcmd/ ./internal/projectconfig/ -run 'Label'`. Verwacht: FAIL (compileert niet).

- [ ] **Stap 3: Implementeer.**
  - `LabelTables` in `projectconfig`.
  - `configTables(root, chosen, labels bool)`: met `labels` wordt de core-lijst `CoreTables(...)` plus `LabelTables(path)`. Het pad komt uit de bestaande `lighthouse.toml` (`paths.labels`, via `toml.Decode`), anders uit `DefaultLabelsPath`. Zet de padcontrole uit Beslissing 2 hier. `mergeTables` blijft de controle op dubbele keys doen.
  - In `buildPlan`: is `o.Labels` gezet en bestaat het doelpad nog niet, dan `p.new = all["core"].LabelsTemplate`, met mode 0o644. Bestaat het al, dan blijft het onaangeroerd, met één regel op `o.Err`: `labels: <path> exists; left as it is`. Geef het pad en het sjabloon mee via de bestaande parameters of één nieuwe struct. Het bestand komt niet in het manifest.
  - Cobra-vlag in `internal/cli/init.go`.
  - `docs/site/data/config.json`: elk bestand onder `pack`, `internal` of `cmd` dat een sleutel noemt (`paths.labels`, `[paths] labels`, `labels.<naam>`, of de Go-tags `toml:"paths"` plus `toml:"labels"`), moet bij die sleutel onder `readers` staan (`tests/test_site_config.py:115-221`). Voeg `internal/initcmd/initcmd.go` (en `internal/projectconfig/projectconfig.go` als die een sleutel zo noemt) toe waar de test dat eist.

- [ ] **Stap 4: Draai** `go test -race ./internal/initcmd/... ./internal/packs/... ./internal/projectconfig/... ./internal/cli/... && uv run pytest -q tests/test_site_config.py`. Verwacht: PASS, ook `TestUpgradeRightAfterInitIsEmpty` en `TestLabelTemplateHasEveryConfiguredLabel`.

- [ ] **Stap 5: Commit** `feat(init): --labels zet taxonomie, [labels] en [paths] labels in de diff`.

### Taak 3: Script `create_labels.py`

**Files:**
- Create: `pack/core/scripts/create_labels.py`
- Modify: `pack/SOURCES.toml` (entry voor het script), `docs/site/data/config.json` (readers)
- Modify: `internal/packs/core_test.go:122-133` (scriptlijst)
- Test: `tests/pack/test_create_labels.py`
- Dogfood: `.lighthouse/scripts/create_labels.py` (via `go run ./cmd/lighthouse init --upgrade`)

**Interfaces:**
- Produces: de CLI `python3 .lighthouse/scripts/create_labels.py [--create]`, met cwd = de repo-root. Stdout is voor de gebruiker. rc 0 bij elke `gh`-uitkomst, rc 2 bij een ontbrekend of leeg taxonomiebestand of een kapotte config. Python-API voor tests: `parse_taxonomy(text: str) -> list[tuple[str, str]]` (naam, kleur in hoofdletters).

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_create_labels.py`. Gebruik de helpers van `tests/pack/_testkit.py`: `install_core(tmp_path, toml)` en `tool_bin` met een `gh`-stub die zijn argumenten naar een logfile schrijft. Voor `api` geeft de stub een vaste lijst namen. Draai het script met `subprocess.run`, met `cwd=root` en een env met `PATH=bin` en `LIGHTHOUSE_REPO_ROOT=str(root)`. `config_path()` (`pack/core/lib/config.py:253-277`) kijkt niet naar de cwd, en de autouse-fixture in `tests/pack/conftest.py` wijst `LIGHTHOUSE_REPO_ROOT` anders naar de gedeelde `INSTALL_ROOT`.
  - `test_parse_template`: `parse_taxonomy` op `pack/core/templates/labels.md` geeft de zes namen uit Beslissing 3, elk met een kleur die op `^[0-9A-F]{6}$` past. Er staat geen `area:docs` bij, want die rij staat in een commentaar.
  - `test_lists_missing_without_create`: de remote heeft `needs-hands`. De stdout heeft vijf regels `gh label create <name> --repo Owner/repo --color <C>` (zonder quotes: `shlex.quote` laat namen met alleen letters, cijfers en `-` ongemoeid). Het log heeft geen `label create`. rc 0.
  - `test_create_makes_only_missing`: met `--create` heeft het log precies vijf keer `label create`, geen voor `needs-hands`, en elke regel heeft `--color`. rc 0.
  - `test_existing_label_case_insensitive`: de remote heeft `Needs-Hands`. Er komt geen create voor `needs-hands`.
  - `test_reads_labels_paginated`: het log van de `api`-aanroep bevat `--paginate` en `repos/Owner/repo/labels`.
  - `test_gh_missing_prints_all_lines`: `tool_bin(..., drop="gh")`. De stdout bevat `could not read` en zes `gh label create`-regels. rc 0.
  - `test_create_failure_prints_line`: de stub geeft rc 1 op `label create`. De stdout bevat voor elk label de `gh label create`-regel. rc 0.
  - `test_no_forge_repo_prints_all_lines`: een toml zonder `[forge]` geeft zes regels zonder `--repo`. rc 0.
  - `test_missing_taxonomy_exits_2`: zonder het bestand op `paths.labels` geeft het script rc 2, en stderr noemt het pad.

- [ ] **Stap 2: Draai** `uv run pytest -q tests/pack/test_create_labels.py`. Verwacht: FAIL (het script bestaat niet).

- [ ] **Stap 3: Implementeer** `pack/core/scripts/create_labels.py`, stdlib plus `lighthouse_lib`. Zet de `sys.path`-regel zoals `pack/core/scripts/board_start.py:32-37`. De docstring legt de normen vast: geen vraag, geen `--force`, rc 0 bij elke `gh`-fout. De werking:
  - `root = Path(config_path()).parent`; het taxonomiebestand is `root / paths.labels`.
  - Remote lezen: `gh api repos/<R>/labels --paginate --jq '.[].name'`, met `stdin=DEVNULL` en `GH_PROMPT_DISABLED=1`. Bij `OSError` of rc ≠ 0 is de remote onbekend.
  - `missing` vergelijkt met `casefold`.
  - Een lege `missing` geeft `labels: all <n> present on <R>`.
  - Met `--create` en een bekende remote doet het script per label `gh label create <name> --repo <R> --color <C>`. Bij succes print het `labels: created <name>`. Bij een fout gaat het label naar de regels.
  - De rest: een kopregel (`labels: missing on <R>; create them with:` of `labels: could not read the labels of <R> (<reason>); create the missing ones with:`) en daarna één regel per label, met elk argument door `shlex.quote` (dus `gh label create blocked-issue --repo Owner/repo --color D93F0B`).
  - `pack/SOURCES.toml`: entry `path = "pack/core/scripts/create_labels.py"`, `new = true`.
  - `docs/site/data/config.json`: voeg `pack/core/scripts/create_labels.py` toe aan `readers` van elke sleutel die het script noemt (`paths.labels`, `forge.repo`, en eventueel andere), zodat `tests/test_site_config.py` groen blijft.

- [ ] **Stap 4:** Voeg `create_labels.py` toe aan de lijst in `internal/packs/core_test.go`. Draai `go run ./cmd/lighthouse init --upgrade` met één `y` en daarna nog eens met `</dev/null`. Verwacht bij de tweede run: `No changes.`.

- [ ] **Stap 5: Draai** `uv run pytest -q tests/pack/test_create_labels.py tests/test_sources.py tests/test_site_config.py && go test ./internal/packs/... && ci/literal-grep.sh`. Verwacht: PASS.

- [ ] **Stap 6: Commit** `feat(core): create_labels.py maakt ontbrekende labels aan of drukt de gh-regels af`, met de geïnstalleerde kopie.

### Taak 4: `--create-labels` en de labelstap na het toepassen

**Files:**
- Modify: `internal/initcmd/initcmd.go` (`Options`, `Run` `:76`, `execute` na `:218`)
- Create: `internal/initcmd/labelsync.go`
- Modify: `internal/cli/init.go`
- Test: `internal/initcmd/labels_test.go`

**Interfaces:**
- Consumes: de script-CLI uit Taak 3, `Options.Labels` uit Taak 2.
- Produces:
  - `Options.CreateLabels bool`, vlag `--create-labels` ("with --labels: create the missing labels on the remote; this flag is the approval").
  - `Options.SyncLabels func(root string, create bool, out, errw io.Writer) error`. nil betekent `runLabelScript`: `exec.Command("python3", ".lighthouse/scripts/create_labels.py"[, "--create"])` met `Dir=root`, `Stdin=nil`, env plus `GH_PROMPT_DISABLED=1` en stdout/stderr naar `out`/`errw`. Een rc ≠ 0 geeft een error.

- [ ] **Stap 1: Schrijf de falende tests:**
  - `TestCreateLabelsNeedsLabels`: alleen `CreateLabels` geeft rc 1. `r.err` bevat `--create-labels needs --labels`. `snapshot` is onveranderd.
  - `TestLabelsSyncRunsOnceAfterApply`: een fake `SyncLabels` noteert `(root, create)` en of `docs/developer/labels.md` op dat moment bestaat. Met `labels, yes`: één aanroep, `create == false`, en het bestand bestond. Met `labels, createLabels, yes`: `create == true`.
  - `TestDeclinedLabelsDoesNotSync`: `run(root, "", all(labels, createLabels))` geeft rc 1 en nul aanroepen.
  - `TestNoLabelsNoSync`: `initYes` geeft nul aanroepen.
  - `TestSyncFailureDoesNotFailInit`: de fake geeft een error. rc 0, en `r.err` bevat `warning: labels:`.
  - `TestLabelsAddsNoQuestion`: `run(root, "y\n", all(labels, createLabels, withTailscale("100.64.0.1")))` geeft `strings.Count(r.out, "[y/N]") == 2`, en `host.toml` is niet geschreven.
  - `TestLabelsSyncRunsOnUpgradeWithoutChanges`: init met `labels, yes`, daarna `upgrade, labels`. Er is één sync-aanroep, en `r.out` begint met `No changes.\n`.
  - `TestLabelsDefaultSyncRunsInstalledScript`: de echte core (`Packs: pack.Files`, zoals `TestInitInstallsRealCore`), `t.Setenv("PATH", <dir met een gh-stub die exit 1 geeft>:<PATH>)`, `labels, yes`. `r.out` bevat `gh label create blocked-issue`, en rc is 0. Daarna `upgrade, labels` op dezelfde repo: `r.out` begint met `No changes.\n` en bevat daarna `gh label create blocked-issue`. Sla de test over als `python3` niet op PATH staat.
  - `TestCreateLabelsRunsInstalledScriptWithCreate`: de echte core; begin met `lighthouse.toml` met `[forge]\nrepo = "Owner/repo"\n`. De `gh`-stub (een shellscript in `t.TempDir()`, vooraan op `PATH`) geeft op `api` de naam `needs-hands` met exit 0, en schrijft bij `label create` zijn argumenten naar een logfile met exit 0. Met `labels, createLabels, yes`: rc 0, en het log heeft precies vijf regels met `label create`, elk met `--repo Owner/repo`, geen voor `needs-hands`. Faalt als `runLabelScript` `--create` niet doorgeeft. Sla over zonder `python3`.
  - De tests uit Taak 2 die `labels` gebruiken, krijgen een no-op `SyncLabels` (helper `noSync := func(o *Options) { o.SyncLabels = func(string, bool, io.Writer, io.Writer) error { return nil } }`), zodat alleen de twee tests hierboven `python3` starten. `TestUpgradeKeepsEditedTaxonomy` houdt daarmee `r.out == "No changes.\n"`.

- [ ] **Stap 2: Draai** `go test ./internal/initcmd/ -run 'Labels|CreateLabels'`. Verwacht: FAIL.

- [ ] **Stap 3: Implementeer.**
  - De controle `CreateLabels && !Labels` staat bovenaan `execute`, voor `repoRoot`.
  - Na `warnIgnored` en vóór `offerHost`, en alleen met `o.Labels`: `o.SyncLabels(root, o.CreateLabels, o.Out, o.Err)`. Een error wordt `warning: labels: <err>` op `o.Err`.
  - De stap draait ook in de tak `No changes.`. Een geweigerde dry run komt er niet: die keert al terug met `errDeclined`.

- [ ] **Stap 4: Draai** `go test -race ./internal/initcmd/... ./internal/cli/...`. Verwacht: PASS.

- [ ] **Stap 5: Commit** `feat(init): --create-labels maakt na het toepassen de ontbrekende labels aan`.

### Taak 5: Documentatie en changelog

**Files:**
- Modify: `internal/cli/init.go:17-23` (`Long`)
- Modify: `docs/site/agents/install.md:160-190` (de keuzes in stap 2, de `printf`-route), `:246-273` (de foutenlijst)
- Create: `changelog.d/issue-95-labeltaxonomie-init.md`
- Check: `grep -rn -- '--no-pack' docs/site` voor andere plekken die de vlaggen van `init` opsommen, en werk die ook bij.

- [ ] **Stap 1:** `Long` krijgt twee regels: `--labels` stelt een taxonomie voor (de diff, één akkoord), en `--create-labels` maakt daarna de ontbrekende labels op de remote aan en vraagt niets.
- [ ] **Stap 2:** `install.md` (Engels) krijgt de keuze "label taxonomy" met de deelvraag "also create the labels on GitHub". De agent bouwt daaruit `lighthouse init --labels [--create-labels]`. De stdin blijft één `y`: `printf 'y\n' | lighthouse init --labels [--create-labels]`. Zonder `--create-labels` toont de agent de afgedrukte `gh label create`-regels aan de gebruiker. De foutenlijst krijgt `lighthouse init: --create-labels needs --labels`.
- [ ] **Stap 3:** Changelog-fragment met `type: added` (`ci/changelog.py:30` kent alleen `added`, `changed`, `fixed`, `removed`, `internal`), één of twee Engelse zinnen.
- [ ] **Stap 4: Draai** de volledige suites direct, niet via `[test_gate] command` (dat leunt op `$LIGHTHOUSE_GATE_TMP`, dat alleen de hook zet): `go test -race ./... && uv run pytest -q && python3 ci/changelog.py check && ci/literal-grep.sh`, en daarna `go run ./cmd/lighthouse init --upgrade </dev/null`. Verwacht: groen en `No changes.`.
- [ ] **Stap 5: Commit** `docs(init): --labels en --create-labels in help en agentgids`.
