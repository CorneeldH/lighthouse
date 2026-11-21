# Ontwerp: een canary per blokkerende regel (#381)

Issue: [#381](https://github.com/CorneeldH/lighthouse/issues/381). Milestone *Kwaliteitspoorten per
taalpack*, K1 in [de spec van de milestone](2026-11-16-zelflerende-lighthouse-design.md).

## Aanleiding en doel

Een poort die stil stopt met kijken, ziet er van buiten uit als schone code: allebei exit 0. Dat
gebeurt als een regel uit een config valt, een pad wordt uitgesloten of een linterversie een regel
hernoemt. Een **canary** is een minimale, bewuste overtreding waarvan een test eist dat de poort
blokkeert.

Doel: elke blokkerende regel van elke hook die lighthouse installeert, heeft een canary. De lijst
van canaries komt uit de hook zelf. Een regel erbij zonder canary laat de test falen; een regel
die de hook stil niet meer vangt ook.

## Hoe het er nu voor staat

| Hook | Blokkeert op | Eigen regellijst |
|---|---|---|
| `pack/python/hooks/ruff-format-check.sh` | `ruff check --select "$BLOCKING_RULES"` | ja, 8 codes (`:98`) |
| `pack/python/hooks/ty-gate.sh` | ty-fouten; alleen-onoplosbare-imports (`:186-203`) | nee |
| `pack/shell/hooks/shell-lint-check.sh` | elke shellcheck-bevinding vanaf `warning`, vanaf `style` in `shell.strict_dirs` (`:97-110`) | nee |
| `pack/go/hooks/go-lint-gate.sh` | elke bevinding van `golangci-lint run ./...` met de config van de repo, en een build die faalt (`:23-24`, `:144`) | nee |
| `pack/go/hooks/gofmt-check.sh` | een bestand dat niet parset | nee |
| core (`pack/core/hooks/blocking`, 7 hooks, Go in `internal/hooks`, `internal/commitgate`, `internal/codelang`) | vrije tekst in `hook.Result{Code: 2, Stderr: …}` | nee |

Het go-pack levert geen `.golangci.yml` mee: bij een adopter bepaalt diens eigen config wat
`go-lint-gate` vangt. De 8 ruff-codes staan ook in `pack/python/scripts/quality/baseline.py:18-25`
en in de python-standard-skill. `tests/pack/python/test_ruff_format.py` dekt er 2 van.
`bash-write-check` stuurt een Bash-write per extensie door naar de poorten in
`internal/bashwrites/gates.go:21-27` (`Gates`).

## Doelen, niet-doelen, anti-doelen

Doelen:

- Elke hook uit de tabel hierboven heeft een **gedeclareerde, machineleesbare lijst blokkeerredenen**
  in de hook zelf.
- Per reden een canary: een minimale overtreding plus een test die exit 2 én de ID van die reden in
  stderr eist, op een vaste plek (§4).
- Een reden zonder canary, of een canary zonder reden, laat de test falen.
- Een config die een vloerregel in zijn geheel uitzet, is geen stil gat meer (§2). Andere manieren
  om een regel stil te leggen, zoals een exclusion, staan in het vervolgissue.
- Eenmalig in de PR: een regel uit een lijst gehaald, de test rood, de output erbij.
- Bestaande losse tests die hetzelfde bewijzen, gaan op in de canary-test.

Niet-doelen:

- `COULD NOT CHECK`-paden. Die blokkeren al luid; de canary jaagt op een poort die stil exit 0 geeft.
  Hun bestaande tests blijven.
- Regels die een adopter zelf extra aanzet. Die config is van de adopter.
- Node- en Expo-packs: die hebben geen blokkerende hook.
- `usage-gate` *(eigen keuze)*: een gebruiksalarm, geen codepoort, en hij blokkeert via JSON in
  plaats van exit 2. "Blokkerend" is hier: exit 2, en voor core de hooks in
  `pack/core/hooks/blocking`.
- Path-exclusions, exclusion-regels zonder pad en gedeeltelijke disables in een linterconfig (zie
  §2): een vervolgissue.

Anti-doelen (mislukt, ook als het technisch werkt):

- Een hook die bij een adopter minder blokkeert dan vandaag. De milestone verbiedt lint-config
  versoepelen.
- Een lijst die met de hand in de test is overgetypt.
- Een tweede kopie van een test, in pytest en in Go.
- Een canary die groen blijft terwijl de reden die hij bewaakt niet afging (§4, *Matchen*).

## Aanpakken

**Taalpacks zonder eigen regels (Go, shell).** Drie opties:

- *Eén canary per drempel*: bewijst dat de hook de linter nog aanroept, vangt geen regel die uit een
  config valt. Afgewezen: dekt het probleem uit het issue niet.
- *Vaste lijst als plafond* (zoals ruff: de hook blokkeert alleen op de lijst): afgewezen, want
  voor een adopter met een strengere config is dat een versoepeling.
- **Vaste lijst als vloer** *(gekozen door de gebruiker)*: de hook blokkeert op alles wat hij nu
  vangt, en garandeert daarbovenop dat de regels op de lijst aanstaan. Elke regel op de vloer
  krijgt een canary. Een config die een vloerregel uitzet, wordt geen stil gat meer.

Ruff blijft een plafond: dat is daar een bewuste keuze (alleen repo-breed-nul-regels per edit, de
rest via de ratel).

**Core-hooks: hoe de test ziet welke reden afging.** Twee opties:

- **ID in stderr** *(gekozen door de gebruiker)*: elke blokmelding draagt een stabiele ID, de
  test leest die uit het subprocess, net als de regelcode bij de packs. Ook een geblokkeerde agent
  heeft er iets aan.
- *ID alleen in `hook.Result`*: meldingen blijven gelijk, maar het bewijs loopt dan niet via het
  proces dat de harness echt draait. Afgewezen.

## Ontwerp

### 1. De lijst in de hook

Elke shell-hook declareert één regel `BLOCKING_RULES="<id>,<id>,…"`, zoals ruff nu al. De test leest
die regel uit het hookbestand; er is geen tweede bron.

| Hook | `BLOCKING_RULES` | Rol |
|---|---|---|
| `ruff-format-check.sh` | de huidige 8 codes | plafond, ongewijzigd |
| `go-lint-gate.sh` | `errcheck,govet,ineffassign,staticcheck,unused` (de golangci-set `standard`) *(eigen keuze)* als vloer, plus `typecheck` als reden *(eigen keuze na spec-review, r1-f1, r2-f1)* | vloer + reden |
| `shell-lint-check.sh` | een korte lijst SC-codes met echt bugrisico en severity `warning` of `error`, zoals `SC2164`; de precieze lijst kiest het plan *(eigen keuze)* | vloer |
| `gofmt-check.sh` | één ID, `parse-error` | enige reden |
| `ty-gate.sh` | twee IDs: type-fout en alleen-onoplosbare-imports | redenen |

Voor `gofmt-check` en `ty-gate` is "regel" een blokkeerreden; ze krijgen dezelfde vorm, zodat de
test één patroon heeft. Elke exit-2-tak van die twee (buiten `COULD NOT CHECK`) print zijn eigen ID
als `[<hook>/<id>]` *(eigen keuze na spec-review, r1-f12)*.

De shell-vloer bevat alleen codes met severity `warning` of `error` *(eigen keuze)*. Buiten
`shell.strict_dirs` blokkeert de hook bewust niet op `info` en `style`, omdat die op schuld elke
edit zouden blokkeren; een `info`-code als `SC2086` op de vloer zou dat ongedaan maken.

**`typecheck` is geen vloerlinter.** In golangci-lint 2.15.0 is `typecheck` geen linter: hij is niet
aan of uit te zetten en `golangci-lint linters` noemt hem niet. Hij staat in `BLOCKING_RULES` als
reden met alleen de gewone canary (een fixture die niet compileert, exit 2 en `(typecheck)`). De
effectieve-setcheck van §2 en de vijandige-configrun van §4 slaan hem over. De test leest dat
onderscheid uit de hook, niet uit een eigen lijst.

**De strict-drempel.** In `shell.strict_dirs` blokkeert de hook ook op `style` en `info`. Dat is een
drempel, geen regellijst. Hij krijgt één eigen ID, `strict-style`, en één canary: een
`style`-overtreding (zoals `SC2001`) in een strict dir moet blokkeren, dezelfde buiten een strict
dir niet *(eigen keuze na spec-review, r1-f2)*.

### 2. De vloer afdwingen

**Wat de vloer tegenhoudt** *(gekozen door de gebruiker)*: een vloerregel die in de config in zijn
geheel uitstaat. Inline `//nolint` en `# shellcheck disable=…` met een reden blijven toegestaan:
zichtbaar en bewust. Path-exclusions (`linters.exclusions`), exclusion-regels zonder pad,
`issues.new-from-rev` en een deel van een linter uitzetten (`settings.govet.disable`, staticcheck
`checks: ["-SA…"]`) vallen buiten de vloer en gaan naar een vervolgissue. Een run die de config
negeert, verliest `new-from-rev` en build tags en blokkeert een adopter met schuld op bestanden die
hij niet aanraakte.

**Go.** `golangci-lint run --enable <linter>` zet een linter die de config uitzet **niet** terug
aan: de `disable` uit de config wint (gemeten met golangci-lint 2.15.0). `--enable-only` laat de
linters van de config vallen en is dus een versoepeling. De hook bepaalt daarom vóór de lintrun de
**effectieve** set linters (bijvoorbeeld via `golangci-lint linters`, met de config van de repo) en
blokkeert luid met de ID van elke vloerlinter die daar niet in staat *(eigen keuze na spec-review,
r1-f5)*. De lintrun zelf blijft zoals hij is. De vloercheck vereist golangci-lint v2; op v1 meldt de
hook dat de vloer niet te controleren is, niet stil *(eigen keuze na spec-review, r1-f10)*.

**Shell.** Een vloercode kan via meer kanalen uitstaan dan `.shellcheckrc` in de root: een rc-bestand
dat shellcheck naar boven vanaf het script vindt, een rc in home of XDG, `SHELLCHECK_OPTS`, en
code-ranges in `disable=`. De hook beslist op het **effectieve** resultaat in dezelfde omgeving als
de echte run, niet op de syntax van één bestand *(eigen keuze na spec-review, r1-f8)*. `--norc` is
verboden: dat laat ook de `enable=` van de adopter vallen, en dan blokkeert de hook minder.

Hoe precies is aan de bouwer, met als eis: bij een adopter blokkeert de hook nooit minder dan
vandaag, en een vloerregel die in zijn geheel uitstaat, blokkeert luid met zijn ID.

### 3. Core-hooks: redenen met een ID

Elke blokkeerreden van de 7 core-hooks (geen `COULD NOT CHECK`) krijgt in Go een gedeclareerde ID,
per hook in één lijst die de test kan opvragen. De melding draagt de ID in de vorm `[<hook>/<reden>]`
*(eigen keuze voor de vorm)*. De bestaande tekst van de melding blijft verder gelijk. Een melding met
meer bevindingen (zoals `md-link-check`, `internal/hooks/mdlinkcheck.go:106-118`) draagt de ID per
bevindingsregel *(eigen keuze na spec-review, r1-f11)*.

**Afdwingen** *(eigen keuze na spec-review, r1-f7)*: in die 7 hooks maakt een blokkerend oordeel zijn
resultaat alleen via een helper die een gedeclareerde reden neemt, en `COULD NOT CHECK` via een eigen,
aparte helper. `hook.Result.Code` blijft geëxporteerd; de andere hooks veranderen niet. Een
statische test over de pakketten van de 7 hooks faalt op een `Code: 2` buiten die twee helpers.

### 4. De canary-tests

- **Python** (`ruff-format-check`, `ty-gate`): pytest in `tests/pack/python/`, dat Python blijft
  volgens [de spec Repo zonder Python](2026-11-16-repo-zonder-python-design.md).
  `test_ruff_format.py` gaat erin op.
- **Go, shell en core**: Go-tests, want tests buiten `tests/pack/python/` gaan naar Go. De pack-hooks
  draaien als subprocess, met dezelfde payload als de harness. De core-hooks via
  `lighthouse hook <name>` of `run.sh`, ook als subprocess.
- **Overtredingen**: één bestand per ID onder `testdata/<hook>/<id>.<ext>` *(eigen keuze)*. De test
  eist een bijectie tussen de lijst en de bestanden: een ID zonder bestand en een bestand zonder ID
  falen allebei.
- **Matchen** *(eigen keuze na spec-review, r1-f6)*: de test kopieert een overtreding naar een
  neutrale naam (zoals `canary.<ext>`) in een tijdelijke map zonder de ID in het pad, en eist exit 2
  plus de ID op de plek waar de tool of hook hem print: ruff `: E722 `, shellcheck `SC2164 (`,
  golangci `(errcheck)`, en `[<hook>/<id>]` voor de rest. Een losse substring telt niet.
- **Vijandige config** *(eigen keuze)*: de default config zet alle vloerregels al aan, dus een run
  daarop bewijst de vloer niet. Elke vloerregel van Go en shell krijgt een tweede run met een config
  die precies die regel uitzet; de test genereert die config, er komt geen fixture bij. Ook daar
  eist de test exit 2 en de ID.
- **`bash-write-check`** *(eigen keuze, bijgesteld na spec-review, r1-f3)*: die hook geeft alleen
  bevindingen van andere poorten door. Hij krijgt geen eigen redenen, maar één canary per poort in
  `bashwrites.Gates` (nu ook `gofmt-check`), via een Bash-write; die eist exit 2 en de ID van die
  poort. De set komt uit `Gates`, niet uit een lijst in de test.

**Waar ze draaien** *(eigen keuze na spec-review, r1-f4)*: de Go-canaries die echte linters nodig
hebben, staan achter een build tag (`canary`). Eén CI-job draait ze: die installeert elke tool die
de canaries nodig hebben, ook die van de poorten uit `bashwrites.Gates`: golangci-lint op
`.golangci-version`, shellcheck via de gepinde lib, uv met ruff op `.ruff-version`, en `lighthouse`
gebouwd op `PATH` *(eigen keuze na spec-review, r2-f2)*. Een ontbrekende tool laat die job falen,
nooit overslaan. Lokaal draait `go test ./...` zonder tag
zoals nu.

**Bestaande pytest** *(eigen keuze na spec-review, r1-f14)*: deze PR verwijdert de pytest-tests die in
een Go-canary opgaan (onder meer in `tests/pack/go/test_gofmt.py`, `tests/pack/shell/test_shellcheck.py`,
`tests/pack/test_md_link_check.py`, `tests/pack/test_bash_write_check.py`). De PR noemt ze, zodat de
triage van *Repo zonder Python* ze niet nog eens port.

### 5. Dubbele kopieën van de ruff-lijst

`baseline.py` en de skill herhalen de 8 codes. Dit issue maakt de hook de bron voor de canaries; de
andere kopieën laat het staan, tenzij het plan een goedkope afleiding vindt. Een test die eist dat
`baseline.py` dezelfde lijst heeft, mag *(eigen keuze)*.

## Randvoorwaarden

- Dogfood: een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie; CI eist `No changes.`
- Gereedschap blijft gepind (`.ruff-version`, de shellcheck-pin, `.golangci-version`).
- Een fragment in `changelog.d/`: de meldingen van de core-hooks veranderen zichtbaar, en de go- en
  shell-hooks blokkeren een config die een vloerregel uitzette. Het fragment en de blokmelding noemen
  `issues.new-from-rev` als route voor schuld *(eigen keuze na spec-review, r1-f13)*.
- Go-code volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.

## Voor de bouwer

- De precieze SC-codes van de shell-vloer.
- Hoe de vloer technisch wordt afgedwongen (§2), binnen de eisen daar.
- De Go-vorm van de redenlijst en de twee helpers (§3), en waar de canary-tests wonen.
- Welke CI-job de `canary`-tag draait: een bestaande uitbreiden of een nieuwe.
- Welke bestaande tests opgaan in de canary-tests (AC 5); de rest blijft.
- Het bewijs voor AC 4: één regel uit een lijst gehaald, de test rood, de output in de PR.
