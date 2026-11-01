# Plattegrond van je repo na `lighthouse init` (#101)

Datum: 2026-11-01. Issue: #101. Milestone: *Kaarten: lighthouse in beeld*.
Bouwt op [`2026-10-27-kaarten-design.md`](2026-10-27-kaarten-design.md) (§3 en K3) en
[`2026-10-31-systeemkaart-design.md`](2026-10-31-systeemkaart-design.md) (stap 2).

## 1. Doel en waarom

De systeemkaart (`docs/site/system.html`) heeft drie stappen: *Install once*,
*lighthouse init*, *Run the loop*. Stap 2 noemt nu zes vakken (Skills, Agents, Hooks, Rules,
Config, `.github/`) en linkt naar de referentiepagina en een stuk tekst eronder. Deze kaart is
de verdieping van stap 2 (Corneel): een plattegrond van de repo van de gebruiker na
`lighthouse init`. Elk gebied is een plek waar `init` schrijft.

Wie de kaart opent, wil weten wat lighthouse in zijn repo heeft gezet, en wat hij zelf mag
bewerken zonder dat `init --upgrade` het terugdraait. De kaart beantwoordt dat per plek: van
wie hij is, uit welk pakket hij komt, wie hem leest, en wat `--upgrade` ermee doet.

## 2. Doelen, niet-doelen, anti-doelen

**Doelen**

- Elke plek waar `init` schrijft staat als gebied op de kaart, ook de vier plekken die geen
  unit in het manifest zijn: `CLAUDE.md`, `lighthouse.toml`, `.lighthouse/manifest.toml` en
  `~/.config/lighthouse/host.toml`.
- Per gebied: eigenaar, pakket(ten), lezers, wat `--upgrade` doet.
- Een zone toont wat `--upgrade` raakt.
- Een Go-test faalt als `init` schrijft naar een plek die niet op de kaart staat, en als een
  gebied op de kaart staat waar `init` niet (meer) schrijft.

**Niet-doelen**

