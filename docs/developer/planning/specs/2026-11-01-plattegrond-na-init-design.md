# Je repo na `lighthouse init`: een visuele boom (#101)

Datum: 2026-11-01. Versie 3. Issue: #101 (heropend na PR #188). Milestone: *Kaarten: lighthouse in beeld*.
Bouwt op [`2026-10-27-kaarten-design.md`](2026-10-27-kaarten-design.md) (§3 en K3) en
[`2026-10-31-systeemkaart-design.md`](2026-10-31-systeemkaart-design.md) (stap 2).

*Versie 3 vervangt de plattegrond van versie 2.* PR #188 bouwde die plattegrond met zones,
legenda en paneel. Corneel vond hem lelijk en onduidelijk. De zoneknoppen herhaalden de indeling,
details als de `--force`-regel stonden in de legenda, en het onderscheid vervangen/aangeraakt was
een detail waar niemand om vroeg. In het gesprek van 2026-11-01 koos hij een visuele boom met
alleen `init` (mockup *A, versie 2*). Wat `--upgrade` doet, staat in #192.

## 1. Doel en waarom

De systeemkaart (`docs/site/system.html`) heeft drie stappen. Stap 2, *lighthouse init*, noemt
zes vakken en linkt naar deze kaart. Wie de kaart opent, wil in één blik zien wat lighthouse in
zijn repo zet, waar dat staat, en welke van zijn eigen bestanden `init` aanraakt. Voor elk
onderdeel wil hij kunnen doorklikken naar de pagina waar het verder uitgelegd is.

De kaart is een overzicht, geen handleiding. Ze beschrijft wat er na `init` staat. Ze beschrijft
niet hoe je lighthouse bijwerkt.

## 2. Doelen, niet-doelen, anti-doelen

**Doelen**

- Elke plek waar `init` schrijft is één rij in de boom, behalve `CLAUDE.md` (zie §4).
- Per rij: wat het is (kleur en teken van de andere kaarten), of `init` het toevoegt of een
  bestand van jou aanpast, en een link naar de pagina waar het verder staat.
- Een schakelaar toont wat `init` toevoegt en wat hij in een bestand van jou aanpast.
- Wat al van jou in een map staat, staat ook op de kaart: het blijft.
- `host.toml` op je werkstation staat apart en valt op. Die zit niet in de repo, dus een
  teamgenoot of een andere machine heeft hem niet.
- Een test faalt als `init` naar een plek schrijft die niet op de kaart staat.

**Niet-doelen**

- Geen `--upgrade`, geen `--force`, geen eigenaarschap per gebied: dat is #192.
- Geen lijst van alle skills, agents en hooks: die staat op de referentiepagina. De rij linkt
  daarheen.
- Geen sleutels van `lighthouse.toml` of `host.toml`: die staan op `config.html`.
- Geen wijziging aan wat `init` doet. `internal/` en `pack/` blijven ongemoeid, op de Go-test na.

**Anti-doelen**

- Een rij met twee dingen erin, zoals `lib/ · scripts/`. Eén rij is één ding.
- Een pijltje ↗ met uitleg erbij. Het pijltje is alleen een link.
- Uitleg in een legenda. Wat uitleg nodig heeft, staat als gewone tekst onder de kaart.
- Kleuren of tekens die afwijken van de andere kaarten.
- Knoppen die herhalen wat de indeling al laat zien.

## 3. De vorm

Goedgekeurd door Corneel in het gesprek ("ja veel strakker, zo doen"). De formele goedkeuring
(AC 1) is een comment in #101 op deze schets.

