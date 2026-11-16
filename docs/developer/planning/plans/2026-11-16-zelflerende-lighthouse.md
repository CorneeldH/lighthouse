# Zelflerende lighthouse — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** de paia-milestones #52 en #63 (met #67 en #68) omzetten naar drie lighthouse-milestones met
hun issues, en paia daarna opruimen.

**Architectuur:** geen code. Het werk is GitHub-artefacten: milestones en issues in
`CorneeldH/lighthouse`, sluiten, inkorten en hernoemen in `CorneeldH/personal_ai_agent`. Elke
lighthouse-artefact gaat via `/lighthouse-write-issue`, dat vorm, labels, milestone-toets en
blokkades regelt. Een voortgangstabel in deze repo maakt het werk hervatbaar.

**Gereedschap:** `gh api` (REST; GraphQL raakte deze sessie twee keer de rate limit),
`/lighthouse-write-issue`, `/lighthouse-write-pr`.

**Spec:** [2026-11-16-zelflerende-lighthouse-design.md](../specs/2026-11-16-zelflerende-lighthouse-design.md)

## Globale randvoorwaarden

- Artefacten in het Nederlands.
- Elk nieuw lighthouse-issue noemt zijn herkomst als volledige URL:
  `https://github.com/CorneeldH/personal_ai_agent/issues/<n>`.
- Een lighthouse-issue beschrijft een vaardigheid voor elke repo. Paia-paden, paia-scripts en
  Beelink-namen staan er alleen in als voorbeeld van een eerste toepassing, nooit als eis.
- Geen paia-issue dicht of ingekort vóórdat het lighthouse-issue waar het naar verwijst bestaat.
- De werknamen T1–T7 en K1–K7 staan niet in issuetitels; wel in de voortgangstabel.
- Een blokkade (`blocked_by`) komt vóór het label `3-blocked-issue` (zie de skill, *3e*).

## Review-focus

- **Halve batch na een rate limit.** Een gestopte uitvoering moet hervatten zonder dubbele issues:
  de voortgangstabel (taak 1) wordt na elk aangemaakt artefact bijgewerkt en is de bron bij hervatten.
- **Dubbel met een bestaand issue.** De skill controleert in stap *0* of er al een issue is.
  Bij een treffer: het bestaande issue aanvullen in plaats van een nieuw maken, en dat in de tabel zetten.
- **Paia-specifiek gekopieerd.** Elke body wordt vóór het aanmaken gecontroleerd op `src/`,
  `scripts/`, `deploy/beelink`, `experiments/` en `~/.local/state/paia`. Een treffer is alleen
  toegestaan als voorbeeld, onder "Eerste toepassing".
- **Dode verwijzing in paia.** Taak 6 controleert elke URL die hij in paia plaatst, voordat hij sluit.
- **Een milestone zonder DoD.** Elke milestone-beschrijving heeft de zeven pitch-koppen van de skill;
  de controle in taak 2 eist ze.

---

### Task 1: Voortgangstabel

**Bestanden:**
- Aanmaken: `docs/developer/planning/plans/2026-11-16-zelflerende-lighthouse-voortgang.md`

