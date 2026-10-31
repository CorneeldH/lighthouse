# Installatiehandleiding voor agents (#88) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eén Engelse markdown-handleiding voor agents op
`https://corneeldh.github.io/lighthouse/agents/install.md`, met een plakzin op de site en tests
die de feiten in de handleiding aan de code vastpinnen.

**Architecture:** Een statisch bestand `docs/site/agents/install.md`, dat Pages al publiceert.
Een nieuw `tests/test_agent_guide.py` controleert flags, tellingen, tools, talen en de plakzin
tegen de bron, in de stijl van `tests/test_getting_started.py`. Op de site komt een plakblok op
`install.html`, met de bestaande copy-knop uit `download.js`. Eén Go-test pint het
`init`-gedrag waar de handleiding op leunt.

**Tech Stack:** Markdown, statische HTML/JS (`docs/site/`), pytest, Go-test.

**Spec:** [2026-10-31-issue-88-agent-installatiehandleiding-design.md](../specs/2026-10-31-issue-88-agent-installatiehandleiding-design.md)

## Global Constraints

- De handleiding is in het Engels. Het plan, de commits en de PR zijn in het Nederlands.
- De plakzin is letterlijk:
  `Read https://corneeldh.github.io/lighthouse/agents/install.md and install lighthouse in this repo.`
- In een `.md`-bestand is een link relatief of `https://`, nooit een absoluut pad.
- Geen `sudo`, nergens.
- Akkoord voor `init` gaat via `printf 'y\n' | lighthouse init …`. Er staat nooit `--yes` in een
  codeblok van de handleiding.
- Taal: alleen `nl` of `en` (`LANGUAGES` in `pack/core/lib/config.py`).
  `[project] code_language` kent alleen `"en"`.
- `lighthouse init` zelf verandert niet. Raak `internal/initcmd/*.go` alleen in `_test.go` aan.
- Site: elke pagina `lang="en"`, geen `innerHTML`, na een html/js-wijziging
  `uv run python -m tests.site_version`. Op `install.html` geen extra `hidden` en geen extra
  `<details>`/`<summary>` (`test_install_page_works_without_js`).
- Elke PR heeft een fragment in `changelog.d/` (`type:` frontmatter, zie
  `changelog.d/issue-156-website-landing.md`).
- Test-gate: `uv run pytest -q <bestanden>` voor Python, `go test ./internal/initcmd/` voor Go.

## Review Focus

1. **De agent heeft geen TTY.** `init` zonder `--yes` met een lege stdin moet niets schrijven en
   exit 1 geven, terwijl de diff wel getoond wordt. Dat is al getest. Taak 1 pint de tweede helft:
   `printf 'y\n'` keurt de diff goed en laat `host.toml` ongeschreven.
2. **De tekst loopt voor op release `v0.2.0`.** Een flag die op `main` bestaat maar in de release
   niet. De handleiding vangt dat met de `--help`-regel. Taak 2 pint dat die regel erin staat.
3. **De gebruiker spreekt geen Nederlands of Engels.** Taak 2 pint dat de taalkeuze precies
   `LANGUAGES` noemt en dat `en` dan de aanbeveling is.
4. **Lighthouse staat er al.** De agent moet stoppen, niet `init` draaien (dat weigert) of
   `--upgrade` gebruiken. Taak 2 pint dat `.lighthouse/manifest.toml` en de stop erin staan.
5. **Codex of OpenCode, waar `init` altijd `missing dependency: superpowers` meldt.** Taak 2 pint
   dat de handleiding die melding en `.agents/skills` noemt.

---

### Task 1: Go-test — één `y` op stdin slaat `host.toml` over

**Files:**
- Test: `internal/initcmd/initcmd_test.go` (naast `TestHostTOMLHasItsOwnQuestion`, regel ~1042)

**Interfaces:**
- Consumes: de bestaande helpers `newRepo`, `run`, `withTailscale`, `exists`, `hostTOMLFor` in
  `initcmd_test.go`.
