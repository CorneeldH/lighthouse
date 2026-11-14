# macOS-tests van de PR naar de releasepoort (#337)

## Doel en waarom

Sinds #90 draaien `python` (drie delen), `go-test` en `go-e2e` op elke PR ook op `macos-latest`:
vijf macOS-jobs per PR. De dispatcher merget pas bij `statusCheckRollup == SUCCESS`
([decide.go](../../../../internal/dispatch/decide.go), `greenPR`), dus elke PR wacht op macOS.

Gemeten over de ci-runs van 2026-10-15 tot 2026-11-14 (57 runs met macOS-jobs):

- Een PR wacht na de rest nog 3,5 min (p50) tot 7,8 min (p90) op macOS, met 17,6 min als
  maximum. In de wachtrij staat macOS tot 11,4 min; ubuntu wacht vrijwel nooit.
- 13 macOS-jobs in 10 runs waren rood terwijl dezelfde job op ubuntu groen was. Geen van die
  failures was een productbug: vier keer een exit 1 terwijl alle tests slaagden, vier keer timing
  of een krappe runner, vier keer een testaanname die op macOS niet klopt (`/private/var`-symlink,
  niet-UTF-8-bestandsnamen op APFS), één keer het netwerk. Elke rode run start een fix-pr-ronde
  die niets met de PR te maken heeft.

Besloten (eigenaar, 2026-11-14): macOS gaat van de PR af en wordt een poort vóór de release. Een
mac-regressie mag `main` halen, maar geen release.

## Doelen

- Een PR draait geen macOS-job meer en wacht er dus ook niet op.
- Een release komt alleen van een commit waarvan de macOS-tests groen waren.
- Rood op macOS laat niets half achter: geen tag, geen changelog-commit. De open fragmenten gaan
  mee met de volgende groene release.
- De macOS-tests zijn met de hand te starten op elke branch, voor een PR die bewust aan
  portabiliteit raakt.

## Non-goals

- De bekende flake-soorten oplossen: #338, direct na dit issue.
- Windows: `windows-measure.yml` blijft zoals het is.
- Een native smoke-test van de darwin-binary in `build.yml`.

## Anti-goals

- Een PR die op een macOS-runner wacht.
- Een release van een commit die macOS niet groen heeft getest, ook niet als `main` tijdens de
  macOS-run verder is geschoven.

## Aanpakken

- **A. Herbruikbare `macos.yml`, als eerste job in `release.yml` (gekozen).** Eén workflow-run
  doet test en release; cron en handmatige start gaan langs dezelfde poort; opnieuw proberen is
  `gh run rerun --failed`.
- **B. macOS-test in `build.yml`, na het taggen.** Afgevallen: tag en changelog-commit staan dan
  al op `main` voordat macOS draait, rood geeft een tag zonder release.
- **C. Losse `macos.yml` bij een push naar `main`, `release.yml` via `workflow_run`.** Afgevallen:
  cron en handmatige start lopen dan via een andere route, en `changelog.py --event` krijgt een
  ander event. Meer bewegende delen voor hetzelfde resultaat.

## Ontwerp

1. **`.github/workflows/macos.yml`**, nieuw, met `workflow_call` en `workflow_dispatch`,
   `permissions: contents: read`. Jobs `python` (matrix `shard: [1, 2, 3]`), `go-test` en
   `go-e2e` op `macos-latest`, met dezelfde stappen en flags als in `ci.yml` nu. Zonder de
   `ci/literal-grep.sh`-stap: die draait alleen op ubuntu. Geen `changes`-filter: de poort test
   altijd alles.
2. **`release.yml`**: een job `macos` die `macos.yml` aanroept; job `release` krijgt
   `needs: macos`. Die regel geldt voor push, cron en `workflow_dispatch`.
3. **Release alleen wat getest is.** De job `release` checkt `ref: main` uit en reset op
   `origin/main`. Wijkt `origin/main` af van de commit die `macos` testte, dan stopt de job
   zonder release en zonder fout, met een regel in de log. Dat is veilig: een nieuwere menselijke
   push start een eigen release-run, en een bot-commit van `release` zelf laat geen fragmenten
   achter. Hetzelfde geldt in de herhaallus wanneer de push geweigerd is omdat `main` bewoog.
4. **`ci.yml`**: `python`, `go-test` en `go-e2e` alleen nog op `ubuntu-latest`. De
   `matrix.os` verdwijnt, de `shard`-matrix blijft. De kopcommentaar en het commentaar bij
   `timeout-minutes` van `python` noemen macOS niet meer.
5. **Pin-tests.** `tests/test_ci_domains_pin.py::test_tests_run_on_linux_and_macos` wordt: de
   drie jobs staan in `macos.yml` op `macos-latest`, en `release` in `release.yml` heeft
   `needs: macos`. `tests/test_ci_go_test_pin.py` controleert ook de `go test`-stappen in
   `macos.yml` op de flags van de gate.
6. **Bij rood** geen automatisch issue. De rode run in Actions en de standaard faal-mail van
   GitHub zijn het signaal. Een bot die issues opent, vraagt `issues: write` en maakt bij elke
   rode push een dubbel. (Voorstel, niet expliciet bevestigd door de eigenaar.)
7. **Docs.** [releasing.md](../../../releasing.md) krijgt een korte sectie over de macOS-poort:
   wat rood betekent en hoe je opnieuw probeert. Een fragment in `changelog.d/`.

## Aan de bouwer

- Hoe `macos` de geteste SHA doorgeeft aan `release` (job-output of `github.sha`).
- Of `timeout-minutes` van `python` in `ci.yml` terug kan van 15 naar 10.
- De precieze tekst van de logregel bij een afgewezen release.
