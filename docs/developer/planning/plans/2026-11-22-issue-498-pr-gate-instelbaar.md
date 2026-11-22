# pr-create-gate: instelbaar hoe streng de issue-eis is — implementatieplan

> **Voor uitvoerders:** verplichte sub-skill: gebruik `superpowers:subagent-driven-development` (aanbevolen) of `superpowers:executing-plans` om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `[pr_gate] issue` in `lighthouse.toml` (`optional` | `required` | `evidence`) bepaalt hoe streng `pr-create-gate` is over de issue-eis van een PR-body, en `/lighthouse-write-pr` schrijft een body die er direct doorheen komt.

**Architectuur:** een nieuwe config-sleutel `pr_gate.issue` (default `optional`, gevalideerd in `internal/config`). `prbody.Repo` krijgt een veld `PRGate` dat `lazyRepo` uit de config vult; `CheckBody` leest het en voegt per waarde een blokkeerreden toe, zodat de handtekening van `CheckBody` gelijk blijft. De skill leest dezelfde waarde met `lighthouse config pr_gate.issue`.

**Techniek:** Go (`internal/config`, `internal/prbody`), JSON-fixtures en `docs/site/data/config.json`, skill in markdown, Node-test voor `docs/site`.

**Spec:** geen aparte spec; de acceptatiecriteria van issue #498 (milestone *M3 — Tweede omgeving*) zijn de spec. Voortgekomen uit #19 (instapniveaus): licht = `optional`, standaard = `required`, streng = `evidence`. Het niveau zelf zet de knop pas als #19 het uitvoert; dit plan levert alleen de knop.

## Beslissingen die dit plan vastlegt

| Waarde | `Closes #N` + Evidence | `Refs #N — <reden>` zonder tabel | `Refs #N` met tabel | `No issue: <reden>` |
|---|---|---|---|---|
| `optional` (default, = gedrag nu) | toegestaan | toegestaan | toegestaan | toegestaan |
| `required` | toegestaan | toegestaan | toegestaan | **blokkeert** |
| `evidence` | toegestaan | **blokkeert** | toegestaan | **blokkeert** |

- Een body met `Refs #N` én `No issue:` telt als `Refs`: onder `required` komt hij door, onder `evidence` alleen met tabel.
- `evidence` eist bij `Refs #N` de vorm van een tabel (`### Evidence #N`, `EvidenceRows` zonder reden), niet het aantal rijen: de issue wordt, net als nu, niet opgehaald voor `Refs`.
- Een ongeldige waarde is een configfout bij `config.Load` (net als `review.roborev`); de gate meldt die al als exit 1 via `fail`.
- De blokkeermelding noemt de waarde en wat de body mist, in de vorm: ``pr_gate.issue = <waarde>: <wat mist>``. `blockedMessage` krijgt de vormenlijst per waarde: onder `required` noemt de footer geen `No issue`, onder `evidence` ook geen `Refs` zonder tabel (anders adviseert de melding een vorm die opnieuw blokkeert).
- Geen nieuwe GitHub-aanname (no-go van de milestone): alles forge-neutraal.

## Globale randvoorwaarden

- Artefacten (plan, tests, commits, changelog-fragment) Nederlands; skilltekst en hookmeldingen Engels.
- Voor elke commit: `[test_gate] command` en `lighthouse imports check`.
- `.claude/hooks/lighthouse/*` nooit met de hand bewerken; de geïnstalleerde skill-kopieën volgen via `go run ./cmd/lighthouse init --upgrade` (dogfood, CI eist `No changes.`).
- `CheckBody` houdt zijn handtekening; `internal/prbody/fixtures_test.go` en `gate.go:288` blijven compileren.

## Review Focus

- `pr_gate.issue = ""` (lege string) of hoofdletters (`Required`): configfout, geen stille terugval op `optional`.
- Zonder `[pr_gate]`-tabel of met een kapotte `lighthouse.toml` bij een commando zonder `gh pr create`: gedrag ongewijzigd (lazy config blijft lazy).
- `Closes #N` zonder Evidence blokkeert in alle drie de waarden (bestaand gedrag mag niet verslappen onder `optional`).
- `Refs #N —` met een te korte reden telt niet als `Refs`: onder `required` valt zo'n body in de vormmelding, niet in de `No issue`-melding.
- Body met `No issue:` en een geldige `Refs #N`: door onder `required`.

---

### Taak 1: configsleutel `pr_gate.issue`

