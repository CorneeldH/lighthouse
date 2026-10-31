# Landingspagina en een eigen installatiepagina — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `docs/site/index.html` wordt een landingspagina met het waarom bovenaan. De installatie
krijgt een eigen pagina, `docs/site/install.html`, met het blok van je eigen platform open en de
andere platforms als ingeklapte `<details>`. Elke pagina haalt scripts en stylesheet op met een
versie in de URL, zodat een oude gecachete `index.js` niet meer naast een nieuwe `index.html`
draait.

**Architectuur:** De vijf installblokken verhuizen ongewijzigd van `index.html` naar
`install.html`, nu als `<details class="platform" data-platform="…">`. `download.js` verliest de
tabs: het opent het gekozen `<details>` en zet het bovenaan. `platform.js` blijft gelijk. Een
nieuw `install.js` is het startpunt van `install.html`. Cache-busting werkt zonder buildstap:
elke lokale `.js`/`.css`-verwijzing krijgt `?v=<8 hex>`, en een test eist dat die waarde gelijk is
aan de hash van alle site-assets. `tests/site_version.py` schrijft die waarde.

**Tech stack:** HTML, CSS, vanilla JS (ES-modules); pytest (`html.parser`, `re`, `hashlib`).

**Spec:** issue #156 (acceptatiecriteria) en de keuze van de gebruiker in de sessie: "Hero + why,
install apart". Er is geen apart specbestand. De vorige versie staat in
[2026-10-31-issue-89-website-download.md](2026-10-31-issue-89-website-download.md).

## Globale randvoorwaarden

- Alle tekst op de site is Engels, elke pagina `<html lang="en">`. Plan en commits zijn
  Nederlands. Code, namen en commentaar zijn Engels.
- Geen framework, geen buildstap, geen externe bibliotheek. `pages.yml` verandert niet. Lokaal
  kijken: `python3 -m http.server --directory docs/site`.
- Site-JS schrijft nooit HTML: geen `innerHTML`, `outerHTML`, `insertAdjacentHTML` of
  `document.write`. Alleen `textContent`, attributen, `createElement` en DOM-verplaatsingen.
- De tekst van de vijf installblokken (`<pre><code>`, path-hint, file-line, Windows-intro)
  verhuist letterlijk. De bestaande tests op checksum, `&&` en PowerShell blijven ongewijzigd
  groen.
- De volgorde van de platformen in de HTML blijft `linux-amd64`, `linux-arm64`, `darwin-arm64`,
  `darwin-amd64`, `windows-amd64` (alfabetisch op OS). Zonder JS zijn ze alle vijf ingeklapt, dus
  er is geen voorkeur.
- `index.html` houdt `id="maps"`, de links naar `chain.html` en `guardrails.html`, de nummers
  #101–#104, `id="why"` en de vier footerlinks (`tests/test_site_pages.py`,
  `tests/test_site_download.py::test_footer_links`).
- Hergebruik de tokens van `map.css` (`--bg`, `--fg`, `--muted`, `--panel`, `--border`, `--link`,
  `--focus`, `--accent`); licht en donker werken via de bestaande `data-theme`.
- Versietoken: letterlijk `?v=` plus 8 kleine hex-tekens, op elke lokale `.js`- en
  `.css`-verwijzing in `docs/site/*.html` en elke relatieve import in `docs/site/*.js`.
- Elke commit draait de test-gate; volledige run: `uv run pytest -q -n auto`.

## Review focus

1. **Een Mac-bezoeker met een oude cache.** De nieuwe HTML vraagt `index.js?v=<nieuw>` en krijgt
   dus nooit de oude module. Gedekt in taak 4
   (`test_every_local_asset_carries_the_site_version`).
2. **Een platform dat `choosePlatform` niet kent** (Android, iPhone, FreeBSD): geen enkel blok
   gaat open en er wordt niets verplaatst. De oude terugval op `linux-amd64` vervalt. Zonder
   DOM-test: bewezen met de screenshot met iPhone-UA in taak 5.
3. **Een klik vóór `getHighEntropyValues`.** Wie al een `<summary>` aanklikte, ziet de keuze niet
   meer verspringen als de architectuur binnenkomt (`touched`). Wordt bekeken in de review van
   taak 1.
4. **Een oude link naar `…/lighthouse/#download`** (de startgids, bladwijzers): met JS komt die
   op `install.html` uit, zonder JS op de Install-knop van de landingspagina. Gedekt in taak 2
   (`id="download"` op de knop) en taak 5 (screenshot na het laden van `index.html#download`).
5. **Donker thema op de nieuwe onderdelen** (hero-knop, `<summary>`, Copy-knop in een
   `<details>`): alleen tokens, geen vaste kleuren. Gedekt door de screenshots in taak 5,
   licht en donker.