- Produces: niets voor latere taken. De handleiding (taak 2) leunt op dit gedrag.

- [ ] **Step 1: Schrijf de test**

```go
func TestOneApprovalLineSkipsHostTOML(t *testing.T) {
	root := newRepo(t)
	r := run(t, root, "y\n", withTailscale("100.64.0.1"))
	if r.code != 0 || !exists(root, ".lighthouse/manifest.toml") {
		t.Fatalf("rc %d\n%s\n%s", r.code, r.out, r.err)
	}
	if !strings.Contains(r.out, "Write "+hostTOMLFor(t)+"? [y/N] ") {
		t.Fatalf("out:\n%s", r.out)
	}
	if _, err := os.Stat(hostTOMLFor(t)); err == nil {
		t.Fatal("host.toml written on EOF")
	}
}
```

- [ ] **Step 2: Draai de test**

Run: `go test ./internal/initcmd/ -run TestOneApprovalLineSkipsHostTOML -v`
Expected: PASS. Het gedrag bestaat al; de test pint het vast.

- [ ] **Step 3: Mutatiecheck**

Laat `ask` in `internal/initcmd/initcmd.go` tijdelijk `true` geven bij een lege regel
(`if answer == "" { return true }`). Draai Step 2 opnieuw. Expected: FAIL met
`host.toml written on EOF`. Draai de wijziging terug met `git checkout internal/initcmd/initcmd.go`.

- [ ] **Step 4: Commit**

```bash
git add internal/initcmd/initcmd_test.go
git commit -m "test(init): één akkoordregel op stdin schrijft geen host.toml (#88)"
```

---

### Task 2: De handleiding en haar tests

**Files:**
- Create: `docs/site/agents/install.md`
- Create: `tests/test_agent_guide.py`

**Interfaces:**
- Consumes: `init_flags()` en `required_tools()` uit `tests/test_getting_started.py` (importeer
  ze, kopieer ze niet). `ROOT` en `SITE` uit `tests/test_site_chain.py`.
- Produces, voor taak 3 en 4, in `tests/test_agent_guide.py`:
  - `GUIDE: Path` = `SITE / "agents" / "install.md"`
  - `PASTE: str` = de plakzin uit Global Constraints
  - `guide_section(title: str) -> str`: de tekst tussen `## <title>` en de volgende `## `,
    zoals `section()` in `test_getting_started.py`

- [ ] **Step 1: Schrijf de tests**

`tests/test_agent_guide.py`. De koppen van de handleiding liggen vast, in deze volgorde:

```python
HEADINGS = [
    "What this is", "Ground rules", "Asking the user", "Look first", "Choices",
    "Install the binary", "Run lighthouse init", "Check", "Final report", "When something fails",
]
```

Tests, met deze asserts:

- `test_headings_in_order`: de `## `-koppen van `GUIDE` zijn precies `HEADINGS`.
- `test_paste_url_is_this_file`: de URL in `PASTE` min `https://corneeldh.github.io/lighthouse/`
  is `GUIDE.relative_to(SITE).as_posix()`.
- `test_init_flags_exist`: de `--flags` in code (backtick-spans en fenced blocks) van de hele
  handleiding zijn een subset van `init_flags()`. `{"pack", "no-pack", "upgrade"}` zit erin.
- `test_approval_goes_through_stdin`: `"printf 'y\\n' | lighthouse init"` staat in
  `guide_section("Run lighthouse init")`. Geen fenced block in de handleiding bevat `--yes`.
- `test_no_changes_is_what_init_prints`: `"No changes."` staat in `guide_section("Check")` én in
  `internal/initcmd/initcmd.go`.
- `test_help_rule`: `guide_section("Ground rules")` bevat `lighthouse init --help` en `sudo`. Geen
  fenced block in de handleiding bevat `sudo`.
