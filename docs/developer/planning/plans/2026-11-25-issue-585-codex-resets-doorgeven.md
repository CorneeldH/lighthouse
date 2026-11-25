# Codex-resets doorgeven aan API, usage-paneel en budgetbeslissing — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** de Codex-`reset_credits` die `lighthouse usage codex` al meet, doorgeven aan `GET /api/usage`, het usage-paneel en `lighthouse codex-usage budget`, zonder ooit een reset in te zetten.

**Architectuur:** een nieuw type `usage.ResetCredits` (`available`, `expires`) met één parser vanuit `CodexState.ResetCredits` (`json.RawMessage`); `nil` betekent `null`. Het API-antwoord krijgt een Codex-specifiek brontype (`CodexReport`), zodat Claude het veld niet krijgt. `BudgetOutcome` krijgt hetzelfde veld en een aangevulde `reason` bij `fallback`; de beslissing blijft gelijk. De UI toont het veld in een eigen kleine component onder de Codex-vensters.

**Tech Stack:** Go (huma, OpenAPI-snapshot), React/TypeScript (orval-client, vitest).

**Spec:** geen aparte spec; het issue [#585](https://github.com/CorneeldH/lighthouse/issues/585) met zijn acceptatiecriteria is de spec.

## Globale randvoorwaarden

- `null` is nooit `0`: onbekend (`rollout-log`, onleesbare credits, nil lezing, `not_measurable` zonder lezing) geeft JSON `null`; `{"available":0,"expires":[]}` geeft `available: 0` en `expires: []` (nooit `null`).
- `expires`: UTC-tijden in `timeLayout`, oudste eerst (zoals `resetCredits()` in `internal/usage/codex.go` ze al aanlevert).
- De app-server krijgt alleen `initialize`, `initialized` en `account/rateLimits/read`. `resetCredits()` en `DecideBudget` (drempels, volgorde, exitcode 6) veranderen niet.
- Claude (`SourceReport.Claude`) krijgt het veld niet.
- `web/openapi.json` en `web/src/api/` nooit met de hand; genereren (zie taak 2).
- Code, commentaar en changelog-fragment in het Engels; docs, commits en PR in het Nederlands.
- Preflight vóór de eerste taak: `cd web && npm ci` (in een verse worktree bestaat `web/node_modules` niet; zonder faalt `npm run gen` met exit 127). Gebruik Node uit `web/.nvmrc` als dat beschikbaar is.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `go run ./cmd/lighthouse imports check` (de `lighthouse` op PATH geeft hier `COULD NOT CHECK`). Nooit `--no-verify`.
- Huma v2.39.1 paniekt op `nullable:"true"` op een objectveld (`schema.go:622`) en maakt `[]string` standaard nullable. Een nullable object krijg je met een struct-brede override: `_ struct{} \`nullable:"true"\`` in `ResetCredits` (`schema.go:979`); `Expires` krijgt `nullable:"false"`.

## Review Focus

- `ResetCredits` is `nil` bij `rollout-log` en bij onleesbare credits: JSON `null`, niet `{"available":0}` (taak 1 en 2).
- Het literal `null` in `CodexState.ResetCredits` (naast een nil `RawMessage`) geeft ook `null` (taak 1).
- `available: 0` met lege `expires` toont in de UI niets over resets of een neutrale tekst, nooit een vervaldatum (taak 4).
- Een `fallback` met `available: 0` of `null` laat `reason` ongewijzigd (taak 3).
- Een `nil`-lezing (`not_measurable` zonder lezing) heeft `reset_credits: null` in de budget-JSON; een `not_measurable` mét lezing (verlopen log) toont de resets van die lezing (taak 3).

---

### Taak 1: Type `ResetCredits` en parser

**Bestanden:**
- Wijzig: `internal/usage/codex.go` (of nieuw `internal/usage/resetcredits.go` naast `resetCredits()`)
- Test: `internal/usage/codex_test.go`

**Interfaces:**
- Produceert: `type ResetCredits struct { Available int \`json:"available"\`; Expires []string \`json:"expires" nullable:"false"\`; _ struct{} \`nullable:"true"\` }` (de `_`-regel maakt het type als geheel nullable in het schema; zie de randvoorwaarden) en `func (s CodexState) Credits() *ResetCredits`. Nil bij `nil`/leeg/`null`/onleesbaar `ResetCredits`; anders de geparste waarde met `Expires` nooit nil (lege slice).

- [ ] **Stap 1: faalende test** `TestCodexStateCredits` (table-driven) met: `nil` → `nil`; `json.RawMessage("null")` → `nil`; `{"available":0,"expires":[]}` → `&ResetCredits{Available: 0, Expires: []string{}}` (altijd met sleutels: de `_`-regel maakt een positionele literal ongeldig) (let op: `Expires` is een lege, niet-nil slice); `{"available":2,"expires":["2026-12-01T10:00:00Z","2026-12-09T10:00:00Z"]}` → die twee, in volgorde; `{"available":"x"}` → `nil`.
- [ ] **Stap 2:** `go test ./internal/usage -run TestCodexStateCredits` faalt (methode ontbreekt).
- [ ] **Stap 3:** implementeer `ResetCredits` en `(CodexState).Credits()` met `json.Unmarshal`; een `Expires` van `nil` na parsen wordt `[]string{}`.
- [ ] **Stap 4:** test slaagt.
- [ ] **Stap 5:** commit `feat(usage): ResetCredits-type voor de Codex-resets (#585)`.

### Taak 2: `GET /api/usage` met `codex.reset_credits`

**Bestanden:**
- Wijzig: `internal/usage/report.go` (type `CodexReport`, `Report.Codex`, `codexReport`), `internal/server/server.go` alleen als het type daar genoemd wordt
- Test: `internal/usage/report_test.go` (`TestReportCodex`, regel ~107), `internal/server/server_test.go`
- Gegenereerd: `web/openapi.json`, `web/src/api/lighthouse.ts`

**Interfaces:**
- Verbruikt: `CodexState.Credits()` (taak 1).
- Produceert: `type CodexReport struct { Session WindowReport \`json:"session"\`; Week WindowReport \`json:"week"\`; ResetCredits *ResetCredits \`json:"reset_credits"\` }`; `Report.Codex` is `CodexReport`. `notMeasurable`-paden en een leesfout geven `ResetCredits: nil`.

- [ ] **Stap 1: faalende tests** in `TestReportCodex`: (a) lezing met `available: 2` en twee vervaldata → `Codex.ResetCredits` gelijk aan die waarde; (b) `available: 0` → `&ResetCredits{Available: 0, Expires: []string{}}` (altijd met sleutels: de `_`-regel maakt een positionele literal ongeldig); (c) `rollout-log`-stand en `ReadCodex` die een fout geeft → `nil`; (d) JSON-marshal van het rapport bevat `"reset_credits":null` in (c) en `"available":0` in (b); (e) `Claude` heeft geen `reset_credits`-sleutel in de JSON.
- [ ] **Stap 2:** tests falen (type ontbreekt).
- [ ] **Stap 3:** implementeer `CodexReport` en vul het in `codexReport`; houd een helper voor de gedeelde vensters zodat `windowReport` niet verdubbelt. Pas in dezelfde stap `fakeReport` in `internal/server/server_test.go:45` aan (`Codex: usage.CodexReport{...}`), anders compileert de servertest niet.
- [ ] **Stap 4:** `go test ./internal/usage/... ./internal/server/...`; `TestOpenAPISnapshot` faalt nu op drift.
- [ ] **Stap 5:** `go test ./internal/server -run TestOpenAPISnapshot -update`, daarna `cd web && npm run gen`. Controleer met `git diff --stat` dat alleen `openapi.json` en `src/api/` veranderen en dat het veld in beide staat: in `openapi.json` is `components.schemas.ResetCredits.type` gelijk aan `["object","null"]`, is `reset_credits` een `$ref` naar dat schema (het veld zelf heeft geen `type`) en is `expires.type` `"array"` zonder `null`; in `lighthouse.ts` accepteert het type van `data.codex.reset_credits` `null` (de exacte vorm, `ResetCredits | null` of een alias, volgt uit orval; controleer dat `null` toegewezen kan worden in `npm run typecheck`) en is `expires` `string[]`. Zet **nooit** `nullable:"true"` op het veld zelf: dat paniekt (`schema.go:622`); is het schema niet zoals hier beschreven, stop dan en meld het in plaats van tags te raden.
- [ ] **Stap 6:** voeg een testgeval aan `internal/server/server_test.go` toe dat het `reset_credits`-veld in het HTTP-antwoord controleert (waarde en `null`).
- [ ] **Stap 7:** commit `feat(usage): GET /api/usage geeft de Codex reset_credits (#585)`.

### Taak 3: `codex-usage budget` met `reset_credits`

**Bestanden:**
- Wijzig: `internal/usage/codex_budget.go` (`BudgetOutcome`, `outcome`-closure, fallback-reason), eventueel hulptekst in `internal/cli/codexusage.go:30`
- Test: `internal/usage/codex_budget_test.go` (o.a. `TestBudgetOutcomeJSONKeyOrder` regel ~155)

**Interfaces:**
- Verbruikt: `CodexState.Credits()`.
- Produceert: `BudgetOutcome.ResetCredits *ResetCredits \`json:"reset_credits"\`` als **laatste** veld (na `thresholds`, zodat de bestaande sleutelvolgorde intact blijft). `fallback` met `Credits().Available > 0` krijgt `reason = "<bestaande reden>; reset available (<N>, first expires <eerste expires>)"` (Engelse tekst, zoals de bestaande reasons; `expires` kan leeg zijn bij `available > 0`: laat dan het deel "first expires" weg).

- [ ] **Stap 1: faalende tests:** (a) `fallback` met `available: 2` en vervaldata: `ResetCredits` gevuld en `Reason` bevat `reset available`; (b) `fallback` met `available: 0` en met `nil`: `Reason` exact als vóór de wijziging; (c) expliciete tabel: `r == nil` (`not_measurable`) → `ResetCredits` nil (JSON `null`); `codex`-beslissing → gevuld volgens de lezing; `not_measurable` uit de `outcome`-closure (verlopen log, lezing bestaat) → gevuld volgens de lezing (onleesbare credits → nil); (d) `Decision`, `Window` en `ResetAt` zijn in alle gevallen gelijk aan de uitkomst zonder credits; (e) bijgewerkte exacte JSON in `TestBudgetOutcomeJSONKeyOrder` met `"reset_credits"` als laatste sleutel.
- [ ] **Stap 2:** tests falen.
- [ ] **Stap 3:** implementeer veld, vulling in de `outcome`-closure en de reden bij `fallback`. Geen wijziging in `DecideBudget`-tak- of drempellogica. Controleer dat `internal/roborev/gate.go` en `internal/review/runner.go` het extra veld negeren (draai hun tests).
- [ ] **Stap 4:** `go test ./internal/usage/... ./internal/roborev/... ./internal/review/... ./internal/cli/...` slaagt.
- [ ] **Stap 5:** commit `feat(codex-usage): budget meldt een beschikbare reset (#585)`.

### Taak 4: Usage-paneel toont de resets

**Bestanden:**
- Maak: `web/src/components/ResetCredits.tsx`, `web/src/components/ResetCredits.test.tsx`, `web/src/components/UsagePanel.test.tsx`
- Wijzig: `web/src/components/UsagePanel.tsx`

**Interfaces:**
- Verbruikt: gegenereerd type `ResetCredits | null` op `data.codex.reset_credits` (naam volgt uit orval; controleer in `web/src/api/lighthouse.ts`); `expires` is `string[]` (niet-nullable, zie randvoorwaarden); een nieuwe `formatDate(iso)` in `@/lib/format` (met test in `format.test.ts` indien aanwezig) levert dag en maand (bv. `Dec 9`), want `formatReset` toont alleen een tijd of weekdag+tijd en is voor een vervaldatum weken ver weg misleidend.
- Produceert: `export function ResetCredits({ credits, now }: { credits: ResetCreditsType | null; now: Date }): JSX.Element | null`.

Gedrag: `null` → `null` renderen (niets; geen "0 resets"). `available > 0` → tekst `<N> reset(s) available` plus `first expires <formatDate(expires[0])>` als `expires[0]` bestaat. `available === 0` → niets renderen (er is geen reset; de balk voor het venster zegt al genoeg) — de beslissing is vastgelegd in de test.

- [ ] **Stap 1: faalende tests** in `ResetCredits.test.tsx`: `null` rendert niets; `{available: 0, expires: []}` rendert niets en nergens staat "0"; `{available: 2, expires: [t1, t2]}` toont "2" en de dag en maand van `t1` (niet `t2`, niet alleen een tijd of weekdag); `{available: 1, expires: []}` toont "1" zonder "expires". In `UsagePanel.test.tsx`: met een mocked `useUsage` (patroon van `App.test.tsx` / `lib/queries.test.tsx`) staat het resetblok onder Codex en niet onder Claude; bij `reset_credits: null` ontbreekt het.
- [ ] **Stap 2:** `cd web && npm test` faalt.
- [ ] **Stap 3:** implementeer de component en plaats hem in `UsagePanel` onder de twee Codex-vensters.
- [ ] **Stap 4:** `cd web && npm run lint && npm run typecheck && npm test` slaagt.
- [ ] **Stap 5:** commit `feat(web): usage-paneel toont de beschikbare Codex-resets (#585)`.

### Taak 5: Geen reset-aanroep vastleggen, docs, changelog

**Bestanden:**
- Wijzig: `docs/usage-venster.md` (budget-sleutellijst ~regel 138-142; API-sectie ~regel 145+)
- Maak: `changelog.d/issue-585-codex-resets.md`
- Test: `internal/usage/codex_test.go`

- [ ] **Stap 1:** zoek of er al een test is die de app-server-methodes vastzet (`grep -n "rateLimits/read\|initialized" internal/usage/*_test.go`). Zo niet: voeg `TestAppServerOnlyCallsReadMethods` toe die de verzonden JSON-RPC-methodes van de bestaande fake app-server-aanpak verzamelt en gelijk stelt aan `initialize`, `initialized`, `account/rateLimits/read`.
- [ ] **Stap 2:** docs: voeg `reset_credits` toe aan de API-vorm (`codex.reset_credits`: `{"available": N, "expires": [...]}` of `null`, nooit `0` bij onbekend; Claude heeft het veld niet) en aan de `codex-usage budget`-sleutellijst (laatste sleutel; bij `fallback` met `available > 0` noemt `reason` de reset; de beslissing verandert niet; Lighthouse zet een reset nooit zelf in).
- [ ] **Stap 3:** changelog-fragment `type: added`, één Engelse regel eindigend op `(#585)`, in het formaat van `changelog.d/issue-556-tab-backlog.md`.
- [ ] **Stap 4:** `go run ./cmd/lighthouse init --upgrade` geeft `No changes.`; volledige `go test ./...`, `go vet ./...`, `go run ./cmd/lighthouse imports check`; `cd web && npm run gen && git status --porcelain -- src/api` is leeg.
- [ ] **Stap 5:** commit `docs(usage): reset_credits in API en budget, changelogfragment (#585)`.
