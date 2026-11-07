# Milestones uitsluiten op titel (#241) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een vijfde sleutel `milestone_title_exclude` in `[controller.triage]` sluit issues uit
van de kandidaatkeuze als de titel van hun milestone op een RE2-regex past.

**Architecture:** `repoconfig.triageFrom` compileert de regex één keer bij het laden en zet hem in
`Triage.MilestoneTitleExclude` (`*regexp.Regexp`, `nil` = geen filter). Een ongeldige regex wordt
een fout in `TriageErr`, zoals een onbekende sleutel: de triage-ronde slaat de repo over met
exitcode 2, de tick blijft draaien. `triage.Candidate` krijgt één regel die `SkipMilestone`
geeft als de milestone-titel past, los van `MilestonesOnly`.

**Tech Stack:** Go (`regexp`, `github.com/BurntSushi/toml`), Markdown.

**Spec:** het issue zelf, #241 (geen aparte spec; de AC leggen het HOE vast).

## Global Constraints

- Sleutelnaam exact `milestone_title_exclude`, onder `[controller.triage]`.
- Regex-dialect: Go RE2 (`regexp.Compile`), getoetst met `MatchString` tegen `forge.Milestone.Title`.
- Leeg (`""`) of afwezig: geen filter, kandidaatkeuze ongewijzigd.
- Ongeldige regex: fout in `Repo.TriageErr` die de sleutel `controller.triage.milestone_title_exclude` noemt; `Load` faalt **niet**.
- Past de titel, dan `SkipMilestone`, ook bij `milestones_only = false`.
- Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`. Code en commentaar Engels; docs, commitberichten en dit plan Nederlands.
- Elke PR een fragment in `changelog.d/` (Engels, zie `docs/releasing.md`).

## Review Focus

1. **Issue zonder milestone, `milestones_only = false`, sleutel gezet** — geen milestone, dus geen titel: het issue blijft kandidaat (geen nil-dereference). Test in Task 2.
2. **Hoofdletters** — RE2 is hoofdlettergevoelig: `Foo - on-hold` past niet op `(ON-HOLD|TO-?DO|DONE)$`. Verwacht en gedocumenteerd (`(?i)` voor wie dat wil). Test in Task 2.
3. **Lege string** — `milestone_title_exclude = ""` mag niet alles uitsluiten (een lege regex past op elke titel). Moet `nil` geven. Test in Task 1.
4. **Gesloten milestone met passende titel bij `milestones_only = true`** — blijft `SkipMilestone`; volgorde van de regels doet er niet toe voor de uitkomst, want beide regels geven `SkipMilestone`. Geen aparte test: de uitkomst kan niet verschillen.
5. **Ongeldige regex in `--dry-run`** — ook dan exitcode 2 en een `skip`-regel, zoals een onbekende sleutel. Test in Task 1 (beide modi).

---

### Task 1: `milestone_title_exclude` lezen en valideren

**Files:**
- Modify: `internal/repoconfig/repoconfig.go` — `Triage` (:44-49), `rawTriage` (:78-83), `triageFrom` (:161-191), commentaar bij `TriageErr` (:27-28)
- Test: `internal/repoconfig/repoconfig_test.go` (bij :238-271)
- Test: `internal/cli/triage_test.go` (naast de test rond :1190-1215)

**Interfaces:**
- Produces: `repoconfig.Triage.MilestoneTitleExclude *regexp.Regexp` — `nil` bij afwezig of `""`, anders de gecompileerde regex.

- [ ] **Step 1: Schrijf de falende tests in `repoconfig_test.go`**

`TestLoadTriageMilestoneTitleExclude`, tabel met drie rijen; per rij `Load` zonder fout en
`TriageErr == nil`:

```go
{"absent", "", ""},                                              // MilestoneTitleExclude == nil
{"empty", `milestone_title_exclude = ""`, ""},                   // MilestoneTitleExclude == nil
{"set", `milestone_title_exclude = "(ON-HOLD|TO-?DO|DONE)$"`, "(ON-HOLD|TO-?DO|DONE)$"}, // .String() == want
```

En een rij in `TestLoadTriageBadKeyDoesNotFailLoad`:

```go
"invalid regex": {`milestone_title_exclude = "("`, "controller.triage.milestone_title_exclude"},
```

`TestLoadTriageDefaults` en `TestLoadTriageValues` blijven ongewijzigd groen (`nil` in beide).

- [ ] **Step 2: Schrijf de falende CLI-test in `triage_test.go`**

`TestTriageBadTitleExcludeSkipsRepo`, naar het patroon van de test rond :1190 (rijen `live` en
`dry run`), met `withTriage("milestone_title_exclude = \"(\"\n")`. Asserties: `exitCode(err) == 2`;
`errOut` begint met `"lighthouse: skip " + fx.dir + ": controller.triage.milestone_title_exclude: "`
en telt één `\n`; `fx.judge.called()` leeg, `out == ""`; `assertWrites(t, fx.srv)` zonder writes.

- [ ] **Step 3: Draai de tests, verwacht rood**

Run: `go test ./internal/repoconfig/ ./internal/cli/ -run 'TitleExclude|BadKey'`
Expected: FAIL — `internal/repoconfig`: `MilestoneTitleExclude` bestaat niet (compileerfout);
`internal/cli`: compileert, maar `TestTriageBadTitleExcludeSkipsRepo` faalt op de `errOut`-assertie,
omdat de sleutel nog als `unknown key controller.triage.milestone_title_exclude` gemeld wordt.

- [ ] **Step 4: Implementeer**

- `Triage`: veld `MilestoneTitleExclude *regexp.Regexp`; doc-commentaar: nil = geen filter.
- `rawTriage`: `MilestoneTitleExclude *string \`toml:"milestone_title_exclude"\``.
- `triageFrom`: bij niet-nil en niet-leeg `regexp.Compile`; fout als
  `fmt.Errorf("controller.triage.milestone_title_exclude: %w", err)` in `errs`, anders het veld zetten.
