# Naamgeving van skills, hooks en agents (#142)

## Doel en waarom

Op de ketenkaart (#99) lezen de namen van de onderdelen niet als wat ze zijn. `testing` klinkt
als een stap, maar is de norm voor een goede test. `worktree-guard` waarschuwt alleen, terwijl
`main-commit-guard` blokkeert: "guard" betekent zo twee dingen. Een naam moet zeggen wat voor
ding het is en wat het doet, zodat de kaart en een lijst met skills zonder uitleg leesbaar zijn.

Dit ontwerp legt een conventie per vorm vast, hernoemt elk onderdeel in `pack/` dat er niet aan
voldoet, en zorgt dat de conventie bijgehouden wordt: in de skill die instructies plaatst en in
een controle.

## Doelen

- Een conventie per vorm (skill, hook, agent), vastgelegd waar `/lighthouse-instrument` (straks
  `place-instruction`) haar plaatst.
- Elke skill en hook in `pack/` voldoet eraan; alle verwijzingen gaan mee.
- `go run ./cmd/lighthouse init --upgrade` geeft daarna `No changes.`
- `init --upgrade` op een repo met de oude namen laat geen hook of skill onder de oude naam
  achter.
- `check-instructions.sh` faalt op een hooknaam die de conventie breekt.

## Non-goals

- `spec-adversary` hernoemen: een eigen issue, zoals #142 zegt.
- Het voorvoegsel `lighthouse-` en de map `.claude/hooks/lighthouse/`: die blijven.
- Een uitzonderingsmechanisme voor wie van de conventie wil afwijken. Besloten: nu niet, de
  controle faalt.
- Het CEDA-veld `metadata.type` (of `subtype`). De soort volgt uit de naam; een afgeleid veld dat
  met de hand bijgehouden wordt, drijft af (de redenering die `instrument` stap 5 al voert).
- Modulenamen onder `pack/*/lib/` en `pack/*/scripts/gate_*.py`: alleen de strings die een
  hooknaam noemen gaan mee.

## Anti-goals

- Een upgrade die een lokaal aangepaste kopie onder de oude naam stilzwijgend verwijdert.
- Een release met half oude en half nieuwe namen.
- Een sleutel in `lighthouse.toml` die een gebruiker moet aanpassen.
- Historische tekst herschrijven: `changelog.d/`, `CHANGELOG.md` en
  `docs/developer/planning/**` blijven zoals ze zijn.

## De conventie

Twee soorten skills, te onderscheiden aan de naam:

| Vorm | Wat het is | Naam | Voorbeeld |
|---|---|---|---|
| workflowskill | een stappenreeks of beslislogica; de sessie *doet* de taak | werkwoord + object | `write-issue`, `review-spec` |
| standard | het keurmerk: wat wij kwaliteit vinden plus onze voorkeur en stijl; geladen naast een andere taak | `<onderwerp>-standard` | `go-standard`, `test-standard` |
| hook, gate | de actie komt er niet doorheen: registratie op `PreToolUse`, `Stop` of `SubagentStop` die exit 2 geeft | `<object>-gate` | `main-commit-gate` |
| hook, check | meldt achteraf of waarschuwt, met of zonder exit 2; de actie is al gebeurd | `<object>-check` | `md-link-check` |
| agent | een beroep | `<object>-<beroep>` | `issue-impact-scout` |

"Standard" komt overeen met `reference` (subtype `knowledge`) uit de CEDA-skillsontologie.

Een hookscript met meer modi heet naar zijn sterkste modus: kan één modus de actie tegenhouden,
dan is het een gate. Zo is `commit-tests` (pre blokkeert, post meldt) een gate.

## Hernoemingen

Besloten door de eigenaar, regel voor regel (2026-10-31).

### Skills

| Nu | Wordt |
|---|---|
| `close-issue`, `fix-pr`, `route-issue`, `start-issue`, `write-issue`, `refresh-expo-lockfiles` | blijven |
| `verify-pr` | `write-pr` |
| `handoff` | `write-handoff` |
| `instrument` | `place-instruction` |
| `spec-adversarial-review` | `review-spec` |
| `plan-adversarial-review` | `review-plan` |
| `worktree-audit` | `audit-worktrees` |
| `testing` | `test-standard` |
| `go-writing` | `go-standard` |
| `python-writing` | `python-standard` |
| `shell-gates` | `shell-standard` |

Geïnstalleerd met voorvoegsel: `lighthouse-write-pr`, enzovoort. Het `name:`-veld in beide
varianten (`claude/` en `agents/`) gaat mee; een afwijking is een laadfout
(`internal/packs/packs.go:447`).

### Hooks

| Nu | Event | Wordt |
|---|---|---|
| `main-commit-guard.sh` | PreToolUse | `main-commit-gate.sh` |
| `pr-create-gate.sh` | PreToolUse | blijft |
| `commit-tests.sh` | PreToolUse + PostToolUse | `commit-test-gate.sh` |
| `usage-guard.sh` (+ map `usage-guard/`) | Stop, SubagentStop, PostToolUse, UserPromptSubmit | `usage-gate.sh` (+ `usage-gate/`) |
| `go-check.sh` | Stop | `go-lint-gate.sh` |
| `ty-check.sh` | Stop | `ty-gate.sh` |
| `worktree-guard.sh` | PreToolUse, waarschuwt alleen | `worktree-check.sh` |
| `worktree-linkcheck.sh` | PostToolUse | `md-link-check.sh` |
| `code-language.sh` | PostToolUse | `code-language-check.sh` |
| `bash-write-gates.sh` | Pre (alleen baseline) + PostToolUse | `bash-write-check.sh` |
| `gofmt.sh` | PostToolUse | `gofmt-check.sh` |
| `ruff-format.sh` | PostToolUse | `ruff-format-check.sh` |
| `shellcheck.sh` | PostToolUse | `shell-lint-check.sh` |

### Woorden die meegaan

- "write gates" wordt "write checks" (CLAUDE.md, AGENTS-blok, docs, hookmeldingen).
- "test gate" en `[test_gate]` in `lighthouse.toml` blijven: `commit-test-gate` is een gate.
- `docs/usage-guard.md` wordt `docs/usage-gate.md`.
- `[usage_guard]` in `lighthouse.toml` en de statusmap `~/.local/state/lighthouse/usage-guard`
  blijven (mijn keuze). De sectie hernoemen verandert een sleutel voor gebruikers, en de
  statusmap wordt gedeeld door alle repo's op een machine: repo's op een oude en een nieuwe
  versie zouden elk hun eigen "eens per venster" krijgen. Dit is een bekende rest van de oude
  naam.
- Agents: `issue-impact-scout` blijft; `spec-adversary` buiten scope.

## Aanpak

Overwogen:

- **A. Eén PR, een commit per stap.** Gekozen. Een release bevat nooit een tussenstand, en de
  controle kan pas landen als de eigen namen kloppen; dat dwingt de volgorde.
- **B. Drie PR's (skills, hooks, conventie + controle).** Kleinere reviews, maar tussen releases
  half oude en half nieuwe namen, en gebruikers maken twee upgrades met hernoemingen door.
- **Afgevallen: een tabel oud → nieuw in Go.** Niet nodig (de enige Go-wijziging is het opruimen
  van lege mappen, zie hieronder): de manifest-reconcile maakt van een
  hernoeming al Remove van de oude unit plus Add van de nieuwe (`internal/reconcile/reconcile.go:79`),
  en `settings.Apply` haalt een lighthouse-registratie weg die niet meer gewenst is
  (`internal/settings/settings.go:118`).

Volgorde van de commits:

1. Skills hernoemen, met alle verwijzingen; `init --upgrade`, installatiekopie mee.
2. Hooks hernoemen, met "write gates" → "write checks"; `init --upgrade`, installatiekopie mee.
3. De conventie in `place-instruction` stap 6 (naamcontract). Die skill heeft één gedeelde
   `SKILL.md`, geen `claude/`- en `agents/`-variant.
4. De naamcontrole in `check-instructions.sh`, met test.
5. Opruimen van lege mappen in `init`, en een test voor de upgrade van een repo met oude namen.
6. Een fragment in `changelog.d/`.

Elke commit is groen op zichzelf (`[test_gate] command`).

## Waar de conventie staat

`place-instruction` stap 6 heeft al het naamcontract (het voorvoegsel). Daar komen de vormregels
uit de tabel hierboven bij, met de reden in één zin per vorm. Stap 5 krijgt één zin: `type` blijft
weg omdat de naam de soort draagt. `AGENTS.md` krijgt niets: de regel geldt bij het maken van een
onderdeel, en dat loopt via `place-instruction`.

## De controle

Een nieuwe check in `pack/core/scripts/check-instructions.sh`, uitkomst `ok`, `FAIL` of
`SKIPPED` zoals de andere. Hij leest alleen de lighthouse-hooks: registraties in
`.claude/settings.json` waarvan het commando onder `.claude/hooks/lighthouse/` valt (dezelfde
grens als `packs.IsLighthouseCommand`). De eigen hooks van een project vallen erbuiten: zonder
uitzonderingsmechanisme zou een repo met een eigen `format.sh` blijvend falen. Voor die hooks is
de conventie advies in `place-instruction`. Een streng instapniveau kan de controle later
uitbreiden; dat staat als kandidaat op #19.

- elk lighthouse-hookscript eindigt op `-gate` of `-check`;
- een `-gate` heeft minstens één registratie op `PreToolUse`, `Stop` of `SubagentStop`;
- een `-check` heeft geen registratie op `Stop` of `SubagentStop`.

Of een `-check` op `PreToolUse` echt nooit blokkeert, en of een workflowskill met een werkwoord
begint, is statisch niet betrouwbaar te meten; dat blijft proza in `place-instruction`.

## Upgrade van een bestaande installatie

Onaangeraakte kopieën onder de oude naam verdwijnen bij `init --upgrade`: bestand, map en
registratie (`settings.Apply` haalt een lighthouse-registratie weg die niet meer gewenst is).

Is één kopie onder een oude naam lokaal aangepast, dan is dat een Conflict, en weigert
`init --upgrade` de hele upgrade zonder iets te schrijven (`internal/initcmd/initcmd.go:184`,
`refused: N locally modified unit(s); rerun with --force to overwrite`). Met `--force` verdwijnen
de oude kopie en haar registratie. Besloten: zo laten. Het is hetzelfde gedrag als bij elk ander
aangepast bestand dat een update krijgt, en er blijft nooit iets half achter. Het
changelog-fragment noemt de weigering en de twee uitwegen (de lokale wijziging terugdraaien, of
`--force`).

**Lege mappen** (mijn keuze). `apply` verwijdert nu alleen bestanden (`initcmd.go:588`), dus
`.claude/skills/lighthouse-verify-pr/`, `.agents/skills/lighthouse-verify-pr/` en
`.claude/hooks/lighthouse/usage-guard/` zouden leeg blijven staan. Na het verwijderen van een
bestand ruimt `init` de oudermappen op die daardoor leeg zijn geworden, tot aan de
installatiewortel (`.claude/skills/`, `.agents/skills/`, `.claude/agents/`,
`.claude/hooks/lighthouse/`). Een map die al leeg was, of die nog iets anders bevat, blijft.

De test toont drie gevallen: een onaangeraakte installatie met oude namen (na de upgrade geen
bestand, map of registratie onder een oude naam), een aangepaste oude kopie (weigering, niets
geschreven), en dezelfde met `--force` (oude kopie en registratie weg).

## Verwijzingen die meegaan

Uit de verkenning; de bouwer zoekt per naam opnieuw met `git grep`:

- `pack/**`: skills, agents (descriptions die een skill noemen), `hooks.json`,
  `requires.toml`, `pack/SOURCES.toml`, `pack/*/instructions/AGENTS.md`, `pack/core/lib/*.py`
  (o.a. `bash_writes.py`, `usage_guard.py`, `pr_body_gate.py`), KPI-scripts.
- Lighthouse's eigen `CLAUDE.md` (hooklijst) en het managed block in `AGENTS.md` (via upgrade).
- `docs/` (o.a. `usage-guard.md`, `getting-started.md`, `install.md`, `developer/labels.md`),
  `docs/site/data/chain.json`, `docs/site/data/packs.json`, `tests/fixtures/site/map.json`.
- Tests: `internal/**/*_test.go`, `e2e/e2e_test.go`, `tests/**`.
- Geïnstalleerde kopieën (`.claude/`, `.agents/`, `.lighthouse/`, `manifest.toml`): alleen via
  `init --upgrade`, nooit met de hand.

De KPI-scripts zoeken op `lighthouse-start-issue` en `lighthouse-route-issue`; die namen blijven,
dus oude metingen blijven geldig. `measure_fix_pr_kpi.py` noemt `lighthouse-fix-pr`, dat ook
blijft.

## Aan de bouwer

- De naam van `LIGHTHOUSE_UNDER_BASH_GATES` en andere interne variabelen.
- De vorm van de upgrade-test (e2e of `internal/initcmd`).
- Hoe de nieuwe check in `check-instructions.sh` de hooks uit `settings.json` leest.
