# Betere detector voor de taal van code — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** de hook van #26 vraagt niet meer "staat hier een Nederlands woord uit de lijst?" maar "is
dit Engels?", met een taalmodel als tweede laag voor twijfelgevallen en een configureerbaar bereik
waarbinnen ook Markdown en bestandsnamen Engels moeten zijn.

**Architectuur:** laag 1 is een nieuwe module `code_language_score.py`: zipf-frequenties Engels
tegen Nederlands uit een gebundeld databestand, met een oordeel `finding`, `doubt` of `clean` per
eenheid. Laag 2 is een nieuwe module `code_language_llm.py`: één aanroep per bestand naar een keten
modellen achter een OpenAI-compatibele endpoint, met een harde wandklok. `code_language.py` houdt de
scanners, krijgt een Markdown-scanner en het bereik, en zet beide lagen achter elkaar; de wrapper
`code-language.sh` geeft een niet-blokkerende melding door, en `bash_writes.py` laat zo'n melding
van een gate door.

**Tech stack:** Python 3.11+ standaardbibliotheek (`gzip`, `urllib.request`, `threading`,
`fnmatch`), bash en `jq`; pytest; wordfreq alleen in het generatiescript (`uv run --with`); Go
alleen voor de lijst geïnstalleerde bestanden.

**Spec:** [2026-10-25-codetaal-detector-design.md](../specs/2026-10-25-codetaal-detector-design.md)
(bouwt op [2026-10-24-codetaal-hook-design.md](../specs/2026-10-24-codetaal-hook-design.md))

## Algemene randvoorwaarden

- Alleen standaardbibliotheek in alles onder `pack/`; de hook draait met kale `python3`
  (besluit 4, *Aan de bouwer*).
- Laag 1-waarden (uitgangspunt *Opbouw*): drempel `1.0` (vondst als de som er strikt boven komt),
  samenstelling `3.0`, delen van een samenstelling minstens 3 letters met zipf NL ≥ `2.5` en NL > EN
  (besluit 2), twijfel bij een woord met NL − EN ≥ `2` en NL ≥ `3` (besluit 6), woordlengte vanaf
  `2` in proza en vanaf `4` in namen (besluit 3). De "≥ 2,0 voor Engels" uit *Opbouw* geldt niet:
  besluit 2 zegt dat een Nederlandse splitsing telt, ook als er een Engelse bestaat.
- Databestand: Engels en Nederlands uit wordfreq, zipf ≥ `1.5`, laden met de stdlib, koude start
  onder `0.5` s (*Aan de bouwer*), met licentie- en naamsvermeldingsbestand ernaast en de licentie in
  `LICENSES/` (besluit 4).
- Laag 2: config in `[code_language_llm]` met `url`, `models`, `key_env`, `env_file` (besluit 7);
  de hook plakt `/chat/completions` achter `url`; time-out `4` s per model en `8` s totaal als
  wandklok (besluit 9); na falen van de hele keten `60` s overslaan via een markeerbestand onder
  `$XDG_RUNTIME_DIR` of `/tmp` (besluit 9).
- Laag 2 voegt alleen toe; een vondst van laag 1 gaat nooit naar de endpoint (besluit 10).
- Zonder `[code_language_llm]`: geen aanroep, geen melding (besluit 12).
- `code_language` blijft de aan-schakelaar; zonder die sleutel doet `code_language_paths` niets
  (besluit 13). Lighthouse zet geen van beide aan (besluit 17).
- Markering in Markdown: `lighthouse: allow-language` in een HTML-commentaar op de regel
  (besluit 15).
- `COULD NOT CHECK` (exit 2, `code-language: COULD NOT CHECK — <reden>` als eerste regel) blijft
  voor wat #26 noemt, plus: databestand ontbreekt of is onleesbaar, en een `[code_language_llm]`
  zonder `url`, met lege `models`, of met een relatief of niet-bestaand `env_file`. Een ontbrekende
  sleutel is falen van laag 2, geen `COULD NOT CHECK`.
- Meldingen, code, namen, commentaar en geïnstalleerde bestanden in het Engels; commitberichten in
  het Nederlands.
- Dogfood: elke commit met een wijziging onder `pack/` draait eerst
  `go run ./cmd/lighthouse init --upgrade` en neemt de geïnstalleerde kopie mee; een tweede run geeft
  `No changes.`
- Testpoort: `uv run pytest -q && go test ./...`.

## Aandachtspunten voor de review

1. **Een fenced codeblok of inline code met Nederlandse namen in een Markdown-bestand binnen het
   bereik**: code is geen taal, dus geen vondst. Test in taak 4.
2. **Een binair of niet-UTF-8-bestand binnen het bereik** (een `.png` onder `pack/`): alleen de naam
   telt, de inhoud wordt nooit gelezen, en het is geen `COULD NOT CHECK`. Test in taak 4.
