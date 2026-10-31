# Website met een eenvoudige download (#89)

## Doel en waarom

Een developer die lighthouse niet kent, ziet op één pagina wat het doet, en installeert het met
één klik of één commando. Nu is er alleen de README. De pagina is de ingang voor de DoD van de
milestone *Product: eerste versie voor anderen*: een developer die niet Corneel is, downloadt
lighthouse via de website op Linux of macOS.

De site uit #99 (`docs/site/`, gedeeld kaartcomponent, Pages-workflow) bestaat al en draait op
<https://corneeldh.github.io/lighthouse/>. Dit issue maakt van de index daar de landingspagina:
de download bovenaan, de kaarten eronder, één deploy.

## Doelen

- Een Engelse landingspagina: wat lighthouse zonder controller doet (hooks, test-gate, skills,
  PR met bewijs), voor wie het is, en de vereisten.
- Een downloadblok dat het besturingssysteem herkent en de juiste binary van de laatste release
  aanbiedt, met de andere platforms eronder. De hoofdroute is het installatiecommando met een
  kopieerknop; de losse binary is een kleine link eronder (besluit 2).
- Een installatiecommando per platform dat de checksum controleert voordat de binary op zijn plek
  komt.
- Links naar de startgids, de handleiding voor agents, de licentie en het changelog.
- De pagina volgt de laatste release zonder handwerk bij een nieuwe tag.
- De kaarten blijven op dezelfde pagina, met dezelfde deploy.

## Non-goals

- Geen eigen domein. De site blijft op `corneeldh.github.io/lighthouse/` (besluit Corneel,
  2026-10-31). Dit beantwoordt de eerste vraag onder *Nog niet vastgelegd* van de milestone
  voorlopig; een domein kan later.
