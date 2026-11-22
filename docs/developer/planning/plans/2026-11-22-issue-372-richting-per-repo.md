# Richting per repo — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lighthouse krijgt een citeerbare set richtingsdocumenten (strategie, uitgangspunten, een bestand per area), een Go-test die hun vorm bewaakt, en lezers in `write-issue`, `map-planned` en `close-issue` die ze gebruiken.

**Architecture:** Drie soorten Markdown-documenten onder `docs/developer/`, aangewezen door `[paths]` in `lighthouse.toml` (nieuwe sleutel `paths.strategy`). Een vormtest leest die paden en de area-tabel (`internal/labels.ParseTaxonomy`). De lezers blijven generiek in `pack/core`; `map-planned` krijgt `strategy` en `area_prefix` in zijn corpus en een poort die een gezet maar ontbrekend pad luid laat falen. Het go-domein van CI wordt uitgebreid zodat een PR die alleen de documenten raakt de test toch draait.

**Tech Stack:** Go 1.26, Markdown, TOML, `node --test` (config.json-test), skills in Markdown onder `pack/core`.

**Spec:** `docs/developer/planning/specs/2026-11-22-issue-372-richting-per-repo-design.md` (verslag: `docs/developer/planning/specs/2026-11-22-issue-372-richting-verslag.md`)

## Global Constraints

- Artefacten (documenten, commitberichten, changelog) in het Nederlands; code, namen, skills en Go-comments in het Engels (`[project] code_language = en`; `pack/**`, `internal/**`, `tests/**` zijn Engels).
- Geen pad en geen areanaam in `pack/core` of `internal/` (generiek; alleen sleutels uit `lighthouse.toml`).
- Zonder sleutel slaat elke lezer stil over; een sleutel die wél gezet is maar naar iets wijst dat niet bestaat is een configfout: `write-issue` zegt het in het gesprek, `map-planned` faalt luid in zijn poort.
- Documenten zonder status en zonder datums. Geen `## Nabij doel` in een lighthouse-area. Koppen zijn gesloten lijsten.
- Strategie-koppen, in volgorde: `## Diagnose`, `## Voor wie`, `## Leidend beleid`, `## Bewust niet`, `## Roadmap`. Area-koppen: `## Crux`, `## Leidend beleid`, `## Bewust niet`.
- ID's: `**S<n>**` (strategie), `**N<n>**` (area), uitgangspunt-kop `## <letter>. <X> boven <Y>` met precies één keer ` boven `, een regel *Bewijs* met minstens twee links (niet vervallen).
- Links in `.md` zijn relatief en wijzen naar een bestaand doel (`md-link-check`): vanuit `docs/developer/uitgangspunten.md` is het `planning/specs/…`, vanuit `docs/developer/areas/<area>.md` is het `../planning/specs/…`.
- Acht areas: `core`, `packs`, `init`, `server`, `dispatcher`, `usage`, `product`, `meta`.
- `pack/`-wijziging: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie (`.claude/skills/`, `.agents/skills/`, `.lighthouse/headings.toml`) in dezelfde commit; CI eist `No changes.`
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command`. Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (classificatiezin boven elke test: welke productiewijziging hem rood maakt).
- Commits als Conventional Commits, atomair, eindigend op `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- Gebouwd in een interactieve sessie; de PR blijft **draft** tot Corneel de inhoud goedkeurt. Geen merge.

## Review Focus

1. **Een uitgangspunt-kop met twee keer ` boven `** (bijvoorbeeld `## C. A boven B boven C`) of zonder: de test meldt `pad:regel`. Test in Taak 6.
2. **Een link in *Bewijs* die in de spec relatief was** (`2026-…-design.md`) maar in het document niet is herschreven, wijst nergens heen: `md-link-check` blokkeert. Gedekt door Taak 5 en 7 (commit-gate) en een test in Taak 6 die links op bestaan toetst.
3. **Een `→ <area>` dat een niet-bestaande area noemt** faalt de test. Test in Taak 6.
4. **Een PR die alleen `areas/core.md` breekt** draait de vormtest tóch in CI (stil groen). Test in Taak 8.
5. **`paths.areas` gezet, map ontbreekt** → `map-planned` faalt luid, geen skip. Test in Taak 3. En: milestone zonder `:` in de titel → `close-issue` stap 5 meldt `areas: skipped`, opent geen PR (contracttest in Taak 4).

