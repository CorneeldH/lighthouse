# Beelink-audit: host-aannames scheiden en expliciet maken — implementatieplan

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans, taak voor taak. Stappen gebruiken checkboxen
> (`- [ ]`).

**Doel:** elke host-aanname in lighthouse staat met een klasse in `docs/host-assumptions.md`, de
kleine gevallen zijn gefikst, en de CI vangt Beelink-literals voortaan ook in de Go-code.

**Architectuur:** vier taken. `pid_alive` meet met `os.kill(pid, 0)` in plaats van `/proc` en kent
een derde toestand, "niet te meten", die altijd blokkeert. `ci/literal-grep.sh` krijgt meerdere
mappen en een optie die tests overslaat; één CI-stap draait de pack-scan en de Go-scan. De
inventaris is een Engelstalig document, gelinkt vanuit `docs/install.md`.

**Tech stack:** Python 3.11 (stdlib), bash, GitHub Actions, pytest.

**Spec:** [2026-11-07-m3-tweede-omgeving-design.md](../specs/2026-11-07-m3-tweede-omgeving-design.md),
§1 en vraag 4. Issue: #250.

## Globale randvoorwaarden

- De vier klassen, letterlijk uit het issue: **Overal**, **Hostkenmerk**, **Linux-only**,
  **Beelink**. In het Engelse document: `Everywhere`, `Host feature`, `Linux-only`, `Beelink`.
- `docs/host-assumptions.md`, skills en code zijn Engels; commits, PR en dit plan Nederlands.
- Go-denylist, exact: `beelink`, `100.76.`, `/home/corneel`, `paia-forge-`, `paia-software`,
  `paia-infra`. Geen kaal `paia`. `_test.go` en `testdata/` vallen buiten de Go-scan.
- Wijzig nooit `.claude/`, `.agents/` of `.lighthouse/` met de hand. Na elke wijziging onder
  `pack/`: `go run ./cmd/lighthouse init --upgrade --yes` (zonder `--yes` leest `init` een lege
  stdin als "nee" en schrijft niets) en de geïnstalleerde kopie meecommitten, **inclusief
  `.lighthouse/manifest.toml`** (daarin staan de sha256 van elke geïnstalleerde unit); daarna geeft
  `go run ./cmd/lighthouse init --upgrade </dev/null` `No changes.` en is `git status --porcelain`
  leeg.
- Elke PR heeft een fragment in `changelog.d/` (formaat: [releasing.md](../../../releasing.md)).
- De audit fikst alleen wat klein is. Een groter geval wordt een issue via
  `/lighthouse-write-issue`, en de inventaris verwijst ernaar.
- Alle shell-aanroepen met `cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-250-beelink-audit`.

## Review Focus

1. **Een lock-pid van `0` of negatief.** `os.kill(0, 0)` signaleert de eigen procesgroep en slaagt
   altijd. Verwacht: `pid_alive` geeft `None` zonder `os.kill` aan te roepen. Test in taak 1.
2. **Native Windows.** Daar stuurt `os.kill(pid, 0)` een `CTRL_C_EVENT` (signaal 0) naar de
   console-groep. Verwacht: bij `os.name == "nt"` geen aanroep, wel `None`. Test in taak 1.
3. **Een andere `OSError` dan `ESRCH`/`EPERM`** (bijv. `EINVAL`). Verwacht: `None`, geen crash en
   geen "veilig weg". Test in taak 1.
4. **De pack-scan verliest stil `pack/pack_test.go`.** Verwacht: het overslaan van tests geldt
   alleen met `LITERAL_SKIP_TESTS=1`; zonder die variabele telt een treffer in een `_test.go`. Test
   in taak 3.
5. **`SOURCES.toml` bij meerdere mappen.** Verwacht: overgeslagen in elke opgegeven map, niet alleen
   in de eerste; een map met slash aan het eind werkt ook. Test in taak 3.

---

### Task 1: `pid_alive` zonder `/proc`

**Bestanden:**
- Wijzig: `pack/core/scripts/worktree_audit.py:163-164` (`pid_alive`), `:183-211` (`_lock_state`),
  `:713` en `:744` (`live=`), `:1272` (melding), en elke annotatie `Callable[[int], bool]`
  (`:184`, `:315`, `:674`, `:1202`, `:1327`, `:1342`, `:1364`, `:1373`, `:1442`, `:1470`)
