# Labeltaxonomie voor deze repo — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development of
> superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken
> checkboxen (`- [ ]`).

**Doel:** lighthouse krijgt de labelassen van personal_ai_agent: een taxonomiebestand, de sleutels
in `lighthouse.toml`, de labels in de repo en precies één area-label op elk open issue.

**Architectuur:** geen code. De configlezer (`pack/core/lib/config.py`) kent `labels.*` en
`paths.labels` al; `lighthouse-write-issue` en `lighthouse-start-issue` lezen ze al. Dit plan vult
alleen de configuratie, het document en de toestand op GitHub.

**Tech stack:** Markdown, TOML, `gh` (REST via `gh api` en `gh label`).

**Spec:** issue #94 (geen apart spec-bestand). Referentie: `docs/developer/labels.md` en
`[labels]` in `lighthouse.toml` van CorneeldH/personal_ai_agent.

## Besluiten

Dit plan legt vast wat het issue open liet:

- **Geen locatie-as.** lighthouse is software: alles is op te pakken in de code van deze repo.
  De locatie-as van personal_ai_agent bestaat omdat die repo software én configuratie van
  machines is (Mac, Beelink); dat is de uitzondering, niet de regel. `labels.location_prefix`
  blijft weg; de invariantencheck slaat de as dan over.
- **Blokkade-as met `3-`-prefix**, zes labels, namen en kleuren als in personal_ai_agent.
  `blocked-issue` en `needs-decision` worden hernoemd, de andere vier zijn nieuw.
- **De GitHub-standaardlabels blijven** (`bug`, `enhancement`, `documentation`, …) als vrije
  domein-as, zoals in personal_ai_agent. Opruimen hoort niet bij dit issue.
