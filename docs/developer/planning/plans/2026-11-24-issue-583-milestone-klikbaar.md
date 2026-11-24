# Milestone klikbaar in lighthouse serve — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** Elke milestone-titel in de SPA van `lighthouse serve` is een link naar de milestone op de forge, in een nieuw tabblad.

**Architectuur:** De forge-lezers (GitHub `html_url`, GitLab `web_url`) vullen een nieuw veld `URL` op `forge.Milestone`. `internal/priorities` neemt het over in `MilestoneRef` en `Milestone` (JSON `url`), de OpenAPI en de gegenereerde client volgen, de SPA rendert `<a href={url} target="_blank" rel="noreferrer">` en laat de link weg bij een lege `url` — zoals de issuenummers (#437) al doen.

**Tech Stack:** Go (`internal/forge`, `internal/priorities`, `internal/server`), React/TypeScript met vitest (`web/`).

**Spec:** geen aparte spec; de acceptatiecriteria van issue #583 zijn de spec:
1. Overal waar de SPA een milestone-titel toont is die een link naar de forge, in een nieuw tabblad.
2. De URL komt van de forge-lezer, niet uit een in de SPA gebouwde URL (werkt ook op GitLab).
3. Het blok zonder milestone krijgt geen link.

## Globale randvoorwaarden

- Artefacten (commits, changelog, docs) Nederlands; code, namen en commentaar Engels (`code_language`).
- Atomaire Conventional Commits; nooit `--no-verify`.
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command`.
- Elke PR krijgt een fragment in `changelog.d/` (zie Taak 4).
- Nieuwe velden staan na de bestaande velden van de structs; `forge.Milestone`-literals zijn overal keyed, dus veilig.
- De SPA maakt zelf nooit een forge-URL; ze gebruikt alleen `url` uit de API.

## Review Focus

- Milestone zonder `url` (lege string, bv. een forge die hem niet geeft): titel blijft platte tekst, geen `<a href="">`.
- Gesloten milestone in `UnassignedView` (“<titel> · closed”): ook die titel is een link.
- GitLab-groepsmilestone: `web_url` kan van een groep komen; de lezer neemt over wat de forge geeft, zonder zelf te bouwen.
- Issue met milestone dat niet in `repo.milestones` staat (gesloten): de URL moet uit `MilestoneRef` komen, niet uit een lookup.
- Verouderde `web/openapi.json` of gegenereerde client: de snapshot-test en de CI-`npm run gen` moeten rood worden.

## Bestandsstructuur

| Bestand | Verandering |
|---|---|
| `internal/forge/issues.go` | `Milestone.URL`; embedded milestone van de issue-lijst leest `html_url` |
| `internal/forge/write_issue.go` | `ghMilestone.URL` (`html_url`); conversie `Milestone(m)` blijft veld-voor-veld gelijk |
| `internal/forge/gitlab_write.go` | `glMilestone.WebURL` (`web_url`) in `milestone()` |
| `internal/forge/gitlab_planning.go` | `glPlanMilestone.WebURL`; beide handmatige `Milestone{…}`-literals nemen hem over |
| `internal/forge/planning.go` | `Milestone`-literal op :129 neemt `URL` over |
| `internal/forge/forgetest/server.go`, `internal/forge/gitlabtest/server_write.go` | fake-JSON met `html_url` / `web_url` |
| `internal/forge/forgesuite/suite.go` | gedeelde suite toetst `URL` voor beide forges |
| `internal/priorities/priorities.go` | `MilestoneRef.URL`, `Milestone.URL` (JSON `url`) |
| `web/openapi.json`, `web/src/api/*` | regenereren |
| `web/src/lib/priorities/derive.ts` | `MilestoneRow.url`, `UnassignedRow.closedMilestone` krijgt titel + url |
| `web/src/components/priorities/{MilestonesView,Toolbar,PrioritiesTab,UnassignedView}.tsx` | links |
| `web/e2e/fake-github.ts` | milestone met `html_url` |
| `docs/developer/areas/server.md` | één zin over de klikbare milestone |
| `changelog.d/issue-583-milestone-klikbaar.md` | fragment |

---

### Taak 1: Forge-lezers leveren `Milestone.URL`

**Bestanden:**
- Wijzig: `internal/forge/issues.go:13-19,49-54,73-75`, `internal/forge/write_issue.go:143-155`, `internal/forge/gitlab_write.go:19-35`, `internal/forge/gitlab_planning.go:28-42,87`, `internal/forge/planning.go:111-133`
- Wijzig (fakes): `internal/forge/forgetest/server.go:80-82`, `internal/forge/gitlabtest/server_write.go`
- Test: `internal/forge/forgesuite/suite.go` (gedeeld; draait op beide fakes), plus de bestaande `_test.go` van `internal/forge` die `Milestone` vergelijken

**Interfaces:**
- Produceert: `forge.Milestone.URL string` — de web-URL van de milestone op de forge; leeg als de forge hem niet geeft. Gevuld voor `Milestones(ctx, state)`, voor `Issue.Milestone`, voor `Planning` (open milestones en de milestone van elke issue) en voor `PrioInputs.Milestones`.

- [ ] **Stap 1: Schrijf de falende tests.** In `forgesuite/suite.go`: breid de fake-milestones uit met een URL (`https://forge.example/m/<n>`) en voeg `TestMilestoneURL` toe (naam in de suite-stijl) die controleert dat `URL` gevuld is in (a) `Milestones`, (b) de `Milestone` van een issue uit `Issues`, (c) `Planning.Milestones` en de milestone van een `PlanIssue`. Voeg een geval toe met een milestone zonder URL in de fake-JSON: verwacht `URL == ""` en geen fout.
- [ ] **Stap 2: Draai ze rood.** `go test ./internal/forge/... -run MilestoneURL` — verwacht FAIL (veld bestaat niet / lege URL).
- [ ] **Stap 3: Implementeer.** Voeg `URL string` toe aan `forge.Milestone` en `HTMLURL string \`json:"html_url"\`` aan `ghMilestone` op dezelfde positie, zodat `Milestone(m)` blijft compileren (veldnamen en -volgorde identiek; `OpenIssues`/`ClosedIssues` blijven daarna). Voeg `html_url` toe aan de anonieme milestone-struct van `issueItem` en kopieer hem in `issue()`. GitLab: `WebURL string \`json:"web_url"\`` op `glMilestone` en `glPlanMilestone`, overnemen in `milestone()` en in de twee handmatige literals (`gitlab_planning.go:32,87`); `planning.go:129` neemt `URL` mee. Bouw nergens zelf een URL; leeg blijft leeg.
- [ ] **Stap 4: Draai groen.** `go test ./internal/forge/...` — verwacht PASS.
- [ ] **Stap 5: Commit.** `git add internal/forge && git commit -m "feat(forge): milestone-URL van de forge-lezers (#583)"`

### Taak 2: URL door `internal/priorities` en de API

**Bestanden:**
- Wijzig: `internal/priorities/priorities.go:23-28,66-75,124-126,137`
- Test: `internal/priorities/priorities_test.go:57`, `internal/server/priorities_test.go:28`, `internal/server/openapi_snapshot_test.go`
- Regenereer: `web/openapi.json`, `web/src/api/*`

**Interfaces:**
- Consumeert: `forge.Milestone.URL`, en de URL op de milestone in `forge.Planning` (Taak 1).
- Produceert: JSON-veld `url` op `MilestoneRef` en `Milestone` van `/api/...priorities`; TypeScript `MilestoneRef.url: string` en `Milestone.url: string` (verplicht, mag leeg zijn).

- [ ] **Stap 1: Schrijf de falende tests.** Pas `priorities_test.go:57` aan naar `MilestoneRef{Number: 7, Title: "M", State: "open", URL: "<fake-url>"}` en voeg een assertie toe dat `Repo.Milestones[i].URL` gevuld is; idem in `internal/server/priorities_test.go:28`. Een issue met een milestone zonder URL geeft `URL == ""`.
- [ ] **Stap 2: Draai rood.** `go test ./internal/priorities/... ./internal/server/...` — verwacht FAIL.
- [ ] **Stap 3: Implementeer.** `URL string \`json:"url"\`` op beide structs; `Build` zet hem uit `pi.Milestone.URL` (`priorities.go:125`) en `m.URL` (`:137`). Controleer eerst hoe `PlanIssue.Milestone` zijn type heeft (`forge.Milestone`) zodat de URL daar al in zit.
- [ ] **Stap 4: Regenereer.** `go test ./internal/server -run TestOpenAPISnapshot -update`, dan `cd web && npm run gen`. Verwacht: `MilestoneRef` en `Milestone` hebben `url` in `web/openapi.json` en `web/src/api`.
- [ ] **Stap 5: Draai groen.** `go test ./internal/forge/... ./internal/priorities/... ./internal/server/...` — verwacht PASS.
- [ ] **Stap 6: Commit.** `git add internal web && git commit -m "feat(priorities): milestone-URL in de API en de gegenereerde client (#583)"`

### Taak 3: SPA toont milestone-titels als link

**Bestanden:**
- Wijzig: `web/src/lib/priorities/derive.ts:293-297,380,476`, `web/src/components/priorities/MilestonesView.tsx:111`, `Toolbar.tsx:148-158`, `PrioritiesTab.tsx:56-59,81`, `UnassignedView.tsx:29`
- Test: `MilestonesView.test.tsx`, `PrioritiesTab.test.tsx`, `UnassignedView.test.tsx`, `web/src/lib/priorities/derive.test.ts`
- Wijzig: `web/e2e/fake-github.ts` (milestone met `html_url`), `web/e2e` mits een bestaande spec de titel raakt

**Interfaces:**
- Consumeert: `MilestoneRef.url` en `Milestone.url` (Taak 2).
- Produceert: een gedeeld component `MilestoneTitle({ title, url })` in `web/src/components/priorities/MilestoneTitle.tsx` dat `<a href={url} target="_blank" rel="noreferrer">{title}</a>` rendert, en bij `url === ''` alleen de titel als tekst. `MilestoneRow.url: string`; `UnassignedRow.closedMilestone: { title: string; url: string } | null`; de legenda-items van de Toolbar dragen `url`.

- [ ] **Stap 1: Schrijf de falende tests.** (a) `MilestoneTitle.test.tsx`: met url → `getByRole('link', {name: 'M'})` heeft `href` en `target="_blank"` en `rel` met `noreferrer`; zonder url → geen link (`queryByRole('link', {name: 'M'})` is `null`). (b) `MilestonesView.test.tsx`: de rijtitel is een link naar de `url` uit de fixture. (c) `PrioritiesTab.test.tsx`: de legenda-label is een link. (d) `UnassignedView.test.tsx`: een gesloten milestone toont “<titel> · closed” met de titel als link; een issue zonder milestone (het blok zonder milestone) toont geen milestone-link. (e) `derive.test.ts`: `MilestoneRow.url` en `closedMilestone.url` komen uit de `MilestoneRef` van het issue, niet uit een lookup.
- [ ] **Stap 2: Draai rood.** `cd web && npm test -- MilestoneTitle MilestonesView PrioritiesTab UnassignedView derive` — verwacht FAIL.
- [ ] **Stap 3: Implementeer.** Maak `MilestoneTitle`; gebruik het in de drie plekken; breid `derive.ts` uit zoals bij Interfaces. Hergebruik de link-opmaak van `Block.tsx:96-97`. Geen URL samenstellen in de SPA.
- [ ] **Stap 4: Verifieer.** `cd web && npm run lint && npm run typecheck && npm test && npm run gen` en `git status --short web/src/api` is leeg (CI-controle, `ci.yml:325-326`). Verwacht: alles groen. Draai `npm run e2e` als Playwright beschikbaar is; zo niet, meld dat uitdrukkelijk.
- [ ] **Stap 5: Commit.** `git add web && git commit -m "feat(web): milestone-titel klikbaar naar de forge (#583)"`

### Taak 4: Documentatie, changelog en eindcontrole

**Bestanden:**
- Wijzig: `docs/developer/areas/server.md` (sectie over milestones, regels ~31-36)
- Maak: `changelog.d/issue-583-milestone-klikbaar.md`

- [ ] **Stap 1: Docs.** Voeg bij de milestone-beschrijving één zin toe: de milestone-titel linkt naar de forge-URL die de forge-lezer levert (`forge.Milestone.URL`), zonder link bij een lege URL.
- [ ] **Stap 2: Changelog-fragment** met frontmatter `type: added` en een Nederlandse zin: milestone-titels in `lighthouse serve` zijn klikbaar naar de forge, in een nieuw tabblad (#583).
- [ ] **Stap 3: Eindcontrole.** `[test_gate] command` uit `lighthouse.toml`, `lighthouse imports check` en `go test ./...`; verwacht groen. Dogfood (`init --upgrade`) is niet nodig: er verandert niets onder `pack/` of in de `init`-uitvoer; bevestig dat met `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Stap 4: Commit.** `git add docs changelog.d && git commit -m "docs(server): klikbare milestone en changelog-fragment (#583)"`

## Zelfreview

- Spec-dekking: AC1 → Taak 3 (alle drie de plekken: rij, legenda, gesloten-regel); AC2 → Taak 1 en 2 (URL van de lezer, SPA bouwt niets); AC3 → Taak 3 (`UnassignedView`-test zonder milestone, `MilestoneTitle` zonder url).
- Typeconsistentie: `URL`/`url` in Go en JSON, `MilestoneTitle`, `closedMilestone` identiek in Taak 3.
- Onbekend, door de uitvoerder in Taak 1 te controleren: of GitHubs issue-lijst `html_url` in de ingebedde milestone meegeeft (de milestone-lijst doet dat zeker) en wat GitLab als `web_url` van een groepsmilestone geeft. Staat hij niet in de issue-lijst, dan haalt `Planning` hem uit de milestone-lijst op nummer; alleen als dat ook niet kan, blijft de URL daar leeg en geldt het lege-URL-gedrag.