- Geen `install.sh` of ander script dat je laat draaien.
- Geen buildstap en geen versienummer dat bij de deploy in de pagina wordt gezet.
- Niet de inhoud van de handleiding voor agents (#88), niet het testen op macOS en Windows (#90),
  niet het verwijderen (#91).
- Geen controller, `lighthouse serve` of SPA op de pagina (No-Go van de milestone).
- Geen nieuwe kaarten; de tegels van #100–#104 blijven zoals ze zijn.

## Anti-goals

- `curl … | sh`, of een commando dat de binary in het `PATH` zet vóór de checksumcontrole.
- Een download die stukgaat als de GitHub-API niet antwoordt of de rate limit bereikt is.
- Een pagina die meer belooft dan getest is: macOS-hooks zijn ongetest, Windows vraagt WSL.

## Randvoorwaarden

- Statisch, zonder buildstap: HTML, CSS en ES-modules in `docs/site/`, zoals #99.
- `pages.yml` blijft ongewijzigd; `tests/test_pages_workflow.py` pint de triggers en vier
  `uses:`-regels.
- De bestaande tests in `tests/test_site_pages.py` blijven gelden: elke relatieve link wijst naar
  een bestand, `<html lang="en">`, geen `innerHTML`, `outerHTML`, `insertAdjacentHTML` of
  `document.write` in site-JS, `index.html` houdt `id="maps"`, de link naar `chain.html` en de
  nummers #100–#104.
- Links naar bestanden buiten `docs/site/` (startgids, licentie, changelog) zijn absolute
  GitHub-URL's: Pages serveert alleen `docs/site/`.
- De assetnamen komen uit `.github/workflows/build.yml`: `lighthouse_linux_amd64`,
  `lighthouse_linux_arm64`, `lighthouse_darwin_amd64`, `lighthouse_darwin_arm64`,
  `lighthouse_windows_amd64.exe` en `SHA256SUMS`.
- De repo is publiek (#93), dus `releases/latest/download/<asset>` en de releases-API werken
  zonder login.

## Aanpakken

1. **Statische links, met een versielabel uit de API (gekozen).** De knoppen en commando's
   gebruiken `https://github.com/CorneeldH/lighthouse/releases/latest/download/<asset>`, dat
   altijd naar de laatste release wijst. Een klein script herkent het OS en haalt
   `https://api.github.com/repos/CorneeldH/lighthouse/releases/latest` op voor het versielabel.
   Faalt die call, dan staat er "latest release" en werkt de download gewoon. Geen buildstap,
   `pages.yml` blijft gelijk.
2. **Versie bij de deploy in de pagina.** `pages.yml` haalt de release op en schrijft het
   nummer in de HTML, met een extra trigger op `release: published`. Meer bewegende delen, een
   gewijzigde workflowtest, en een gemiste trigger laat een oude versie staan.
3. **Een `install.sh` op de site.** Afgevallen: het script moet zelf weer geverifieerd worden
   voordat het draait, en wordt in de praktijk `curl | sh`.

Corneel koos 1: de AC met de minste bewegende delen, en de download werkt ook als de API faalt.

## Besluiten

### 1. Eén pagina: `index.html` wordt de landingspagina

`docs/site/index.html` blijft het enige ingangsbestand. `<title>` en `<h1>` worden "Lighthouse",
met een ondertitel van één zin. De volgorde van boven naar beneden (Corneel koos variant A,
download bovenaan):

1. kop met naam, één zin en de themaknop (bestaat al);
2. download (`id="download"`): het versielabel, het installatiecommando voor het herkende
   platform met een kopieerknop, de link "or download the file", en de tabs met de andere
   platforms;
3. wat het doet: hooks, test-gate, skills, PR met bewijs;
4. voor wie en vereisten;
5. "Why lighthouse?" (bestaande `<details id="why">`, ongewijzigd);
6. de kaarten (bestaande `<section id="maps">`, ongewijzigd);
7. een voet met de vier links.

`chain.html` linkt al naar `index.html` en blijft dat doen.

### 2. Het commando is de hoofdroute, de losse binary een bijzaak

Lighthouse is een terminalprogramma zonder ondertekende installer. Een binary die via de browser
binnenkomt, mist het exec-bit, slaat de checksumcontrole over, en wordt op macOS door Gatekeeper
geweigerd (quarantaine-attribuut, niet ondertekend). Een download met `curl` heeft die
problemen niet. Daarom zet het downloadblok bovenaan het installatiecommando voor het herkende
platform, met een kopieerknop, zoals `uv`, Deno en Bun dat doen. Daaronder staat een kleine link
"or download the file" naar `releases/latest/download/<asset>`, met één regel dat je die zelf
tegen `SHA256SUMS` controleert en `chmod +x` geeft (besluit Corneel, 2026-10-31).

Dit vult het AC "een downloadknop die het besturingssysteem herkent en de juiste binary van de
laatste release aanbiedt" in: het blok herkent het OS en biedt de binary van dat platform aan,
via het commando en via de link.

### 3. Platformherkenning

Een nieuw module-script `docs/site/download.js`, geladen vanuit `index.js` of los in
`index.html`. De kern is een pure functie die uit de browsergegevens één platform kiest, zodat
die zonder browser te testen is.

| Herkend | Bovenaan | Toelichting erbij |
|---|---|---|
| Linux, x86-64 | tab Linux x86-64 | — |
| Linux, ARM (`aarch64`/`arm64` in de UA) | tab Linux ARM | — |
| macOS | tab macOS Apple silicon | "Intel Mac? Take x86-64" met de link naar de tab macOS Intel |
| Windows | tab Linux x86-64, voor WSL | "The hooks need WSL: run this inside WSL"; de Windows-tab met de `.exe` staat bij de andere platforms |
| Anders (mobiel, onbekend) | niets uitgelicht | alle tabs, de eerste open |

Bronnen, in deze volgorde: `navigator.userAgentData` (platform, en waar beschikbaar de
architectuur via `getHighEntropyValues(["architecture"])`), dan `navigator.userAgent` en
`navigator.platform`. Een browser vertelt bij macOS niet betrouwbaar of het Apple silicon is,
daarom is arm64 de standaard op macOS.

Zonder JS toont de pagina alle vijf de platforms met hun commando en hun link; het script
opent de tab van het herkende platform en zet de toelichting erbij. De pagina is dus ook zonder
JS volledig bruikbaar.

### 4. Versielabel

Het script haalt `releases/latest` van de API op en zet `tag_name` met `textContent` boven het
commando ("v0.2.0"). Bij een fout, een time-out of een rate limit (60 per uur per IP zonder login)
staat er "latest release". Het label is informatie; geen link hangt ervan af.
`releases/latest` slaat prereleases over, net als `releases/latest/download/`, dus label en
download wijzen naar dezelfde release.

### 5. Installatiecommando's

Eén tab per platform: Linux x86-64, Linux ARM, macOS Apple silicon, macOS Intel, Windows. Elke
tab is één blok dat je in één keer plakt. De stappen zijn met `&&` geketend, zodat bij een
checksumfout niets in het `PATH` komt. Doel is `~/.local/bin`, zonder `sudo` (besluit Corneel,
2026-10-31; zie besluit 9 voor de startgids). De laatste stap roept de binary met het volle pad
aan: op een verse machine staat `~/.local/bin` vaak pas na een nieuwe login op het `PATH`, en
een kale `lighthouse version` gaf dan "command not found" na een geslaagde installatie.

Linux (x86-64; ARM gelijk met `arm64`):

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
sha256sum --check --ignore-missing SHA256SUMS &&
chmod +x lighthouse_linux_amd64 &&
mkdir -p ~/.local/bin && mv lighthouse_linux_amd64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

macOS gelijk, met `shasum -a 256 -c --ignore-missing SHA256SUMS` en de `darwin`-asset. Onder de
Linux- en de macOS-tabs staat dat `~/.local/bin` op het `PATH` moet staan, met de regel om dat
te doen.

Windows (PowerShell) haalt de `.exe` en `SHA256SUMS` op, vergelijkt `Get-FileHash` met de regel
uit `SHA256SUMS` en stopt met een fout als die niet gelijk zijn. Boven het blok staat dat de
hooks WSL vragen en dat je in WSL de Linux-tab gebruikt.

Na de commando's volgt één zin met de volgende stap: `lighthouse init` in je repo, met een link
naar de startgids.

### 6. Inhoud van de tekstblokken

- **Wat het doet**: vier korte blokken. Hooks die een verkeerde stap tegenhouden (commit op
  `main`, kapotte links), een test-gate bij elke commit, skills die een issue van begin tot PR
  dragen, en een PR-body met bewijs per acceptatiecriterium. Gebaseerd op `docs/getting-started.md`
  en de "Why lighthouse?"-tekst; zonder controller.
- **Voor wie**: een developer met een GitHub-repo en Claude Code. Codex en OpenCode lezen
  `AGENTS.md`, maar daar draaien de hooks niet.
- **Vereisten**: Claude Code, Python 3.11+, `git`, `jq`, `gh` en de Superpowers-plugin; `codex`
  optioneel. Linux getest; macOS: de CLI draait, de hooks zijn ongetest (#90); Windows: WSL. Met
  een link naar de volledige tabel in de startgids.

### 7. Links

In de voet, als absolute URL's:

| Link | Doel |
|---|---|
| Getting started | `https://github.com/CorneeldH/lighthouse/blob/main/docs/getting-started.md` |
| Agent guide | `https://github.com/CorneeldH/lighthouse/issues/88`, met "(coming)"; #88 zet de link om naar het document |
| License | `https://github.com/CorneeldH/lighthouse/blob/main/LICENSE` |
| Changelog | `https://github.com/CorneeldH/lighthouse/blob/main/CHANGELOG.md` |

### 8. Tests

Naast de bestaande tests:

- **Assetnamen**: elke releaselink op de site (HTML en JS) noemt een asset die
  `.github/workflows/build.yml` bouwt, en elk gebouwd platform komt op de site voor. Een
  hernoemde asset in de build breekt dan de test, niet de download.
- **Checksum vóór het `PATH`**: elk installatieblok bevat een checksumcontrole vóór de stap die
  de binary verplaatst, en nergens staat `| sh` of `| bash`.
- **Links**: `index.html` bevat de vier links uit besluit 7 en `id="download"`.
- **Platformkeuze**: de pure functie uit besluit 3 tegen een tabel van UA-strings, één rij per
  regel uit de tabel. Een pytest start `node` op de module. Zonder `node` faalt de test (geen
  stille skip). Hij draait in de volledige pytest-run (commit op `main`, CI); `lighthouse.toml`
  en `gate_python.py` veranderen niet. De bestaande sitetests worden op dezelfde manier
  gedraaid.

Verificatie: `uv run pytest -q tests/test_pages_workflow.py tests/test_site_pages.py
tests/test_site_chain.py tests/test_site_packs.py` plus de nieuwe tests; lokaal kijken met
`python3 -m http.server --directory docs/site`. Na de merge: de deploy draait, en het gekopieerde
commando installeert een werkende binary, op Linux en op de Mac van Corneel. Beide uitkomsten
horen bij het bewijs bij het sluiten.

### 9. De startgids volgt

`docs/getting-started.md`, sectie *Install*: het downloadblok krijgt dezelfde stappen als de
Linux-tab van de site, dus `~/.local/bin` en geketend met `&&`, en een link naar de site voor de
andere platforms. De route met `go install` blijft `~/go/bin`, want daar zet Go de binary.
`docs/install.md` (de serverhandleiding) houdt `~/go/bin`, omdat de systemd-unit
`%h/go/bin/lighthouse` start. Valt een bestaande test om door deze wijziging, dan past die mee
aan.

### 10. Overig

- Een fragment in `changelog.d/issue-89-website-download.md` met `type: internal`: de site
  verandert de binary niet (`docs/releasing.md`).
- `README.md` linkt de site al; daar hoeft niets te veranderen.
- Geen `pack/`-wijziging, dus geen `init --upgrade`.

## Aan de bouwer

- Of `download.js` vanuit `index.js` geïmporteerd wordt of een eigen `<script>` krijgt.
- De vorm van de tabs (knoppen met `aria-selected`, of `<details>`), mits toegankelijk en zonder
  JS bruikbaar.
- De precieze teksten, binnen besluit 6.
- De vorm van de kopieerknop, mits met `navigator.clipboard` en zonder HTML-sinks.
- Styling in `map.css` met de bestaande tokens, licht en donker.

## Keuzes van Claude (niet door Corneel besloten)

- `index.html` wordt de landingspagina; geen aparte pagina naast de kaartindex (besluit 1).
- macOS standaard arm64, Intel als link eronder (besluit 3).
- Windows krijgt bovenaan de Linux-tab voor WSL; de Windows-tab met de `.exe` staat bij de
  andere platforms (besluit 3).
- Stappen met `&&` geketend, de laatste met het volle pad, de `PATH`-regel onder Linux en macOS
  (besluit 5).
- De agent-gids linkt tot #88 klaar is naar het issue (besluit 7).
- De platformtest als pytest die `node` start, alleen in de volledige run en CI (besluit 8).
- `docs/install.md` houdt `~/go/bin` voor de systemd-unit (besluit 9).
- Changelogfragment `type: internal` (besluit 10).