- **Acht areas uit het issue, alle acht.** `packs` en `usage` hebben nu geen open issue, maar wel
  recent werk (#21, #65, #66); een area is een blijvende as.
- **Grens product/meta:** voor wie is het? Wie lighthouse gebruikt → `product` (ook de kaarten en
  de installatiehandleiding). Wie aan lighthouse werkt → `meta`.

## Globale eisen

- Artefacten in het Nederlands (`[project] language = "nl"`): `labels.md`, commitberichten, PR.
- Links in `.md` relatief en naar een bestaand doel; `worktree-linkcheck` blokkeert anders.
- Elke shellopdracht met `cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-94-labeltaxonomie`
  ervoor; de cwd drijft terug naar de hoofdcheckout.
- `gh`-aanroepen met `dangerouslyDisableSandbox: true` (Go-TLS wordt anders geblokkeerd).
- Niets onder `pack/`: geen `init --upgrade` nodig.

## Reviewfocus

1. **Venster tussen hernoemen en merge.** Na `gh label edit` heet het label `3-blocked-issue`,
   maar `main` leest tot de merge de default `blocked-issue`. Een `/lighthouse-close-issue` of
   `unblock_dependents.py` op `main` in dat venster mist het label. Taak 2 hernoemt daarom pas
   vlak voor de PR, en de PR wordt direct gemerged.
2. **Hernoemen, niet verwijderen en opnieuw maken.** Alleen `gh label edit --name` houdt de
   labels op de issues (#93, #91, #90, #89, #88, #78, #100–#104 dragen `blocked-issue`).
   Controle in taak 2, stap 3.
3. **Issues met een PR.** De invariantencheck slaat PR's over; een area-label op een PR is niet
   nodig.
4. **Een nieuw issue tussen taak 3 en de controle.** De check geeft het dan; label het en draai
   de check opnieuw.
5. **#94 zelf** krijgt ook een area-label (`2-area:meta`), anders faalt de check op dit issue.

---

### Taak 1: taxonomiebestand en configuratie

**Bestanden:**
- Maak: `docs/developer/labels.md`
- Wijzig: `lighthouse.toml` (nieuwe tabellen `[labels]` en `[paths]`, na `[forge]`)

**Interfaces:**
- Levert: `labels.area_prefix = "2-area:"`, de zes blokkadelabels met `3-`-prefix,
  `paths.labels = "docs/developer/labels.md"`. Taak 2 en 3 gebruiken exact deze namen.

- [ ] **Stap 1: schrijf `docs/developer/labels.md`**

  Vorm als in personal_ai_agent, zonder de locatie-as en zonder de board- en milestonesecties
  (die horen bij `lighthouse-write-issue`). Inhoud:

  - Kop `# Labeltaxonomie`, één alinea: `lighthouse-write-issue` leest dit bestand via
    `[paths] labels`; de skill kent zelf geen labels.
  - Tabel met twee assen: `2-area:*` precies 1 (*Waar hoort het bij?*), `3-*` 0 of 1 (*Waarom kan
    dit nu niet doorlopen?*). Eén zin: er is geen `1-loc:`-as, want lighthouse is software en
    alles is op te pakken in de code; een locatie-as hoort bij een repo die ook machines
    configureert. Eén
    zin: de GitHub-standaardlabels (`bug`, `enhancement`, `documentation`) zijn een vrije as.
  - Sectie `## 2-area: — waar hoort het bij`, tabel Area · Kleur · Vuistregel:

    | Area | Kleur | Vuistregel |
    |---|---|---|
    | `2-area:core` | `4C1D95` | `pack/core`: hooks, keten-skills, gates |
    | `2-area:packs` | `5B21B6` | taalpakketten (`pack/go`, `pack/python`, `pack/shell`, `pack/expo`) |
    | `2-area:init` | `6D28D9` | `lighthouse init`, `--upgrade`, het manifest, het blok in `AGENTS.md`, en het weer verwijderen |
    | `2-area:server` | `7C3AED` | `lighthouse serve`, SQLite, de SPA, de forge-lezer |
    | `2-area:dispatcher` | `8B5CF6` | tick, decide, apply, werkplekken (M2), en kenn-forge als werkplek |
    | `2-area:usage` | `A78BFA` | de venstermeters en de usage-guard |
    | `2-area:product` | `C4B5FD` | release, website, installatie, licentie, documentatie voor gebruikers |
    | `2-area:meta` | `DDD6FE` | CI, docs voor wie aan lighthouse werkt, planning, deze repo zelf |

    Daaronder: precies één; passen er twee, kies het zwaartepunt. De kleuren lopen van donker
    (fundament) naar licht.
  - Grensgevallen, elk één regel met een voorbeeldissue:
    - core/usage: de usage-guard is een hook in `pack/core`, maar hoort bij `usage` (#66).
    - core/init: wat `init` schrijft is `init`; wat de geschreven hook of skill doet is `core`.
    - product/meta: voor wie is het? Gebruiker → `product` (#99–#104, #88); ontwikkelaar → `meta`.
    - product/init: platformsteun en release-binaries zijn `product` (#90, #78); de
      opdracht `init` zelf is `init` (#91, #46).
    - dispatcher/server: wat de controller doet is `dispatcher`; wat je ervan ziet in de SPA is
      `server` (#69).
  - Sectie `## 3- — de blokkade`: de toets (*kan ik dit gewoon tegen de agent zeggen?*), hoogstens
    één, laagste rang wint, en de tabel Rang · Label · Kleur · Bindt wanneer, letterlijk de zes
    rijen uit personal_ai_agent (`docs/developer/labels.md`, sectie 3c):

    | Rang | Label | Kleur | Bindt wanneer |
    |---|---|---|---|
    | 1 | `3-needs-shaping` | `FBCA04` | Wat/waarom nog niet scherp. Alles erna is prematuur |
    | 2 | `3-needs-decision` | `FBCA04` | Eén keuze van Corneel blokkeert, de rest is helder |
    | 3 | `3-blocked-issue` | `D93F0B` | Wacht op een ander issue in deze repo |
    | 4 | `3-blocked-external` | `D93F0B` | Wacht op derde partij, API, vendor, levering |
    | 5 | `3-needs-hands` | `FBCA04` | Hij moet instellen, invoeren, aansluiten, aanschaffen |
    | 6 | `3-needs-testing` | `FBCA04` | Bouw staat; hij moet oordelen of het doet wat bedoeld is |

    Amber = de bal ligt bij Corneel, rood = buiten. `3-blocked-issue` gaat nooit mee in
    `gh issue create`; de `blocked_by`-relatie komt eerst.

- [ ] **Stap 2: voeg toe aan `lighthouse.toml`, na `[forge]`**

  ```toml
  [labels]
  blocked_issue = "3-blocked-issue"
  blocked_external = "3-blocked-external"
  needs_shaping = "3-needs-shaping"
  needs_decision = "3-needs-decision"
  needs_hands = "3-needs-hands"
  needs_testing = "3-needs-testing"
  area_prefix = "2-area:"

  [paths]
  labels = "docs/developer/labels.md"
  ```

- [ ] **Stap 3: controleer dat de configlezer het leest**

  Run:
  ```bash
  for k in labels.area_prefix labels.blocked_issue labels.needs_testing paths.labels; do python3 .lighthouse/lib/lighthouse_lib/config.py "$k"; done; python3 .lighthouse/lib/lighthouse_lib/config.py labels.location_prefix; echo "rc=$?"
  ```
  Verwacht: `2-area:`, `3-blocked-issue`, `3-needs-testing`, `docs/developer/labels.md`, dan
  `config: labels.location_prefix is not set` en `rc=1`.

- [ ] **Stap 4: installeer de web-afhankelijkheden**

  `ci/domains.py:29` telt `lighthouse.toml` als web-pad; zonder `web/node_modules` blokkeert de
  test-gate met `COULD NOT CHECK`. Draai zonder sandbox (netwerk):
  ```bash
  cd web && npm ci
  ```

- [ ] **Stap 5: commit**

  ```bash
  git add docs/developer/labels.md lighthouse.toml
  git commit -m "docs(labels): labeltaxonomie met area- en blokkade-as (#94)"
  ```
  De test-gate draait bij de commit de web-, go- en python-checks; rood blokkeert.

### Taak 2: labels in de repo

Geen bestanden. Voer uit **na** taak 1 en vlak voor de PR (reviewfocus 1).

- [ ] **Stap 1: nulmeting, dan hernoem de twee bestaande blokkadelabels**

  Direct vóór het hernoemen; noteer het getal (`VOOR`):
  ```bash
  gh api "repos/CorneeldH/lighthouse/issues?state=open&labels=blocked-issue&per_page=100" --jq 'length'
  ```
  Dan:
  ```bash
  gh label edit blocked-issue --name 3-blocked-issue --color D93F0B
  gh label edit needs-decision --name 3-needs-decision --color FBCA04
  ```

- [ ] **Stap 2: maak de vier nieuwe blokkadelabels en de acht areas**

  `gh label create <naam> --color <kleur> --description "<omschrijving>"`, met namen en kleuren
  uit taak 1. Omschrijving: de kolom *Bindt wanneer* of *Vuistregel*, ingekort tot ≤ 100 tekens
  (GitHub-limiet).

- [ ] **Stap 3: controleer**

  ```bash
  gh label list --limit 100 --json name,color --jq '.[] | select(.name|test("^(2-area:|3-)")) | "\(.name) \(.color)"' | sort
  gh api "repos/CorneeldH/lighthouse/issues?state=open&labels=3-blocked-issue&per_page=100" --jq 'length'
  ```
  Verwacht: veertien regels, kleuren gelijk aan taak 1 (hoofdletterongevoelig). Tweede opdracht:
  gelijk aan `VOOR` uit stap 1.

### Taak 3: area-label op elk open issue

Geen bestanden. Na taak 2.

- [ ] **Stap 1: zet de labels**

  `gh issue edit <nr> --add-label <area>`, één aanroep per issue. Indeling, getoetst tegen de 27
  open issues op 2026-10-27 (#106 en #69 zijn inmiddels gesloten):

  | Area | Issues |
  |---|---|
  | `2-area:core` | #80 |
  | `2-area:init` | #95, #91, #47, #46, #19 |
  | `2-area:server` | #44 |
  | `2-area:dispatcher` | #85, #77, #73, #56, #35, #34, #31 |
  | `2-area:product` | #104, #103, #102, #101, #100, #99, #93, #90, #89, #88, #86, #78 |
  | `2-area:meta` | #94 |

  Een issue dat na 2026-10-27 is geopend: kies met de vuistregels uit `labels.md`.

- [ ] **Stap 2: draai de invariantencheck van `lighthouse-write-issue` (stap 6)**

  Het blok uit `.claude/skills/lighthouse-write-issue/SKILL.md`, sectie *6. Check the
  invariants*, letterlijk, maar met `set -o pipefail` ervoor en `echo "rc=$?"` erna (een
  `gh api`-fout geeft anders lege uitvoer met rc 0).
  Verwacht: `rc=0`, stdout leeg. Op stderr staat precies één verwachte regel,
  `config: labels.location_prefix is not set`: er is geen locatie-as, dus die as wordt
  overgeslagen. Elke stdout-regel is een issue zonder of met twee area-labels, of met meer dan
  één blokkadelabel: herstel en draai opnieuw. `rc` ≠ 0: de GitHub-read faalde, draai opnieuw.

- [ ] **Stap 3: push en PR**

  ```bash
  git push -u origin HEAD
  ```
  Dan schrijft `/lighthouse-verify-pr` de body (`Closes #94`, één bewijsrij per AC: het bestand, de
  `config.py`-uitvoer uit taak 1 stap 3, de labellijst uit taak 2 stap 3, de lege uitvoer uit
  stap 2).

- [ ] **Stap 4: merge**

  Na groen: draai stap 2 opnieuw (een issue kan tijdens de CI-wachttijd zijn geopend), herstel
  tot de uitvoer leeg is, en merge direct (reviewfocus 1). Daarna:
  ```bash
  git -C /home/corneel/repositories/lighthouse pull --ff-only
  ```
  Andere worktrees lezen tot een rebase nog `blocked-issue`; een `--remove-label blocked-issue`
  daar faalt luid (het label bestaat niet meer).
