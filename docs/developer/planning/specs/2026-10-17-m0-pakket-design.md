# Ontwerp: M0 — Pakket

Issue: #1 · Milestone: [M0 — Pakket](https://github.com/CorneeldH/lighthouse/milestone/1) · Datum: 2026-10-17

Bouwt voort op de [scope-spec](https://github.com/CorneeldH/personal_ai_agent/blob/main/docs/developer/planning/specs/2026-10-09-lighthouse-scope-design.md)
en de M0-pitch (de beschrijving van milestone 1, met het blok *Besloten in het plan van #1085*).
Wat daar staat, geldt hier ook en wordt niet herhaald, tenzij deze spec ervan afwijkt. Afwijkingen
staan gemarkeerd als **Afwijking van de scope-spec**.

"paia" is in deze spec `CorneeldH/personal_ai_agent`; nummers met `paia#` verwijzen daarheen,
kale nummers naar lighthouse.

## Aanleiding en doel

De keten-skills, agents en generieke checks leven in paia, vastgeknoopt aan de repo-naam, het
board en de Beelink. M0 verhuist ze naar lighthouse en levert ze terug via `lighthouse init`.
Daarmee is de grens repo ↔ lighthouse vanaf M0 afgedwongen, en kan een tweede repo (eerst een
eigen repo op de Mac, later de tweede gebruiker) dezelfde keten krijgen zonder Corneels paden,
host of board.

**M0 is een basis die op zichzelf werkt.** Wat projectafhankelijk is (het board, de release-flow,
de pre-push-keten), is optioneel: zonder slaat de skill de stap over en meldt dat. Na M0 bouwen we
uit vanaf de lijst *Verbeterpunten*.

## Doelen

- Een Go-binary met alleen `lighthouse init` (en `init --upgrade`).
- Vier pakketten: `pack/core`, `pack/python`, `pack/shell`, `pack/expo`.
- paia haalt zijn keten via `lighthouse init`; de oude kopieën zijn weg.
- Alle instructies in de pakketten in het Engels; de taal van wat de skills in een repo schrijven
  komt uit `lighthouse.toml`.
- De vier DoD-punten van de pitch, plus de projectconfig, het besluit over de grensgevallen en CI
  op GitHub-hosted runners (issue #1).

## Non-goals

- Geen `serve`, `tick` of `run`; geen SQLite, geen SPA (M1, M2).
- De usage-guard en de venstermeters verhuizen niet (M1).
- De Python-controller (`src/controller/`, `scripts/controller_*`) verhuist niet (M2).
- Geen Claude Code-plugin (zie *Besluiten*, punt 5).
- Geen GitLab, geen installatiegids (M3).
- Geen nieuwe checks. Complexipy, import-linter en de TS-complexiteitslint komen pas als ze in paia
  bestaan.

## Anti-goals

M0 is mislukt, ook als alles technisch werkt, wanneer:

- paia na M0 twee bronnen heeft voor een keten-skill (de oude kopie en de lighthouse-kopie);
- een pakketbestand nog een repo-naam, labelnaam, board-ID, hostnaam of host-IP als literal draagt;
- `lighthouse init` iets schrijft zonder akkoord, of een projecteigen hookregistratie of tabel in
  `lighthouse.toml` weggooit;
- een guard in een verse repo stil niets doet (fail-open zonder melding);
- een Nederlands issue in paia na de vertaling niet meer door de keten komt.

## Besluiten

| # | Besluit | Reden |
|---|---|---|
| 1 | De generieke scripts onder de keten-skills gaan als Python mee in `pack/core` (zie *Inventaris*). Herbouw in Go komt in M1/M2 | `python3` ≥ 3.11 is al een vereiste; Go-herbouw nu is M0 te groot |
| 2 | De pakketinhoud zit in de binary via `go:embed`. De versie van een pakket is de versie van de binary | Eén versie, werkt offline, geen fetch-pad |
| 3 | Installatie: `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@<tag>`, met `GOPRIVATE=github.com/CorneeldH/*` zolang de repo private is. Release-binaries pas in M3 | Kleinste route voor twee eigen machines |
| 4 | `--upgrade` weigert een bestand dat lokaal gewijzigd is, toont de diff en vraagt `--force` | Een lokale wijziging verdwijnt nooit stil |
| 5 | Kopiëren, geen Claude Code-plugin. De pakketmappen volgen wel de plugin-indeling (`skills/`, `agents/`, `hooks/`), zodat een plugin later goedkoop is | Een plugin is Claude-only (`.agents/skills/` is de spiegel voor Codex) en per gebruiker geïnstalleerd, niet per repo gepind |
| 6 | Skillnamen krijgen het prefix `lighthouse-` (bijvoorbeeld `lighthouse-start-issue`). De Agent Skills-standaard staat in `name` alleen `a-z`, `0-9` en `-` toe, dus `lighthouse:` kan niet. Nederlandse namen worden Engels: `pauzeer` → `pause`, `python-schrijven` → `python-writing`, `check-instructies.sh` → `check-instructions.sh`. *Mijn keuze* voor de vertaalde namen | Voor de tweede gebruiker zegt `paia-` niets; later hernoemen is een tweede migratie |
| 7 | Taal: instructies in skills, agents en hookmeldingen in het Engels. De taal van artefacten (issues, PR's, specs, plannen, commits) komt uit `[project] language` in `lighthouse.toml`, met `nl` of `en`. De gesprekstaal regelt de gebruiker zelf (bijvoorbeeld `"language"` in `~/.claude/settings.json`); lighthouse raakt die niet | Gebruikers praten in hun eigen taal met Claude, ook met Engelse skills; de repo kiest de taal van wat erin komt |
| 8 | Koppen die code of een skill als contract leest (zoals *Acceptatiecriteria*), staan in een koppentabel in `pack/core`: per sleutel een variant per taal. Lezen accepteert elke bekende variant; schrijven gebruikt de projecttaal. M0 levert `nl` en `en` | Oude Nederlandse issues blijven werken; een nieuwe taal is een kolom erbij |
| 9 | `paia-controller` blijft in paia tot M2. **Afwijking van de scope-spec**, die hem in *Kern* zet | De skill stuurt de Python-controller (`controller_gather.py`, `controller_state.py`, `controller_tick.sh`, `voortgang.py`), die tot M2 in paia blijft |
| 10 | M0 wordt vijf sub-issues: (a0) → (a) → (b) → (c) → (d), zie *Opdeling* | Large past niet in één PR |
| 11 | Bewijs van "verse repo op een andere host" in drie stappen: een e2e-test in de CI van lighthouse op een GitHub-runner, daarna een andere repo op de Mac. De tweede gebruiker is M3 en hoort niet bij de DoD van M0 | De CI is herhaalbaar; de Mac is de echte tweede host |
| 12 | Bevriezen per pakket: de overstap-PR van een pakket in paia is het moment waarop de bron verhuist. Vanaf die PR wordt die inhoud alleen nog in lighthouse gewijzigd | Geen twee bronnen tegelijk, zonder alles tegelijk te bevriezen |
| 17 | De release-flow (`release.py`, `changelog_gen.py`, `sbom`, `publiccode.yml`, `chore-merge.yml`) en de pre-push-keten (`pre-push.sh`, `workflow-audit.sh`, `scripts/hooks/install.sh`) blijven in M0 in paia. `close-issue` en `fix-pr` roepen ze aan als ze bestaan en melden het anders. Uitzondering op besluit 14 | `release.py` hangt aan paia's LLM-gateway (`src.llm`) en `paia-bot`; zonder `pre-push.sh` stoppen workflow-audit, de ratel en de instructielaag-gate zonder melding (`install.sh:31-37`). Meeverhuizen vergt een git-hook-installer in `init` |
| 13 | Lighthouse dogfoodt: (a0) zet met de hand een bootstrap neer; na (b) draait `lighthouse init` op lighthouse zelf, op een branch, en de diff ruimt de bootstrap op | Wie aan lighthouse werkt, heeft de guards vanaf dag één |
| 14 | **Eerst lostrekken, dan verbeteren.** Alles wat een verhuizende skill nodig heeft, verhuist mee: scripts (`review_pair.py`, `codex_usage.py`; uitzondering: besluit 17), workflows (`unblock-dependents.yml`) en templates (`pull_request_template.md`). Projectinhoud die een skill leest, zoals `docs/developer/tenets.md`, `docs/developer/areas/` en `docs/developer/planning/history/`, blijft van het project en komt via `[paths]`. Namen van secrets en bots, zoals `paia-bot`, gaan naar de config. Wat beter of handiger kan, komt op de lijst *Verbeterpunten*, niet in de verhuis-PR. Wel toegestaan in de verhuis-PR, omdat het bestand anders buiten paia niet werkt: imports omzetten naar `lighthouse_lib`, de repo-root via git vinden, literals naar de config, en fail-open-gaten dichten | Een verhuizing die ook herontwerpt, is niet te reviewen; gedrag blijft eerst gelijk |
| 15 | Het comment-protocol tussen skills en controller verhuist naar `pack/core/lib/`: de opmaak en het lezen van artefacten (nu `src/controller/artefacten.py` en het protocoldeel van `src/controller/comments.py`). De paia-controller importeert het tot M2 uit `.lighthouse/lib/`. De marker wordt `<!-- lighthouse: {json} -->`; lezers accepteren tot M2 ook `<!-- paia: … -->`. De koppen (`🤖 Handoff`, `🤖 Vraag`, …) en de vraagvelden (`Waar het staat`, `Wat er speelt`, …) gaan in `headings.toml` | Eén bron voor het protocol; de controller blijft werken zonder twee kopieën |
| 16 | Het board is optioneel. Zonder `[forge.board]` slaan de skills de bordstappen over en melden ze dat. De statusoptie `Controller` is alleen verplicht als `[controller]` bestaat. `init` maakt geen board aan | Lighthouse zelf, de CI-test en de Mac-repo hebben geen board |

Besluiten 18 tot en met 29 komen uit de inventaris voor (b), op paia `33833601` (comment op #7).
Ze gelden voor (b) en (c).

| # | Besluit | Reden |
|---|---|---|
| 18 | Elke hook die niet kon controleren, meldt dat met het vaste token `COULD NOT CHECK`. `bash_writes.classify` herkent "niet gecontroleerd" aan dat token, niet aan de Nederlandse eerste regel van een gate. Een gate en `classify` worden in dezelfde PR vertaald | `classify` (`bash_writes.py:805-824`) matcht nu `NIET gecontroleerd` en `Linkcheck: KON NIET CONTROLEREN`. Vertaal je één kant, dan telt "niet gecontroleerd" als een echte bevinding |
| 19 | `bash-write-gates` stuurt alleen door naar gates die geïnstalleerd zijn. Ontbreekt de gate voor een extensie, omdat zijn taalpakket ontbreekt, dan meldt hij per bestand `not checked — no <pack> gate installed`. Hij blokkeert dan niet | Nu geeft een ontbrekende `ruff-format.sh` of `shellcheck.sh` exit 127, en dus exit 2 bij elke `.py`- of `.sh`-write. Dit is de regel van *Kruisverwijzingen*, toegepast op een hook |
| 20 | Heeft een repo geen `[test_gate] command`, dan blokkeert de test-gate niet. Hij meldt bij elke commit `no test command configured, nothing was tested`. De geheugenrem (`gates.slice`, `choom`) is een eigenschap van de host: de slice-naam staat in `host.toml`, en zonder die sleutel draait de gate zonder rem en meldt hij dat | Een letterlijke kopie laat zo'n commit stil door (anti-goal). Altijd blokkeren maakt een repo zonder tests onwerkbaar. `gates.slice` bestaat alleen op de Beelink |
| 21 | Alle sleutels in het comment-protocol worden vaste Engelse tokens, niet alleen die van de handoff. Dat geldt voor de meta van een artefact (`soort`→`kind`, `versie`→`version`, `deel`→`part`, `delen`→`parts`, `bestand`→`file`, `vervangt`→`replaces`; `commit` en `hash` blijven), voor de sleutels van 🤖 Gestart, Herbaseerd, Hervat, Klaar en Teruggegeven, en voor de triage-marker. Lezers accepteren tot M2 beide vormen. De waarden `spec` en `plan` blijven. *Mijn keuze* | Dezelfde regel als in *Taal* voor de handoff. Dit zijn protocoltokens, geen proza. De bouwer legt de volledige afbeelding vast in (b) |
| 22 | `init` merget ook de `permissions.allow`-regels die een pakket vraagt, met dezelfde eigendomsregel als bij hooks. Een regel die naar `.claude/skills/lighthouse-*` of `.claude/hooks/lighthouse/` wijst, is van lighthouse; elke andere regel is projecteigen en blijft staan. Elke regel is een eenheid in het manifest | De reviewskills draaien `review.sh` en `verify.sh` via een allowlist-regel (spec-review C:261-262, plan-review C:172), en de tests pinnen die. Stap 5 van *init* dekte alleen hookregistraties |
| 23 | Nieuwe sleutels, buiten de tabellenlijst van *Portabiliteit*:<br>- `[forge] default_branch`, default `main`;<br>- `[codex_usage]`, met de vijf drempels uit paia als defaults;<br>- `[worktree_audit] protected_refs` en `protected_paths`, bovenop een vaste `default_branch`;<br>- `[project] instructions_word_ceiling`, default 3000;<br>- `[project] skill_prefix`.<br>**Afwijking van de pitch**, net als `[project]` en `[ci]` | `main` staat hardcoded in `git_invocations.py:561`, `main-commit-guard.sh`, `worktree-guard.sh:62`, `worktree_audit.py:850-860` en `unblock-dependents.yml:40`. `codex_usage.py:484-489` haalt zijn drempels uit `src.config`. `entire/*` en `.port-registry` zijn paia-eigen. De configlezer van (a) laat onbekende tabellen al door (`config.py:6`) |
| 24 | Zonder `[forge.board]` geeft `board_start.py` in beide modi `board: skipped — no [forge.board] configured` en exit 0, ook met `--heropen` | `--heropen` geeft nu exit 1 (`board_start.py:143-144`), dus `close-issue` meldt `board: FOUT` in elke repo zonder board |
| 25 | Het protocol in `lighthouse_lib` exporteert publiek wat de controller nu privé gebruikt: `Comment`, `KOP`, en wat nu `_LETTERS` en `_MAX_VRAGEN` heet. Het controllerbeleid blijft in paia:<br>- `parse_handoff` leest `lane` als gewoon veld, en de controller valideert het tegen `NATIVE` en `INTERACTIEF`;<br>- het controllerproza in `render_klaar` (`comments.py:365-366`) komt van de aanroeper | Lostrekken zonder het gedrag te wijzigen: dezelfde controle, op een andere plek. Zonder publieke namen breekt de paia-controller bij de overstap in (d) |
| 26 | Van `codex_usage.py` krijgt core alleen `budget` en `read`, wat de reviewskills nodig hebben. `gate`, `_parkeer` en `schrijf_marker` blijven in paia tot M1, bij de usage-guard. `usage_window.py` laadt het core-deel uit `.lighthouse/scripts/`. *Akkoord Corneel, 2026-10-18* | Het gate-deel draagt `PAIA_CONTROLLER_RUN` en de marker `paia/resume` (`codex_usage.py:522-531`), en zou de literal-grep laten falen. Splitsen is wel een kleine herstructurering. Het alternatief: het hele bestand meenemen, met die literals in de config |
| 27 | De functies die `plan_review_record.py` en `review_pair.py` uit `experiments/lekmeting-keten/lk_common.py` halen, komen in `lighthouse_lib`. De default datamap wordt `${XDG_DATA_HOME:-~/.local/share}/lighthouse/review-records/`, met de override `LIGHTHOUSE_DATA`. Paia zet die override in (d) als de records op de oude plek moeten blijven. *Mijn keuze* | `lk_common.py` is paia-eigen en staat niet in de inventaris. Zonder vervanger werken de reviewskills buiten paia niet |
| 28 | `write-issue` gaat naar `pack/core` als skelet: het zoeken naar duplicaten, de templatekeuze, het milestone-oordeel en de blokkade-invariant. De labeltaxonomie (`write-issue` C:95-197, 345-350, 385-414) blijft projectinhoud en komt via `[paths] labels`. Namen die code leest, zoals `blocked_issue`, staan in `[labels]`. *Akkoord Corneel, 2026-10-18* | Ongeveer 60% van de skill is paia's taxonomie. Die als config modelleren (`[labels.axes.*]`) is herontwerp, en dat past niet in besluit 14 |
| 29 | `close-issue` zoekt de vrijgeef-run op de nieuwe naam `lighthouse-unblock-dependents.yml`. De hernoeming en de skillwijziging gebeuren in dezelfde PR | `close-issue` C:384-386 zoekt met `--workflow unblock-dependents.yml` en `displayTitle endswith " #<nr>"`. Hernoem je alleen de workflow, dan vindt de skill de run niet meer |

## Opbouw

### Binary

- Go-module `github.com/CorneeldH/lighthouse`, entrypoint `cmd/lighthouse`, Cobra voor de
  commando's (gelijk aan de stack uit de scope-spec). *Mijn keuze:* de nieuwste stabiele Go-versie
  bij de start van (a).
- Commando's in M0: `lighthouse init`, `lighthouse init --upgrade [--force]`, `lighthouse version`.
- CI op GitHub-hosted runners: `golangci-lint`, `go test ./...`, een Python-lane (`uv` +
  `pytest`) voor de hooks, helpers en scripts in `pack/`, de literal-grep (zie *Portabiliteit*) en
  de e2e-test van besluit 11.
- *Mijn keuze:* de paia-tests van bestanden die verhuizen, verhuizen mee (onder meer
  `test_pr_body_gate.py`, `test_bash_write_gates.py`, `test_post_artefact.py` en
  `test_issue_skills_contract.py`). Tests die Nederlandse zinnen of labelnamen in een SKILL.md
  vastleggen, worden herschreven tegen de koppentabel en de config, in dezelfde PR als de
  vertaling. Zo heeft de vertaling een vangnet.

### Indeling in de lighthouse-repo

```
cmd/lighthouse/        entrypoint
internal/...           init, upgrade, config, merge (vrij voor de bouwer)
pack/core/             skills/  agents/  hooks/  lib/  scripts/  instructions/  headings.toml
pack/python/           skills/  hooks/  scripts/  lighthouse.toml.fragment
pack/shell/            skills/  hooks/  scripts/
pack/expo/             skills/
```

`pack/*/skills/<naam>/` draagt per skill beide harnasvarianten: `claude/SKILL.md` en
`agents/SKILL.md`, of één bestand als ze gelijk zijn. Hoe precies, beslist de bouwer; eis is dat
de lighthouse-repo zelf de enige plek is waar het verschil tussen de varianten staat.

### Waar de bestanden in een projectrepo landen

*Mijn keuze*, zodat projecteigen bestanden en lighthouse-bestanden niet botsen:

| Soort | Pad in de projectrepo |
|---|---|
| Skills | `.claude/skills/lighthouse-<naam>/` en `.agents/skills/lighthouse-<naam>/` |
| Agents | `.claude/agents/lighthouse-<naam>.md` |
| Hooks | `.claude/hooks/lighthouse/<naam>.sh` |
| Alle Python (hookhelpers, configlezer, protocol) | `.lighthouse/lib/lighthouse_lib/` (één importeerbaar pakket) |
| Scripts | `.lighthouse/scripts/` |
| Workflows en templates | `.github/workflows/lighthouse-<naam>.yml`, `.github/pull_request_template.md` |
| Manifest | `.lighthouse/manifest.toml` |

Elk verhuisd script en elke helper vindt de repo-root via `git rev-parse --show-toplevel` (of de
env-override `LIGHTHOUSE_REPO_ROOT`), nooit via `Path(__file__).parent.parent`. Nu rekenen onder
meer `post_artefact.py:28`, `board_start.py:28` en `pr_body_gate.py` (`parents[3]`) de root uit
hun eigen pad; op de nieuwe plek wijst dat naar de verkeerde map.

### Versiemarker en manifest

- **Het manifest is de bron van waarheid.** `.lighthouse/manifest.toml` (gecommit) noemt per
  geïnstalleerde eenheid pakket, versie en de sha256 zoals `init` hem schreef. Een eenheid is een
  bestand, een beheerd blok in `AGENTS.md` (zie *init*) of één hookregistratie in
  `.claude/settings.json`. `--upgrade` vergelijkt de huidige inhoud met die hash: gelijk = veilig
  te vervangen; anders = lokaal gewijzigd, weigeren, diff tonen (besluit 4).
- **Een marker alleen waar het formaat het toelaat.** Frontmatter van een skill of agent:
  `metadata.lighthouse: "<pakket>@<versie>"`. Shell, Python en YAML: een commentaarregel
  `# lighthouse: <pakket>@<versie>` (onder de shebang als die er is). Geen marker in JSON (zoals
  de schema's van de adversarial reviews) en niet in `pull_request_template.md`, want daar lekt hij
  in elke PR.
- Check 11 en de bumplane slaan in paia een **hele skillmap** over als zijn `SKILL.md` de marker
  draagt, niet per bestand.
- De versie is de tag van de binary. Een lokale build (`(devel)` of een pseudo-versie) schrijft
  `dev` als versie; `--upgrade` naar een tag vervangt die zonder churn op de hash.
- `--upgrade` met dezelfde binaryversie en geen lokale wijziging geeft een lege diff (DoD 1).

### init

De stappen van de scope-spec (herkennen, voorstellen met diffs, na akkoord schrijven, `--upgrade`)
gelden. Aanvullingen:

1. **Afhankelijkheden.** Controleert `gh`, `git`, `jq`, `uv`, `python3` met `import tomllib`,
   `codex` en de Superpowers-plugin (aanwezig als `superpowers` in
   `~/.claude/plugins/installed_plugins.json` staat). Wat ontbreekt, meldt hij met de naam van de
   skill of hook die het nodig heeft. *Mijn keuze:* een ontbrekende afhankelijkheid blokkeert
   alleen de skills die hem nodig hebben. De guards hebben `git`, `jq` en `python3` nodig, niet
   `codex` of Superpowers, zodat de e2e-test in de CI zonder die twee kan draaien.
2. **`lighthouse.toml`.** Bestaat hij, dan vult `init` alleen ontbrekende tabellen en sleutels aan,
   door tekst toe te voegen, niet door het bestand opnieuw te schrijven. Zo blijven commentaar en
   elke onbekende tabel staan (paia heeft `[controller]`, dat tot M2 van paia is). Board-ID's en
   optie-ID's meet hij met `gh project field-list`.
3. **`host.toml`.** Ontbreekt `~/.config/lighthouse/host.toml`, dan stelt `init` hem voor met `name`
   en `tailscale_ip` en schrijft hij hem na akkoord. Hij komt nooit in de repo. De guards hebben hem
   niet nodig; alleen de skills die de host-identiteit vragen.
4. **`AGENTS.md`.** De keten-afspraken staan in één beheerd blok per pakket tussen
   `<!-- lighthouse:begin <pakket> -->` en `<!-- lighthouse:end <pakket> -->`, alleen in `AGENTS.md`.
   `CLAUDE.md` importeert `AGENTS.md` al (`@AGENTS.md`) en mag niets dupliceren
   (`test_instructielagen.py`). `init` en `--upgrade` raken alleen tekst binnen de markers.
   Bestaat `AGENTS.md` niet, dan maakt `init` hem aan, en `CLAUDE.md` met `@AGENTS.md`. *Mijn keuze.*
5. **`.claude/settings.json`.** Merget de hookregistraties van de gekozen pakketten. Een registratie
   die naar `.claude/hooks/lighthouse/` wijst, is van lighthouse; de rest is projecteigen en blijft
   staan, in dezelfde volgorde. Een nieuwe lighthouse-registratie komt achteraan in de matchergroep
   van zijn event en matcher; bestaat die groep niet, dan komt er een nieuwe groep achteraan.
   `init` meldt elke registratie die naar een script wijst dat niet bestaat.
6. **Niet-interactief.** `--yes` keurt alle voorstellen goed, voor de CI en voor scripts.

### Configlezer

`lighthouse_lib` levert één Python-configlezer voor hooks, scripts en skills, met een CLI
(`python3 -m lighthouse_lib.config <sleutel>`, met `.lighthouse/lib` op `PYTHONPATH`; de hooks en
scripts zetten dat zelf). Hij neemt het gedrag over van paia's `src/controller/identity.py` en
`src/controller/host.py`: luid valideren, rc 2 bij een configuratiefout, de env-overrides
`LIGHTHOUSE_TOML` en `LIGHTHOUSE_HOST_TOML` voor tests. Anders dan `identity.py` eist hij
`[forge.board]`, `[controller]` en de optie `Controller` niet (besluit 16).

Tot M2 lezen in paia twee lezers hetzelfde bestand: de controller via `identity.py`, de pakketten
via de configlezer. Dat mag, want er is één bron: het bestand. De testgevallen voor het gedeelde
deel (`[forge]`, `[forge.board]` als hij er is, `host.toml`) draaien tegen beide.

### Portabiliteit

- Elke literal uit de inventaris komt onder `[forge]`, `[forge.board]` (inclusief de bestaande
  `priority_field_id`), `[forge.board.status_options]`, `[forge.board.priority_options]`,
  `[labels]`, `[paths]`, `[test_gate]`, `[project]` of `[ci]`. **Afwijking van de pitch:**
  `[project]` (taal) en `[ci]` (zie hieronder) staan niet in de tabellijst van de pitch.
- De CI van lighthouse grept `pack/` hoofdletterongevoelig op een deny-lijst: `CorneeldH`,
  `corneel`, `/home/`, `personal_ai_agent`, `personal-ai-agent`, `paia` (dus ook `paia-bot`,
  `PAIA_*`, `paia-run-`), de board-ID's en optie-ID's uit paia's `lighthouse.toml`,
  `100.76.126.126`, `beelink`, `gates.slice` en de labelnamen van paia. De deny-lijst staat in de
  lighthouse-repo, buiten `pack/`.
- **Workflows lezen geen TOML.** `init` vult workflowbestanden bij het kopiëren in uit de config
  (bijvoorbeeld `runs-on` uit `[ci] runs_on`, en de naam van het bot-secret uit `[ci]`). De hash in
  het manifest is die van het ingevulde bestand. Nu draagt `unblock-dependents.yml`
  `runs-on: [self-hosted, Linux, X64, beelink]`.

### Taal

- Bij de verhuizing in (b) en (c) worden skills, agents, hookmeldingen en de instructieblokken naar
  het Engels vertaald. Het gedrag blijft gelijk.
- Een skill die een artefact schrijft, leest `[project] language` en schrijft daarin, inclusief de
  koppen uit `headings.toml`.
- **Koppen** die code of een skill als contract leest, gaan in `headings.toml` (besluit 8). Bekende
  lezers: `pr_body_gate.py` (`_AC_KOP_RE`, *Acceptatiecriteria*) en `src/controller/comments.py`
  (de `🤖 <Soort>`-koppen en de vraagvelden). De bouwer inventariseert in (b) de rest.
- **Protocoltokens** zijn geen proza en hangen niet af van de taal: de sleutels en waarden van een
  handoff (`volgende`, `wanneer` ∈ {`nu`, `nacht`}, `anker`, `basis`), het vraagslot en de marker.
  *Mijn keuze:* ze worden vaste Engelse tokens (`next`, `when` ∈ {`now`, `night`}, `anchor`,
  `base`, marker `lighthouse:`). Tot M2 schrijven de skills de nieuwe vorm, en lezers accepteren
  ook de oude, zoals bij de marker (besluit 15).
- `changelog_gen.py` leest *Definition of Done*. Hij hangt aan de release-flow en blijft in paia
  (besluit 17).

## Inventaris

Bron: de indeling van de scope-spec, nagelopen in paia op 2026-10-17. De bouwer maakt in (b) de
volledige lijst per bestand, met zijn literals.

| Pakket | Skills | Agents | Hooks en helpers | Scripts |
|---|---|---|---|---|
| `pack/core` | `start-issue`, `route-issue`, `handoff`, `verify-pr`, `fix-pr`, `close-issue`, `spec-adversarial-review`, `plan-adversarial-review`; de grensgevallen die naar core gaan | `issue-impact-scout`, `spec-adversary` | `main-commit-guard`, `worktree-guard`, `worktree-linkcheck`, `bash-write-gates`, `pr-create-gate`, `pre-commit-tests` (mechanisme); `lib/`: `bash_writes`, `deadline`, `evidence`, `gate_slot`, `git_invocations`, `md_links`, `no_verify`, `pr_body_gate`, het comment-protocol (besluit 15) | `post_artefact.py`, `board_start.py`, `plan_review_record.py`, `review_pair.py`, `codex_usage.py`, de reviewmeting (`measure_start_issue_kpi.py`, `measure_fix_pr_kpi.py`), de instructielaag-checks (`check-instructions.sh`, de generieke checks eruit), de configlezer; templates: `unblock-dependents.yml`, `pull_request_template.md` |
| `pack/python` | `python-schrijven` | | `ruff-format`, `ty-check` | `quality/ratchet.py`, coverage; default `[test_gate]` |
| `pack/shell` | `shell-gates` | | `shellcheck` | `shellcheck-repo.sh` |
| `pack/expo` | `refresh-expo-lockfiles` | | | |

De lijst is niet compleet. Volgens besluit 14 verhuist alles wat een core-skill nodig heeft. De
bouwer loopt in (b) per skill na wat hij aanroept, importeert en leest. De paia-tests voor deze
bestanden verhuizen mee (zie *Binary*).

De inventaris op paia `33833601` (comment op #7) vond nog deze bestanden die niet in de tabel staan:

- `worktree_audit.py`, nodig voor `worktree-audit` en voor `close-issue --only`;
- `unblock_dependents.py`;
- `src/controller/host.py`, nodig voor `plan_review_record.py:88`; gaat op in de configlezer;
- `experiments/lekmeting-keten/lk_common.py` (besluit 27);
- de drempels in `src/config.py` → `CodexUsageConfig` (besluit 23).

`board_start.py` haalt geen board-helpers uit `src/controller/`; zijn enige src-import is
`identity`.

Blijft in paia: `paia-controller` (besluit 9), `usage-guard` en `paia-pauzeer` (M1; *mijn keuze*:
`pause` is gebouwd rond de usage-guard, `usage_window.py` en `usage_resume.py`, en verhuist met
de usage-guard mee in M1), `oom-score-session`,
`refresh-hot-cache`, `statusline`, `vault-write-guard`, de `entire`-hooks, `paia-new-beelink-service`,
de vault-skills en alles wat de scope-spec niet noemt.

**Kruisverwijzingen tussen pakketten.** `fix-pr` (core) roept `quality/ratchet.py` (python) aan.
Een core-skill mag een taalpakket gebruiken als het er is en zegt het anders. Hij faalt nooit omdat
een taalpakket ontbreekt.

### Grensgevallen

Toets: kan de skill met een projectconfig voor labels en status generiek worden? De bouwer toetst
elke skill in (b) en legt het besluit met reden vast in deze spec, in een sectie *Besluit
grensgevallen*. Verwachting van Corneel: `write-issue`, `worktree-audit` en `instrumenteren` gaan
naar `pack/core`; `backlog-stand` en `board-sweep` blijven in paia.

### Besluit grensgevallen

Getoetst op paia `33833601` (comment op #7). De verwachting klopt voor alle vijf.

| Skill | Besluit | Reden |
|---|---|---|
| `write-issue` | `pack/core`, als skelet (besluit 28) | Core-skills leunen op zijn blokkade-invariant: route-issue C:31, 98, 107, 112, 175; start-issue C:13, 23; close-issue C:157, 164, 183, 729. Ook `unblock_dependents.py` leunt erop |
| `worktree-audit` | `pack/core` | `close-issue` roept `worktree_audit.py --only` aan (C:418, 437). De skill gebruikt geen labels en geen board |
| `instrumenteren` | `pack/core`, ingekort | De generieke tekst verwijst naar de checks 3, 4, 5, 7, 8, 10 en 11 van `check-instructions.sh`, en naar de `source`-regel. De checks 6, 9, 12 en 13 zijn paia-eigen (prefix, provenance, persoonlijke skills, `domain`) en blijven in paia |
| `backlog-stand` | blijft in paia | Heeft het board, de controller (`controller-klaar`, status `Controller`) en `scripts/board_report.py` nodig |
| `board-sweep` | blijft in paia | Een config zou de hele skill herhalen: assen, area-set, verwijderlijst en koppen. Heeft het board nodig |

Core-skills die naar `backlog-stand`, `board-sweep` of `paia-controller` verwijzen, doen dat
volgens *Kruisverwijzingen*: als de skill er is, en anders melden ze het. Het gaat om route-issue
C:27, 31, 38, 63, 136, 171; start-issue C:24, 309; close-issue C:166; en handoff C:192.

## Opdeling

Elk sub-issue is een eigen issue in lighthouse, met deze DoD.

**(a0) Bootstrap.** Met de hand in lighthouse: `CLAUDE.md`/`AGENTS.md` met de werkafspraken, een
CI-skelet (GitHub-hosted), en kopieën van `main-commit-guard` en `worktree-guard` met hun
registratie. Geen keten-skills (die verwijzen nog naar paia-scripts).
*DoD:* een commit op `main` in lighthouse wordt geblokkeerd; de CI draait op een PR.

**(a) Skelet en mechanisme.** Go-skelet, CI (golangci-lint, tests, literal-grep), `init` en
`--upgrade` met manifest, marker, de merge van `settings.json` en de beheerde blokken, en
`pack/core` met alleen `main-commit-guard` en de configlezer.
*DoD:* de e2e-test in de CI maakt een verse repo, draait `lighthouse init`, en een commit op `main`
wordt geblokkeerd. `init --upgrade` geeft direct daarna een lege diff.

*DoD-detail:* de e2e-test stuurt een PreToolUse-payload naar het commando dat hij uit de
gemergede `.claude/settings.json` leest, niet naar het script direct. Zo bewijst hij ook de
registratie.

**(b) Rest van `pack/core`.** Alle core-bestanden uit de inventaris, vertaald, met de configlezer,
de koppentabel en het besluit over de grensgevallen. Per overgenomen bestand legt (b) de paia-SHA
vast waar hij vandaan komt, in `pack/SOURCES.toml`. Daarna `lighthouse init` op lighthouse zelf,
op een branch, en de bootstrap van (a0) eruit.
*DoD:* de literal-grep is groen; lighthouse draait zijn eigen `pack/core`.

**(c) Taalpakketten.** `pack/python`, `pack/shell`, `pack/expo`, vertaald.
*DoD:* `init` in een Python-repo stelt `pack/python` voor met de default `[test_gate]`, en de
test-gate blokkeert een commit met een rode test.

**(d) Overstap in paia.** Per pakket één PR in paia:
- `lighthouse init` installeert het pakket; de oude kopieën gaan weg;
- elke verwijzing naar een hernoemde skill (`paia-start-issue` → `lighthouse-start-issue`, ook in
  `src/controller/` en `scripts/`) gaat mee;
- de bumplane (`scripts/skills_upstream_bump.py`) slaat bestanden met een lighthouse-marker over;
- check 11 (`scripts/hooks/check-instructies.sh`) slaat ze over, en `spiegel-uitzonderingen.txt`
  houdt alleen projecteigen skills;
- de andere checks en tests die `lighthouse-*` laten falen, passen mee. Check 6 (eist het prefix
  `paia-` of `origin: external`), check 8 (zijn regex ziet geen hooks in submappen van
  `.claude/hooks/`), check 13 (eist paia's eigen `metadata.domain`) en de tests
  `test_skill_mirror.py` en `test_instructielagen.py` (prefix, `EXPECTED_METADATA`, de vastgepinde
  Nederlandse rijen in `AGENTS.md`). *Mijn keuze:* check 13 slaat skills met de marker over; een
  pakket draagt geen `domain`, want dat is paia's eigen indeling;
- `AGENTS.md` + `CLAUDE.md` blijven onder het plafond van 3000 woorden (nu 2996). De PR haalt de
  keten-afspraken uit het projectdeel als het beheerde blok erbij komt, en eindigt netto ≤ 3000;
- de oude hookregistraties (`.claude/hooks/main-commit-guard.sh` en dergelijke) gaan expliciet uit
  `settings.json`, in dezelfde PR als het verwijderen van de scripts;
- de core-PR schrijft paia's huidige testcommando in `[test_gate]` van `lighthouse.toml`, zodat
  er tussen de core-PR en de python-PR geen commit zonder test-gate is;
- de controller herschrijft een oude `/paia-*`-naam naar de nieuwe vóór de launch en in
  `review_sprong`, met een test. Tijdens het mergen van de core-PR staat `controller-tick.timer`
  stil;
- voor elk overgenomen bestand is `git log <sha>..main -- <pad>` uit `pack/SOURCES.toml` leeg, of
  de wijziging is overgezet naar lighthouse;
- `lighthouse.toml` krijgt `[project] language = "nl"`.
*DoD:* de vier DoD-punten van de pitch, plus een andere repo op de Mac waarin na `lighthouse init`
de main-commit-guard vuurt.

## Verbeterpunten

Een lijst in het issue van elk sub-issue, onder de kop *Verbeterpunten*. Daar komt wat tijdens
de verhuizing beter of handiger kan, zonder dat het in de verhuis-PR wordt opgelost. Na M0 wordt
elk punt een eigen issue. Het eerste punt staat er al:

- **Optionaliteit.** Welke stappen in de core-skills (release, unblock-dependents, tenets, areas,
  history) moeten projectafhankelijk aan of uit kunnen? Vergelijkbaar met hoe het board optioneel
  is (besluit 16).
- **Release-flow en pre-push-keten naar lighthouse.** Wat ervan generiek is, en of `init` git-hooks
  moet installeren (besluit 17).
- **Locale van git.** `main-commit-guard.sh:148`, `pre-commit-tests.sh:197` en `pr_body_gate.py:475`
  matchen op de Engelse git-tekst "not a git repository". Onder een andere locale blokkeren ze ten
  onrechte. Dat faalt dicht, niet open, en is dus een verbetering: `LC_ALL=C` op die aanroepen.
- **`main-commit-guard` en `git push --delete`.** Vanuit een checkout op `main` blokkeert de guard
  ook het verwijderen van een andere remote branch.

## Aan de bouwer

- De interne Go-indeling, de TOML-library en de testopzet.
- De exacte vorm van de harnasvarianten per skill (zie *Indeling*).
- De volledige lijst van literals en de sleutels waaronder ze komen, binnen de tabellen hierboven.
- Welke checks van `check-instructies.sh` generiek zijn en welke paia-eigen blijven.
- De formulering van de vertaling.

## Risico's

- **Hernoemen raakt de controller.** `src/controller/` (`decide.py`, `triage.py`, `comments.py`,
  `overzicht.py`) en scripts als `board_start.py` en `usage_resume.py` noemen skills bij naam. Een
  gemiste verwijzing laat een onbemande run stil de verkeerde of geen skill starten.
  Tegenmaatregel: (d) grept paia op `paia-<keten-skill>` en eist nul treffers buiten de historie.
- **Issues die al lopen.** Open issues dragen handoff-comments met `/paia-start-issue` en
  dergelijke, en de `ALLOWLIST` in `src/controller/comments.py` matcht exact. De controller geeft
  `handoff.volgende` letterlijk door als prompt (`decide.py:2113`). *Mijn keuze:* in (d)
  herschrijft de controller een oude `/paia-*`-naam naar de nieuwe, vóór de launch (zie (d)). Die
  aliassen gaan er in M2 uit, als de controller naar Go gaat. Oude comments worden niet herschreven.
- **Vertaling verandert gedrag.** Tegenmaatregel: per skill één PR-diff waarin de reviewer naast
  elkaar ziet wat er stond en wat er staat; geen herstructurering in dezelfde stap.
- **Twee configlezers tot M2.** Ze kunnen uit elkaar lopen. Tegenmaatregel: dezelfde testgevallen
  voor het gedeelde deel (zie *Configlezer*).
- **Fail-open guards.** `worktree-guard.sh` stopt nu met exit 0 zonder melding als `jq` ontbreekt
  (`worktree-guard.sh:12-14`) en op een paar andere paden. Dat botst met de anti-goals. *Mijn
  keuze:* dit repareren is een toegestane uitzondering op besluit 14, en gebeurt in (b).