---

### Taak 1: `install.html` met uitklapbare platformen

**Files:**
- Create: `docs/site/install.html`, `docs/site/install.js`
- Modify: `docs/site/download.js` (tabs eruit, `<details>`-model erin), `docs/site/map.css`
  (`.platform-tabs` eruit; `details.platform`, `summary` en `.others-title` erin),
  `docs/site/index.html` (de `#download`-sectie eruit; taak 2 bouwt de nieuwe landingspagina)
- Test: `tests/test_site_download.py`

**Interfaces:**
- Produces: `initDownload(root: Element): void` in `download.js`, dezelfde naam als nu. `root`
  is `<main id="install">` op `install.html`. `install.html` heeft `<p id="release-version">`
  en `<p id="platform-note" hidden>` binnen `root`.
- Produces: `install.html` met `<title>Install lighthouse</title>`, een back-link
  `<a class="back-link" href="index.html">Lighthouse</a>` (patroon van `chain.html:16`) en
  `<button id="theme-toggle">`.

- [ ] **Stap 1: Pas de tests aan op `install.html`**

In `tests/test_site_download.py`:
- `INSTALL = SITE / "install.html"`; `install_blocks()` leest `INSTALL`.
- `test_site_assets_are_the_built_assets` leest `[INDEX, INSTALL, *SITE.glob("*.js")]`.
- Vervang `test_page_works_without_js` door `test_install_page_works_without_js`:

```python
def test_install_page_works_without_js():
    text = INSTALL.read_text(encoding="utf-8")
    assert re.findall(r"<[^>]*\bhidden\b[^>]*>", text) == ['<p id="platform-note" hidden>']
    panels = re.findall(r"<(\w+)([^>]*\bdata-platform=[^>]*)>", text)
    assert [tag for tag, _ in panels] == ["details"] * len(TABS)
    assert not [attrs for _, attrs in panels if re.search(r"\bopen\b", attrs)]
    assert text.count("<summary>") == len(TABS)
```

- `test_footer_links` blijft op `INDEX`, maar de laatste regel wordt
  `assert "<title>Lighthouse</title>" in text` (taak 2 test `id="download"`).
- Nieuw: `test_install_page_title`: `"<title>Install lighthouse</title>" in INSTALL.read_text(...)`.

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_site_download.py`
Verwacht: FAIL, `FileNotFoundError` op `install.html`.

- [ ] **Stap 3: Bouw `install.html`, `install.js` en het nieuwe `download.js`**

- `install.html`: kop zoals `chain.html` (h1 `Install lighthouse`, back-link, theme-knop), dan
  `<main id="install">` met `#release-version`, de lead-alinea uit de huidige `index.html`,
  `#platform-note`, de vijf blokken als `<details class="platform" data-platform="…">` met
  `<summary>` = de huidige `<h3>`-tekst, en de `next-step`-alinea. Onderaan dezelfde footer als
  `index.html`. Ook een link naar de vereisten:
  `<a href="index.html#who">requirements</a>`.
- `install.js`: `initTheme(document.getElementById("theme-toggle"))` en
  `initDownload(document.getElementById("install"))`.
- `download.js`:
  - `buildTabs`, de toetsenbordnavigatie en `FALLBACK_TAB` vervallen.
  - Een interne `select(name: string | null)`: sluit alle panelen, opent `name`, zet dat paneel
    vóór het eerste paneel, en zet direct erna één `<h2 class="others-title">Other platforms</h2>`
    (eenmalig aangemaakt, met `textContent`). Bij `null` gebeurt niets (review focus 2).
  - `touched` wordt `true` bij een klik op een `summary` of op `.platform-note-link`.
  - `showNote`: de Intel-knop heet `Open the macOS Intel build.` en roept
    `select("darwin-amd64")` aan. De WSL-tekst blijft.
  - `showRelease` en `addCopyButtons` blijven; de Copy-knop staat in het `<details>`, vóór de
    `<pre>`.
- `map.css`: de regels van `.platform-tabs` en `[role=tab]` vervallen. Nieuw: `details.platform`
  als kaart (rand, `--panel`), `summary` met cursor, vette naam en een eigen pijltje,
  `.others-title` in `--muted`, kleiner dan h2.
- `index.html`: haal `<section id="download">` weg, en uit `index.js` `initDownload`.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

Run: `uv run pytest -q tests/test_site_download.py tests/test_site_pages.py`
Verwacht: PASS. `test_site_pages` controleert ook dat links en imports van `install.html`
bestaan en dat de pagina `lang="en"` heeft.

- [ ] **Stap 5: Commit**