- Wijzig: `pack/core/skills/audit-worktrees/SKILL.md:74`
- Test: `tests/pack/test_worktree_audit.py`
- Geïnstalleerd (via `init --upgrade`): `.lighthouse/scripts/worktree_audit.py`,
  `.claude/skills/lighthouse-audit-worktrees/SKILL.md`, `.agents/skills/lighthouse-audit-worktrees/SKILL.md`

**Interfaces:**
- Produceert: `Alive = Callable[[int], Optional[bool]]` (moduleniveau, na de imports; een alias
  wordt tijdens runtime geëvalueerd, en `bool | None` geeft op Python 3.9 een `TypeError` vóór de
  nette versiemelding van `config.py`);
  `pid_alive(pid: int) -> bool | None` — `True` bestaat (ook `EPERM`), `False` alleen bij `ESRCH`,
  `None` niet te meten.

- [ ] **Stap 1: schrijf de falende tests** in een nieuwe klasse `TestPidAlive`, plus twee tests in
  `TestLockHandling`. Patch `audit.os.kill` en `audit.os.name` met `monkeypatch` (het script laadt
  `os` nog niet; stap 3 voegt `import os` toe, dus de tests falen eerst op `AttributeError`).

```python
class TestPidAlive:
    def test_own_pid_is_alive(self):
        assert audit.pid_alive(os.getpid()) is True

    def test_eperm_counts_as_alive(self, monkeypatch):
        def deny(pid, sig):
            raise PermissionError(errno.EPERM, "Operation not permitted")
        monkeypatch.setattr(audit.os, "kill", deny)
        assert audit.pid_alive(4242) is True

    def test_only_esrch_is_dead(self, monkeypatch):
        def gone(pid, sig):
            raise ProcessLookupError(errno.ESRCH, "No such process")
        monkeypatch.setattr(audit.os, "kill", gone)
        assert audit.pid_alive(4242) is False

    def test_other_oserror_is_not_measurable(self, monkeypatch):
        def odd(pid, sig):
            raise OSError(errno.EINVAL, "Invalid argument")
        monkeypatch.setattr(audit.os, "kill", odd)
        assert audit.pid_alive(4242) is None

    @pytest.mark.parametrize("pid", [0, -1])
    def test_non_positive_pid_is_not_measurable_and_never_signalled(self, monkeypatch, pid):
        monkeypatch.setattr(audit.os, "kill", lambda *_: pytest.fail("os.kill called"))
        assert audit.pid_alive(pid) is None

    def test_native_windows_is_not_measurable_and_never_signalled(self, monkeypatch):
        monkeypatch.setattr(audit.os, "kill", lambda *_: pytest.fail("os.kill called"))
        monkeypatch.setattr(audit.os, "name", "nt")
        assert audit.pid_alive(4242) is None
```

In `TestLockHandling`, met de bestaande `row(...)`-vorm van `test_live_lock_pid_blocks` en
`alive=lambda _pid: None`:
- `test_unmeasurable_lock_pid_blocks`: `verdict.rank == 2`, `verdict.safe is False`,
  `"not measurable" in verdict.text`.
- `test_unmeasurable_session_pid_is_live`: in `TestBuildRows`, via `self._rows(...)` met een
  **niet-gelockte** worktree (`audit.WorktreeInfo("/repo/.claude/worktrees/wt-a", "feat-a", False,
  None)`) en `alive=lambda _pid: None`: de rij van `feat-a` heeft `live is True` en is niet `safe`.
  Een gelockte worktree bewijst hier niets: de lock alleen geeft al rang 2. Test daarnaast de
  hulpfunctie uit stap 3 direct: `_is_live(None) is True`, `_is_live(False) is False`.

Voeg `import errno` en `import os` toe aan de testimports.

- [ ] **Stap 2: draai de tests, zie ze falen**

Run: `uv run pytest -q tests/pack/test_worktree_audit.py -k "PidAlive or unmeasurable or is_live"`
Verwacht: FAIL (`AttributeError: module 'worktree_audit' has no attribute 'os'` of verkeerde waarde).

