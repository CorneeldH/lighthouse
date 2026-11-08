# code-language-check in Go: implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen
> gebruiken checkboxes (`- [ ]`).

**Doel:** stap 3 van de milestone *Lighthouse zonder Python* (#276). De hook wordt
`lighthouse hook code-language-check`, met de frequentietabel, de scanners en de LLM-aanroep in
Go. Weg: `code_language.py`, `code_language_llm.py`, `code_language_score.py` en
`code-language-check.sh`.

**Architectuur:** één nieuw pakket `internal/codelang` met vier delen. De scorer (laag 1) met de
woordfrequenties via `go:embed`. De scanners per taal, die eenheden opleveren (`Unit`: regel,
soort, tekst). `Check` past scope, vrijstellingen en manifest toe. De keten (laag 2) praat
via `net/http` met een deadline per model en voor de hele keten. Het pakket registreert de hook
zelf (`hook.Register` in `init`), en `internal/cli` importeert het blanco. Dit is de eerste echte
hook op de runtime van #274. Daarom krijgt `hook.Result` hier een veld voor platte stdout, en
slaat `hook.Run` de meldingen over bij een geneste aanroep onder bash-write-check.

**Tech stack:** Go (`regexp`, `compress/gzip`, `net/http`, `go:embed`), pytest voor de
black-box-tests via `run.sh` en voor de overgang van de fixtures.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md):
§1, §2, §9, §10, *Anti-doelen* en *Opdeling* stap 3.

## Globale eisen

- Hetzelfde oordeel en dezelfde tekst als de bash-hook plus `code_language.py`, op dezelfde
  payloads (spec §2). Exitcodes: 0 schoon of alleen een melding, 2 bij bevindingen of bij
  `COULD NOT CHECK`.
- Twee vormen van `COULD NOT CHECK`, letterlijk zoals nu:
  - **Hookniveau** (slechte JSON, een pad dat niet op te lossen is, een git-fout): het blok van
    `cannot_check()` uit `code-language-check.sh:34-45`. Dat is de eerste regel
    `code-language-check: COULD NOT CHECK — <reden>`, een lege regel,
    `This hook checks that code is written in English.`,
    `Nothing was checked; that is not a green light.` en, bij een detail, een lege regel plus het
    detail.
  - **Checkerniveau** (config, manifest, UTF-8, Python-syntaxis, data, de keten): één regel
    `code-language-check: COULD NOT CHECK — <reden>`.
- Bevindingen gaan naar stderr: het rapport van `_report` (`code_language.py:707-730`). Bij een
  melding volgt een lege regel en dan de melding. Alleen een melding gaat naar stdout als JSON
  `{"systemMessage": m, "hookSpecificOutput": {"hookEventName": "PostToolUse", "additionalContext": m}}`.
  Onder `LIGHTHOUSE_UNDER_BASH_WRITE_CHECK` is het platte tekst met een `\n` erachter.
- Constanten letterlijk uit Python: `THRESHOLD = 1.0`, `COMPOUND_SCORE = 3.0`, `PART_MIN = 3`,
  `PART_NL_FLOOR = 2.5`, `DOUBT_LEAN = 2.0`, `DOUBT_NL = 3.0`, `NAME_MIN = 4`, `SPLIT_MAX = 40`,
  `TEXT_MIN = 2`, `_MAX_LINES = 20`, `_TEXT_LIMIT = 60` (in runes, niet in bytes),
  `PER_MODEL_S = 4s`, `TOTAL_S = 8s`, `QUIET_S = 60s`, `MAX_TOKENS_FLOOR = 512`, de marker
  `lighthouse: allow-language`.
- Bevroren: niets in deze stap. `bash_writes.py` is nog niet overgezet en mag dus veranderen.
  Het verandert alleen in hoe het de gate start (taak 8).
- Python-regex naar RE2: `\w` en `[^\W\d_]` zijn in Python Unicode. Schrijf ze uit als
  `[\p{L}\p{N}_]` en `[\p{L}\p{Nl}\p{No}]`. Lookbehind en lookahead bestaan niet in RE2.
  Schrijf ze met de hand uit (taak 2 en 3 noemen waar).
- `str.splitlines()` splitst ook op `\r`, `\r\n`, `\v`, `\f`, `\x1c`–`\x1e`, `\x85`, ` ` en
  ` `. Daarvoor komt één helper `splitLines(s string) []string` in `internal/codelang`. Waar
  Python `"\n"` telt (`_line_of`, `source.count("\n")`), telt Go ook alleen `\n`.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Code, comments en
  meldingen zijn Engels. Commits, PR en issue-comments zijn Nederlands.
- Elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie. CI eist `No changes.`
- Changelog: `changelog.d/issue-276-code-language-check-go.md` met `type: changed` (minor,
  spec §10).

## Review-focus

1. **Python dat wel tokeniseert maar grammaticaal fout is** (`x = = 1`). Python gaf
   `COULD NOT CHECK` via `ast.parse`. Go heeft geen volledige parser en vangt alleen lexerfouten:
   een open string, een open haakje op EOF, een losse sluiter, een NUL-byte en een inconsistente
   dedent. De rest scant hij gewoon. **Dit is een bewuste afwijking.** Hij staat in de PR-body, en
   de test `TestPythonGrammarErrorIsScanned` (taak 4) legt hem vast, zodat de afwijking zichtbaar
   blijft en niet stil groeit.
2. **CRLF en `\r` in een bestand.** De regelnummers moeten gelijk blijven aan die van Python. →
   scan-fixtures `python_crlf`, `shell_crlf` en `go_crlf` (taak 1 neemt ze op vanuit Python, en
   taak 3 en 4 laten ze slagen).
3. **Unicode in namen en tekst** (`naïef_bestand`, `İ`, `½`). Python's `\w`, `[^\W\d_]` en
   `str.lower()` tegenover RE2 en `strings.ToLower`. → fixtures `unicode_name` en `unicode_prose`
   in `score.json` (taak 1 en 2).
