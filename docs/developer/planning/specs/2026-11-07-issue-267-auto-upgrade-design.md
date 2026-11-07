# Automatische upgrade van de lighthouse-binary — ontwerp

Issue: #267. Grootte: kleine wijziging.

## Waarom

Op de Beelink draaide de controller `v0.10.1` terwijl de fix van #263 al in `v0.11.3` zat.
Upgraden is handwerk ([install.md](../../../install.md), [releasing.md](../../../releasing.md#upgrading)),
dus een gemergde fix bereikt de controller pas als iemand eraan denkt. Doel: een nieuwe release
staat binnen een dag vanzelf op de host.

## Doelen, niet-doelen, anti-doelen

- Doel: elke dag de nieuwste release ophalen, de checksum controleren en `~/go/bin/lighthouse`
  atomair vervangen, alleen bij een nieuwere tag. Daarna `lighthouse.service` herstarten.
- Niet-doel: `lighthouse init --upgrade` in de repo's (#46). Windows.
- Anti-doel: een upgrade die een werkende binary vervangt door een kapotte of halve download, of
  een eigen build (`dev`) overschrijft.

## Besloten met de gebruiker

- **Meteen live.** Een nieuwe release gaat bij de eerstvolgende dagelijkse run live, zonder
  wachttijd en zonder onderscheid tussen patch en minor.
- **Niet wachten op runs.** De upgrade wacht niet tot er geen run meer loopt. De tick start de
  binary elke 5 minuten opnieuw; een atomaire `mv` raakt een lopende tick niet.
- **In lighthouse**, niet in paia: `deploy/systemd/`, voor elke host.

## Aanpakken

| Aanpak | Afweging | Oordeel |
|---|---|---|
| Go-subcommando `lighthouse self-update`, aangeroepen door een dunne unit | logica testbaar met `httptest`; werkt op elke host met de release-binary | **gekozen** (keuze van de bouwer, gebruiker liet het hoe over) |
| Shellscript in `deploy/` (curl, `sha256sum`) | minder Go; test via pytest met stubs op `PATH`; versievergelijking in shell is broos | afgevallen |
| Units in paia `deploy/beelink` | alleen voor de Beelink | afgevallen door de keuze van de gebruiker |

## Ontwerp

**`lighthouse self-update`** (nieuw subcommando in `internal/cli`, logica in een eigen package):

1. Huidige versie via `version.String()`. Is die `dev`: melden `self-update: skipped — dev build`,
   exit 0, niets aanraken.
2. Nieuwste release via `GET https://api.github.com/repos/CorneeldH/lighthouse/releases/latest`
   (`tag_name`; die endpoint slaat drafts en prereleases over). Basis-URL en repo zijn in de test
   te vervangen.
3. Tag niet nieuwer dan de huidige (semver-vergelijking op `vX.Y.Z`): melden
   `self-update: up to date (<tag>)`, exit 0.
4. Download `lighthouse_<GOOS>_<GOARCH>` en `SHA256SUMS` van die tag. Ontbreekt de regel voor het
   asset, of klopt de hash niet: fout, exit ≠ 0, oude binary blijft.
5. Doel is `os.Executable()` na `filepath.EvalSymlinks`. Schrijf naar een tijdelijk bestand in
   dezelfde map, `chmod 0755`, `os.Rename` over het doel. Bij elke fout vóór de rename: tijdelijk
   bestand weg, oude binary blijft.
6. Met `--restart <unit>` en alleen na een vervanging: `systemctl --user try-restart <unit>`.
   Mislukt dat, dan exit ≠ 0 met de melding; de nieuwe binary blijft staan.
7. Op Windows: weigeren met een melding, exit ≠ 0.

Elke uitkomst is één regel op stdout of stderr, zodat de journal hem toont.

**Units** in `deploy/systemd/`:

- `lighthouse-upgrade.service`: `Type=oneshot`,
  `ExecStart=%h/go/bin/lighthouse self-update --restart lighthouse.service`, dezelfde hardening als
  `lighthouse.service` met `ReadWritePaths=%h/go/bin`. Kopcommentaar met *Why*-regels en de
  installatiestappen, zoals `lighthouse.service`.
- `lighthouse-upgrade.timer`: `OnCalendar=daily`, `RandomizedDelaySec=1h`, `Persistent=true`.

**Docs**: `docs/install.md` (aanzetten: units kopiëren, `systemctl --user enable --now
lighthouse-upgrade.timer`; uitzetten: `disable --now`; welke versie draait: `lighthouse version`
en `/api/health`) en `docs/releasing.md` *Upgrading* (stap 1 en 2 doet de timer, als die aanstaat).

**Tests** (Go, `httptest`, tijdelijke map als doel): vervangt bij nieuwere tag en goede checksum;
vervangt niet bij verkeerde checksum, ontbrekende `SHA256SUMS`-regel, gelijke tag, oudere tag of
`dev`; `--restart` roept de restart alleen na een vervanging aan (runner injecteerbaar).

**Changelog**: fragment in `changelog.d/`.

**Op de Beelink**: na de merge en de release de units installeren en de timer aanzetten; bewijs
`systemctl --user list-timers` en een journal-regel.

## Aan de bouwer

- Semver: eigen kleine vergelijking of `golang.org/x/mod/semver`, volgens `/lighthouse-go-standard`.
- De exacte foutmeldingen en de package-naam.
- GitHub-API zonder token (60 verzoeken per uur volstaat voor één per dag); een `GH_TOKEN` in de
  omgeving mag mee, hoeft niet.

## Risico

Elke release gaat zonder review live op de controller. De checksum vangt een kapotte download,
geen slechte release. Terug naar een vorige versie gaat met de hand, zoals nu.
