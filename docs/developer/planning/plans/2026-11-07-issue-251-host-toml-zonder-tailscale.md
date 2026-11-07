# Hostprofiel: host.toml zonder Tailscale — implementatieplan (#251)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `host.tailscale_ip` wordt optioneel, zodat een host zonder Tailscale een met de hand
geschreven `host.toml` (alleen voor `gate_*` of `[usage]`) kan hebben zonder dat iets breekt.

**Architecture:** `load_host` leest `tailscale_ip` zoals nu `gate_slice`: afwezig is `None`,
aanwezig wordt gevalideerd zoals nu. De twee lezers die de host herkennen (`host-check` en
`plan_review_record.recognize()`) behandelen `None` als "andere host", zonder `is_local` aan te
roepen. `init` blijft alleen met Tailscale een `host.toml` voorstellen; alleen de waarschuwing
verandert. Docs, configuratiekaart en changelog volgen.

**Tech Stack:** Python 3.11+ (`pack/core/lib`, `pack/core/scripts`, pytest via `uv run`), Go
(`internal/initcmd`), markdown/JSON-docs.

**Spec:** [2026-11-07-m3-tweede-omgeving-design.md](../specs/2026-11-07-m3-tweede-omgeving-design.md), §2.

## Global Constraints

- `host-check` blijft betekenen: "dit is de host waar de controller altijd aan staat". Exit 1
  zonder `tailscale_ip`, net als `plan_review_record` (`None`).
- `init` stelt geen `host.toml` met alleen `name` voor. `host.name` blijft `UNREAD`.
- `internal/hostconfig` leest de sleutel niet; niet aanraken.
- Een aanwezige `tailscale_ip` wordt gevalideerd zoals nu (IPv4, geen unspecified/loopback/multicast).
- Instructies Engels; artefacten (docs onder `docs/`, commits, PR) Nederlands, behalve
  `docs/getting-started.md`, `docs/site/agents/install.md` en het changelog-fragment: die zijn Engels.
- Dogfood: een wijziging onder `pack/` → `go run ./cmd/lighthouse init --upgrade` en de
  geïnstalleerde kopie meecommitten; daarna geeft een tweede run `No changes.`
- Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`.
- Nooit `--no-verify`.

## Review Focus

1. **`host.toml` zonder `tailscale_ip`, `config.py host.tailscale_ip`**: rc 1 ("niet gezet"), geen
   rc 2 en geen traceback. Test in taak 1.
2. **`tailscale_ip = ""` of een ander ongeldig type blijft een fout** (rc 2): "optioneel" betekent
   "mag ontbreken", niet "mag leeg". Bestaande `test_load_host_rejects_invalid_ip` dekt `""`; blijft staan.
3. **`plan_review_record.host_refusal` zonder sleutel**: de melding is `not on the host from
   host.toml`, geen `HostError`-tekst en geen traceback. Test in taak 2.
4. **De commit-test-gate met een `host.toml` met alleen `name` en `gate_slots`** leest `gate_slots`
   gewoon. Test in taak 1 (`host.gate_slots` via `main`).
5. **Geen `host.toml` op de host**: `host-check` blijft rc 2 (`host unknown: no host.toml`), en
   `write-handoff` behandelt elke exit ≠ 0 als "elders". Niet veranderen; taak 6 toont het.

---

### Taak 1: `load_host` en `host-check` zonder `tailscale_ip`

**Files:**
- Modify: `pack/core/lib/config.py:235-245` (`Host`), `:482-494` (`load_host`), `:604-620` (`_host_check`)
- Test: `tests/pack/test_config.py:411-452`, `:542-561`

**Interfaces:**
- Produces: `Host.tailscale_ip: str | None = None` (het veld blijft op plaats 2, zodat
  `Host("beelink", "100.76.126.126")` geldig blijft); `load_host` zet `None` als de sleutel ontbreekt.

- [ ] **Stap 1: Pas de tests aan en voeg toe**

In `test_load_host_missing_key`: haal de case `('[host]\nname = "box"\n', "missing key host.tailscale_ip")`
en id `tailscale_ip` weg. Nieuw:

```python
def test_load_host_without_tailscale_ip(tmp_path):
    host = load_host(_host(tmp_path, '[host]\nname = "box"\ngate_slots = 2\n'))
    assert (host.name, host.tailscale_ip, host.gate_slots) == ("box", None, 2)


