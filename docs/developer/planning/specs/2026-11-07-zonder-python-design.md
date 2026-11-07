# Ontwerp: Lighthouse zonder Python

Milestone: *Lighthouse zonder Python* (nieuw) · Datum: 2026-11-07 · Bouwt op M0 (besluit 1 van
[de M0-spec](2026-10-17-m0-pakket-design.md): "Herbouw in Go komt in M1/M2") en gaat vóór het
Windows-issue van [M3](2026-11-07-m3-tweede-omgeving-design.md) (#90).

## Aanleiding en doel

In M0 ging de Python van paia als tussenstap mee in `pack/core`, omdat herbouwen in Go toen te
groot was. Die herbouw is er niet gekomen. Daardoor heeft elke host nog `python3` ≥ 3.11 nodig
(`tomllib`), naast de lighthouse-binary. De M3-spec moet die eis per OS uitleggen. Ook het
Windows-werk wordt er zwaarder door: een hook is daar bash plus Python onder Git Bash.

**Doel:** lighthouse draait op een host zonder Python. Alles wat `lighthouse init` installeert
roept de binary aan, niet `python3` en niet een eigen bash-hook met logica.

**Klaar als:** een verse `lighthouse init` met de pakketten `core`, `go`, `node`, `shell` en
`expo` werkt op een host zonder `python3` op het PATH. Dat betekent: de hooks geven hetzelfde
oordeel als nu op dezelfde payloads, en elke skill-opdracht en elk script draait. Een CI-job
bewijst dat (zie *Bewijs*).

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Elke hook van `core` wordt helemaal Go: `lighthouse hook <naam> [stand]`.
- Elk script en elke lib-aanroep die een skill doet, wordt een subcommando van de binary.
- De taalpakketten `go`, `node`, `shell` en `expo` hebben geen Python meer nodig.
- De binary roept zelf geen `python3` meer aan (`internal/initcmd/workflows.go`,
  `internal/deps`).
- `init --upgrade` ruimt in elke repo op wat `pack/core` onder `.lighthouse/lib/` en
  `.lighthouse/scripts/` installeerde. Wat `pack/shell` en `pack/python` daar zetten, blijft.
  *(eigen keuze na spec-review, r1-f13)*

**Niet-doelen**

- `pack/python` blijft Python. Dat is het taalpakket voor Python-repo's, en daar is Python er
  toch al. Alleen zijn aanroepen van de core-lib gaan over op de binary (§5).
- Python in deze repo die niet wordt geïnstalleerd: `tests/` (pytest mag blijven), `tools/`,
  `ci/` en de site-tests.
- De bash-hooks van de taalpakketten worden geen Go: `gofmt-check`, `go-lint-gate`,
  `shell-lint-check`. Ze roepen vooral een linter aan. Wat ze nu met Python doen, gaat over op
  de binary (§5): `go-lint-gate` gebruikt `python3` voor `turn_roots.py` en leest
  `usage-gate/alarm.sh`, `shell-lint-check` gebruikt `config.py` en `python3 -c`.
- Nieuwe checks of ander gedrag. Een hook geeft na de ombouw hetzelfde oordeel als ervoor.

**Anti-doelen.** Dit telt als mislukt, ook als het technisch werkt:

- Een gate die stil verdwijnt. Kan een hook niet checken (geen binary, kapotte config), dan zegt
  hij `COULD NOT CHECK`, zoals nu.
- Een Python-module die na de milestone naast zijn Go-versie blijft bestaan. Tijdens de
  milestone mag dat alleen zo: een PR zet een module over naar Go; heeft de Python-versie daarna
  nog Python-afnemers, dan blijft hij **bevroren** staan. Bevroren betekent: geen wijziging,
  behalve een fix die in dezelfde PR ook in Go landt, met een gedeelde fixture als bewijs. De
  PR die de laatste Python-afnemer omzet, haalt de module weg. `internal/protocol` en
  `protocol.py` zijn nu al zo'n tweeling. *(eigen keuze na spec-review, r1-f1: de importgraaf
  laat "weg in dezelfde PR" niet toe)*