4. **Een f-string en een geneste quote in Python 3.12** (`f"{x["a"]}"`, `f"{y!r:>{w}}"`). Die
   mogen geen `COULD NOT CHECK` geven: de repo heeft er zulke. → scan-fixture
   `python_fstring_nested` (taak 1 en 4). Daarnaast de pariteitsrun over de hele repo (taak 5).
5. **Een hookaanroep onder bash-write-check met een versie- of buildmelding die openstaat.** Er
   mag geen JSON tussen de platte tekst komen. → `TestRunNestedSkipsNotices` (taak 7).

---

### Taak 1: inline gevallen naar gedeelde fixtures (nog tegen Python)

Spec §9: eerst naar fixtures, dan overzetten. Deze taak verandert geen productiecode. Pytest leest
de nieuwe fixtures tegen de Python-modules en blijft groen. Dat bewijst dat de fixtures de oude
verwachtingen zijn.

**Files:**
- Create: `tests/pack/fixtures/code_language/scan.json`, `score.json`, `score_table.json`,
  `scope_dirs.json`, `check.json`, `llm.json`, `units/<naam>.json`, `corpus/` (een bevroren
  kopie, zie stap 4)
- Modify: `tests/pack/test_code_language.py`, `test_code_language_score.py`,
  `test_code_language_llm.py`

**Formaten** (alle JSON, UTF-8, gesorteerde sleutels, twee spaties inspringing):

- `scan.json`: `[{"name", "lang": "python|shell|go|markdown", "source", "want": [[line, kind, text, doubt]]}]`.
  In plaats van `want` kan er `"cannot_check": true` staan. `want` is gesorteerd op regel, zoals
  Python dat doet.
- `score.json`:
  `{"split_name": {"<naam>": [...]}, "names": {"<naam>": "finding|doubt|clean"}, "texts": [{"text", "line_breaks", "want"}], "dutch_split": {"<woord>": bool}, "dutch_participle": {"<woord>": bool}}`.
  Dit is tegen de echte tabel.
- `score_table.json`: `{"table": {"<woord>": [en, nl]}, "jargon": [...], "cases": [{"fn": "judge_name|judge_text|word_score|dutch_split", "arg", "line_breaks"?, "want"}]}`.
  Dit zijn de gevallen die nu de fixture `table` (monkeypatch) gebruiken.
- `scope_dirs.json`: `[{"relative", "globs", "want": [...] | null}]`.
- `check.json`:
  `[{"name", "toml_extra", "files": {"<rel>": "<tekst>"}, "files_b64"?: {...}, "session_table"?, "path", "findings": [[line, kind, text]], "doubts": [[line, kind, text]], "cannot_check"?: true}]`.
  Een lezer zet de bestanden in een tijdelijke map. `toml_extra` komt achter `code_language = "en"`
  in `MINIMAL_TOML`. Zonder `"code_language"` in `toml_extra` mag de sleutel `"off": true` hem
  weglaten.
- `llm.json`: `{"prompt": [{"items": [[kind, text]], "want"}], "parse": [{"content", "n", "want": [bool] | null}]}`.
  `null` betekent `ValueError`.
- `units/<naam>.json`: `{"source": "<pad onder fixtures/code_language>", "lang", "units": [[line, kind, text, verdict]]}`.
  Dit is **elke** eenheid die de scanner aan `judge_name` en `judge_text` gaf, de schone
  inbegrepen, gesorteerd op `(line, kind, text)`.

- [ ] **Stap 1: zet de inline gevallen om.** Doe dit per testfunctie in `test_code_language.py`,
  `test_code_language_score.py` en `test_code_language_llm.py`. Is een geval data (bron →
  uitkomst), dan gaat het naar de fixture hierboven, en de test wordt een lezer met
  `@pytest.mark.parametrize` over die fixture. De verwachtingen kopieer je letterlijk uit de
  asserts. Waar een test `doubt` niet assert, vul je `doubt` één keer in met de uitkomst van
  Python en controleer je hem met de hand. Gedragsgevallen blijven inline staan: timing, HTTP,
  bestandsrechten, `monkeypatch` op `FREQ_FILE`, markerbestanden en `main()` via `capsys`. Die
  gaan in taak 2, 5, 6 en 7 als Go-test mee. De afbeelding staat in de tabel onder deze taak.
- [ ] **Stap 2: voeg de review-focusgevallen toe.** In `scan.json` komen `python_crlf`,
  `shell_crlf`, `go_crlf` (een bron met `\r\n` en een Nederlandse comment op regel 3) en
  `python_fstring_nested` (`x = f"{d["a"]}"\n# lees het bestand\n`). In `score.json` komen
  `unicode_name` (`naïef_bestand`) en `unicode_prose` (`Dit is İstanbul ½ niet goed.`). De
  verwachting is wat Python nu geeft, één keer met de hand nagekeken.
- [ ] **Stap 3: draai pytest.**
  `uv run pytest -q tests/pack/test_code_language.py tests/pack/test_code_language_score.py tests/pack/test_code_language_llm.py`.
  Verwacht: alles groen. Het aantal testgevallen is minstens zo groot als vóór de omzetting.
  Noteer beide aantallen in de commit.