- [ ] **Stap 3: implementeer** in `pack/core/scripts/worktree_audit.py`:
  - `import errno`, `import os`, `from typing import Optional`; `Alive` als boven; vervang elke
    `Callable[[int], bool]`.
  - `pid_alive`: `pid <= 0` of `os.name == "nt"` geeft `None` vóór elke aanroep (commentaar: op
    Windows is signaal 0 `CTRL_C_EVENT`); dan `os.kill(pid, 0)`; `ProcessLookupError` → `False`,
    `PermissionError` → `True`, andere `OSError` → `None`.
  - `_lock_state`: `None` geeft `Verdict(2, "⛔", f"locked, pid {row.lock_pid} not measurable")`;
    werk de docstring bij ("unknown or unmeasurable is not dead").
  - `_is_live(state: bool | None) -> bool`: `state is not False`. Gebruik op `:713` en `:744`:
    `live=session is not None and _is_live(alive(session.pid))`.
  - `:1272`: `no longer in /proc` wordt `no longer running`.

- [ ] **Stap 4: SKILL.md:74** — rang 2 wordt: "worktree `locked` and the lock pid still runs
  (`os.kill(pid, 0)` succeeds or gives `EPERM`), or the pid is unknown or not measurable (native
  Windows), or a running session has the worktree as cwd".

- [ ] **Stap 5: draai de tests**

Run: `uv run pytest -q tests/pack/test_worktree_audit.py`
Verwacht: PASS, alle tests.

- [ ] **Stap 6: dogfood**

Run: `go run ./cmd/lighthouse init --upgrade --yes && go run ./cmd/lighthouse init --upgrade </dev/null`
Verwacht: de tweede keer `No changes.`

- [ ] **Stap 7: commit**

```bash
git add pack/core/scripts/worktree_audit.py pack/core/skills/audit-worktrees/SKILL.md \
  tests/pack/test_worktree_audit.py .lighthouse/scripts/worktree_audit.py \
  .claude/skills/lighthouse-audit-worktrees .agents/skills/lighthouse-audit-worktrees \
  .lighthouse/manifest.toml
git commit -m "fix(worktree-audit): pid meten zonder /proc, niet te meten blokkeert (#250)"
git status --porcelain   # verwacht: leeg
```

---

### Task 2: `journalctl -k` gemarkeerd als Linux-only

**Bestanden:**
- Wijzig: `pack/core/skills/fix-pr/claude/SKILL.md:157-166`, `pack/core/skills/fix-pr/agents/SKILL.md:158-167`
- Geïnstalleerd: `.claude/skills/lighthouse-fix-pr/SKILL.md`, `.agents/skills/lighthouse-fix-pr/SKILL.md`

- [ ] **Stap 1: tekst** — in beide varianten, direct vóór het `journalctl`-blok, dezelfde zin:
  "Linux only (systemd journal). On macOS: `log show --last 1h --predicate 'eventMessage CONTAINS
  \"memorystatus\"'`; elsewhere, or without access to the runner host, skip the confirmation: a
  step without `completed_at` plus a green local run is the recognition." Geen hostnaam, geen
  pad (M3-no-go: geen host-specifieke tekst in pakketten).

- [ ] **Stap 2: controle**

Run: `go run ./cmd/lighthouse init --upgrade --yes && go run ./cmd/lighthouse init --upgrade </dev/null && ci/literal-grep.sh`
Verwacht: tweede upgrade `No changes.`, literal-grep rc 0.

Draai ook `uv run pytest -q tests/pack -k "fix_pr or skill"`; een test die de pariteit van de
claude- en agents-variant vastpint moet groen blijven.

- [ ] **Stap 3: commit**

```bash
git add pack/core/skills/fix-pr .claude/skills/lighthouse-fix-pr .agents/skills/lighthouse-fix-pr \
  .lighthouse/manifest.toml
git commit -m "docs(fix-pr): journalctl -k als Linux-only markeren (#250)"
git status --porcelain   # verwacht: leeg
```

---

### Task 3: literal-grep ook op `internal/` en `cmd/`

**Bestanden:**
- Wijzig: `ci/literal-grep.sh`
- Nieuw: `ci/literal-denylist-go.txt`
- Wijzig: `ci/literal-allowlist.txt`
- Wijzig: `.github/workflows/ci.yml:112-113`
- Wijzig: `ci/domains.py:39-48` (`_NOT_GO`) en het commentaar op `:25-31`
- Test: `tests/test_literal_grep.py`, `tests/test_ci_domains_pin.py:70-86`, `tests/test_domains.py:50-51`

