# Ontwerp: hook voor de taal van code

Issue: #26 · Datum: 2026-10-24

## Aanleiding en doel

Lighthouse schrijft code in het Engels: namen van modules, klassen, functies en variabelen, en ook
commentaar en docstrings. Dat hangt nu af van wie of welk model de code schrijft, en dat gaat mis:
in een diff duiken Nederlandse namen en Nederlands commentaar op die er niet horen. In `pack/` staat
bovendien nog veel Nederlands uit de verhuizing (plan (b), ontwerpkeuze 3, onder *Verbeterpunten*),
bijvoorbeeld de docstrings in `pack/core/lib/bash_writes.py`.

Deze spec levert een hook die dat bewaakt bij elke write. De hook blokkeert een codebestand dat na
de write nog niet-Engelse namen, commentaar of docstrings bevat, met bestand en regel.

**Eerst de hook, daarna de vertaalslag.** Dit issue levert alleen het mechanisme. In lighthouse
zelf staat de hook na de merge nog uit; hij gaat aan nadat het bestaande Nederlands vertaald is.
Dat aanzetten hoort bij de vertaalslag, niet bij dit issue.

## Doelen

- Een hook die na elke write (Edit, Write, en Bash-writes via `bash-write-gates`) een codebestand
  controleert op niet-Engelse namen, commentaar en docstrings, en elke vondst meldt met bestand en
  regel.
- Een eigen configsleutel voor de taal van code, los van `[project] language` (de taal van
  artefacten). Een repo kan Nederlandse issues hebben en Engelse code.
- Uitzonderingen voor tekst die bewust een andere taal heeft.
- Zichtbaar bij twijfel: kan de hook niet beslissen of niet meten, dan zegt hij dat (besluit 18 van
  de [M0-spec](2026-10-17-m0-pakket-design.md)).

## Non-goals

- **Het bestaande Nederlands vertalen.** Dat is de vertaalslag, een apart stuk werk na deze hook.
- **Markdown, docs, TOML en andere niet-code.** Die vallen onder de artefacttaal of zijn data.
- **Stringliterals** (behalve docstrings). Een string is data of tekst voor een gebruiker, geen naam
  of commentaar. Testdata als `"hallo"`, koppen uit `headings.toml` die in code als string staan, en
  de oude tokens in `protocol.py` (`_LEGACY_KEYS`, `LEGACY_MARKERS`) vallen daardoor al buiten de
  check.
