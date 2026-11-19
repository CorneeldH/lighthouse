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
| `pack/python/hooks/ty-gate.sh` | ty-fouten; alleen-onoplosbare-imports | nee |
| `pack/shell/hooks/shell-lint-check.sh` | elke shellcheck-bevinding vanaf `warning` (`style` in `shell.strict_dirs`) | nee |
| `pack/go/hooks/go-lint-gate.sh` | elke bevinding van `golangci-lint run ./...` met de config van de repo | nee |
| `pack/go/hooks/gofmt-check.sh` | een bestand dat niet parset | nee |
| core (`pack/core/hooks/blocking`, 7 hooks, Go in `internal/hooks`, `internal/commitgate`, `internal/codelang`) | vrije tekst in `hook.Result{Code: 2, Stderr: …}` | nee |

Het go-pack levert geen `.golangci.yml` mee: bij een adopter bepaalt diens eigen config wat
`go-lint-gate` vangt. De 8 ruff-codes staan ook in `pack/python/scripts/quality/baseline.py:18-25`
en in de python-standard-skill. `tests/pack/python/test_ruff_format.py` dekt er 2 van.

## Doelen, niet-doelen, anti-doelen

Doelen:

- Elke hook uit de tabel hierboven heeft een **gedeclareerde, machineleesbare lijst blokkeerredenen**
  in de hook zelf.
- Per reden een canary: een minimale overtreding plus een test die exit 2 én de ID van die reden in
  stderr eist.
- Een reden zonder canary, of een canary zonder reden, laat de test falen.
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
- Path-exclusions en gedeeltelijke disables in een linterconfig (zie §2): een vervolgissue.

Anti-doelen (mislukt, ook als het technisch werkt):

- Een hook die bij een adopter minder blokkeert dan vandaag. De milestone verbiedt lint-config
  versoepelen.
- Een lijst die met de hand in de test is overgetypt.
- Een tweede kopie van een test, in pytest en in Go.

## Aanpakken

**Taalpacks zonder eigen regels (Go, shell).** Drie opties:

- *Eén canary per drempel*: bewijst dat de hook de linter nog aanroept, vangt geen regel die uit een
  config valt. Afgewezen: dekt het probleem uit het issue niet.
- *Vaste lijst als plafond* (zoals ruff: de hook blokkeert alleen op de lijst): afgewezen, want
  voor een adopter met een strengere config is dat een versoepeling.
- **Vaste lijst als vloer** *(gekozen door de gebruiker)*: de hook blokkeert op alles wat hij nu
  vangt, en garandeert daarbovenop dat de regels op de lijst altijd aanstaan. Elke regel op de vloer
  krijgt een canary. Een config die een vloerregel uitzet, wordt geen stille gat meer.

Ruff blijft een plafond: dat is daar een bewuste keuze (alleen repo-breed-nul-regels per edit, de
rest via de ratel).

**Core-hooks: hoe de test ziet welke reden afging.** Twee opties:

- **ID in stderr** *(gekozen door de gebruiker)*: elke blokmelding eindigt op een stabiele ID, de
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
| `go-lint-gate.sh` | `errcheck,govet,ineffassign,staticcheck,unused` (de golangci-set `standard`) *(eigen keuze)* | vloer |
| `shell-lint-check.sh` | een korte lijst SC-codes met echt bugrisico en severity `warning` of `error`, zoals `SC2164`; de precieze lijst kiest het plan *(eigen keuze)* | vloer |
| `gofmt-check.sh` | één ID, bijvoorbeeld `parse-error` | enige reden |
| `ty-gate.sh` | twee IDs: type-fout en alleen-onoplosbare-imports | redenen |

Voor `gofmt-check` en `ty-gate` is "regel" een blokkeerreden; ze krijgen dezelfde vorm, zodat de
test één patroon heeft.

De shell-vloer bevat alleen codes met severity `warning` of `error` *(eigen keuze)*. Buiten
`shell.strict_dirs` blokkeert de hook bewust niet op `info` en `style`, omdat die op schuld elke
edit zouden blokkeren; een `info`-code als `SC2086` op de vloer zou dat ongedaan maken.

### 2. De vloer afdwingen

- **Go**: de hook zet de vloer aan bovenop de config van de repo (`golangci-lint run --enable …`).
  Wat de config verder aanzet, blijft blokkeren.