**Interfaces:**
- Produceert: `ci/literal-grep.sh [dir ...]` (standaard `pack`); env `LITERAL_DENYLIST`,
  `LITERAL_ALLOWLIST` (bestaand) en `LITERAL_SKIP_TESTS=1` (nieuw: voegt
  `--exclude=*_test.go --exclude-dir=testdata` toe).

- [ ] **Stap 1: falende tests** in `tests/test_literal_grep.py` (hergebruik `_grep`; geef het een
  `*targets`-variant of een tweede helper voor meerdere mappen):
  - `test_go_tests_are_skipped_only_on_request`: `a_test.go` en `testdata/x.json` met `beelink`;
    zonder env rc 1, met `LITERAL_SKIP_TESTS=1` rc 0.
  - `test_several_dirs_are_scanned`: twee mappen, de treffer in de tweede; rc 1 en die map in stdout.
  - `test_sources_toml_is_skipped_in_every_dir`: `SOURCES.toml` met `paia` in beide mappen, de
    tweede met slash aan het eind opgegeven; rc 0.
  - `test_go_denylist_has_no_bare_paia`: de patronen van `ci/literal-denylist-go.txt` (zonder
    commentaar en lege regels) zijn exact de zes uit de randvoorwaarden.
  - `test_go_is_clean`: `LITERAL_DENYLIST=ci/literal-denylist-go.txt LITERAL_SKIP_TESTS=1` op
    `internal` en `cmd` geeft rc 0. Deze test pint de allowlist-regel van `stop.go:34`.

In `tests/test_ci_domains_pin.py`: de stap blijft er één, maar wordt een `run: |`-blok met
precies deze twee regels; pas de regex en de `count`-assert daarop aan (de bedoeling blijft: de
grep draait in één shard op één os). In `tests/test_domains.py:50-51`: voeg
`(["ci/literal-denylist-go.txt"], {"python"})` toe.

- [ ] **Stap 2: draai, zie falen**

Run: `uv run pytest -q tests/test_literal_grep.py tests/test_ci_domains_pin.py tests/test_domains.py`
Verwacht: FAIL op de nieuwe tests.

- [ ] **Stap 3: script** — `DIRS=("${@:-pack}")`, slash aan het eind per map eraf; `grep -r` krijgt
  alle mappen; de awk-`skip` wordt een set van `<dir>/SOURCES.toml` per map. Werk de kopregel
  (`Usage:`) en het commentaar bij. De pack-standaard verandert niet.
  **Bash 3.2:** het script draait op de macOS-shards met `/bin/bash` 3.2 onder `set -u`; daar is een
  lege array-expansie `"${EXCL[@]}"` een `unbound variable`-fout, en in `HITS=$(...)` wordt die rc 1
  stil tot `exit 0`. Breid een mogelijk lege array dus uit als `${EXCL[@]+"${EXCL[@]}"}` (voor `DIRS`
  niet nodig: die heeft altijd minstens `pack`), of gebruik een `if`/`else` met twee
  `grep`-aanroepen.

- [ ] **Stap 4: lijsten**
  - `ci/literal-denylist-go.txt`: kopcommentaar zoals de pack-lijst (waarom geen kaal `paia`: de
    leesmarkers `<!-- paia:` en de `/paia-`-herschrijving in `internal/protocol` en
    `internal/dispatch` zijn bewuste compatibiliteit), dan de zes patronen.
  - `ci/literal-allowlist.txt`: commentaarregel "kenn-forge unit name, class Beelink; #254 moves it
    into configuration. Pinned by tests/test_literal_grep.py::test_go_is_clean.", dan exact
    `internal/workplace/kennforge/stop.go:` + tab + `return "paia-forge-" + profile + "-" + string(b)`.
    Controleer de regel met `sed -n 34p internal/workplace/kennforge/stop.go | cat -A`.

- [ ] **Stap 5: CI en domains**

```yaml
      - if: matrix.shard == 1 && matrix.os == 'ubuntu-latest'
        run: |
          ci/literal-grep.sh
          LITERAL_DENYLIST=ci/literal-denylist-go.txt LITERAL_SKIP_TESTS=1 ci/literal-grep.sh internal cmd
```

