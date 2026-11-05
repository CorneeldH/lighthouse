# Release-binaries met de echte SPA (#78) — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** de vijf releasebinaries van `build.yml` bevatten de echte Vite-build in plaats van de placeholder, een smoke-stap bewijst dat op de linux/amd64-binary, en `docs/install.md` beschrijft installeren als downloaden.

**Afbakening:** #86 leverde het releasebeleid ([`docs/releasing.md`](../../../releasing.md)), de releaseworkflow en de binaries met placeholder. Het AC over het releasebeleid is daarmee afgedekt; dit plan voegt alleen de frontendbuild, de SPA-smoke en de installgids toe.

**Architectuur:** een nieuwe job `web` in `build.yml` checkt de tag uit, bouwt `web/` (Node uit `web/.nvmrc`, `npm ci && npm run build`, uitvoer in `internal/web/dist`) en uploadt die map als artifact `spa-dist`. Elke `build`-matrixjob downloadt het artifact naar `internal/web/dist` vóór `go build`. `internal/web/dist/*` staat in `.gitignore`, dus de boom blijft schoon en de binary blijft de tag rapporteren (VCS-stempel). Na de versiecontrole draait `ci/spa-smoke.sh` op de linux/amd64-binary.

**Tech stack:** GitHub Actions, Node 22 (`web/.nvmrc`), Go, bash (`ci/spa-smoke.sh`), pytest.

## Globale randvoorwaarden

- Code, workflowcommentaar en `docs/install.md` in het Engels; dit plan en commits in het Nederlands (Conventional prefix).
- Acties op SHA met `# vX.Y.Z`-commentaar; hergebruik de SHA's uit `ci.yml` (checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0, upload-artifact v7.0.2, download-artifact v8.0.2). Elke checkout `persist-credentials: false`, elke job `timeout-minutes`, elke stappenjob `runs-on: ubuntu-latest`.
- `tests/test_release_workflows_pin.py` blijft groen: de matrix, `-trimpath`, `CGO_ENABLED=0`, de uitvoermap `$RUNNER_TEMP/dist/` en de rechten (`contents: write` alleen in `release.release`, `release.build`, `build.publish`) veranderen niet. De nieuwe job krijgt alleen `contents: read`.
- De binaries gaan nooit in de checkout; alleen `internal/web/dist` (genegeerd door git) wordt gevuld. De stap `test -z "$(git status --porcelain)"` blijft vóór de download staan, of blijft groen omdat de map genegeerd is.
- Testgate: `python3 ci/domains.py gate`.
- Het kopregelcommentaar van `build.yml` ("The binary embeds the placeholder SPA, so there is no npm build here.") klopt na deze wijziging niet meer en wordt herschreven.

## Bestanden

- Wijzig: `.github/workflows/build.yml`
- Wijzig: `tests/test_release_workflows_pin.py`
- Wijzig: `docs/install.md`
- Wijzig: `docs/releasing.md` (alleen als er een zin staat die de placeholder noemt; controleer met `grep -n -i placeholder docs/releasing.md`)
- Nieuw: `changelog.d/<fragment>` volgens `changelog.d/.lighthouse` (type `added`, Engelse tekst)

## Taak 1: pintests voor de nieuwe vorm (rood eerst)

**Bestanden:** `tests/test_release_workflows_pin.py`

- [ ] Stap 1: voeg tests toe die falen op de huidige `build.yml`:
  - `test_build_embeds_real_spa`: de job `web` bestaat, bevat `npm ci` en `npm run build` (met `node-version-file: web/.nvmrc`) en uploadt `internal/web/dist`; elke `build`-matrixjob heeft `needs: [resolve, web]`, downloadt dat artifact naar `internal/web/dist` en doet dat vóór de stap `Build`.
  - `test_build_runs_spa_smoke`: de job `build` roept `ci/spa-smoke.sh "$RUNNER_TEMP/dist/lighthouse_linux_amd64"` aan, na `Check version`, met dezelfde `if: matrix.goos == 'linux' && matrix.goarch == 'amd64'`.
  - Pas de bestaande tests aan waar de checkout-telling een extra `persist-credentials: false` vraagt: die telling is al `checkouts - exempt`, dus er is niets te wijzigen zolang de nieuwe checkout `false` heeft.
- [ ] Stap 2: `python3 -m pytest tests/test_release_workflows_pin.py -q`; verwacht: de twee nieuwe tests rood, de rest groen.
- [ ] Stap 3: commit `test(build): pin de SPA-build en SPA-smoke in build.yml (#78)`.

## Taak 2: frontendbuild en smoke in `build.yml`

**Bestanden:** `.github/workflows/build.yml`