3. **Een rommelig antwoord van het model**: JSON in een ```` ```json ````-blok of met tekst eromheen
   wordt gelezen; een antwoord zonder een van de nummers is onleesbaar en de volgende schakel komt aan
   de beurt. Test in taak 5.
4. **Een markeerbestand dat niet te lezen of te schrijven is** (map niet schrijfbaar, inhoud geen
   getal): laag 2 draait gewoon, de hook meldt niets over het bestand. Test in taak 5.
5. **Een glob zonder joker of een bestand direct onder het vaste begin** (`pack/**` met
   `pack/notulen.md`, of glob `docs/x.md`): geen mapnamen uit het vaste begin, wel de bestandsnaam.
   Test in taak 4.

---

### Taak 1: databestand, generatiescript en naamsvermelding

**Bestanden:**
- Maak: `tools/gen_code_language_freq.py`
- Maak: `pack/core/lib/code_language_freq.tsv.gz` (gegenereerd)
- Maak: `pack/core/lib/code_language_freq_NOTICE.txt`
- Maak: `LICENSES/CC-BY-SA-4.0.txt`
- Wijzig: `pack/SOURCES.toml`, `internal/packs/core_test.go` (lijst geïnstalleerde bestanden)
- Test: `tests/test_gen_code_language_freq.py`

**Interfaces:**
- Levert: het bestand `code_language_freq.tsv.gz`: gzip van UTF-8-regels `woord\ten\tnl\n`,
  gesorteerd op woord, `en` en `nl` als gehele getallen (zipf × 10, afgerond; `0` = komt niet voor).
- Levert in het script: `denied_patterns(path: Path) -> list[str]`, `keep(word: str, patterns:
  list[str]) -> bool`, `encode(rows: list[tuple[str, int, int]], level: int = 9) -> bytes`,
  `main() -> int`.

- [ ] **Stap 1: schrijf de falende tests** in `tests/test_gen_code_language_freq.py`; laad het
  script met `importlib.util.spec_from_file_location` (geen package). Het script importeert wordfreq
  alleen in de functie die de rijen maakt, zodat de test zonder wordfreq draait.
  - `test_keep_drops_denied_substrings`: met patronen `["corneel", "paia"]`:
    `keep("corneel", p) is False`, `keep("xpaiax", p) is False`, `keep("read", p) is True`;
    hoofdletters tellen niet (`keep("Paia", p) is False`).
  - `test_denied_patterns_skip_comments_and_blanks`: een bestand met `# x`, een lege regel en
    `Beelink` geeft `["beelink"]`.
  - `test_encode_is_deterministic_and_sorted`: `encode([("b", 10, 0), ("a", 74, 56)])` twee keer is
    byte-gelijk, en `gzip.decompress(...)` is `"a\t74\t56\nb\t10\t0\n"`.
- [ ] **Stap 2:** `uv run pytest -q tests/test_gen_code_language_freq.py` → FAIL (bestand bestaat
  niet).
- [ ] **Stap 3: schrijf `tools/gen_code_language_freq.py`.** Rijen: de unie van
  `wordfreq.iter_wordlist("en")` en `iter_wordlist("nl")`, alleen woorden die volledig uit letters
  bestaan (`re.fullmatch(r"[^\W\d_]+", w)`), kleine letters, `round(zipf_frequency(w, lang) * 10)`
  per taal, bewaard als de ongeronde `max(zipf_en, zipf_nl) >= 1.5` (filter vóór het afronden), en
  `keep` tegen `ci/literal-denylist.txt`.
  `encode(rows, level=9)`: `gzip.compress(data, compresslevel=level, mtime=0)`. `main` probeert
  `level` 9, 8, 7, 6 op volgorde en schrijft de eerste uitvoer waarin geen patroon als bytes in
  `uitvoer.lower()` staat (`ci/literal-grep.sh` leest het binaire bestand ook) naar
  `pack/core/lib/code_language_freq.tsv.gz`; geeft geen enkel niveau schone bytes, dan rc 1 met het
  patroon in de melding. De moduledocstring noemt het commando uit stap 4 en de wordfreq-versie.
- [ ] **Stap 4: genereer.** `uv run --with wordfreq==3.1.1 python tools/gen_code_language_freq.py`
  → rc 0; `zcat pack/core/lib/code_language_freq.tsv.gz | wc -l` geeft ongeveer 280.000; het bestand
  is ongeveer 1,3 MB.
- [ ] **Stap 5: naamsvermelding.** `code_language_freq_NOTICE.txt` (Engels): afgeleid van wordfreq
  3.1.1 (Robyn Speer, <https://github.com/rspeer/wordfreq>), de data valt onder CC BY-SA 4.0
  (<https://creativecommons.org/licenses/by-sa/4.0/>), alleen dit databestand en niet de rest van
  lighthouse; de wijzigingen (alleen `en` en `nl`, zipf ≥ 1.5, × 10 afgerond, woorden uit de
  denylist weg); daaronder letterlijk de sectie met bronvermeldingen van de data uit de README van
  wordfreq 3.1.1. `LICENSES/CC-BY-SA-4.0.txt`:
  `curl -fsSL https://raw.githubusercontent.com/spdx/license-list-data/main/text/CC-BY-SA-4.0.txt`;
  de eerste regel bevat `Attribution-ShareAlike 4.0 International` (op 2026-10-25:
  `Creative Commons Attribution-ShareAlike 4.0 International`). De tekst wordt niet aangepast.
- [ ] **Stap 6: registreer.** In `pack/SOURCES.toml` twee `[[file]]`-items met `new = true` voor het
  databestand en de vermelding, met een commentaarregel erboven die naar
  `tools/gen_code_language_freq.py` en wordfreq verwijst. In `internal/packs/core_test.go` de twee
  geïnstalleerde paden onder `.lighthouse/lib/lighthouse_lib/`, op alfabetische plek.
- [ ] **Stap 7:** `go run ./cmd/lighthouse init --upgrade`, daarna
  `uv run pytest -q tests/test_gen_code_language_freq.py tests/test_sources.py tests/test_literal_grep.py && go test ./internal/packs/`
  → PASS; een tweede `init --upgrade` → `No changes.`
- [ ] **Stap 8: commit.**

```bash
git add tools/ tests/test_gen_code_language_freq.py pack/core/lib/code_language_freq* LICENSES/ pack/SOURCES.toml internal/packs/core_test.go .lighthouse/ .claude/hooks/lighthouse/
git commit -m "feat(core): woordfrequenties EN/NL uit wordfreq als databestand (#37)"
```

### Taak 2: laag 1 — score per eenheid

**Bestanden:**
- Maak: `pack/core/lib/code_language_score.py`
- Maak: `pack/core/lib/code_language_jargon.txt`
- Wijzig: `pack/core/requires.toml` (`lighthouse_lib.code_language_score` in de lijst `python3`),
  `pack/SOURCES.toml`, `internal/packs/core_test.go`
- Test: `tests/pack/test_code_language_score.py`

**Interfaces:**
- Consumeert: het databestand uit taak 1.
- Levert in `code_language_score`:
  - `class DataError(Exception)`: databestand of jargonlijst ontbreekt of is onleesbaar; de
    boodschap is de reden.
  - `frequencies() -> Mapping[str, tuple[int, int]]`: `(en, nl)` als zipf × 10, één keer per proces
    geladen.
  - `jargon() -> frozenset[str]`: de woorden uit `code_language_jargon.txt` (één per regel, `#` is
    commentaar), één keer geladen.
  - `split_name(name: str) -> list[str]` en `words(text: str) -> list[str]`: verhuisd uit
    `code_language.py`, gedrag ongewijzigd.
  - `word_score(word: str) -> float`: `(nl − en) / 10`; een woord dat in geen van beide voorkomt
    geeft `COMPOUND_SCORE` als `dutch_split(word)`, anders `0.0`.
  - `dutch_split(word: str) -> bool`: besluit 2.
  - `judge_name(name: str) -> str` en `judge_text(text: str) -> str`: `"finding"`, `"doubt"` of
    `"clean"`.
  - Constanten `THRESHOLD = 1.0`, `COMPOUND_SCORE = 3.0`, `PART_MIN = 3`, `PART_NL_FLOOR = 2.5`,
    `DOUBT_LEAN = 2.0`, `DOUBT_NL = 3.0`, `NAME_MIN = 4`, `TEXT_MIN = 2`.

- [ ] **Stap 1: schrijf de falende tests** in `tests/pack/test_code_language_score.py`
  (`score = load_hook_lib("code_language_score")`). Twee groepen.

  *Algoritme, met een nep-tabel* (`monkeypatch.setattr(score, "frequencies", lambda: TABLE)` en
  `monkeypatch.setattr(score, "jargon", lambda: frozenset({"gids"}))`):
  - `test_threshold_is_strict`: een naam waarvan de som precies `1.0` is → `"clean"`; `1.1` →
    `"finding"`.
  - `test_doubt_needs_a_strongly_dutch_word`: `legacy_lijst` met `legacy = (40, 10)` en
    `lijst = (20, 50)` (som 0, `lijst` leunt 3 en NL 5) → `"doubt"`; met `lijst = (20, 35)` (leunt
    1,5) → `"clean"`.
  - `test_short_parts_skipped_in_names_not_in_prose`: `judge_name("val_typ_ver") == "clean"` terwijl
    die drie in de tabel sterk Nederlands zijn; `judge_text("de van een")` met `de`, `van`, `een`
    Nederlands → `"finding"`.
  - `test_jargon_counts_as_english`: `gids = (10, 40)` → `judge_name("gids") == "clean"`.
  - `test_dutch_split`: met `werk = (30, 50)`, `boom = (25, 40)` en `werkboom` afwezig →
    `dutch_split("werkboom")` en `word_score("werkboom") == 3.0`; met `boom = (45, 40)` (EN hoger)
    → `False` en score `0.0`; `abcdef` met `abc` en `def` afwezig → `False`.
  - `test_prose_drops_inline_code_and_urls`: ``judge_text("see `lees_bestand` at https://x.nl/van")``
    met alleen Engelse `see` en `at` in de tabel → `"clean"`.

  *Echte data* (spec, *Besluiten* en *Bewijs*):
  - `test_spec_compounds`: `dutch_split` is waar voor `schrijfpoorten`, `padpatronen`, `werkboom`,
    onwaar voor `maxsize`, `worktree`, `dataclass`.
  - `test_spec_false_alarms_are_clean`: `judge_name` geeft `"clean"` voor `gids`, `opname`,
    `getint`, `anno`, `arg_vals`, `SCHEMA`.
  - `test_spec_doubt`: `judge_name("legacy_processed_lijst_weg") == "doubt"`.
  - `test_prose`: `judge_text("Het bestand wordt niet gelezen.") == "finding"`,
    `judge_text("Read the file and return its lines.") == "clean"`.
  - `test_dutch_names_found`: de 51 Nederlandse namen uit de *Bijlage* van de spec als lijst;
    `NOT_FOUND = {"legacy_processed_lijst_weg": "doubt (measured)", "verzamelbump_container_images":
    "missed (measured)", "codetaal_hook": "missed (measured)"}`; elke andere naam is `"finding"`, en
    elke naam in `NOT_FOUND` is géén `"finding"` (zodat de lijst krimpt als dat verandert).
  - `test_english_names_not_found`: de 43 Engelse tegenhangers uit de *Bijlage*: geen enkele is
    `"finding"`.
  - `test_missing_data_is_data_error`: `monkeypatch.setattr(score, "FREQ_FILE",
    "nope.tsv.gz")` plus `score.frequencies.cache_clear()` → `pytest.raises(score.DataError)`; in de
    teardown opnieuw `cache_clear()`.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_code_language_score.py` → FAIL.
- [ ] **Stap 3: schrijf `code_language_score.py`.** `frequencies` en `jargon` met
  `functools.cache`; interne code roept ze aan via de modulenaam, zodat een test ze kan vervangen.
  `FREQ_FILE = "code_language_freq.tsv.gz"` en `JARGON_FILE = "code_language_jargon.txt"`, gelezen
  naast `__file__`; elke `OSError`, `EOFError`, `gzip.BadGzipFile`, `UnicodeDecodeError` of
  `ValueError` wordt `DataError`. `dutch_split` gebruikt een memo per aanroep, geen cache op
  moduleniveau. `judge_*`: woorden korter dan de grens en jargonwoorden vallen weg; som van
  `word_score` > `THRESHOLD` → `"finding"`; anders een woord in de tabel met
  `(nl − en)/10 ≥ DOUBT_LEAN` en `nl/10 ≥ DOUBT_NL` → `"doubt"`; anders `"clean"`; geen woorden
  over → `"clean"`. `judge_text` haalt eerst `` `…` `` en `https?://\S+` weg. Jargonlijst start met
  `gids`, `opname`, `getint`, `anno`, `vals`, `schema`, plus wat de corpustests nog
  vinden aan Engels jargon (taak 3 en 4).
- [ ] **Stap 4:** dezelfde pytest-run → PASS. Meet de koude start:
  `python3 -c "import sys,time; sys.path.insert(0,'pack/core/lib'); import code_language_score as s; t=time.perf_counter(); s.frequencies(); print(time.perf_counter()-t)"`
  → onder `0.5`. Kom je erboven, verander de vorm van het laden (niet het bestandsformaat van
  taak 1) tot het eronder komt.
- [ ] **Stap 5: registreer** de module en de jargonlijst in `pack/SOURCES.toml` (`new = true`) en in
  `internal/packs/core_test.go`; `init --upgrade`; `go test ./internal/packs/` → PASS.
- [ ] **Stap 6: commit** (`feat(core): laag 1 vergelijkt EN- en NL-frequentie per woord (#37)`).

### Taak 3: scanners op laag 1, woordenlijsten weg

**Bestanden:**
- Wijzig: `pack/core/lib/code_language.py`
- Verwijder: `pack/core/lib/code_language_nl_content.txt`, `pack/core/lib/code_language_nl_function.txt`
  (en hun items in `pack/SOURCES.toml` en `internal/packs/core_test.go`)
- Maak: `tests/pack/fixtures/code_language/stdlib/` (zie stap 1) met `LICENSE-PSF.txt`
- Test: `tests/pack/test_code_language.py`, nieuw `tests/pack/test_code_language_corpus.py`

**Interfaces:**
- Consumeert: `code_language_score.judge_name`, `judge_text`, `split_name`, `words`, `DataError`.
- Levert in `code_language`:
  - `Finding(NamedTuple)`: `line: int`, `kind: str` (`comment`, `docstring`, `name`; taak 5 voegt
    `directory` en `prose` toe), `text: str` (de naam, of de eerste regel van het blok),
    `layer: str = "layer 1"`, `doubt: bool = False` (waar = twijfelgeval, geen vondst),
    `unit: str = ""` (de hele eenheid voor laag 2; leeg bij een naam).
  - `Inspection(NamedTuple)`: `findings: list[Finding]`, `doubts: list[Finding]`.
  - `scan_python`, `scan_shell`, `scan_go(source: str) -> list[Finding]`: vondsten en twijfelgevallen
    door elkaar, gesorteerd op regel.
  - `check_file(path: Path, root: Path) -> Inspection`.
  - `split_name` blijft importeerbaar uit `code_language` (alias van die in `code_language_score`).
  - `code_language` importeert de module zelf (`from lighthouse_lib import code_language_score`) en
    roept `code_language_score.judge_name(...)` enzovoort aan, geen losse namen: een test vervangt
    zo een functie op `cl.code_language_score`. Dat is een ander moduleobject dan
    `load_hook_lib("code_language_score")` (`tests/pack/_testkit.py:136-149`), dus tests van
    `code_language` patchen altijd `cl.code_language_score`.
  - Weg: `is_dutch_text`, `is_dutch_name`, `FUNCTION_WORDS`, `CONTENT_WORDS`.

- [ ] **Stap 1: fixture.** Kopieer uit de stdlib van de Python in `.venv` (3.13) naar
  `tests/pack/fixtures/code_language/stdlib/`, plat van naam: `argparse.py`, `shutil.py`,
  `tempfile.py`, `textwrap.py`, `difflib.py`, `configparser.py`, `csv.py`, `functools.py`,
  `json/decoder.py` → `json_decoder.py`, `tomllib/_parser.py` → `tomllib_parser.py`. Daarnaast
  `LICENSE-PSF.txt`, gekopieerd uit `LICENSE.txt` naast die stdlib, met bovenaan één regel welke
  Python-versie de bron was.
- [ ] **Stap 2: schrijf de falende tests.**
  In `tests/pack/test_code_language_corpus.py` (spec, *Bewijs*):
  - `test_stdlib_fixture_clean`: per bestand in de fixture `cl.scan_python(...) == []`: nul vondsten
    én nul twijfelgevallen.
  - `test_go_english`: `cl.scan_go` over `internal/**/*.go` en `cmd/**/*.go`: geen twijfelgevallen
    meegeteld, en elke vondst bevat een woord uit
    `KNOWN_DUTCH = {"ontwerpkeuze": "Dutch in Go until #58 translates it", "ontwerpkeuzes": ...,
    "besluit": ...}`; elk woord in `KNOWN_DUTCH` hoort bij minstens één vondst, zodat de lijst krimpt
    als #58 het weghaalt.
  - `test_config_py_english`: `cl.scan_python` over `pack/core/lib/config.py` geeft geen vondst
    buiten `KNOWN_DUTCH` (twijfelgevallen tellen niet).
  - `test_hooks_english`: idem met `cl.scan_shell` over `pack/core/hooks/*.sh`.
  In `tests/pack/test_code_language.py`:
  - vervang `test_banned_words_absent`, `test_dutch_text`, `test_dutch_name` en
    `test_english_corpus_has_no_dutch_words` door niets: taak 2 en de corpustests dekken ze.
    `BANNED` verdwijnt.
  - verhuis `test_python_english_clean` (config.py), `test_go_english_clean` en
    `test_shell_english_corpus` (`pack/core/hooks/*.sh`) naar de corpustests: ze eisen nu `== []` op
    de scan, maar de scan geeft ook twijfelgevallen, en lighthouse heeft echt Nederlands
    (`besluit` in `pack/core/lib/config.py:51` en in de kopcommentaar van de hooks, `ontwerpkeuze`
    in Go). `test_go_english_clean` gaat op in `test_go_english`; `test_config_py_english` en een
    nieuwe `test_hooks_english` (zelfde vorm, over `pack/core/hooks/*.sh` met `scan_shell`) tellen
    alleen vondsten (`not f.doubt`), en hun vondsten moeten ook in `KNOWN_DUTCH` vallen.
  - `test_python_comment_block`: blok `# first line\n# dit is niet goed` → vondst op regel 2 met
    tekst `first line` (het blok telt als geheel).
  - `test_doubt_is_not_a_finding`: `legacy_processed_lijst_weg = 1\n` →
    `check_file(...).findings == []` en `[d.text for d in check_file(...).doubts] ==
    ["legacy_processed_lijst_weg"]`.
  - `test_finding_layer`: elke vondst uit `check_file` heeft `layer == "layer 1"`, en
    `_report` zet ` [layer 1]` achter de regel: `out[1] == f"{path}:1: comment: dit is niet goed [layer 1]"`.
  - `test_missing_data_is_cannot_check`: `monkeypatch.setattr(cl.code_language_score, "FREQ_FILE",
    "nope")` plus `cl.code_language_score.frequencies.cache_clear()` (ook in de teardown) →
    `pytest.raises(cl.CannotCheck)` uit `check_file`.
  - bestaande `check_file`-tests: vergelijk `.findings` in plaats van de lijst zelf.
  - `test_dutch_corpus_found`: `found` telt alleen vondsten (`not f.doubt`); een blok dat alleen
    twijfel is, staat in een nieuwe lijst `DOUBT_BLOCKS: dict[int, str]` met reden, met dezelfde
    controles als `UNDETECTABLE_BLOCKS` (staat het blok niet meer als twijfel, dan gaat het uit de
    lijst). `UNDETECTABLE_BLOCKS` wordt kleiner of leeg: de test eist al dat een gevonden regel uit
    de lijst gaat.
- [ ] **Stap 3:** `uv run pytest -q tests/pack/test_code_language.py tests/pack/test_code_language_corpus.py`
  → FAIL.
- [ ] **Stap 4: implementeer.** `_dutch_blocks` wordt `_judge_blocks`: de tekst van de niet-gemarkeerde
  regels samen (`"\n".join`) gaat naar `judge_text`; niet-`clean` geeft één `Finding` op de eerste
  regel met `doubt = verdict == "doubt"` en `unit` = die samengevoegde tekst. Docstrings idem.
  `_name_findings` gebruikt `judge_name`. `check_file` splitst de scan in `findings` en `doubts`; de
  bestandsnaam gaat via dezelfde weg. `DataError` wordt `CannotCheck`. `_report` zet
  ` [<layer>]` achter elke regel en meldt alleen `findings`; `main` geeft `check_file(...).findings`
  aan `_report`. Een vals alarm uit de corpustests is Engels jargon: zet het op de jargonlijst
  (taak 2), niet in de test. Echt Nederlands (`besluit`, `ontwerpkeuze` en wat nog meer opduikt in
  lighthouse zelf) gaat nooit op de jargonlijst, maar in `KNOWN_DUTCH`, met reden.
- [ ] **Stap 5:** dezelfde pytest-run → PASS; daarna `uv run pytest -q tests/pack/` → PASS (de
  hooktests van #26 blijven ongewijzigd groen).
- [ ] **Stap 6:** verwijder de twee woordenlijsten met hun items, `init --upgrade`,
  `go test ./internal/packs/` → PASS.
- [ ] **Stap 7: commit** (`feat(core): scanners oordelen met laag 1; woordenlijsten van #26 weg (#37)`).

### Taak 4: bereik en Markdown

**Bestanden:**
- Wijzig: `pack/core/lib/config.py` (`SCHEMA`), `pack/core/lib/code_language.py`,
  `pack/core/hooks/code-language.sh` (geen extensiefilter meer)
- Test: `tests/pack/test_config.py`, `tests/pack/test_code_language.py`,
  `tests/pack/test_code_language_corpus.py`, `tests/pack/test_code_language_hook.py`

**Interfaces:**
- Consumeert: `Finding`, `Inspection`, `judge_text`, `judge_name` (taak 2–3).
- Levert:
  - `lookup(cfg, "project.code_language_paths") -> list[str]` (`KeyError` als niet gezet).
  - `scan_markdown(source: str) -> list[Finding]`, alle met `kind == "prose"`.
  - `scope_dirs(relative: str, globs: list[str]) -> list[str] | None`: `None` als geen glob het pad
    dekt; anders de mapnamen tussen het vaste begin van elke dekkende glob en het bestand (unie, in
    padvolgorde, zonder dubbele).
  - Nieuwe soort `directory` (regel 1, tekst de mapnaam).

- [ ] **Stap 1: schrijf de falende tests.**
  - `tests/pack/test_config.py`: `test_code_language_paths_is_list_of_str` (`["pack/**"]` wordt
    gelezen; `"pack"` → `ConfigError`).
  - `tests/pack/test_code_language.py`, Markdown:
    - `test_markdown_units`: een document met frontmatter (`description: Lees het bestand en geef de
      regels terug.`), een kop, een alinea over twee regels, een lijstitem en een tabel met een cel
      `niet gevonden`: elk geeft een vondst op zijn eigen regel; dezelfde structuur in het Engels
      geeft `[]`.
    - `test_markdown_folded_frontmatter`: `description: >` met twee ingesprongen regels is één
      eenheid, op de regel van `description`.
    - `test_markdown_code_is_not_language` *(aandachtspunt 1)*: een fenced blok (```` ``` ```` en
      `~~~`) met `lees_bestand = 1`, een ingesprongen codeblok (vier spaties na een lege regel, niet
      in een lijst) met `controleer_poort(invoer)`, inline `` `lees_bestand` `` in een Engelse zin, een link
      `[the file](docs/lees-bestand.md)` en een kale URL met Nederlandse woorden → `[]`.
    - `test_markdown_marker`: `Dit is niet goed. <!-- lighthouse: allow-language -->` → `[]`.
  - Bereik:
    - `test_scope_dirs`: `scope_dirs("pack/core/skills/x/SKILL.md", ["pack/**"]) == ["core",
      "skills", "x"]`; met `["pack/core/skills/*/SKILL.md"]` → `["x"]`; `scope_dirs("docs/a.md",
      ["pack/**"]) is None` *(aandachtspunt 5)*: `scope_dirs("pack/notulen.md", ["pack/**"]) == []`
      en `scope_dirs("docs/x.md", ["docs/x.md"]) == []`.
    - `test_scope_markdown_and_toml` (met `code_language_paths = ["pack/**"]`): `pack/a.md` met een
      Nederlandse alinea → vondst `prose`; `pack/notulen.toml` met Nederlandse inhoud → precies één
      vondst, `name` `notulen`; `pack/bestanden/a.toml` → vondst `directory` `bestanden`;
      `docs/a.md` en `docs/notulen.toml` → `.findings == []`; `docs/lees_bestand.py` → vondst
      `name`, zoals #26.
    - `test_scope_binary_only_name` *(aandachtspunt 2)*: `pack/plaatje.png` met bytes `b"\xff\xd8"`
      → één vondst `name` `plaatje`, geen `CannotCheck`; `pack/logo.png` → `.findings == []`.
    - `test_scope_off_without_code_language`: `code_language_paths` zonder `code_language` →
      `pack/a.md` met Nederlands geeft `.findings == []` en `.doubts == []`.
    - `test_out_of_scope_does_not_load_data`: `monkeypatch.setattr(cl.code_language_score,
      "frequencies", boom)` → `check_file` op `docs/a.toml` geeft `.findings == []` en
      `.doubts == []` zonder fout; dezelfde patch op `pack/notulen.toml` binnen het bereik roept
      `boom` wel aan (de stam `notulen` haalt `NAME_MIN`, dus de tabel moet erbij), zodat de test
      de goede module raakt.
    - `test_report_names_path`: een vondst `directory` geeft in `main` dezelfde uitlegregel als een
      modulenaam ("rename the file", `code_language_exempt`).
  - `tests/pack/test_code_language_corpus.py`: `test_pack_markdown_english`: `cl.scan_markdown` over
    `pack/**/*.md` geeft geen vondst.
  - `tests/pack/test_code_language_hook.py`: `test_hook_markdown_in_scope` (Write van `pack/a.md`
    met een Nederlandse alinea in een repo met `code_language_paths = ["pack/**"]` → exit 2,
    `prose:` in stderr) en `test_hook_toml_out_of_scope` (`docs/notulen.toml` → exit 0, lege stderr).
- [ ] **Stap 2:** de vier testbestanden → FAIL.
- [ ] **Stap 3: config.** `"project.code_language_paths": _STRS` in `SCHEMA`, niet in `DEFAULTS`.
- [ ] **Stap 4: `scan_markdown`.** Een regelscanner, geen parser. Frontmatter (bestand begint met
  `---`): per regel `sleutel: waarde` de waarde zonder aanhalingstekens (`>`, `|`, `>-`, `|-` en
  leeg openen een gevouwen waarde), `- x` de `x`, een ingesprongen regel zonder sleutel hoort bij de
  vorige eenheid. Daarna: fenced blokken (```` ``` ```` en `~~~`, tot hetzelfde hek) vallen weg,
  en een ingesprongen codeblok ook (regels met vier spaties of een tab vooraan, na een lege regel en
  niet als vervolg van een lijstitem, tot de eerste niet-ingesprongen regel); een
  kop (`#{1,6} `) is één eenheid; een lijstitem (`[-*+]` of `\d+[.)]`) begint een eenheid; een
  tabelrij (begint met `|`) geeft per cel een eenheid, scheidingsrijen (`^[\s|:-]+$`) vallen weg;
  andere regels horen bij de lopende alinea; een lege regel sluit af. Een regel met `MARKER` valt uit
  zijn eenheid. Per eenheid gaan eerst weg: HTML-commentaar en -tags, `![alt](src)` en
  `[tekst](doel)` worden `alt` en `tekst`, `<http…>`; daarna `judge_text`.
