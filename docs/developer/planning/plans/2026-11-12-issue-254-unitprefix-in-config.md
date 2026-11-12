# Unitprefix uit de kenn-forge-adapter naar de configuratie — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** Het prefix `paia-forge-` van de systemd-unitnaam komt uit `host.toml` (`[dispatch.forge] unit_prefix`), zonder default in de code, en de allowlist-regel voor `stop.go` verdwijnt.

**Architectuur:** `hostconfig.DispatchForge` krijgt `UnitPrefix`; `tick.go` geeft het door aan `kennforge.Adapter.UnitPrefix`; `UnitName` neemt het prefix als eerste parameter. Een leeg prefix is een fout bij `Stop`, geen stille terugval: zonder default in de code moet een host zonder de sleutel hard falen.

**Tech Stack:** Go (`/lighthouse-go-standard`), BurntSushi/toml (bestaand), `ci/literal-grep.sh`, pytest.

**Spec:** [`2026-11-07-m3-tweede-omgeving-design.md`](../specs/2026-11-07-m3-tweede-omgeving-design.md), §1. Issue #254.

## Globale randvoorwaarden

- Het prefix is een volledige tekenreeks inclusief slotstreepje (`paia-forge-`); `UnitName` plakt `prefix + profile + "-" + base`.
- Geen default voor het prefix in de code (`DefaultDispatch` laat het leeg); `ci/literal-denylist-go.txt` houdt `paia-forge-` buiten de Go-code.
- Go-namen en -commentaar in het Engels; docs en commitberichten in het Nederlands; het changelog-fragment in het Engels (`docs/releasing.md`).
- Tests volgen `/lighthouse-test-standard`: elke test faalt als het gedrag breekt (mutatiecontrole in Taak 3).
- Alleen het prefix. `podman`, `systemctl --user` en forge-targets vallen buiten dit issue.

## Review Focus

- Bestaande Beelink-`host.toml` zonder `unit_prefix`: `Stop` geeft een fout die de sleutel `dispatch.forge.unit_prefix` noemt, geen stop van een verkeerde unit (Taak 2).
- `workplace = "local"` met een `unit_prefix` in `host.toml`: geen fout, de sleutel is geldig maar wordt genegeerd (Taak 1).
- Prefix zonder slotstreepje (`foo`): de unitnaam is `foo<profile>-<base>`; het prefix is letterlijk, geen stille aanvulling (Taak 2).

---

## Bestandsstructuur