- [ ] **Stap 4: leg de eenheden van het corpus vast.** Kopieer drie bestanden naar
  `tests/pack/fixtures/code_language/corpus/`: `pack/core/hooks/code-language-check.sh` als
  `hook.sh`, `internal/hook/hook.go` als `hook.go.txt` (met `.txt`, zodat `go build` en
  golangci-lint hem niet zien) en `pack/core/skills/start-issue/SKILL.md` als `skill.md`. Draai
  dan één keer dit script uit de scratchpad (niet committen; het importeert modules die taak 9
  weghaalt):

  ```python
  # dump_units.py <out-dir> <file>...: every unit the scanner judges, with its verdict.
  # Run from the repo root: uv run python dump_units.py <out-dir> <file>...
  import json, sys
  from pathlib import Path
  from tests.pack._testkit import load_hook_lib
  cl = load_hook_lib("code_language")
  score = cl.code_language_score
  LANG = {".py": ("python", cl.scan_python), ".sh": ("shell", cl.scan_shell),
          ".txt": ("go", cl.scan_go), ".md": ("markdown", cl.scan_markdown)}
  out = Path(sys.argv[1])
  for name in sys.argv[2:]:
      path = Path(name)
      lang, scan = LANG[path.suffix]
      units, real_name, real_text = [], score.judge_name, score.judge_text
      score.judge_name = lambda n: units.append(["name", n, v := real_name(n)]) or v
      score.judge_text = lambda t, **k: units.append(["text", t, v := real_text(t, **k)]) or v
      try:
          found = scan(path.read_text(encoding="utf-8"))
      finally:
          score.judge_name, score.judge_text = real_name, real_text
      # The scanner's own line per unit comes from the findings; for clean units the
      # Go test compares the multiset of (kind, text, verdict), see Task 5.
      (out / f"{path.stem}.json").write_text(json.dumps(
          {"source": str(path), "lang": lang,
           "units": sorted(units)}, ensure_ascii=False, indent=2, sort_keys=True) + "\n")
  ```

  Draai het op de tien bestanden van `stdlib/*.py`, op `bash_writes_nl.py` en op de drie
  corpus-bestanden. De uitvoer gaat naar `tests/pack/fixtures/code_language/units/`. Met
  `units` gaat het dus om de multiset `[soort-aanroep, tekst, oordeel]`. Regelnummers staan al
  in `scan.json`, en de bevindingen met regel zie je in `want`.
- [ ] **Stap 5: commit.**
  `test(code-language): inline gevallen naar gedeelde fixtures (#276)`.

| Blijft gedrag, wordt Go-test | Taak |
|---|---|
| `test_missing_data_is_cannot_check`, `test_corrupt_data_is_data_error`, `test_missing_data_is_data_error`, `test_out_of_scope_does_not_load_data` | 2, 5 |
| alle tests in `test_code_language_llm.py` behalve prompt en parse | 6 |
| `test_finding_layer`, `test_main_output`, `test_main_notice_contract`, `test_report_*`, `test_not_en_becomes_finding` t/m `test_bad_chain_spares_unchecked_files` | 7 |
| `test_code_language_corpus.py` | 5 |

### Taak 2: de scorer (laag 1)

**Files:**
- Create: `internal/codelang/score.go`, `internal/codelang/score_test.go`,
  `internal/codelang/lines.go`, `internal/codelang/data/freq.tsv.gz`,
  `internal/codelang/data/jargon.txt`, `internal/codelang/data/NOTICE.txt` (gekopieerd met `cp`
  uit `pack/core/lib/`; taak 9 haalt de originelen weg)

**Interfaces:**
- Produces:
  - `type Verdict string` met `Clean`, `Doubt` en `Finding` (`"clean"`, `"doubt"`, `"finding"`).
  - `type Scorer struct` (niet-geëxporteerde velden `freq map[string][2]int`, `jargon map[string]bool`).
  - `func NewScorer(freq map[string][2]int, jargon []string) *Scorer` en
    `func DefaultScorer() (*Scorer, error)`. De laatste laadt de ingebedde data één keer met
    `sync.OnceValues`. Een fout is `*CannotCheckError` met
    `cannot read code_language_freq.tsv.gz: <err>`. De oude bestandsnaam staat in de tekst,
    zodat de melding gelijk blijft.
  - `(*Scorer) JudgeName(name string) Verdict`, `(*Scorer) JudgeText(text string, lineBreaks bool) Verdict`,
    `(*Scorer) WordScore(w string) float64`, `(*Scorer) DutchSplit(w string) bool` en
    `(*Scorer) DutchParticiple(w string) bool`.
  - `func SplitName(name string) []string` en `func Words(text string) []string`.
  - `type CannotCheckError struct{ Reason string }`, met `Error()` dat `Reason` geeft.
  - `func splitLines(s string) []string`, de semantiek van `str.splitlines()`.

- [ ] **Stap 1: schrijf de tests.** `TestScoreFixture` leest `score.json` met de echte data.
  `TestScoreTableFixture` leest `score_table.json` met `NewScorer`. `TestSplitLines` is een
  tabel met de scheidingstekens uit *Globale eisen* en `"a\n"` → `["a"]`. `TestLoadCorruptData`
  test `loadFreq(io.Reader)` op een kapotte gzip-stroom en een regel met twee velden; beide
  geven `*CannotCheckError`.
- [ ] **Stap 2: draai** `go test -run 'TestScore|TestSplitLines|TestLoad' ./internal/codelang/`.
  Verwacht: FAIL (`undefined`).
- [ ] **Stap 3: implementeer.** Drie dingen schrijf je met de hand uit, want RE2 kan ze niet:
  - `SplitName` (`[A-Z]+(?![a-z])|[A-Z]?[a-z]+`): op een hoofdletter neem je de langste reeks
    hoofdletters. Volgt er een kleine letter en is de reeks langer dan één, dan geef je de reeks
    min de laatste letter en ga je verder vanaf die letter. Is de reeks één letter lang, dan neem
    je de hoofdletter plus de reeks kleine letters. Op een kleine letter neem je de reeks kleine
    letters. Al het andere sla je over. Alleen ASCII, en alles in kleine letters.
  - Het splitsen in zinnen (`(?<=[.!?])(?<!\.[^\W\d_]\.)\s+`, met `|\n` bij `lineBreaks`): splits
    op een reeks Unicode-witruimte die begint direct na `.`, `!` of `?`. Doe dat niet als de drie
    tekens ervoor `.`, een letter en `.` zijn. Met `lineBreaks` splitst ook elke `\n`.
  - `DutchSplit`: dezelfde recursie met memo als Python, op runes en niet op bytes.

  `Words` is `[\p{L}\p{Nl}\p{No}]+` over `strings.ToLower(text)`. Laat `unicode_prose` beslissen
  of `strings.ToLower` volstaat. Zo niet, dan gebruik je een map met de uitzonderingen die Python
  anders doet: `İ` → `i̇`.
