# ci/domains en ci/loop_roadmap naar Go — implementatieplan

> **Voor agentic workers:** VEREIST SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** `ci/domains.py` en `ci/loop_roadmap.py` worden Go-programma's onder `ci/`, met dezelfde vlaggen, uitvoer en exitcodes; `ci.yml` en `loop-roadmap.yml` roepen de Go-versies aan en hun Python-tests gaan na triage mee.

**Architectuur:** Twee `main`-pakketten in dezelfde Go-module, `ci/domains` en `ci/loop_roadmap`, volgens het patroon van `ci/changelog` (#360): `main()` roept `os.Exit(run(args, stdin, stdout, stderr))` aan, zodat `run` testbaar is. `loop_roadmap` praat met `gh` via `os/exec`, achter een kleine interface zodat de tests een fake gebruiken. Workflows krijgen `actions/setup-go` (zoals de `changelog`-job) en roepen `go run ./ci/<naam>` aan.

**Tech Stack:** Go (module-versie uit `go.mod`), stdlib alleen, `gh` CLI voor `loop_roadmap`.

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md) (§1 triage, §4 scripts, §7 CI).

## Globale randvoorwaarden

- Zelfde vlaggen, uitvoer en exitcodes als het origineel (issue #361). Geen ander gedrag (No-Go van de milestone).
- Exitcodes: 0 ok, 1 oordeel, 2 `COULD NOT CHECK` of gebruiksfout. `go run` maakt elke niet-nul exit tot 1; dat staat in de doc-comment, zoals in `ci/changelog/main.go`.
- Code, commentaar en bestandsnamen in het Engels; artefacten (PR-body, changelog-fragment, dit plan) in het Nederlands.
- Geen `pack/python/`, `tests/pack/python/`, `web/` of oude plannen aanraken.
- Een wijziging onder `.github/workflows/` wordt door de workflow-pusher gepusht (`workflow-push-gate`): de uitvoerder pusht die taak niet zelf.
- Het Python-origineel wordt in dezelfde PR verwijderd; `git ls-files ci/*.py` is daarna leeg.

## Review Focus

- `--github-output` onschrijfbaar: Python geeft een traceback (exit 1); Go geeft exit 2 met een duidelijke melding. Dit is een bewust, in de doc-comment genoteerd verschil.
- Een pad met niet-UTF-8-bytes op stdin: Python gebruikt `surrogateescape`; Go leest bytes en vergelijkt ze als string zonder te falen.
- Lege stdin of alleen NUL-bytes: `web=false`, `python=false`, `go=false`, exit 0.
- `--github-output` op een bestaand bestand: toevoegen, eerdere regels behouden; een onschrijfbaar pad geeft een duidelijke fout en geen paniek.
- Geen subcommando of een onbekend subcommando: exit 2 met gebruikstekst op stderr, niets op stdout.
- `loop_roadmap` met een kapotte of ontbrekende `loop.json`: exit 2 `COULD NOT CHECK`, nooit exit 1.
- `gh` ontbreekt op `PATH` of faalt: exit 2 `COULD NOT CHECK`.
- Een `graphql`-antwoord zonder `closingIssuesReferences`: exit 2, geen nil-pointer.

---

## Bestandsstructuur

| Bestand | Rol |
|---|---|
| `ci/domains/main.go` | CLI: `classify --github-output FILE`, stdin NUL-gescheiden |
| `ci/domains/domains.go` | `Classify(paths []string) map[string]bool` en de tabellen |
| `ci/domains/domains_test.go` | tabeltest met letterlijke verwachtingen, CLI-tests |
| `ci/loop_roadmap/main.go` | CLI: `--pr N`, exitcodes |
| `ci/loop_roadmap/roadmap.go` | `Planned`, `Problems`, `PRProblems` (zuivere functies) |
| `ci/loop_roadmap/gh.go` | `ghClient` interface + implementatie via `gh api` |
| `ci/loop_roadmap/roadmap_test.go`, `main_test.go` | tests na triage |
| `.github/workflows/ci.yml`, `loop-roadmap.yml` | aanroep naar Go |
| `tests/test_ci_domains_pin.py` | naald en `uses`-teller bijwerken (blijft tot #370) |
| `tests/test_site_loop.py` | kruiscontrole die `ci/loop_roadmap.py` importeert |
| `docs/developer/labels.md` | verwijzing naar het script |
| `changelog.d/issue-361-ci-domains-loop-roadmap-go.md` | type `internal` |

## Taak 1: `ci/domains` naar Go

**Bestanden:**
- Aanmaken: `ci/domains/domains.go`, `ci/domains/main.go`, `ci/domains/domains_test.go`
- Lezen (bron): `ci/domains.py`, `tests/test_domains.py` (tabel `test_classify`, regels 41-78, CLI-tests 216-255)

**Interfaces:**
- Produceert: `func Classify(paths []string) map[string]bool` met sleutels `web`, `python`, `go`; `func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int`.

- [ ] **Stap 1: triage van `tests/test_domains.py`.** Per testfunctie de kernvraag uit `lighthouse-test-standard`: welke productiewijziging laat hem falen? Leg per bestand/groep de bestemming vast in een tijdelijke notitie voor de PR-tabel (bestand, aantal tests, bestemming, één zin reden; bij *gedekt* de Go-testnaam). De 31 rijen van `test_classify` gaan als één groep naar Go; de CLI-tests (uitvoer, `--github-output` toevoegen, subcommando verplicht → exit 2) gaan naar Go; de Go-bronscans (`tests/test_domains.py:127-144`, `192-213`: geen `.go`-bestand noemt `docs/`, `changelog.d` of `ci/literal-`) beschermen de aanname achter `_NOT_GO_DIRS` en `_NOT_GO` en blijven bestaan, nu als Go-test (zie Taak 2).
- [ ] **Stap 2: schrijf de falende tabeltest** `TestClassify` in `ci/domains/domains_test.go`: elke rij van `test_classify` letterlijk overgenomen (pad(en) → verwachte set domeinen), met de rij voor `ci/domains/main.go` → `web`, `go`, `python` in plaats van `ci/domains.py`. Plus `TestRunCLI`: stdin `a\0b\0`, uitvoer exact `web=…\npython=…\ngo=…\n` in die volgorde; `TestRunGithubOutputAppends`; `TestRunNoSubcommand` (exit 2, stdout leeg); lege stdin.
- [ ] **Stap 3: draai `go test ./ci/domains/`.** Verwacht: FAIL (pakket ontbreekt).
- [ ] **Stap 4: implementeer** `Classify` in `ci/domains/domains.go` met de tabellen uit `ci/domains.py:19-51` (`webToo` met `ci/domains/domains.go` en `ci/domains/main.go` in plaats van `ci/domains.py`, `goToo`, `goDocs`, `goFixtures`, `notGoDirs`, `notGo`), de commentaren over de meting meenemen en bijwerken. `run` in `main.go`: `flag.FlagSet` per subcommando, usage naar stderr bij een fout, exit 2; stdin splitsen op `\x00`, lege delen overslaan; `--github-output` openen met `O_APPEND|O_CREATE|O_WRONLY`.
- [ ] **Stap 5: draai `go test ./ci/domains/ && go vet ./ci/domains/`.** Verwacht: PASS.
- [ ] **Stap 6: mutatiecontrole.** Haal `ci/domains/main.go` uit `webToo`; de tabeltest moet falen. Zet terug.
- [ ] **Stap 7: commit** `feat(ci): ci/domains naar Go (#361)`.

## Taak 2: de Go-bronscans een nieuw thuis geven

**Bestanden:**
- Wijzigen: `ci/domains/domains_test.go` (of een eigen `ci/domains/sources_test.go`)
- Lezen: `tests/test_domains.py:20-38, 81-144, 192-213`

- [ ] **Stap 1: schrijf `TestGoSourcesDoNotReadNotGoPaths`**: loopt over de `.go`-bestanden die `git ls-files -z --cached --others --exclude-standard -- '*.go'` geeft (zoals `tests/test_domains.py:129-131`; geen bestandssysteemwandeling, die zou `.claude/worktrees/` meelezen) en faalt wanneer er een `docs/`, `changelog.d` of `ci/literal-` noemt. Port `_without_default_paths` (`tests/test_domains.py:84-124`) volledig, met de tekstvervangingen per bestand (replace-only, geen hele-bestandsuitzondering) en de uitzondering voor `ci/changelog/*.go`. Nieuw: `ci/domains/domains.go` (bevat de tabelliteralen zelf) en `ci/loop_roadmap/*.go` (productiecode noemt `docs/site/data/loop.json` en `docs/developer/labels.md` als uitvoertekst; `loop-roadmap.yml` draait op elke PR, dus de uitzondering verbergt geen gemiste CI-run), allebei met een reden in een commentaar; `*_test.go`-bestanden van `ci/` alleen waar ze een literal echt nodig hebben, per bestand en met reden.
- [ ] **Stap 2: schrijf `TestGoSourcesDoNotReadTestsOrRootMD`** (port van `tests/test_domains.py:147-213`, `_repo_reads`): een Go-bestand dat vanuit zijn pakketmap via `../` of `".."`-delen `tests/` (buiten `tests/pack/fixtures/`) of een los root-`.md`-bestand (`README`, `CHANGELOG`, `CONTRIBUTING`, `SECURITY`, `CLA`) leest, faalt. Go `regexp` (RE2) kent geen `(?!…)`: match breed en filter het `tests/pack/fixtures/`-voorvoegsel (en `"tests","pack","fixtures"`-delen) in code. Port de vier zelftests (`test_repo_reads_*`, regels 147-189) letterlijk als Go-tests van de hulpfunctie.
- [ ] **Stap 3: draai `go test ./ci/domains/`.** Verwacht: PASS. Voeg tijdelijk `"docs/x"` toe aan een niet-uitgezonderd bestand; verwacht FAIL; draai terug.
- [ ] **Stap 4: commit** `test(ci): bronscans van de domeinen in Go (#361)`.

## Taak 3: `ci/loop_roadmap` naar Go

**Bestanden:**
- Aanmaken: `ci/loop_roadmap/{main,roadmap,gh}.go`, `ci/loop_roadmap/roadmap_test.go`, `ci/loop_roadmap/main_test.go`
- Lezen (bron): `ci/loop_roadmap.py`, `tests/test_loop_roadmap.py`, `tests/test_site_loop.py:27-31, 209-218`

**Interfaces:**
- Produceert: `func Planned(chain Chain) []Item` met `type Item struct { Name string; Issue *int }`, in volgorde chips dan handoffs zoals het Python-woordenboek (Go-map-iteratie is willekeurig; `Problems` en `PRProblems` doorlopen deze slice en bouwen een opzoekmap apart); `func Problems(chain Chain, open map[int]string) []string`; `func PRProblems(chain Chain, closed map[int]bool, touched map[string]bool) []string`; `type ghClient interface { OpenLoopIssues(repo string) (map[int]string, error); ClosedLoopIssues(repo string, pr int) (map[int]bool, error); TouchedFiles(repo string, pr int) (map[string]bool, error) }`; `func run(args []string, env func(string) string, gh ghClient, root string, stdout, stderr io.Writer) int`.

- [ ] **Stap 1: triage van `tests/test_loop_roadmap.py` en de kruiscontrole in `tests/test_site_loop.py:209-218`.** Zelfde notitievorm als Taak 1. De zuivere functies (`planned`, `problems`, `pr_problems`) gaan naar Go met hun letterlijke verwachtingen; tests die alleen `gh`-aanroepen met `subprocess` mocken en geen gedrag van het script bewijzen, gaan weg als *beschermt niets* tenzij ze een `COULD NOT CHECK`-pad dekken. De kruiscontrole "site en roadmap zijn het eens over een geplande kaart" vergelijkt twee implementaties en kan niet naar Go (Go roept `planned_problems` niet aan). Ze blijft in Python tegen `planned_problems` alleen (zie Taak 4 stap 3); de vier gevallen (issue / issue+milestone / alleen milestone / niets → gemarkeerd, de laatste twee) staan letterlijk in `TestProblems` en heten in de triagerij *gedekt*. Geen Go-test leest `docs/site/data/loop.json`: `docs/` valt buiten `go`, zo'n test zou op een PR die alleen die map raakt niet draaien.
- [ ] **Stap 2: schrijf de falende tests**: `TestPlanned`, `TestProblems`, `TestPRProblems` (de uitvoertekst letterlijk, bijvoorbeeld `<name>: planned without an issue`), `TestRunMainMode` en `TestRunPRMode` met een fake `ghClient`, en de exitcodes: kapotte `loop.json` → 2, `gh`-fout → 2 met `COULD NOT CHECK: …` op stderr, oordeel → 1, ok → 0.
- [ ] **Stap 3: draai `go test ./ci/loop_roadmap/`.** Verwacht: FAIL.
- [ ] **Stap 4: implementeer** de zuivere functies naar het origineel (`ci/loop_roadmap.py:36-90`); `gh.go` roept `gh api` aan met dezelfde argumenten als de Python (`repos/{repo}/issues?labels=loop&state=open&per_page=100 --paginate`, graphql `closingIssuesReferences`, `pulls/{pr}/files`); `--paginate` met `--jq` uitvoer regel voor regel verwerken. Repo uit `GITHUB_REPOSITORY`, standaard `CorneeldH/lighthouse`. `SCRIPT` wordt `ci/loop_roadmap/main.go`: in PR-modus telt elke gewijzigde `ci/loop_roadmap/*.go` als "script aangeraakt". Houd de doc-comment van de CLI zoals het origineel en noteer de verschillen (zoals `ci/changelog/main.go`).
- [ ] **Stap 5: draai `go test ./ci/loop_roadmap/ && go vet ./ci/loop_roadmap/`.** Verwacht: PASS.
- [ ] **Stap 6: mutatiecontrole.** Keer in `Problems` een vergelijking om; minstens één test moet falen. Zet terug.
- [ ] **Stap 7: commit** `feat(ci): ci/loop_roadmap naar Go (#361)`.

## Taak 4: workflows, pintests en documentatie

**Bestanden:**
- Wijzigen: `.github/workflows/ci.yml` (regels 1, 26-51, 130), `.github/workflows/loop-roadmap.yml` (regels 5, 17, 34), `tests/test_ci_domains_pin.py` (naald regel 47, teller regel 192), `docs/developer/labels.md:42,47`
- Verwijderen: `ci/domains.py`, `ci/loop_roadmap.py`, `tests/test_domains.py`, `tests/_domains.py`, `tests/test_loop_roadmap.py`
- Wijzigen: `tests/test_site_loop.py` (importeert het verwijderde script); `tests/test_shard.py:44` blijft ongewijzigd

- [ ] **Stap 1: pas de pintest eerst aan** (rood): naald wordt `go run ./ci/domains classify`; de `uses`-teller gaat naar 26 omdat de `changes`-job een `actions/setup-go`-stap krijgt met `go-version-file: go.mod` en `cache: false`, naar het voorbeeld van de `changelog`-job (`ci.yml:63-70`, pin-test `198-210`). Draai `uv run pytest -q tests/test_ci_domains_pin.py`; verwacht FAIL.
- [ ] **Stap 2: wijzig `ci.yml`**: `changes`-job krijgt een `actions/setup-go`-stap (de checkout staat er al, `ci.yml:34`; de teller 26 gaat daarvan uit) en roept `go run ./ci/domains classify --github-output "$GITHUB_OUTPUT"` aan; commentaren op regel 1 en 130 bijwerken. `loop-roadmap.yml`: setup-go toevoegen, `push.paths` bevat `ci/loop_roadmap/**`, `run: go run ./ci/loop_roadmap …`; commentaar bijwerken. Zorg dat `ci/domains/**` en `ci/loop_roadmap/**` onder `go` en `python` vallen volgens `Classify`.
- [ ] **Stap 3: verwijder** de vijf Python-bestanden uit het kopje; verwijder in `tests/test_site_loop.py` de moduleniveau-import van het script (regels 27-31) en `roadmap.problems` uit `test_site_and_roadmap_agree_on_a_planned_item`; de vier gevallen blijven staan tegen `planned_problems` alleen (`assert bool(planned_problems(chain)) is flagged`), de Go-kant staat in `TestProblems`; `tests/test_shard.py:44` is een synthetische CRC32-nodeid die geen bestaand bestand nodig heeft: laat hem staan en sluit `tests/test_shard.py` uit van de grep in stap 5; werk de docstring `tests/test_ci_domains_pin.py:45` bij naar `ci/domains`.
- [ ] **Stap 4: werk `docs/developer/labels.md` bij** naar `go run ./ci/loop_roadmap`.
- [ ] **Stap 5: verifieer**: `go test ./ci/... && go vet ./ci/...`, `uv run pytest -q tests/test_ci_domains_pin.py tests/test_site_loop.py tests/test_shard.py`, `git ls-files 'ci/*.py'` (leeg), `grep -rn 'domains\.py\|loop_roadmap\.py' . --include='*' -l --exclude-dir=.git --exclude-dir=planning` (geen treffer buiten `docs/developer/planning/` en `tests/test_shard.py`).
- [ ] **Stap 6: commit** de niet-workflowdelen apart van de workflows (`workflow-push-gate`): `refactor(ci): Python-originelen en tests weg (#361)`, daarna `ci: workflows roepen ci/domains en ci/loop_roadmap in Go aan (#361)`. De pusher pusht de workflowcommit.

## Taak 5: changelog-fragment en afronding

**Bestanden:**
- Aanmaken: `changelog.d/issue-361-ci-domains-loop-roadmap-go.md`

- [ ] **Stap 1: schrijf het fragment** (front matter `type: internal`, tekst: `ci/domains` en `ci/loop_roadmap` zijn Go).
- [ ] **Stap 2: `go run ./ci/changelog check --base origin/main --head HEAD`** moet slagen.
- [ ] **Stap 3: draai `[test_gate] command` uit `lighthouse.toml` en `lighthouse init --upgrade`-controle**: `go run ./cmd/lighthouse init --upgrade` geeft `No changes.` (alleen nodig als `pack/` of `internal/` is geraakt; verwacht niet).
- [ ] **Stap 4: PR-body** (via `/lighthouse-write-pr`) bevat de triagetabel van spec §1 uit de notities van Taak 1 en 3 en het bewijs per acceptatiecriterium.
- [ ] **Stap 5: commit** `docs(changelog): fragment voor #361`.

## Zelfreview

- Spec §4 (main-pakket, `go run ./ci/<naam>`) → Taak 1, 3. Spec §7 (`ci/domains` beslist de jobs; setup-go in `changes`) → Taak 4. Spec §1 (triagetabel in PR-body) → Taak 1, 3, 5. AC 1 → Taak 4. AC 2 → Taak 1 stap 2. AC 3 → Taak 5. AC 4 → Taak 5.
- Onzeker en bij uitvoering te bevestigen: of `go run` in de `changes`-job de benodigde toolchain vindt zonder cache (de stap volgt de `changelog`-job), en of de Go-bronscan van `tests/pack/` fixtures nog Python nodig heeft.
