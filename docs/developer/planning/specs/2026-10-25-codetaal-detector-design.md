# Ontwerp: betere detector voor de taal van code

Issue: #37 · Datum: 2026-10-25 · Bouwt op: [hook voor de taal van code](2026-10-24-codetaal-hook-design.md) (#26)

## Aanleiding en doel

De hook van #26 herkent Nederlands met een woordenlijst: een blok of naam is Nederlands als er een
woord uit de lijst in staat. Wat niet in de lijst staat, glipt door. Dat gebeurde op 2026-10-24 met
een Nederlandse bestandsnaam. Twee dingen werkten daar samen: de hook stond in die repo niet aan
(geen `code_language`), en de lijst had de naam ook niet gevangen.

Deze spec draait de vraag om. De hook vraagt niet meer "staat hier een Nederlands woord uit de lijst
in?", maar "is dit Engels?". Wat niet als Engels herkend wordt, is een vondst. Daarnaast wordt het
bereik groter: niet alleen codebestanden, maar alles onder de paden waar een repo Engels eist,
zoals `pack/` in lighthouse. Docs en artefacten blijven in de projecttaal.

**Volgorde.** Dit issue levert het mechanisme. In lighthouse staat de hook na de merge nog steeds
uit. De vertaalslag is een eigen issue (zie *Vervolg*): die vertaalt het bestaande Nederlands met
deze detector als meetlat, en zet in dezelfde PR `code_language` en het bereik aan. Zo staat main
nooit rood.

## Doelen

- Herkenning die Engels herkent in plaats van Nederlands op te sommen, zodat een Nederlands woord
  buiten elke lijst toch gevonden wordt.
- Een tweede laag voor de twijfelgevallen: een taalmodel via een OpenAI-compatibele endpoint (de
  LiteLLM-gateway op de Beelink, een gratis API), met een time-out per aanroep.
- Een configureerbaar bereik: paden waarbinnen alles Engels moet zijn, ook Markdown en
  bestandsnamen.
- Zichtbaar bij twijfel (besluit 18 van de [M0-spec](2026-10-17-m0-pakket-design.md)): een
  tweede laag die faalt, valt terug op de eerste laag en meldt dat.
- Een regel in de instructies, zodat de agent meteen Engels schrijft en de hook het vangnet is.

## Non-goals

- **De vertaalslag** en het aanzetten van de hook in lighthouse. Zie *Vervolg*.
- **Andere talen dan Nederlands herkennen als niet-Engels.** Laag 1 vergelijkt Engels met
  Nederlands. In de meting gaf vergelijken met Duits, Frans en Spaans erbij vier keer zoveel valse
  alarmen (96 tegen 23), zonder Nederlands dat anders gemist werd. *Mijn keuze*: de vergelijkingstaal
  staat in de data, niet in de code, zodat er later een bij kan.
- **Stringliterals**, zoals in #26: data en tekst voor gebruikers, geen naam of commentaar. Daardoor
  blijven de Nederlandse varianten in `headings.toml` en een Nederlandstalige UI in een
  consumer-repo mogelijk.
- **Jev als schakel.** Jev zit achter een wachtlijst, en de API (`/v1/systemone`) is niet
  OpenAI-compatibel. Komt er toegang, dan is een schakel van dat type een vervolg.
- **Inhoud van TOML, YAML, JSON en tekstbestanden.** Binnen het bereik telt daar alleen de
  bestandsnaam. *Mijn keuze*: de inhoud is meestal data, en de meeste commentaarregels daarin zijn
  kort.
- **Een lokaal model op de machine zelf** (Ollama of een Python-model). Het lokale model draait op de
  Beelink, achter dezelfde gateway.

## Anti-goals

- **Een hook die zwijgt bij twijfel.** Een tweede laag die niet antwoordt, mag nooit lezen als
  "schoon".
- **Een taalmodel dat een vondst wegpraat.** Laag 2 voegt alleen vondsten toe. Een vondst van laag 1
  blijft staan, wat het model er ook van vindt.
- **Een hook die de agent stillegt als de Beelink traag is.** De tweede laag blokkeert nooit op
  zijn eigen falen: die meldt en valt terug.
- **Een geheim in `lighthouse.toml`.** De API-sleutel komt uit de omgeving of uit een env-bestand
  buiten de repo.
- **Main rood.** Geen test of CI-stap in dit issue die heel lighthouse op Engels controleert.