- Een host die na een upgrade van de binary met andere gates werkt dan de repo verwacht, zonder
  dat iemand het ziet.

## Inventaris

| Wat | Waar | Omvang | Wie roept het aan |
|---|---|---|---|
| Hook-libs | `pack/core/lib/*.py` | 21 modules, ~10k regels | 8 hooks, skills |
| Skill-scripts | `pack/core/scripts/*.py` | 11 scripts, ~5.9k regels | 10 skills, 1 workflow |
| Shell-scripts met Python erin | `pack/core/skills/review-spec/review.sh` (3 inline `python3 -` + `codex_usage.py`), `pack/core/scripts/check-instructions.sh` (`python3 -c`, `config.py`) | 2 scripts | `review-spec`, `review-plan`; CI en skills |
| Hook-hulpjes | `pack/core/hooks/{permission-check,usage-gate}/*.py` | 3 bestanden | 2 hooks |
| Core-hooks (bash) | `pack/core/hooks/*.sh` | 9 hooks, ~2k regels; `commit-test-gate.sh` alleen al 922 | Claude Code |
| Config uit skills | `python3 .lighthouse/lib/lighthouse_lib/config.py <key>` | ~200 aanroepen in SKILL.md's | skills van alle pakketten |
| Gates van taalpakketten | `pack/go/scripts/gate_go.py`, `pack/node/scripts/gate_node.py` | 335 regels | `[test_gate] command` |
| Workflow | `pack/core/workflows/unblock-dependents.yml` | `uv run … python unblock_dependents.py` | GitHub-runner |
| Binary | `internal/initcmd/workflows.go:117`, `internal/deps/deps.go:55` | 2 plekken | `init` |
| Hooks van taalpakketten | `pack/go/hooks/go-lint-gate.sh` (`turn_roots.py`, `alarm.sh`), `pack/python/hooks/ty-gate.sh` (idem), `pack/shell/hooks/shell-lint-check.sh` (`config.py`, `python3 -c`) | 3 hooks | Claude Code |
| Overige `python3` in geïnstalleerde tekst | `write-pr` (pipe naar `pr_body_gate.py`), `place-instruction` (`config.py`, `scripts/measure_skill_surface.py`), `shell-standard` (`realpath`-recept met `python3 -c`), `go-standard` (`python3` als vereiste tool) | 4 skills | agents |

`pack/*/requires.toml` houdt per unit bij welke tool hij nodig heeft. Nu staat `python3` bij
bijna elke unit van `core` en bij drie van `go`.

De inventaris is pas volledig met `grep -rn python3` over wat een verse `init` met `core`, `go`,
`node`, `shell` en `expo` installeert. Stap 1 draait die grep en zet elke treffer in het issue van
de stap die hem weghaalt (zie *Opdeling*). *(eigen keuze na spec-review, r1-f4)*

**Importgraaf** (bepaalt de volgorde, zie *Opdeling*). Bladeren zonder lighthouse-import:
`config`, `datadir`, `deadline`, `filelock`, `gate_slot`, `git_invocations`, `host_memory`,
`md_links`, `repo_identity`, `code_language_score`. Daarboven:

| Module | Importeert | Python-afnemers |
|---|---|---|
| `headings` | `config` | bijna alles |
| `protocol` | `config`, `headings` | `artefacts`, `post_artefact` |
| `git_invocations` | — | `bash_writes`, `turn_roots`, `pr_body_gate`, `no_verify` |
| `bash_writes` | `config`, `deadline`, `git_invocations`, `repo_identity` | `commit_base`, `turn_roots` |
| `pr_body_gate` | `config`, `git_invocations`, `headings`, `repo_identity` | `evidence`, `worktree_audit` |
| `usage_guard` | `config` | `usage-gate/controller.py`, `usage-gate/resume.py` |

## Aanpakken