- **Een model of externe detector.** De hook draait bij elke write: een modelaanroep is te traag,
  kost geld en werkt niet offline. Een betere detector (bijvoorbeeld
  [Jev](https://typesafe.ai/blog/introducing-system-one-models-and-jev) of een lokaal alternatief)
  is een vervolg-issue: #37.
- **Een codetaal anders dan Engels.** De hook kent alleen het doel `en`.
- **Lighthouse zelf aanzetten.** Zie *Aanleiding en doel*.

## Anti-goals

- **Een hook die zwijgt bij twijfel.** Een bestand dat niet te parsen is, of een config die niet te
  lezen is, mag nooit lezen als "schoon".
- **Valse alarmen op gewoon Engels.** Een Engelse regel die als Nederlands geblokkeerd wordt, maakt
  de hook onbruikbaar. De woordenlijst bevat daarom geen woord dat ook gangbaar Engels is.
- **De hook staat aan in een repo die er niet om vroeg.** Een repo met Nederlandse code die
  `lighthouse init` draait, mag niet plots bij elke edit geblokkeerd worden.

## Besluiten

| # | Besluit | Reden |
|---|---|---|
| 1 | De hook **blokkeert** (exit 2), hij meldt niet alleen | Een melding laat het bestand half-Nederlands achter. Het doel is dat elk bestand dat je aanraakt Engels wordt |
| 2 | De hook controleert **het hele bestand**, niet alleen de nieuwe regels | Wat je aanraakt, zet je in het Engels. De gates krijgen bovendien alleen een bestandspad, geen diff |
| 3 | De codetaal staat in `[project] code_language`. Alleen `"en"` is geldig; een andere waarde is een `ConfigError`. *Mijn keuze* (naam en plek) | Los van `[project] language`, zoals het issue vraagt. Andere doeltalen vragen een andere detector: non-goal |
| 4 | Zonder `code_language` doet de hook niets en zegt hij niets (exit 0) | Uit is een instelling, geen twijfel. Een melding bij elke write in elke repo zonder de sleutel is ruis. Besluit 18 gaat over "kon niet controleren", niet over "staat uit" |
| 5 | `lighthouse init` schrijft `code_language` niet, ook niet bij een nieuwe repo; `--upgrade` laat de sleutel staan of weg. *Mijn keuze* | Anti-goal: de hook staat alleen aan in een repo die erom vroeg. Lighthouse zelf zet hem aan na de vertaalslag |
| 6 | Herkenning met een Nederlandse woordenlijst plus heuristiek, in Python met alleen de standaardbibliotheek | Zoals `bash_writes.py` en `md_links.py`. Geen model: zie *Non-goals* |
| 7 | Talen: Python (`.py`), shell (`.sh`, `.bash`) en Go (`.go`), herkend aan de extensie. *Mijn keuze* (alleen extensie, geen shebang) | Dezelfde regel als `GATES` in `bash_writes.py`. Go heeft geen eigen pakket, maar lighthouse zelf is Go |
| 8 | Uitzonderingen: (a) stringliterals vallen buiten de check (*Non-goals*); (b) padpatronen in `[project] code_language_exempt` (lijst van globs) slaan een bestand over; (c) een markering aan het eind van een regel, `lighthouse: allow-language`, slaat die regel over. *Mijn keuze* (vorm en namen) | Dekt de drie gevallen uit het issue: koppen en oude tokens staan als string in code (a), testdata die geen string is of een hele fixture (b), bewust Nederlands commentaar (c) |
| 9 | Geen Go-binary of andere parser als afhankelijkheid; Go en shell worden met een eenvoudige tokenizer in Python gelezen | Een repo met lighthouse heeft Python 3, maar niet per se Go of een shellparser |
| 10 | De hook leest `code_language` uit de `lighthouse.toml` van de repo waarin het **geschreven bestand** staat (de git-root van dat bestand), niet uit de checkout waar de hook geïnstalleerd is. Ligt het bestand buiten een git-repo, of heeft die repo geen `lighthouse.toml`, dan is de hook uit (besluit 4). Alleen een `lighthouse.toml` die er is maar een `ConfigError` geeft, is `COULD NOT CHECK`. *Mijn keuze* | Anti-goal: een edit in een andere repo of een scratchmap wordt niet geblokkeerd omdat deze repo de hook aanzette. En de branch die `code_language` aanzet (de vertaalslag), test de hook al op zijn eigen branch |
| 11 | Bestanden die lighthouse zelf installeert (elk pad in `.lighthouse/manifest.toml`: `.lighthouse/lib/`, `.lighthouse/scripts/`, `.claude/hooks/lighthouse/`, ...) slaat de hook over. *Mijn keuze* | Die code is de verantwoordelijkheid van lighthouse, niet van de repo die hem installeert. Anders blokkeert een `lighthouse init --upgrade` via Bash in een repo met de hook aan op elk geïnstalleerd bestand zolang lighthouse nog Nederlands is. In lighthouse zelf staat dezelfde code ook onder `pack/`, en daar controleert de hook hem wel |

## Opbouw

### Wat de hook leest

Per taal haalt de hook drie soorten tekst uit het bestand, elk met regelnummer:

- **Commentaar.** Python: `COMMENT`-tokens uit `tokenize`. Shell: `#` buiten een string en niet de
  shebang. Go: `//` en `/* … */` buiten een string.
- **Docstrings.** Python: de docstring van module, klasse en functie (via `ast`, met regelnummer).
  Go en shell hebben er geen; Go-doccommentaar is gewoon commentaar.
- **Namen op de plek waar ze ontstaan.** De bestandsnaam (module), en in het bestand: Python `def`,
  `class`, argumenten, toewijzingsdoelen, `for`-doelen, `import … as`. Shell: functienamen en
  toegewezen variabelen (`naam=`, `local naam`, `export naam`). Go: `package`, `func`, `type`, `var`,
  `const`, `:=`, parameters en struct-velden.

Een naam die alleen gebruikt wordt (een aanroep van een functie uit een andere module) telt niet: die
wordt gemeld in het bestand waar hij ontstaat. Zo krijgt één Nederlandse naam één melding.

### Hoe de hook herkent

- **Commentaar en docstrings.** De eenheid is een **blok**: een docstring, of een reeks
  commentaarregels direct onder elkaar (een `/* … */` is één blok). De tekst wordt in woorden
  gesplitst (kleine letters, letters met accenten erbij). Een blok is Nederlands als het minstens één
  woord bevat uit de lijst met Nederlandse **functiewoorden** (lidwoorden, voornaamwoorden,
  voorzetsels, voegwoorden en hulpwerkwoorden: "het", "een", "niet", "geen", "wordt", "zijn", "voor",
  "naar", "maar", "ook", ...) of uit de lijst met **inhoudswoorden** hieronder. *Mijn keuze*: een
  losse regel als `# --- post (taak 7): bron 1, claims, dispatch, uitvoer ---` heeft geen
  functiewoord, en een regel als `` # `%gd` van `git reflog ...` `` alleen woorden die ook Engels
  zijn; in een blok met een Nederlandse buurregel, of via een inhoudswoord, worden ze toch gevangen.
  De melding noemt de eerste regel van het blok.
- **Namen.** Een naam wordt gesplitst op `_` en op de overgang van klein naar hoofdletter
  (`leesBestand` → `lees`, `bestand`). De naam is Nederlands als minstens één deel in de lijst met
  functiewoorden of in een lijst met Nederlandse **inhoudswoorden** staat (woorden die in code vaak
  voorkomen: "bestand", "regel", "naam", "lees", "schrijf", "gevonden", "besluit", "kop", ...).
- **Beide lijsten bevatten geen woord dat ook gangbaar Engels is.** Voorbeelden die er dus níet in
  mogen: `die`, `met`, `door`, `over`, `van`, `map`, `pad`, `is`, `in`, `op`, `been`, `had`, `was`,
  `hem`. Dat is de bescherming tegen valse alarmen (anti-goal).
- De lijsten staan als data in de lib van core, één woord per regel, zodat ze zonder codewijziging
  te verfijnen zijn.

### Wat de hook meldt

Bij een vondst exit 2, met op stderr:

```text
code-language: <bestand> is not English
<bestand>:<regel>: comment: <eerste 60 tekens van de regel>
<bestand>:<regel>: name: <naam>
...
```

Maximaal twintig regels met vondsten, daarna `… and <n> more`. Daarna één regel uitleg: dat het
hele bestand Engels moet zijn, en hoe je een regel bewust uitzondert (besluit 8c).

Kan de hook niet controleren, dan exit 2 met `code-language: COULD NOT CHECK — <reden>` op de eerste
regel (besluit 18; `bash_writes.classify` herkent dat token). Dat geldt voor: een payload die geen
JSON is, geen `python3`, een `lighthouse.toml` die een `ConfigError` geeft, een Python-bestand dat
`tokenize` of `ast` niet kan lezen, een bestand dat niet als UTF-8 te lezen is.

Geen vondst, geen `code_language`, een andere extensie, een bestand onder `code_language_exempt` of
in het manifest (besluit 11), een bestand buiten een git-repo of in een repo zonder `lighthouse.toml`
(besluit 10), of een bestand dat niet bestaat: exit 0 zonder uitvoer.

### Waar het landt

- **De logica** in een nieuwe lib-module in `pack/core/lib/` (geïnstalleerd onder
  `.lighthouse/lib/lighthouse_lib/`), met de woordenlijsten ernaast.
- **De hook** als dunne wrapper `pack/core/hooks/code-language.sh`, zoals `worktree-linkcheck.sh`
  dat `md_links.py` aanroept. Geregistreerd in `pack/core/hooks/hooks.json` onder `PostToolUse`,
  matcher `Edit|Write`, naast `worktree-linkcheck.sh`.
- **Bash-writes.** `bash_writes.py` kent nu één gate per extensie (`GATES`). De codetaal-gate moet
  daar naast `ruff-format.sh` en `shellcheck.sh` draaien, en ook voor `.go`, dat nu geen gate heeft.
  Hoe (`GATES` naar een lijst per extensie, of een tweede tabel), kiest de bouwer. Vast staat: de
  codetaal-gate draait na een herschrijvende gate (`_SIGN_AFTER_GATE`), zodat hij de versie ná
  `ruff format` leest, en besluit 19 blijft gelden.
- **Config.** `project.code_language` (`str`) en `project.code_language_exempt` (`list[str]`) in
  `SCHEMA` van `pack/core/lib/config.py`, zonder default. De waardecheck van besluit 3 staat naast
  die van `project.language`.

## Bewijs

- **Geen vals alarm op Engels.** Een test draait de herkenning over Engelse code die er al is: de
  Go-code onder `internal/` en `cmd/` en de Engelse delen van `pack/core/lib/` (bijvoorbeeld
  `config.py`). Uitkomst: nul vondsten.
- **Vangt Nederlands.** Een test draait de herkenning over bekende Nederlandse code: de docstrings
  en het commentaar in `bash_writes.py` zoals ze nu zijn, vastgelegd als fixture (het bestand zelf
  wordt straks vertaald). Uitkomst: elke Nederlandse docstring en elk Nederlands commentaarblok
  wordt gevonden.
- **Per taal** een fixture met een Nederlandse naam, een Nederlands commentaar en, voor Python, een
  Nederlandse docstring, en een Engelse tegenhanger zonder vondst.
- **Hookgedrag** zoals bij de bestaande gates: exit 0 zonder sleutel, exit 2 met bestand en regel,
  `COULD NOT CHECK` bij een kapot bestand en een kapotte config, de markering, `exempt` en het
  manifest slaan over, een bestand in een andere repo zonder `lighthouse.toml` geeft exit 0, en
  hetzelfde gedrag via `bash-write-gates`.
- De tellers die elke hook vastpinnen, gaan mee: `test_every_registration_has_a_payload` in
  `tests/pack/test_hooks_minimal_config.py`, de lijst geïnstalleerde bestanden in
  `internal/packs/core_test.go`, en `.lighthouse/manifest.toml` via
  `go run ./cmd/lighthouse init --upgrade` (dogfood; CI wil `No changes.`).
- Na dogfood staat de hook in lighthouse geïnstalleerd, maar zonder `code_language` in
  `lighthouse.toml`: hij doet niets (besluit 4).

## Aan de bouwer

Vrij te kiezen: de naam van de lib-module, de exacte woordenlijsten (binnen de regel "geen gangbaar
Engels woord"), hoe `bash_writes.py` meerdere gates per extensie draait, de tokenizer voor shell en
Go, en de exacte tekst van de meldingen (Engels, zoals alle hookmeldingen).

Niet vrij: de besluiten hierboven, en dat een vondst bestand en regel noemt.

## Risico's

- **Gemiste Nederlandse namen.** Een inhoudswoord dat niet in de lijst staat, glipt door. Dat is
  geaccepteerd: valse alarmen zijn erger, en de lijst groeit mee. Het vervolg-issue met een betere
  detector lost dit structureel op.
- **Eén woord kan een vals alarm geven** in een Engels blok met een Nederlandse eigennaam of
  citaat. De markering van besluit 8c vangt dat op; komt het vaak voor, dan gaat de drempel naar
  twee woorden.
- **De eerste edit in een Nederlands bestand wordt groot**, zodra een repo de hook aanzet: het hele
  bestand moet mee. Dat is bedoeld (besluit 2), en de reden dat lighthouse eerst vertaalt en dan
  aanzet.