- Commentaar bij `TriageErr`: "an unknown key, a value below 1 or an invalid regex".

- [ ] **Step 5: Draai de tests, verwacht groen**

Run: `go test -race ./internal/repoconfig/ ./internal/cli/`
Expected: PASS

- [ ] **Step 6: Mutatiecheck**

Laat tijdelijk de `errs = append(...)` voor de regex weg: de rij `invalid regex` en
`TestTriageBadTitleExcludeSkipsRepo` moeten rood worden. Zet terug.

- [ ] **Step 7: Commit**

```bash
git add internal/repoconfig/ internal/cli/triage_test.go
git commit -m "feat(repoconfig): lees milestone_title_exclude in [controller.triage]"
```

### Task 2: Kandidaatregel op milestone-titel

**Files:**
- Modify: `internal/triage/facts.go` — `Candidate` (:62-69)
- Test: `internal/triage/facts_test.go` — `TestCandidate` (:29+)

**Interfaces:**
- Consumes: `repoconfig.Triage.MilestoneTitleExclude *regexp.Regexp` (Task 1).

- [ ] **Step 1: Schrijf de falende rijen in `TestCandidate`**

Vooraf in de test: `exclude := regexp.MustCompile("(ON-HOLD|TO-?DO|DONE)$")`,
`withExclude := testCfg; withExclude.MilestoneTitleExclude = exclude`,
`offExclude := repoconfig.Triage{MilestoneTitleExclude: exclude}`. Een hulpje
`titled(t string) func(*Facts)` zet `f.Milestone = &forge.Milestone{Number: 1, Title: t, State: "open"}`.