**Bestanden:**
- Wijzig: `internal/config/config.go` (`schema` na `imports.check`, `defaults`, validatie naast `review.roborev` rond regel 715)
- Wijzig: `tests/pack/fixtures/config-schema.json` (sleutellijst + defaults), `tests/pack/fixtures/config/defaults/expect.json`, en zo nodig `config/full/{lighthouse.toml,expect.json}`
- Test: `internal/config/config_test.go`
- De kaart `docs/site/data/config.json` en `docs/site/test/config.test.mjs` komen in de commit van taak 2, niet hier: `[test_gate] command` draait `node --test docs/site/test/*.test.mjs` bij elke commit die `internal/`, `pack/` of `cmd/` raakt (`lighthouse.toml:47`), en de kaarttest leidt de `readers` af uit de actuele bestanden. Taak 1 is daarom een werkboomstap zonder commit; de commit van taak 2 bevat config, gate en kaart samen.

**Interfaces:**
- Produceert: `lighthouse config pr_gate.issue` drukt `optional` af zonder sleutel; `config.Lookup(cfg, "pr_gate.issue")` en `cfg.Raw["pr_gate"]` (map) geven de gezette waarde.

- [ ] **Stap 1: faalende test** in `internal/config/config_test.go`, naar het voorbeeld van `runImportsCheck` (regel 373-399): `TestPRGateIssue` met vier subtests tegen een temp-toml:
  - geen `[pr_gate]` → `Run([]string{"pr_gate.issue"})` geeft `optional`, rc 0;
  - `issue = "required"` en `issue = "evidence"` → die waarde, rc 0;
  - `issue = "strict"` → rc 2 (config-fout) en stderr bevat ``key pr_gate.issue must be 'optional', 'required' or 'evidence'``;
  - `issue = ""` → rc 2 (geen stille terugval).
- [ ] **Stap 2: draai** `go test ./internal/config/ -run TestPRGateIssue -v`. Verwacht: FAIL (`unknown key`).
- [ ] **Stap 3: implementeer** in `config.go`: `{"pr_gate.issue", kStr}` in `schema`, `"pr_gate.issue": "optional"` in `defaults`, en in `build` een controle direct onder die van `review.roborev` die elke waarde buiten de drie met `errf("%s: key pr_gate.issue must be 'optional', 'required' or 'evidence', not %s", path, pyRepr(v))` afwijst (ook `""`; controleer dat `mayBeEmpty` die niet toelaat).
- [ ] **Stap 4: fixtures bijwerken**: `config-schema.json` (sleutel en default), `config/defaults/expect.json` en eventueel `config/full`.
- [ ] **Stap 5: draai** `go test ./internal/config/... ./internal/packcontract/...`. Verwacht: PASS. Draai hier nog niet `node --test test/config.test.mjs`: de sleutel staat dan wel in het schema maar nog niet op de kaart.
- [ ] **Stap 6: geen commit.** Laat de wijzigingen staan; de commit volgt in taak 2, stap 6.

### Taak 2: de gate past de waarde toe

**Bestanden:**
- Wijzig: `internal/prbody/form.go` (`Repo`-struct krijgt `PRGate string`, "" = `optional`; `CheckBody` regel 402-443)
- Wijzig: `internal/prbody/gate.go` (`lazyRepo` regel 307-324 zet `PRGate` uit `cfg.Raw["pr_gate"]["issue"]`, naar het voorbeeld van `tokenEnv`; de doc-comment van die code noemt de sleutel letterlijk als `pr_gate.issue`, anders ziet `config.test.mjs` gate.go niet als reader)
- Test: `internal/prbody/form_test.go` (nieuw of bestaand bestand voor `CheckBody`; volg het patroon van `fixtures_test.go`), `internal/prbody/gate_test.go`

**Interfaces:**
- Verbruikt: `config.Config.Raw` (taak 1 garandeert een geldige waarde of lege string).
- Produceert: `Repo.PRGate` met waarden `""`/`"optional"`, `"required"`, `"evidence"`; `CheckBody(body, repo, acCount)` onveranderd van vorm.