- [ ] **Stap 4: draai** de tests uit stap 2 en `go test -race ./internal/codelang/`. Verwacht:
  PASS.
- [ ] **Stap 5: commit.** `feat(codelang): scorer van laag 1 in Go (#276)`.

### Taak 3: scanners voor shell, Go en Markdown

**Files:**
- Create: `internal/codelang/unit.go`, `scan_shell.go`, `scan_go.go`, `scan_markdown.go`,
  `scan_test.go`

**Interfaces:**
- Consumes: `Scorer` en `splitLines` (taak 2).
- Produces:
  - `type Unit struct{ Line int; Kind, Text, Body string; Name bool; LineBreaks bool }`. `Kind`
    is `comment`, `docstring`, `prose`, `name` of `directory`. `Text` is de naam of de eerste
    regel. `Body` is de hele eenheid voor `JudgeText` en laag 2. `Name` kiest `JudgeName`.
  - `type Finding struct{ Line int; Kind, Text, Body, Layer string; Doubt bool }`. `Body` is
    `Unit.Body`, wat laag 2 te zien krijgt (`d.unit or d.text` in Python).
  - Een naam op een regel met de marker wordt geen `Unit`. Python roept `judge_name` daar niet
    aan, en `TestUnitsFixture` (taak 5) telt de aanroepen.
  - `func scanShell(src string) []Unit`, `func scanGo(src string) []Unit` en
    `func scanMarkdown(src string) []Unit`. Ze geven eenheden met de marker al weggelaten, net als
    Python.
  - `func judge(s *Scorer, units []Unit) []Finding`, gesorteerd stabiel op regel, zoals Python's
    `sorted(key=line)`.
  - `func Scan(s *Scorer, lang, src string) ([]Finding, error)`, met `lang` als `python`, `shell`,
    `go` of `markdown`.

- [ ] **Stap 1: schrijf** `TestScanFixture` over `scan.json` (alleen `lang` ≠ `python` in deze
  taak; taak 4 schakelt Python in). Hij vergelijkt `[[line, kind, text, doubt]]` letterlijk.
- [ ] **Stap 2: draai** `go test -run TestScanFixture ./internal/codelang/`. Verwacht: FAIL.
- [ ] **Stap 3: port** `_scan_shell_text`, `scan_shell`, `_scan_go_text`, `_go_names`, `scan_go`,
  `_frontmatter_units` en `scan_markdown`, regel voor regel. Drie dingen schrijf je met de hand
  uit:
  - `_GO_DEFINE` (`(?<![\w.])`): controleer de rune vóór de match zelf.
  - `_SHELL_DECLARED` (`(?=[ \t=\n]|$)`): controleer de rune ná de naam zelf.
  - `_HTML_COMMENT` wordt `(?s)<!--.*?-->`. RE2 kent lazy matching.

  `_IDENT` wordt `[A-Za-z_][\p{L}\p{N}_]*`. Offsets blijven byte-offsets in Go, en het tellen
  van `\n` blijft daarmee gelijk. Snijd tekst alleen op runegrenzen.
- [ ] **Stap 4: draai** de test. Verwacht: PASS.
- [ ] **Stap 5: commit.** `feat(codelang): scanners voor shell, Go en Markdown (#276)`.

### Taak 4: Python-lexer en -scanner

**Files:**
- Create: `internal/codelang/pylex.go`, `internal/codelang/scan_python.go`,
  `internal/codelang/pylex_test.go`
- Modify: `internal/codelang/scan_test.go` (Python in `TestScanFixture`)

**Interfaces:**
- Produces:
  - `func scanPython(src string) ([]Unit, error)`. De fout is `*CannotCheckError` met
    `cannot parse Python: <reden>`.
  - Intern: `type pyToken struct{ kind pyKind; text string; line, endLine int; depth int }`, met
    de soorten `name`, `op`, `str`, `fstr`, `comment`, `newline`, `nl`, `indent`, `dedent`,
    `number` en `eof`.

**Wat de lexer doet** (de tokenize-regels die dit plan nodig heeft):
- Strings met de prefixen `r`, `u`, `b`, `f`, `rb`, `br`, `fr` en `rf`, in elke combinatie van
  hoofd- en kleine letters. Enkel, `'''` en `"""`, en een backslash-continuatie binnen een string.
  Een f-string volgt de regels van 3.12: binnen `{…}` staan expressies, met geneste quotes en een
  eigen haakjesdiepte. `{{` is letterlijk.
