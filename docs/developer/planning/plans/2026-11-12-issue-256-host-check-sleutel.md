# host-check met een expliciete sleutel — implementatieplan (#256)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een expliciete sleutel `host.controller` in `host.toml` zegt of dit de host van de controller
is; `host-check` en `Recognize()` lezen die, en een `host.toml` zonder de sleutel behoudt de
uitkomst van nu.

**Architecture:** Eén beslisfunctie per taal (Go in `internal/config/host.go`, Python-tweeling in
`pack/core/lib/config.py`) die `host.controller` eerst leest en bij afwezigheid terugvalt op de
bestaande `tailscale_ip`-toets. `HostCheck` en `Recognize` gebruiken die ene functie, zodat ze gelijk
blijven. Docs, configuratiekaart en changelog volgen.

**Tech Stack:** Go (`internal/config`, `internal/review`), Python 3.11+ (`pack/core/lib`, pytest),
markdown/JSON-docs.

**Spec:** [2026-11-07-m3-tweede-omgeving-design.md](../specs/2026-11-07-m3-tweede-omgeving-design.md), §2
(slot: "De milestone vervangt deze toets door een expliciete sleutel").

## Global Constraints

- Sleutelnaam: `host.controller`, een TOML-boolean. Elke andere waarde is een configuratiefout (rc 2),
  met de vorm van de bestaande meldingen: `key host.controller must be true or false: <repr>`.
- Betekenis: `true` = deze machine is de host van de controller; `false` = niet; afwezig = terugval op
  de oude regel (`tailscale_ip` aanwezig én op deze machine; anders "andere host", rc 1).
- Bij `host.controller = true` wordt het adres niet gemeten: `host.toml` hoort bij deze machine.
- Een afwezige sleutel geeft exact dezelfde rc, stdout en stderr als nu (de bestaande tests en
  fixtures blijven ongewijzigd groen).
- `tailscale_ip` blijft gevalideerd zoals nu. `internal/hostconfig` niet aanraken.
- `/lighthouse-write-handoff` blijft `lighthouse config host-check` aanroepen; de skilltekst verandert
  alleen in zijn uitleg, de aanroep niet.
- Dogfood: een wijziging onder `pack/` → `go run ./cmd/lighthouse init --upgrade`, geïnstalleerde
  kopie meecommitten, tweede run geeft `No changes.`