- Geen sleutels van `lighthouse.toml` of `host.toml`: die staan op de configuratiekaart
  (`config.html`, #104). Het gebied `lighthouse.toml` linkt daarheen.
- Geen lijst van alle skills, agents en hooks per gebied: die staat op de referentiepagina.
  Het paneel van een gebied linkt daarheen.
- Geen wijziging aan wat `init` doet. Deze kaart beschrijft; `internal/` en `pack/` blijven
  ongemoeid, dus de dogfood-regel (`init --upgrade`) geldt niet.
- Geen generator (no-go van de milestone): de kaartdata is handgeschreven; de test bewaakt hem.

**Anti-doelen**

- De kaart zegt "overschreven bij `--upgrade`" waar dat niet zo is. Heb je een deel van
  lighthouse lokaal gewijzigd of verwijderd (een bestand, een blok in `AGENTS.md`, een hook in
  `settings.json`), dan weigert `--upgrade` de hele run en schrijft niets, tot je `--force`
  geeft (`internal/initcmd/initcmd.go:175-187`, `internal/initcmd/workspace.go:51-95`,
  `internal/reconcile/reconcile.go:79-105`). Dat geldt alleen voor de units in het manifest
  (file, block, hook, permission). `lighthouse.toml` en `.lighthouse/manifest.toml` zijn geen
  unit: een verwijderde sleutel van lighthouse in `lighthouse.toml` komt zonder vraag terug
  (`projectconfig.AppendMissing`), en `manifest.toml` wordt elke run opnieuw geschreven. De
  kaart zegt de weigering daar dus niet.
- Het oogt als een bestandsboom uit een IDE in plaats van een plattegrond.
- Je herkent je eigen repo er niet in.

## 3. De schets

Corneel koos deze vorm in het gesprek van 2026-11-01. De formele goedkeuring (AC 1) is een
comment van Corneel in #101 op deze schets; pas daarna wordt gebouwd.

```
your repo/
┌───────────────────────────────────────────────────────────────────┐
│ ┌ AGENTS.md ──────────┐ ┌ CLAUDE.md ──┐ ┌ lighthouse.toml ───────┐ │
│ │ your text           │ │ @AGENTS.md  │ │ your values            │ │
│ │ ▓ block per pack ▓  │ │ once, only  │ │ ░ missing keys added ░ │ │
│ └─────────────────────┘ │ if neither  │ │   → config map         │ │
│                         │ exists      │ └────────────────────────┘ │
│                         └─────────────┘                            │
│ ┌ .claude/ ──────────────────────────────────────────────────────┐ │
│ │ ▓ hooks/lighthouse/ ▓   ▓ skills/lighthouse-*/ ▓               │ │
│ │ ▓ agents/lighthouse-*.md ▓                                     │ │
│ │ settings.json: ░ lighthouse hooks + permissions ░, rest yours  │ │
│ │ your own skills and agents stay                                │ │
│ └────────────────────────────────────────────────────────────────┘ │
│ ┌ .agents/skills/ ───────┐   ┌ .lighthouse/ ─────────────────────┐ │
│ │ ▓ lighthouse-*/ ▓      │   │ ▓ lib/ scripts/ headings.toml ▓   │ │
│ └────────────────────────┘   │ ▓ manifest.toml (every run) ▓     │ │
│ ┌ .github/ ──────────────────┐└───────────────────────────────────┘ │
│ │ ▓ pull_request_template ▓  │                                     │
│ │ ▓ workflows/lighthouse-* ▓ │    your code, your tests, …         │
│ └────────────────────────────┘                                     │
└───────────────────────────────────────────────────────────────────┘
  on your workstation: ~/.config/lighthouse/host.toml (Tailscale only)

  ▓ lighthouse's: --upgrade replaces it
  ░ shared: only lighthouse's block or keys
    blank: yours, init leaves it alone
  - - dashed line around ▓ and ░ = the zone --upgrade touches
  changed or deleted a file, block or hook of lighthouse's? --upgrade stops
  and writes nothing until you pass --force
```

*Na het gesprek aangepast:* de legenda zei eerst "a file you changed: only with --force". De
code weigert dan de hele run, ook voor blokken en hooks; de laatste regel van de legenda zegt
dat nu. Na de spec-review (r1-f2) noemt die regel "a file, block or hook" in plaats van
"something": voor de sleutels in `lighthouse.toml` en voor `manifest.toml` weigert `--upgrade`
niet *(eigen keuze na spec-review, r1-f2)*.

**Grondplan.** De kamers volgen de echte mappen van de repo: de root met drie bestanden,
`.claude/`, `.agents/`, `.lighthouse/`, `.github/`. Een kamer *your code, your tests, …* staat
erbij als lege ruimte van de gebruiker, zodat zichtbaar is dat lighthouse maar een deel van de
repo bezet. Die kamer is geen gebied (`init` schrijft er niet), maar licht wel op in de zone
*Yours* (keuze van Claude). `host.toml` staat buiten de muur, als klein vak *on your
workstation*.

**Drie niveaus** (kaarten-design §3):

1. **Het geheel.** De plattegrond over de volle breedte.
2. **Een zone.** Knoppen boven de kaart: *All*, *Lighthouse's*, *Shared*, *Yours*,
   *--upgrade touches*. De gekozen zone licht op, de rest dimt. Zelfde gedrag als de
   routeknoppen van `renderFlowMap` in `docs/site/map.js` (`aria-pressed`, klasse `dim`), op
   `loop.html` en `session.html`. `renderConfig` heeft geen knoppen en geen paneel.
3. **Eén gebied.** Klik een gebied: het paneel (`drawerFor`) toont in gewone taal wat het is,
   en daaronder *Owner*, *Comes from* (pakketten), *Read by* (lezers), *On --upgrade* en een
   link verder (referentiepagina of configuratiekaart).

**Kleur zegt van wie het is**, net als op de andere kaarten, maar hier met drie eigenaren:
lighthouse (vol), shared (gearceerd), yours (alleen rand). Geen nieuwe kleuren: de tokens van
`map.css` (keuze van Claude).

## 4. De gebieden

Bron: de layout in de doc-comment van `internal/packs/packs.go`, `IsInstallPath` in hetzelfde
bestand, en `buildPlan` in `internal/initcmd/initcmd.go`.

| Gebied | Eigenaar | Hoe `init` schrijft | Pakketten | `--upgrade` |
|---|---|---|---|---|
| `AGENTS.md` | shared | één managed block per pakket; rest van jou | core, go, python, shell | vervangt of verwijdert alleen de blokken |
| `CLAUDE.md` | yours | `@AGENTS.md`, alleen als `AGENTS.md` en `CLAUDE.md` allebei ontbreken | — (init zelf) | idem: alleen als beide ontbreken; een bestaand `CLAUDE.md` nooit |
| `lighthouse.toml` | shared | voegt ontbrekende tabellen en sleutels toe; verandert nooit een bestaande waarde | core, python (fragment) | voegt alleen ontbrekende sleutels toe; sleutels van een verwijderd pakket blijven staan |
| `.claude/hooks/lighthouse/` | lighthouse | hele bestanden | core, go, python, shell | vervangt; verwijdert wat een pakket niet meer levert |
| `.claude/skills/lighthouse-*/` | lighthouse | hele bestanden | core, expo, go, python, shell | idem |
| `.agents/skills/lighthouse-*/` | lighthouse | hele bestanden | core, expo, go, python, shell | idem |
| `.claude/agents/lighthouse-*.md` | lighthouse | hele bestanden | core | idem |
| `.claude/settings.json` | shared | voegt de hooks en `permissions.allow`-regels van lighthouse samen; de rest is van jou | core, go, python, shell (hooks), core (permissies) | vervangt alleen de items van lighthouse |
| `.lighthouse/lib/lighthouse_lib/` | lighthouse | hele bestanden | core | idem |
| `.lighthouse/scripts/` | lighthouse | hele bestanden | core, go, node, python, shell | idem |
| `.lighthouse/headings.toml` | lighthouse | heel bestand | core | idem |
| `.lighthouse/manifest.toml` | lighthouse | elke run opnieuw | — (init zelf) | schrijft het opnieuw |
| `.github/pull_request_template.md` | lighthouse | heel bestand | core | idem |
| `.github/workflows/lighthouse-*.yml` | lighthouse | hele bestanden, `@@key@@` ingevuld uit de config; alleen als elke sleutel gezet is (`ci.bot_client_id_var` en `ci.bot_private_key_secret` hebben geen default), anders niet geïnstalleerd | core | vervangt als elke sleutel gezet is; mist er een, dan blijft een geïnstalleerde workflow zoals hij is |
| `~/.config/lighthouse/host.toml` | yours | alleen bij `init`, alleen met Tailscale, na een eigen vraag | — (init zelf) | meldt alleen dat hij ontbreekt |

Voor elk gebied met units in het manifest (`AGENTS.md`, `.claude/settings.json` en de
bestandsgebieden) geldt bovendien: heb je het deel van lighthouse lokaal gewijzigd of
verwijderd, dan weigert `--upgrade` de hele run tot je `--force` geeft. Het paneel zegt dat bij
elk van die gebieden, en niet bij `lighthouse.toml` en `manifest.toml` (zie Anti-doelen).

De pakketkolom is een momentopname van vandaag; de test (§6) bewaakt hem, de tabel hier niet.

**Lezers** (voorstel, de bouwer vult aan): `AGENTS.md` en `CLAUDE.md` lezen de harnassen
(Claude Code, Codex, OpenCode); `.claude/skills/` en `.claude/agents/` leest Claude Code,
`.agents/skills/` Codex en OpenCode; `.claude/settings.json` leest Claude Code;
`lighthouse.toml` leest `pack/core/lib/config.py` (en via die alle skills en hooks);
`headings.toml` leest `pack/core/lib/headings.py`; `manifest.toml` leest `internal/manifest`;
de PR-template leest GitHub en `pack/core/skills/write-pr/SKILL.md`; de workflows leest GitHub
Actions; `host.toml` leest `internal/hostconfig` (voor `lighthouse usage` en `serve`). Elke
lezer is een pad of een naam uit de vaste lijst van §6 *(eigen keuze na spec-review, r1-f5)*.

## 5. Benaderingen

| | Vorm | Voor | Tegen |
|---|---|---|---|
| **A (gekozen)** | plattegrond langs de echte mappen, eigenaar als vulling, `--upgrade` als zone | je herkent je repo; eigenaar en upgrade liggen er als laag overheen | meer opmaakwerk dan een lijst |
| B | drie kolommen per eigenaar | beantwoordt "mag ik dit bewerken" het snelst | een lijst, geen kaart; je repo is onherkenbaar |
| C | ringen: jouw code in het midden, lighthouse aan de rand | beeldend | abstract; zo liggen de mappen niet |

Corneel koos A ("deze vorm").

## 6. Wat vastligt

**Pagina en data**

- `docs/site/repo.html` + `docs/site/repo.js`. De kop volgt `config.html` (`<html lang="en">`,
  terug-link, themaknop, `#serve-hint`, `<details class="why">`); het paneel
  `<aside id="detail">` en de knoppenrij volgen `loop.html` en `session.html`, want
  `config.html` heeft geen paneel. Titel: *Your repo after init* (keuze van Claude).
- Data in `docs/site/data/repo.json`, handgeschreven. Per gebied: `id`, `label` (het pad zoals
  de gebruiker het ziet), `room` (de kamer: `root`, `.claude`, `.agents`, `.lighthouse`,
  `.github`, `workstation`), `owner` (`lighthouse` | `shared` | `yours`), `how`, `upgrade`,
  `about` (één Engelse zin), `packs` (lijst, leeg voor init zelf), `readers` en `more` (link
  naar de referentiepagina of `config.html`). Plus `place`: de plaatssleutel uit de
  golden file van de Go-test (zie Tests), bijvoorbeeld `AGENTS.md`,
  `.claude/skills/lighthouse-*/` of `.github/workflows/lighthouse-*.yml`. `host.toml` heeft
  geen `place`. Daarnaast een lijst `rooms` (id, label), met de kamer `yours` voor *your code,
  your tests, …* zonder gebieden.
- Een nieuwe renderer `renderRepo(root, data, detail)` in `docs/site/map.js`, met een pure
  helper zonder DOM (zoals `configEntries`) die de tests via `tests/_site_node.py` aanroepen.
- De plattegrond is HTML (geneste `section`s in een CSS-grid, gebieden als `button`), geen SVG
  (keuze van Claude: rechthoeken in rechthoeken zijn DOM-werk; knoppen zijn toetsenbord- en
  schermlezer-vriendelijk; op telefoonbreedte stapelen de kamers).
- Data alleen via `textContent` en attributen (bestaande test op `innerHTML`).

**Inhaken op de site**

- Stap 2 van de systeemkaart krijgt één link *Map of your repo* naar `repo.html`: een nieuw,
  optioneel stapveld in `system.json` (bijvoorbeeld `map: {label, href}`) dat `renderStory`
  onder de vakken rendert, geen zevende item, want een item vraagt een `form` en `kind` die de
  link niet heeft *(eigen keuze na spec-review, r1-f6)*. De zes vakken
  houden hun huidige links (referentiepagina, de secties `rules` en `config`), en die secties
  blijven staan (keuze van Claude: een vak dekt vaak meerdere gebieden, zodat een link per vak
  naar één gebied half klopt; en `repo.html` rendert zijn gebieden uit JSON, dus statische
  ankers per gebied zouden de data dupliceren). Geen hash-afhandeling op `repo.html`.
- `index.html`: de tegel *Map 3 · planned · #101* wordt een werkende link naar `repo.html`;
  `tests/test_site_pages.py` (`MAP_PAGES`, de index-test met `#101`) en
  `tests/test_site_system.py` (hrefs van stap 2) gaan mee.
- `python3 -m tests.site_version` na elke wijziging aan site-JS of -CSS.

**Tests**

AC 5 loopt via een golden file, omdat Go `docs/` niet mag lezen: `tests/test_domains.py` faalt
op een `.go`-bestand dat `docs/` noemt, en `ci/domains.py` slaat de Go-job over bij een wijziging
die alleen onder `docs/` valt. De Python-job draait bij elke wijziging buiten `web/`, dus ook
bij `internal/`, `pack/` en `docs/` (keuze van Claude).

- **Go**: een test in `internal/initcmd` draait een echte `init` met alle pakketten
  (`o.Packs = nil`, `o.AddPacks` = alle pakketten onder `pack/`) in een lege temp-repo, met
  `o.Lookup` op een stub die elke sleutel beantwoordt. Zonder die stub installeert `init` geen
  workflow (`ci.bot_client_id_var` en `ci.bot_private_key_secret` hebben geen default), en dan
  ontbreekt de plek `.github/workflows/lighthouse-*.yml` in de golden *(eigen keuze na
  spec-review, r1-f1)*. Hij
  vat elk geschreven pad samen tot een plaatssleutel: een map van lighthouse wordt die map
  (`.claude/hooks/lighthouse/`, `.lighthouse/lib/lighthouse_lib/`, `.lighthouse/scripts/`),
  een skill `<skilldir>lighthouse-*/`, een agent `.claude/agents/lighthouse-*.md`, een
  workflow `.github/workflows/lighthouse-*.yml`, al het andere het pad zelf. Per sleutel
  schrijft hij één regel `<sleutel>\t<soorten>\t<pakketten>` (soorten en pakketten uit
  `.lighthouse/manifest.toml`; voor `CLAUDE.md`, `lighthouse.toml` en `manifest.toml`, die
  buiten het manifest vallen, `init` als soort; de pakketten van `lighthouse.toml` leidt de test
  af als core plus elk gekozen pakket met een niet-lege `Config`, zoals `configTables`, en
  `CLAUDE.md` en `manifest.toml` krijgen geen pakket *(eigen keuze na spec-review, r1-f4)*), gesorteerd,
  en vergelijkt met `internal/initcmd/testdata/places.golden`. Met `-update` schrijft hij de
  golden opnieuw. Schrijft `init` naar een nieuwe plek, dan wordt de Go-test rood.
- **Python** (`tests/test_site_repo.py`, in de stijl van `tests/test_site_config.py`) leest de
  golden en `repo.json`:
  - elke regel van de golden hoort bij precies één gebied (`place`), en elk gebied met een
    `place` staat in de golden. Na een `-update` wordt dus de Python-test rood tot de kaart
    meegaat;
  - de `packs` van een gebied zijn exact de pakketten uit de golden;
  - `owner` klopt met de soorten: alleen `file` → `lighthouse`; `block`, `hook` of
    `permission` → `shared`; `init` → zoals vastgelegd per plek (`CLAUDE.md` yours,
    `lighthouse.toml` shared, `manifest.toml` lighthouse);
  - precies één gebied zonder `place`, in de kamer `workstation` (`host.toml`);
  - geldige `owner` en `room`; elke lezer is een bestaand pad (bestand of map) onder `pack/`,
    `internal/` of `cmd/` dat het gebied noemt, of een naam uit een vaste lijst (`Claude Code`,
    `Codex`, `OpenCode`, `GitHub`, `GitHub Actions`). "Noemt" = de basename van `place` (of van
    `label` zonder `place`) zonder `*` en zonder slash aan het eind, bijvoorbeeld
    `headings.toml`, `host.toml` of `lighthouse-`, staat letterlijk in het bestand of in een
    bestand van de map *(eigen keuze na spec-review, r1-f5)*; elke `more`-link bestaat;
  - geen gebied `lighthouse.toml` of `.lighthouse/manifest.toml` noemt `--force` in `upgrade`;
  - de zone *--upgrade touches* bevat precies de gebieden met `owner` `lighthouse` of `shared`;
  - mutatietests op een kopie van de data en van de golden.
- Een changelog-fragment `changelog.d/issue-101-plattegrond-na-init.md`, `type: added`, Engels.

## 7. Aan de bouwer

- De exacte opmaak van de kamers (verhoudingen, volgorde binnen een kamer) zolang het grondplan
  van §3 herkenbaar blijft, ook op telefoonbreedte.
- De Engelse teksten per gebied (`about`, `how`, `upgrade`), gecontroleerd tegen de code.
- De lezers per gebied, met de regel uit §6; vindt de bouwer voor een gebied geen lezer, dan
  meldt hij dat en verzint hij er geen.
- Het exacte regelformaat van `places.golden`, zolang het één regel per plaatssleutel is en
  Python het zonder Go kan lezen.
- Bijvangst van `init` die geen plek is: het opruimen van `__pycache__` en lege mappen bij
  verwijderen, en `chmod 0755` op bestaande hooks. Die staan niet als gebied op de kaart; de
  Go-test negeert ze.