De stap blijft in de python-job: die draait bij elke wijziging buiten `web/`, dus ook bij Go-code
en bij de lijsten. `ci/domains.py`: `ci/literal-denylist-go.txt` in `_NOT_GO`, en het commentaar
noemt de Go-scan.

- [ ] **Stap 6: draai**

Run: `uv run pytest -q tests/test_literal_grep.py tests/test_ci_domains_pin.py tests/test_domains.py && ci/literal-grep.sh && LITERAL_DENYLIST=ci/literal-denylist-go.txt LITERAL_SKIP_TESTS=1 ci/literal-grep.sh internal cmd`
Verwacht: PASS, beide scans rc 0.

Mutatiecheck: haal de allowlist-regel tijdelijk weg; `test_go_is_clean` moet dan falen met
`stop.go:34` in de uitvoer. Zet hem terug.

- [ ] **Stap 7: commit**

```bash
git add ci/ .github/workflows/ci.yml tests/test_literal_grep.py tests/test_ci_domains_pin.py tests/test_domains.py
git commit -m "ci(literal-grep): Go-denylist ook op internal/ en cmd/ (#250)"
```

---

### Task 4: inventaris, installatiegids en changelog

**Bestanden:**
- Nieuw: `docs/host-assumptions.md`
- Wijzig: `docs/install.md:6-14`
- Wijzig: `docs/test-gate.md:50-62` (voorbeeld host-neutraal)
- Nieuw: `changelog.d/issue-250-beelink-audit.md` (naam volgt de branch, [releasing.md](../../../releasing.md))

- [ ] **Stap 1: scan** — draai en bewaar (in de scratchpad) de treffers op `pack/`, `internal/`,
  `cmd/`, `deploy/`, `docs/` (zonder `docs/developer/planning/`), tests en `testdata/` uitgezonderd.
  Hoofdletterongevoelig (`-i`), anders mist de scan `Beelink`:

```bash
grep -rniIE '/proc|journalctl|systemctl|systemd|pgrep|pkill|podman|choom|tailscale|date -d|sha256sum|127\.0\.0\.1|~/\.kenn|beelink|gates\.slice|100\.76\.|/home/|paia-forge-|tomllib' \
  pack internal cmd deploy docs --exclude='*_test.go' --exclude-dir=testdata --exclude-dir=planning \
  > <scratchpad>/host-scan.txt
cut -d: -f1 <scratchpad>/host-scan.txt | sort -u > <scratchpad>/host-scan-files.txt
```

  Groepeerregel: één rij per bestand en aanname, niet per treffer; de `Where`-kolom noemt de
  regel(s).

- [ ] **Stap 2: schrijf `docs/host-assumptions.md`** (Engels). Opbouw: één alinea doel, de vier
  klassen als tabel (betekenis, waar het mag staan, uit het issue), dan één tabel met kolommen
  `Where` (relatief pad + regel, als link), `Assumption`, `Class`, `On another host`, `Issue`.
  Minstens deze rijen (bron: spec vraag 4 en de Go-kant):