- Artefacten Nederlands, behalve `docs/getting-started.md`, `docs/host-assumptions.md` en het
  changelog-fragment (Engels). Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`.

## Review Focus

- `host.controller = "yes"` (string) of `1` (int): rc 2, geen stille `true`.
- `host.controller = true` zonder `tailscale_ip`: rc 0 en `Recognize() == true`.
- `host.controller = false` mét lokaal `tailscale_ip`: rc 1 (de sleutel wint van het adres).
- `host.controller = true` in een host.toml met een ongeldig `tailscale_ip`: nog steeds rc 2 (validatie blijft).
- Oudere geïnstalleerde `config.py`-kopieën in andere repo's op dezelfde host: een nieuwe sleutel mag
  hun `host.toml`-lezing niet breken (controleer in Taak 1 of onbekende sleutels worden geweigerd).

---

### Task 1: Go — sleutel, validatie en beslissing

**Files:**
- Modify: `internal/config/host.go` (struct `Host`, `hostKeys`, `LoadHost`, `HostValue`, `Recognize`, `HostCheck`)
- Test: `internal/config/config_test.go` (naast `TestHostCheckNotLocal`, :247), `internal/review/record_test.go` (hostToml-helper, :38-77)
- Test: `tests/pack/fixtures/config-schema.json` (snapshot van de sleutellijst)

**Interfaces:**
- Produces: `Host.Controller *bool`; `func (h *Host) isController() (is bool, err error)` — `true` bij
  `Controller == &true`, `false` bij `&false`, anders `TailscaleIP != nil` en `IsLocal(*TailscaleIP)`.
  `HostCheck` en `Recognize` roepen alleen deze aan. `host.controller` staat in `hostKeys` en is
  optioneel (`optionalHostKeys` blijft `hostKeys[2:]`, dus de sleutel komt na `host.tailscale_ip`).

- [ ] **Step 1: Controleer onbekende sleutels.** Lees `LoadHost` en de fixtures `host-bad-in-other-key`,
  `host-key-in-bad-lighthouse-toml`: weigert een oudere lezer onbekende `host.*`-sleutels? Noteer de
  uitkomst als kopje "Compatibiliteit" in de docs-taak (Task 3). Geen codewijziging.
- [ ] **Step 2: Schrijf de falende tests** in `internal/config/config_test.go`:
  `TestHostCheckController` (tabel: `controller=true` zonder ip → rc 0 en `host: <name>`;
  `controller=true` met niet-lokaal ip → rc 0; `controller=false` met lokaal ip → rc 1, stderr noemt
  `host.controller`; sleutel afwezig + lokaal ip → rc 0; sleutel afwezig + geen ip → rc 1 met de
  bestaande tekst `has no tailscale_ip, so it is not the always-on host`; `controller="yes"` en
  `controller=1` → rc 2 met `key host.controller must be true or false`), en in
  `internal/review/record_test.go` `TestRecognizeController` met dezelfde gevallen via `Recognize()`
  (true / false / afwezig-zonder-ip). Draai `go test ./internal/config/ ./internal/review/` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `Host.Controller`, de sleutel in `hostKeys`, validatie in `LoadHost`
  (`bool`-assertion, foutmelding hierboven), de `HostValue`-tak (`"true"`/`"false"`, anders `notSet`),
  `isController()` en laat `HostCheck` en `Recognize` die gebruiken. Pas de doc-comments aan
  (`Recognize`, `HostCheck`, `Host.TailscaleIP`). Bij `controller=false` meldt `HostCheck` op stderr:
  `config: host <name> (<path>) has host.controller = false, so it is not the controller host`.
- [ ] **Step 4: Werk de schema-snapshot bij** (`tests/pack/fixtures/config-schema.json`, regel met
  `host.tailscale_ip`: voeg `host.controller` toe) en draai
  `go test ./internal/config/... ./internal/review/... ./internal/cli/...` — verwacht PASS.
- [ ] **Step 5: Commit** — `feat(config): host.controller als expliciete sleutel voor host-check en Recognize (#256)`

### Task 2: Python-tweeling

**Files:**
- Modify: `pack/core/lib/config.py` (`load_host` ~:508-534, key-lijst ~:190/249, `_host_check` :629-650, usage-tekst :19)
- Test: `tests/pack/test_config.py` (:563-583), fixtures onder `tests/pack/fixtures/config/` (`host-check-controller-true`, `-false`, `-invalid`, elk met `expect.json` in het bestaande formaat van `host-check-no-tailscale-ip`)
- Test: `tests/pack/test_config_fixtures.py` (snapshot :105-107 volgt de Go-schema-fixture)

**Interfaces:**
- Consumes: de semantiek en meldingen uit Task 1 (gelijke rc en stderr per geval).
- Produces: `Host.controller: bool | None` in de Python-dataclass.

- [ ] **Step 1: Schrijf de fixtures en tests**: dezelfde zes gevallen als Task 1, met de exacte stderr van Go.
  Draai `python3 -m pytest tests/pack/test_config.py tests/pack/test_config_fixtures.py -q` — verwacht FAIL.
- [ ] **Step 2: Implementeer** de sleutel in `load_host`, de key-lijst en `_host_check` (zelfde volgorde
  van beslissen als `isController`). Werk het gebruik in de moduledocstring bij.
- [ ] **Step 3: Draai** `python3 -m pytest tests/pack/test_config.py tests/pack/test_config_fixtures.py tests/test_site_config.py -q` — verwacht PASS
  (de site-test volgt in Task 3; zet de key daar vóór deze run in `docs/site/data/config.json` als hij faalt).
- [ ] **Step 4: Commit** — `feat(config): Python-tweeling van host.controller (#256)`

### Task 3: Skill, docs, kaart, init-voorstel, changelog

**Files:**
- Modify: `pack/core/skills/write-handoff/SKILL.md:171-172` (uitleg "always-on host" noemt `host.controller`; de aanroep `lighthouse config host-check` blijft letterlijk)
- Modify: `docs/host-assumptions.md:25,43`, `docs/test-gate.md:55,62`, `docs/getting-started.md:144` (Compatibiliteit uit Task 1 Step 1; voorbeeld `host.toml` met `controller = true` voor een host zonder Tailscale), `docs/site/data/config.json:964,975-978` (nieuwe sleutel, `yes` gelezen door `host-check`/`Recognize`)
- Modify: `tests/test_site_config.py:26` (SET-lijst krijgt `host.controller`)
- Create: `changelog.d/issue-256-host-controller.md` (Engels; noemt de upgrade-volgorde: eerst elke repo op de host `lighthouse init --upgrade`, dan pas de sleutel zetten)
- Modify: `internal/initcmd/host.go:67-92` **niet**: `init` blijft alleen met Tailscale voorstellen en schrijft de sleutel niet (AC 2 kiest voor "dezelfde uitkomst", niet voor een voorstel).

**Interfaces:**
- Consumes: `host.controller` uit Task 1 en 2.

- [ ] **Step 1: Pas de teksten en de kaart aan**, in dezelfde commit als de skill-uitleg.
- [ ] **Step 2: Dogfood**: `go run ./cmd/lighthouse init --upgrade`, commit `.claude/skills`, `.agents/skills`,
  `.lighthouse/lib`-kopieën mee; draai het nogmaals — verwacht `No changes.`
- [ ] **Step 3: Bewijs AC 3**: `python3 -m pytest tests/pack/test_skills_issue_flow.py tests/pack/test_skill_contracts.py -q`
  — PASS (de skill bevat nog `lighthouse config host-check`); `lighthouse config host-check` op deze
  (Beelink-achtige) host geeft dezelfde rc als vóór de wijziging (noteer vóór/ná in de PR).
- [ ] **Step 4: Volledige run**: `go test ./...` en `python3 -m pytest tests -q` — verwacht PASS.
- [ ] **Step 5: Commit** — `docs(config): host.controller in skill, gids, kaart en changelog (#256)`

## Self-review

- AC 1 (sleutel, `host-check` en `recognize()` lezen hem): Task 1 en 2; `recognize()` leeft nu in Go (`Recognize`), `plan_review_record.py` bestaat niet meer.
- AC 2 (bestaande host.toml zonder sleutel, zelfde uitkomst): getest in Task 1 en 2 (afwezig-gevallen); kant "init --upgrade stelt voor" bewust niet gekozen.
- AC 3 (write-handoff ongewijzigd): Task 3 Step 3.
