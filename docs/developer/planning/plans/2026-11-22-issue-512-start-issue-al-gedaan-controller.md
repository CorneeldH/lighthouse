# start-issue in een controller-run: "al gedaan" eindigt in een vervolgstap — Implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** Een controller-run van `lighthouse-start-issue` die bij stap 2 ("Already done?") `likely` of `not measurable` vindt, eindigt in een handoff of een `lighthouse ask`, nooit in de keuze als chattekst.

**Architectuur:** Alleen skilltekst. De twee controller-regels in stap 2 (`likely`, `not measurable`) krijgen een uitkomst die de controller leest: bij `likely` een handoff `next: /lighthouse-close-issue <nr>` (close-issue entry B controleert zelf de criteria en vraagt bij twijfel zelf, B7), bij `not measurable` of een twijfelgeval één bundel via `lighthouse ask`. Hetzelfde patroon als #394 voor close-issue B7. Geen Go-code in tick of dispatch (no-go van de milestone: geen nieuwe controllerfuncties).

**Tech Stack:** Markdown-skills in `pack/core/skills/start-issue/{claude,agents}/SKILL.md`, Go-contracttests in `internal/packcontract/`.

**Spec:** geen spec; het issue (#512) is de bron en de acceptatiecriteria leggen de opzet vast.

## Globale randvoorwaarden

- Skilltekst en changelog in het Engels; dit plan en `docs/tick.md` in het Nederlands.
- Geïnstalleerde kopieën (`.claude/skills/`, `.agents/skills/`) nooit met de hand bewerken: `go run ./cmd/lighthouse init --upgrade` en committen (CI eist `No changes.`).
- De skilltekst bevat geen literaal issuenummer van deze repo (`skills_test.go:763-766`).
- De bestaande test `TestStartIssueAsksAlreadyDoneBeforeTheWorktree` (`issueflow_test.go:261`) blijft groen: "Controller run (if present)", "already done? likely — ", "/lighthouse-close-issue <nr>", "carry on anyway", "already done? not measurable — fetch failed" blijven in stap 2; "gh issue close" en "wontfix" komen er niet in.
- Voor een commit: `lighthouse imports check` naast `[test_gate] command`.

## Review Focus

- Controller-run met `likely` en een covering commit zonder `Refs`-PR (directe fix): de run schrijft toch de handoff naar close-issue, dat verifieert zelf. Test: stap 2 noemt de handoff voor élke `likely` in een controller-run.
- Controller-run met `not measurable — fetch failed`: eindigt in `lighthouse ask`, niet in een handoff (close-issue zou op verouderde data meten). Test: de bullet noemt `lighthouse ask` en niet `next: /lighthouse-close-issue`.
- Interactieve run: ongewijzigd, de keuze blijft chattekst aan de gebruiker. Test: de oude interactieve formulering "The user decides" blijft staan.
- Twijfel tussen `likely` en `no` (commit noemt het issue maar dekt de AC onzeker): de run vraagt via `lighthouse ask` in plaats van te raden. Test: stap 2 noemt "in doubt" met `lighthouse ask`.
- Een run die met alleen "as the last text" eindigt, wordt weer teruggegeven. Test: stap 2 bevat die zin niet meer.

---

### Taak 1: Skillstap + contracttest

**Bestanden:**
- Wijzig: `pack/core/skills/start-issue/claude/SKILL.md` (stap 2, bullets `likely` regels ~94-98 en `not measurable` ~99-103)
- Wijzig: `pack/core/skills/start-issue/agents/SKILL.md` (dezelfde bullets, identieke tekst)
- Test: `internal/packcontract/issueflow_test.go` (naast regel 261)

**Interfaces:**
- Consumes: `variants(t, startIssue)`, `section`, `flat`, `wantAll`, `wantNone` uit `helpers_test.go` en `issueflow_test.go`.
- Produces: functie `TestStartIssueAlreadyDoneEndsInFollowUpInControllerRun(t *testing.T)`.

- [ ] **Stap 1: Schrijf de falende test**

Boven de test een regel `// Classification: fails when a controller run that finds the work already done ends with the choice as chat text, or loses its handoff to /lighthouse-close-issue or its lighthouse ask.` Dan een test met `for variant, body := range variants(t, startIssue)`, `step2 := flat(section(t, body, "### 2.", "### 3."))` en:
  - `wantAll(t, step2, "/lighthouse-write-handoff", "next: /lighthouse-close-issue <nr>", "lighthouse ask", "--phase start", "in doubt")`
  - `wantNone(t, step2, "as the last text")`

- [ ] **Stap 2: Draai hem rood**

Run: `go test ./internal/packcontract/ -run TestStartIssueAlreadyDoneEndsInFollowUpInControllerRun`
Verwacht: FAIL op ontbrekende strings.

- [ ] **Stap 3: Pas beide SKILL.md-varianten aan**

In stap 2, bullet `already done? likely`: de zin "Controller run (if present): put the choice ... as the last text and stop; you never close yourself." vervangen door: in een controller-run post `/lighthouse-write-handoff` (controller mode) een handoff `next: /lighthouse-close-issue <nr>` met het bewijs (commit-sha) onder `Context:`, vanuit de worktree van het issue, en stop. De controller start dan `/lighthouse-close-issue` (rij `handoff`); close-issue entry B verifieert de criteria en vraagt zelf bij open criteria. Je sluit zelf nooit. Is het bewijs twijfelachtig (*in doubt*: de commit noemt het issue maar dekt de AC onzeker): één bundel `lighthouse ask --issue <nr> --phase start` met opties "close through `/lighthouse-close-issue <nr>`", "carry on anyway", `--recommended` op de eerste, en stop zonder handoff.

In bullet `not measurable — fetch failed`: de controller-zin vervangen door één bundel `lighthouse ask --issue <nr> --phase start` met opties "try the fetch again" en "carry on anyway", `--recommended` op de eerste, en stop zonder handoff (close-issue zou op verouderde data meten). De interactieve zin ("the user chooses") blijft. Beide teksten in `[project] language` van de ask-velden noemen; het commando zelf in een codeblok is niet nodig, een verwijzing naar het voorbeeld in de controller-instructies volstaat. Geen literaal issuenummer.

- [ ] **Stap 4: Draai groen**

Run: `go test ./internal/packcontract/ -run 'StartIssue|Skills'`
Verwacht: PASS, ook `TestStartIssueAsksAlreadyDoneBeforeTheWorktree`.

- [ ] **Stap 5: Mutatiecontrole**

Verwijder tijdelijk `next: /lighthouse-close-issue <nr>` uit één variant; de nieuwe test moet rood worden voor die variant. Herstel.

- [ ] **Stap 6: Dogfood en commit**

Run: `go run ./cmd/lighthouse init --upgrade`, dan `lighthouse imports check`.

```bash
git add pack/core/skills/start-issue internal/packcontract/issueflow_test.go .claude/skills .agents/skills
git commit -m "fix(pack): start-issue in een controller-run eindigt bij al gedaan in een handoff of vraag"
```

### Taak 2: docs/tick.md en changelog

**Bestanden:**
- Wijzig: `docs/tick.md` (alinea onder de tabel, bij "Regel 14", regel ~334)
- Maak: `changelog.d/issue-512-start-issue-already-done-controller-run.md`

- [ ] **Stap 1: Beschrijf de afloop in `docs/tick.md`**

Voeg bij "Regel 14" een zin toe: een start-issue-run in een controller-run die vindt dat het werk al op de standaardbranch staat (`already done? likely`), eindigt in een Handoff `next: /lighthouse-close-issue <nr>` (rij 13c start die nacontrole), of in een Vraag bij twijfel of een mislukte fetch. Eindigt ze zonder beide, dan geeft regel 14 *geen handoff of vraag*. Geen nieuwe rij: het is geen nieuwe route, alleen een bestaande (13c) die nu ook vanuit start-issue bereikt wordt.

- [ ] **Stap 2: Fragment**

Bestand met front matter `type: changed` en één Engelse zin: "A controller run of start-issue that finds the work already on the default branch now ends in a handoff to /lighthouse-close-issue or a question instead of plain chat text, so the controller no longer hands the issue back."

- [ ] **Stap 3: Controleer en commit**

Run: `.claude/hooks/lighthouse/run.sh md-link-check` is automatisch; draai verder `go test ./internal/packcontract/ ./...` en `lighthouse imports check`.
Verwacht: PASS.

```bash
git add docs/tick.md changelog.d
git commit -m "docs(tick): afloop van al gedaan in start-issue en changelog-fragment"
```

## Open punten voor de uitvoerder

- Valideer dat `lighthouse ask --phase` elke vrije string accepteert (`internal/cli/ask.go`, `--phase` is een vrije `string`, getest met `p`): dan is `--phase start` geldig; zo niet, gebruik `execution`.
- Bevestig in `internal/dispatch` of rij 13c ook start bij een issue met een al gemergede `Refs`-PR (docs/tick.md rij 13c zegt: alleen wachten bij een open PR of een PR die ná de handoff mergede). Zo ja: geen code nodig; zo nee: meld het als nieuw issue, niet hier oplossen (no-go milestone).