## Meting

Gemeten op 2026-10-25 met wegwerpscripts (niet in de repo). Engels: 9483 eenheden uit de
Python-standaardbibliotheek (20 modules), de Go-code onder `internal/` en `cmd/`, de skills onder
`pack/**/*.md`, `pack/core/lib/config.py` en drie shellscripts van git. Nederlands: 246 eenheden uit
de fixture `bash_writes_nl.py`, de specs in `docs/developer/planning/specs/` en 51 Nederlandse namen
(waarvan 6 samenstellingen). Een eenheid is een commentaarblok, docstring, naam, bestandsnaam of
alinea Markdown.

De Python-code van lighthouse zelf telde niet als Engels: daar staan nog ongeveer 1000 Nederlandse
blokken. Ook in de Go-code staat Nederlands (`ontwerpkeuze`, `besluit` in `internal/initcmd/`); de
detector vond dat als enige "vals alarm" in Go, en het is echt Nederlands.

| Detector | NL gevonden | Valse alarmen EN | Per eenheid | Koude start | Offline | Kosten |
|---|---|---|---|---|---|---|
| woordenlijst #26 | 195/246 | 0 | 0,002 ms | 0,01 s | ja | 0 |
| frequentie EN-vs-NL, samenstellingen gesplitst | 237/246 | 7 | 0,03 ms | 0,3 s (gebundelde lijst, stdlib) | ja | 0 |
| lingua, alleen EN/NL | 238/246 | 764 | 0,05 ms | 0,01 s, 292 MB installatie | ja | 0 |
| fastText lid.176 | 243/246 | 3988 | 0,02 ms | 0,2 s | ja | 0 |
| `anthropic-haiku-4-5` via LiteLLM, 20 twijfelgevallen in één aanroep | 17/20 | zie tekst | 1,5 s per aanroep | n.v.t. | nee | abonnement |
| `local-qwen3.6-35b-a3b` via LiteLLM, idem | 18/20 | zie tekst | 3,7 s warm, 2× time-out > 60 s | n.v.t. | Beelink | 0 |
| `fast` via LiteLLM, idem | 17/20 | zie tekst | 2,6 s en 18,6 s | n.v.t. | Beelink | 0 |
| Jev | niet gemeten: wachtlijst | | volgens leverancier 70–500 ms | | nee | $0,042 per miljoen tokens |

De zeven valse alarmen van de frequentiedetector zijn allemaal namen: `gids`, `opname` (twee keer),
`getint`, `anno`, `arg_vals`, `SCHEMA`. Elk taalmodel noemde `gids`, `opname` en `anno` ook
niet-Engels. Wat de frequentiedetector in deze eerste meting miste: samenstellingen die ook in Engelse delen te
splitsen zijn (`werkboom`; na de spec-review gevangen door besluit 2), en blokken met veel code en alleen korte Nederlandse woorden
(`` `%gd` van `git reflog` is … met `--date` ``). Op die gevallen scoorden de taalmodellen goed.

Algemene taaldetectoren (lingua, fastText) zijn goed op proza maar slecht op namen van één of twee
woorden. Daarom zijn ze geen kandidaat voor laag 1.

## Besluiten

