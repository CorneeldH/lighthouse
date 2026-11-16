# Web-UI in de module-source van een tag — ontwerp (#349)

## Waarom

`go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<tag>` levert nu een binary die alleen
de placeholder-pagina serveert. `internal/web/web.go` embedt `all:dist`, maar
`/internal/web/dist/*` staat in `.gitignore`. De module-source van een tag bevat dus alleen
`dist/.gitkeep`. Alle installatieroutes schrijven `~/go/bin/lighthouse`. Eén `go install` van
een tag vervangt daardoor een release-binary, en de systemd-unit serveert daarna de placeholder.

Doel: `go install …@<tag>` levert dezelfde binary als de release, met de gebouwde UI erin.

## Doelen

- `go install …@<tag>` en `…@latest` serveren de SPA: `GET /` geeft
  `<meta name="lighthouse-ui" content="spa" />`.
- De release-binaries en `go install …@<tag>` embedden letterlijk dezelfde bestanden: de `dist`
  in de tag-commit.
- CI faalt als `dist` in een tag ontbreekt of niet past bij `web/` op die tag.

## Niet-doelen

- `go install …@main` of `@<branch>` met UI. Die blijven de placeholder serveren.
- Oude tags (van vóór deze wijziging) achteraf repareren.
- De `Choose`-fallback of de placeholder-pagina veranderen.

## Anti-doelen

- Gebouwde bestanden in PR's of op `main`. Elke UI-PR zou dan wijzigingen in gehashte
  bestandsnamen dragen, en parallelle UI-PR's zouden botsen op `dist/index.html`.
- Een vuile tree tijdens lokale ontwikkeling: `npm run build` mag `git status` niet veranderen.
- Twee bronnen voor de UI van één release (een artifact voor de binaries, een commit voor
  `go install`). Dan kunnen ze uit elkaar lopen.

## Aanpak

Drie manieren overwogen:

1. **`dist` alleen in de tag-commit (gekozen).** De release maakt eerst de changelog-commit
   (C1) en daarbovenop een commit die alleen `internal/web/dist` toevoegt (C2). `main` gaat naar
   C1, de tag wijst naar C2. `main` blijft vrij van build-output. Elke tag draagt een `dist` die
   gebouwd is uit `web/` van die tag. Prijs: `release.yml` maakt twee commits, de tag ligt naast
   `main`, en `@main` blijft een placeholder.
2. **`dist` bij elke release op `main`.** Zoals 1, maar C2 landt ook op `main`. Afgewezen:
   tussen twee releases loopt de getrackte `dist` op `main` achter op `web/`. Een lokale
   `npm run build` maakt de tree dan vuil.
3. **`dist` altijd in de repo.** Elke UI-PR commit `dist` mee, CI controleert de drift per PR.
   Afgewezen om het eerste anti-doel: churn en merge-conflicten in elke UI-PR.

Gekozen door de gebruiker: 1.

## Gemeten

Vite bouwt deterministisch. Drie builds (twee in de worktree, één vanuit een kopie van `web/`
op een ander pad) gaven met `diff -r` identieke output: 412K, met `index.html`, `.gitkeep`, één
JS-bestand, één CSS-bestand en vijf Geist-`woff2`-fonts. Gemeten op één machine en één
Node-versie. In CI gebruiken release en check dezelfde runner-image en dezelfde `setup-node`
uit `web/.nvmrc`.

Gitleaks met `.gitleaks.toml` vindt niets in de gebouwde UI. Gitleaks draait in CI alleen op
pull requests, en een tag-commit zit nooit in een PR. Een allowlist voor `internal/web/dist/` is
niet nodig; `.gitleaks.toml` verbiedt pad-allowlists ook.

`main` heeft geen ruleset en geen branch protection. De atomische push van de bot (C1 naar
`main`, tag op C2) wordt dus niet geblokkeerd.

`ci/changelog.py` vindt de vorige versie met `git tag --list v*`, niet via de voorouders van
`main`. Een tag naast `main` breekt de versiebump dus niet. `internal/version` meldt `dev` voor
elke pseudo-versie; een build van `main` blijft dus `dev`, ook nu geen tag meer een voorouder
van `main` is.

## Werking

### Eén script: `ci/tag-with-ui.sh <tag> <dist-bron>`