def test_cli_tailscale_ip_absent_is_rc_1(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(_host(tmp_path, '[host]\nname = "box"\ngate_slots = 2\n')))
    assert main(["host.tailscale_ip"]) == 1
    assert main(["host.gate_slots"]) == 0
    assert capsys.readouterr().out == "2\n"


def test_host_check_without_tailscale_ip_is_rc_1(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(_host(tmp_path, '[host]\nname = "box"\n')))
    with patch.object(cfgmod, "is_local", side_effect=AssertionError("is_local called")):
        assert main(["host-check"]) == 1
    err = capsys.readouterr().err
    assert "no tailscale_ip" in err
    assert "Traceback" not in err
```

- [ ] **Stap 2: Draai, verwacht rood**

Run: `uv run pytest -q tests/pack/test_config.py -k "tailscale_ip or host_check"`
Expected: FAIL — `ConfigError: … missing key host.tailscale_ip`.

- [ ] **Stap 3: Implementeer**

- `Host.tailscale_ip: str | None = None`.
- `load_host`: `tailscale_ip = host.get("tailscale_ip")`; valideer alleen als het niet `None` is,
  met de bestaande check en foutmelding.
- `_host_check`: na `load_host`, als `host.tailscale_ip is None`: print naar stderr
  `config: host {host.name} ({path}) has no tailscale_ip, so it is not the always-on host` en
  return 1, zonder `is_local`. Anders ongewijzigd.
- `_host_value` en `HOST_KEYS` blijven: een `None`-waarde geeft al `KeyError` → rc 1.

- [ ] **Stap 4: Draai, verwacht groen**

Run: `uv run pytest -q tests/pack/test_config.py tests/test_checkpoint_scan.py`
Expected: PASS.

- [ ] **Stap 5: Commit**

```bash
git add pack/core/lib/config.py tests/pack/test_config.py
git commit -m "feat(config): host.tailscale_ip is optioneel in host.toml (#251)"
```

### Taak 2: `recognize()` zonder `tailscale_ip`

**Files:**
- Modify: `pack/core/scripts/plan_review_record.py:142-148`
- Test: `tests/pack/test_plan_review_record.py` (na `test_host_refusal_recognized`, ±l.357)

**Interfaces:**
- Consumes: `Host.tailscale_ip: str | None` uit taak 1.

- [ ] **Stap 1: Schrijf de tests**

```python
def test_recognize_without_tailscale_ip(tmp_path, monkeypatch):
    path = tmp_path / "host.toml"
    path.write_text('[host]\nname = "server"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(path))
    monkeypatch.setattr(rec, "is_local", lambda ip: pytest.fail("is_local called"))
    assert rec.recognize() is None


def test_host_refusal_without_tailscale_ip(tmp_path, monkeypatch):
    path = tmp_path / "host.toml"
    path.write_text('[host]\nname = "server"\n', encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(path))
    text = REAL_HOST_REFUSAL("spec")
    assert text is not None
    assert "not on the host from host.toml" in text
```

`rec.is_local` is een module-attribuut: het script importeert `is_local` uit `lighthouse_lib.config` (l.94-99).

- [ ] **Stap 2: Draai, verwacht rood**

Run: `uv run pytest -q tests/pack/test_plan_review_record.py -k tailscale_ip`
Expected: FAIL (`is_local called`, of `TypeError` op `None`).

- [ ] **Stap 3: Implementeer** — `recognize()` geeft `None` als `host.tailscale_ip is None`, zonder
  `is_local`; de docstring zegt het.

- [ ] **Stap 4: Draai, verwacht groen**

Run: `uv run pytest -q tests/pack/test_plan_review_record.py`
Expected: PASS.

- [ ] **Stap 5: Commit** — `fix(plan-review-record): host zonder tailscale_ip is niet deze host (#251)`.

### Taak 3: De waarschuwing van `init`

**Files:**
- Modify: `internal/initcmd/host.go:84`
- Test: `internal/initcmd/initcmd_test.go:1108-1118` (`TestInitSkipsHostTOMLWithoutTailscale`)

- [ ] **Stap 1: Pas de test aan** — de verwachte tekst wordt exact:

```text
host.toml not proposed: tailscale ip unavailable; lighthouse works without host.toml. For the gate_* keys (memory brake, slots) write it by hand: see "A host.toml without Tailscale" in docs/getting-started.md
```

`TestInitRejectsBadTailscaleIP` checkt alleen `host.toml not proposed` en blijft.

- [ ] **Stap 2: Draai, verwacht rood** — `go test ./internal/initcmd/ -run HostTOML` → FAIL.
- [ ] **Stap 3: Implementeer** — vervang de tekst op `host.go:84`. De meldingen op l.71 en l.89
  blijven.
- [ ] **Stap 4: Draai, verwacht groen** — `go test -race ./internal/initcmd/` → PASS.
- [ ] **Stap 5: Commit** — `feat(init): host.toml-melding zegt dat de keten zonder host.toml werkt (#251)`.

### Taak 4: Docs, configuratiekaart, changelog

**Files:**
- Modify: `docs/site/data/config.json:936-960`, `tests/test_site_config.py:35`, `docs/test-gate.md:53-55`,
  `docs/host-assumptions.md:25-27,42`, `docs/getting-started.md:116-119`, `docs/site/agents/install.md:185-188,270`
- Create: `changelog.d/issue-251-host-toml-zonder-tailscale.md`

- [ ] **Stap 1: Kaart en test**
  - `tests/test_site_config.py`: haal `"host.tailscale_ip": "yes"` uit `REQUIRED` (`SET` blijft:
    `init` schrijft hem).
  - `config.json`, `host.tailscale_ip`: `"required": "no"`, `about`:
    `"The Tailscale address of this workstation; init writes it on a machine with Tailscale. Without it, host-check reports another host."`
    De `about` van de tabel `host` (l.941): `"Who this machine is. lighthouse init writes name and tailscale_ip on a machine with Tailscale; without Tailscale, write it by hand only for the gate_* keys or [usage]."`
  - Run: `uv run pytest -q tests/test_site_config.py` → PASS. Was hij vóór de kaartwijziging rood
    op `host.tailscale_ip: required yes, expected no`? Dat is de check dat de test bijt.
- [ ] **Stap 2: `docs/test-gate.md`** — `name`: `# verplicht in elke host.toml`;
  `tailscale_ip`: `# optioneel; zonder deze sleutel is dit voor host-check een andere host`.
  Voeg onder het voorbeeld één zin toe: een host zonder Tailscale laat `tailscale_ip` weg, en zet
  eerst elke repo op de host op een release met deze wijziging (zie getting-started).
- [ ] **Stap 3: `docs/getting-started.md`** — vervang de geciteerde waarschuwing (l.116-119) door
  de nieuwe tekst uit taak 3, en voeg een sectie `## A host.toml without Tailscale` toe, met:
  - de keten werkt zonder `host.toml`; je hebt hem alleen nodig voor `gate_*` (memory brake,
    slots) of `[usage]`;
  - een voorbeeld voor een gewone Linux-server:

    ```toml
    [host]
    name = "server"
    gate_slots = 2
    gate_min_mem_available_gb = 4
    ```

    met de opmerking dat `gate_slice` een systemd-slice vraagt en de `gate_min_*` alleen op Linux
    meten; link (relatief, vanuit `docs/getting-started.md`) naar `test-gate.md` voor alle sleutels;
  - **de waarschuwing**: first run `lighthouse init --upgrade` in every repository on this host,
    on a release with this change. Only then write a `host.toml` without `tailscale_ip`: an older
    copy of `config.py` rejects it, and then the test gate blocks every commit in that repository.
- [ ] **Stap 4: `docs/site/agents/install.md`** — l.188: `host.toml` is for `lighthouse serve`
  and the test gate's `gate_*` keys; this guide does not write it. l.270: noem de nieuwe tekst
  (`warning: host.toml not proposed: tailscale ip unavailable; …`), nog steeds onschuldig.
- [ ] **Stap 5: `docs/host-assumptions.md`** — rij 1 (l.25): `On another host` wordt "Without
  Tailscale, leave `tailscale_ip` out; `host-check` and `plan_review_record` then report another
  host", `Issue` `—`. Rij 2 (l.26): `init` warns that the chain works without `host.toml`, `Issue`
  `—`. Rij 3 (l.27): `Issue` `—`. l.42: "Every key is optional except `name`".
- [ ] **Stap 6: Changelog** — `changelog.d/issue-251-host-toml-zonder-tailscale.md`:

  ```markdown
  ---
  type: changed
  ---
  `host.tailscale_ip` is optional in `host.toml`, so a host without Tailscale can set the `gate_*` keys. Run `lighthouse init --upgrade` in every repository on the host first: an older copy rejects a `host.toml` without `tailscale_ip`, and its test gate then blocks every commit.
  ```

- [ ] **Stap 7: Controleer** — `uv run pytest -q tests/test_site_config.py tests/pack/test_skill_contracts.py`
  → PASS; `md-link-check` blokkeert niets.
- [ ] **Stap 8: Commit** — `docs(host): host.toml zonder tailscale_ip, met compat-waarschuwing (#251)`.

### Taak 5: Dogfood

- [ ] **Stap 1:** `go run ./cmd/lighthouse init --upgrade` → de `.lighthouse/`-kopieën van
  `config.py` en `plan_review_record.py` veranderen.
- [ ] **Stap 2:** tweede run → `No changes.`
- [ ] **Stap 3:** `git diff --stat` toont alleen `.lighthouse/lib/lighthouse_lib/config.py` en
  `.lighthouse/scripts/plan_review_record.py` (plus wat `init` aan manifest bijwerkt).
- [ ] **Stap 4: Commit** — `chore(dogfood): init --upgrade na #251`.

### Taak 6: Doorloop zonder `host.toml` (bewijs voor het laatste AC)

Gesimuleerd op deze host; de echte doorloop op een Linux-server zonder Tailscale is handwerk.

- [ ] **Stap 1: Zonder `host.toml`** — met `LIGHTHOUSE_HOST_TOML=$SCRATCH/none.toml` (bestaat niet):
  - `python3 .lighthouse/lib/lighthouse_lib/config.py host-check; echo $?` → rc 2, `host unknown: no host.toml`.
  - `python3 .lighthouse/lib/lighthouse_lib/config.py host.gate_slots; echo $?` → rc 1.
  - `python3 -c` op `plan_review_record.host_refusal("spec")` → `SPEC-REVIEW NOT RECORDED: host unknown: …`.
- [ ] **Stap 2: Met `host.toml` zonder `tailscale_ip`** (`[host]\nname = "server"\ngate_slots = 2\n`):
  `host-check` rc 1 met `no tailscale_ip`; `host.gate_slots` → `2`; `host_refusal` → `not on the host from host.toml`.
- [ ] **Stap 3: Een commit met de test-gate** — een hook krijgt de omgeving van de harness, niet
  die van een Bash-aanroep. Draai daarom de gate zelf, zoals `tests/pack/test_commit_test_gate.py`
  dat doet (`Gate.run`, l.150-170): `LIGHTHOUSE_HOST_TOML=<pad> bash .claude/hooks/lighthouse/commit-test-gate.sh post`
  met een PostToolUse-payload van een `git commit` op stdin, na een kleine docs-commit in deze
  worktree. Verwacht: geen `COULD NOT CHECK`, wel `no gate memory threshold in host.toml` (stap 1)
  of de slots-regel met `gate_slots = 2` (stap 2). Beide varianten.
- [ ] **Stap 4: Leg de uitvoer vast** in de PR-body (Evidence). Het AC "op een gewone Linux-server"
  blijft open voor de handen van de gebruiker: de PR zegt `Refs #251`, niet `Closes`, tenzij die
  doorloop er dan al is.