- Een comment loopt van `#` buiten een string tot het einde van de regel. Een regel die eindigt
  op `\` loopt door.
- Haakjesdiepte telt over `([{`. Binnen haakjes komt geen `newline`, `indent` of `dedent`.
- Fouten worden `*CannotCheckError`: een NUL-byte, een niet-afgesloten string of triple-string,
  EOF met open haakjes (`EOF in multi-line statement`), een losse of verkeerde sluiter, een
  dedent naar een niveau dat nooit bestond, en een `\` aan het einde van het bestand.

**Wat de scanner opneemt** (gelijk aan `_name_nodes`, `ast.get_docstring` en
`_comment_findings`). Lees per logische regel op diepte 0:
- `def`/`async def NAME`, en elke parameternaam tussen de haakjes van de def: na `(`, `,`, `*`,
  `**` of `/`, op diepte 1 van die haakjes, als hij gevolgd wordt door `:`, `=`, `,` of `)`.
  `lambda`-parameters tot de `:` op dezelfde diepte.
- `class NAME`.
- Toewijzingsdoelen. Elk segment vóór een `=` op diepte 0 van de regel, het doel van een
  augmented assignment (`+=` enz.) en `NAME :` van een geannoteerde toewijzing (ook zonder
  waarde). Binnen een doel tellen: een kale `NAME` die niet gevolgd wordt door `.`, `(` of `[`
  (inclusief in tuple- en lijstvormen en na `*`), en het laatste attribuut van een keten
  `a.b.NAME` die direct vóór de `=` eindigt. Voor dat attribuut telt de regel waarop het
  attribuut eindigt.
- `for TARGET in`, ook in comprehensions op elke diepte en in `async for`. `with … as TARGET`.
  `except … as NAME`. `import x as NAME` en `from x import y as NAME`. `NAME :=` op elke diepte.
- Niet: keyword-argumenten in een aanroep, `global`, `nonlocal`, `del`, `import x` zonder `as`,
  capture-namen in `match`/`case` en typeparameters (`def f[T]`). In `ast` zijn dat geen
  `Name(Store)`.
- Docstring: is het eerste statement van de module, een `class` of een `def` een
  expressiestatement van alleen string-literals (geen `f` en geen `b`; samengevoegde literals
  tellen samen), dan geldt de gedecodeerde waarde (escapes alleen zonder `r`) na
  `inspect.cleandoc`. De regel is de regel van het eerste stringtoken. Een docstring met de
  marker sla je over.
- Comments: aaneengesloten regels met een comment vormen een blok, met
  `text = strings.TrimSpace(strings.TrimLeft(tok, "#"))`.

- [ ] **Stap 1: schrijf de tests.** Schakel `lang == "python"` in in `TestScanFixture`. Voeg
  `TestPythonGrammarErrorIsScanned` toe: `scanPython("x = = 1\n# lees het bestand\n")` geeft geen
  fout en een comment-eenheid op regel 2. Dat is review-focus 1, en het commentaar van de test
  noemt de afwijking. `TestPyLexErrors` is een tabel met een bron per fout uit de lijst hierboven,
  elk met `*CannotCheckError`.
- [ ] **Stap 2: draai** `go test -run 'TestScanFixture|TestPy' ./internal/codelang/`. Verwacht:
  FAIL.
- [ ] **Stap 3: implementeer** `pylex.go` en `scan_python.go` volgens de regels hierboven.
- [ ] **Stap 4: draai** de tests. Verwacht: PASS. Daarna draai je de eenheden van taak 5 al
  lokaal mee als controle.
- [ ] **Stap 5: commit.** `feat(codelang): Python-lexer en -scanner (#276)`.

### Taak 5: `Check` en het corpus

**Files:**
- Create: `internal/codelang/check.go`, `check_test.go`, `corpus_test.go`

**Interfaces:**
- Consumes: `Scan` en de scanners (taak 3 en 4), `config.Load` en `config.Lookup`
  (`internal/config`), `manifest.Read` (`internal/manifest`).
- Produces:
  - `type Inspection struct{ Findings, Doubts []Finding; Chain *Chain }`. `Chain` komt uit taak
    6; tot dan alleen het type.
  - `func Check(scorer func() (*Scorer, error), path, root, sessionTOML string) (Inspection, error)`.
    De scorer is een functie, zodat een bestand buiten de scope de data nooit laadt
    (`test_out_of_scope_does_not_load_data`). Hij geeft
    hetzelfde terug als `check_file`, in dezelfde volgorde van stappen: geen `lighthouse.toml` is
    niets; geen `project.code_language` is niets; scope; exempt; manifest; keten; lezen met de
    BOM eraf; scannen; mappen; de stam van het bestand als naam op regel 1.
  - `func ScopeDirs(relative string, globs []string) ([]string, bool)`. `false` betekent `None`.
  - `func matchGlob(glob, name string) bool`, de semantiek van `fnmatch.fnmatchcase`. Daarin
    matcht `*` ook `/`, `[!…]` is negatie en de rest is letterlijk. `path.Match` doet dat niet:
    schrijf de vertaling naar een regexp met de hand, net als `fnmatch.translate`.

- [ ] **Stap 1: schrijf de tests.** `TestCheckFixture` leest `check.json` en zet de bestanden in
  `t.TempDir()`. `TestScopeDirsFixture` leest `scope_dirs.json`. `TestMatchGlob` is een tabel met
  `pack/**` tegen `pack/a/b.md`, `*.py` tegen `a/b.py` (true, omdat `*` ook `/` matcht),
  `[!a]x` en `x[`. `TestUnitsFixture` leest `units/*.json`, scant de bron, roept per eenheid de
  echte scorer aan en vergelijkt de multiset `[soort-aanroep, tekst, oordeel]`. Voor een naam
  is de tekst `Unit.Text`, anders `Unit.Body`. `TestOutOfScopeDoesNotLoadData` laat `Check` een
  `.toml` buiten de scope lezen met een scorer-functie die `t.Fatal` aanroept.
- [ ] **Stap 2: schrijf** `corpus_test.go` met dezelfde vijf gevallen als
  `test_code_language_corpus.py`: de tien bestanden in `stdlib/` zijn schoon, de Go-bestanden
  onder `internal/` en `cmd/` zijn zonder bevinding, `pack/core/lib/config.py` zolang die
  bestaat (staat hij er niet, dan `t.Skip` met de reden *verwijderd in stap 12*), de
  `pack/core/hooks/*.sh`, de Markdown onder `pack/`, en de hele repo via `git ls-files -z` met
  `Check`. Dat laatste geval eist eerst dat `code_language = "en"` en dat `pack/**`, `internal/**`
  en `cmd/**` in `code_language_paths` staan. De root is `../..` vanaf het pakket.
- [ ] **Stap 3: draai** `go test -run 'TestCheck|TestScope|TestMatch|TestUnits|TestOutOfScope|TestCorpus' ./internal/codelang/`.
  Verwacht: FAIL, en daarna PASS na de implementatie.
- [ ] **Stap 4: pariteitsrun over de hele repo** (eenmalig, als bewijs, niet gecommit). Draai
  het script van taak 1, stap 4 op elk bestand uit `git ls-files` met de extensie `.py`, `.sh`,
  `.bash` of `.go`, en op elk `.md`-bestand onder `pack/`. Zet `.go` daarvoor in `LANG` naast
  `.txt`. Schrijf het resultaat naar een scratchmap. Draai daarna
  `LIGHTHOUSE_CODELANG_UNITS=<scratchmap> go test -run TestUnitsFixture ./internal/codelang/`.
  `TestUnitsFixture` leest die variabele als alternatieve map. Verwacht: PASS. Elk verschil los
  je op in de scanner, niet in de fixture. Zet het aantal bestanden en de uitkomst in de PR-body.
- [ ] **Stap 5: commit.** `feat(codelang): Check, scope en corpus in Go (#276)`.

### Taak 6: laag 2, de keten

**Files:**
- Create: `internal/codelang/llm.go`, `llm_state_unix.go` (`//go:build unix`),
  `llm_state_other.go` (`//go:build !unix`; daar is de map nooit privé, dus nooit een marker),
  `llm_test.go`

**Interfaces:**
- Produces:
  - `type Chain struct{ URL string; Models []string; KeyEnv, EnvFile string }`.
  - `func ChainFrom(c *config.Config) (*Chain, error)`. Hij geeft `nil, nil` zonder tabel. Een
    fout gebruikt de teksten van `chain_from`: `key code_language_llm.url is missing`,
    `key code_language_llm.models is missing or empty`,
    `code_language_llm.env_file is relative: <x>` en
    `code_language_llm.env_file does not exist: <p>` (met `~` uitgevouwen).
  - `type Reply struct{ NotEnglish []bool; Model string; Failures []string }`. `NotEnglish` is
    `nil` als de keten faalde.
  - `func Ask(ctx context.Context, ch *Chain, items [][2]string, stateDir string, perModel, total time.Duration) Reply`.
  - `func BuildPrompt(items [][2]string) string` en
    `func ParseAnswer(content string, n int) ([]bool, error)`.
  - `func DefaultStateDir() string`: `$XDG_RUNTIME_DIR` of `/tmp`, plus
    `lighthouse-code-language-<uid>`.

- [ ] **Stap 1: schrijf de tests.** `TestPromptFixture` en `TestParseFixture` lezen `llm.json`.
  Daarna één Go-test per gedragstest uit `test_code_language_llm.py`, met dezelfde naam in
  CamelCase en dezelfde asserts. Ze draaien tegen `httptest.Server` met de modi `answer`,
  `silent`, `drip`, `http500`, `garbage` en `invalid`, net als `tests/pack/_fake_llm.py`. De
  server controleert `Authorization`, `model`, `max_tokens` (≥ 512) en `temperature` = 0, omdat
  die bij het contract horen. De faalteksten zijn letterlijk: `<model>: HTTP 500`,
  `<model>: no answer within 4.0 s`, `<model>: unreadable answer`,
  `<model>: total time of 8 s is up`, `skipped: the chain failed <n> s ago` en
  `key: <ENV> is set in neither the environment nor env_file`. Bij `URLError` gebruikt Python
  `str(exc.reason)`. Go geeft de fout van de transportlaag zonder de URL-prefix (`*url.Error`
  wordt `Err.Error()`). De pytest op de hook assert alleen de prefix `m1: `.
- [ ] **Stap 2: draai** `go test -race -run 'TestPrompt|TestParse|TestAsk|TestKey|TestQuiet|TestMarker|TestState' ./internal/codelang/`.
  Verwacht: FAIL.
- [ ] **Stap 3: implementeer.** Per model geldt `context.WithTimeout(min(perModel, rest))` op
  een `http.Request`. Een server die druppelt, stopt dan bij de deadline van de context, dus
  zonder thread. De marker: `sha1(url)[:12]`, alleen in een map die privé is (lstat, eigen uid,
  geen schrijfrecht voor groep of anderen), met `O_NOFOLLOW`, inhoud `time.Now()` als
  Unix-seconden (float), en rust bij een leeftijd van 0 tot 60 s.
- [ ] **Stap 4: draai** de tests. Verwacht: PASS, ook met `-race`.
- [ ] **Stap 5: commit.** `feat(codelang): laag 2 met deadline en rustperiode in Go (#276)`.

### Taak 7: rapport, hook en runtime

**Files:**
- Create: `internal/codelang/report.go`, `internal/codelang/hook.go`, `report_test.go`,
  `hook_test.go`
- Modify: `internal/hook/hook.go` (`Result.Text`, `Env.Nested`), `internal/hook/hook_test.go`,
  `internal/cli/hook.go` (blanco import, `Nested` uit de omgeving)

**Interfaces:**
- Consumes: `Check` (taak 5), `Ask` en `Chain` (taak 6), `hook.Register` en `hook.Result`.
- Produces:
  - `hook.Result.Text string`. `Run` schrijft het letterlijk naar stdout als `Output` leeg is.
  - `hook.Env.Nested bool`. Is die gezet, dan voegt `Run` geen versie- of buildmelding toe.
    `hookEnv` zet hem als `LIGHTHOUSE_UNDER_BASH_WRITE_CHECK` niet leeg is.
  - `func Resolve(ctx context.Context, in Inspection, path string, underGates bool, stateDir string) ([]Finding, string)`.
    De string is de melding of `""`. De teksten van `_notice` en `resolve` zijn letterlijk, met
    de reden `layer 2 skipped under bash-write-check` en de prefix `NOTICE — `.
  - `func Report(path string, found []Finding) string`: `_report` letterlijk, met de regels voor
    de modulenaam en de mapnaam.
  - `func runHook(ctx context.Context, root, stand string, p hook.Payload) hook.Result`. Die
    registreert zich in `init()` als `code-language-check`.

**`runHook` stap voor stap** (gelijk aan `code-language-check.sh:56-142`):
1. `p.Raw` is geen JSON → hookniveau `the payload is not valid JSON.`. Ontbreekt
   `tool_input.file_path` of is die leeg → `Result{}`.
2. Een relatief pad wordt `cwd` (of `os.Getwd()`) plus het pad. Canoniek maken gaat als
   `realpath -m`: `filepath.Abs`, en dan `filepath.EvalSymlinks` op het langste deel dat bestaat,
   met de rest er weer achter. Lukt dat niet → hookniveau
   `cannot resolve '<abs>' (no 'realpath -m' and no python3).`. Is het geen regulier bestand →
   `Result{}`.
3. `git -C <dir> rev-parse --show-toplevel` met `LC_ALL=C`. Bevat de uitvoer
   `not a git repository` → `Result{}`. Een andere fout → hookniveau
   `git could not read '<dir>'.` met de git-uitvoer als detail. Een lege root → hookniveau
   `git gave no repository root for '<dir>'.`.
4. `sessionTOML` = `LIGHTHOUSE_TOML`, anders `<root-arg>/lighthouse.toml`. `root-arg` is de root
   van de runtime (`LIGHTHOUSE_REPO_ROOT` of git); de ROOT van het bestand telt hier niet.
5. `Check`, daarna `Resolve` met `underGates` uit de omgeving en `DefaultStateDir()`. Een fout →
   checkerniveau.
6. Zijn er bevindingen → `Code: 2`, `Stderr` = `Report(...)` + (`"\n\n" + melding` als er een
   melding is) + `"\n"`. Alleen een melding → onder gates `Text` = melding + `"\n"`, anders
   `Output` = de JSON uit *Globale eisen*. Niets → `Result{}`.

- [ ] **Stap 1: schrijf de tests.** In `internal/hook/hook_test.go`: `TestRunWritesText` (een
  hook met `Text` → stdout letterlijk, rc 0) en `TestRunNestedSkipsNotices` (versies wijken af,
  `Nested: true` → stdout is alleen `Text`, en er komt geen marker in `StateDir`). In
  `report_test.go`: de gedragstests uit de tabel van taak 1, rij 3, met dezelfde asserts.
  `TestReportCutsAtSixtyRunes` gebruikt een comment van 70 tekens `é`. In `hook_test.go` (van
  codelang): `TestHookCanonicalPath` (een symlink naar een map, en een pad naar een bestand dat
  niet bestaat in een map die wel bestaat) en `TestHookNotAGitRepo` → `Result{}`.
- [ ] **Stap 2: draai** `go test -race ./internal/hook/ ./internal/codelang/`. Verwacht: FAIL.
- [ ] **Stap 3: implementeer**, plus `import _ "github.com/CorneeldH/lighthouse/internal/codelang"`
  in `internal/cli/hook.go`.
- [ ] **Stap 4: draai** `go test -race ./...`. Verwacht: PASS. Proef met de hand:
  `printf '{"tool_input":{"file_path":"%s"}}' "$PWD/x.py" | go run ./cmd/lighthouse hook code-language-check`
  met een Nederlandse comment in `x.py`. Verwacht: rc 2 en het rapport. Ruim `x.py` daarna op.
- [ ] **Stap 5: commit.** `feat(codelang): hook code-language-check in Go (#276)`.

### Taak 8: de registratie om naar `run.sh`

**Files:**
- Modify: `pack/core/hooks/hooks.json:51` (`${CLAUDE_PLUGIN_ROOT}/hooks/run.sh code-language-check`,
  timeout 15 blijft), `pack/core/lib/bash_writes.py`, `tests/pack/test_code_language_hook.py`,
  `tests/pack/test_hooks_minimal_config.py`, `tests/pack/test_bash_write_check.py`,
  `pack/core/hooks/bash-write-check.sh:3` (de comment), `docs/site/data/packs.json`,
  `docs/site/map.js:216`

**Interfaces:**
- Produces: `bash_writes.GATE_ARGV: dict[str, tuple[str, ...]] = {"code-language-check.sh": ("run.sh", "code-language-check")}`.
  `run_gate` voert `[gate_path(root, argv[0]), *argv[1:]]` uit, met
  `argv = GATE_ARGV.get(gate, (gate,))`. `GATES`, `GATE_PACKS` en de labels in de uitvoer
  (`== main.go (code-language-check.sh) ==`) blijven ongewijzigd. Stap 2 (#275) kan
  `md-link-check.sh` hier op dezelfde manier toevoegen.

- [ ] **Stap 1: hooktests via `run.sh`.** In `test_code_language_hook.py` wordt `HOOK`
  `INSTALLED_HOOKS / "run.sh"`. `_run` start `[str(HOOK), "code-language-check"]`, en een
  autouse-fixture vraagt `lighthouse_bin`. Er verandert geen assert. Twee tests gaan weg, omdat
  hun onderwerp niet meer bestaat:
  - `test_hook_cannot_check_without_tomllib` (er is geen Python import meer);
  - `test_hook_broken_import_is_never_an_empty_block`. Het gedrag dat hij bewaakte (een crash is
    nooit een pass) bewijst `TestPanickingHookExits3` in `internal/hook` al: exit 3, en `run.sh` maakt
    daar volgens de blokkeerlijst exit 2 van.

  Noem beide met hun reden in de PR-body.
- [ ] **Stap 2: draai** `uv run pytest -q tests/pack/test_code_language_hook.py`. Verwacht:
  FAIL, want de registratie en de installatie lopen nog via de `.sh`, en `run.sh` vindt de hook
  pas na stap 3.
- [ ] **Stap 3: zet om.** Pas `hooks.json` en `GATE_ARGV` aan, en draai daarna
  `go run ./cmd/lighthouse init --upgrade`. Verwacht in de uitvoer: de oude registratie weg, de
  nieuwe `run.sh code-language-check` erbij. `test_hooks_minimal_config.py` kiest de payload
  voortaan op hooknaam: `script.split()[1]` als het eerste woord `run.sh` is. In de verzameling
  van `test_every_registration_has_a_payload` staat dan `code-language-check` in plaats van
  `code-language-check.sh`. De test die elke registratie draait, en
  `test_go_write_is_checked_for_code_language`, krijgen de fixture `lighthouse_bin`. In `packs.json` krijgt de hook `"source": "internal/codelang"`.
  `map.js:216` gebruikt voortaan `entry.source ? REPO_TREE + entry.source : …/hooks/<naam>.sh`.
  Pas `hooks` in `referenceEntries` aan zodat hij `entry` doorgeeft.
- [ ] **Stap 4: draai**
  `uv run pytest -q tests/pack/test_code_language_hook.py tests/pack/test_hooks_minimal_config.py tests/pack/test_bash_write_check.py tests/pack/test_bash_writes.py tests/test_site_reference.py tests/test_site_packs.py`.
  Verwacht: PASS.
- [ ] **Stap 5: commit**, inclusief de geïnstalleerde kopie.
  `feat(code-language-check): registratie via run.sh en de Go-hook (#276)`.

### Taak 9: de Python weg, afronden

**Files:**
- Delete: `pack/core/lib/code_language.py`, `code_language_llm.py`, `code_language_score.py`,
  `code_language_freq.tsv.gz`, `code_language_jargon.txt`, `code_language_freq_NOTICE.txt`,
  `pack/core/hooks/code-language-check.sh`, `tests/pack/test_code_language.py`,
  `test_code_language_score.py`, `test_code_language_llm.py`, `test_code_language_corpus.py`
- Modify: `pack/SOURCES.toml:547-573` (de zes entries weg; de comment over wordfreq en de
  attributie gaan mee naar een `README` in `internal/codelang/data/`, die `NOTICE.txt` noemt),
  `pack/core/requires.toml` (`code-language-check` uit `jq` en `python3`; `git` erbij als hij er
  nog niet staat), `internal/packs/core_test.go:58,101-106`, `tools/gen_code_language_freq.py:1,20`
  (de uitvoer naar `internal/codelang/data/freq.tsv.gz`), `tests/test_gen_code_language_freq.py`
  (als die het pad pint), `docs/host-assumptions.md:33`
  (de link naar `code_language.py` weg), `changelog.d/issue-276-code-language-check-go.md` (nieuw)

- [ ] **Stap 1: haal weg en werk bij** zoals hierboven. Pytest-tests die alleen de Python-modules
  testten, gaan met hun module weg. Hun gevallen leven in de fixtures, die Go leest (taak 2 t/m
  7). Een test die bewaakt dat iets weg is, schrijf je niet (`/lighthouse-test-standard`).
- [ ] **Stap 2: draai** `go run ./cmd/lighthouse init --upgrade`. Verwacht: de vijf
  lib-bestanden en `code-language-check.sh` onder `.lighthouse/lib/lighthouse_lib/` en
  `.claude/hooks/lighthouse/` zijn weg, net als hun regels in `.lighthouse/manifest.toml`. Draai
  het nog een keer. Verwacht: `No changes.`
- [ ] **Stap 3: controleer.** `grep -rn 'code_language\.py\|code_language_llm\|code_language_score\|code-language-check\.sh' --exclude-dir=.git .`
  geeft alleen nog treffers in `CHANGELOG.md`, in specs en plannen, en in de labels
  `code-language-check.sh` van `bash_writes.py` en zijn tests. Een verse init zonder
  `pack/python` heeft geen `python3` meer in `code-language-check`: draai de grep van het
  issuecommentaar opnieuw in een tijdelijke repo
  (`go run ./cmd/lighthouse init` met `core`, `go`, `node`, `shell` en `expo`, en dan
  `grep -rn python3 .claude/hooks/lighthouse/`). Verwacht: geen treffer voor
  `code-language-check`.
- [ ] **Stap 4: changelog.**

  ```markdown
  ---
  type: changed
  ---
  `code-language-check` runs in the `lighthouse` binary (`lighthouse hook code-language-check`) instead of bash and Python; the verdicts are unchanged. Python that tokenizes but does not parse (`x = = 1`) is now scanned instead of reported as `COULD NOT CHECK`. Update the `lighthouse` binary on your PATH and run `lighthouse init --upgrade`.
  ```
- [ ] **Stap 5: de volledige KPI-tabel van `/lighthouse-go-standard`**, plus
  `uv run pytest -q`. Rapporteer per rij de omgeving (GOOS, `-race`, `GOWORK`).
- [ ] **Stap 6: commit.**
  `refactor(code-language-check): Python-checker en bash-hook weg (#276)`.

## Klaar

- Elk acceptatiecriterium van #276 heeft een bewijs. AC1 is taak 8, stap 4. AC2 is taak 1 en de
  Go-tests die dezelfde fixtures lezen. AC3 is `corpus_test.go`, `TestUnitsFixture` en de
  pariteitsrun van taak 5. AC4 is taak 9, stap 3. AC5 is taak 9, stap 2. AC6 is taak 9, stap 4.
- De PR-body noemt de afwijking van review-focus 1 en de twee hooktests die weggingen, met hun
  reden.
