# Website met een eenvoudige download — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `docs/site/index.html` wordt de landingspagina van lighthouse: bovenaan het
installatiecommando voor het herkende platform met een checksumcontrole, daaronder wat lighthouse
doet, voor wie, de vereisten, "Why lighthouse?" en de kaarten.

**Architectuur:** Statische HTML in `docs/site/index.html` met vijf platformpanelen die zonder JS
alle vijf zichtbaar zijn. `docs/site/platform.js` bevat twee pure functies zonder DOM
(platformkeuze, versielabel), die een pytest via `node` test. `docs/site/download.js` koppelt ze
aan de pagina: tabs, toelichting, versielabel uit de API, kopieerknop. `pages.yml` verandert niet.

**Tech stack:** HTML, CSS, vanilla JS (ES-modules); pytest (`html.parser`, `re`, `subprocess`
naar `node`).

**Spec:** [2026-10-31-website-download-design.md](../specs/2026-10-31-website-download-design.md).
Issue #89.

## Globale randvoorwaarden

- Alle tekst op de site is Engels, elke pagina `<html lang="en">`. Plan en commits zijn
  Nederlands. Code, namen en commentaar zijn Engels, ook in `docs/site/`.
- Geen framework, geen buildstap, geen externe bibliotheek. Lokaal kijken:
  `python3 -m http.server --directory docs/site`.
- Site-JS schrijft nooit HTML: geen `innerHTML`, `outerHTML`, `insertAdjacentHTML`,
  `document.write` (`tests/test_site_pages.py`). Alleen `textContent`, attributen en
  `createElement`.
- `index.html` houdt `id="maps"`, de link naar `chain.html`, de nummers #100–#104 en
  `<details id="why">` ongewijzigd.
- Releaselinks altijd `https://github.com/CorneeldH/lighthouse/releases/latest/download/<asset>`.
  Assets: `lighthouse_linux_amd64`, `lighthouse_linux_arm64`, `lighthouse_darwin_arm64`,
  `lighthouse_darwin_amd64`, `lighthouse_windows_amd64.exe`, `SHA256SUMS`.
- API voor het label: `https://api.github.com/repos/CorneeldH/lighthouse/releases/latest`.
  Terugvaltekst letterlijk `latest release`.
- Installatiemap `~/.local/bin`, zonder `sudo`. Nergens `| sh` of `| bash`.
- Links buiten `docs/site/` zijn absolute GitHub-URL's (spec besluit 7).
- `pages.yml`, `lighthouse.toml`, `pack/` en `docs/install.md` veranderen niet.
- Hergebruik de tokens van `map.css` (`--bg`, `--fg`, `--muted`, `--panel`, `--border`, `--link`,
  `--focus`, `--accent`); licht en donker werken via de bestaande `data-theme`.

## Review Focus

- **Geen JS of een oude browser**: alle vijf de panelen staan zichtbaar met hun commando en hun
  link; niets heeft `hidden` in de HTML. Test in taak 2.
- **API faalt, time-out of rate limit**: het label blijft `latest release` en de commando's werken.
  Pure functie getest in taak 1; `download.js` vangt elke fout (taak 3).
- **Checksumfout tijdens het plakken**: geen stap na de controle draait. Elke bash-regel behalve de
  laatste eindigt op `&&`, de controle staat vóór `mv`. Test in taak 2.
- **`~/.local/bin` staat nog niet op het `PATH`**: de laatste regel is
  `~/.local/bin/lighthouse version`, niet een kale `lighthouse version`. Test in taak 2.
- **Geen `navigator.clipboard`** (http buiten localhost, oude browser): geen kopieerknop, het
  commando blijft selecteerbaar. Handmatige check in taak 3.

---

### Taak 1: `platform.js` — platformkeuze en versielabel

**Files:**
- Create: `docs/site/platform.js`
- Create: `tests/test_site_download.py`

**Interfaces:**
- Produces:
  - `export function choosePlatform(info)`; `info` is
    `{ platform: string, userAgent: string, architecture: string }` (elk mag `""` zijn).
    Geeft `{ tab: string | null, note: "intel" | "wsl" | null }`. `tab` is één van
    `"linux-amd64"`, `"linux-arm64"`, `"darwin-arm64"`, `"darwin-amd64"`, `"windows-amd64"`.
  - `export function releaseLabel(data)`: geeft `data.tag_name` als dat een string is die
    begint met `v` en een cijfer, anders `"latest release"`. `data` mag `null`/`undefined` zijn.
  - In `tests/test_site_download.py`: `run_node(expression: str)`, die `platform.js` importeert,
    `JSON.stringify(<expression>)` print en het geparste resultaat teruggeeft.