| | Aanpak | Wint | Kost |
|---|---|---|---|
| A | De bash-hooks blijven; `python3 "$LIB/x.py"` wordt `lighthouse lib x` | Kleinste stap per PR | Bash en Go blijven naast elkaar; het Windows-werk blijft even groot |
| **B** | **Elke hook helemaal naar Go: `lighthouse hook <naam>`; de bash verdwijnt** | **Eén taal, één testsuite; lost ook Git Bash op Windows grotendeels op** | **`commit-test-gate` (922 regels bash met `systemd-run`, slots, deadline) moet mee** |
| C | Per hook kiezen: dunne hooks naar B, dikke naar A | Minder werk dan B | De dikke hooks, en dus het moeilijkste Windows-werk, blijven bash |

**Gekozen: B** (gebruiker, 2026-11-07). C laat precies het Windows-knelpunt staan, en B maakt het
Windows-issue van M3 kleiner.

## Ontwerp

### 1. De binary is verplicht (besluit gebruiker)

Wie in een lighthouse-repo werkt, heeft de binary op het PATH. Hij installeert die met de
release-download of bouwt hem met `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<versie>`.
Ontbreekt de binary, dan meldt een hook:

```
COULD NOT CHECK: lighthouse not on PATH — install v<versie>: go install github.com/CorneeldH/lighthouse/cmd/lighthouse@v<versie>, or download it from the release
```

`<versie>` komt uit `.lighthouse/manifest.toml`. Bij `version = "dev"` noemt de melding
`go install ./cmd/lighthouse` vanuit de checkout.

**Blokkeren gaat zoals nu** (besluit gebruiker na spec-review, r1-f5). Een hook die nu blokkeert
als zijn tooling ontbreekt, blokkeert ook zonder binary: exit 2, de melding op stderr. Dat zijn
`main-commit-gate`, `commit-test-gate`, `pr-create-gate`, `bash-write-check`, `md-link-check` en
`code-language-check`. Een hook die nooit blokkeert (`worktree-check`, `permission-check`,
`usage-gate` bij `prompt`), geeft de melding zonder te blokkeren. Gevolg: zonder binary blokkeert
`main-commit-gate` elke Bash-call. Dat is bedoeld: de melding zegt hoe je hem installeert.

### 2. Hoe Claude Code een hook aanroept

`init` registreert per hook één opdracht in `.claude/settings.json`:

```
.claude/hooks/lighthouse/run.sh <naam> [stand]
```

`run.sh` is het enige stuk shell dat overblijft: POSIX `sh`, rond de twintig regels. Het kent
de lijst van blokkerende hooks uit §1; die lijst wordt met de pakketten geïnstalleerd en loopt
dus gelijk met de registraties. Het zoekt `lighthouse` op het PATH. Ontbreekt die, dan geeft het
de melding van §1 volgens die lijst: exit 2 met stderr, of exit 0 met de melding als
`systemMessage`. Anders roept het `lighthouse hook "$@"` aan en geeft de exitcode door, met één
uitzondering: exit 3 betekent *onbekende hook* (§3) en wordt volgens dezelfde lijst exit 2 of
exit 0 met melding. Zo'n vast stuk is nodig, want zonder binary geeft een
kale `lighthouse hook …` in `settings.json` alleen een hookfout met `command not found`, zonder
uitleg hoe je de binary krijgt. *(Mijn keuze; een inline `command -v … ||` in de registratie kan ook, maar is
slechter te testen.)*

`lighthouse hook <naam>` leest de payload van stdin, zoals de bash-hooks nu. Het houdt het
contract van Claude Code aan: exit 2 met de reden op stderr blokkeert, JSON op stdout
(`additionalContext`, `systemMessage`, `decision`) werkt zoals nu. Elke hook geeft exact hetzelfde
oordeel en dezelfde tekst als zijn bash-voorganger, op dezelfde payloads.

### 3. Versies van binary en repo

De logica zit in de binary. Wat de repo installeert, is alleen nog registraties, `run.sh` en
tekst (skills, AGENTS.md). Een andere binary-versie dan in de manifest is daarom geen reden om
niet te checken. De hook draait gewoon, en zet een `systemMessage` met
`lighthouse v<binary> ≠ v<manifest> in this repo — run lighthouse init --upgrade`, één keer per
sessie. *(Mijn keuze: COULD NOT CHECK bij elk verschil zou elke upgrade van de binary alle gates
stilzetten tot `init --upgrade`.)* Bij `version = "dev"` komt die melding niet.