- `test_superpowers_count`: het aantal units onder `superpowers` in `pack/core/requires.toml` (nu
  9) staat als cijfer in `guide_section("Choices")`, in de regel die `Superpowers` noemt.
- `test_every_required_tool_is_named`: `required_tools()` is een subset van de backtick-spans in
  `guide_section("Look first")`.
- `test_languages_are_the_config_languages`: lees `LANGUAGES` uit `pack/core/lib/config.py` met
  `ast` of een regex. De regel in `guide_section("Choices")` die `` `[project] language` `` noemt,
  heeft precies die waarden als backtick-spans van twee kleine letters. Die regel noemt `en` als
  aanbeveling bij elke andere taal: assert dat `"any other language"` erin staat.
- `test_harness_notes`: de handleiding noemt `AskUserQuestion`, `.agents/skills`,
  `missing dependency: superpowers` en `CODEX_MISSING`.
- `test_stops_when_already_installed`: `guide_section("Look first")` noemt
  `.lighthouse/manifest.toml` en `stop`.
- `test_binary_blocks`: `guide_section("Install the binary")` noemt
  `DOWNLOAD + "SHA256SUMS"` en `DOWNLOAD + f"lighthouse_{tab.replace('-', '_')}"` voor elke tab
  in `TABS` behalve `windows-amd64`, plus `sha256sum --check` en `shasum -a 256 -c`. Ze noemt ook
  `WSL`. `DOWNLOAD` en `TABS` importeer je uit `tests/test_site_download.py`.