Bouwt zelf niet. `<dist-bron>` is een map buiten de worktree (`$RUNNER_TEMP/ui-dist`, waar de
workflow het artifact van job `ui` neerzet). Het script kopieert die map naar `internal/web/dist`,
voegt hem geforceerd toe (`git add -f`), commit dat als `chore(release): <tag> web-UI` en zet een geannoteerde tag
`<tag>` op die commit. Het script pusht niet. Het faalt (exit ≠ 0) als
`<dist-bron>/index.html` ontbreekt.

Twee gebruikers, allebei een workflow: `release.yml` en `prerelease.yml`. Zo wordt elke tag die
`build.yml` bouwt op een runner gemaakt, met dezelfde Node-versie als de controle. Er bestaat
geen lokaal gemaakte tag meer: een lokale build kan door een andere Node-, npm- of OS-versie
afwijken en dan de controle in `build.yml` laten falen.

### `prerelease.yml` (nieuw)

Vervangt het met de hand pushen van een prerelease-tag (gekozen door de gebruiker).

- `workflow_dispatch` met invoer `tag`. De workflow weigert alles wat geen prerelease-vorm
  `vX.Y.Z-<suffix>` heeft, zodat hij nooit een gewone release-tag zet.
- Job `ui` (`contents: read`): checkt de ref uit waarvan de dispatch gestart is, zet `setup-node`
  uit `web/.nvmrc`, bouwt de SPA en uploadt `dist` als artifact.
- Job `tag` (`contents: write`, needs `ui`): checkt dezelfde ref uit, downloadt het artifact, draait
  `ci/tag-with-ui.sh "$tag"` en pusht alleen de tag
  (`git push origin "refs/tags/$tag"`). Geen branch-push. Zie *Geen npm-code met schrijftoken*
  hieronder voor wanneer het token bereikbaar is.
- Job `build` (`contents: write`: `build.yml`-job `publish` vraagt dat, en een aangeroepen
  workflow krijgt nooit meer rechten dan de aanroeper geeft; zoals in `release.yml`):
  roept `build.yml` aan met de tag, zoals `release.yml` dat doet. Een tag die met
  `GITHUB_TOKEN` gepusht is, start namelijk geen push-workflow.

### Geen npm-code met schrijftoken

`npm ci` en `npm run build` (install-scripts, Vite-plugins) mogen niet verhuizen naar de
schrijvende jobs `release` (`release.yml`) en `tag` (`prerelease.yml`). Tot nu toe draaide npm
alleen in `build.yml`-job `web` met `contents: read` en `persist-credentials: false`. Eis: `npm ci` en `npm run build` draaien nooit in een job die later een schrijftoken
vasthoudt (een stap in dezelfde job kan via een geplante git-hook of `$GITHUB_ENV` code laten
draaien in de latere push-stap). Een aparte job met `contents: read` en
`persist-credentials: false` bouwt `internal/web/dist` en geeft het als artifact door aan de job die
het met `git add -f` commit, tagt en pusht. `ci/tag-with-ui.sh` krijgt de gebouwde `dist` dus als
invoer en bouwt zelf niet; de schrijvende job draait geen npm en geen `setup-node`. Een bron voor de
UI blijft de tag-commit: binaries en `go install` embedden wat C2 bevat. De pin-test legt vast dat
de schrijvende jobs geen `npm` en geen `setup-node` bevatten.
*(own choice after spec review, r2-f1)* *(r1-f3: een gecompromitteerde npm-dependency kon anders naar `main` en tags pushen; `main` heeft
geen ruleset.)*

### `release.yml`, job `release`

- Een nieuwe job `ui` (`contents: read`, `persist-credentials: false`, `actions/setup-node` met
  `node-version-file: web/.nvmrc` en npm-cache, zoals de `web`-job van `build.yml`) bouwt `dist` op
  `TESTED` en geeft het als artifact door. Job `release` krijgt geen `setup-node`, hangt van `ui` af
  en downloadt het artifact. Geen npm in een schrijvende job: zie boven.
- Na `git commit -m "chore(release): $tag"` (C1): onthoud `main_sha=$(git rev-parse HEAD)`,
  draai `ci/tag-with-ui.sh "$tag"` (C2 plus tag).
- De push wordt `git push --atomic origin "$main_sha:refs/heads/main" "refs/tags/$tag"`.
  Commit en tag landen nog steeds samen of niet.