"Eén keer per sessie" houdt een marker bij in de state-dir van lighthouse, per `session_id` uit
de payload. De marker wordt pas gezet als de melding echt meegaat op een call die niet blokkeert,
want bij exit 2 negeert Claude Code de JSON op stdout. *(eigen keuze na spec-review, r1-f11)*

Een hook die de binary niet kent (registratie nieuwer dan de binary) geeft exit 3 met
`COULD NOT CHECK: unknown hook <naam> — update lighthouse to v<manifest>`; `run.sh` maakt daar
exit 2 of een melding van (§2). Bij `version = "dev"` zegt de melding `go build -o
.lighthouse/bin/lighthouse ./cmd/lighthouse` in plaats van een versie (§10).

### 4. Subcommando's voor skills

Elke aanroep van `python3 .lighthouse/…` in een skill wordt een subcommando. De naam volgt het
script, de vlaggen en uitvoer blijven gelijk, zodat de skilltekst alleen het commando verandert:

| Nu | Straks |
|---|---|
| `python3 .lighthouse/lib/lighthouse_lib/config.py <key>` | `lighthouse config <key>` |
| `python3 .lighthouse/lib/lighthouse_lib/headings.py <key>` | `lighthouse headings <key>` |
| `python3 .lighthouse/lib/lighthouse_lib/evidence.py run …` | `lighthouse evidence run …` |
| `python3 .lighthouse/scripts/<script>.py …` | `lighthouse <script met streepjes> …`, bijvoorbeeld `lighthouse worktree-audit` |
| `python3 .lighthouse/scripts/gate_go.py -- …` | `lighthouse gate go -- …` |
| `.lighthouse/scripts/check-instructions.sh` | `lighthouse check-instructions` |
| `.claude/skills/lighthouse-review-spec/review.sh` | `lighthouse review-run` (de hele runner, ook de inline Python) |
| `… \| python3 .lighthouse/lib/lighthouse_lib/pr_body_gate.py "$PWD"` (`write-pr`) | `… \| lighthouse pr-body-gate "$PWD"` |
| `python3 -c '…realpath…'` (`shell-standard`) | een recept zonder Python (`realpath -m`, anders `lighthouse path realpath`) |
| `python3 scripts/measure_skill_surface.py` (`place-instruction`) | `lighthouse measure-skill-surface`, of de regel weg als het script alleen in deze repo bestaat |

`lighthouse config` geeft dezelfde exitcodes als `config.py`: 0 met waarde, 1 als de sleutel niet
gezet is, 2 bij een fout. Skills leunen op dat onderscheid.

**Toestemming.** `pack/core/permissions.json` geeft nu vooraf toestemming voor
`review.sh` en `verify.sh`, en `IsLighthouseRule` (`internal/packs/packs.go:147`) kent alleen
regels in `.claude/skills/lighthouse-*` en de hook-map. Die functie gaat ook regels van de vorm
`Bash(lighthouse <subcommando>:*)` herkennen, zodat `permissions.json` ze kan meeleveren en `init`
ze beheert. Anders wacht een review-run zonder toezicht op een prompt. *(eigen keuze na
spec-review, r1-f9)*

### 5. Taalpakketten

- `gate_go.py` en `gate_node.py` worden `lighthouse gate go` en `lighthouse gate node`.
  `pack/go` en `pack/node` hebben geen `lighthouse.toml.fragment`. Dat blijft zo, want `init`
  weigert een `test_gate.command` uit twee pakketten (`internal/initcmd/fragment_test.go:54`).
  `[test_gate] command` blijft van de gebruiker. *(r1-f2)*