- [ ] **Stap 1: faalende tests** `TestCheckBodyPRGate` in `internal/prbody/` als tabel (naam, `PRGate`, body, verwacht aantal blockers en een substring). Per waarde een body die net wel en net niet doorkomt, met `Repo{Kind: "github", Slug: "o/r", PRGate: …}`:
  - `optional` en `""`: `No issue: een geldige reden hier` → geen blockers; `Refs #5 — een geldige reden hier` zonder tabel → geen blockers.
  - `required`: `No issue: een geldige reden hier` → 1 blocker met ``pr_gate.issue = required`` en `` `No issue` ``; `Refs #5 — een geldige reden hier` zonder tabel → geen blockers; `Refs #5 — kort` (reden < `MinReason`) → blocker waarvan de melding met ``pr_gate.issue = required:`` begint en `No issue` niet als toegestane vorm noemt; `No issue: een geldige reden hier` plus `Refs #5 — een geldige reden hier` → geen blockers.
  - `evidence`: `Refs #5 — een geldige reden hier` zonder tabel → 1 blocker met ``pr_gate.issue = evidence`` en ``### Evidence #5``; dezelfde body mét een geldige tabel (`### Evidence #5` met één rij, zoals de bestaande fixtures) → geen blockers; `No issue: …` → blocker met ``pr_gate.issue = evidence``; `Closes #5` met geldige tabel → geen blockers (de `acCount`-stub levert 1).
  - alle drie: `Closes #5` zonder Evidence → blocker (bestaande melding, niet verslapt).
  Plus in `gate_test.go` één `Main`-test in het patroon van `TestMainNoIssuePasses` (regel 367): met een `lighthouse.toml` met `[pr_gate] issue = "required"` en een `gh pr create --body-file` met `No issue: …` is de exit 2 en bevat stderr ``pr_gate.issue = required`` en in de footer geen `No issue`; dezelfde `Main`-test onder `evidence` met `No issue: …` bevat in de melding en footer geen kale `Refs #N — <reason>` (alleen `Refs` mét `### Evidence #N`); zonder de tabel is dezelfde body exit 0.
- [ ] **Stap 2: draai** `go test ./internal/prbody/ -run 'PRGate' -v`. Verwacht: FAIL.
- [ ] **Stap 3: implementeer `CheckBody`-uitbreiding** in `form.go`: de bestaande "no valid form"-blocker (regel 433-441) krijgt onder `required` of `evidence` het voorvoegsel ``pr_gate.issue = <waarde>:`` en noemt alleen de toegestane vormen; `blockedMessage` in `gate.go` krijgt de waarde mee en toont alleen de toegestane vormen in de footer (onder `optional` ongewijzigd, zodat de bestaande fixtures groen blijven). Verder onderaan, na de bestaande verwerking van `Closes` en `Refs`, twee extra regels op basis van `repo.PRGate`:
  - `required` of `evidence`, geen `targets`, geen `refs` en `NoIssue(body)`: blocker ``pr_gate.issue = <waarde>: the body has no issue — `No issue: <reason>` is not allowed; name an issue with `Closes #N` + `### Evidence #N` or (only under `required`) `Refs #N — <reason>`; under `evidence` `Refs #N` needs its `### Evidence #N` table `` (de bestaande "no valid form"-tak blijft voor wie niets geldigs heeft; `No issue` is dan niet de enige vorm, dus voorkom dubbele meldingen).
  - `evidence`, een `Refs #N` zonder `HasEvidenceBlock(body, N)` (en `N` niet in `targets`): blocker ``pr_gate.issue = evidence: Refs #N has no `### Evidence #N` table`` — één per issue, in de bestaande lus bij regel 423-431.
  Houd het één plek per regel; geen nieuwe helper tenzij de lus er onleesbaar van wordt.
- [ ] **Stap 4: implementeer de wiring** in `gate.go`: `lazyRepo` vult `Repo.PRGate` uit `cfg.Raw["pr_gate"]` (alleen lezen, validatie deed taak 1); laat de lazy-eigenschap intact.
- [ ] **Stap 5: draai** `go test ./internal/prbody/... ./internal/hooks/... ./internal/cli/... ./internal/packcontract/... ./e2e/...`. Verwacht: PASS, inclusief de bestaande `cases.json`-fixtures (ongewijzigde default).
- [ ] **Stap 5b: kaart.** Voeg `docs/site/data/config.json` toe (entry naar het voorbeeld van `imports.check`: `level: "advanced"`, `required: "no"`, `default: "optional"`, `options` `["optional","required","evidence"]`, `about`: één Engelse zin; `readers`: `internal/prbody/form.go` en `internal/prbody/gate.go`; de skill komt in taak 3) en de regel `"pr_gate.issue": ["optional", "required", "evidence"]` in `expected` van de test *options are the values lighthouse accepts* in `docs/site/test/config.test.mjs` (hardgecodeerd, regel ~537-549). Draai `cd docs/site && node --test test/config.test.mjs`. Verwacht: PASS.
- [ ] **Stap 6: commit** (taak 1 en 2 samen) `feat(prbody): pr-create-gate volgt pr_gate.issue (#498)`; alles uit taak 1, de gate en de kaart.