```
[ Everything | Adds | Edits your file ]          ← schakelaar

your repo/                                        ← donkere rand (--badge-bg)
├─ § AGENTS.md                         ↗           rules
├─ ⚙ lighthouse.toml                   ↗           config
├─ .claude/
│  ├─ ⚡ hooks/lighthouse/             ↗           hook
│  ├─ ✓ skills/lighthouse-*/           ↗           lighthouse (skill)
│  ├─ ◆ agents/lighthouse-*.md         ↗           agent
│  ├─ ⚙ settings.json                              config
│  └─ your own skills, agents and settings         yours (grijs, alleen rand)
├─ .agents/
│  ├─ ✓ skills/lighthouse-*/           ↗           lighthouse (skill)
│  └─ your own skills
├─ .lighthouse/
│  ├─ ⚙ lib/                                       config
│  ├─ ⚙ scripts/
│  ├─ ⚙ headings.toml
│  └─ ⚙ manifest.toml
├─ .github/
│  ├─ ⑂ pull_request_template.md       ↗           github
│  ├─ ⑂ workflows/lighthouse-*.yml     ↗
│  └─ your own workflows
└─ your code, your tests, your config, …

On your workstation                               ← eigen vak, rand --session
  ⚙ ~/.config/lighthouse/host.toml     ↗
  Only with Tailscale. It is not in the repo, so a teammate or another machine does not have it.

Tekst onder de kaart: wat .lighthouse/ bevat, wat settings.json krijgt, wat host.toml doet.
```

**De boom.** Een ingesprongen lijst met verbindingslijnen, zoals een bestandsboom. Mappen zijn
kopjes zonder kleur. De rijen zijn gekleurde vakken met het teken ervoor. Op telefoonbreedte
blijft de boom één kolom. Een lang pad breekt af, zodat de pagina niet horizontaal scrollt.

**Kleur en teken.** De `kind-*`-klassen en de tekens uit `SORTS` in `docs/site/map.js`:

| Rij | Klasse | Teken |
|---|---|---|
| `AGENTS.md` | `kind-rules` | § |
| `lighthouse.toml`, `settings.json`, `.lighthouse/*`, `host.toml` | `kind-config` | ⚙ |
| `hooks/lighthouse/` | `kind-hook` | ⚡ |
| `skills/lighthouse-*/` (beide) | `kind-lighthouse` | ✓ *(keuze van Claude: het teken van de standaardskill, zoals in de mockup)* |
| `agents/lighthouse-*.md` | `kind-agent` | ◆ |
| PR-template, workflows | `kind-github` | ⑂ |
| *your own …* | geen kleur, alleen een rand (`--border`), tekst `--muted` | geen |

**De schakelaar.** Drie standen in een `role="group"` met `aria-pressed`: *Everything* (begin),
*Adds* en *Edits your file*. In *Adds* en *Edits your file* dimmen de vakken die er niet bij
horen, zoals de chips op de andere kaarten (`.dim`). De grijze rijen *your own …* dimmen niet: ze
zijn geen werk van `init`. Bij *Edits your file* blijven er drie over: `AGENTS.md`,
`lighthouse.toml` en `settings.json`.

**Links.** Een ↗ is een gewone `a` en staat altijd zonder tekst. De toegankelijke naam komt uit de
rij zelf. Geen paneel en geen klik op het vak.

| Rij | Link |
|---|---|
| `AGENTS.md` | `system.html#rules` |
| `lighthouse.toml`, `host.toml` | `config.html` |
| `hooks/lighthouse/` | `reference.html#hooks` |
| `skills/lighthouse-*/` (beide) | `reference.html#skills` |
| `agents/lighthouse-*.md` | `reference.html#agents` |
| PR-template, workflows | `reference.html#github` |
| `settings.json`, de vier rijen van `.lighthouse/` | geen: uitgelegd onder de kaart |

**Onder de kaart.** Gewone alinea's, geen legenda:

- `.lighthouse/`: wat `lib/`, `scripts/`, `headings.toml` en `manifest.toml` zijn. Ze staan
  nergens anders op de site.
- `settings.json`: `init` voegt de hooks van lighthouse en zijn `permissions.allow`-regels toe.
  De rest van het bestand is van jou.
- `host.toml`: waarom het ertoe doet. Hij staat alleen op deze machine, `init` vraagt eerst
  (tenzij `--yes`), en wat hem leest werkt op een andere machine niet zonder eigen `host.toml`.