- **Migratie van dat commando.** Een bestaand `command` roept nog `python3
  .lighthouse/scripts/gate_go.py` of `gate_node.py` aan, en die bestanden verdwijnen. Daarom
  herschrijft `init --upgrade` precies die twee vormen: `python3 .lighthouse/scripts/gate_go.py`
  wordt `lighthouse gate go`, `python3 .lighthouse/scripts/gate_node.py` wordt `lighthouse gate
  node`. De rest van het commando blijft letterlijk staan, en `init` toont de oude en de nieuwe
  regel. Verwijst het commando op een andere manier naar `.lighthouse/scripts/` of
  `lighthouse_lib`, dan herschrijft `init` niets en meldt het luid welk commando kapot gaat. De
  upgrade gaat door. *(eigen keuze na spec-review, r1-f3)*
- `go-lint-gate.sh` en `ty-gate.sh` (`pack/python`) gaan over op `lighthouse turn-roots` in de
  stap van `turn_roots` (r1-f6), en op `lighthouse usage alarm` (exit 0 = alarm actief) in de stap
  van `usage-gate`. In die stap verdwijnt `usage-gate/alarm.sh`. *(r1-f7)*
- `shell-lint-check.sh` en `shellcheck-repo.sh` lezen hun config met `lighthouse config`.
  `shell-lint-check.sh` leest de payload alleen nog met `jq` (zonder `jq`: `COULD NOT CHECK`) en
  bouwt zijn `systemMessage` met `jq -n` in plaats van `python3 -c`.
- `pack/python` gebruikt de core-lib op vier plekken: `config.py` in skills en hooks,
  `turn_roots.py` in `ty-gate.sh`, `config`, `headings` en `protocol` als import in
  `quality/prcov_issues.py`, en `config` in `quality/ratchet.py`. Die roepen voortaan de binary
  aan: `lighthouse config`, `lighthouse headings`, `lighthouse protocol …` en `lighthouse
  turn-roots` (besluit gebruiker). Voor `prcov_issues.py` zijn dat: `lighthouse protocol
  parse-header` (comment op stdin, JSON op stdout), `lighthouse protocol robot` (de
  robotmarkering), `lighthouse headings <key>` en `lighthouse config --json forge.board` (de
  board-tabel als JSON). *(eigen keuze na spec-review, r1-f14)* Zo staat elke regel logica één keer in Go. De rest van
  `pack/python` (`gate_python.py`, `quality/`) blijft Python. De PR die een module weghaalt, zet
  ook de aanroep in `pack/python` om.

### 6. De workflow op GitHub

`unblock-dependents.yml` downloadt de release-binary van de versie in de manifest
(`gh release download` met `GH_TOKEN`) en draait `lighthouse unblock-dependents --issue
"$ISSUE"`. `uv` en Python verdwijnen uit de workflow. Bij een `dev`-versie (deze repo) bouwt de
workflow de binary uit de checkout met `go build`. *(Mijn keuze.)*

### 7. De binary zelf

- Stap 1 zet `config.py` helemaal over naar Go, met zijn schema en defaults (zoals
  `ci.runs_on` = `ubuntu-latest`, `config.py:142`). Dat wordt de enige lezer van
  `lighthouse.toml`. `internal/repoconfig` leest nu alleen wat de dispatcher nodig heeft en faalt
  op een ontbrekende `[forge] repo` (`repoconfig.go:125`); hij bouwt voortaan op die lezer. Hoe
  precies, is aan de bouwer, zolang schema en defaults op één plek staan.
- `internal/initcmd/workflows.go` kopieert nu `core/lib/*.py` naar een tijdelijke map en draait
  `config.py`. In stap 1 vult hij de workflow-sleutels met de nieuwe Go-lezer. *(r1-f8)*
- `internal/deps` controleert `python3` niet meer voor `core`; `requires.toml` van `core`, `go`,
  `node`, `shell` en `expo` noemt `python3` niet meer. `pack/python` houdt het.
- `internal/protocol` wordt de enige lezer van het protocol; `protocol.py` verdwijnt.

### 8. Opruimen in repo's

Na de laatste PR installeert `pack/core` niets meer onder `.lighthouse/lib/` en
`.lighthouse/scripts/`. `init --upgrade` haalt die bestanden weg via de manifest (de
`reconcile.Remove`-route bestaat al). Een lokaal gewijzigd bestand blijft staan en wordt gemeld,
zoals nu. `pack/python` installeert zijn eigen scripts nog wel onder `.lighthouse/scripts/`.