| # | Besluit | Reden |
|---|---|---|
| 1 | **Laag 1** vergelijkt per woord hoe vaak het in het Engels en in het Nederlands voorkomt (zipf-frequentie). Een eenheid is een vondst als de som van (NL − EN) over de woorden boven een drempel komt. Vervangt de twee woordenlijsten van #26; besluit 6 van #26 vervalt | Meting: 237/246 tegen 195/246, met 7 valse alarmen op 9483 |
| 2 | Een woord dat in geen van beide talen voorkomt, wordt gesplitst. Lukt dat in delen van minstens drie letters die elk Nederlands leunen (zipf NL ≥ 2,5 en NL > EN), dan telt het als Nederlands, ook als er een Engelse splitsing bestaat. *(own choice after spec review, r1-f2)* | Vangt `schrijfpoorten`, `padpatronen` en `werkboom` (`werk` en `boom` leunen allebei Nederlands). `maxsize`, `worktree` en `dataclass` hebben geen splitsing in Nederlands-leunende delen. Gemeten: 240/246 Nederlands gevonden, en geen extra valse alarmen |
| 3 | In proza tellen woorden vanaf twee letters mee, in namen pas vanaf vier | Korte functiewoorden (`de`, `van`, `een`) dragen proza. In namen zijn korte delen meestal afkortingen (`val`, `typ`, `ver`) |
| 4 | De frequenties staan als gebundeld databestand in de lib van core, afgeleid van [wordfreq](https://github.com/rspeer/wordfreq) (Engels en Nederlands, zipf ≥ 1,5, ongeveer 280.000 woorden, 1,3 MB gzip). Laden met alleen de standaardbibliotheek. Naast het databestand staat een licentie- en naamsvermeldingsbestand dat mee installeert, en `LICENSES/` noemt het ook. Het generatiescript laat woorden uit `ci/literal-denylist.txt` weg. *Mijn keuze; vermelding en denylist: own choice after spec review, r1-f6, r1-f8* | Geen pip-afhankelijkheid: de hook draait met kale `python3`, zoals in #26 besluit 9. De data valt onder CC BY-SA 4.0, en `lighthouse init` kopieert het bestand naar elke consumer-repo: de vermelding moet dus mee. wordfreq bevat `paia`, `wbso` en `corneel`, en die breken `ci/literal-grep.sh` |
| 5 | Een kleine **jargonlijst** met woorden die in code Engels zijn maar in de data Nederlands lijken (`gids`, `opname`, `getint`, `anno`, `vals`, `schema`, ...). Een woord daarop telt als Engels | De zeven valse alarmen uit de meting. Ze zijn deterministisch en klein, dus een lijst is goedkoper dan een model |
| 6 | **Twijfel** is een eenheid die onder de drempel blijft, maar een woord bevat dat sterk Nederlands leunt (NL − EN ≥ 2 en NL ≥ 3). Alleen twijfelgevallen gaan naar laag 2. *(own choice after spec review, r1-f1, r1-f2)* | Gemeten: 0 twijfelgevallen in de Python-standaardbibliotheek, en 29 op 9483 Engelse eenheden. Die 29 zitten allemaal in lighthouse zelf, en zijn echt Nederlands (`besluit`, `Versiemarker`). Aan Nederlandse kant: `legacy_processed_lijst_weg` (`lijst`). Een bredere definitie (elk onbekend woord, scores tussen nul en de drempel) gaf twijfel in 19 van de 20 stdlib-modules, en dan draait laag 2 bij bijna elke write |
| 7 | **Laag 2** staat in één tabel `[code_language_llm]` met platte sleutels, zoals `SCHEMA` die al kent: `url` (de basis-URL van een OpenAI-compatibele API, inclusief `/v1`; de hook plakt er `/chat/completions` achter), `models` (lijst, de keten in volgorde), `key_env` (de naam van een omgevingsvariabele) en optioneel `env_file` (een bestand met `NAAM=waarde`-regels; `~` wordt uitgeklapt, en een relatief pad is een `ConfigError`). De hook probeert de modellen op volgorde, en het eerste geldige antwoord telt. *(own choice after spec review, r1-f3, r1-f10)* | Eén endpoint met een keten van modellen dekt de LiteLLM-gateway, die zelf naar haiku, lokale qwen of een gratis API routeert. Een lijst tabellen past niet in `SCHEMA` (`config.py:332-335`). Geen geheim in de config |
| 8 | Eén aanroep per bestand, met alle twijfelgevallen genummerd in één prompt, en een JSON-antwoord per nummer (`EN` of `NOT-EN`). Een antwoord dat niet te lezen is, telt als falen van die schakel | Meting: één batch van 20 kost 1,5 s, ongeveer evenveel als één losse vraag |
| 9 | Time-out: 4 s per model en 8 s voor laag 2 in totaal, als **harde wandkloktijd**: de aanroep draait in een thread of subproces dat bij de deadline wordt losgelaten, zodat ook DNS of een druppelend antwoord de grens niet passeert. Faalt de hele keten, dan schrijft de hook dat met tijdstip in een markeerbestand onder `$XDG_RUNTIME_DIR` (of `/tmp`), zoals `bash_writes.py` zijn staat bewaart. De volgende 60 s slaat elke hookaanroep laag 2 over en meldt dat. Is het markeerbestand niet te lezen of te schrijven, dan negeert de hook het; dat wordt nooit `COULD NOT CHECK`. *(Mijn keuze; wandklok en markeerbestand: own choice after spec review, r1-f5, r1-f9)* | De hook heeft 15 s (`hooks.json`). Een time-out van `urllib` begrenst alleen socketoperaties. De lokale modellen gaven soms een time-out van meer dan 60 s |
| 10 | Laag 2 voegt alleen vondsten toe. `NOT-EN` maakt van een twijfelgeval een vondst, `EN` laat het vallen. Een vondst van laag 1 gaat nooit naar laag 2 | Anti-goal: een model praat geen vondst weg. Laag 1 blijft voorspelbaar en testbaar |
| 11 | Faalt de hele keten (geen netwerk, time-out, fout, onleesbaar antwoord), dan blokkeert de hook niet op dat falen. Hij geeft het oordeel van laag 1, plus een **zichtbare melding**: dat laag 2 niet antwoordde (met de reden per schakel) en welke twijfelgevallen daardoor niet gecontroleerd zijn. De melding gaat naar de gebruiker (`systemMessage`) en naar de agent (`additionalContext`). Vondsten van laag 1 blokkeren zoals altijd. *Mijn keuze* (beide) | Besluit 18: niet stil. En niet blokkeren: een trage Beelink mag de agent niet stilleggen. De agent ziet zo de twijfelgevallen, en kan ze zelf nalopen |
| 12 | Zonder ketenconfig doet laag 2 niets en zegt de hook daar niets over | Zoals besluit 4 van #26: uit is een instelling, geen twijfel. Een consumer-repo zonder gateway krijgt anders bij elke write een melding |
| 13 | **Bereik**: `[project] code_language_paths`, een lijst globs. Binnen die globs is elk bestand in scope: de bestandsnaam altijd, en ook de mapnamen tussen het vaste begin van de glob en het bestand (bij `pack/**` en `pack/core/skills/<naam>/SKILL.md`: `core`, `skills` en `<naam>`), de inhoud van codebestanden zoals in #26, Markdown als proza, en van andere bestanden alleen de naam. Buiten de globs geldt #26: codebestanden op extensie, met hun bestandsnaam. `code_language_exempt` en het manifest (besluit 11 van #26) slaan nog steeds over. `code_language` blijft de aan-schakelaar: zonder die sleutel doet `code_language_paths` niets. *(aan-schakelaar: own choice after spec review, r1-f11)* | In lighthouse moet alles onder `pack/`, `internal/` en `cmd/` Engels zijn, ook skills. `docs/` en artefacten blijven Nederlands. Een consumer-repo zonder de sleutel werkt zoals nu |
| 14 | Markdown als proza: alinea's, koppen, lijsten en tabelcellen, plus de waarden in de frontmatter. Codeblokken, inline code, URL's en linkdoelen tellen niet mee. *Mijn keuze* | De `description` in de frontmatter van een skill is een instructie, en hoort dus Engels te zijn. Code en paden zijn geen taal |
| 15 | De markering `lighthouse: allow-language` werkt ook in Markdown, als HTML-commentaar aan het eind van een regel | Dezelfde uitzondering als besluit 8c van #26 |
| 16 | Een regel in het beheerde instructieblok van core (`pack/core/instructions/AGENTS.md`): met `[project] code_language` gezet zijn namen, commentaar, docstrings en bestandsnamen Engels, en ook alles onder `code_language_paths`; artefacten blijven in `[project] language`. De laag bevestigt de bouwer met `/lighthouse-instrument` | De skillkant van "skill en hook": de agent moet het weten vóór de write, niet pas bij de blokkade |
| 17 | Lighthouse zet in dit issue `code_language` en `code_language_paths` niet aan | Mergevolgorde: main mag niet rood. Aanzetten hoort bij de vertaalslag |
| 18 | Bash-writes van niet-codebestanden binnen het bereik (`.md`, `.toml`, een `mv`) controleert de hook in dit issue niet. Edit en Write wel. *Mijn keuze* | Dat vraagt dat `bash_writes.py` per repo de config leest, in een pre met een time-out van 5 s, en dat is een flinke verbouwing. Skills ontstaan bijna altijd via Edit en Write. Het vangnet is de test over heel lighthouse uit #58, die bij de test gate draait |
| 19 | Via `bash-write-gates` draait laag 2 niet. De twijfelgevallen komen dan in de niet-blokkerende melding van besluit 11, met de reden "layer 2 skipped under bash-write-gates". Hoe de gate dat weet (een omgevingsvariabele die `bash_writes.py` zet), kiest de bouwer. *(own choice after spec review, r1-f1)* | `bash-write-gates` draait de gates per bestand achter elkaar, met één deadline van 90 s (`bash_writes.py:807-810`). Een Bash-call die veel bestanden schrijft, zou anders per bestand op laag 2 wachten, en dan wordt alles `unchecked` en blokkeert het |

## Opbouw

### Laag 1 per eenheid

1. Splits de eenheid in woorden. Proza: letterreeksen, kleine letters. Namen: `split_name` van #26.
   Voor proza gaan inline code (tussen backticks) en URL's eerst weg.
2. Laat de woorden vallen die korter zijn dan de grens van besluit 3, of die op de jargonlijst staan.
3. Per woord: de score is `nl − en`, met de zipf-frequenties uit het databestand. Komt het woord in
   geen van beide voor, dan volgt besluit 2: een Nederlandse splitsing telt als een vaste positieve
   score, anders telt het woord als nul.
4. Is de som groter dan de drempel, dan is het een vondst. Blijft de som eronder, maar is er een
   woord dat sterk Nederlands leunt (besluit 6), dan is het een twijfelgeval. Anders is het schoon.

De drempel en de vaste score voor een samenstelling kiest de bouwer. Ze
moeten de meetdoelen onder *Bewijs* halen. Uitgangspunt uit de meting: drempel 1, samenstelling 3,
splitsing met delen van zipf ≥ 2,5 voor Nederlands en ≥ 2,0 voor Engels.

### Laag 2 per bestand

Na laag 1 over het hele bestand: zijn er twijfelgevallen, is er een `[code_language_llm]` en draait
de hook niet via `bash-write-gates` (besluit 19), dan één aanroep per model volgens besluit 7 tot en
met 9. De prompt noemt per geval wat het is (naam, commentaar,
bestandsnaam), en zegt dat afkortingen en codejargon Engels zijn.

### Wat de hook meldt

Bij vondsten: zoals #26 (exit 2, bestand, regel, soort, tekst, maximaal twintig). Elke vondst krijgt
er de laag bij (`layer 1` of `layer 2 (<model>)`), zodat een vals alarm terug te voeren is.

Bij het falen van laag 2 (besluit 11), met of zonder vondsten van laag 1:

```text
code-language: layer 2 did not answer (<schakel>: <reden>; …); <n> doubtful units not checked:
<bestand>:<regel>: name: <naam>
...
```

Is er geen vondst, dan blijft de exit 0 en gaat de melding als niet-blokkerende `systemMessage` naar
de gebruiker (zoals `worktree-guard.sh`) en als `additionalContext` naar de agent (zoals
`pr-create-gate.sh`). Via `bash-write-gates` moet dezelfde melding zichtbaar worden;
hoe, kiest de bouwer (zie *Aan de bouwer*).

`COULD NOT CHECK` blijft gelden voor wat #26 al noemt, en daarnaast voor een databestand dat
ontbreekt of niet te lezen is, en voor een `[code_language_llm]` die er wel is maar niet klopt (geen
`url` of lege `models`, een relatief of niet-bestaand `env_file`). Een sleutel die in de omgeving en
het `env_file` ontbreekt, is falen van laag 2 (besluit 11), geen `COULD NOT CHECK`.

### Config

- `project.code_language_paths` (`list[str]`, globs zoals bij `code_language_exempt`), zonder
  default.
- `code_language_llm.url` (`str`), `code_language_llm.models` (`list[str]`),
  `code_language_llm.key_env` (`str`) en `code_language_llm.env_file` (`str`), in `SCHEMA` van
  `pack/core/lib/config.py` (besluit 7). Voorbeeld:

  ```toml
  [code_language_llm]
  url = "http://<gateway-host>:8099/v1"
  models = ["anthropic-haiku-4-5", "local-qwen3.6-35b-a3b"]
  key_env = "LITELLM_MASTER_KEY"
  env_file = "~/.config/litellm/env"
  ```

### Waar het landt

- Laag 1 en 2 in de lib-module van #26 (`code_language.py`), of in een nieuwe module ernaast. De
  bouwer kiest.
- Het databestand en de jargonlijst naast de module, in `pack/core/lib/`. De woordenlijsten van #26
  gaan weg, met hun vermelding in `internal/packs/core_test.go`, `pack/SOURCES.toml` en
  `.lighthouse/manifest.toml`. De nieuwe bestanden komen in dezelfde drie. *(SOURCES.toml: own choice after spec review, r1-f7)*
- Een script dat het databestand opnieuw maakt uit wordfreq, buiten `pack/`, zodat het niet bij
  consumers landt. Het script draait met `uv run --with wordfreq`, en hoeft niet in de test gate.
- Het licentie- en naamsvermeldingsbestand naast het databestand (besluit 4), en een vermelding in
  `LICENSES/`.
- Dogfood: `go run ./cmd/lighthouse init --upgrade`, en het geïnstalleerde resultaat gecommit.

## Bewijs

- **Geen vals alarm op Engels.** Een test draait laag 1 over een vast Engels corpus: een selectie
  uit de stdlib-modules van de *Bijlage*, als fixture onder `tests/pack/fixtures/code_language/`
  met de PSF-licentievermelding, plus de Go-code onder `internal/` en `cmd/`, de skills onder
  `pack/**/*.md` en `pack/core/lib/config.py`. Doel: nul vondsten, en nul twijfelgevallen in de
  stdlib-fixture. *(fixture in plaats van de stdlib van de draaiende Python: own choice after spec
  review, r1-f4; de modules verschillen per Python-versie, en `pyproject.toml` staat 3.11 toe)* Bekend Nederlands
  in Go (`ontwerpkeuze`, `besluit`) staat met reden in een lijst in de test, zoals `ENGLISH_BLOCKS`
  in #26, totdat de vertaalslag het weghaalt. De modules staan in de *Bijlage*.
- **Vangt Nederlands.** De fixture `bash_writes_nl.py`: elk Nederlands blok wordt gevonden, en
  `UNDETECTABLE_BLOCKS` wordt kleiner of leeg. De Nederlandse namen uit de *Bijlage*: minstens 48 van
  de 51 zijn een vondst. De rest staat met reden in de test (gemeten: `legacy_processed_lijst_weg` is
  twijfel; `verzamelbump_container_images` en `codetaal_hook` worden gemist). De Engelse tegenhangers uit de *Bijlage*
  geven geen vondst. De specs telden mee in de meting, maar zijn geen testdoel: ze veranderen.
- **Laag 2** met een nep-endpoint in de test (een lokale HTTP-server):
  - `NOT-EN` geeft een vondst, met de laag in de melding;
  - `EN` geeft geen vondst;
  - een time-out, een HTTP-fout, een onleesbaar antwoord en een ontbrekende sleutel geven elk
    exit 0 plus de melding van besluit 11, met de twijfelgevallen erin;
  - de tweede schakel wordt geprobeerd als de eerste faalt;
  - een vondst van laag 1 gaat nooit naar het endpoint;
  - zonder ketenconfig gaat er geen aanroep uit en komt er geen melding.
- **Binnen de time-out.** Een nep-endpoint dat nooit antwoordt, één dat druppelend antwoordt, en een
  host die niet resolvet: de hook eindigt telkens binnen 10 s. Via `bash-write-gates` gaat er geen
  aanroep uit (besluit 19), en staan de twijfelgevallen in de melding.
- **Bereik.** Een `.md` en een `.toml` binnen `code_language_paths` met een Nederlandse naam of
  alinea geven een vondst. Dezelfde bestanden erbuiten geven niets. Een `.py` erbuiten wordt
  gecontroleerd zoals in #26.
- **Hookgedrag van #26** blijft groen, met de tellers die elke hook vastpinnen.

## Aan de bouwer

Vrij te kiezen: de module-indeling, de vorm van het databestand (zolang het met de stdlib laadt en
de koude start onder 0,5 s blijft), de exacte drempels binnen de meetdoelen, de inhoud van de
jargonlijst, de naam van de ketentabel, en de prompt. Ook vrij: hoe de melding van besluit 11 via
`bash-write-gates` zichtbaar wordt, bijvoorbeeld als regel in de uitvoer van de post of als eigen
oordeel naast `ok`, `finding` en `unchecked`. Eén voorwaarde: hij blijft niet stil en blokkeert
niet.

Niet vrij: de besluiten hierboven, dat laag 2 alleen toevoegt, en dat de hook zonder pip-pakket
draait.

## Risico's

- **De Beelink is traag of onbereikbaar.** Dan valt laag 2 terug en meldt dat. Gebeurt dat vaak,
  dan is de melding ruis. De time-out en de volgorde van de schakels zijn de knoppen.
- **Code naar een externe dienst.** Laag 2 stuurt twijfelgevallen (namen, commentaarregels) naar
  wat in de keten staat. Met de gateway is dat de Beelink, en via die gateway mogelijk Anthropic.
  Een repo kiest dat zelf door een keten te zetten.
- **Licentie van de data.** CC BY-SA 4.0 geldt voor het databestand, niet voor de rest van
  lighthouse. Blijkt dat toch een probleem, dan is hunspell (`en_US`, `nl_NL`) het alternatief. Dat
  is wel opnieuw meten, want hunspell kent geen frequenties.
- **Valse alarmen in een consumer-repo** op jargon dat niet in de meting zat. De jargonlijst en de
  markering vangen dat op. De lijst groeit mee.

## Vervolg

- **Vertaalslag lighthouse** (nieuw issue, wacht op #37): vertaalt het Nederlands in `pack/`,
  `internal/`, `cmd/`, `tests/` en `.lighthouse/` met deze detector als meetlat. In dezelfde PR komen
  `code_language = "en"` en `code_language_paths` in `lighthouse.toml`. Die PR is pas klaar als de
  detector over de hele tree nul vondsten geeft.
- **Paia aanzetten**, in paia zelf, na de vertaalslag van lighthouse.
- **Jev als schakel**, zodra er toegang is.

## Bijlage: meetcorpus

Deze lijsten maken de meting en de testdoelen reproduceerbaar.

**Modules uit de standaardbibliotheek** (relatief aan `sysconfig.get_paths()["stdlib"]`):
`argparse.py`, `subprocess.py`, `pathlib/_local.py`, `tomllib/_parser.py`, `json/decoder.py`,
`shutil.py`, `tempfile.py`, `logging/__init__.py`, `dataclasses.py`, `functools.py`,
`textwrap.py`, `difflib.py`, `configparser.py`, `zipfile/__init__.py`, `csv.py`, `enum.py`,
`typing.py`, `inspect.py`, `ast.py`, `tokenize.py`. De meting gebruikte daarnaast `git-sh-setup`,
`git-submodule` en `git-mergetool` uit `/usr/libexec/git-core`. Die zijn niet op elke machine te
vinden, dus de test neemt ze niet mee.

**Nederlandse namen (51):** `lees_bestand`, `schrijf_uitvoer`, `verwerk_transcript`, `maak_handoff`,
`controleer_poort`, `opschonen`, `vergadering_notities`, `zoek_issue`, `haal_labels_op`,
`bewaar_resultaat`, `bereken_dekking`, `wachtrij_leegmaken`, `controleerTaal`, `leesConfiguratie`,
`VerwerkerFout`, `BestandNietGevonden`, `aantal_regels`, `huidige_tak`, `standaard_tak`,
`gebruiker_naam`, `taak_lijst`, `sluit_issue`, `stuur_bericht`, `nieuwe_versie`, `oude_kopie`,
`werkboom_audit`, `bord_status`, `sleutel_waarde`, `invoer_controle`, `uitzonderingen_lijst`,
`blokkade_label`, `schrijfpoorten`, `hookmelding_tekst`, `padpatronen`, `geinstalleerde_bestanden`,
`afhankelijkheden`, `laatste_wijziging`, `voorganger_pr`, `opvolger`, `tijdslimiet`,
`terugval_woordenlijst`, `vondsten`, `kon_niet_controleren`, `verzamelbump_container_images`,
`legacy_processed_lijst_weg`, `codetaal_hook`, `vergaderingen_als_data`, `dagelijkse_samenvatting`,
`notulen_verwerken`, `spraak_naar_tekst`, `agenda_bijwerken`.

**Engelse tegenhangers (43):** `read_file`, `write_output`, `process_transcript`, `make_handoff`,
`check_gate`, `cleanup`, `meeting_notes`, `find_issue`, `fetch_labels`, `save_result`,
`compute_coverage`, `drain_queue`, `checkLanguage`, `readConfig`, `ParserError`, `FileNotFound`,
`line_count`, `current_branch`, `default_branch`, `user_name`, `task_list`, `close_issue`,
`send_message`, `new_version`, `old_copy`, `worktree_audit`, `board_status`, `key_value`,
`input_check`, `exceptions_list`, `blocked_label`, `write_gates`, `hook_message`, `path_patterns`,
`installed_files`, `dependencies`, `last_change`, `predecessor_pr`, `successor`, `timeout`,
`fallback_wordlist`, `findings`, `could_not_check`.