De zinnen worden gecontroleerd tegen de code (`buildPlan` en `planConfig` in
`internal/initcmd/initcmd.go`, `offerHost` in `internal/initcmd/host.go`, de lezers van
`internal/hostconfig`).

## 4. De rijen

| Rij (`place`) | Map | Soort | `init` |
|---|---|---|---|
| `AGENTS.md` | root | rules | edits: één managed block per pakket |
| `lighthouse.toml` | root | config | edits: voegt ontbrekende tabellen en sleutels toe |
| `.claude/hooks/lighthouse/` | `.claude/` | hook | adds |
| `.claude/skills/lighthouse-*/` | `.claude/` | skill | adds |
| `.claude/agents/lighthouse-*.md` | `.claude/` | agent | adds |
| `.claude/settings.json` | `.claude/` | config | edits: hooks en permissies samengevoegd |
| `.agents/skills/lighthouse-*/` | `.agents/` | skill | adds |
| `.lighthouse/lib/lighthouse_lib/` | `.lighthouse/` | config | adds (label `lib/`) |
| `.lighthouse/scripts/` | `.lighthouse/` | config | adds |
| `.lighthouse/headings.toml` | `.lighthouse/` | config | adds |
| `.lighthouse/manifest.toml` | `.lighthouse/` | config | adds |
| `.github/pull_request_template.md` | `.github/` | github | adds |
| `.github/workflows/lighthouse-*.yml` | `.github/` | github | adds |
| `~/.config/lighthouse/host.toml` (geen `place`) | werkstation | config | adds |

**`CLAUDE.md` staat er niet op** (Corneel). `init` schrijft hem alleen als `AGENTS.md` en
`CLAUDE.md` allebei ontbreken, met één regel `@AGENTS.md`, en raakt een bestaande nooit aan. De
test sluit hem uit met een vaste lijst `OFF_THE_MAP = {"CLAUDE.md": "<reden>"}`. Een tweede
uitzondering vraagt een wijziging van die lijst, en dus een review.

**Toevoegen of aanpassen** volgt uit de soorten in de golden. `block`, `hook` of `permission`
betekent *edits*: `init` zet iets in een bestand dat ook van jou is. Alleen `file` betekent
*adds*. Bij soort `init` geldt een vaste lijst: `lighthouse.toml` *edits*,
`.lighthouse/manifest.toml` *adds*.

## 5. Benaderingen

| | Vorm | Voor | Tegen |
|---|---|---|---|
| **A (gekozen)** | ingesprongen boom, gekleurde rijen | leest als je repo; compact; telefoon | minder "kaart" dan B |
| B | mappen als witte kaarten op een donker bord | visueler | rijen per map minder goed te volgen |
| C | takken van links naar rechts | beeldend | klapt op een telefoon toch om naar A |

Corneel koos A, en daarna versie 2: één ding per rij, pijltjes zonder tekst, *yours* per map, en de
schakelaar adds/edits in plaats van init/upgrade.

**Hoe het gebouwd wordt** *(keuze van Claude, door Corneel bevestigd)*: verder op wat PR #188
neerzette. De Go-test met `places.golden` blijft, want alleen zo faalt een test als `init` een
nieuwe plek krijgt (AC 5). Een boom vast in de HTML zou eenvoudiger zijn, maar dan ziet geen test
dat nog.

## 6. Wat vastligt

**Data.** `docs/site/data/repo.json`, handgeschreven:

- `dirs`: de mappen in volgorde (`root`, `.claude`, `.agents`, `.lighthouse`, `.github`), elk met
  `label` en optioneel `yours` (de tekst van de grijze rij).
- `rows`: per rij `id`, `place` (de plaatssleutel uit de golden; niet bij `host.toml`), `label`,
  `dir` (een map uit `dirs`, of `workstation`), `kind`, `act` (`adds` | `edits`) en optioneel
  `href`.
- `yours`: de tekst van de laatste rij (*your code, your tests, your config, …*).
- `workstation`: de korte tekst in het vak *On your workstation*.