### 9. Tests

- Een module die naar Go gaat, neemt zijn unittests mee. De Go-tests lezen dezelfde fixtures in
  `tests/pack/fixtures/`, zodat het oordeel aantoonbaar gelijk blijft. Nu hebben alleen
  `code_language`, `codex_usage`, `protocol` en `roborev` fixture-bestanden. Bij de andere
  modules staan de gevallen inline in pytest. Die haalt de PR eerst naar gedeelde fixtures, en
  zet daarna pas over. *(r1-f15)*
- `internal/initcmd/budget_parity_test.go` draait `.lighthouse/scripts/check-instructions.sh`.
  Die test gaat mee in de stap van `check-instructions`.
- De black-box-tests van de hooks (`tests/pack/test_*_gate.py`, `test_*_check.py`) blijven pytest.
  Ze roepen voortaan `run.sh` aan, met een binary die de testsuite één keer bouwt. Zo bewijzen
  dezelfde tests dat het gedrag van voor en na de ombouw gelijk is. *(Mijn keuze.)*
- `test_*_skill.py` (tests op de tekst van SKILL.md) blijven pytest en lopen mee met de nieuwe
  commando's.
- Pas Go-code aan volgens `/lighthouse-go-standard` en tests volgens `/lighthouse-test-standard`.

### 10. Dogfood

Lighthouse draait zijn eigen hooks. Een PR die een hook ombouwt, bewijst die hook met de
pytest-tests op een gebouwde binary; dat de eigen sessie hem ook draait (zie onder), is geen
bewijs. *(Mijn keuze.)*

Deze repo heeft `version = "dev"` in de manifest; §1 en §3 noemen wat daarbij anders is.

Een PR van deze milestone verandert wat de eigen sessies aanroepen: een nieuwe hookregistratie,
`lighthouse config` in de skills, `lighthouse gate go` in `[test_gate] command`. De
geïnstalleerde binary kent die pas na de volgende release. Zonder maatregel blokkeert
`main-commit-gate` dan elke Bash-call (§1), en een release met alleen een patch-fragment wacht
tot de wekelijkse cron (`.github/workflows/release.yml:2-3`). Daarom:

- Elke PR van deze milestone krijgt een `minor`-fragment in `changelog.d/`, zodat de release
  direct op de merge volgt.
- Na de merge draait wie in deze repo werkt `go install ./cmd/lighthouse` op `main`, of wacht
  op de release.

*(eigen keuze na spec-review, r1-f10)*

Dat dekt `main`, niet de worktree van een PR vóór de merge. Die worktree registreert de nieuwe
hook al (de PR commit de uitvoer van `init --upgrade`), terwijl de geïnstalleerde binary de vorige
release is. Een sessie in die worktree (fix-pr, een roborev-ronde, een handoff) zou dan bij elke
Bash-call vastlopen op `main-commit-gate`. Daarom, alleen bij `version = "dev"` *(eigen keuze na
spec-review, r2-f1)*:

- `run.sh` gebruikt `.lighthouse/bin/lighthouse` uit de checkout (in `.gitignore`). Is die er
  niet, of is hij gebouwd op een andere `HEAD` dan nu (een stempelbestand ernaast), dan bouwt
  `run.sh` hem eerst met `go build ./cmd/lighthouse`. Zo draaien de hooks van een worktree altijd
  de code van die branch.
- Claude Code start meerdere hooks tegelijk, dus de build gaat onder een lock (`mkdir`-lock
  naast de binary; wie hem niet krijgt, wacht tot de stempel klopt). De build schrijft naar een
  tijdelijk bestand in dezelfde map, `mv` zet het op zijn plek, en pas daarna komt de stempel.
  Een hook voert dus nooit een half geschreven binary uit. *(eigen keuze na spec-review, r3-f2)*
