# Pins voor overgenomen skills, Superpowers en geïntegreerde tools (#325)

## Doel en waarom

Lighthouse hangt aan tekst en gereedschap van anderen en legt nergens vast op welke versie.
Een autoupdate van Superpowers brak de controller al twee keer (#34, #237); dat bleek pas in een
gestrande run. kenn-forge, roborev en codex lopen intussen stil achter: lokaal v0.5.1, v0.69.0
en 0.157.0, upstream v0.8.0, v0.75.0 en 0.169.0 (gemeten 2026-11-15). De velden die
[`place-instruction`](../../../../pack/core/skills/place-instruction/SKILL.md) voor een
overgenomen skill voorschrijft (`upstream`, `upstream-commit`, `upstream-path`) leest niets
terug. Een pin zonder lane verstart stil.

Dit issue maakt van elke afhankelijkheid een beoordeelde pin, met één lane die per pin
`current`, `behind` of `error` meldt en een achterstand als PR of issue aanbiedt.

Besloten (eigenaar, 2026-11-14 en 2026-11-15):

- **Lighthouse staat volledig los van personal_ai_agent (paia).** Geen pin, script of bron
  wijst nog naar paia.
- **Eerst intern, daarna in de skills.** Fase 1 (dit issue): het mechanisme en de pins van
  lighthouse zelf, gedraaid op de lighthouse-repo. Fase 2 (vervolgissue): `place-instruction`
  en een pack-workflow, zodat elke repo de lane via `lighthouse init` krijgt.
- **De cedanl-tekst gaat eruit.** cedanl/.github heeft geen licentie; wat `place-instruction`
  daaruit vertaalde, wordt eigen tekst.
- **`upstream` blijft één veld per skill.** Een skill met tekst uit twee bronnen houdt er één
  als upstream; de rest wordt eigen tekst.
- **`test-standard` wordt `own`.** Geen van de drie bronnen is een hoofdbron; de gedeelde
  zinsdelen worden eigen tekst. Een inspiratie- of bronnensectie hoort niet in een skill: een
  agent leest die bij elke aanroep.
- **Het PR-pad hoort bij fase 1.** Een schone merge wordt een PR met toelichting, zoals de
  bumplane van paia al doet; een conflict wordt een issue.
- **Het lokale signaal meldt, het weigert niet.** Een afwijkende Superpowers houdt de
  controller niet tegen.
- **Go-native.** Eén Go-commando en één workflow, geen overgezette paia-scripts.

## Doelen

- Elke afhankelijkheid van lighthouse heeft een vastgelegde, beoordeelde versie: de
  Superpowers-plugin, kenn-forge, roborev en codex. Na ontwerp 1 heeft lighthouse geen
  overgenomen skill of bestand meer; dat deel van de lane is af als het op fixtures werkt en
  pakt de eerste echte overname op zonder codewijziging.
- `lighthouse pins check` meldt per pin `current`, `behind` of `error`.
- Een wekelijkse workflow biedt een achterstand aan: een PR voor een overgenomen skill die
  schoon mergt, een issue bij een conflict of een verdwenen pad, één issue per achterstand van
  plugin of tool met de release-notes van de tussenliggende versies.
- Lokaal meldt lighthouse wanneer de geïnstalleerde Superpowers of een geïnstalleerde tool
  afwijkt van de pin.
- De herkomst in de repo klopt: `SOURCES.toml` zonder paia-herkomst, `origin` op elke skill.

## Niet-doelen

- Fase 2: `place-instruction` uitbreiden met de lane en een pack-workflow die `init` in elke
  repo installeert. Vervolgissue, na deze spec.
- Een pin automatisch ophogen. Ophogen is altijd een mensenbesluit (zie *Een pin ophogen*).
- Tools zonder GitHub-releases meten. Vandaag heeft elke pin een GitHub-repo met releases; een
  andere bron is een nieuwe `source`-soort, later.
- De geïnstalleerde versie bijwerken (een plugin of tool installeren of downgraden).

## Anti-doelen

- **Een pin die niet te meten is, telt als `current`.** Ontbrekend, ongeldig, upstream
  onbereikbaar, een tag die niet parseert, een tool die geen versie geeft: altijd `error`, luid.
- **Een PR van de lane wordt automatisch gemerged.** Een skill is een instructie voor een
  agent; een mens leest de diff. Ook de dispatcher mag hem niet kunnen mergen.
- **De meting voert upstream-code uit.** Ze leest git-objecten en de release-API, verder niets.
- **Een tweede issue voor dezelfde achterstand.**
- **De controller weigert een run om een versie-afwijking.**

## Randvoorwaarden

- Agent Skills-spec: onder `metadata:` alleen strings. Daarom één `upstream` per skill, geen
  lijst ([`place-instruction`](../../../../pack/core/skills/place-instruction/SKILL.md), stap 5).
- [`internal/forge`](../../../../internal/forge/forge.go) is de enige weg naar de forge. Het
  kent vandaag geen issue of PR aanmaken, geen issue- of PR-body bijwerken en geen releases van
  een andere repo; die methodes komen erbij.
- Een write in CI gaat via de GitHub App uit `[ci]` (`PAIA_BOT_*`), zoals in
  [`unblock-dependents.yml`](../../../../pack/core/workflows/unblock-dependents.yml). Een PR van
  `GITHUB_TOKEN` start geen CI; die van de App wel.
- De dispatcher merget alleen de eigen PR van een run, niet-draft, bij board-status
  `Controller` ([`decide.go`](../../../../internal/dispatch/decide.go), `mergeReady` en
  `ownOpenPR`).
- `.lighthouse/` is van `init` (`manifest.toml`); handwerk hoort daar niet.
- Superpowers staat per scope in `~/.claude/plugins/installed_plugins.json`: `user`, en
  `project` met een `projectPath`. De versies verschillen per scope (nu `user` 7.0.2, sommige
  paia-worktrees 7.0.1).
- Tags: codex tagt `rust-v0.169.0`, de andere drie `vX.Y.Z`. kenn-forge publiceert onder
  `kenn-io/forge`.
- Elke PR heeft een `changelog.d/`-fragment en een body die `pr-body-gate` accepteert.

## Aanpakken

- **A. Go-native (gekozen).** Pakket `internal/pins` en commando `lighthouse pins check`; een
  wekelijkse workflow draait het en maakt PR's en issues via `internal/forge`. Eén mechanisme
  voor skills, bestanden, plugin en tools; past bij de Go-ports van #340 en #342 en bij de
  forge-interface van #346.
- **B. De drie paia-scripts overzetten.** Afgevallen: Python, waar lighthouse van afgaat
  ([zonder-python](2026-11-07-zonder-python-design.md)); drie mechanismen waar #325 er één
  vraagt; en weer een band met paia.
- **C. Renovate of Dependabot voor de tools.** Afgevallen: volgt geen skilltekst, toetst de
  geïnstalleerde versie niet lokaal, en voegt een externe dienst toe. Het blijven twee
  mechanismen.

## Ontwerp

### 1. Herkomst rechtzetten (eerst, los van de lane)

- **`pack/SOURCES.toml`.** De 56 regels met `source_path` en `source_sha` (allemaal paia,
  bootstrap van #12; 14 met een lijst als `source_path`) worden `new = true`. Een regel die nog `source_path` heeft, krijgt een
  verplicht `source_repo` (`owner/repo`); `source_sha` blijft de volle SHA, en `source_path`
  is dan één string, geen lijst *(eigen keuze na spec-review, r1-f13)*. Een bestand in de map
  van een overgenomen skill krijgt `pin = "<sleutel>"` en verder geen bronvelden: de pin staat
  in de frontmatter van die skill *(eigen keuze na spec-review, r1-f1)*. Na deze stap heeft
  geen enkele regel meer een bron; de lane kan er wel een lezen (zie *Wat een pin is*).
  `tests/test_sources.py` eist precies één van `new = true`, `pin` of de drie bronvelden; de kop
  van `SOURCES.toml` noemt paia niet meer.
- **`place-instruction`.** Wat uit cedanl/.github `create-skill` komt (stap 1, stap 2 en de
  `source:`-rij van de tabel in stap 5), wordt in eigen woorden herschreven. De regels zelf
  blijven gelden; alleen de tekst is nieuw. `origin: own`, geen `upstream`.
- **`test-standard`.** Blijft `origin: own`, zonder `upstream`. De zinsdelen die letterlijk
  (zes woorden of meer) gelijk zijn aan een van drie bronnen, worden eigen tekst:
  - superpowers `skills/test-driven-development/writing-good-tests.md`: "what production change
    should make this test fail", "a fake that accepts anything proves only that…", "missing
    state change or side effect", "empty or default return, missing validation on empty, zero
    or…";
  - kenn-io/agentsview `.agents/skills/testing-without-tautologies/SKILL.md`: dezelfde eerste
    zin, "first decide whether the production change was intended", "shell scripts do not read
    them, run the script against…";
  - kenn-io/forge `skills/small-change-verification/SKILL.md`: "this is a class change; the
    likely regression surface…".

  De sectie *Inspiration* gaat eruit. `source: "#22"` blijft. De meting om dit te controleren:
  geen 6-gram van `test-standard` komt nog voor in een van de drie bronnen (eenmalige check bij
  de PR, geen blijvende test).
- **`origin` op elke skill.** De 15 `SKILL.md`-varianten onder `pack/` zonder `origin` krijgen
  `origin: own`. Daarna `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopieën
  onder `.claude/skills/` mee in dezelfde commit.

### 2. Wat een pin is

Drie soorten, op drie plekken. Wat bij de repo hoort (de beoordeelde versie) staat in de repo;
wat per host verschilt (de geïnstalleerde versie) leest lighthouse live, zonder `host.toml`.

| Soort | Waar de pin staat | Upstream-meting | Lokale meting |
|---|---|---|---|
| Overgenomen skill | frontmatter: `upstream`, `upstream-commit`, `upstream-path` | inhoud van `upstream-path` op pin vs. default branch | — |
| Overgenomen bestand | `pack/SOURCES.toml`: `source_repo`, `source_sha`, `source_path` | idem, per `source_path` | — |
| Plugin of tool | `lighthouse.toml`, `[pins.<naam>]` | laatste release vs. pin | geïnstalleerd vs. pin |

`lighthouse.toml` en niet `.lighthouse/pins.toml`: `.lighthouse/` is van `init`, en een pin
ophogen is handwerk. In fase 1 staat de sectie alleen in de lighthouse-repo.

```toml
[pins.superpowers]
kind = "plugin"            # plugin | tool
repo = "obra/superpowers"
version = "7.0.2"

[pins.kenn-forge]
kind = "tool"
repo = "kenn-io/forge"
version = "0.5.1"
command = "kenn-forge"     # default: de naam van de pin
version_args = ["version"] # default: ["--version"]

[pins.roborev]
kind = "tool"
repo = "roborev-dev/roborev"
version = "0.69.0"
version_args = ["version"]

[pins.codex]
kind = "tool"
repo = "openai/codex"
version = "0.157.0"
tag_prefix = "rust-v"      # default: "v"
```

De beginwaarden zijn wat nu geïnstalleerd is: daarmee draait lighthouse vandaag. De eerste run
meldt dus drie keer `behind`, en dat is de bedoeling.

`upstream` in de frontmatter is een URL plus licentie (`place-instruction`, stap 1). De lane
haalt `owner/repo` uit het eerste `https://github.com/<owner>/<repo>`; een `upstream` waaruit dat
niet lukt, is `error`.

**Vindplaats en sleutel** *(eigen keuze na spec-review, r1-f2)*. De meting zoekt skillpins alleen
in `pack/*/skills/`, nooit in de geïnstalleerde kopieën (`.claude/skills/`, `.agents/skills/`):
die dragen dezelfde frontmatter en zouden elke pin twee tot vier keer tellen. De sleutel van een
pin is stabiel en uniek, en dezelfde in uitvoer, branchnaam en marker:

- skill: de skillmap relatief aan `pack/`, bijvoorbeeld `core/skills/test-standard`. De
  varianten `claude/SKILL.md` en `agents/SKILL.md` in die map vormen samen één pin en moeten
  dezelfde `upstream`, `upstream-commit` en `upstream-path` dragen; verschillen ze, dan is de pin
  `error`. Bij de merge hoort het upstream-`SKILL.md` bij elke variant-`SKILL.md`; elk ander
  bestand bij hetzelfde relatieve pad in de skillmap;
- overgenomen bestand: zijn `path` uit `SOURCES.toml`;
- plugin of tool: de naam van `[pins.<naam>]`.

Voor een `SKILL.md` en de bestanden in zijn map is de frontmatter de bron van waarheid;
`SOURCES.toml` verwijst daar alleen naar (`pin`).

### 3. De meting: `lighthouse pins check`

Twee metingen, apart te draaien:

- **Upstream** (netwerk): elke pin tegen upstream. Voor CI en voor de hand.
- **Lokaal** (geen netwerk): elke plugin- en toolpin tegen wat op deze host staat.

Zonder vlag draait het commando beide. De vlagnamen zijn aan de bouwer. Elke meting geeft per
pin een eigen uitslag; ze worden nooit tot één status samengevoegd *(eigen keuze na
spec-review, r1-f3)*:

- tekst: één regel per pin per meting, `<sleutel> upstream <status> <detail>` en
  `<sleutel> lokaal <status> <detail>`;
- `--json`: per pin een object met `key`, `kind` en de velden `upstream` en `local`, elk
  `{status, detail}` of afwezig als die meting niet draaide of niet voor die soort geldt.

Exitcode 1 zodra één uitslag van één meting `error` is, anders 0; `behind` en `ahead` zijn geen
fout van het commando.

**Skill en bestand, upstream.** Een bare, partial clone (`--filter=blob:none`) van
`owner/repo` in een tijdelijke map; geen checkout, geen hooks, geen submodules. Vergelijk de
tree van `upstream-path` op de pin met die op de default branch van upstream:

- gelijk → `current`, ook als upstream-HEAD verschoof;
- ongelijk → `behind`, met de commits die het pad raakten;
- pin-commit niet te vinden, pad bestaat niet op de pin of niet meer op HEAD, clone mislukt,
  veld ontbreekt of is ongeldig → `error`. Een pad dat upstream verdween is `error` en geen
  `behind`: daar valt niets te mergen.

**Plugin en tool, upstream.** Alle releases van `repo` via de release-API, met paginering tot de
laatste pagina, zonder drafts en prereleases *(eigen keuze na spec-review, r1-f8: de release
van codex 0.157.0 staat op plek 146 van 200)*. Een tag zonder `tag_prefix`, of een rest die
geen `MAJOR.MINOR.PATCH` is, wordt overgeslagen als het een ander soort tag is en is `error` als
de tag van de pin zelf zo niet parseert. De release van de pin moet bestaan onder de bruikbare
releases *(eigen keuze na spec-review, r1-f4)*:

- laatste release hoger dan `version` → `behind`, met de tussenliggende releases;
- gelijk → `current`;
- de release van de pin bestaat niet, de laatste release is lager dan `version`, er is geen
  enkele bruikbare release, of de API is onbereikbaar → `error`.

**Plugin, lokaal.** Superpowers uit `installed_plugins.json`, naam `superpowers@<marketplace>`.
De scope volgt de repo, in deze volgorde: `project` met `projectPath` gelijk aan de huidige
worktree, dan gelijk aan de hoofd-checkout (via `git rev-parse --git-common-dir`), dan `user`.
Geen bestand of geen passend item → `error`. Wijkt `version` af van de pin → een melding met
beide versies (`behind` als lager, `ahead` als hoger: hoger is niet beoordeeld en dus ook geen
`current`).

**Tool, lokaal.** `<command> <version_args>`, met een time-out (`kenn-forge` en `roborev`
kennen geen `--version`, alleen het subcommando `version`). Het eerste semver-token in de
uitvoer telt, compleet: `0.169.0-alpha.11` blijft `0.169.0-alpha.11` en is nooit gelijk aan
een pin `0.169.0`. Vergelijken gaat op semver-voorrang, dus een prerelease van de pinversie is
`behind` *(eigen keuze na spec-review, r1-f7)*.
Een tool die lokaal draait is geïnstalleerde code van de gebruiker, geen upstream-code die de
meting ophaalt. Niet op PATH, time-out, of geen versie in de uitvoer → `error`. Afwijking zoals
bij de plugin.

De lokale meting voegt de status `ahead` toe; de upstream-meting kent alleen de drie uit #325.

### 4. Lokaal signaal

`lighthouse init` (ook `--upgrade`) draait de lokale meting en meldt elke afwijking en elke
`error`, zoals het nu een ontbrekende tool meldt
([`internal/deps`](../../../../internal/deps/deps.go)): melden, niet stoppen. De meldingen gaan
naar stderr en de exitcode van `init` verandert niet; stdout blijft schoon voor de dogfood-stap
(`No changes.`) in CI, waar geen van de tools staat *(eigen keuze na spec-review, r1-f12)*. De controller
(tick, dispatch) doet niets met pins. Dat is een besluit, geen vergeten stap.

### 5. De lane: `.github/workflows/pins.yml`

In fase 1 een workflow van de lighthouse-repo zelf, niet uit `pack/`. Wekelijks (`schedule`)
en met `workflow_dispatch`. Hij draait `go run ./cmd/lighthouse pins lane` (naam aan de
bouwer) met een token van de GitHub App uit `[ci]`, rechten `contents`, `pull-requests` en
`issues` op write. De logica zit in Go; de YAML blijft dun. Vandaag vraagt lighthouse die App
alleen `issues: write`; of hij de andere twee rechten heeft en naar `pins/*` mag pushen
(rulesets), controleert de bouwer vóór de eerste run. Ontbreekt een recht, dan is dat een
instelling in de App, geen andere App.

- **Eén run tegelijk** *(eigen keuze na spec-review, r1-f6)*. Eén `concurrency`-groep voor de
  hele workflow, `cancel-in-progress: false`: zoeken en aanmaken zijn niet atomair, dus een
  geplande run en een handmatige start mogen niet overlappen.
- **Wie commit** *(eigen keuze na spec-review, r1-f11)*. De App pusht, maar auteur en committer
  van een lane-commit zijn `github-actions[bot]`
  (`41898282+github-actions[bot]@users.noreply.github.com`). Die staat al in `bots` van
  [`cla-signatures.json`](../../../../.github/cla-signatures.json), als *commits from workflows
  in this repository*; `paia-bot[bot]` staat er niet in, en `check_cla.py` weigert hem in
  dezelfde PR toe te voegen.
- **Per pin, niet alles-of-niets** *(eigen keuze na spec-review, r1-f8)*. Mislukt een
  forge-write of een push voor één pin, dan gaat de lane door met de volgende en eindigt de run
  met exitcode 1 en de fout per pin. Omdat de lane eerst zoekt (zie *Geen dubbel werk*), maakt
  een volgende run het af zonder dubbel item.

Per pin met `behind` of `error` in de upstream-meting:

- **Skill of bestand, `behind`.** Probeer per bestand een 3-way-merge (`git merge-file`):
  basis = het bestand op de pin, ours = het lokale bestand (bij een skill de hele `SKILL.md`,
  frontmatter inbegrepen), theirs = het bestand op upstream-HEAD. Is `upstream-path` een map,
  dan per bestand erin; een bestand dat upstream nieuw is, komt mee, een bestand dat upstream
  verdween, geeft een conflict. Elke `origin` gaat zo; bij `extended` is een conflict de
  gewone uitkomst.
  - Schoon → één rollende branch `pins/<sleutel>` met de merge, de opgehoogde
    `upstream-commit` (of `source_sha`), een `changelog.d/`-fragment, de bijgewerkte
    `SOURCES.toml` (een nieuw bestand krijgt `pin = "<sleutel>"`, een verdwenen bestand verliest
    zijn regel; *eigen keuze na spec-review, r1-f1*) en, als het pad onder `pack/` ligt, de
    kopieën die `init --upgrade` daarna schrijft (anders faalt de dogfood-stap in
    [`ci.yml`](../../../../.github/workflows/ci.yml)). Raakt de merge of `init --upgrade` een
    bestand onder `.github/workflows/`, dan wordt het een issue en geen PR: pushen daarheen
    vraagt het recht `workflows`, en dat krijgt de lane niet *(eigen keuze na spec-review,
    r1-f16)*. De PR is een **draft**, heeft geen
    `Closes`, staat niet op het board en heeft een body met `No issue: <reden>`, de
    upstream-commits met hun berichten, en de diff. Bestaat de PR al, dan wordt de branch
    bijgewerkt (force-push mag hier: de branch is van de lane, nooit van een mens) en de body
    vervangen.
  - Conflict → een issue met dezelfde toelichting en de conflicterende bestanden.
- **Plugin of tool, `behind`.** Eén issue per pin met de release-notes van elke release tussen
  pin en laatste release. Komt er een nieuwere release bij terwijl het issue openstaat, dan
  wordt de body bijgewerkt; er komt geen tweede issue.
- **`error`.** Eén issue per pin met de fout. Een rode run is niet genoeg: een wekelijkse
  workflow die rood is, ziet niemand.

**Upstream-tekst in een body** *(eigen keuze na spec-review, r1-f8 en r1-f9)*. Release-notes,
commitberichten en diffs komen van anderen en zijn groot: de notes van codex 0.157.0 tot
0.169.0 zijn samen ~198.000 tekens, en GitHub weigert een body boven 65.536. Daarom:

- alle upstream-tekst staat in een fenced codeblok (met een fence die langer is dan elke reeks
  backticks in de tekst), zodat GitHub er geen `@`-vermelding, `#N`-verwijzing of
  sluitwoord (`Fixes #2118`) in leest;
- per release of commit: tag of korte SHA, link, en een uittreksel van hoogstens een vast
  aantal tekens; de diff als `--stat` plus een compare-link naar upstream;
- de hele body blijft onder een vaste grens ruim onder 65.536 tekens; wat afvalt, wordt een
  regel "nog N releases, zie <link>".

De grenzen zelf zijn aan de bouwer.

**Geen dubbel werk** *(eigen keuze na spec-review, r1-f5, r1-f10 en r1-f14)*. Elk issue en elke
PR van de lane draagt een onzichtbare marker in de body met de sleutel, het soort item en de
doelversie (bij skill en bestand: de upstream-commit), bijvoorbeeld
`<!-- lighthouse-pin: codex behind 0.169.0 -->` (vorm aan de bouwer). Soorten: `pr` (schone
merge), `conflict` (issue), `behind` (issue, plugin of tool), `error` (issue). De lane zoekt
alle items van die sleutel, open en gesloten, issues en PR's.

*(Eigen keuze na spec-review, r2-f1 en r2-f2.)* Twee sporen per sleutel, elk met hoogstens één
open item: het **achterstandsspoor** (`pr`, `conflict`, `behind`) en het **foutspoor**
(`error`). Een fout raakt het achterstandsspoor niet: een tijdelijke storing sluit geen open
`behind`-issue of draft-PR.

Sluit de lane zelf een item, dan zet hij in de sluitcomment een tweede marker
(`<!-- lighthouse-pin-closed-by-lane -->`, vorm aan de bouwer). Een item dat de lane sloot,
telt bij het zoeken niet mee. Alleen een item dat een mens sloot, betekent "deze versie niet".

Achterstandsspoor, bij uitkomst `pr`, `conflict` of `behind`:

| Gevonden | Actie |
|---|---|
| open item, zelfde soort, zelfde doelversie | niets |
| open item, zelfde soort, oudere doelversie | bijwerken (body, en bij `pr` de branch) |
| open item, ander soort (`pr` ↔ `conflict`) | het oude sluiten met de lane-marker, het nieuwe aanmaken |
| geen open item; een door een mens gesloten item met dezelfde doelversie (elk soort) | niets |
| anders | aanmaken |

Bij uitkomst `current` blijft een open item in dit spoor staan: de mens sluit het met de PR die
de pin ophoogt. Bij uitkomst `error` verandert er in dit spoor niets.

Foutspoor:

| Uitkomst | Gevonden | Actie |
|---|---|---|
| `error` | open `error` | body bijwerken |
| `error` | geen open `error` | een nieuw `error`-issue, ook als er een gesloten is: een fout blijft luid |
| meetbaar (`current`, `behind`, `pr`, `conflict`) | open `error` | sluiten met de lane-marker |

**Nooit automatisch mergen.** Draft, geen `Closes`, geen board-item en geen milestone: de
dispatcher merget alleen de eigen PR van een run (`mergeReady`), en `milestones_only = true` laat
triage een item zonder milestone liggen. De lane zelf roept `MergePR` nooit aan.

### 6. Forge-uitbreiding

`internal/forge` krijgt wat de lane nodig heeft, met een contracttest zoals de bestaande
methodes: issue aanmaken, issue-body bijwerken, issue sluiten met een comment, PR aanmaken als
draft, PR-body bijwerken, PR sluiten met een comment, issues én PR's in elke staat (open en
gesloten) met hun body vinden op een marker (`OpenIssues` slaat PR's over en `OpenPRs` ziet
alleen open PR's; *eigen keuze na spec-review, r1-f5*), en alle releases van een andere repo
lezen, gepagineerd. Of releases op `Forge` horen of op een losse lezer per
upstream-repo, is aan de bouwer: `Forge` is nu één repository.

### 7. Een pin ophogen

Komt in `docs/` (plek aan de bouwer) en kort in de PR-body van de lane:

- **Skill of bestand.** Lees de draft-PR, pas aan waar nodig, zet hem op ready en merge hem
  zelf. Bij een conflict-issue: neem over wat je wilt, zet `upstream-commit` op de upstream-HEAD
  die je las, en sluit het issue met die PR.
- **Plugin of tool.** Lees de release-notes in het issue, probeer de versie lokaal, en hoog
  `version` in `lighthouse.toml` op in een PR die het issue sluit.

## Tests

Elk anti-doel heeft een test die faalt als het gedrag breekt:

- Elke `error`-oorzaak uit sectie 3 geeft `error` en exitcode ≠ 0, nooit `current`: ontbrekend
  veld, ongeldige SHA, onbekende `kind`, onbereikbare repo, pad weg, tag die niet parseert,
  tool niet op PATH, tool zonder versie, `installed_plugins.json` weg.
- Een upstream die alleen buiten het pad verschoof, geeft `current`.
- De scopekeuze voor Superpowers: worktree, dan hoofd-checkout, dan `user`, met een fixture
  waarin de scopes verschillende versies hebben.
- Twee runs achter elkaar op dezelfde achterstand: één issue, en bij een nieuwere release een
  bijgewerkte body in plaats van een tweede issue.
- Een gesloten issue voor doelversie X: een volgende run op X maakt niets; een run op X+1 maakt
  een nieuw issue. Hetzelfde voor een gesloten lane-PR.
- Een `error`-issue wordt gesloten zodra de pin weer meetbaar is; breekt de pin later opnieuw,
  dan komt er een nieuw `error`-issue.
- Elke rij van beide tabellen in *Geen dubbel werk* heeft een test, de overgangen `pr` ↔
  `conflict` inbegrepen: het oude item gaat dicht, het nieuwe komt. Ook `pr` X → `conflict` X →
  `pr` X geeft weer een PR, omdat de lane de eerste PR zelf sloot. En een `error` naast een open
  `behind`-issue laat dat issue open.
- Een schone merge geeft een draft-PR zonder `Closes`, met de opgehoogde pin, de bijgewerkte
  `SOURCES.toml` en, onder `pack/`, de dogfood-kopieën; een conflict geeft een issue en geen
  PR; een merge die `.github/workflows/` raakt, geeft een issue.
- Een body met upstream-tekst vol `@naam`, `#123` en `Fixes #123` levert geen vermelding,
  verwijzing of sluitwoord op (test op de gerenderde body: alles staat in het codeblok), en
  blijft onder de grens, ook met de notes van twintig releases.
- De releases worden gepagineerd gelezen: een fixture met de pin op pagina 2 geeft geen `error`.
- Een pin die geen bestaande release is, of hoger dan de laatste release, geeft `error`.
- Een lokale `0.169.0-alpha.11` tegen een pin `0.169.0` is `behind`, nooit `current`.
- Twee varianten van één skill met verschillende `upstream-commit` geven `error`; een
  geïnstalleerde kopie onder `.claude/skills/` telt nooit als pin.
- De lane roept `MergePR` nooit aan (fake forge).
- De meting voert niets uit de clone uit *(eigen keuze na spec-review, r1-f15: hooks reizen
  niet mee met een clone, dus een hook in de fixture-upstream bewijst niets)*: de clone heeft
  geen werkboom, en een hook via een globale `core.hooksPath` die een merkbestand schrijft,
  draait niet tijdens de meting.

Netwerk en git-upstreams als lokale fixtures (bare repo's in `t.TempDir()`, een fake forge);
geen test praat met GitHub.

## Aan de bouwer

- De vlagnamen van `pins check`, de naam van het lane-subcommando en de vorm van de marker.
- Releases op `Forge` of als losse lezer.
- Waar de documentatie van *Een pin ophogen* komt.
- Of de herkomst-opschoning (ontwerp 1) een eigen PR wordt vóór de lane. Aanbevolen: ja, het is
  los te reviewen en blokkeert niets.
- De grenzen van een body: tekens per release of commit, en voor de hele body.
- Aanbevolen indeling in drie plannen, elk los te leveren *(eigen keuze na spec-review,
  r1-f17)*: (1) herkomst, ontwerp 1; (2) meting en lokaal signaal, ontwerp 2 tot en met 4;
  (3) lane en forge-uitbreiding, ontwerp 5 en 6.

## Keuzes van de schrijver

- Plugin- en toolpins in `lighthouse.toml` en niet in `.lighthouse/pins.toml`, omdat `init`
  `.lighthouse/` beheert.
- Beginwaarden van de pins = wat nu geïnstalleerd is.
- `ahead` als vierde status, alleen in de lokale meting.
- De lane-PR is een draft; een `error` wordt een issue, niet alleen een rode run.
- Een verdwenen upstream-pad is `error`, geen `behind`.
- Eén lane-item per pin per doelversie; sluiten zonder ophogen door een mens = "deze versie
  niet".
- De lane sluit zijn eigen `error`-issue zodra de pin weer meetbaar is.
- Lane-items krijgen geen milestone, zodat triage en controller ze laten liggen.
- De App achter `[ci]` blijft de schrijver; een ontbrekend recht wordt een instelling van die
  App, geen nieuwe App.
- Bij een skill is "ours" de hele `SKILL.md`, frontmatter inbegrepen; een map gaat per bestand.
- Fase 1 zet de workflow in `.github/workflows/` van lighthouse zelf; fase 2 verplaatst hem naar
  `pack/`.
