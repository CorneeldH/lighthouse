# Ontwerp: de repo zonder Python

Milestone: nog aan te maken · Datum: 2026-11-16 · Bouwt op
[Lighthouse zonder Python](2026-11-07-zonder-python-design.md) (#274–#285), die de Python uit
wat `init` installeert haalt. Deze spec doet de rest van de repo: `ci/`, `tools/` en `tests/`.

## Aanleiding en doel

Na #285 heeft een gebruiker geen `python3` meer nodig. In de repo zelf staat het nog wel: zeven
Python-scripts in `ci/` en `tools/`, drie shellscripts die `python3` aanroepen, en een
pytest-suite van 3249 tests. Die vorige spec liet dat bewust staan (*Niet-doelen*).

Het doel is snelheid, van CI en van de ontwikkellus van agents (besluit gebruiker). Een agent
compileert en test veel vaker dan een mens; een snelle lus weegt dan zwaarder dan leesbaarheid
([Wes McKinney, *Agent ergonomics*](https://wesmckinney.com/blog/agent-ergonomics/)). Go past
daarop: `go vet` en de compiler vangen fouten vóór de testrun, en `go test` cachet per pakket,
zodat alleen geraakte pakketten opnieuw draaien. pytest draait elke keer alles.

Gemeten op een groene `ci.yml`-run (run 43916464946): drie pytest-shards van 128, 163 en 163 s,
`go-test` 147 s, `go` 117 s, `go-e2e` 73 s.

Een tweede doel komt van de gebruiker: kritisch kijken of elke test iets toevoegt. Agents
schrijven te veel tests. Deze milestone port daarom geen testset, maar sorteert hem.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Elk Python-script in `ci/` en `tools/` wordt Go, met dezelfde vlaggen, uitvoer en exitcodes.
- `ci/spa-smoke.sh`, `ci/windows-measure.sh` en `tools/hooks/pre-push-secret-scan.sh` roepen geen
  `python3` meer aan.
- Elke test onder `tests/` buiten `tests/pack/python/` krijgt één van vier bestemmingen (§1).
- De site-tests van `docs/site/` draaien met `node --test`.
- CI draait geen pytest-shards meer; alleen een kleine job voor `pack/python`.
- De docs noemen geen `python3 -m http.server` meer om de site te bekijken.

**Niet-doelen**

- `pack/python` en `tests/pack/python/` (317 tests) blijven Python en pytest. Dat is het
  taalpakket voor Python-repo's.
- `web/`: die heeft al Vitest en Playwright in TypeScript.
- Ander gedrag van een script of hook. Een port geeft hetzelfde oordeel als het origineel.
- De bestaande Go-tests versnellen. Wel meten (§7).
- Oude plannen in `docs/developer/planning/plans/` herschrijven: dat zijn verslagen.

**Anti-doelen.** Dit telt als mislukt, ook als het technisch werkt:

- Een test die 1-op-1 naar Go gaat terwijl hij niets beschermt. Een getranslitereerde tautologie
  is nog steeds een tautologie, nu in een snellere taal.
- Een gedrag dat stil onbewaakt raakt. Elke test die weggaat, heeft een reden in de triagetabel
  (§1), en "gedekt door" noemt de test die het overneemt.
- Een test die na de milestone zowel in pytest als in Go bestaat.
- CI die langzamer wordt dan nu (wandklok van `ci.yml`, §7).

## Inventaris

| Bestand | Regels | Aangeroepen door |
|---|---|---|
| `tools/hooks/checkpoint_scan.py` | 994 | `ci.yml`, checkpoint-push (#228) |
| `tools/check_cla.py` | 488 | `cla.yml` |
| `ci/changelog.py` | 313 | `release.yml`, `ci.yml`, `build.yml` |
| `tools/hooks/pre-push-secret-scan.sh` | 237 | pre-push-hook |
| `ci/loop_roadmap.py` | 166 | `loop-roadmap.yml` |
| `ci/windows-measure.sh` | 152 | `windows-measure.yml` |
| `ci/domains.py` | 96 | `ci.yml` (welke jobs draaien) |
| `tools/gen_code_language_freq.py` | 77 | met de hand; maakt `internal/codelang/data/` |
| `ci/spa-smoke.sh` | 64 | `build.yml`, `ci.yml` |

Tests: 87 testbestanden buiten `tests/pack/python/`, ongeveer 2930 tests. Ruwe indeling:

- `tests/pack/` (ongeveer 23.000 regels): hooks als black box, skilltekst als contract,
  taalpakketten `go`, `shell`, `node`, `expo`. Een deel test Python-modules die #285 weghaalt
  (`test_config.py`, `test_headings.py`, `test_datadir.py`, `test_hook_lib.py`), en
  `test_skill_scripts_gone.py` is een negative-existence-test.
- `tests/` in de root (ongeveer 8.600 regels): de scripts uit de tabel, pins in workflows,
  instructiebestanden, dogfood.
- 14 site-tests (`tests/test_site_*.py`, `tests/_site_node.py`): die roepen nu al `node` aan via
  `subprocess` om `docs/site/*.js` te testen.
- Python-bestanden als testdata: `tests/pack/fixtures/code_language/**/*.py`.

## Aanpakken

- **A. Alles 1-op-1 naar Go.** Voorspelbaar, maar duur, en het dupliceert wat `_test.go` al dekt.
  Haalt het tweede doel niet.
- **B. Per testbestand sorteren.** Elke test krijgt een bestemming; alleen wat iets bewijst, wordt
  geport. **Gekozen** (besluit gebruiker).
- **C. Alleen `ci/` en `tools/`.** Goedkoop, maar de pytest-shards blijven en het doel wordt niet
  gehaald.

## Ontwerp

### 1. Triage per test

Elke test buiten `tests/pack/python/` krijgt één bestemming:

| Bestemming | Wanneer |
|---|---|
| **weg — onderwerp weg** | Wat hij test, verdwijnt (met #285 of in deze milestone). |
| **weg — gedekt** | Een bestaande `_test.go` faalt op dezelfde productiewijziging. Noem die test. |
| **weg — beschermt niets** | Geen antwoord op de kernvraag, of een rode vlag uit `lighthouse-test-standard`. |
| **naar Go** | Hij beantwoordt de kernvraag en niets anders dekt het. |
| **naar `node --test`** | Alleen de site-tests (§5). |

De kernvraag komt uit `lighthouse-test-standard`: *welke productiewijziging laat deze test
falen?* Hij geldt per testfunctie, niet per bestand: een bestand dat naar Go gaat, neemt alleen
de functies mee die hem beantwoorden. Geparametriseerde gevallen tellen als één test als ze op
dezelfde mutatie falen.

De triage staat per issue in de PR-body, als tabel: bestand, aantal tests, bestemming per test of
per groep, en één zin reden. Bij **weg — gedekt** de naam van de Go-test. De totalen (tests
voor, geport, weg) gaan in de afronding (§8).

### 2. Skillcontracten

Ongeveer 80 testbestanden lezen skill- of instructietekst en pinnen woorden. Voor een
instructiebestand is de tekst het product (`lighthouse-test-standard`, *Contract tests*), dus
die tests mogen blijven bestaan, als ze een blijvende invariant bewaken. Een pin op een zin die
geen gedrag stuurt, of een "staat nog steeds niet in de tekst" na een migratie, gaat weg.

Ze landen in één Go-pakket dat `pack/` leest via een relatief pad. `go test` neemt gelezen
bestanden mee in zijn cachesleutel, dus een gewijzigde `SKILL.md` maakt de cache ongeldig. Het
pakket en zijn naam kiest de bouwer.

### 3. Hooks en scripts als black box

Een test die een hook of een geïnstalleerd script draait, gaat naar Go als hij iets bewijst dat de
unit-tests in `internal/` niet bewijzen: de registratie, de aanroep via `run.sh`, de
samenwerking met git. De `go-e2e`-job is daar de plek voor. De payloads van #285
(`tests/pack/fixtures/hooks/`) blijven de gedeelde fixtures.

### 4. Scripts in `ci/` en `tools/`

Elk script wordt een `main`-pakket in dezelfde Go-module, aangeroepen met `go run ./ci/<naam>` of
`go run ./tools/<naam>`. Ze gaan niet in de `lighthouse`-binary: dat is gereedschap voor deze
repo, niet voor gebruikers. *(eigen keuze)* Hun tests gaan mee als `_test.go`, na triage.

**Uitzondering: `checkpoint_scan`.** Die draait niet alleen in CI. `pre-push-secret-scan.sh` roept
hem vier keer per push aan, en de systemd-unit van de host draait hem
(`docs/secret-scan.md`). Met `go run` compileert elke push de scanner, en heeft de host een
Go-toolchain nodig. Hij wordt daarom een subcommando `lighthouse checkpoint-scan`, verborgen uit
`--help`: de binary staat op beide plekken al. `docs/secret-scan.md` en de unit gaan mee.
`install.sh` valt bij een oude branch terug op het script van de main-checkout; na de port valt
hij terug op de binary. *(eigen keuze na builder-check)*

`ci/changelog.py` eerst: het zit in het releasepad (`release.yml`). Een port die anders uitvoert,
breekt de volgende release.

`tools/gen_code_language_freq.py` draait alleen met de hand; de bouwer port het of haalt het weg
als `internal/codelang/data/` niet meer opnieuw gegenereerd hoeft te worden. Dat laatste beslist
de gebruiker in het issue.

### 5. Site-tests

De site-tests testen JavaScript, dus ze worden `node --test`-tests naast `docs/site/`, zonder
`package.json` en zonder afhankelijkheden. Node staat al in CI (`spa-smoke.sh`). Wat in
`test_site_pages.py` HTML parst, doet de bouwer in Node zonder externe parser, of met een
regex als dat dezelfde mutaties vangt.

`docs/getting-started.md` en `CHANGELOG.md`-tekst die nog komt, noemen een commando zonder Python
om `docs/site/` lokaal te bekijken. Welk, kiest de bouwer; het moet werken met wat een
lighthouse-ontwikkelaar al heeft (Go of Node). Bestaande `CHANGELOG.md`-regels blijven staan.

### 6. Shellscripts

`ci/spa-smoke.sh`, `ci/windows-measure.sh` en `tools/hooks/pre-push-secret-scan.sh` vervangen hun
`python3`-aanroepen door shell, `jq` of de Go-port van §4. Wat ze testen, verandert niet.

Uitzondering: `ci/windows-measure.sh` meet welke tools een Windows-runner heeft
(`for p in python3 python py uv jq`). Die probe gebruikt Python niet, hij kijkt of het er is. Hij
blijft staan; *Bewijs* telt hem niet mee. *(eigen keuze na builder-check)*

### 7. CI

`ci/domains` (Go) beslist welke jobs draaien. De drie pytest-shards verdwijnen. Er komt één
job die alleen `tests/pack/python/` draait. Die tests bouwen de Go-binary (`lighthouse_bin` in
`tests/conftest.py`) en draaien hooks van `pack/core`, dus de job draait als `pack/python/`,
`tests/pack/python/`, `pack/core/`, `cmd/` of `internal/` geraakt is. De `uv`-installatie in
`go-e2e` voor de e2e-tests van `pack/python` gaat naar die job. De gedeelde helpers
(`tests/pack/_testkit.py` en wat `tests/pack/python/` nog importeert) verhuizen naar
`tests/pack/python/`. `pyproject.toml` en `uv.lock` houden alleen wat die job nodig heeft.
*(eigen keuze na builder-check)*

**Meten.** `ci.yml` draait alleen op `pull_request`, en `ci/domains` kiest de jobs per PR. Issue 1
voegt daarom `workflow_dispatch` toe met een invoer die alle domeinen aanzet, en neemt de
nulmeting: drie runs op `main` vóór er iets geport is. De afronding meet op dezelfde manier. Na
mag de wandklok van de run niet hoger zijn dan de nulmeting; de som van de runnerminuten moet
lager zijn. *(eigen keuze na builder-check)*

### 8. Volgorde

Na #285: dat haalt Python-modules weg, en hun tests met de triage **weg — onderwerp weg**. Daarna
`ci/changelog` eerst, dan de rest parallel. De afronding als laatste.

## Opdeling in issues

1. `ci/changelog` naar Go, met zijn tests na triage. Ook: `workflow_dispatch` met alle domeinen in
   `ci.yml`, en de nulmeting (§7).
2. `ci/domains` en `ci/loop_roadmap` naar Go.
3. `tools/check_cla` naar Go, en `checkpoint_scan` als `lighthouse checkpoint-scan` (§4), met
   `docs/secret-scan.md` en de systemd-unit.
4. `tools/gen_code_language_freq`: port of weg, op besluit van de gebruiker.
5. De shellscripts zonder `python3` (§6).
6. Triage en port: de root-tests van `tests/` (pins, instructiebestanden, dogfood).
7. Triage en port: `tests/pack/` hooks en scripts als black box (§3).
8. Triage en port: `tests/pack/` skillcontracten (§2).
9. Triage en port: `tests/pack/go`, `shell`, `node`, `expo`.
10. Site-tests naar `node --test`, en het commando om de site te bekijken (§5).
11. Afronden: CI zonder pytest-shards, `pyproject.toml` en `uv.lock` ingekort, de meting (§7) en
    de totalen van de triage.

## Bewijs

- `git ls-files '*.py'` geeft alleen paden onder `pack/python/` en `tests/pack/python/`.
- `grep -rn python3 .github/ ci/ tools/` geeft alleen de `pack/python`-job en de toolprobe van
  `ci/windows-measure.sh`.
- `go test ./...` en `node --test docs/site/` zijn groen; de `pack/python`-job ook.
- De meting van §7 staat in de PR van de afronding: wandklok niet hoger, runnerminuten lager.
- Elke PR die tests weghaalt, heeft de triagetabel van §1.

## Aan de bouwer

- Lees `lighthouse-test-standard` en `lighthouse-go-standard` vóór de eerste test.
- Een test die je wilt porten maar niet kunt laten falen met een mutatie, port je niet.
- Twijfel tussen **weg — gedekt** en **naar Go**: mutateer de productiecode en draai de
  Go-test. Faalt hij, dan is het gedekt.