---

### Taak 1: Sleutel `paths.strategy` in schema, repoconfig en site-config

**Files:**
- Modify: `internal/config/config.go` (de `paths.*`-rijen rond regel 82)
- Modify: `internal/repoconfig/repoconfig.go` (struct `Paths` regel ~50, ruwe `[paths]`-struct regel ~158, de conversie `Paths(raw.Paths)`)
- Modify: `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/defaults/expect.json`, `tests/pack/fixtures/config/full/expect.json`, `tests/pack/fixtures/config/full/lighthouse.toml`
- Modify: `docs/site/data/config.json`
- Test: `internal/repoconfig/repoconfig_test.go` (`TestLoadReadsPaths`)

**Interfaces:**
- Produces: `config` kent `paths.strategy` (`kStr`, geen standaard); `repoconfig.Paths{Labels, Tenets, Areas, Strategy string}`.

- [ ] **Step 1: Test rood.** Pas `TestLoadReadsPaths` aan: de toml-invoer krijgt `strategy = "docs/s.md"` en `want := Paths{Labels: "docs/developer/labels.md", Tenets: "docs/t.md", Areas: "docs/areas", Strategy: "docs/s.md"}`.
- [ ] **Step 2:** `go test ./internal/repoconfig -run TestLoadReadsPaths` → FAIL (veld `Strategy` bestaat niet).
- [ ] **Step 3:** Voeg `Strategy` toe aan `repoconfig.Paths` (ook de doc-comment) en aan de ruwe struct (`toml:"strategy"`). Voeg `{"paths.strategy", kStr}` toe naast `paths.areas` in `internal/config/config.go`.
- [ ] **Step 4:** Werk de fixtures bij: `config-schema.json` (schema- en sleutellijst, op de plek van de volgorde in `config.go`), `config/full/lighthouse.toml` (`strategy = "s"` onder `[paths]`), `config/full/expect.json` (een aanroepblok `paths.strategy` → `rc 0`, `stdout "s\n"`), `config/defaults/expect.json` (een blok `paths.strategy` → `rc 1`, stderr `config: paths.strategy is not set\n`, naar het model van `paths.tenets`).
- [ ] **Step 5:** Voeg in `docs/site/data/config.json`, in tabel `paths`, een sleutel toe: `key: "paths.strategy"`, `level: "advanced"`, `required: "no"`, `about: "The file with the project's strategy, read when writing an issue and when shaping a milestone."`, `example: "docs/strategy.md"`, `readers: ["internal/repoconfig/repoconfig.go"]` (de skill komt erbij in Taak 2). Zet de `about` van `paths.areas` op: `"The folder with one file per area: its crux, leading policy and deliberate no's, read when writing an issue. Closing a milestone updates its near goal when the area file has that heading."`. Niet in `UNREAD` van `docs/site/test/config.test.mjs`.
- [ ] **Step 6:** `go test ./internal/config ./internal/repoconfig` → PASS. `node --test docs/site/test/config.test.mjs` → PASS.
- [ ] **Step 7: Commit** `feat(config): paths.strategy`.

### Taak 2: `write-issue` leest de richting (pitch en 3f)

**Files:**
- Modify: `pack/core/skills/write-issue/claude/SKILL.md` (regel ~266 *Planned on the map*, regel ~394 *Description*) en `pack/core/skills/write-issue/agents/SKILL.md` (dezelfde twee plekken)
- Modify: `.claude/skills/…/write-issue`, `.agents/skills/…/write-issue` via `init --upgrade`
- Test: `internal/packcontract/writeissue_test.go`

**Interfaces:**
- Consumes: sleutel `paths.strategy` uit Taak 1 (`TestConfigKeysKnown` eist dat hij bestaat).
- Produces: skilltekst die `lighthouse config paths.strategy` noemt.