**Interfaces:**
- Levert: een tabel met de kolommen `Werknaam | Lighthouse | Herkomst paia | Paia-actie | Status`.
  Een rij per artefact uit de spec: drie milestones, T1–T7, K1–K7, het losse #1395-issue,
  explore/exploit-ontwerp (paia #1553), multi-harness-slice (paia #1126), en de verhuizingen van
  lighthouse #103 en #168. `Paia-actie` is `sluiten`, `inkorten` of `—`, volgens de Overzettabel
  van de spec.

- [ ] **Stap 1: Schrijf de tabel** met lege kolommen `Lighthouse` en `Status`.
- [ ] **Stap 2: Controleer** dat elke paia-rij uit de Overzettabel van de spec erin staat (#1495,
  #1088, #1087, #1082, #993, #1395, #1285, #470, #590, #1194, #1193, #1286, #1287, #1205, #644, #667,
  #1553, #1126) en dat #1199 de actie `—` heeft (blijft in paia).
- [ ] **Stap 3: Commit** — `docs(plan): voortgangstabel zelflerende lighthouse`.

### Task 2: De drie milestones

**Interfaces:**
- Gebruikt: de secties *Milestone 1*, *Milestone 2* en *Milestone 3* van de spec.
- Levert: drie milestonenummers in de voortgangstabel.

- [ ] **Stap 1: Maak de milestones** via `/lighthouse-write-issue` (*Milestones*):
  - *Telemetrie: wat voegt elk onderdeel toe*. Appetite Large.
  - *Kwaliteitspoorten per taalpack*. Appetite Large.
  - *Explore/exploit: lighthouse stelt zichzelf bij*. Geparkeerd: de appetite start pas zodra
    milestone 1 een week gegevens heeft.

  Problem, Solution, DoD en No-Gos komen uit de spec. *Nog niet besloten* krijgt de punten uit
  *Aan de bouwer*.
- [ ] **Stap 2: Controleer.** Voor elk van de drie nummers:

  ```bash
  gh api repos/CorneeldH/lighthouse/milestones/<n> --jq .description
  ```

  Verwacht: alle zeven koppen van de pitch. De uitvoer van `lighthouse headings` voor de drie
  `milestone_*`-sleutels staat erin.
- [ ] **Stap 3: Werk de voortgangstabel bij en commit** —
  `docs(plan): milestones zelflerende lighthouse aangemaakt`.

### Task 3: Issues van milestone 1 (T1–T7)

**Interfaces:**
- Gebruikt: het milestonenummer van taak 2; de T-tabel en de begrippen uit de spec.
- Levert: zeven issuenummers.

- [ ] **Stap 1: Maak T1 eerst.** Gegevensmodel voor traject, inzet, indeling, bevinding en
  uitkomst. De acceptatiecriteria noemen:
  - de indeling `standaard | schaduw:<onderdeel> | variant:<dimensie>=<waarde>`;
  - de bronnen dispatcher-runs plus interactieve Claude Code-sessies, gekoppeld via branch en worktree;
  - idempotente schrijvers;
  - geen tweede opslag.
- [ ] **Stap 2: Maak T2–T6**, elk met `blocked_by` op T1. Vul de acceptatiecriteria uit de T-tabel
  van de spec aan met deze waarden:
  - T3: de beperking tot de host uit `host.toml` vervalt; de index is een verwijzing plus inhoudshash.
  - T4: het venster is 7 dagen, en de bronnen zijn fix-commits, reverts, bug-issues en
    `fix-pr`-rondes.
  - T6: de vier antwoorden over de planreview uit de DoD van milestone 1, plus het label
    "correlatie".
- [ ] **Stap 3: Maak T7** met `blocked_by` op T1 en op lighthouse #325.
- [ ] **Stap 4: Controleer.**

  ```bash
  gh api "repos/CorneeldH/lighthouse/issues?milestone=<n>&state=open&per_page=100" --jq length
  ```

  Verwacht: `7`. Daarna T2–T7 elk:

  ```bash
  gh api repos/CorneeldH/lighthouse/issues/<n>/dependencies/blocked_by --jq '.[].number'
  ```

  Verwacht: T1, en bij T7 ook `325`.
- [ ] **Stap 5: Werk de voortgangstabel bij en commit.**

### Task 4: Issues van milestone 2 (K1–K7)

**Interfaces:**
- Gebruikt: het milestonenummer van taak 2; de K-tabel en het onderscheid poort/meting uit de spec.
- Levert: zeven issuenummers.

- [ ] **Stap 1: Maak K1–K4 en K7.** Elk issue noemt welke packs het raakt (`python`, `go`, `node`,
  `expo`, en voor K1 ook `core`). K1: de lijst van canaries is afgeleid van de regellijst van de
  hook zelf. K4: noemt deze repo (`.golangci.yml`) als eerste toepassing.
- [ ] **Stap 2: Maak K5 en K6**, elk met `blocked_by` op lighthouse #121. Beide zijn een meting,
  geen poort: een trend in `lighthouse.db`, en per ronde hoogstens één beslisissue.
- [ ] **Stap 3: Controleer.** Het aantal issues in de milestone is `7`, en bij K5 en K6 staat `121`
  in `blocked_by`. Gebruik dezelfde commando's als in taak 3.
- [ ] **Stap 4: Werk de voortgangstabel bij en commit.**

### Task 5: Milestone 3, het losse issue en de verhuizingen

**Interfaces:**
- Gebruikt: het milestonenummer van milestone 3.

- [ ] **Stap 1: Maak het ontwerpissue explore/exploit** (herkomst paia #1553). Het gebruikt het
  gegevensmodel van T1 en noemt de schaduwmeting op reviews als eerste vergelijking.
- [ ] **Stap 2: Maak het issue voor de multi-harness-slice** (herkomst paia #1126). Codex- en
  OpenCode-sessies worden traject-bron, en een extra harness krijgt een controlematrix met
  positieve en negatieve proeven.
- [ ] **Stap 3: Zet lighthouse #103 en #168 in milestone 3:**

  ```bash
  gh api -X PATCH repos/CorneeldH/lighthouse/issues/<n> -F milestone=<m>
  ```

- [ ] **Stap 4: Maak het losse #1395-issue** zonder milestone. Het beoordeelt elke regel in het
  `pack/core`-blok van `AGENTS.md` en in `pack/*/instructions/`, met de toetssteen uit paia #1395:
  hook, feit of aantoonbare fout.
- [ ] **Stap 5: Controleer.** Milestone 3 heeft 4 open issues: de twee nieuwe, #103 en #168.
- [ ] **Stap 6: Werk de voortgangstabel bij en commit.**

### Task 6: Paia opruimen

**Interfaces:**
- Gebruikt: de kolommen `Lighthouse` en `Paia-actie` van de voortgangstabel. Elke rij met een
  paia-actie heeft hier een lighthouse-nummer.

- [ ] **Stap 1: Sluit de verhuisde issues** (actie `sluiten`): #1495, #1088, #1087, #1082, #993,
  #1285, #1193, #1205, #1553 en #1126. Eerst een comment
  `Verhuisd naar <lighthouse-URL('s)>: lighthouse levert dit nu voor elke repo.`, daarna:

  ```bash
  gh api -X PATCH repos/CorneeldH/personal_ai_agent/issues/<n> -f state=closed -f state_reason=not_planned
  ```

- [ ] **Stap 2: Kort de gesplitste issues in** (actie `inkorten`): #1395, #470, #590, #1194, #1286,
  #1287, #644 en #667. De body houdt alleen het toepassingsdeel uit de Overzettabel, met bovenaan
  `De vaardigheid staat in <lighthouse-URL>; dit issue past haar toe op paia.` Het issue blijft open
  in milestone #52. #1395 staat nu in #63 en verhuist naar #52 (`-F milestone=52`).
- [ ] **Stap 3: Werk de paia-milestones bij:**
  - #52: hernoemen naar `paia: kwaliteitspoorten toepassen`. De beschrijving verwijst naar de
    lighthouse-milestone *Kwaliteitspoorten per taalpack*. De DoD houdt alleen de open
    toepassingsissues plus #1199.
  - #63, #67 en #68: een verwijzing naar de lighthouse-milestones bovenaan de beschrijving, dan
    sluiten.
- [ ] **Stap 4: Controleer:**

  ```bash
  gh api "repos/CorneeldH/personal_ai_agent/milestones?state=open&per_page=100" --jq '.[].title' | grep -i lighthouse
  ```

  Verwacht: geen treffer.

  ```bash
  gh api "repos/CorneeldH/personal_ai_agent/issues?milestone=52&state=open&per_page=100" --jq '.[].number'
  ```

  Verwacht: precies #470, #590, #644, #667, #1194, #1199, #1286, #1287 en #1395.
- [ ] **Stap 5: Werk de voortgangstabel bij en commit.**

### Task 7: Spec bijwerken en PR

**Bestanden:**
- Wijzigen: `docs/developer/planning/specs/2026-11-16-zelflerende-lighthouse-design.md`. In de
  Overzettabel komt een kolom `Lighthouse` met de echte nummers als links.
- Aanmaken: `changelog.d/fix-zelflerend-milestones-design.md` met `type: internal`.

- [ ] **Stap 1: Werk de spec bij en schrijf het fragment.**
- [ ] **Stap 2: Controleer de links.** De `md-link-check`-hook draait bij elke Edit of Write van
  een `.md`-bestand. Verwacht: geen blokkade. Een blokkade noemt de dode of absolute link.
- [ ] **Stap 3: Commit, push, PR** via `/lighthouse-write-pr`. De body is
  `No issue: herverdeling van paia-milestones naar lighthouse, vastgelegd in spec en voortgangstabel`.