- [ ] **Stap 5: `check_file`.** Volgorde: geen `lighthouse.toml` of geen `code_language` → leeg;
  exempt of manifest → leeg; `dirs = scope_dirs(relative, paths)`. Codebestand (extensie in
  `EXTENSIONS`) → scan zoals nu. Binnen het bereik (`dirs is not None`): `.md`/`.markdown` →
  `scan_markdown`, elk ander bestand → geen inhoud, en elke map uit `dirs` via `judge_name` als
  `directory`. De bestandsnaam (`path.stem`) wordt beoordeeld voor een codebestand en voor elk
  bestand binnen het bereik. Buiten het bereik en geen codebestand → leeg, zonder `frequencies()`
  aan te roepen. Het vaste begin van een glob: de segmenten vóór het eerste segment met `*`, `?` of
  `[`; is er geen joker, dan het hele pad zonder bestandsnaam. Matchen met `fnmatch.fnmatchcase`,
  zoals `code_language_exempt`.
- [ ] **Stap 6: wrapper.** Haal in `code-language.sh` het `case` op `*.py|*.sh|*.bash|*.go` weg;
  pas de kopcommentaar aan (matcher filtert niet meer zelf; bereik via `code_language_paths`).
- [ ] **Stap 7:** de vier testbestanden → PASS; `uv run pytest -q tests/pack/` → PASS.
- [ ] **Stap 8:** `init --upgrade`; commit (`feat(core): bereik code_language_paths, ook Markdown en
  mapnamen (#37)`).