- [ ] **Step 1: Test rood.** In `writeissue_test.go`, een test `TestWriteIssueReadsDirection` (classificatiezin: faalt als de pitch of 3f stopt met `paths.areas`/`paths.strategy` te lezen of het ID niet meer noemt). Per variant: `section` van `#### 3f.` (of de kop die de *Planned on the map*-alinea draagt) bevat `` `lighthouse config paths.strategy` ``, `paths.tenets`, `paths.areas` en de zin "name the ID". De *Description*-bullet van de milestone bevat `paths.areas`, `paths.strategy`, de Engelse betekenisfrasen "diagnosis" en "deliberate no" (geen Nederlandse kopnamen in `pack/`; tot #500 vinden de lezers koppen op hun betekenis), en "doubt becomes a question in the conversation".
- [ ] **Step 2:** `go test ./internal/packcontract -run TestWriteIssueReadsDirection` → FAIL.
  - Pin ook, per variant, het onderscheid: een sleutel die niet gezet is (`lighthouse config` rc 1) slaat stil over, een gezet pad dat niet bestaat wordt in het gesprek gemeld ("does not exist"-achtige vaste formulering; kies één en pin die voor `paths.areas` én `paths.strategy`).
- [ ] **Step 3: Pitch (beide varianten).** Vervang de bullet "If the project has tenets … names the crux of its main area" door: met `lighthouse config paths.areas` (alleen als gezet) noemt de pitch onder *Problem / Opportunity* de crux van de area('s) waar de issues van de milestone het meest onder vallen (het `labels.area_prefix`-label van die issues); met `lighthouse config paths.strategy` toetst hij de milestone aan de diagnosis en de deliberate no's van de strategie en citeert `strategy/S<n>`. Twijfel wordt een vraag in het gesprek. Een gezet pad dat niet bestaat zegt de skill in het gesprek; hij slaat niet stil over.
- [ ] **Step 4: 3f (beide varianten).** Bij *Do we want it?*: lees naast `paths.tenets` en `paths.areas` ook `paths.strategy`, en noem bij de vraag het ID dat het oordeel draagt (`<area>/N<n>`, `strategy/S<n>`, `tenet <letter>`). Geen pad of areanaam in de tekst.
- [ ] **Step 5:** Voeg `pack/core/skills/write-issue` toe aan `readers` van `paths.strategy` in `docs/site/data/config.json`. `go run ./cmd/lighthouse init --upgrade`; `go test ./internal/packcontract` en `node --test "docs/site/test/*.test.mjs"` → PASS.
- [ ] **Step 6: Commit** `feat(write-issue): leest strategie, uitgangspunten en areas` (inclusief geïnstalleerde kopieën).

### Taak 3: `map-planned` leest de richting (corpus, poort, skill)

**Files:**
- Modify: `internal/scheduled/mapplanned/corpus.go` (struct `corpus`, `buildCorpus`)
- Modify: `internal/scheduled/mapplanned/mapplanned.go` (`Gate`)
- Modify: `pack/core/skills/map-planned/SKILL.md` (corpusschema regel ~29, regel ~37, stap 4 regel ~87)
- Modify: `docs/tick.md` (regel ~631, corpusbeschrijving)
- Test: `internal/scheduled/mapplanned/job_test.go` (`TestStageWritesTheCorpus`, nieuwe gate-test), `internal/packcontract/mapplanned_test.go`

**Interfaces:**
- Consumes: `repoconfig.Paths.Strategy`, `env.Repo.Labels.AreaPrefix` (veld bestaat in `repoconfig.Labels`).
- Produces: corpusvelden `"strategy"` (string) en `"area_prefix"` (string) in `.lighthouse/run/map-planned-corpus.json`.

- [ ] **Step 1: Tests rood.**
  - `TestStageWritesTheCorpus`: rig met `[paths] strategy = "docs/s.md"` en `[labels] area_prefix = "2-area:"`; assert `c["strategy"] == "docs/s.md"` en `c["area_prefix"] == "2-area:"`; zonder die sleutels zijn beide `""`.
  - `TestGateFailsOnMissingDirectionPath` (classificatiezin: faalt als `Gate` een gezet `paths.tenets`/`paths.areas`/`paths.strategy` dat op `origin/<default>` niet bestaat stil laat doorlopen): voor elk van de drie sleutels geeft `Gate` een error die de sleutelnaam en het pad bevat; zonder sleutel geen error.
  - `mapplanned_test.go`: `wantAll(t, f, … "`strategy`", "`area_prefix`")`; isoleer de stap-4-sectie en eis daar de driepadconditie (`tenets`, `areas` óf `strategy` niet leeg) en dat `area_prefix` het issue aan het area-bestand koppelt (classificatiezin: faalt als alleen het schema is bijgewerkt en de oude conditie `tenets or areas` blijft staan).
- [ ] **Step 2:** `go test ./internal/scheduled/mapplanned ./internal/packcontract -run 'Corpus|Direction|MapPlanned'` → FAIL.
- [ ] **Step 3:** `corpus` krijgt `Strategy string `json:"strategy"`` en `AreaPrefix string `json:"area_prefix"``; `buildCorpus` vult ze uit `env.Repo.Paths.Strategy` en `env.Repo.Labels.AreaPrefix`.
- [ ] **Step 4:** `Gate`: vóór de `rows`-check, voor elke gezette sleutel uit `{paths.tenets, paths.areas, paths.strategy}`: bestaat het pad op `origin/<branch>` (gebruik `ls-tree origin/<branch> -- <pad>` zoals `show()`, lege uitvoer = ontbreekt; geen `cat-file -e`, want de `git`-helper maakt van elke exit ≠ 0 een generieke fout zonder sleutel en pad), anders `return scheduled.Verdict{}, fmt.Errorf("%s = %q does not exist at origin/%s", key, path, branch)`. Een lege sleutel slaat over.
- [ ] **Step 5: Skill.** Corpusschema krijgt `"strategy": "…", "area_prefix": "…"`; de alinea bij `tenets` en `areas` zegt dat `strategy` ook een pad is en `area_prefix` het labelprefix waarmee een issue aan `<areas>/<area>.md` gekoppeld wordt zonder te gokken; stap 4 draait als `tenets`, `areas` of `strategy` niet leeg is; de `wish`-vraag noemt het ID dat het oordeel draagt. Vraagkop blijft de eigen kop van #476 (`🗺 Map · map-planned`), niet `🤖 Vraag`. Geen pad of areanaam in de tekst (`TestMapPlannedSkillNamesNoLabelOrPath` blijft groen).
- [ ] **Step 6:** `docs/tick.md` corpusbeschrijving: `strategy` en `area_prefix` erbij. `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Step 7:** Voeg `internal/scheduled/mapplanned/mapplanned.go` toe aan `readers` van `paths.tenets`, `paths.areas`, `paths.strategy` (en van een andere `paths.*`/`labels.*`-sleutel die het bestand noemt) in `docs/site/data/config.json`: `config.test.mjs` telt elk niet-testbestand onder `internal/` dat de sleutel noemt als lezer.
- [ ] **Step 7b:** `go test ./internal/scheduled/... ./internal/packcontract ./internal/repoconfig` en `node --test docs/site/test/config.test.mjs` → PASS.
- [ ] **Step 8: Commit** `feat(map-planned): corpus met strategy en area_prefix, poort op gezette paden`.

### Taak 4: `close-issue` stap 5 slaat over zonder area-bestand of kop

**Files:**
- Modify: `pack/core/headings.toml` (nieuwe sleutel), `.lighthouse/headings.toml` via `init --upgrade`
- Modify: `pack/core/skills/close-issue/claude/SKILL.md` en `agents/SKILL.md` (Step 5, regel ~839–857) + geïnstalleerde kopieën
- Test: `internal/packcontract/closeissue_test.go`

**Interfaces:**
- Produces: heading-sleutel `area_near_goal` (`nl = "Nabij doel"`, `en = "Near goal"`), te lezen met `lighthouse headings area_near_goal`.

- [ ] **Step 1: Test rood.** `TestCloseIssueStep5SkipsWithoutNearGoal` (classificatiezin: faalt als stap 5 niet meer vóór de `git worktree add` stopt bij een titel zonder `:`, een ontbrekend `<paths.areas>/<area>.md` of een ontbrekende kop). Per variant, `section(t, body, "### Step 5.", "## When NOT to close")`: `wantAll` op `lighthouse headings area_near_goal`, `areas: skipped — `, "lowercase", en per geval een eigen vaste formulering (geen `:` in de titel; geen bestand `<paths.areas>/<area>.md`; geen kop) — pin alle drie in beide varianten. `wantOrder` op een frase die alleen de nieuwe controle heeft (`lighthouse headings area_near_goal`) vóór `git worktree add`; `areas: skipped` staat er al in de oude tekst en bewijst niets. Plus een test dat `headings.toml` `area_near_goal` heeft met `nl = "Nabij doel"` en `en = "Near goal"` (naar het model van de bestaande headings-tests in `scout_test.go`).
- [ ] **Step 2:** `go test ./internal/packcontract -run 'CloseIssueStep5|Headings'` → FAIL.
- [ ] **Step 3:** Voeg `[area_near_goal]` toe aan `pack/core/headings.toml` (onder een kop `# Areas`).
- [ ] **Step 4:** Herschrijf Step 5 in beide varianten: na `paths.areas`-check, vóór de worktree: leid `<area>` af uit het deel vóór `:` na `project.milestone_prefix`; geen `:` in de titel, geen bestand `<paths.areas>/<area>.md` (area in kleine letters), of geen kop `lighthouse headings area_near_goal` in dat bestand → rapporteer `areas: skipped — <reden>` en stop. Rest van de stap ongewijzigd.
- [ ] **Step 5:** `go run ./cmd/lighthouse init --upgrade`; `go test ./internal/packcontract` → PASS.
- [ ] **Step 6: Commit** `feat(close-issue): stap 5 slaat over zonder nabij-doelkop`.

### Taak 5: De documenten

**Files:**
- Create: `docs/developer/strategie.md`, `docs/developer/uitgangspunten.md`, `docs/developer/areas/{core,packs,init,server,dispatcher,usage,product,meta}.md`
- Modify: `lighthouse.toml` (`[paths]`)

**Interfaces:**
- Produces: bestanden in de vorm van spec §1; sleutels `tenets`, `areas`, `strategy` in `lighthouse.toml`.

Geen test hier: Taak 6 toetst de vorm. Schrijf de inhoud nu, de test volgt direct (de commit-gate faalt pas als de test er is).

- [ ] **Step 1: `strategie.md`.** Vijf koppen in vaste volgorde. *Diagnose*: de alinea en vijf begrippen uit spec §2 letterlijk. *Voor wie*: Corneel; een bevriende ontwikkelaar; collega's bij CEDA; iedereen. *Leidend beleid* (3–5 bullets) en *Bewust niet* (`**S1**`…`**S9**`, de negen onderwerpen uit spec §2) uit het concept; *Roadmap*: één zin, geen lijst. Bij elke `S<n>` een citaat naar de spec of milestone die hem draagt (relatieve links).
- [ ] **Step 2: `uitgangspunten.md`.** Inleiding met de voorrangsregel; zeven koppen `## A. … boven …` t/m `## G. …` in volgorde van de tabel in spec §2; per uitgangspunt één zin *Waarom*, de extra zin bij A, C en G, en `*Bewijs*: [..](planning/specs/…), [..](…)` met de drie specs van de tabel (links herschreven naar `planning/specs/…`; controleer dat elk doel bestaat).
- [ ] **Step 3: area-bestanden.** Per area `## Crux` (conceptzin uit spec §2, uitgewerkt tot één alinea), `## Leidend beleid` (3–5 bullets) en `## Bewust niet` (`**N1**`…, met `→ <area>` waar het bij een buur hoort), alles met citaten (`../planning/specs/…`) uit de specs en milestones; zoek per area de beslissingen op. Dun bewijs (`usage`, `product`) staat niet in de crux (spec §2); alleen de PR meldt het.
- [ ] **Step 4:** Zet onder `[paths]` in `lighthouse.toml`: `tenets = "docs/developer/uitgangspunten.md"`, `areas = "docs/developer/areas"`, `strategy = "docs/developer/strategie.md"`.
- [ ] **Step 5:** `md-link-check` slaagt bij elke write (geen blokkade). Handmatig: `grep -c "^## " docs/developer/areas/*.md` geeft overal 3.
- [ ] **Step 6: Commit** `docs(richting): strategie, uitgangspunten en areas, sleutels in lighthouse.toml`. (Als de commit-gate wegens de nog ontbrekende test niets blokkeert; zo wel, doe Taak 6 eerst en commit samen.)

### Taak 6: De vormtest

**Files:**
- Create: `internal/direction/direction.go` (parser/controle, `package direction`) en `internal/direction/direction_test.go`; of, als de plaats in `internal/` niet past bij `lighthouse imports check`, `internal/packcontract/direction_test.go` (kies na `lighthouse imports check`; test hergebruikt `internal/labels.ParseTaxonomy`, geen tweede parser).
- Test: dezelfde pakketten, fixtures als strings in de test

**Interfaces:**
- Consumes: `internal/labels.ParseTaxonomy(text string) []labels.Label` (veld `Name`), `lighthouse.toml` via `internal/repoconfig.Load(dir)` (`Paths`, `Labels.AreaPrefix`).
- Produces: `func Check(root string) []string` — alle fouten tegelijk, elk als `pad:regel: melding`; leeg = goed. Plus `TestDirectionDocumentsHaveTheirShape` die `Check` op de repo-root draait en elke fout meldt.

- [ ] **Step 1: Tests rood** (tabel van fixtures in `t.TempDir()`, per geval één fout verwacht, in `direction_test.go`; classificatiezin boven elke test):
  - `TestCheckAreaFilePerLabel`: label zonder bestand en bestand zonder label → beide fouten.
  - `TestCheckAreaHeadings`: ontbrekende, verwisselde of extra `##`-kop (ook `## Nabij doel`) → fout met regel.
  - `TestCheckNotNowIDs`: bullet zonder ID, twee ID's, dubbel ID, vorm `**N1a**`, `→ <area>` die niet bestaat → fout; geldige `→ core` → geen fout.
  - `TestCheckTenetHeadings`: kop met geen of twee keer ` boven `; dubbele letter; gat in A..hoogste letter (vervallen letter telt mee, `(vervallen)` in kop); volgorde van letters vrij; niet-vervallen kop zonder *Bewijs*-regel met ≥2 links → fout.
  - `TestCheckStrategy`: koppen in volgorde, `**S<n>**`-ID's uniek en één per bullet.
  - `TestCheckNotNowIDs` en `TestCheckStrategy` gebruiken fixtures met `- **N1** …`-bullets (de vorm in de echte documenten). `TestCheckUnsetOrUnreadable`: ontbrekende sleutel `paths.strategy` (of tenets/areas) of onleesbaar bestand → fout met de sleutelnaam; hier geen skip.
  - `TestCheckLinksResolve`: een relatieve link in een document die nergens heen wijst → fout met `pad:regel` (dekt de herschreven spec-links).
  - `TestCheckReportsAllErrorsAtOnce`: twee fouten in twee bestanden → beide in één resultaat.
- [ ] **Step 2:** `go test ./internal/direction` → FAIL (`Check` ontbreekt).
- [ ] **Step 3:** Implementeer `Check(root string) []string`: laad `lighthouse.toml`; per sleutel (`strategy`, `tenets`, `areas`) ontbreekt/onleesbaar → fout met naam; areas = `labels.ParseTaxonomy` van `paths.labels` gefilterd op `Labels.AreaPrefix`, naam zonder prefix = bestandsnaam; koppen met regelnummers via een eenvoudige regel-voor-regel-scan; de ID-scan verwijdert eerst exact één bulletmarker (`- `) aan het begin van de regel en pakt daarna op de rest; een bullet is een regel die met `- ` begint; regexes (op de regel na het verwijderen van `- `): `^\*\*N(\d+)\*\*`, `^\*\*S(\d+)\*\*`, tenet-kop `^## ([A-Z])\. (.+)$`, link `\]\(([^)#]+)(#[^)]*)?\)` (alleen relatieve; bestaat vanaf de map van het bestand). Fouten gesorteerd op pad:regel.
- [ ] **Step 4:** `TestDirectionDocumentsHaveTheirShape`: `Check` op de repo-root (zoek de root via `go env`-vrije weg: `../..` vanaf het testpakket); `t.Errorf` per fout.
- [ ] **Step 4b:** Voeg `internal/direction/direction.go` toe aan `readers` van elke sleutel die het bestand met naam noemt (`paths.strategy`, `paths.tenets`, `paths.areas`, `paths.labels`, `labels.area_prefix`) in `docs/site/data/config.json`.
- [ ] **Step 5:** `go test ./internal/direction`, `node --test docs/site/test/config.test.mjs` en `lighthouse imports check` → PASS. Als een document uit Taak 5 faalt: herstel het document, niet de test.
- [ ] **Step 6: Commit** `test(direction): vormtest voor strategie, uitgangspunten en areas`.

### Taak 7: README

**Files:**
- Modify: `README.md`

- [ ] **Step 1:** De regel "Roadmap: the milestones of this repository" krijgt een link `[strategie](docs/developer/strategie.md)` (tekst in het Nederlands). De sectie *Design* met de link naar de scope-spec in personal_ai_agent wijst voortaan naar `docs/developer/strategie.md`.
- [ ] **Step 2:** `md-link-check` slaagt (relatief, bestaand doel).
- [ ] **Step 3: Commit** `docs(readme): roadmap en scope wijzen naar de strategie`.

### Taak 8: CI draait de vormtest ook als alleen de documenten veranderen

**Files:**
- Modify: `ci/domains/domains.go` (`goDocs`, `isNotGo`/`Classify`, commentaar boven `goDocs`)
- Test: `ci/domains/domains_test.go`

**Interfaces:**
- Produces: `goDocPrefixes = []string{"docs/developer/areas/"}` naast de losse bestanden in `goDocs`: `docs/developer/strategie.md`, `docs/developer/uitgangspunten.md`, `docs/developer/labels.md`.

- [ ] **Step 1: Test rood.** Rijen in `ci/domains/domains_test.go`: `docs/developer/strategie.md`, `…/uitgangspunten.md`, `…/labels.md`, `docs/developer/areas/core.md` → `set("go", "python")`; `docs/developer/other.md` blijft `set("python")`; `docs/developer/areas-extra.md` (naam begint wel met de prefix zonder `/`) → `set("python")`.
- [ ] **Step 2:** `go test ./ci/domains` → FAIL.
- [ ] **Step 3:** `goDocs` krijgt de drie bestanden; nieuwe `goDocPrefixes` met `docs/developer/areas/`; `Classify` raakt go als `goDocs[p]` of een prefix past. Werk het commentaar boven `goDocs` bij met de reden (de vormtest leest ze; zonder dit merget een PR die alleen een area-bestand breekt groen).
- [ ] **Step 4:** `go test ./ci/domains` → PASS. Draai ook `TestGoSourcesDoNotReadNotGoPaths` (`go test ./ci/domains -run Sources`): de vormtest noemt nu `docs/`-paden; als die test klaagt, voeg de gewenste uitzondering toe in `withoutExemptions` met reden.
- [ ] **Step 5: Commit** `ci(domains): documenten van de richting raken het go-domein`.

### Taak 9: Changelog, dogfood en eindcontrole

**Files:**
- Create: `changelog.d/issue-372-richting-per-repo.md`, `changelog.d/issue-372-richting-per-repo-2.md`

- [ ] **Step 1:** Fragment 1: `type: added` (frontmatter), regel: de richtingsdocumenten (strategie, uitgangspunten, areas), `paths.strategy` en de vormtest. Fragment 2: `type: changed`, regel: `write-issue`, `map-planned` en `close-issue` lezen de richting. Nederlands.
- [ ] **Step 2:** Volledige controle: `go run ./cmd/lighthouse init --upgrade` → `No changes.`; `go test -race ./...`; `lighthouse imports check`; `node --test "docs/site/test/*.test.mjs"`; `python3 pack/python/scripts/gate_python.py -- uv run pytest -q`.
- [ ] **Step 3: Commit** `docs(changelog): richting per repo`.
- [ ] **Step 4:** Meld in de PR: waar het bewijs dun is (`usage`, `product`). Run `/lighthouse-write-pr` stap V2b, push, handoff `next: /lighthouse-write-pr <nr>`. `lighthouse forge pr create` kent geen `--draft`: het handoff-blok noemt dat de run van `/lighthouse-write-pr` direct na het openen `gh pr ready --undo <nr>` draait en controleert met `lighthouse forge pr view <nr>` dat de PR draft is; zo kan de controller hem niet mergen vóór Corneels goedkeuring.

---

## Self-review

- **Spec-dekking:** §1 documenten → T5; §2 inhoud → T5; §3 config → T1; §4 lezers → T2 (pitch, 3f), T3 (map-planned), T4 (close-issue); §5 test → T6, CI-domein → T8; §6 README/docs/dogfood/changelog → T3 (tick.md), T1 (config.json), T7, T9; §7 issues (#499–#501) zijn al gedaan, geen taak; §8 één draft-PR → T9.
- **Volgorde:** T1 (sleutel) vóór T2 (anders `TestConfigKeysKnown` rood); T5 vóór of samen met T6 (de test faalt op "niet gezet"); T4 uiterlijk vóór de `paths.areas`-sleutel (T5) landt in main: allemaal één PR.
- **Open keuze voor de bouwer:** de plaats van de vormtest (T6) volgt uit `lighthouse imports check`.