Regels voor `choosePlatform`, in deze volgorde (zoeken hoofdletterongevoelig in de samenvoeging
van `platform` en `userAgent`):

| Signaal | Resultaat |
|---|---|
| bevat `android`, `iphone` of `ipad` | `{tab: null, note: null}` |
| bevat `windows`, `win32` of `win64` | `{tab: "linux-amd64", note: "wsl"}` |
| bevat `mac` en `architecture` is `x86` | `{tab: "darwin-amd64", note: null}` |
| bevat `mac` | `{tab: "darwin-arm64", note: "intel"}` |
| bevat `linux` en (`architecture` is `arm` of UA bevat `aarch64`/`arm64`) | `{tab: "linux-arm64", note: null}` |
| bevat `linux` | `{tab: "linux-amd64", note: null}` |
| anders | `{tab: null, note: null}` |

`android` vóór `linux`, want Android-UA's bevatten `Linux`. Geen kale `win`: dat zit ook in
`Darwin`.

- [ ] **Stap 1: Schrijf de falende tests**

```python
"""docs/site/platform.js picks the install tab the visitor sees first, and the release label
falls back to text when the API gives nothing usable."""

import json
import shutil
import subprocess

import pytest

from tests.test_site_chain import SITE

PLATFORM_JS = SITE / "platform.js"


def run_node(expression: str):
    node = shutil.which("node")
    assert node, "node is needed for the platform tests (no silent skip)"
    script = (
        f"import * as p from {json.dumps(PLATFORM_JS.as_uri())};\n"
        f"const {{ choosePlatform, releaseLabel }} = p;\n"
        f"console.log(JSON.stringify({expression}));"
    )
    out = subprocess.run(
        [node, "--input-type=module", "-e", script],
        capture_output=True, text=True, check=True, timeout=30,
    )
    return json.loads(out.stdout)


UA = {
    "linux": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/130 Safari/537.36",
    "linux_arm": "Mozilla/5.0 (X11; Linux aarch64; rv:131.0) Gecko/20100101 Firefox/131.0",
    "mac": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15",
    "windows": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130",
    "android": "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/130 Mobile",
    "iphone": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15",
    "other": "Mozilla/5.0 (X11; FreeBSD amd64; rv:131.0) Gecko/20100101 Firefox/131.0",
}

CASES = [
    ({"platform": "Linux x86_64", "userAgent": UA["linux"], "architecture": ""}, "linux-amd64", None),
    ({"platform": "Linux aarch64", "userAgent": UA["linux_arm"], "architecture": ""}, "linux-arm64", None),
    ({"platform": "Linux", "userAgent": UA["linux"], "architecture": "arm"}, "linux-arm64", None),
    ({"platform": "MacIntel", "userAgent": UA["mac"], "architecture": ""}, "darwin-arm64", "intel"),
    ({"platform": "macOS", "userAgent": UA["mac"], "architecture": "arm"}, "darwin-arm64", "intel"),
    ({"platform": "macOS", "userAgent": UA["mac"], "architecture": "x86"}, "darwin-amd64", None),
    ({"platform": "Win32", "userAgent": UA["windows"], "architecture": ""}, "linux-amd64", "wsl"),
    ({"platform": "Linux armv8l", "userAgent": UA["android"], "architecture": ""}, None, None),
    ({"platform": "iPhone", "userAgent": UA["iphone"], "architecture": ""}, None, None),
    ({"platform": "", "userAgent": UA["other"], "architecture": ""}, None, None),
    ({"platform": "", "userAgent": "curl/8.9 (Darwin 24.0)", "architecture": ""}, None, None),
    ({"platform": "", "userAgent": "", "architecture": ""}, None, None),
]


@pytest.mark.parametrize(("info", "tab", "note"), CASES)
def test_choose_platform(info, tab, note):
    assert run_node(f"choosePlatform({json.dumps(info)})") == {"tab": tab, "note": note}


@pytest.mark.parametrize(
    ("data", "label"),
    [
        ({"tag_name": "v0.2.0"}, "v0.2.0"),
        ({"tag_name": "v1.10.3-rc.1"}, "v1.10.3-rc.1"),
        ({"message": "API rate limit exceeded"}, "latest release"),
        ({"tag_name": "<b>v1</b>"}, "latest release"),
        ({"tag_name": 2}, "latest release"),
        (None, "latest release"),
    ],
)
def test_release_label(data, label):
    assert run_node(f"releaseLabel({json.dumps(data)})") == label
```