- `test_final_report_fields`: `guide_section("Final report")` noemt `lighthouse version`,
  `init --upgrade` en `/lighthouse-start-issue`.

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_agent_guide.py`
Expected: FAIL, elke test met `FileNotFoundError` op `docs/site/agents/install.md`.

- [ ] **Step 3: Schrijf `docs/site/agents/install.md`**

Geschreven aan de agent, in de tweede persoon en in de gebiedende wijs, in het Engels. De inhoud
per kop komt uit de spec:

- *What this is*: één alinea; de officiële URL en `github.com/CorneeldH/lighthouse/releases` als
  bron (spec, besluit 3.1).
- *Ground rules*: spec, besluit 3.2. Gebruik letterlijk "Never use `sudo`" en de regel dat je
  alleen flags uit `lighthouse --help` en `lighthouse init --help` gebruikt.
- *Asking the user*: spec, besluit 3.3. `AskUserQuestion`, de eerste optie met "(Recommended)",
  anders een genummerde lijst met de aanbeveling eerst; één keuze tegelijk.
- *Look first*: spec, besluit 3.4. Een korte tabel met tool, pakket en wat je mist zonder, met
  dezelfde namen als de *Requirements* van `docs/getting-started.md`. Plus de stop bij een
  bestaande `.lighthouse/manifest.toml`.
- *Choices*: de tabel uit spec, besluit 4, met de twee opmerkingen voor Codex/OpenCode.
- *Install the binary*: de bash-blokken uit `docs/site/install.html` voor `linux-amd64`,
  `linux-arm64`, `darwin-arm64` en `darwin-amd64`, met de checksum vóór gebruik. Native Windows:
  meld dat de hooks bash nodig hebben, en raad WSL aan.
- *Run lighthouse init*: de zes stappen uit spec, besluit 5, met de reden voor `printf 'y\n'`.
- *Check*: `lighthouse init --upgrade` moet `No changes.` geven.
- *Final report*: het vaste blok uit spec, besluit 6, als fenced `text`-sjabloon.
- *When something fails*: spec, besluit 3.10. Elke melding letterlijk zoals `init` haar print;
  zoek de tekst op in `internal/initcmd/initcmd.go`.

- [ ] **Step 4: Draai de tests**

Run: `uv run pytest -q tests/test_agent_guide.py tests/test_site_pages.py`
Expected: PASS. `test_site_pages.py` controleert nu ook de relatieve links in de nieuwe `.md`.

- [ ] **Step 5: Mutatiecheck**

Zet tijdelijk `--yes` in een fenced block van *Run lighthouse init*, en verander de `9` in `8`.
Draai Step 4. Expected: FAIL in `test_approval_goes_through_stdin` en `test_superpowers_count`.
Draai terug.

- [ ] **Step 6: Commit**

```bash
git add docs/site/agents/install.md tests/test_agent_guide.py
git commit -m "docs(site): installatiehandleiding voor agents (#88)"
```

---

### Task 3: Plakblok en links op de site

**Files:**
- Modify: `docs/site/install.html` (na `<p id="release-version">`, de zin `Next:` op regel ~105,
  de footer op regel ~112)
- Modify: `docs/site/index.html:130` (footer)
- Modify: `docs/site/download.js:43` (de aanroep van `addCopyButtons`)
- Modify: `tests/test_site_download.py:189-195` (`test_footer_links`)
- Test: `tests/test_agent_guide.py`

**Interfaces:**
- Consumes: `PASTE`, `GUIDE` uit taak 2.
- Produces: `<section id="agent" class="agent-prompt">` op `install.html`, met daarin
  `<pre><code>` + `PASTE`.

- [ ] **Step 1: Schrijf de tests**

In `tests/test_agent_guide.py`:

- `test_paste_block_on_install_page`: `install.html` heeft een element met `id="agent"` (gebruik
  `ElementById` uit `tests/test_site_pages.py`). De tekst bevat `PASTE`. Het staat vóór de eerste
  `data-platform`.
- `test_paste_block_has_copy_button`: `download.js` geeft `.agent-prompt` mee aan
  `addCopyButtons` (regex op `addCopyButtons\(\[[^\]]*\.agent-prompt`).
- `test_guide_is_linked`: `install.html` en `index.html` hebben elk `href="agents/install.md"`.
  Geen van beide heeft nog `issues/88`.

In `tests/test_site_download.py`, `test_footer_links`: vervang `"issues/88"` in de lijst door een
aparte assert op `href="agents/install.md"`.

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_agent_guide.py tests/test_site_download.py`
Expected: FAIL in de drie nieuwe tests en in `test_footer_links`.

- [ ] **Step 3: Pas de site aan**

- `install.html`: een `<section id="agent" class="agent-prompt">` met een `<h2>` ("Install with
  your agent"), één zin uitleg en `<pre><code>` + `PASTE`. Plaats het na
  `<p id="release-version">` en vóór `download-lead`. De footerlink wordt
  `<a href="agents/install.md">Agent guide</a>`. De zin `Next:` krijgt "or let your agent do it"
  met een link naar `agents/install.md`.
- `index.html`: de footerlink wordt `<a href="agents/install.md">Agent guide</a>`.
- `download.js`: `addCopyButtons([...panels, ...root.querySelectorAll(".agent-prompt")])`. Pas
  het commentaar bovenaan aan, zodat het de copy-knop van het plakblok noemt.
- `map.css`: alleen als het blok zonder eigen stijl slecht oogt. Hergebruik de stijl van
  `.platform pre`.
- Draai `uv run python -m tests.site_version`.

- [ ] **Step 4: Draai de sitetests**

Run: `uv run pytest -q tests/test_agent_guide.py tests/test_site_download.py tests/test_site_pages.py tests/test_site_assets.py`
Expected: PASS.

- [ ] **Step 5: Kijk in de browser**

Run: `python3 -m http.server --directory docs/site 8000`, open
`http://localhost:8000/install.html`. Controleer: het plakblok staat bovenaan, de knop "Copy"
kopieert precies `PASTE`, en `agents/install.md` opent.

- [ ] **Step 6: Commit**

```bash
git add docs/site tests/test_agent_guide.py tests/test_site_download.py
git commit -m "feat(site): plakzin voor agents op de installatiepagina (#88)"
```

---

### Task 4: Startgids en changelog

**Files:**
- Modify: `docs/getting-started.md` (sectie `## Install`, regel ~36)
- Create: `changelog.d/issue-88-agent-installatiehandleiding.md`
- Test: `tests/test_agent_guide.py`

**Interfaces:**
- Consumes: `PASTE` uit taak 2; `section()` uit `tests/test_getting_started.py`.

- [ ] **Step 1: Schrijf de test**

`test_getting_started_names_the_paste`: `section("Install")` uit `test_getting_started.py` bevat
`PASTE` en een link `https://corneeldh.github.io/lighthouse/agents/install.md`.

- [ ] **Step 2: Draai de test en zie hem falen**

Run: `uv run pytest -q tests/test_agent_guide.py::test_getting_started_names_the_paste`
Expected: FAIL.

- [ ] **Step 3: Pas de startgids aan en schrijf het fragment**

- `docs/getting-started.md`: bovenaan `## Install` één alinea: wil je dat je agent het doet, plak
  dan deze zin (fenced `text`-blok met `PASTE`). Daarna "The agent follows
  [the agent guide](https://corneeldh.github.io/lighthouse/agents/install.md)". Gebruik geen
  relatieve link naar `site/agents/install.md`: de lezer van de startgids zit op GitHub, niet op
  Pages.
- `changelog.d/issue-88-agent-installatiehandleiding.md`: `type: added` (nieuw gedrag dat een
  gebruiker kan gebruiken, `docs/releasing.md`), en één Engelse zin over de plakzin en de
  handleiding voor agents. De bestandsnaam is de branchnaam (`docs/releasing.md`).

- [ ] **Step 4: Draai de tests**

Run: `uv run pytest -q tests/test_agent_guide.py tests/test_getting_started.py tests/test_site_pages.py`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/getting-started.md changelog.d/issue-88-agent-installatiehandleiding.md tests/test_agent_guide.py
git commit -m "docs(getting-started): plakzin voor de agent (#88)"
```

---

### Task 5: Bewijsrun door Corneel (handmatig, op macOS in Claude Code)

Dit is de laatste taak, en Corneel voert hem uit. Een subagent doet hem niet.

**Files:**
- Modify: `docs/site/agents/install.md`, alleen als de run iets laat zien

- [ ] **Step 1: Push de branch**

```bash
git push -u origin issue-88-agent-installatiehandleiding
```

Expected: de raw-URL hieronder geeft de handleiding (`curl -fsS <url> | head -3`).

- [ ] **Step 2: Corneel draait de run**

Op de Mac, in een bestaande eigen GitHub-repo zonder lighthouse en zonder Node, in een nieuwe
sessie van Claude Code. Corneel plakt alleen deze zin:

```text
Read https://raw.githubusercontent.com/CorneeldH/lighthouse/issue-88-agent-installatiehandleiding/docs/site/agents/install.md and install lighthouse in this repo.
```

Hij beantwoordt de vragen live. De binary is release `v0.2.0` (darwin).

Expected: elke keuze uit spec, besluit 4, komt als vraag. Niets wordt geïnstalleerd of geschreven
vóór een ja. De laatste `lighthouse init --upgrade` geeft `No changes.`. Het eindrapport staat in
beeld.

- [ ] **Step 3: Repareer wat misging**

Elke afwijking wordt een fix in `docs/site/agents/install.md`, met een test in
`tests/test_agent_guide.py` als het een feit is. Commit, push, en terug naar Step 2. Een defect in
`lighthouse init` zelf wordt een nieuw issue (`/lighthouse-write-issue`), geen fix hier.

- [ ] **Step 4: Leg het bewijs vast**

Het eindrapport van de groene run, en een samenvatting van de gestelde vragen met de antwoorden,
gaan in de PR-body onder het bewijs voor AC 5. Daar staat ook de afwijking: een bestaande repo
zonder lighthouse, in plaats van een schone repo. `/lighthouse-verify-pr` schrijft de body.