**Weg uit PR #188:** `owner`, `upgrade`, `how`, `about`, `packs`, `readers`, `more`, de zones
(`REPO_ZONES`), het paneel, de legenda, en in `tests/test_site_repo.py` de toetsen op eigenaar,
lezers en `--force`. AC 3 vraagt geen lezers meer.

**Renderer.** `renderRepo(root, data)` in `docs/site/map.js`, met een pure helper
`repoEntries(data)` zonder DOM, die de tests via `tests/_site_node.py` aanroepen. De boom is
HTML: geneste `ul`/`li` en native `a`, geen SVG. Data gaat alleen via `textContent` en attributen.

**Pagina.** `docs/site/repo.html` + `repo.js` blijven. De kop volgt `config.html`. De
`<details class="why">` wordt korter. De legenda en `<aside id="detail">` gaan weg. Daaronder komt
de tekst van §3. De link vanaf de systeemkaart, de tegel op de index en de link in
`docs/getting-started.md` blijven.

**Tests** (`tests/test_site_repo.py`, in de stijl van `tests/test_site_config.py`):

- Elke regel van de golden is precies één rij, behalve wat in `OFF_THE_MAP` staat. Elke rij met
  een `place` staat in de golden. Er is precies één rij zonder `place`, en die zit in
  `workstation`.
- `act` klopt met de soorten (§4).
- `kind` is een bekende soort, en `dir` een map uit `dirs` of `workstation`.
- Elke `href` bestaat, met anker: voor `system.html` tegen de sectie-id's in `data/system.json`,
  anders `id="…"` in de HTML.
- Er is geen rij met een scheidingsteken (`·`, `,`) in zijn label (anti-doel: één ding per rij).
- Mutatietests op kopieën van data en golden.
- `repoEntries` geeft bij *Edits your file* precies `AGENTS.md`, `lighthouse.toml` en
  `settings.json`.

De Go-test (`internal/initcmd/places_test.go`, `testdata/places.golden`) verandert niet.

**Site.** `python3 -m tests.site_version` na elke wijziging aan site-JS of -CSS. Geen nieuwe
kleuren: alleen de bestaande tokens en `kind-*`-klassen. Licht en donker via die tokens.

**Changelog.** Een fragment `changelog.d/issue-101-repokaart-boom.md`, `type: changed`, Engels.

**Beslist na de bouwerscheck** *(keuzes van Claude)*:

- *Adds* laat alleen de rijen met `act` `adds` vol zien, en *Edits your file* alleen die met
  `edits`. De rij `host.toml` dimt in *Edits your file*. Het vak *On your workstation* zelf
  dimt niet. De grijze rijen *your own …* en *your code, …* dimmen nooit.
- `repoEntries(data, mode)`, met `mode` ∈ `everything` | `adds` | `edits`, geeft de mappen in
  volgorde. Elke map heeft zijn rijen, en elke rij een `dim`-vlag. Het werkstation is een eigen
  item aan het eind. De tests vinden rijen op `place` of `label`, niet op `id`.
- `repo.json` krijgt een nieuwe vorm (`dirs`/`rows`/`dir` in plaats van `rooms`/`areas`/`room`).
  De oude id's vervallen.
- Het vak *On your workstation* krijgt een rand in `--session`. De rij erin is `kind-config`.
- De toets "één ding per rij" geldt alleen voor `label` van een rij, niet voor de teksten van
  *yours* of van het werkstation.
- De titel blijft *Your repo after init*. De ondertitel wordt Engels en zonder `--upgrade`,
  bijvoorbeeld *Everything lighthouse init puts in your repo, and the files of yours it edits.*

## 7. Aan de bouwer

- De Engelse teksten onder de kaart en in het vak *On your workstation*, gecontroleerd tegen de
  code.
- De opmaak van de boom (lijnen, inspringing, afstand), zolang hij leest als mockup *A, versie 2*,
  ook in donker en op 360px.
- Of de map-kopjes een teken krijgen. De mockup heeft er geen.