### Taak 5: laag 2 — keten, wandklok, markeerbestand

**Bestanden:**
- Maak: `pack/core/lib/code_language_llm.py`, `tests/pack/_fake_llm.py`
- Wijzig: `pack/core/lib/config.py` (`SCHEMA`), `pack/core/requires.toml`
  (`lighthouse_lib.code_language_llm` in `python3` en `tomllib`: het importeert `config`),
  `pack/SOURCES.toml`,
  `internal/packs/core_test.go`
- Test: `tests/pack/test_code_language_llm.py`, `tests/pack/test_config.py`

**Interfaces:**
- Consumeert: `config.load`, `config.lookup`, `config.ConfigError`.
- Levert in `code_language_llm`:
  - `Chain(NamedTuple)`: `url: str`, `models: tuple[str, ...]`, `key_env: str | None`,
    `env_file: Path | None`.
  - `chain_from(cfg: config.Config) -> Chain | None`: `None` alleen als de tabel
    `code_language_llm` ontbreekt (`"code_language_llm" not in cfg.raw`); een aanwezige tabel, ook
    een lege, zonder `url`, met ontbrekende of lege `models`, of met een `env_file` dat na
    `expanduser` relatief is of niet bestaat → `ConfigError` (spec *Wat de hook meldt*). Zonder
    `key_env` gaat er geen `Authorization`-kop mee: *eigen keuze* (de spec noemt `key_env` niet bij
    `COULD NOT CHECK`; een gratis endpoint zonder sleutel blijft zo bruikbaar).
  - `Reply(NamedTuple)`: `not_english: list[bool] | None` (per item, `None` als de keten faalde),
    `model: str | None`, `failures: list[str]` (`"<model>: <reden>"`, of `"key: <reden>"`,
    of `"skipped: <reden>"`).
  - `ask(chain: Chain, items: list[tuple[str, str]], *, state_dir: Path, per_model: float =
    PER_MODEL_S, total: float = TOTAL_S) -> Reply`; `items` is `(kind, text)`.
  - `build_prompt(items: list[tuple[str, str]]) -> str`, `parse_answer(content: str, n: int) ->
    list[bool]` (`ValueError` als het niet te lezen is).
  - `default_state_dir() -> Path`: `$XDG_RUNTIME_DIR` (of `/tmp`) `/ "lighthouse-code-language"`.
  - Constanten `PER_MODEL_S = 4.0`, `TOTAL_S = 8.0`, `QUIET_S = 60.0`.