- **Nooduitgang.** Bestaat `.lighthouse/bin/use-path` (in `.gitignore`), dan slaat `run.sh` de
  checkout-binary over en gebruikt `lighthouse` van het PATH. Een agent maakt dat bestand met de
  Write-tool, die geen Bash-gate passeert. Zo komt een sessie ook uit een branch waarvan de
  gate compileert maar elke Bash-call blokkeert. De blokkademelding van een hook in een
  `dev`-checkout noemt die uitgang. *(eigen keuze na spec-review, r3-f1)*
- Lukt die build niet (de branch compileert niet), dan valt `run.sh` terug op `lighthouse` van
  het PATH. Kent die binary de hook niet, dan wordt de onbekende hook bij `dev` een melding en
  geen blokkade. Een worktree met kapotte Go-code sluit de agent dus niet buiten Bash; de
  melding noemt de build-fout en het commando van §3.
- Buiten `dev` bouwt `run.sh` nooit iets.

Elke PR draait `go run ./cmd/lighthouse init --upgrade` en commit het resultaat, zoals AGENTS.md
voorschrijft.

## Opdeling in issues

De volgorde volgt de importgraaf (*Inventaris*): eerst wat andere modules gebruiken. Elke stap
noemt wat hij in Go bouwt, welke Python hij **weghaalt** (de laatste Python-afnemer is om) en wat
**bevroren** blijft (§ *Anti-doelen*). *(eigen keuze na spec-review, r1-f1)*

1. **Fundament.** Go: `config` (heel `config.py`, schema en defaults, §7), `headings`, `datadir`,
   de hook-runtime (`lighthouse hook`, `run.sh`, de lijst van blokkerende hooks, de
   versiemelding), `lighthouse protocol` (§5), de toestemmingsregel (§4), `workflows.go` op de
   Go-lezer. De skill-aanroepen van `config.py` en `headings.py` in alle pakketten gaan over
   (mechanisch, ~220 regels), en `prcov_issues.py` en `ratchet.py` van `pack/python` ook. De
   volledige `python3`-grep (*Inventaris*) komt in de issues. Weg: niets. Bevroren: `config.py`,
   `headings.py`, `datadir.py`, `protocol.py`.
2. **`usage-gate`, `permission-check`, `md-link-check`.** Go: `usage_guard`, `md_links`,
   `lighthouse usage alarm`. `go-lint-gate.sh` en `ty-gate.sh` gaan op `lighthouse usage alarm`
   over. Weg: `usage_guard.py`, `md_links.py`, de drie `.py` onder de hooks, `alarm.sh`.
3. **`code-language-check`.** Weg: `code_language.py`, `code_language_llm.py`,
   `code_language_score.py`.
4. **`main-commit-gate` en `worktree-check`.** Go: `git_invocations`. Bevroren:
   `git_invocations.py`.
5. **`bash-write-check`** (na 4). Go: `bash_writes`, `turn_roots`, `deadline`, `repo_identity`,
   `filelock`; `lighthouse turn-roots`. `go-lint-gate.sh` en `ty-gate.sh` gaan op `lighthouse
   turn-roots` over. Weg: `turn_roots.py`. Bevroren: `bash_writes.py` (voor `commit_base`),
   `deadline.py`, `repo_identity.py`.
6. **`pr-create-gate`** (na 4 en 5). Go: `pr_body_gate`, `evidence`; `lighthouse pr-body-gate`,
   `lighthouse evidence`. `write-pr` gaat over. Weg: `evidence.py`. Bevroren: `pr_body_gate.py`
   (voor `worktree_audit`).
7. **`commit-test-gate`** (na 5). Go: `commit_base`, `gate_slot`, `host_memory`, `no_verify`.
   Mag in twee PR's: `pre` en `post`. `systemd-run` blijft achter `host.gate_slice`, alleen op
   Linux. Weg: `commit_base.py`, `bash_writes.py`, `gate_slot.py`, `host_memory.py`,
   `no_verify.py`, `deadline.py`.
8. **Skill-scripts, deel 1** (na 6 en 7). `post-artefact`, `board-start`, `unblock-dependents`
   (+ de workflow, §6), `worktree-audit`. Weg: de vier scripts, `artefacts.py`, `protocol.py`,
   `pr_body_gate.py`, `git_invocations.py`, `repo_identity.py`.