- **Shell**: shellcheck zet de vloercodes standaard aan. Een `.shellcheckrc` kan ze uitzetten
  (`disable=`); dat moet niet stil kunnen. De hook voorkomt het of blokkeert luid. Een
  `# shellcheck disable=SCxxxx <reden>` op één regel blijft toegestaan, zoals de hookmelding nu al
  zegt.

**Wat de vloer tegenhoudt** *(gekozen door de gebruiker)*: alleen een vloerregel die in de config
in zijn geheel uitstaat — een `disable` van een linter in `.golangci.yml`, `disable=<code>` of
`disable=all` in `.shellcheckrc`. Eén run van de hook, geen aparte run die de config negeert.

Buiten de vloer, en dus toegestaan:

- inline `//nolint` en `# shellcheck disable=…`: zichtbaar en bewust;
- path-exclusions (`linters.exclusions` in golangci) en `issues.new-from-rev`;
- een deel van een linter uitzetten (`settings.govet.disable`, staticcheck `checks: ["-SA…"]`).

Het issue noemt "een pad wordt uitgesloten" als motief. Dat wordt een vervolgissue, niet deze
poort: een run die de config negeert, verliest `new-from-rev` en build tags en blokkeert een
adopter met schuld op bestanden die hij niet aanraakte.

Hoe precies (flags, conflict tussen config en CLI, versie van golangci) is aan de bouwer, met als
eis: bij een adopter blokkeert de hook nooit minder dan vandaag.

### 3. Core-hooks: redenen met een ID

Elke blokkeerreden van de 7 core-hooks (geen `COULD NOT CHECK`) krijgt in Go een gedeclareerde ID,
per hook in één lijst die de test kan opvragen. Een blokkerend resultaat kan alleen met zo'n reden
worden gemaakt, zodat een nieuwe reden niet buiten de lijst om ontstaat. De melding eindigt op de
ID, in de vorm `[<hook>/<reden>]` *(eigen keuze voor de vorm)*. De bestaande tekst van de melding
blijft verder gelijk.

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
- Per ID eist de test exit 2 en de ID in stderr. Voor linterregels is de ID de regelcode die de
  linter zelf print (`E722`, `SC2164`, `errcheck`).
- **Vijandige config** *(eigen keuze)*: de default config zet alle vloerregels al aan, dus een run
  daarop bewijst de vloer niet. Elke vloerregel van Go en shell krijgt een tweede run met een config
  die precies die regel uitzet; de test genereert die config, er komt geen fixture bij. Ook daar
  eist de test exit 2 en de ID.
- **`bash-write-check`** *(eigen keuze)*: die hook geeft alleen bevindingen van andere poorten door
  (ruff, shellcheck, md-link-check, code-language-check). Hij krijgt geen eigen redenen, maar één
  canary per poort die hij doorgeeft, via een Bash-write; die eist exit 2 en de ID van die poort.
- Een test die zijn linter mist, faalt, met uitzondering van wat de bestaande pack-tests nu
  overslaan; de keuze tussen falen en overslaan volgt het bestaande patroon per pack.

### 5. Dubbele kopieën van de ruff-lijst

`baseline.py` en de skill herhalen de 8 codes. Dit issue maakt de hook de bron voor de canaries; de
andere kopieën laat het staan, tenzij het plan een goedkope afleiding vindt. Een test die eist dat
`baseline.py` dezelfde lijst heeft, mag *(eigen keuze)*.

## Randvoorwaarden

- Dogfood: een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie; CI eist `No changes.`
- Gereedschap blijft gepind (`.ruff-version`, de shellcheck-pin, `.golangci-version`).
- Een fragment in `changelog.d/`: de meldingen van de core-hooks veranderen zichtbaar, en de go- en
  shell-hooks worden strenger bij een config die een vloerregel uitzette.
- Go-code volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.

## Voor de bouwer

- De precieze SC-codes van de shell-vloer.
- Hoe de vloer technisch wordt afgedwongen (§2).
- De Go-vorm van de redenlijst en waar de canary-tests wonen.
- Welke bestaande tests opgaan in de canary-tests (AC 5); de rest blijft.
- Het bewijs voor AC 4: één regel uit een lijst gehaald, de test rood, de output in de PR.