- Levert in `tests/pack/_fake_llm.py`: `FakeEndpoint(modes: dict[str, str] | None = None, default:
  str = "answer", verdict: str = "NOT-EN")`, een contextmanager met `.url` (eindigt op `/v1`) en
  `.requests: list[dict]` (de JSON-bodies). Modi per model: `answer` (per genummerd item `verdict`,
  als JSON), `silent` (antwoordt nooit), `drip` (kop, daarna één byte per 0,5 s), `http500`,
  `garbage` (geen JSON). Een `ThreadingHTTPServer` op `127.0.0.1:0` met `daemon_threads = True`.

- [ ] **Stap 1: schrijf de falende tests.**
  - `tests/pack/test_config.py`: `test_code_language_llm_keys` (de vier sleutels worden gelezen;
    `models = "x"` → `ConfigError`; `code_language_llm = 1` → `ConfigError`).
  - `tests/pack/test_code_language_llm.py` (`llm = load_hook_lib("code_language_llm")`):
    - `test_chain_from`: zonder tabel `None`; volledig voorbeeld uit spec *Config* met een bestaand
      `env_file` onder `tmp_path` → `Chain` met `~` uitgeklapt; een lege `[code_language_llm]`,
      zonder `url`, met `models = []`, met `env_file = "rel/env"` en met een niet-bestaand absoluut
      pad → elk `ConfigError`.
    - `test_prompt_and_parse`: `build_prompt([("name", "a"), ("comment", "x\ny")])` bevat de regels
      `1. name: a` en `2. comment: x y`, zegt dat afkortingen en codejargon Engels zijn, en vraagt
      JSON met `EN` of `NOT-EN`. `parse_answer('{"1":"EN","2":"NOT-EN"}', 2) == [False, True]`;
      in een ```` ```json ````-blok en met tekst eromheen idem *(aandachtspunt 3)*;
      `'{"1":"EN"}'` met `n = 2`, `'{"1":"maybe","2":"EN"}'` en `"nee"` → `ValueError`.
    - `test_answer_used`: `FakeEndpoint()` → `ask(...)` geeft `not_english == [True]`, `model` het
      eerste model, en de body heeft `model`, `messages`, `temperature == 0`; de kop is
      `Authorization: Bearer <sleutel>` met de sleutel uit `monkeypatch.setenv`.
    - `test_key_from_env_file`: sleutel niet in de omgeving maar `KEY=abc` in het `env_file` → kop
      `Bearer abc`.
    - `test_missing_key_fails_without_call`: geen sleutel → `not_english is None`, `failures ==
      ["key: …"]` met de naam van de variabele, en `requests == []`.
    - `test_chain_falls_through`: `modes={"m1": "http500", "m2": "answer"}` → `model == "m2"`,
      `failures == ["m1: HTTP 500"]`.
    - `test_every_failure_kind`: per modus `silent`, `drip`, `http500`, `garbage`, en een url naar
      `http://lighthouse-test.invalid/v1`: één model, `per_model=0.5`, `total=1.0` →
      `not_english is None`, één reden, en `ask` keert terug binnen `1.5` s.
    - `test_total_deadline`: drie modellen `silent`, `per_model=0.5`, `total=0.8` → binnen `1.2` s
      terug; het derde model staat in `failures` met de reden dat de totale tijd op is.
    - `test_quiet_period`: na een gefaalde keten slaat een tweede `ask` met dezelfde `state_dir` de
      endpoint over (`requests` groeit niet; `failures == ["skipped: …"]`); met een andere `url`
      niet; na een geslaagde aanroep is het markeerbestand weg.
    - `test_unusable_marker_is_ignored` *(aandachtspunt 4)*: `state_dir` is een bestand (niet te
      maken als map) en, apart, het markeerbestand bevat `"x"` → `ask` vraagt de endpoint gewoon en
      gooit niets.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_code_language_llm.py tests/pack/test_config.py`
  → FAIL.
- [ ] **Stap 3: config.** `"code_language_llm.url": _STR`, `"code_language_llm.models": _STRS`,
  `"code_language_llm.key_env": _STR`, `"code_language_llm.env_file": _STR` in `SCHEMA`.
- [ ] **Stap 4: `code_language_llm.py`.** Sleutel: `os.environ[key_env]`, anders de regel
  `key_env=…` uit `env_file` (`#` en lege regels overslaan, aanhalingstekens om de waarde weg).
  Elke modelaanroep draait in een `threading.Thread(daemon=True)` die `urllib.request.urlopen` doet
  met `timeout=per_model`; de hoofdthread wacht met `join(min(per_model, wat er van total over is))`
  en laat een levende thread los: reden `no answer within <t> s`. Redenen verder `HTTP <code>`,
  `unreadable answer`, en de tekst van een `URLError`. Body: `{"model", "messages": [{"role":
  "user", "content": prompt}], "temperature": 0, "max_tokens": 20 + 12 * len(items)}`; inhoud uit
  `choices[0].message.content`. Markeerbestand: `state_dir / f"layer2-failed-{sha1(url)[:12]}"` met
  het tijdstip van falen; jonger dan `QUIET_S` → `skipped: the chain failed <n> s ago`. Elke
  `OSError` of `ValueError` rond het markeerbestand wordt genegeerd.