| name | mod | cfg | want |
|---|---|---|---|
| `title ON-HOLD excluded` | `titled("Foo - ON-HOLD")` | `withExclude` | `SkipMilestone` |
| `title without suffix` | `titled("Foo")` | `withExclude` | `SkipNone` |
| `title DONE, milestones_only off` | `titled("Foo - DONE")` | `offExclude` | `SkipMilestone` |
| `title TODO without dash` | `titled("nexus: x TODO")` | `withExclude` | `SkipMilestone` |
| `title lower case` | `titled("Foo - on-hold")` | `withExclude` | `SkipNone` |
| `no milestone, exclude, milestones_only off` | `f.Milestone = nil` | `offExclude` | `SkipNone` |
| `title ON-HOLD, no key` | `titled("Foo - ON-HOLD")` | `testCfg` | `SkipNone` |

- [ ] **Step 2: Draai, verwacht rood**

Run: `go test ./internal/triage/ -run TestCandidate`
Expected: FAIL op `title ON-HOLD excluded`, `title DONE, milestones_only off`, `title TODO without dash`.

- [ ] **Step 3: Implementeer**

In `Candidate`, een `case` direct na de `MilestonesOnly`-regel:
`cfg.MilestoneTitleExclude != nil && f.Milestone != nil && cfg.MilestoneTitleExclude.MatchString(f.Milestone.Title)`
→ `SkipMilestone`, met een commentaarregel dat dit ook geldt bij `milestones_only = false`.

- [ ] **Step 4: Draai, verwacht groen**

Run: `go test -race ./internal/triage/ ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Mutatiecheck**

Vervang tijdelijk de `case` door `false &&`: de drie rijen uit Step 2 moeten rood worden. Zet terug.

- [ ] **Step 6: Commit**

```bash
git add internal/triage/
git commit -m "feat(triage): sluit milestones uit op titel met milestone_title_exclude"
```

### Task 3: Documentatie en changelog

**Files:**
- Modify: `docs/triage.md` — toml-voorbeeld (:103-109), sleuteltabel (:111-116), foutalinea (:118-121), *Kandidaten* (:139-147)
- Create: `changelog.d/issue-241-milestone-title-exclude.md`

- [ ] **Step 1: `docs/triage.md`**

- Toml-voorbeeld: regel `milestone_title_exclude = "(ON-HOLD|TO-?DO|DONE)$"` erbij.
- Tabelrij: `milestone_title_exclude` | leeg | Een Go-RE2-regex, getoetst tegen de titel van de milestone. Past hij, dan is het issue geen kandidaat, ook met `milestones_only = false`. Hoofdlettergevoelig; `(?i)` vooraan maakt hem ongevoelig. Leeg of afwezig: geen filter.
- Foutalinea: "Een onbekende sleutel, een waarde onder 1 of een ongeldige regex is een fout die de sleutel noemt."
- *Kandidaten*, na de eerste zin: "Met `milestone_title_exclude` valt een issue af waarvan de milestone-titel op die regex past, bijvoorbeeld een geparkeerde milestone die op `ON-HOLD` eindigt."

- [ ] **Step 2: Fragment**

```markdown
---
type: added
---
`lighthouse triage` reads `milestone_title_exclude` in `[controller.triage]`: an issue in a milestone whose title matches that regex is no candidate.
```

- [ ] **Step 3: Verifieer**

Run: `python3 .lighthouse/scripts/gate_go.py -- go test -race ./... && golangci-lint run`
Expected: PASS, geen bevindingen. `md-link-check` blokkeert niet.

- [ ] **Step 4: Commit**

```bash
git add docs/triage.md changelog.d/issue-241-milestone-title-exclude.md
git commit -m "docs(triage): milestone_title_exclude in tabel en kandidaatregels"
```

## Na de merge

AC *In een release*: een `added`-fragment releaset op de eerstvolgende maandag 06:00 UTC, of eerder
met de `release`-workflow met de hand. De PR draagt daarom `Refs #241`, niet `Closes`; het issue
sluit pas na de release (`/lighthouse-close-issue`).
