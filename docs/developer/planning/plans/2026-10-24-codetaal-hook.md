# Hook voor de taal van code — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** een hook die na elke write een Python-, shell- of Go-bestand blokkeert zolang het
niet-Engelse namen, commentaar of docstrings bevat, met bestand en regel.

**Architectuur:** de herkenning en het lezen van het bestand zitten in één lib-module
`pack/core/lib/code_language.py` met een CLI (rc 0 schoon/uit, rc 1 vondsten, rc 2 kon niet
controleren). Een dunne wrapper `pack/core/hooks/code-language.sh` hangt aan PostToolUse
`Edit|Write`; `bash_writes.py` krijgt meerdere gates per extensie, zodat dezelfde wrapper ook na
Bash-writes draait.

**Tech stack:** Python 3.11+ standaardbibliotheek (`tokenize`, `ast`, `tomllib`, `fnmatch`), bash,
`jq`; tests met pytest; Go alleen voor de lijst geïnstalleerde bestanden.

**Spec:** [2026-10-24-codetaal-hook-design.md](../specs/2026-10-24-codetaal-hook-design.md)

## Algemene randvoorwaarden

- Config: `[project] code_language` (`str`, alleen `"en"`) en `[project] code_language_exempt`
  (`list[str]`, globs), allebei zonder default (besluit 3, 8b).
- Zonder `code_language`: exit 0, geen uitvoer (besluit 4). `init` schrijft de sleutel niet
  (besluit 5); `internal/` verandert daarvoor niet.
- Alleen standaardbibliotheek; geen Go-binary of shellparser als afhankelijkheid (besluit 6, 9).
- Extensies `.py`, `.sh`, `.bash`, `.go`, alleen op extensie (besluit 7).
- Stringliterals worden niet gelezen, docstrings wel (spec *Non-goals*).
- Markering per regel: de letterlijke tekst `lighthouse: allow-language` (besluit 8c).
- Config uit de `lighthouse.toml` van de git-root van het geschreven bestand; geen git-repo of geen
  `lighthouse.toml` = uit; een `ConfigError` = `COULD NOT CHECK` (besluit 10).
- Elk pad in `<root>/.lighthouse/manifest.toml` (`[[unit]] path`) wordt overgeslagen (besluit 11).
- Kon niet controleren: exit 2 met `code-language: COULD NOT CHECK — <reden>` als eerste regel
  (besluit 18 van de M0-spec).
- Melding: kop `code-language: <bestand> is not English`, daarna `<bestand>:<regel>: <soort>: <tekst>`
  met `<soort>` uit `comment`, `docstring`, `name`; tekst van commentaar en docstring afgekapt op 60
  tekens; maximaal 20 regels, daarna `… and <n> more`; daarna één regel uitleg met de markering.
- Hookmeldingen, code, namen en commentaar in het Engels; commitberichten in het Nederlands.
- Woordenlijsten bevatten geen van deze woorden (spec): `die`, `met`, `door`, `over`, `van`, `map`,
  `pad`, `is`, `in`, `op`, `been`, `had`, `was`, `hem`.
- Dogfood: na de wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en gaat de
  geïnstalleerde kopie mee in de commit; daarna geeft een tweede run `No changes.`
- Testpoort: `uv run pytest -q && go test ./...`.

## Aandachtspunten voor de review

1. **Een half geschreven Python-bestand** (syntaxfout midden in een edit): `COULD NOT CHECK` met
   reden, geen traceback. Test in taak 3.
2. **Een heredoc in shell** met regels die met `#` beginnen: dat is data, geen commentaar, en geeft
   geen vondst. Test in taak 4.
3. **`//` of `/*` in een Go-string of raw string** (backticks, bijvoorbeeld een URL): geen commentaar.
   Test in taak 4.
4. **Een bestand dat geen UTF-8 is**: `COULD NOT CHECK`, geen crash. Test in taak 5.
5. **Een relatief `file_path` in de payload**, opgelost tegen `cwd`, en een bestand in een andere
   git-repo zonder `lighthouse.toml`: exit 0. Test in taak 6.

---