- [ ] **Stap 5:** dezelfde pytest-run → PASS.
- [ ] **Stap 6:** registreer de module (`SOURCES.toml`, `core_test.go`, `requires.toml`),
  `init --upgrade`, `go test ./internal/packs/` → PASS; commit
  (`feat(core): laag 2 vraagt een keten modellen met harde time-out (#37)`).

### Taak 6: laag 2 in de hook, melding, en het Bash-pad

**Bestanden:**
- Wijzig: `pack/core/lib/code_language.py` (`Inspection.chain`, `resolve`, `main`),
  `pack/core/hooks/code-language.sh` (rc 0 met uitvoer), `pack/core/lib/deadline.py`
  (`run_until(..., env=...)`), `pack/core/lib/bash_writes.py` (`UNDER_GATES_ENV`, `run_gate`,
  `_report_verdict`)
- Test: `tests/pack/test_code_language.py`, `tests/pack/test_code_language_hook.py`,
  `tests/pack/test_bash_writes.py`, `tests/pack/test_deadline.py`

**Interfaces:**
- Consumeert: `code_language_llm.chain_from`, `ask`, `default_state_dir`, `Reply` (taak 5);
  `FakeEndpoint` (taak 5).
- Levert:
  - `Inspection` krijgt `chain: code_language_llm.Chain | None = None`; `check_file` vult het via
    `chain_from` (ook zonder twijfelgevallen, zodat een foute tabel meteen `COULD NOT CHECK` is).
  - `code_language.resolve(inspection: Inspection, path: Path, *, under_gates: bool, state_dir:
    Path) -> tuple[list[Finding], str | None]`: alle vondsten (laag 1 plus `NOT-EN`-twijfel met
    `layer = f"layer 2 ({model})"`) en de melding, of `None`.
  - `code_language.UNDER_GATES_ENV` en `bash_writes.UNDER_GATES_ENV`, beide
    `"LIGHTHOUSE_UNDER_BASH_GATES"`.
  - CLI-contract `code_language.py <abs-path> <root>`: rc 0 en lege stdout = schoon; rc 0 met stdout
    = alleen een melding; rc 1 = vondsten (met de melding eronder als die er is); rc 2 = kon niet
    controleren.
  - `deadline.run_until(..., env: Mapping[str, str] | None = None)`: doorgegeven aan het kind;
    `None` = erven zoals nu.

