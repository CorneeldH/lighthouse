# Ontwerp: testskill zonder tautologieën

Issue: #22 · Datum: 2026-10-24

## Aanleiding en doel

Geen incident, wel inspiratie. Een vergelijking van `paia-python-schrijven` met de skills van
kenn-io leverde ideeën op die lighthouse sterker maken:

- [agentsview `testing-without-tautologies`](https://github.com/kenn-io/agentsview/blob/main/.agents/skills/testing-without-tautologies/SKILL.md)
- [forge `small-change-verification`](https://github.com/kenn-io/forge/blob/main/skills/small-change-verification/SKILL.md)

Het doel: een agent schrijft tests die falen als het beschermde gedrag breekt, en meldt "klaar"
pas als dat aantoonbaar is.

## Scope

#22 levert drie dingen:

1. een nieuwe, taalneutrale testskill in `pack/core`;
2. een herziening van `pack/python/skills/python-writing` (de opvolger van `paia-python-schrijven`);
3. een nieuwe rule in het core-blok.

**Buiten scope:**

- Taak 3 uit het issue (Go-skill). Een nieuw taalpakket `pack/go` vraagt een eigen ontwerp en
  wordt een eigen issue (zie *Vervolg*).
- `pack/shell/skills/shell-gates` aansluiten op de testskill (verwijzing, classificatiezin,
  shell-vorm van de normen). Besloten: niet in #22, een vervolgissue (zie *Vervolg*). Tot dan
  noemt de testskill shell-gates alleen "if present".
- context-sync en de steekproef van specs en plannen (al buiten scope in het issue).

## Non-goals en anti-goals

- **Geen** Go-, agentsview- of forge-specifieke inhoud in de testskill: geen `require.X`, geen
  PG-pariteit, geen Playwright, geen Svelte, geen Huma.
- **Anti-goal:** een testskill die een agent ertoe brengt de eigen contracttests van lighthouse
  te "repareren". Die tests lezen skilltekst en pinnen woorden, en dat is daar juist goed (zie
  *Contracttests tegen codetests*).
- **Geen** wijziging aan de bestaande python-writing-onderdelen die al in orde zijn: de
  issue-archeologie (#562, #806, #1016) is al weg, en `tests/pack/python/test_python_writing.py`
  verbiedt `#\d{3,}`. Een kolom "Gemeten" bestaat niet. Die twee taken uit het issue zijn al
  gedaan.

## 1. De testskill `lighthouse-testing`

**Plaats:** `pack/core/skills/testing/`. Core, omdat de skill voor elke repo en elke taal geldt.
Of het één gedeelde `SKILL.md` wordt of twee varianten (`claude/`, `agents/`), laat ik aan de
bouwer; `pack/core/skills/instrument/SKILL.md` is het voorbeeld van een gedeelde.

**Description:** triggert op een test schrijven, aanpassen, repareren of reviewen, op mocks,
fakes en asserts, op het aanpassen van een test na een falende run, en op het plannen van een
teststrategie, vóór er een bestand open is. Hij triggert op het werk, niet op de taal. Negatieve
grens: de taalspecifieke uitwerking staat in de taalskill (`lighthouse-python-writing` en
`lighthouse-shell-gates`, if present).

**Verhouding tot `superpowers:test-driven-development`** (mijn call): aanvullend. TDD draagt de
rood-groen-lus (*wanneer* je een test schrijft); deze skill draagt *wat een goede test is*. De
description noemt die grens. Noemt de skilltekst `superpowers:`, dan eist de contracttest
`superpowers` in `pack/core/requires.toml`; core heeft die al.

**Inhoud** (Engels, zoals alle instructies):

- **Kernvraag** vóór elke test: welke productiewijziging laat deze test falen? Kun je die niet
  noemen, ontwerp de test dan opnieuw.
- **Assert waarneembaar effect**: returnwaarde, geschreven bestand, stdout of exitcode, fout.
  Geen private state.
- **Specifieke fakes**: controleer argumenten, aantal aanroepen en branch waar die bij het
  contract horen.
- **Aparte doubles per branch**: succes, fout en malformed delen geen fake.
- **Mock het subject niet**: mock grenzen en trage of niet-deterministische collaborators, niet
  wat je test.
- **Eerst onderzoeken, dan de verwachting aanpassen**: draai een verwachte waarde niet om om een
  rode test groen te krijgen. Beslis eerst of de productiewijziging bedoeld is.
- **Geen mirror assertions**: bereken de verwachte waarde niet met de logica die je test. Gebruik
  letterlijke waarden, met de hand gecontroleerde fixtures of invarianten.
- **Geen upstream-gedrag testen**: test de eigen grens, niet dat een library doet wat hij
  belooft.
- **Geen bevestiging van de huidige code**: geen tests op een kale toewijzing, een getter of
  doorgeven, tenzij er validatie, een default, een afleiding of een side effect in zit.
- **Shellscripts uitvoeren, niet lezen**: draai het script tegen gecontroleerde invoer en assert
  de output, de side effects of de exitcode.
- **Geen negative-existence tests** na verwijderen. Het verwijderen bewijst zichzelf, en de
  vervanger heeft een gedragstest. Dit sluit aan op *replace, don't layer* in python-writing.
- **Mutatiecheck bij afronden**: muteer de productiecode in gedachten. Bij elke realistische
  mutatie moet minstens één test falen: een verkeerde constante of branch, een ontbrekende
  statewijziging of side effect, een lege of default return, of een ontbrekende validatie op
  leeg, nul of malformed. Faalt er geen, dan is de test waarschijnlijk tautologisch.
- **Rode vlaggen**: de taalneutrale lijst uit agentsview.

### Contracttests tegen codetests

De skill maakt expliciet onderscheid tussen twee soorten tests.

- **Codetests** testen gedrag van code. Daarvoor gelden alle regels hierboven.
- **Contracttests op instructietekst** (skills, agents, `AGENTS.md`) lezen de tekst en pinnen
  woorden. Bij een instructiebestand *is* de tekst het product. Daar is de tekst lezen dus geen
  rode vlag. Voorbeelden in lighthouse: `tests/pack/test_skill_contracts.py` en
  `tests/pack/python/test_python_writing.py`.

Een verbod in een contracttest mag als het een **blijvende invariant** is ("een skill noemt geen
issuenummers"). Het mag niet als het alleen bewaakt dat iets weg blijft na een migratie: dat is
een negative-existence test, ook in een contracttest.

### De classificatiezin en de klassentabel

Vóór het editen schrijft de agent één zin:

```text
This is a <class> change; the likely regression surface is <observable behavior>.
```

De klassen staan **centraal in deze skill**, taalneutraal, elk met een regressievraag en een
minimale check. Een startpunt (de bouwer mag aanscherpen, de klassen blijven taalneutraal):

| Klasse | Regressievraag | Minimale check |
|---|---|---|
| `pure-logic` | welke uitkomst verandert bij welke input? | unittest met letterlijke verwachting |
| `io/filesystem` | welk bestand komt anders op schijf? | test tegen een tijdelijke map |
| `cli-output` | wat ziet de gebruiker anders in stdout of exitcode? | het commando draaien, output asserten |
| `hook/gate` | wat blokkeert nu wel of niet meer? | het script draaien met een blokkerende en een doorlatende invoer |
| `config` | welk gedrag schakelt om? | een test per stand |
| `test-only` | welke productiewijziging vangt de test nu wel of niet? | de mutatiecheck |

Vallen er meerdere klassen onder een wijziging, dan geldt de strengste check, of beide.

**Waar de zin staat** (mijn call): in de chat, vóór de eerste edit. Niet in commitbericht of
PR-body; daarvoor zouden `/lighthouse-verify-pr` en de PR-template mee moeten, en dat valt buiten
#22.

Waarom centraal: de klassen gaan over *wat* er breekt, en dat is niet taalgebonden. Het *hoe
testen* staat in de taalskill. Eén lijst, één onderhoudspunt. Gekozen "voor nu": blijkt het
openen van een tweede skill op het moment van editen in de praktijk niet te werken, dan krijgt
elke taalskill een eigen tabel.

## 2. Herziening `lighthouse-python-writing`

Beide varianten (`claude/` en `agents/`). Het verschil tussen de varianten blijft beperkt tot de
OpenCode-tekst, zoals `test_python_writing.py` pint.

- **Description** triggert ook bij plannen: een refactor, modules samenvoegen, een teststrategie
  kiezen. De bestaande "NOT for … `lighthouse-shell-gates`" blijft.
- **Classificatiezin vóór editen**: python-writing vraagt om de zin en verwijst voor de klassen
  naar `lighthouse-testing`. Geen eigen tabel.
- **Blok "Klaar melden"** (Engels, bv. `## Reporting done`): draai aan het eind alle KPI-rijen
  en meld per rij het commando, de uitkomst, de gesynchroniseerde extras en wat niet meetbaar was.
  Dit sluit aan op de bestaande Non-negotiables (een nul zonder extras is geen nul, drie
  uitkomsten).
- **Verwijzing naar `lighthouse-testing`** voor de testnormen.
- **Python-uitwerking van de testnormen**, kort, als concrete vorm van de algemene regel:
  - letterlijke verwachting: `pytest.mark.parametrize` met letterlijke `expected`, niet berekend;
  - specifieke fakes: `create_autospec` of `spec=`, en `assert_called_once_with` waar het
    argument bij het contract hoort;
  - subject niet mocken: echte `tmp_path`, `monkeypatch` en `capsys` in plaats van een gemockte
    collaborator;
  - shellscripts: uitvoeren via `subprocess.run` tegen `tmp_path`, niet de bron lezen.

  De bestaande valkuilen ("patch target is de importnaam", "`assert_called_once()` toevoegen")
  blijven staan.

  **Framing** (mijn call): python-writing zegt nu dat projectnormen in `.claude/rules/python.md`
  (if present) staan, "so one place carries them". Dat principe blijft. De nieuwe sectie heet de
  *Python-vorm van `lighthouse-testing`*: de norm staat in de testskill, python-writing geeft
  alleen het Python-gereedschap ervoor. De pairing-tabel krijgt een rij voor `lighthouse-testing`
  (draagt: de testnormen en de klassentabel; wanneer: bij elke test en vóór het editen).

**Niet aanraken:** de kop `## Reaching a complexity limit: deepen, don't split`
(`ratchet.py` `COMPLEXITY_SKILL` leest hem), de KPI-tabel (≥ 4 rijen met een commando in
backticks) en alles wat `test_python_writing.py` verder pint.

## 3. De rule

Een nieuwe bullet in `pack/core/instructions/AGENTS.md`. Daarmee geldt hij in elke lighthouse-repo
en in elke harness, ook Codex en OpenCode, waar geen hooks draaien. Tekst:

> - Finish what you are already authorised to do; never end on an offer to do it ("ready to push if you want" is not done).

De tekst is goedgekeurd en gaat letterlijk het blok in.

**Grens:** de rule gaat alleen over acties waarvoor de toestemming er al is, uit de opdracht of
een eerdere "ja". Een onomkeerbare of naar buiten gerichte actie *zonder* toestemming vraag je
nog steeds eerst. Die grens is ontwerpmotivatie en gaat niet het blok in: "already authorised"
in de rule draagt hem.

## Randvoorwaarden

- **Woordbudget van het core-blok:** maximaal 250 woorden (`tests/test_instruction_files.py`).
  Het blok is nu 226 woorden. De rule (26) en de skillregel `/lighthouse-testing` (~8) brengen
  het op ~260. **Mijn call:** de grens blijft en de rule blijft letterlijk; de ruimte komt uit
  inkorten van bestaande regels zonder betekenisverlies:
  - de bullet over `heading:<key>`, met behoud van wat
    `test_core_block_says_how_to_read_a_heading` pint
    (`python3 .lighthouse/lib/lighthouse_lib/headings.py <key>`, "never the literal
    `heading:<key>`", "reading accepts every language variant", `.lighthouse/headings.toml`);
  - de omschrijvingen in de skilllijst (bv. "read-only triage, one route per issue" → "read-only
    triage").
- **Skilllijst in het core-blok:** elke map in `pack/core/skills` moet als `` `/lighthouse-<skill>` ``
  in het blok staan (zelfde test).
- **Gepinde lijsten:** `internal/packs/core_test.go` pint de geïnstalleerde core-bestanden en de
  tools.
- **Herkomst** (mijn call): de testskill is eigen werk, vers geschreven, met agentsview en forge
  als inspiratie, niet als overgenomen tekst. forge heeft geen herkenbare licentie
  (`NOASSERTION`), dus geen formulering van forge overnemen; agentsview is MIT, maar ook daar
  schrijven we zelf. Frontmatter volgens `/lighthouse-instrument`: `origin: own`,
  `source: "#22"`, de twee URL's als inspiratie. In `pack/SOURCES.toml` (`tests/test_sources.py`)
  krijgen de nieuwe bestanden `new = true`; de bestaande python-writing-entries
  (`source_path`/`source_sha` naar paia) blijven.
- **Contracttest voor skills** (`tests/pack/test_skill_contracts.py`) dekt de nieuwe skill
  automatisch: naam, paden, configsleutels, links, beide varianten, overlap van descriptions,
  `requires.toml` per gebruikt tool. Woorden als `controller` of `release` in een core-skill
  vragen "if present" in de buurt.
- **Dogfood:** na de wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade`, en
  de geïnstalleerde kopie wordt gecommit. CI eist `No changes.`
- **Craft van de skill:** via `/lighthouse-instrument` (laag en herkomst) en
  `superpowers:writing-skills`; valideren met `.lighthouse/scripts/check-instructions.sh`.

## Aan de bouwer

- Eén gedeelde `SKILL.md` of twee varianten voor de testskill.
- De exacte Engelse formulering van de skilltekst, de rode vlaggen en het blok "Klaar melden".
- De klassentabel aanscherpen (namen, checks), zolang hij taalneutraal blijft.
- Of de testskill een eigen per-skilltest krijgt naast de contracttest.
- De volgorde van de commits.

## Vervolg

Een nieuw issue voor shell-gates: `pack/shell/skills/shell-gates` (beide varianten) verwijst naar
`lighthouse-testing`, vraagt de classificatiezin en geeft de shell-vorm van de normen (script
draaien tegen gecontroleerde invoer, exitcode en output asserten, de bron niet lezen).

Een nieuw issue voor de Go-skill (`pack/go`), met daarin wat al bekend is:

- het concept "Go schrijven in deze repo" uit #22 als startpunt;
- `go test` standaard met `-shuffle=on`, en `-count=1` alleen bij verdenking van een verouderd
  cacheresultaat (correctie op het concept, dat `-count=1` standaard had);
- de Go-uitwerking van de testnormen: table-driven met letterlijke `want`, `t.TempDir()`,
  `httptest`, `require` tegen `assert` (alleen bij testify);
- de hooks, `.golangci.blocking.yml` en `.claude/rules/go.md` uit het concept bestaan in
  lighthouse niet en moeten in dat ontwerp een plek krijgen;
- de classificatiezin, met verwijzing naar de klassentabel in `lighthouse-testing`.