- `internal/hostconfig/hostconfig.go` — `DispatchForge.UnitPrefix`, raw-key `unit_prefix`.
- `internal/workplace/kennforge/adapter.go` — veld `UnitPrefix string`.
- `internal/workplace/kennforge/stop.go` — `UnitName(prefix, profile, worktreePath)`; `Stop` weigert een leeg prefix.
- `internal/cli/tick.go:110` — geeft `cfg.Forge.UnitPrefix` door.
- `ci/literal-allowlist.txt` — regel en commentaar (r. 11-13) weg.
- `tests/test_literal_grep.py` — docstring van `test_go_is_clean` bijwerken.
- Docs: `docs/tick.md` (§ `[dispatch.forge]`), `docs/workplace.md` (§ Stop), `docs/host-assumptions.md` (rij #254).
- `changelog.d/issue-254-unitprefix-config.md`.

### Taak 1: Sleutel `unit_prefix` in hostconfig

**Bestanden:**
- Wijzig: `internal/hostconfig/hostconfig.go` (`DispatchForge`, raw struct, toepassing bij r. ~179-188)
- Test: `internal/hostconfig/hostconfig_test.go`

**Interfaces:**
- Produceert: `hostconfig.DispatchForge.UnitPrefix string` (default `""`).

- [ ] **Stap 1: Schrijf de falende tests** in `hostconfig_test.go`: `TestLoadDispatchUnitPrefix` (TOML `[dispatch.forge]\nunit_prefix = "x-"` geeft `d.Forge.UnitPrefix == "x-"`) en `TestDefaultDispatchHasNoUnitPrefix` (`DefaultDispatch().Forge.UnitPrefix == ""`). Zet de default-vergelijking bij r. ~54 niet om; die moet ongewijzigd groen blijven.
- [ ] **Stap 2: Draai ze** — `go test ./internal/hostconfig/ -run UnitPrefix -v`; verwacht FAIL (veld bestaat niet / onbekende sleutel).
- [ ] **Stap 3: Implementeer** `UnitPrefix string` in `DispatchForge` met commentaar (kenn-forge-only, geen default) en `UnitPrefix *string \`toml:"unit_prefix"\`` in de raw `Forge`-struct; pas toe zoals `WebURL`. Geen default in `DefaultDispatch`.
- [ ] **Stap 4: Draai** `go test ./internal/hostconfig/...`; verwacht PASS.
- [ ] **Stap 5: Commit** — `feat(hostconfig): unit_prefix onder [dispatch.forge]`.

### Taak 2: Adapter en `UnitName` nemen het prefix

**Bestanden:**
- Wijzig: `internal/workplace/kennforge/adapter.go:27-28`, `stop.go:22-35,58`, `internal/cli/tick.go:110-115`
- Test: `internal/workplace/kennforge/stop_test.go`, `stop_systemd_test.go:30`, `internal/cli/tick_test.go`

**Interfaces:**
- Verbruikt: `DispatchForge.UnitPrefix` (Taak 1).
- Produceert: `func UnitName(prefix, profile, worktreePath string) string`; `Adapter.UnitPrefix string`.

- [ ] **Stap 1: Schrijf de falende tests** in `stop_test.go`:
  - `TestUnitName`: tabel krijgt kolom `prefix`; bestaande gevallen met `"paia-forge-"`, plus `{"other-", "software", "/w/issue-7-x", "other-software-issue-7-x"}` en `{"foo", "infra", "/w/a", "fooinfra-a"}`.
  - `TestStopUsesConfiguredPrefix`: fake runner verwacht `systemctl --user stop other-infra-issue-7-x` en `is-active other-infra-issue-7-x` (inactive) met `Adapter.UnitPrefix = "other-"`; `Stop` geeft nil. Dit is de test voor AC 3.
  - `TestStopWithoutUnitPrefixFails`: `UnitPrefix == ""` geeft een fout die `dispatch.forge.unit_prefix` bevat en voert géén `systemctl` uit (runner zonder verwachte calls).
  - In `tick_test.go`: test dat de gebouwde `*kennforge.Adapter` `UnitPrefix` uit `[dispatch.forge] unit_prefix` krijgt (patroon: r. ~1729, `appendHostToml`).
- [ ] **Stap 2: Draai** `go test ./internal/workplace/kennforge/ ./internal/cli/ -run 'UnitName|ConfiguredPrefix|WithoutUnitPrefix|UnitPrefix' -v`; verwacht FAIL (compilatiefout of rode assert).
- [ ] **Stap 3: Implementeer** `UnitName(prefix, profile, worktreePath)` (`return prefix + profile + "-" + string(b)`, zonder literal); `Adapter.UnitPrefix`; in `Stop`: lege `a.UnitPrefix` geeft `fmt.Errorf("stop workspace %s: dispatch.forge.unit_prefix is not set", workspaceID)` vóór elke runner-aanroep; `tick.go` zet `UnitPrefix: cfg.Forge.UnitPrefix`. Pas bestaande tests aan: `Adapter`-literal in `adapter_test.go:71` (`newFakeForge`; `stop_test.go` gebruikt die via `stopAdapter`), `stop_systemd_test.go:30` (`UnitName("lhtest-", "lhtest", dir)`) én `stop_systemd_test.go:49` (`a.UnitPrefix = "lhtest-"`, zodat `Stop` dezelfde unit stopt die `systemd-run` startte) en `contract_test.go:68` krijgen `UnitPrefix: "paia-forge-"`.
- [ ] **Stap 4: Draai** `go test ./internal/workplace/... ./internal/cli/... ./internal/hostconfig/...`; verwacht PASS.
- [ ] **Stap 5: Commit** — `feat(kennforge): unitprefix uit de configuratie, geen default in de code`.

### Taak 3: Allowlist weg, CI-check, docs, changelog

**Bestanden:**
- Wijzig: `ci/literal-allowlist.txt` (r. 11-13 weg), `tests/test_literal_grep.py:179` (docstring: "Pins that the Go code holds no allowlisted literal."), `docs/tick.md` (blok `[dispatch.forge]`: `unit_prefix = "paia-forge-"  # prefix van de systemd-unit per run; verplicht met kenn-forge, geen standaard` en een zin onder het blok dat de sleutel bij `workplace = "local"` niet wordt gelezen), `docs/workplace.md` (§ Stop, r. ~209: `<unit_prefix><profiel>-<basename>`), `docs/host-assumptions.md` (rij #254: verwijs naar de config-sleutel, werk de regelankers `stop.go#L34`/`workplace.md#L209` en de naburige `hostconfig.go#L69`-rij bij, kolom klasse/opvolger bijwerken zoals de overige opgeloste rijen), `docs/developer/planning/plans/2026-11-07-issue-250-host-aannames.md` laat je staan (historie).
- Maak: `changelog.d/issue-254-unitprefix-config.md` (Engels; front matter `type: changed`, want breaking voor bestaande kenn-forge-hosts; tekst: het unitprefix staat nu in `[dispatch.forge] unit_prefix`; hosts met kenn-forge moeten `unit_prefix = "paia-forge-"` toevoegen, anders faalt elke `Stop`).
- Test: `tests/test_literal_grep.py`, `ci/literal-grep.sh`.

- [ ] **Stap 1: Verwijder** de allowlist-regel en het commentaar; draai `uv run pytest tests/test_literal_grep.py -v` en `LITERAL_DENYLIST=ci/literal-denylist-go.txt LITERAL_SKIP_TESTS=1 ci/literal-grep.sh internal cmd` (zonder argumenten scant `ci/literal-grep.sh` alleen `pack/`); verwacht PASS en geen `stop.go`-hit.
- [ ] **Stap 2: Mutatiecontrole** — zet tijdelijk `"paia-forge-" +` terug in `UnitName`: `TestStopUsesConfiguredPrefix` en `test_go_is_clean` moeten falen. Draai ze, zie rood, herstel.
- [ ] **Stap 3: Docs en changelog** bijwerken zoals hierboven; `.claude/hooks/lighthouse/run.sh md-link-check` blokkeert kapotte links, controleer dat de edits groen blijven.
- [ ] **Stap 4: Volledige verificatie** — `go build ./... && go vet ./... && go test ./...`, `golangci-lint run`, `uv run pytest tests/test_literal_grep.py`, `LITERAL_DENYLIST=ci/literal-denylist-go.txt LITERAL_SKIP_TESTS=1 ci/literal-grep.sh internal cmd`; alles groen. `init --upgrade` is niet nodig (geen wijziging onder `pack/` of in wat `init` schrijft); controleer met `go run ./cmd/lighthouse init --upgrade` dat het `No changes.` meldt.
- [ ] **Stap 5: Commit** — `docs(kennforge): allowlist-regel weg, unit_prefix gedocumenteerd`.

## Dekking van de acceptatiecriteria

| AC | Taak |
|---|---|
| Prefix uit config, `paia-forge-` expliciet in `host.toml` van de Beelink, niet als default | 1, 2 (de Beelink-`host.toml` staat buiten de repo: de PR vermeldt dat `unit_prefix = "paia-forge-"` daar moet komen; docs/tick.md toont de waarde) |
| Allowlist-regel weg, `literal-grep.sh` groen | 3 |
| Test: ander prefix stopt de juiste unit | 2 (`TestStopUsesConfiguredPrefix`) |