- [ ] **Stap 1: schrijf de falende tests.**
  - `tests/pack/test_code_language.py`, met `FakeEndpoint` en een repo met `[code_language_llm]`
    (één model `m1`, `key_env` gezet via `monkeypatch.setenv`), bestand `a.py` met regel 1
    `lees_bestand = 1` (vondst laag 1) en regel 2 `legacy_processed_lijst_weg = 2` (twijfel):
    - `test_not_en_becomes_finding`: `verdict="NOT-EN"` → vondsten op regel 1 (`layer 1`) en 2
      (`layer 2 (m1)`), melding `None`; de prompt in `requests[0]` bevat
      `legacy_processed_lijst_weg` en niet `lees_bestand` (besluit 10).
    - `test_en_drops_doubt`: `verdict="EN"` → alleen regel 1.
    - `test_layer1_only_no_call`: een bestand met alleen `lees_bestand = 1` → `requests == []`.
    - `test_no_chain_is_silent`: zonder tabel → vondst regel 1, melding `None`, geen aanroep.
    - `test_failed_chain_notice`: `default="http500"` → vondsten alleen laag 1, en de melding is
      precies
      `code-language: layer 2 did not answer (m1: HTTP 500); 1 doubtful units not checked:\n<path>:2: name: legacy_processed_lijst_weg`.
      Parametrisch ook `default="garbage"` (reden `m1: unreadable answer`) en zonder sleutel in
      omgeving en `env_file` (reden begint met `key: `), elk met dezelfde vondsten en de
      twijfelregel in de melding (spec *Bewijs*, laag 2).
    - `test_under_gates_skips`: `under_gates=True` → `requests == []` en de melding begint met
      `code-language: NOTICE — layer 2 did not answer (layer 2 skipped under bash-write-gates); 1 doubtful units not checked:`.
    - `test_bad_chain_is_cannot_check`: `models = []` → `pytest.raises(cl.CannotCheck)` uit
      `check_file`, ook voor een bestand zonder twijfel.
  - `tests/pack/test_code_language_hook.py` (subprocess, `XDG_RUNTIME_DIR` per test op een eigen
    `tmp_path`, één model):
    - `test_hook_notice_without_findings`: alleen de twijfelregel, endpoint `http500` → exit 0,
      stdout is JSON met `systemMessage` en `hookSpecificOutput.additionalContext` (`hookEventName
      == "PostToolUse"`) die allebei `legacy_processed_lijst_weg` bevatten.
    - `test_hook_notice_with_findings`: beide regels, `http500` → exit 2, stderr bevat de vondst
      en de meldingsregel `layer 2 did not answer`.
    - `test_hook_ends_within_deadline`: parametrisch over `silent`, `drip` en
      `http://lighthouse-test.invalid/v1` → exit 0 binnen `10` s wandklok, melding in stdout.
    - `test_hook_under_gates`: met `LIGHTHOUSE_UNDER_BASH_GATES=1` → `requests == []`, exit 0, en
      stdout is platte tekst die met `code-language: NOTICE — ` begint.
  - `tests/pack/test_deadline.py`: `test_run_until_env`: een kind `sh -c 'echo $X'` met
    `env={**os.environ, "X": "y"}` en `capture=True` geeft `b"y\n"`; `os.environ` heeft geen `X`.
  - `tests/pack/test_bash_writes.py`:
    - `test_run_gate_sets_under_gates_env`: stub-gate `echo "$LIGHTHOUSE_UNDER_BASH_GATES"` →
      output `b"1\n"`, en `"LIGHTHOUSE_UNDER_BASH_GATES" not in os.environ` daarna.
    - `test_notice_on_rc0_is_passed_on`: `fake_gate` geeft `("ok", b"code-language: NOTICE — x\na.py:2: name: y")`
      → `run_post` exit 0, lege stderr, en zowel `additionalContext` als `systemMessage` in de
      stdout-JSON bevatten `== x.py (code-language.sh) ==` en `NOTICE — x` (besluit 11: gebruiker
      én agent).
    - `test_plain_note_has_no_system_message`: een gewone `outcome.note` (de melding van een
      ontbrekend taalpakket) geeft nog steeds alleen `additionalContext`, geen `systemMessage`.
    - `test_chatter_on_rc0_stays_silent`: `("ok", b"formatted 1 file")` → `run_post` geeft
      `(0, "", "")`.
    - `test_under_gates_env_matches`: `bw.UNDER_GATES_ENV == cl.UNDER_GATES_ENV`.