```bash
git add docs/site/install.html docs/site/install.js docs/site/download.js docs/site/map.css docs/site/index.html docs/site/index.js tests/test_site_download.py
git commit -m "feat(site): installatie op een eigen pagina, platformen uitklapbaar (#156)"
```

### Taak 2: de landingspagina

**Files:**
- Modify: `docs/site/index.html`, `docs/site/index.js`, `docs/site/map.css`
- Test: `tests/test_site_download.py`

**Interfaces:**
- Consumes: `install.html` uit taak 1.
- Produces: `index.html` met `<a id="download" class="hero-button" href="install.html">` in de
  hero, `<section id="why">` (geen `<details>`), `<section id="what">`, `<details id="who">`,
  `<section id="maps">`, in die volgorde.

- [ ] **Stap 1: Schrijf de falende test**

```python
def test_landing_page_puts_the_why_first():
    text = INDEX.read_text(encoding="utf-8")
    assert "<pre" not in text
    assert re.search(r'<a\b[^>]*\bid="download"[^>]*\bhref="install.html"', text)
    assert re.search(r'<section\b[^>]*\bid="why"', text)
    assert re.search(r'<details\b[^>]*\bid="who"', text)
    order = [text.index(f'id="{i}"') for i in ["download", "why", "what", "who", "maps"]]
    assert order == sorted(order)
```

Een `id="download"` met `href` in een andere volgorde dan `id` faalt in deze regex. Schrijf de
knop dus met `id` vóór `href`.

- [ ] **Stap 2: Draai de test en zie hem falen**

Run: `uv run pytest -q tests/test_site_download.py::test_landing_page_puts_the_why_first`
Verwacht: FAIL op de `<a … id="download"`-regex.

- [ ] **Stap 3: Bouw de landingspagina**

- Hero (`<header class="hero">`): h1 `Lighthouse`, de huidige subtitel, de knop
  `Install lighthouse`, en een tweede link `Why lighthouse?` naar `#why`. De theme-knop blijft
  rechtsboven.
- `<section id="why">`: h2 `Why lighthouse?`, de zes alinea's en de "Further reading"-regel uit
  het huidige `<details id="why">`, letterlijk.
- `<section id="what">` ongewijzigd.
- `<details id="who">` met `<summary>Who it is for, and what it needs</summary>` en de huidige
  inhoud van `#who`.
- Kaarten en footer ongewijzigd; de footer krijgt vooraan `<a href="install.html">Install</a>`.
- `index.js`: `initTheme`, en `if (location.hash === "#download") location.replace("install.html")`.
  De code die `#why` openklapt vervalt, want `#why` is geen `<details>` meer.