### Taak 1: configsleutels

**Bestanden:**
- Wijzig: `pack/core/lib/config.py` (`SCHEMA`, en `load` naast de check van `project.language`)
- Test: `tests/pack/test_config.py`

**Interfaces:**
- Levert: `lookup(cfg, "project.code_language") -> str` (`KeyError` als niet gezet) en
  `lookup(cfg, "project.code_language_exempt") -> list[str]` (`KeyError` als niet gezet).

- [ ] **Stap 1: schrijf de falende tests** in `tests/pack/test_config.py`, in de stijl van de
  bestaande tests daar:
  - `test_code_language_en_is_read`: `code_language = "en"` → `lookup(...) == "en"`.
  - `test_code_language_absent_is_keyerror`: zonder sleutel → `KeyError`.
  - `test_code_language_other_value_is_config_error`: `"nl"` → `ConfigError` met
    `project.code_language` en `en` in de tekst.
  - `test_code_language_exempt_is_list_of_str`: `["tests/fixtures/*"]` wordt gelezen; `"x"` (geen
    lijst) → `ConfigError`.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_config.py -k code_language` → FAIL.
- [ ] **Stap 3:** voeg `"project.code_language": _STR` en `"project.code_language_exempt": _STRS` toe
  aan `SCHEMA` (niet aan `DEFAULTS`); in `load` een check naast die van `project.language`: aanwezig
  en niet `"en"` → `ConfigError(f"{path}: key project.code_language must be en, not {value!r}")`.
- [ ] **Stap 4:** dezelfde pytest-run → PASS; daarna `uv run pytest -q tests/pack/test_config.py`
  → PASS.
- [ ] **Stap 5: commit** `feat(config): sleutels voor de taal van code (#26)`.

### Taak 2: herkenning van Nederlandse tekst en namen

**Bestanden:**
- Maak: `pack/core/lib/code_language.py` (alleen de herkenning in deze taak)
- Maak: `pack/core/lib/code_language_nl_function.txt`, `pack/core/lib/code_language_nl_content.txt`
  (één woord per regel, kleine letters, `#`-regels zijn commentaar)
- Wijzig: `internal/packs/core_test.go` — elk bestand onder `pack/core/lib/` is een geïnstalleerde
  unit, en `TestCorePack` pint de lijst (`core_test.go:30-115`). Voeg
  `.lighthouse/lib/lighthouse_lib/code_language.py`, `code_language_nl_content.txt` en
  `code_language_nl_function.txt` (onder dezelfde map) toe op hun alfabetische plek; anders blokkeert
  de testpoort de commit van deze taak.
- Test: `tests/pack/test_code_language.py`

**Interfaces:**
- Levert, in `code_language.py`:
  - `split_name(name: str) -> list[str]` — splitst op `_`, op cijfers en op de overgang klein→hoofd
    en HOOFD→Hoofd (`HTTPServer` → `http`, `server`), alles in kleine letters, lege delen weg.
  - `words(text: str) -> list[str]` — woorden van letters (accenten erbij), kleine letters.
  - `is_dutch_text(text: str) -> bool` — minstens één woord uit de functie- of inhoudslijst.
  - `is_dutch_name(name: str) -> bool` — minstens één deel van `split_name` in een van de lijsten.
  - `FUNCTION_WORDS: frozenset[str]`, `CONTENT_WORDS: frozenset[str]` — gelezen uit de twee `.txt`
    naast de module (`Path(__file__).with_name(...)`).

- [ ] **Stap 1: schrijf de falende tests:**
  - `test_split_name`: `leesBestand` → `["lees", "bestand"]`; `read_file_2` → `["read", "file"]`;
    `HTTPServer` → `["http", "server"]`.
  - `test_banned_words_absent`: de veertien woorden uit *Algemene randvoorwaarden* zitten in geen
    van beide lijsten.
  - `test_dutch_text`: `"Het bestand wordt niet gelezen."` → True;
    `"# --- post (taak 7): bron 1, claims, dispatch, uitvoer ---"` → True (via inhoudswoorden);
    `"Read the file and return its lines."` → False.
  - `test_dutch_name`: `lees_bestand`, `gevondenRegels` → True; `read_file`, `foundLines`,
    `map_path` → False.
  - `test_english_corpus_has_no_dutch_words`: elk commentaarwoord en elke naam in de Go-bestanden
    onder `internal/` en `cmd/` (ruw: haal eerst per regel de stringliterals weg met
    `` re.sub(r'"(?:\\.|[^"\\])*"|`[^`]*`|\'(?:\\.|[^\'\\])*\'', "", line) ``; daarna alle woorden na
    `//`, en alle identifiers via `re.findall(r"[A-Za-z_][A-Za-z0-9_]*", line)`) geeft
    `is_dutch_text`/`is_dutch_name` False. Strings zijn data (spec, *Non-goals*): zonder die stap
    levert de SHA-256 in `internal/manifest/manifest_test.go:109` (`…140de5dae…`) via `split_name`
    het deel `de` op.
    Faalt dit, dan staat er een woord in een lijst dat ook in Engelse code voorkomt: haal het eruit.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_code_language.py` → FAIL.
- [ ] **Stap 3:** implementeer de vijf functies en vul de lijsten. Functiewoorden: lidwoorden,
  voornaamwoorden, voorzetsels, voegwoorden, hulpwerkwoorden (start: `de`, `het`, `een`, `niet`,
  `geen`, `wordt`, `worden`, `zijn`, `voor`, `naar`, `maar`, `ook`, `nog`, `dus`, `als`, `dat`,
  `deze`, `dit`, `wel`, `zo`, `bij`, `uit`, `tot`, `om`, `te`, `er`, `kan`, `moet`, `mag`, `heeft`,
  `hebben`). Inhoudswoorden: woorden die in de huidige Nederlandse code onder `pack/` vaak voorkomen
  (start: `bestand`, `bestanden`, `regel`, `regels`, `naam`, `lees`, `schrijf`, `gevonden`, `kop`,
  `taak`, `bron`, `uitvoer`, `invoer`, `melding`, `oordeel`, `wortel`, `tak`, `controleer`,
  `controleren`, `fout`, `leeg`, `alsnog`). **Niet** `besluit`: Engels commentaar in lighthouse citeert
  besluiten als `(spec, besluit 4)` (`internal/initcmd/initcmd.go:5`, `pack/core/lib/config.py:51`), en
  dat blijft zo na de vertaalslag. Elke toevoeging moet `test_english_corpus_has_no_dutch_words`
  overleven.
- [ ] **Stap 4:** dezelfde run → PASS.
- [ ] **Stap 5: commit** `feat(pack): herkenning van Nederlandse tekst en namen (#26)`.

### Taak 3: Python lezen

**Bestanden:**
- Wijzig: `pack/core/lib/code_language.py`
- Test: `tests/pack/test_code_language.py`

**Interfaces:**
- Gebruikt: `is_dutch_text`, `is_dutch_name` (taak 2).
- Levert:
  - `class Finding(NamedTuple): line: int; kind: str; text: str` — `kind` is `"comment"`,
    `"docstring"` of `"name"`; `text` is de naam, of de eerste regel van het blok.
  - `class CannotCheck(Exception)` — de boodschap is de reden.
  - `MARKER = "lighthouse: allow-language"`
  - `scan_python(source: str) -> list[Finding]`, gesorteerd op regel; gooit `CannotCheck` bij een
    `tokenize.TokenError`, `SyntaxError` of `IndentationError`.

- [ ] **Stap 1: schrijf de falende tests:**
  - `test_python_comment_block`: twee aaneengesloten commentaarregels, alleen de tweede met
    `"niet"` → één vondst, `kind == "comment"`, `line` = regel van de eerste.
  - `test_python_docstring`: functie met docstring `"""Lees het bestand."""` → één
    `"docstring"`-vondst op de regel van de docstring.
  - `test_python_names`: `def lees_bestand(pad_naam): resultaat = 1` plus `class Gevonden:` →
    vondsten voor `lees_bestand`, `pad_naam`, `Gevonden` (en `resultaat` alleen als het in de lijst
    staat; de test gebruikt alleen woorden uit taak 2).
  - `test_python_strings_are_not_read`: `x = "het bestand"` → geen vondst.
  - `test_python_use_site_is_not_a_finding`: `from elders import lees_bestand` gevolgd door
    `lees_bestand()` → geen vondst (een import zonder `as` ontstaat niet in dit bestand).
  - `test_python_marker`: commentaarregel met `niet` en `lighthouse: allow-language` → geen vondst;
    een naam op een regel met de markering → geen vondst.
  - `test_python_syntax_error_is_cannot_check` (aandachtspunt 1): `"def f(:\n"` → `CannotCheck`.
  - `test_python_english_clean`: `pack/core/lib/config.py` → geen vondst.
- [ ] **Stap 2:** run → FAIL.
- [ ] **Stap 3:** implementeer `scan_python`. Commentaar via `tokenize.generate_tokens`
  (`COMMENT`-tokens; aaneengesloten regels vormen één blok); docstrings via `ast.get_docstring` op
  module, klasse en (async) functie, regel = `node.body[0].lineno`; namen via `ast.walk`:
  `FunctionDef`/`AsyncFunctionDef`/`ClassDef.name`, elk `arg.arg`, `Name` met `Store`-context,
  `Attribute` met `Store`-context (`.attr`), `alias.asname`, `ExceptHandler.name`. De markering op de
  regel van een commentaarregel of naam slaat die over; in een docstring slaat hij de hele docstring
  over. Eén vondst per naam per regel.
- [ ] **Stap 4:** run → PASS.
- [ ] **Stap 5: commit** `feat(pack): Python-bestanden lezen voor de taal van code (#26)`.

### Taak 4: shell en Go lezen

**Bestanden:**
- Wijzig: `pack/core/lib/code_language.py`
- Test: `tests/pack/test_code_language.py`

**Interfaces:**
- Gebruikt: `Finding`, `MARKER`, `is_dutch_text`, `is_dutch_name` (taak 2, 3).
- Levert: `scan_shell(source: str) -> list[Finding]` en `scan_go(source: str) -> list[Finding]`,
  allebei gesorteerd op regel, zonder `CannotCheck` (een heuristische tokenizer kan niet falen).

- [ ] **Stap 1: schrijf de falende tests:**
  - shell: `test_shell_comment` (`# lees het bestand` → `comment`); `test_shell_shebang_skipped`;
    `test_shell_hash_in_string_and_param` (`echo "# niet"`, `${#regels}`, `$#` → geen vondst);
    `test_shell_heredoc_is_data` (aandachtspunt 2: `cat <<EOF` met een regel `# het bestand` → geen
    vondst; ook `<<-'EOF'`); `test_shell_names` (`lees_bestand() {`, `function controleer {`,
    `regels=3`, `local bestand`, `export NAAM_BESTAND=x` → vondsten); `test_shell_marker`.
  - go: `test_go_line_and_block_comment` (`// lees het bestand`, `/* niet\n gelezen */` als één
    blok); `test_go_comment_in_string` (aandachtspunt 3: `"http://x"`, `` `// niet` ``, `'/'` →
    geen vondst); `test_go_names` (`package bestanden`, `func leesBestand(regel string)`,
    `func (r *T) Controleer()`, `type Melding struct { Naam string }`, `var fout error`,
    `const Leeg = 0`, `gevonden := 1`, gegroepeerd `var ( bron int )` → vondsten); `test_go_marker`;
    `test_go_english_clean`: alle `.go` onder `internal/` en `cmd/` → geen vondst.
- [ ] **Stap 2:** run → FAIL.
- [ ] **Stap 3:** implementeer beide met een tekenscanner die de strings van de taal overslaat
  (shell: `'…'`, `"…"` met `\`-escapes, heredoc-body tot de afsluiter; Go: `"…"`, `` `…` ``, `'…'`).
  Shell-commentaar: `#` aan het begin van een woord, niet regel 1 met `#!`. Namen via regexen op de
  gestripte regels voor de vormen uit de tests.
- [ ] **Stap 4:** run → PASS.
- [ ] **Stap 5: commit** `feat(pack): shell- en Go-bestanden lezen voor de taal van code (#26)`.

### Taak 5: bestand controleren en CLI

**Bestanden:**
- Wijzig: `pack/core/lib/code_language.py`
- Maak: `tests/pack/fixtures/code_language/bash_writes_nl.py` — letterlijke kopie van
  `pack/core/lib/bash_writes.py` zoals die op `origin/main` staat bij de start van deze taak
  (`git show origin/main:pack/core/lib/bash_writes.py`)
- Test: `tests/pack/test_code_language.py`

**Interfaces:**
- Gebruikt: `scan_python`, `scan_shell`, `scan_go`, `CannotCheck`, `Finding` (taak 3, 4);
  `config.load`, `config.lookup`, `config.ConfigError` (taak 1).
- Levert:
  - `EXTENSIONS: dict[str, Callable[[str], list[Finding]]]` = `.py`→`scan_python`,
    `.sh`/`.bash`→`scan_shell`, `.go`→`scan_go`.
  - `check_file(path: Path, root: Path) -> list[Finding]` — `path` absoluut, `root` de git-root.
    Lege lijst als: andere extensie, geen `root/lighthouse.toml`, geen `code_language`, pad onder
    een glob uit `code_language_exempt` (`fnmatch.fnmatchcase` op het repo-relatieve POSIX-pad; `*`
    gaat over `/` heen), of pad in het manifest. Vondsten voor de modulenaam: `path.stem`, als
    `"name"` op regel 1. Gooit `CannotCheck` bij: `ConfigError`, manifest dat er is maar niet te lezen
    is, `UnicodeDecodeError`, en wat de scanner gooit.
  - `main(argv: list[str]) -> int` — `code_language.py <abs-path> <root>`: rc 0 zonder uitvoer bij
    geen vondsten; rc 1 met de volledige melding (formaat in *Algemene randvoorwaarden*) op stdout;
    rc 2 met de reden op stderr bij `CannotCheck` én bij elke andere exception (een `OSError` bij
    het lezen, een `RecursionError` in `ast`, ...): een crash mag nooit als rc 1 = vondsten lezen
    (besluit 18).

- [ ] **Stap 1: schrijf de falende tests** (met `tmp_path` als root, een `lighthouse.toml` met
  `MINIMAL_TOML` uit `tests/pack/_testkit.py` plus `code_language = "en"` onder `[project]`):
  - `test_check_off_without_key`, `test_check_off_without_toml`, `test_check_other_extension`.
  - `test_check_exempt_glob`: `code_language_exempt = ["tests/fixtures/*"]` → bestand onder
    `tests/fixtures/a/b.py` met Nederlands → leeg.
  - `test_check_manifest_skips_managed`: `.lighthouse/manifest.toml` met een `[[unit]]`
    `path = ".lighthouse/lib/lighthouse_lib/x.py"` → dat bestand leeg.
  - `test_check_config_error_is_cannot_check`: `code_language = "nl"` → `CannotCheck`.
  - `test_check_not_utf8_is_cannot_check` (aandachtspunt 4): bytes `b"# \xff\xfe\n"` → `CannotCheck`.
  - `test_check_module_name`: `lees_bestand.py` met Engelse inhoud → één `name`-vondst, regel 1.
  - `test_dutch_corpus_found`: `scan_python` over de fixture `bash_writes_nl.py`: elke docstring
    (`ast.get_docstring` op module, klassen, functies) en elk commentaarblok heeft een vondst op zijn
    eerste regel, behalve de regels in een constante `ENGLISH_BLOCKS` in de test, elk met een
    commentaar waarom dat blok Engels is.
  - `test_main_output`: rc 1, stdout begint met `code-language: <pad> is not English`, een regel
    `<pad>:<n>: comment: …`, en bij 25 vondsten precies 20 vondstregels plus `… and 5 more`; rc 2
    met reden op stderr bij een `CannotCheck`; rc 2 met reden op stderr als `check_file` een
    onverwachte exception gooit (monkeypatch); rc 0 en lege stdout bij een schoon bestand.
- [ ] **Stap 2:** run → FAIL.
- [ ] **Stap 3:** implementeer `EXTENSIONS`, `check_file` en `main` (plus `if __name__ == "__main__":
  sys.exit(main(sys.argv[1:]))`). Vul daarna de woordenlijsten van taak 2 aan tot
  `test_dutch_corpus_found` groen is: de startlijsten missen nog Nederlandse blokken in de fixture
  (bijvoorbeeld `# De map van de gates, onder de repo-root.` en `# GATES kent de extensie`). Elke
  toevoeging moet `test_english_corpus_has_no_dutch_words` overleven. `ENGLISH_BLOCKS` is alleen voor
  blokken die echt Engels zijn, nooit om een gemist Nederlands blok weg te zetten. De uitlegregel na de vondsten:
  `The whole file must be English. A line that is deliberately not English gets "lighthouse: allow-language".`
  Import van `config` zoals de andere CLI-modules in de lib dat doen wanneer ze als script draaien
  (kijk naar `md_links.py` en `bash_writes.py`).
- [ ] **Stap 4:** run → PASS.
- [ ] **Stap 5: commit** `feat(pack): bestand controleren op de taal van code (#26)`.

### Taak 6: de hook bij Edit en Write

**Bestanden:**
- Maak: `pack/core/hooks/code-language.sh` (uitvoerbaar)
- Wijzig: `pack/core/hooks/hooks.json` (PostToolUse, matcher `Edit|Write`, timeout 15, na
  `worktree-linkcheck.sh` in dezelfde `hooks`-lijst)
- Wijzig: `tests/pack/test_hooks_minimal_config.py`: voeg `"code-language.sh"` toe aan de set op
  regels 122-130, **en** geef `_payload` (regels 52-77) een tak voor `code-language.sh`: een
  Edit-payload van een `.py`-bestand in de worktree (zoals de tak van `worktree-linkcheck.sh`); met
  `MINIMAL_TOML` (geen `code_language`) verwacht de test exit 0. Zonder die tak gooit `_payload`
  `AssertionError("no payload for ...")`.
- Wijzig: `internal/packs/core_test.go`: voeg `.claude/hooks/lighthouse/code-language.sh` toe op zijn
  alfabetische plek, en zet de hooktelling op regel 116 van `kinds[KindHook] != 8` naar `!= 9`. De
  lib-bestanden staan er sinds taak 2 al in.
- Wijzig: `pack/core/requires.toml`: `code-language` onder `git`, `jq`, `python3` en `tomllib`;
  `lighthouse_lib.code_language` onder `python3` en `tomllib`.
- Test: `tests/pack/test_code_language_hook.py`

**Interfaces:**
- Gebruikt: de CLI van taak 5 via `"$LIB_ROOT/.lighthouse/lib/lighthouse_lib/code_language.py" <abs> <root>`.
- Levert: het hookcontract dat taak 7 via `bash-write-gates` aanroept: payload op stdin met
  `.tool_input.file_path` en `.cwd`; exit 0 schoon/uit, exit 2 met vondsten of met
  `code-language: COULD NOT CHECK — <reden>` als eerste regel.

- [ ] **Stap 1: schrijf de falende tests** in de stijl van `tests/pack/test_worktree_linkcheck.py`
  (subprocess, echte tijdelijke git-repo, `INSTALLED_HOOKS / "code-language.sh"`):
  - `test_hook_off_without_key` → exit 0, lege stderr.
  - `test_hook_blocks_dutch_comment` → exit 2, stderr bevat `<bestand>:<regel>: comment:`.
  - `test_hook_relative_path_from_cwd` (aandachtspunt 5): `file_path` relatief, `cwd` de repo → exit 2.
  - `test_hook_other_repo_without_toml` (aandachtspunt 5): bestand in een tweede git-repo zonder
    `lighthouse.toml` → exit 0.
  - `test_hook_outside_git` → exit 0.
  - `test_hook_cannot_check_bad_json`, `test_hook_cannot_check_config_error`,
    `test_hook_cannot_check_syntax_error`: exit 2, eerste regel begint met
    `code-language: COULD NOT CHECK — `.
  - `test_hook_missing_file` → exit 0.
  - `test_hook_exempt_skips`: `code_language_exempt = ["tests/fixtures/*"]`, Nederlands bestand onder
    `tests/fixtures/` met een relatief `file_path` → exit 0.
  - `test_hook_manifest_skips`: Nederlands bestand op een pad uit `.lighthouse/manifest.toml` → exit 0.
  - `test_hook_marker_skips`: Nederlands commentaar met `lighthouse: allow-language` → exit 0.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_code_language_hook.py` → FAIL.
- [ ] **Stap 3:** schrijf de wrapper naar het model van `worktree-linkcheck.sh`: kopcommentaar (doel,
  matcher, niet fail-open, besluit 18), `cannot_check`, tooling-check (`jq`, `git`, `python3`),
  payload lezen, extensiefilter (`*.py|*.sh|*.bash|*.go`), pad oplossen tegen `cwd`, ontbrekend
  bestand exit 0, git-root (`not a git repository` → exit 0), `LIB_ROOT` zoals daar, CLI aanroepen:
  rc 0 → exit 0; rc 1 → stdout naar stderr, exit 2; elke andere rc → `cannot_check` met de stderr
  van de CLI. Registreer in `hooks.json`.
- [ ] **Stap 3b:** werk `test_hooks_minimal_config.py` (set én `_payload`-tak), `core_test.go`
  (pad én telling 9) en `requires.toml` bij, zoals onder *Bestanden*.
- [ ] **Stap 4:** `uv run pytest -q tests/pack/test_code_language_hook.py tests/pack/test_hooks_minimal_config.py && go test ./internal/...` → PASS.
- [ ] **Stap 5: commit** `feat(pack): hook voor de taal van code bij Edit en Write (#26)`.

### Taak 7: meerdere gates per extensie in bash-write-gates

**Bestanden:**
- Wijzig: `pack/core/lib/bash_writes.py` (`GATES`, `GATE_PACKS`, `gate_path`, `run_gate`, `_dispatch`,
  en de drie plekken die `in GATES` testen: `seed_root`, de pool in `_pool`, `_dispatch`)
- Test: `tests/pack/test_bash_writes.py` (regels 39, 46-54, 1254-1261 aanpassen, plus elke plek die
  `run_gate` vervangt of aanroept: de nep-gates op 877, 965-967, 987-989, 1025, 1088 en 1145-1147
  krijgen de signatuur `(root, rel, gate, deadline)`; de directe aanroepen op 1270 en 1278 krijgen
  een gate-argument; controleer met `grep -n run_gate tests/pack/*.py` dat er geen over is),
  `tests/pack/test_bash_write_gates.py`

**Interfaces:**
- Gebruikt: het hookcontract van taak 6.
- Levert:
  - `GATES: dict[str, tuple[str, ...]]`: `.sh`/`.bash` → `("shellcheck.sh", "code-language.sh")`,
    `.py` → `("ruff-format.sh", "code-language.sh")`, `.go` → `("code-language.sh",)`,
    `.md`/`.markdown` → `("worktree-linkcheck.sh",)`. Volgorde = draaivolgorde: een herschrijvende
    gate (`_SIGN_AFTER_GATE`) staat altijd vóór `code-language.sh`.
  - `GATE_PACKS` krijgt `"code-language.sh": "core"`.
  - `gate_path(root: str, gate: str) -> Path` — het pad van één gate.
  - `run_gate(root: str, rel: str, gate: str, deadline: float) -> tuple[str, bytes]`.

- [ ] **Stap 1: pas de tests aan en schrijf de nieuwe:**
  - `test_bash_writes.py`: de GATES-asserts naar de tuples hierboven; `set(bw.GATE_PACKS) ==
    {g for gs in bw.GATES.values() for g in gs}`; `gate_path(root, "shellcheck.sh") == hooks /
    "shellcheck.sh"`.
  - `test_dispatch_runs_every_gate_in_order`: met twee stub-gates voor één extensie draaien beide,
    in volgorde; beide oordelen staan als eigen `== <rel> (<gate>) ==`-deel in de bundel.
  - `test_dispatch_combined_verdict`: is één gate `unchecked`, dan blijft het bestand in `pending`
    en niet in `seen`; zijn alle gates `ok`/`finding`/`missing`, dan staat het in `seen`.
  - `test_dispatch_signature_after_rewriting_gate`: de signatuur in `seen` is die van ná de laatste
    gate uit `_SIGN_AFTER_GATE` die draaide, anders die van vóór de eerste gate.
  - `test_deadline_makes_progress_over_many_files` (regel ~863): de deadline blijft **per bestand**
    gecontroleerd, niet per gate. Een bestand waarvan de eerste gate begon, krijgt al zijn gates
    (elke `run_gate` is zelf door `deadline` begrensd: een gate na de deadline geeft `unchecked`).
    Een controle per gate zou in deze test elke post alleen de eerste gate van `a.sh` laten draaien,
    `a.sh` bleef `unchecked` in `pending`, en de dispatch kwam nooit verder. `fake_gate` legt
    `(rel, gate)` vast; verwacht `judged == [("a.sh", "shellcheck.sh"), ("a.sh", "code-language.sh"),
    ("b.sh", "shellcheck.sh"), ("b.sh", "code-language.sh"), ("c.sh", "shellcheck.sh"),
    ("c.sh", "code-language.sh")]` en ongewijzigd `codes == [2, 2, 2, 0]`.
  - `test_bash_write_gates.py`: een Bash-write van een `.go`-bestand met Nederlands commentaar in
    een repo met `code_language = "en"` blokkeert met `(code-language.sh)` in de uitvoer; zonder de
    sleutel geen blokkade.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_bash_writes.py tests/pack/test_bash_write_gates.py` → FAIL.
- [ ] **Stap 3:** implementeer. In `_dispatch` blijft de deadlinecontrole per bestand (boven de
  gatelus); per bestand een lus over `GATES[ext]`: elke gate
  `run_gate` + `_report_verdict`; `_STOPPED` stopt de dispatch zoals nu; het gecombineerde oordeel is
  `"unchecked"` als één gate dat gaf, anders het oordeel van de laatste gate; daarna één
  `_record_verdict`. Een gate die mist (besluit 19) meldt per bestand en blokkeert niet, zoals nu.
- [ ] **Stap 4:** dezelfde run → PASS.
- [ ] **Stap 5: commit** `feat(pack): bash-write-gates draait meerdere gates per extensie (#26)`.

### Taak 8: dogfood en volledige testpoort

**Bestanden:**
- Wijzig (gegenereerd): `.claude/hooks/lighthouse/code-language.sh`,
  `.lighthouse/lib/lighthouse_lib/`, `.claude/settings.json`, `.lighthouse/manifest.toml`
- `lighthouse.toml` verandert **niet**: de hook blijft uit in lighthouse (spec, *Aanleiding en doel*).
  De vertaalslag die hem aanzet, zet `tests/pack/fixtures/code_language/*` in
  `code_language_exempt`: die fixture is bewust Nederlands.

- [ ] **Stap 1:** `go run ./cmd/lighthouse init --upgrade --yes` → de nieuwe hook, lib-bestanden en
  registratie verschijnen. Zonder `--yes` vraagt `init` om bevestiging en weigert hij bij een lege
  stdin (exit 1, niets geschreven; `internal/initcmd/initcmd.go:200`).
- [ ] **Stap 2:** `go run ./cmd/lighthouse init --upgrade` nog eens, zonder `--yes` → `No changes.`
- [ ] **Stap 3:** `uv run pytest -q && go test ./...` → alles groen.
- [ ] **Stap 4:** controleer dat de hook in lighthouse uit staat:
  `echo '{"tool_input":{"file_path":"pack/core/lib/bash_writes.py"},"cwd":"'"$PWD"'"}' | .claude/hooks/lighthouse/code-language.sh; echo $?` → `0`, geen uitvoer.
- [ ] **Stap 5: commit** `chore(pack): codetaal-hook geïnstalleerd in lighthouse zelf (#26)`.