- De controle "is onze tag op de remote?" na een mislukte push blijft zoals hij is.
- De build staat buiten de retry-lus (in job `ui`), dus `timeout-minutes: 10` van `release`
  blijft genoeg *(own choice after spec review, r1-f5)*.
- Bij een nieuwe poging reset de lus naar `origin/main`. Na een mislukte
  poging is HEAD C2 en volgt `git reset --hard origin/main` het gevolgde `dist` weg. Daarom leeft
  het artifact buiten de worktree en kopieert `ci/tag-with-ui.sh` het bij elke poging opnieuw
  *(own choice after spec review, r3-f1)*.

De macOS-poort test nog steeds `TESTED` (= C1's ouder). C1 voegt alleen changelog toe, C2 alleen
`dist`.

### `build.yml`

- Job `web` bouwt niet meer voor een artifact, maar controleert de tag:
  1. `git ls-files --error-unmatch internal/web/dist/index.html` — faalt als de tag geen UI
     draagt.
  2. `npm ci && npm run build` in `web/` (`contents: read`, geen schrijftoken).
  3. `test -z "$(git status --porcelain --ignored -- internal/web/dist)"` — faalt als de
     opnieuw gebouwde UI afwijkt van de UI in de tag. `--ignored` is nodig: een nieuw bestand
     in `dist` is genegeerd en zou anders onzichtbaar blijven.

  Dit is het patroon van de orval-drift-check in `ci.yml`.
- Job `build` (de matrix) downloadt geen `spa-dist` meer. Hij bouwt uit de checkout van de tag,
  waar `dist` getrackt is. De controle op een schone tree blijft vóór de build staan en dekt nu
  ook `dist`. `ci/spa-smoke.sh` blijft. `build` hangt nog af van `web`, zodat een tag zonder
  passende UI geen binaries oplevert.
- Nieuwe job `go-install` (needs `resolve` en `web`, `contents: read`): checkt de tag uit
  (`ref: refs/tags/${{ needs.resolve.outputs.tag }}`, `persist-credentials: false`; nodig voor
  `ci/spa-smoke.sh` en `go.mod`), zet `actions/setup-go` met `go-version-file: go.mod` en
  installeert met
  `GOPRIVATE=github.com/CorneeldH/lighthouse GOBIN="$RUNNER_TEMP/bin" go install github.com/CorneeldH/lighthouse/cmd/lighthouse@$TAG`
  en draait `ci/spa-smoke.sh` op die binary. Dat bewijst criterium 1 bij elke release.
  `GOPRIVATE` impliceert `GONOPROXY` en `GONOSUMDB` voor alleen onze module: de tag komt
  rechtstreeks van GitHub, zonder te wachten op de proxy, en een prerelease-tag komt niet in de
  checksum-database. Geen globale `GOPROXY=direct`: dependencies (o.a. `modernc.org/*`) en de
  Go-toolchain komen dan nog via de proxy, niet via volledige VCS-clones.
  *(own choice after spec review, r1-f1)*
- `publish` hangt ook af van `go-install`: geen GitHub-release als `go install` geen UI levert.

### `.gitignore`, Vite en `ci.yml`

Ongewijzigd. `dist` blijft op `main` genegeerd. `ci.yml` raakt de tag niet; de `binary`-job
blijft de SPA zelf bouwen.

### Tests

`tests/test_release_workflows_pin.py` pint nu het artifact `spa-dist` in `build.yml`
(`test_build_embeds_real_spa`). Die pin gaat weg; nieuwe pins:

- `release.yml` roept `ci/tag-with-ui.sh` aan en pusht `$main_sha:refs/heads/main` met de tag
  in één `--atomic` push.
- `build.yml`-job `web` bevat de drie controles hierboven; de matrix heeft geen
  `download-artifact` voor `spa-dist` meer.
- `build.yml` heeft een job `go-install` met `ci/spa-smoke.sh`, en `publish` hangt ervan af.
- `prerelease.yml` weigert een tag zonder prerelease-suffix, roept `ci/tag-with-ui.sh` aan,
  pusht alleen `refs/tags/$tag` en roept `build.yml` aan. Zijn jobs `tag` en `build` komen op de
  allow-list van jobs met schrijfrechten in de pin-test; `prerelease.yml` komt in de lussen van de
  pin-, schrijfrecht- en credential-tests (nu alleen `release.yml`, `build.yml`, `macos.yml`), en
  de credential-pin telt `persist-credentials` volgens *Geen npm-code met schrijftoken*.
- `test_build_checkouts_pin_the_tag` telt vier checkouts (de nieuwe `go-install` pint de tag ook).
- Geen npm-code met schrijftoken (zie hierboven) is vastgelegd in de pin-test.

`ci/tag-with-ui.sh` krijgt een test die het script in een tijdelijke git-repo draait (met een
`<dist-bron>`; en een tweede aanroep na `git reset --hard` van de eerste) en controleert: de tag wijst naar een commit met `internal/web/dist/index.html`,
de ouder van die commit bevat `dist` niet, en het script faalt zonder `index.html`.

De bestaande `internal/web/web_test.go` dekt criterium 3 al: zonder `dist/index.html` kiest
`Choose` de placeholder.

### Documentatie

- `docs/install.md`: "Install with Go" zegt dat `go install …@<tag>` en `@latest` de UI
  meeleveren, en dat `@main` de placeholder serveert. De alinea "the last one wins" noemt alleen
  nog een build van `@main` of een clone zonder `npm run build` als route naar de placeholder.
  De regel bij `content="placeholder"` (rond regel 139) idem.
- `docs/site/install.html`: de zin over `go install` volgt `docs/install.md`.
- `docs/releasing.md`, *The release commit*: twee commits. `main` krijgt de changelog-commit, de
  tag wijst naar de commit daarboven die `internal/web/dist` toevoegt. De tag is dus geen
  voorouder van `main`.
- `docs/releasing.md`, *Testing the build*: een prerelease-tag maak je door `prerelease.yml` te
  starten met `vX.Y.Z-rc.N`, vanaf de branch die je wilt testen. Een met de hand gepushte tag
  zonder `dist` laat `build.yml` falen.
- `docs/releasing.md`, *Recovering a failed release*: faalt de controle van job `web` op een tag
  die al gepusht is, dan is de uitweg een nieuwe patch-release (na het herstel van de oorzaak),
  nooit een verplaatste of opnieuw gezette tag. Dat geldt ook als `build` later opnieuw draait en
  de build dan afwijkt, bijvoorbeeld door een nieuwere runner-image. `build` opnieuw draaien werkt
  alleen voor tags van na deze wijziging.

## Vastgelegd

- Een build vanuit een clone van `main` zonder `npm run build` serveert de **placeholder**
  (criterium 3).
- `@main` serveert de placeholder; alleen tags dragen de UI.
- `build.yml` bouwt de UI niet meer apart: de binaries embedden de `dist` uit de tag.
- Een tag zonder `dist`, of met een `dist` die niet bij `web/` past, laat `build.yml` falen
  voordat er binaries of een release zijn.
- Elke tag ontstaat op een runner, via `release.yml` of `prerelease.yml` (keuze van de
  gebruiker).
- Faalt de controle op een gepushte tag, dan volgt een nieuwe patch-release; een tag wordt nooit
  verplaatst (mijn keuze, past bij "nooit een tag met de hand").

## Aan de bouwer

- De exacte vorm van `ci/tag-with-ui.sh` (shell volgens de shell-standaard van de repo, als die
  er is) en de commitboodschap van C2.
- Of `go-install` een prerelease-tag ook controleert (ja, tenzij dat in de praktijk faalt).
- Een fragment in `changelog.d/`.

## Bijgewerkte acceptatiecriteria van #349

- [ ] `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<tag>` van een nieuwe tag levert
  een binary waarvan `lighthouse serve` de SPA serveert: `GET /` geeft
  `<meta name="lighthouse-ui" content="spa" />`. De job `go-install` in `build.yml` bewijst dat
  per tag.
- [ ] De release-workflow blijft groen, en de release-binaries dragen dezelfde UI: de `dist` uit
  de tag-commit.
- [ ] Een build vanuit een clone zonder `npm run build` serveert de placeholder.
- [ ] `docs/install.md` ("Install with Go" en de alinea over "the last one wins") zegt niet meer
  dat `go install` van een tag alleen een placeholder serveert.
- [ ] `build.yml` faalt als `internal/web/dist/index.html` niet in de tag staat, of als een
  nieuwe build van `web/` op die tag afwijkt van de `dist` in de tag.