- `map.css`: `.hero` (ruimte boven en onder, h1 groter dan nu), `.hero-button` (gevuld in
  `--accent`, tekst leesbaar in beide thema's, `:focus-visible` met `--focus`). De `.why`-regels
  gaan van een `<details>` naar een sectie met dezelfde breedte als `.what`.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

Run: `uv run pytest -q tests/test_site_download.py tests/test_site_pages.py`
Verwacht: PASS, ook `test_index_*` in `test_site_pages.py` (`id="maps"`, #101–#104) en de
`index.html#why`-links in `chain.html` en `guardrails.html`.

- [ ] **Stap 5: Commit**

```bash
git add docs/site/index.html docs/site/index.js docs/site/map.css tests/test_site_download.py
git commit -m "feat(site): landingspagina met het waarom bovenaan (#156)"
```

### Taak 3: startgids en changelog

**Files:**
- Modify: `docs/getting-started.md:53`
- Create: `changelog.d/issue-156-website-landing.md`

- [ ] **Stap 1: Wijs de startgids naar de installatiepagina**

`<https://corneeldh.github.io/lighthouse/#download>` wordt
`<https://corneeldh.github.io/lighthouse/install.html>`.

- [ ] **Stap 2: Schrijf het fragment**

```markdown
---
type: changed
---
The site opens with what lighthouse is and why, and installs on a page of its own: your platform open, the others one click away.
```

`changed` is een geldig type (`docs/releasing.md:51`); het geeft een minor release op maandag.

- [ ] **Stap 3: Draai de tests**

Run: `uv run pytest -q tests/test_getting_started.py`
Verwacht: PASS. `:75` eist alleen `https://corneeldh.github.io/lighthouse/` in de tekst.

- [ ] **Stap 4: Commit**

```bash
git add docs/getting-started.md changelog.d/issue-156-website-landing.md
git commit -m "docs(getting-started): link naar de installatiepagina (#156)"
```

### Taak 4: versie in elke asset-URL

**Files:**
- Create: `tests/site_version.py`, `tests/test_site_assets.py`
- Modify: alle `docs/site/*.html` (`href="map.css"`, `src="<page>.js"`) en alle
  `docs/site/*.js` met een relatieve import

**Interfaces:**
- Produces, in `tests/site_version.py`:
  - `site_version(site: Path = SITE) -> str`: de eerste 8 hex-tekens van sha256 over de
    bestandsnaam en inhoud van `site/*.js` en `site/*.css` (bovenste niveau, gesorteerd op naam).
    Haal `?v=[0-9a-f]{8}` uit de inhoud vóór het hashen, zodat het schrijven van de versie de
    versie niet verandert.
  - `asset_refs(path: Path) -> list[str]`: elke lokale `.js`/`.css`-verwijzing zoals ze er
    staat, met query: `href`/`src` in HTML, `from "./…"` in JS.
  - `write(site: Path = SITE) -> list[Path]`: zet elke verwijzing op `<naam>?v=<site_version()>`
    en geeft de gewijzigde bestanden terug.
  - `python -m tests.site_version` roept `write()` aan en print de gewijzigde paden.

- [ ] **Stap 1: Schrijf de falende tests**

```python
def test_every_local_asset_carries_the_site_version():
    version = site_version()
    pages = sorted(SITE.glob("*.html")) + sorted(SITE.glob("*.js"))
    refs = [(p.name, r) for p in pages for r in asset_refs(p)]
    assert refs, "no asset references found"
    assert [(p, r) for p, r in refs if not r.endswith(f"?v={version}")] == []


def test_version_follows_the_assets_not_the_tokens(tmp_path):
    for p in [*SITE.glob("*.js"), *SITE.glob("*.css")]:
        (tmp_path / p.name).write_text(p.read_text(encoding="utf-8"), encoding="utf-8")
    before = site_version(tmp_path)
    css = tmp_path / "map.css"
    css.write_text(css.read_text(encoding="utf-8") + "\n/* x */\n", encoding="utf-8")
    changed = site_version(tmp_path)
    assert changed != before
    js = tmp_path / "chain.js"
    text = js.read_text(encoding="utf-8")
    js.write_text(re.sub(r"(\./map\.js)(\?v=[0-9a-f]{8})?", r"\1?v=00000000", text), encoding="utf-8")
    assert js.read_text(encoding="utf-8") != text
    assert site_version(tmp_path) == changed
```

Plus één test dat `asset_refs` op `chain.js` de import van `./map.js` vindt.

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_site_assets.py`
Verwacht: FAIL; `index.html` verwijst naar `index.js` zonder `?v=`.

- [ ] **Stap 3: Schrijf `tests/site_version.py` en zet de versies**

Run: `uv run python -m tests.site_version`
Verwacht: de paden van alle `docs/site/*.html` en van `chain.js`, `guardrails.js`,
`download.js`, `index.js` en `install.js`.

- [ ] **Stap 4: Draai de volledige suite**

Run: `uv run pytest -q -n auto`
Verwacht: PASS. Let op `tests/test_site_pages.py` (links met `?v=` lossen op, omdat de query
eraf gaat) en op elke test die een site-module via `node` importeert.

- [ ] **Stap 5: Commit**

```bash
git add tests/site_version.py tests/test_site_assets.py docs/site
git commit -m "feat(site): versie in elke asset-URL tegen een gemengde cache (#156)"
```

Elke latere wijziging aan een `.js` of `.css` in `docs/site/` vraagt opnieuw
`uv run python -m tests.site_version`; anders is `test_every_local_asset_carries_the_site_version`
rood. Zet die regel in de docstring van `tests/test_site_assets.py`.

### Taak 5: bewijs in de browser

Geen commit; het bewijs gaat in de PR (acceptatiecriterium 7).

- [ ] **Stap 1: Serveer de site**

Run: `python3 -m http.server 8765 --directory docs/site` (op de achtergrond)

- [ ] **Stap 2: Maak de screenshots met Playwright-Chromium**

Gebruik een script in de scratch-map, niet in de repo. Leg vast:
- `index.html` en `install.html` met Mac-UA
  (`Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15`) en
  met Linux-UA, in licht en donker (`colorScheme`);
- `install.html` met `javaScriptEnabled: false`;
- `install.html` met iPhone-UA (review focus 2: niets open);
- `index.html#download`, en daarna de `page.url()`: die moet op `install.html` eindigen.

Verwacht: met Mac-UA staat `macOS Apple silicon` bovenaan en open, met een Copy-knop en de
Intel-notitie. Zonder JS zijn alle vijf blokken dicht.

- [ ] **Stap 3: Voeg de screenshots en de URL-uitvoer toe aan de PR-body**

Via `/lighthouse-verify-pr`.