- [ ] Stap 1: herschrijf het kopcommentaar: de binary embedt de echte SPA, gebouwd in job `web`.
- [ ] Stap 2: voeg job `web` toe, `needs: resolve`, `permissions: contents: read`, `timeout-minutes: 10`: checkout van `refs/tags/<tag>`, `actions/setup-node` met `node-version-file: web/.nvmrc`, `cache: npm`, `cache-dependency-path: web/package-lock.json`, dan `npm ci` en `npm run build` met `working-directory: web`, daarna `actions/upload-artifact` met `name: spa-dist`, `path: internal/web/dist`, `if-no-files-found: error`.
- [ ] Stap 3: in job `build`: `needs: [resolve, web]`; na de checkout en na `test -z "$(git status --porcelain)"` een stap `actions/download-artifact` met `name: spa-dist`, `path: internal/web/dist`. Controleer lokaal dat `git status --porcelain` na het vullen van die map leeg blijft (`.gitignore` regel `/internal/web/dist/*`).
- [ ] Stap 4: voeg na `Check version` toe, met dezelfde `if`: een stap `Check SPA` die `ci/spa-smoke.sh "$RUNNER_TEMP/dist/lighthouse_linux_amd64"` draait.
- [ ] Stap 5: lokaal bewijs dat de stappen kloppen: `cd web && npm ci && npm run build && cd ..`, dan `CGO_ENABLED=0 go build -trimpath -o "$TMPDIR/lh" ./cmd/lighthouse` en `ci/spa-smoke.sh "$TMPDIR/lh"`; verwacht `spa-smoke: ok (...)`. Draai ook `ci/spa-smoke.sh` op een placeholder-binary (zonder `npm run build`) en bevestig dat die faalt met `the binary embeds no real SPA build`: dat is het bewijs dat de smoke de placeholder afwijst (AC 2).
- [ ] Stap 6: `python3 -m pytest tests/test_release_workflows_pin.py tests/test_site_download.py -q` is groen; `actionlint` draait als het in de repo staat (`grep -rn actionlint .github ci`).
- [ ] Stap 7: commit `feat(build): releasebinaries embedden de echte SPA en draaien de SPA-smoke (#78)`.

Opmerking over de versie: de binary leest zijn versie uit de VCS-info; `git status --porcelain` negeert genegeerde bestanden, dus de gevulde `internal/web/dist` maakt de boom niet vuil en `Check version` (`lighthouse <tag>`) blijft de poort voor AC 3.

## Taak 3: installgids

**Bestanden:** `docs/install.md`, eventueel `docs/releasing.md`

- [ ] Stap 1: in `## Requirements`: Node is alleen nodig om uit een clone te bouwen; zeg dat letterlijk.
- [ ] Stap 2: in `## Download a release binary`: verwijder "serves a placeholder page: it does not carry the web UI (#78)"; schrijf dat de binary de web-UI bevat en `lighthouse serve` hem direct serveert, zonder Node en zonder clone. Voeg de upgraderoute uit het releasebeleid toe of verwijs ernaar (`docs/releasing.md`, relatieve link) en controleer dat die tekst daar al staat.
- [ ] Stap 3: in `## Install with Go`: de tekst "serves only a placeholder page" blijft waar voor `go install` en blijft; voeg één zin toe dat de downloadroute de aanbevolen route zonder clone is.
- [ ] Stap 4: in `## Build from a clone`: label als route voor ontwikkelaars; de slotalinea ("a release binary or tag install replaces a UI build from a clone, and the systemd unit then serves the placeholder page") past aan: een releasebinary serveert nu de UI, alleen een `go install`-van-tag serveert de placeholder.
- [ ] Stap 5: `grep -n -i placeholder docs/install.md docs/releasing.md`; elke resterende vermelding moet over `go install` gaan.
- [ ] Stap 6: markdownlinks zijn relatief en wijzen naar bestaande doelen (de hook `md-link-check.sh` blokkeert anders).
- [ ] Stap 7: commit `docs(install): installeren door een releasebinary te downloaden (#78)`.

## Taak 4: changelogfragment en afronding

- [ ] Stap 1: voeg een fragment toe in `changelog.d/` (naamconventie: kijk naar `ci/changelog.py` en een eerder fragment in `git log -- changelog.d`), type `added`: `Release binaries now embed the real web UI, so `lighthouse serve` works after a plain download.`
- [ ] Stap 2: `python3 ci/changelog.py check` is groen.
- [ ] Stap 3: `python3 ci/domains.py gate` is groen; `go test ./...` blijft groen (geen Go-wijziging verwacht).
- [ ] Stap 4: commit `docs(changelog): fragment voor de SPA in releasebinaries (#78)`.

## Acceptatiecriteria naar taak

| AC | Waar |
|---|---|
| Tag start workflow die frontend bouwt, daarna de binary (≥ `linux/amd64`), en aan de release hangt | Taak 2 (stap 2–3; publicatie bestond al uit #86) |
| Echte SPA in de binary, bewezen door een smoke-stap | Taak 2 (stap 4–5), Taak 1 |
| `lighthouse --version` toont de tag | bestaande `Check version`; blijft bewaakt door Taak 2 stap 3 |
| `docs/install.md` beschrijft downloaden; clone-route voor ontwikkelaars | Taak 3 |
| Releasebeleid ligt vast | al geleverd door #86 ([`docs/releasing.md`](../../../releasing.md)) |

## Verificatie na merge

De eerstvolgende release (of een `vX.Y.Z-rc.N`-tag, zie [`docs/releasing.md`](../../../releasing.md)) draait de nieuwe `web`-job en de smoke-stap; de PR zelf kan `build.yml` niet echt draaien. Dat is het enige onderdeel dat pas na de merge te bewijzen is.