- [ ] **Stap 2: Draai, verwacht rood**

Run: `uv run pytest -q tests/test_site_download.py`
Expected: FAIL, `node` meldt `Cannot find module …/platform.js` (`CalledProcessError`).

- [ ] **Stap 3: Schrijf `docs/site/platform.js`**

Bovenaan één regel commentaar in de stijl van `index.js`. Geen DOM, geen top-level side
effects: de module moet in `node` te importeren zijn. `releaseLabel` toetst met
`/^v\d[\w.-]*$/`. Er is geen `package.json` boven `docs/site/`; Node 22 herkent de ES-module
zelf (lokaal nagegaan met v22.22). Faalt de test in CI met een `SyntaxError` op `export`, dan is
de `node` van de runner te oud: zet de Python-job dan op `actions/setup-node` met
`web/.nvmrc`, zoals de web-job.

- [ ] **Stap 4: Draai, verwacht groen**

Run: `uv run pytest -q tests/test_site_download.py tests/test_site_pages.py`
Expected: alles PASS (`test_component_never_writes_html_from_data` ziet het nieuwe bestand ook).

- [ ] **Stap 5: Commit**

```bash
git add docs/site/platform.js tests/test_site_download.py
git commit -m "feat(site): platformkeuze en versielabel voor de download (#89)"
```

### Taak 2: de landingspagina in `index.html`

**Files:**
- Modify: `docs/site/index.html` (kop l.6–17; nieuw blok vóór `<details id="why">`; secties na
  `#why`, vóór `#maps`; voet na `#maps`; de comment op l.49 verdwijnt)
- Modify: `docs/site/map.css` (nieuwe regels onderaan, na `.tile`)
- Test: `tests/test_site_download.py`

**Interfaces:**
- Produces (voor taak 3): in `index.html`
  - `<section id="download">` met daarin `<p id="release-version">latest release</p>`,
    `<p id="platform-note" hidden></p>` en één `<div class="platform" data-platform="<tab>">`
    per tab, in de volgorde `linux-amd64`, `linux-arm64`, `darwin-arm64`, `darwin-amd64`,
    `windows-amd64`. Elk paneel heeft een `<h3>` (`Linux x86-64`, `Linux ARM`,
    `macOS Apple silicon`, `macOS Intel`, `Windows`), één `<pre><code>` met het commando, en een
    `<a class="file-link">` met de tekst `or download the file`.
  - `id="platform-note"` is de enige `hidden` in de pagina.

Volgorde van de pagina, zoals spec besluit 1: kop (`<title>Lighthouse</title>`,
`<h1>Lighthouse</h1>`, één zin als `.subtitle`, themaknop), `#download`,
`<section id="what">`, `<section id="who">`, `<details id="why">`, `#maps`,
`<footer class="links">`.

De commando's zijn vaste tekst (spec besluit 5). In de HTML wordt `&&` geschreven als
`&amp;&amp;`. Linux x86-64:

```bash
cd "$(mktemp -d)" &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/lighthouse_linux_amd64 &&
curl -fsSLO https://github.com/CorneeldH/lighthouse/releases/latest/download/SHA256SUMS &&
sha256sum --check --ignore-missing SHA256SUMS &&
chmod +x lighthouse_linux_amd64 &&
mkdir -p ~/.local/bin && mv lighthouse_linux_amd64 ~/.local/bin/lighthouse &&
~/.local/bin/lighthouse version
```

| Paneel | Asset | Controle |
|---|---|---|
| `linux-arm64` | `lighthouse_linux_arm64` | `sha256sum --check --ignore-missing SHA256SUMS` |
| `darwin-arm64` | `lighthouse_darwin_arm64` | `shasum -a 256 -c --ignore-missing SHA256SUMS` |
| `darwin-amd64` | `lighthouse_darwin_amd64` | `shasum -a 256 -c --ignore-missing SHA256SUMS` |