| Plek | Klasse | Issue |
|---|---|---|
| `pack/core/lib/config.py:482-494`, `plan_review_record.py:147-148` (`host.tailscale_ip` verplicht) | Hostkenmerk | #251 |
| `internal/initcmd/host.go:77-93` (`tailscale ip -4`) | Hostkenmerk | #251 |
| `worktree_audit.py` `pid_alive` | Overal (na taak 1; native Windows: niet te meten) | — |
| fix-pr `journalctl -k` | Linux-only (na taak 2) | — |
| `commit-test-gate.sh:518-546` (`systemd-run`/`choom`, achter `host.gate_slice`) | Hostkenmerk | — |
| `commit-test-gate.sh:459`, `pack/core/lib/host_memory.py` (`/proc/meminfo`) | Linux-only, al gedocumenteerd in [test-gate.md](../../../test-gate.md) | — |
| `pack/core/lib/config.py:27` (Python ≥ 3.11, `tomllib`) | Overal (vereiste) | — |
| `pack/shell/scripts/lib/shellcheck-bin.sh:22-25,67` (`sha256sum`, geen Intel-Mac/Windows) | Linux-only | geen issue zolang de DoD-run `pack/shell` niet gebruikt (spec vraag 4) |
| `internal/cli/brake.go:42-47`, `internal/dispatch/brake.go`, `internal/meminfo` (`/proc/meminfo`) | Linux-only (dispatcher) | milestone "Werkplek zonder kenn-forge" |
| `internal/hostconfig/hostconfig.go:69,72` (`127.0.0.1:8091`, `~/.kenn/forge/auth_token`) | Beelink-default achter een sleutel | #253 |
| `internal/workplace/kennforge/stop.go:34` (`paia-forge-`) | Beelink | #254 |
| `internal/workplace/kennforge/stop.go` (`pgrep`/`pkill`/`systemctl`/`podman`) | Linux-only | #253 |
| `internal/usage/meter.go:165` (`pgrep -x claude`) | Linux-only of Overal-zonder-Windows: bepaal uit de code | — |
| `deploy/systemd/lighthouse.service` | Linux-only (voorbeeld) | — |
| `docs/test-gate.md:50-62` (voorbeeld-`host.toml`) | Hostkenmerk (na de fix in stap 2b) | — |
| `docs/workplace.md:174` (unitnaam `paia-forge-<profiel>-…`) | Beelink (beschrijft `stop.go:34`) | #254 |
| `docs/install.md` (`journalctl --user`, systemd user units) | Linux-only, al vereiste | — |
| hooks op native Windows | Linux-only/WSL | #90 |

  Elke treffer uit stap 1 die hier niet staat, krijgt een rij. Is er een treffer die geen
  host-aanname is (bijv. de `paia`-leesmarkers), dan geen rij. Een nieuw geval dat groter is dan
  een kleine fix: issue via `/lighthouse-write-issue`, nummer in de kolom `Issue`.

  Een rij voor `usage-gate.sh` (`date -d` met BSD-fallback, `pack/core/hooks/usage-gate.sh:75`):
  Overal.

  Dekkingscontrole: elk bestand in `host-scan-files.txt` staat in de `Where`-kolom, of in een korte
  lijst "Scanned, no host assumption" onderaan het document (met één reden per bestand, bijv. de
  `paia`-leesmarkers). Controleer dat met een lus over `host-scan-files.txt` en `grep -F` op
  `docs/host-assumptions.md`; geen bestand mag ontbreken.

- [ ] **Stap 2b: `docs/test-gate.md` host-neutraal** — de spec (§1) staat klasse Beelink alleen
  toe in `host.toml`, `deploy/` of personal_ai_agent, niet in `docs/`. Kop "Voorbeeld: een
  Beelink" wordt "Voorbeeld: een thuisserver", `name = "beelink"` wordt `name = "thuisserver"`; de
  rest van het voorbeeld (`gates.slice`, drempels) blijft, want dat zijn hostkenmerken achter
  sleutels. Daarna: `grep -ni beelink docs/test-gate.md` geeft niets.

- [ ] **Stap 3: `docs/install.md`** — in de vereisten verdwijnt "`(usage-gate.sh` uses GNU
  `date -d`)"; de zin wordt "The CLI runs on macOS, but the hooks are untested there." Voeg een
  regel toe: "Every assumption about the host, with what another host does:" plus een relatieve
  markdown-link naar `host-assumptions.md`.

- [ ] **Stap 4: changelog** — `changelog.d/issue-250-beelink-audit.md`, `type: fixed`: "The
  worktree audit measures a lock pid without `/proc`, so it works on macOS; a pid it cannot measure
  blocks removal. `docs/host-assumptions.md` lists every assumption about the host."

- [ ] **Stap 5: controle**

Run: `uv run pytest -q tests/ -k "docs or changelog or link" && ci/literal-grep.sh`
Verwacht: PASS, rc 0. De hook `md-link-check` blokkeert bij een dode of absolute link.

- [ ] **Stap 6: commit**

```bash
git add docs/host-assumptions.md docs/install.md docs/test-gate.md changelog.d/issue-250-beelink-audit.md
git commit -m "docs(host): inventaris van host-aannames, installatiegids linkt ernaar (#250)"
```

---

## Afronding

- Volledige test gate: `[test_gate] command` uit `lighthouse.toml` (de hook draait die na elke commit).
- `go run ./cmd/lighthouse init --upgrade` geeft `No changes.`
- PR via `/lighthouse-write-pr` (`Closes #250` met bewijs per AC).