- [ ] **Stap 2:** de vier testbestanden → FAIL.
- [ ] **Stap 3: `resolve` en `main`.** Geen twijfel of geen `chain` → laag 1 en `None`. `under_gates`
  → reden `layer 2 skipped under bash-write-gates`, prefix `code-language: NOTICE — `. Anders
  `ask(chain, [(d.kind, d.unit or d.text) for d in doubts], state_dir=state_dir)`; bij
  `not_english is None` de melding met `"; ".join(failures)`. Meldingsvorm (spec *Wat de hook
  meldt*): kopregel `code-language: [NOTICE — ]layer 2 did not answer (<redenen>); <n> doubtful
  units not checked:`, dan per twijfelgeval `<path>:<regel>: <soort>: <tekst>`, tekst afgekapt zoals
  in `_report`. `main` leest `UNDER_GATES_ENV` en `default_state_dir()`, en volgt het CLI-contract.
- [ ] **Stap 4: wrapper.** Bij rc 0 met niet-lege `OUTPUT`: met `LIGHTHOUSE_UNDER_BASH_GATES` gezet
  `printf '%s\n' "$OUTPUT"`, anders
  `jq -cn --arg m "$OUTPUT" '{systemMessage: $m, hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $m}}'`;
  daarna exit 0. Rc 1 blijft stderr + exit 2.
- [ ] **Stap 5: Bash-pad.** `run_until` geeft `env` door aan `subprocess.Popen`. `run_gate` geeft
  `env={**os.environ, UNDER_GATES_ENV: "1"}` mee. `_report_verdict`: bij `verdict == "ok"` en
  `_NOTICE = "NOTICE"` in de eerste uitvoerregel → `outcome.note("== <label> (<gate>) ==\n" + tekst,
  notice=True)`; anders blijft `ok` stil. `_Outcome.note(text, notice: bool = False)` onthoudt de
  notice-delen, en `render` zet die delen bij exit 0 én exit 2 ook in een topniveau-`systemMessage`
  naast `hookSpecificOutput`, zodat de gebruiker ze ziet (besluit 11, via `outcome.note` van
  besluit 19). `classify` verandert niet.
- [ ] **Stap 6:** de vier testbestanden → PASS; `uv run pytest -q` → PASS.
- [ ] **Stap 7:** `init --upgrade`; commit (`feat(core): laag 2 in de hook, zichtbaar bij falen,
  overgeslagen onder bash-write-gates (#37)`).

### Taak 7: de regel in het instructieblok

**Bestanden:**
- Wijzig: `pack/core/instructions/AGENTS.md`, `tests/test_instruction_files.py`
- Gevolg van `init --upgrade`: het beheerde blok in `AGENTS.md`

**Interfaces:**
- Consumeert: niets uit eerdere taken behalve de sleutelnamen `code_language` en
  `code_language_paths`.

- [ ] **Stap 1: laag bevestigen.** `/lighthouse-instrument` heeft `disable-model-invocation: true`
  (`pack/core/skills/instrument/SKILL.md:4`): de uitvoerder kan het niet aanroepen. Lees in plaats
  daarvan `.claude/skills/lighthouse-instrument/SKILL.md` en loop de beslisprocedure voor de laag
  zelf door met de regel van besluit 16, zonder stappen die het netwerk gebruiken (`npx skills
  find`). Zet de redenering in twee à drie zinnen in het commitbericht, met de vermelding dat de
  skill gelezen en niet gedraaid is. Wijst de procedure een andere laag aan dan het beheerde blok
  van core, stop en leg het voor aan de gebruiker.
- [ ] **Stap 2: schrijf de falende test.** In `test_core_block_word_budget`: tokens
  `"[project] code_language"` en `"code_language_paths"` erbij; het budget gaat van 260 naar 290
  woorden, en de docstring noemt de regel voor de taal van code (het blok staat nu op 255).
- [ ] **Stap 3:** `uv run pytest -q tests/test_instruction_files.py` → FAIL.
- [ ] **Stap 4:** voeg na de regel over artefacten toe:
  `- With \`[project] code_language\` set, names, comments, docstrings and file names are English, and so is everything under \`code_language_paths\`; artefacts stay in \`[project] language\`.`
- [ ] **Stap 5:** `init --upgrade`; `uv run pytest -q tests/test_instruction_files.py` → PASS;
  tweede `init --upgrade` → `No changes.`
- [ ] **Stap 6: commit** (`docs(core): regel voor de taal van code in het instructieblok (#37)`).

### Taak 8: eindcontrole

- [ ] **Stap 1:** `uv run pytest -q && go test ./...` → PASS.
- [ ] **Stap 2:** `go run ./cmd/lighthouse init --upgrade` → `No changes.`;
  `ci/literal-grep.sh` → rc 0.
- [ ] **Stap 3:** `grep -n "code_language" lighthouse.toml` → niets (besluit 17).
- [ ] **Stap 4:** loop *Bewijs* van de spec langs en wijs per punt de test aan; een punt zonder test
  is een gat: terug naar de taak die het hoort te dekken.