Onder de Linux-panelen, buiten `<code>`: "Not on your `PATH` yet? Add
`export PATH="$HOME/.local/bin:$PATH"` to `~/.bashrc`." Onder de macOS-panelen hetzelfde met
`~/.zshrc`.

Windows (PowerShell), met erboven "The hooks need WSL: inside WSL, use the Linux tab. The
Windows binary runs the CLI only.":

```powershell
$base = "https://github.com/CorneeldH/lighthouse/releases/latest/download"
Invoke-WebRequest "$base/lighthouse_windows_amd64.exe" -OutFile lighthouse.exe
Invoke-WebRequest "$base/SHA256SUMS" -OutFile SHA256SUMS
$want = ((Select-String -Path SHA256SUMS -Pattern "lighthouse_windows_amd64.exe").Line -split "\s+")[0]
$have = (Get-FileHash -Algorithm SHA256 lighthouse.exe).Hash.ToLower()
if ($have -ne $want) { throw "Checksum mismatch: do not run lighthouse.exe" }
.\lighthouse.exe version
```

De `file-link` van elk paneel wijst naar de asset van dat paneel, met erachter "Check it against
`SHA256SUMS` and `chmod +x` it yourself." (Windows: zonder `chmod`). Na de panelen één zin:
"Next: run `lighthouse init` in your repository," met een link naar de startgids.

`#what`, `#who` en de vereisten: tekst binnen spec besluit 6. De voet: de vier links uit spec
besluit 7, met als teksten `Getting started`, `Agent guide (coming, #88)`, `License`,
`Changelog`.

- [ ] **Stap 1: Schrijf de falende tests** (toevoegen aan `tests/test_site_download.py`)

```python
import re
from html.parser import HTMLParser

from tests.test_site_chain import ROOT

INDEX = SITE / "index.html"
BUILD = ROOT / ".github" / "workflows" / "build.yml"
DOWNLOAD = "https://github.com/CorneeldH/lighthouse/releases/latest/download/"
TABS = ["linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64", "windows-amd64"]
CHECKS = ["sha256sum --check", "shasum -a 256 -c", "Get-FileHash"]


class InstallBlocks(HTMLParser):
    """The command text of the <pre><code> in each element with data-platform."""
    # Implementer: track the current data-platform, collect handle_data inside <code>
    # (convert_charrefs=True unescapes &amp;). Result: self.blocks: dict[str, str].


def install_blocks() -> dict[str, str]: ...


def built_assets() -> set[str]:
    pairs = re.findall(r"goos:\s*(\w+)\s*\n\s*goarch:\s*(\w+)", BUILD.read_text(encoding="utf-8"))
    assert pairs, "no goos/goarch matrix in build.yml"
    return {f"lighthouse_{os}_{arch}" + (".exe" if os == "windows" else "") for os, arch in pairs}


def test_site_assets_are_the_built_assets():
    texts = [p.read_text(encoding="utf-8") for p in [INDEX, *SITE.glob("*.js")]]
    named = {a for t in texts for a in re.findall(re.escape(DOWNLOAD) + r"([\w.]+)", t)}
    assert named == built_assets() | {"SHA256SUMS"}


def test_every_platform_has_an_install_block():
    assert list(install_blocks()) == TABS


def test_checksum_comes_before_the_binary_is_used():
    for tab, block in install_blocks().items():
        lines = [l for l in block.strip().splitlines() if l.strip()]
        check = next(i for i, l in enumerate(lines) if any(c in l for c in CHECKS))
        use = next(i for i, l in enumerate(lines) if " mv " in f" {l}" or l.rstrip().endswith("version"))
        assert check < use, tab
        assert "| sh" not in block and "| bash" not in block, tab


def test_bash_blocks_stop_on_the_first_failure():
    for tab, block in install_blocks().items():
        if tab == "windows-amd64":
            assert "throw" in block
            continue
        lines = [l.rstrip() for l in block.strip().splitlines() if l.strip()]
        assert all(l.endswith("&&") for l in lines[:-1]), tab
        assert lines[-1] == "~/.local/bin/lighthouse version", tab


def test_page_works_without_js():
    text = INDEX.read_text(encoding="utf-8")
    assert re.findall(r"<[^>]*\bhidden\b[^>]*>", text) == ['<p id="platform-note" hidden>']


def test_footer_links():
    text = INDEX.read_text(encoding="utf-8")
    base = "https://github.com/CorneeldH/lighthouse/"
    for url in ["blob/main/docs/getting-started.md", "issues/88", "blob/main/LICENSE", "blob/main/CHANGELOG.md"]:
        assert f'href="{base}{url}"' in text, url
    assert 'id="download"' in text and "<title>Lighthouse</title>" in text
```