### Taak 3: skill, docs, changelog en dogfood

**Bestanden:**
- Wijzig: `pack/core/skills/write-pr/SKILL.md` (regel 11, 21-23, V3 regel 150-163)
- Wijzig: `docs/getting-started.md` (regel 243: de beschrijving van `pr-create-gate`) en `docs/test-gate.md` niet; zoek eerst met `grep -rn "No issue" docs/*.md` of een andere gebruikersdoc de drie vormen noemt
- Maak: `changelog.d/issue-498-pr-gate-instelbaar.md` (`type: changed`, één Engelse zin)
- Wijzig: `docs/site/data/config.json` (voeg bij de entry uit taak 2 de reader `pack/core/skills/write-pr` toe — de directory van de skill, niet `SKILL.md`, zoals `readerUnit` in de test)
- Test: `internal/packcontract/writepr_test.go` en `skills_test.go` (bestaand; voeg een controle toe dat de skill `pr_gate.issue` noemt)

**Interfaces:**
- Verbruikt: `lighthouse config pr_gate.issue` (rc 0 met `optional`/`required`/`evidence`; rc 1 = sleutel niet gezet door een oudere binary: behandel als `optional`, zeg dat in de skill; rc 2 = kapotte config: stop zoals de skill al doet bij `paths.labels`).

- [ ] **Stap 1: faalende test** in `internal/packcontract/writepr_test.go`: de skilltekst bevat `lighthouse config pr_gate.issue` en de drie waarden, en `No issue` met `required`. Draai en zie FAIL.
- [ ] **Stap 2: pas de skill aan** (Engels): lees onder *When* en vóór V1 één keer `lighthouse config pr_gate.issue`. Regels in de skill: zonder issuenummer en waarde `optional` blijft `No issue: <reason>`; bij `required` of `evidence` is een PR zonder nummer niet toegestaan → stop en meld dat de repo een issue eist (geen PR, geen vraag). In V3: bij `evidence` is de `### Evidence #N`-tabel verplicht, ook als alle criteria open zijn: dan is er niets te bewijzen en is `Refs` niet haalbaar → meld dat en stop (zoals de gate zou blokkeren), zonder een rij voor een open criterium te verzinnen. Pas regel 11 aan zodat "always `Refs #N`" de waarde `optional` noemt voor `No issue`.
- [ ] **Stap 3: kaart, docs en changelog**: voeg de reader `pack/core/skills/write-pr` toe aan de entry in `docs/site/data/config.json` en draai `cd docs/site && node --test test/config.test.mjs` (verwacht PASS). Pas de `pr-create-gate`-zin in `docs/getting-started.md` aan en noem `[pr_gate] issue`; schrijf het changelog-fragment.
- [ ] **Stap 4: dogfood** `go run ./cmd/lighthouse init --upgrade`; commit de geïnstalleerde kopieën (`.claude/skills/lighthouse-write-pr/`, `.agents/skills/lighthouse-write-pr/`) mee. Draai daarna nogmaals en verwacht `No changes.`.
- [ ] **Stap 5: draai** `go test ./internal/packcontract/... ./internal/dispatch/...`, `[test_gate] command` en `lighthouse imports check`. Verwacht: PASS.
- [ ] **Stap 6: commit** `docs(write-pr): skill en docs lezen pr_gate.issue (#498)`.

## Zelfreview

- **AC 1** (drie waarden, default gelijk aan nu): taak 1 (sleutel, default, validatie) en taak 2 (`optional` = bestaand gedrag, bevestigd door de ongewijzigde fixtures).
- **AC 2** (melding noemt waarde en wat mist): taak 2, stap 1 en 3.
- **AC 3** (`/lighthouse-write-pr` leest de waarde): taak 3.
- **AC 4** (tests per waarde, net wel en net niet): taak 2 stap 1, plus taak 1 voor de config.
- **DoD** gereviewd, tests groen, docs bijgewerkt: taak 3 en de eindreview van de uitvoerder.