9. **Skill-scripts, deel 2.** `plan-review-record`, `review-pair`, `roborev-gate`,
   `roborev-comment`, `codex-usage`, `review.sh` als `lighthouse review-run`, en
   `check-instructions.sh` als `lighthouse check-instructions` (met
   `budget_parity_test.go`).
10. **KPI-scripts.** `measure-start-issue-kpi`, `measure-fix-pr-kpi`.
11. **Taalpakketten.** `lighthouse gate go|node`, de migratie van `[test_gate] command` (§5),
    `shell-lint-check.sh`, `shellcheck-repo.sh`, de Python-vrije tekst in `shell-standard`,
    `go-standard` en `place-instruction`.
12. **Afronden** (als laatste). Weg: `config.py`, `headings.py`, `datadir.py` en wat er nog
    bevroren staat. `internal/deps`, `requires.toml`, opruimen (§8), de CI-job van *Bewijs*, en
    de M3-spec en de installatiegids zonder Python-eis.

Na stap 1 kunnen 2, 3, 4, 9, 10 en 11 parallel. 5 wacht op 4; 6 op 4 en 5; 7 op 5; 8 op 6 en 7.
Elke PR krijgt een `minor`-fragment (§10).

## Bewijs

- Een CI-job (Linux) haalt `python3` uit het PATH en doet een verse `lighthouse init` met `core`,
  `go`, `node`, `shell` en `expo`. Daarna voert hij per hookregistratie één geldige payload uit
  (een vaste set onder `tests/pack/fixtures/hooks/`, het gewone pad per hook). Geen van die calls
  geeft `COULD NOT CHECK` of `command not found`. *(eigen keuze na spec-review, r1-f12: de
  pytest-tests sturen bewust ook kapotte payloads, die wel `COULD NOT CHECK` horen te geven)*
- `grep -rn python3` op de geïnstalleerde bestanden van die init geeft niets, ook niet in tekst.
- `windows-measure.yml` draait opnieuw. De uitkomst gaat als meting naar #90, niet als gate.

## Gevolgen voor M3

De M3-spec gaat uit van Python ≥ 3.11 in de installatiegids (§5) en noemt "Python op Windows" als
risico. Na deze milestone vervalt dat: de gids noemt `gh`, `jq`, `git` en de binary. Stap 12
past de M3-spec daarop aan. Het Windows-issue begint pas na deze milestone.

## Aan de bouwer

- De Go-pakketindeling onder `internal/` voor hooks en subcommando's.
- Of een hook en een subcommando die dezelfde lib gebruiken, één Go-pakket delen.
- De opdeling van stap 7 in één of twee PR's, en het samenvoegen van kleine stappen als een PR
  klein blijft.
- Hoe de testsuite de binary één keer bouwt (fixture, `conftest.py`).

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, §10 Dogfood (new r2-f1 block) / §10 opening paragraph): The diff makes dev hooks run the branch's own code, and falls back to the PATH binary only when `go build` fails. A branch whose code compiles but whose hook misbehaves (e.g. `main-commit-gate` blocking every Bash call) has no exit. The binary is rebuilt only on a HEAD change, and a commit needs Bash. The diff also leaves the opposite rule in place a few lines up: 'in deze repo roept `run.sh` de geïnstalleerde `lighthouse` aan, niet de code van de branch'. The PR test strategy rests on that rule. The implementer must pick one of the two.
- **r3-f2** (MAJOR, §10 Dogfood (new r2-f1 block), the rebuild in `run.sh`): Several Claude Code hooks fire in parallel on one event. Each one runs its own `run.sh`, so after every HEAD change they all see a stale stamp at the same moment. They then all run `go build -o .lighthouse/bin/lighthouse` into the same path and write the same stamp. The spec defines no lock, and no build-to-temp-then-rename. One `run.sh` can exec the binary while another is still rewriting it. The exec then fails with exit 126/ENOEXEC or crashes. Claude Code treats that as a non-blocking hook error, so a blocking gate passes unchecked.