`InstallBlocks` en `install_blocks` vult de implementer in (gedrag staat in de docstring). De
imports gaan naar de kop van het bestand, bij die van taak 1.

- [ ] **Stap 2: Draai, verwacht rood**

Run: `uv run pytest -q tests/test_site_download.py`
Expected: de zes nieuwe tests FAIL (geen `data-platform`, geen links); taak 1 blijft PASS.

- [ ] **Stap 3: Herschrijf `index.html` en voeg de stijl toe aan `map.css`**

Panelen als `.platform`-blokken onder elkaar; `pre` met `overflow-x: auto` en de `--panel`-
achtergrond; `#what` als raster van vier blokken in de stijl van `.tile`. Max. breedte zoals
`.why` (900px).

- [ ] **Stap 4: Draai, verwacht groen**

Run: `uv run pytest -q tests/test_site_download.py tests/test_site_pages.py tests/test_site_chain.py tests/test_site_packs.py tests/test_pages_workflow.py`
Expected: alles PASS.

- [ ] **Stap 5: Draai het Linux-commando echt**

Run: `HOME=$(mktemp -d) bash -c '<het Linux x86-64-blok uit de HTML>'`
Expected: `lighthouse_linux_amd64: OK` en daarna de versie van de laatste release (`v0.2.0` of
nieuwer). Noteer de uitvoer voor het PR-bewijs.

- [ ] **Stap 6: Commit**

```bash
git add docs/site/index.html docs/site/map.css tests/test_site_download.py
git commit -m "feat(site): landingspagina met installatiecommando per platform (#89)"
```

### Taak 3: `download.js` — tabs, toelichting, versielabel, kopieerknop

**Files:**
- Create: `docs/site/download.js`
- Modify: `docs/site/index.js` (één regel: `import "./download.js";` of een aanroep van de
  export hieronder)
- Modify: `docs/site/map.css` (stijl voor de tabs en de kopieerknop)

**Interfaces:**
- Consumes: `choosePlatform`, `releaseLabel` uit `./platform.js` (taak 1); de ids en
  `data-platform`-panelen uit taak 2.
- Produces: `export function initDownload(root)`, met `root` = `document.getElementById("download")`.
  Doet niets als `root` `null` is.

Gedrag:
1. Bouwt met `createElement` een `role="tablist"` met één `<button role="tab">` per paneel
   (tekst = de `<h3>` van het paneel), vóór het eerste paneel. Panelen krijgen `role="tabpanel"`;
   alleen het gekozen paneel blijft zichtbaar (`hidden` op de rest). Pijltjestoetsen links/rechts
   wisselen van tab.
2. Kiest de tab met `choosePlatform`. Bij `tab: null` is `linux-amd64` open en is er geen
   toelichting. `info` komt uit `navigator.userAgentData?.platform ?? navigator.platform`,
   `navigator.userAgent`, en `architecture` uit
   `navigator.userAgentData?.getHighEntropyValues(["architecture"])` (fout of ontbrekend → `""`).
3. `note: "intel"` → `#platform-note` toont "Intel Mac? Take the macOS Intel tab." met een knop die
   die tab opent; `note: "wsl"` → "The hooks need WSL: run this inside WSL." `hidden` gaat eraf.
4. `fetch` naar de API met `AbortSignal.timeout(5000)`; `#release-version` krijgt
   `releaseLabel(json)`. Elke fout laat `latest release` staan.
5. Alleen als `navigator.clipboard?.writeText` bestaat: een knop `Copy` per `<pre>`, die de
   `textContent` van de `<code>` kopieert en twee seconden `Copied` toont.

- [ ] **Stap 1: Schrijf `download.js` en koppel het in `index.js`**

- [ ] **Stap 2: Draai de sitetests**

Run: `uv run pytest -q tests/test_site_download.py tests/test_site_pages.py`
Expected: alles PASS (geen HTML-sinks, `./platform.js` en `./download.js` resolven).

- [ ] **Stap 3: Handmatige browsercheck**

Run: `python3 -m http.server --directory docs/site 8089`, open `http://localhost:8089/`.
Expected, in een Chromium-browser:
- de tab van je eigen platform staat open, het label toont `v0.2.0` (of nieuwer);
- met DevTools → *Network conditions* → een user agent met `Macintosh` toont de pagina na
  herladen een macOS-tab (op een x86-machine macOS Intel, want Chromium meldt `architecture`
  `x86`); met een user agent met `Windows NT` de Linux-tab en de WSL-toelichting;
- *Copy* zet het commando op het klembord;
- met JS uit (DevTools → *Disable JavaScript*) staan alle vijf de panelen onder elkaar, zonder
  tabs en zonder kopieerknop;
- met *Offline* blijft het label `latest release`;
- licht en donker thema zijn beide leesbaar.

- [ ] **Stap 4: Commit**

```bash
git add docs/site/download.js docs/site/index.js docs/site/map.css
git commit -m "feat(site): tabs, platformherkenning en kopieerknop op de downloadpagina (#89)"
```

### Taak 4: de startgids volgt de site

**Files:**
- Modify: `docs/getting-started.md` (sectie *Install*, l.36–58)
- Modify: `tests/test_getting_started.py::test_install_offers_binary_then_go` (l.66–72)

**Interfaces:**
- Consumes: het Linux x86-64-blok uit taak 2, letterlijk.

- [ ] **Stap 1: Breid de test uit**

Voeg toe aan `test_install_offers_binary_then_go`:

```python
    assert "mv lighthouse_linux_amd64 ~/.local/bin/lighthouse" in text
    assert "~/go/bin/lighthouse" not in text
    assert "https://corneeldh.github.io/lighthouse/" in text
```

- [ ] **Stap 2: Draai, verwacht rood**

Run: `uv run pytest -q tests/test_getting_started.py::test_install_offers_binary_then_go`
Expected: FAIL op de `~/.local/bin`-regel.

- [ ] **Stap 3: Pas *Install* aan**

Het bash-blok wordt het Linux x86-64-blok uit taak 2. De zin erna noemt de site
(`https://corneeldh.github.io/lighthouse/#download`) voor de andere platforms en houdt de
bestaande link naar `install.md#download-a-release-binary` voor de details. De Go-route en de
zin over `~/go/bin` blijven: daar zet Go de binary.

- [ ] **Stap 4: Draai, verwacht groen**

Run: `uv run pytest -q tests/test_getting_started.py tests/test_site_pages.py`
Expected: alles PASS (`test_site_links_resolve` leest ook de startgids).

- [ ] **Stap 5: Commit**

```bash
git add docs/getting-started.md tests/test_getting_started.py
git commit -m "docs(getting-started): installatie naar ~/.local/bin, zoals de site (#89)"
```

### Taak 5: changelogfragment en de volledige run

**Files:**
- Create: `changelog.d/issue-89-website-download.md`

- [ ] **Stap 1: Schrijf het fragment**

```markdown
---
type: internal
---
The site on GitHub Pages opens with an install command per platform that checks `SHA256SUMS` before the binary lands in `~/.local/bin`, and keeps the maps below it. The getting-started guide installs to the same place.
```

- [ ] **Stap 2: Draai de volledige Python-suite**

Run: `uv run pytest -q`
Expected: alles PASS, waaronder de fragmentcheck van `tests/test_changelog.py`.

- [ ] **Stap 3: Commit**

```bash
git add changelog.d/issue-89-website-download.md
git commit -m "docs(changelog): fragment voor de downloadpagina (#89)"
```

## Na de merge (bewijs voor het sluiten, niet in deze branch)

- `pages.yml` draait groen op `main`; `https://corneeldh.github.io/lighthouse/` toont de download.
- Op de Mac van Corneel: het macOS-commando van de site geeft `OK` van `shasum` en de versie.
- Op Linux hetzelfde (uitvoer van taak 2 stap 5, nu vanaf de live site).
