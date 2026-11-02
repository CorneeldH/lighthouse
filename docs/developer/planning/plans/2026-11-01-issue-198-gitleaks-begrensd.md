# Secret-scan van entire-checkpoints per unieke blob — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De scan van `refs/entire/*` leest elke unieke blob één keer, onthoudt wat schoon is,
draait begrensd en laat schone checkpoints door terwijl vuile lokaal blijven (#198).

**Architecture:** Eén Python-module `tools/hooks/checkpoint_scan.py` (stdlib, `python3 -I`) met
drie subcommando's: `run-bounded` (elke gitleaks-aanroep in een systemd-scope), `scan` (de
blobmodus voor de pre-push-hook) en `push` (de begrensde eerste push). De bestaande hook
`tools/hooks/pre-push-secret-scan.sh` splitst de refs en roept de module aan.

**Tech Stack:** bash, Python 3 stdlib, git (`rev-list --objects`, `cat-file --batch`,
`fast-import` in tests), gitleaks 8.30.1 (`gitleaks dir`), systemd-run/choom, pytest.

**Spec:** [2026-11-01-gitleaks-checkpoints-begrensd-design.md](../specs/2026-11-01-gitleaks-checkpoints-begrensd-design.md)

## Global Constraints

- Draai gitleaks **nooit** onbegrensd over `refs/entire/*`; ook niet om iets te proberen.
- Python: alleen stdlib, aangeroepen als `python3 -I`; de module zoekt `config.py` via een pad
  relatief aan zijn eigen plek (`../../.lighthouse/lib`), niet via de cwd.
- Hookmeldingen in het Engels, letterlijk zoals in de spec; `docs/secret-scan.md` in het Nederlands.
- Constanten: `BATCH_BYTES = 64 * 2**20`, `HOOK_VOLUME_LIMIT = 256 * 2**20`, `FINDINGS_CAP = 200`.
- Defaults zonder `host.toml`-sleutel: `scan_memory_max = "2G"`, `scan_cpu_quota = "200%"`.
- Scope: `-p MemoryMax=… -p MemorySwapMax=0 -p CPUQuota=…`, `--slice=<host.gate_slice>` alleen
  als gezet, `GOMAXPROCS` = quota/100 (afgerond naar beneden, minimaal 1), `choom -n 800`, `nice -n 10`.
- De letterlijke slicenaam van de Beelink komt niet in code, tests of docs (`ci/literal-denylist.txt`).
- `config.py` wijzigt in `pack/core/lib/config.py`; daarna `go run ./cmd/lighthouse init --upgrade`
  en de geïnstalleerde kopie meecommitten. CI eist `No changes.`
- Blobmodus alleen voor `refs/entire/*`. `refs/heads/entire/*` en gewone refs: `gitleaks git` over
  het commitbereik, begrensd via `run-bounded`.
- De ledger bevat alleen blobs die gitleaks in een afgeronde batch (rc 0 of 1, rapport gelezen)
  zonder finding zag.
- Atomaire commits, Conventional Commits in het Nederlands, eindigend op de Co-Authored-By-regel.

## Review Focus

1. **Config gewijzigd na een scan**: een blob in de ledger van de oude `.gitleaks.toml` wordt
   opnieuw gescand. Test in Taak 4 (`test_config_change_rescans_everything`).
2. **Kill in batch 2 van 3**: batch 1 staat in de ledger, batch 2 en 3 niet, de scan is rood.
   Test in Taak 4 (`test_kill_mid_run_keeps_earlier_batches`).
3. **gitleaks rc 0 zonder rapport**: geen groen, `COULD NOT CHECK`, ledger leeg. Test in Taak 4
   (`test_missing_report_is_not_clean`).
4. **Eén blob in een schone en een vuile ref**: beide refs worden vastgehouden. Test in Taak 6
   (`test_shared_dirty_blob_holds_every_ref`).
5. **`refs/heads/entire/*` op de werkbranch**: gaat door `gitleaks git` over het commitbereik,
   niet door de blobmodus. Naar de echte checkpoint-remote sluit `--not --remotes=<remote>` niets
   uit (die remote heeft geen remote-tracking refs), dus dat bereik is de hele geschiedenis van de
   werkbranch, nu wel begrensd. Test in Taak 5 (`test_shadow_branch_uses_commit_range`).

---

### Taak 1: allowlist voor de Anthropic beta-headernaam

**Files:**
- Modify: `.gitleaks.toml`
- Test: `tests/test_pre_push_secret_scan.py`

**Interfaces:** geen.

- [ ] **Stap 1: schrijf de falende test**, `@needs_gitleaks`:
  `test_gitleaks_config_allows_beta_header_name`. Schrijf in `tmp_path/t.jsonl` de regel
  `{"anthropic-beta":"oauth-2025-04-20","interleaved-thinking-2025-05-14"}` en in
  `tmp_path/u.jsonl` die regel plus `FAKE_PAT`. `gitleaks dir <bestand> --config .gitleaks.toml
  --no-banner --exit-code 1` geeft rc 0 op `t.jsonl` en rc 1 op `u.jsonl`.
- [ ] **Stap 2:** `uv run pytest -q tests/test_pre_push_secret_scan.py -k beta_header`, verwacht FAIL (rc 1 op `t.jsonl`).
- [ ] **Stap 3:** voeg een `[[allowlists]]` toe met `targetRules = ["generic-api-key"]`,
  `regexTarget = "match"` en een regex die precies de match op `"oauth-2025-04-20","interleaved-thinking-2025-05-14"` dekt.
  Bepaal de exacte match met `gitleaks dir t.jsonl -v` (geen pad, geen bredere regex). Commentaar
  in de stijl van de `row.TriggerKey`-regel.
- [ ] **Stap 4:** dezelfde test, verwacht PASS; ook de hele `tests/test_pre_push_secret_scan.py` groen.
- [ ] **Stap 5:** commit `chore(security): allowlist voor de naam van de Anthropic beta-header (#198)`.

### Taak 2: `host.scan_memory_max` en `host.scan_cpu_quota`

**Files:**
- Modify: `pack/core/lib/config.py` (`HOST_KEYS`, `_OPTIONAL_HOST_KEYS`, `Host`, validator, sleutelkaart)
- Modify (via `init --upgrade`): `.lighthouse/lib/lighthouse_lib/config.py`
- Modify: `docs/site/data/config.json` (twee entries in de `host`-tabel naast `host.gate_slice`:
  `level: advanced`, `required: no`, geen `default`, `readers: []`)
- Modify: `tests/test_site_config.py` (`host.scan_memory_max` en `host.scan_cpu_quota` in `UNREAD`:
  de enige lezer, `tools/hooks/checkpoint_scan.py`, ligt buiten `pack/`, `internal/` en `cmd/`,
  zie `tests/test_site_config.py:210`)
- Test: `tests/pack/test_config.py`, `tests/test_site_config.py`

**Interfaces:**
- Produces: `Host.scan_memory_max: str | None = None`, `Host.scan_cpu_quota: str | None = None`;
  `config.py host.scan_memory_max` geeft rc 0 met de waarde, rc 1 als niet gezet.

- [ ] **Stap 1: falende tests** in de `host.toml`-sectie:
  - `test_load_host_scan_limits`: `scan_memory_max = "2G"`, `scan_cpu_quota = "150%"` → beide velden zo.
  - `test_load_host_scan_limits_absent`: zonder de sleutels → beide `None`.
  - `test_load_host_rejects_invalid_scan_limits`, parametrized: `scan_memory_max` in `'""'`, `"42"`,
    `'"2 GB"'`; `scan_cpu_quota` in `'""'`, `'"150"'`, `'"0%"'` → `ConfigError` met het pad en de sleutelnaam.
    Geldig geheugen: `^[1-9][0-9]*[KMGT]?$`; geldige quota: `^[1-9][0-9]*%$`.
- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_config.py -k scan`, verwacht FAIL.
- [ ] **Stap 3:** implementeer naar het patroon van `gate_slice` (beide sleutels optioneel, rc 1 zonder `host.toml`).
- [ ] **Stap 4:** `uv run pytest -q tests/pack/test_config.py tests/test_site_config.py` PASS (de
  commit-gate draait bij `config.py` alleen `test_config.py`; `test_site_config.py` vangt een sleutel
  die niet op de kaart staat); `go run ./cmd/lighthouse init --upgrade` en daarna nogmaals → `No changes.`
- [ ] **Stap 5:** commit `feat(config): host.scan_memory_max en host.scan_cpu_quota (#198)`, met de geïnstalleerde kopie.

### Taak 3: begrenzing — `run-bounded`

**Files:**
- Create: `tools/hooks/checkpoint_scan.py`
- Test: `tests/test_checkpoint_scan.py` (nieuw; stubs op `PATH` zoals `tests/pack/test_commit_test_gate.py:86-96,112-114`,
  dus ook een `systemctl`-stub: geen test hangt af van de user-bus van de host)

**Interfaces:**
- Produces:
  - `@dataclass(frozen=True) class Limits: memory_max: str; cpu_quota: str; slice: str | None`
  - `load_limits() -> Limits` (defaults bij ontbrekende sleutel of ontbrekende `host.toml`;
    `ConfigError` → exit 2 met `COULD NOT CHECK — <fout>`)
  - `wrap(argv: list[str], limits: Limits) -> tuple[list[str], str | None]` — gewrapte argv en de
    reden als er geen plafond is (`None` als er wel een is)
  - `@dataclass class Run: rc: int; ceiling_hit: bool; output: str; max_rss_kb: int | None` —
    `max_rss_kb` is `ru_maxrss` uit `os.wait4` op het kindproces (in KB op Linux; `systemd-run --scope`,
    `env`, `choom` en `nice` doen `exec`, dus dat is gitleaks); `None` als het niet te meten is
  - `run_bounded(argv: list[str], limits: Limits) -> Run`
  - CLI: `checkpoint_scan.py run-bounded -- <argv…>`: geeft rc 0/1 van het commando door; plafond
    of andere rc → exit 2 met de melding op stderr.

- [ ] **Stap 1: falende tests**. Elke test zet `LIGHTHOUSE_HOST_TOML` op een eigen, volledige
  `host.toml` (`name`, `tailscale_ip`, plus de sleutels van die test), zodat de echte
  `~/.config`-`host.toml` nooit meedoet. Elke wrap- en plafondtest zet naast `systemd-run` en `choom`
  een slagende `systemctl`-stub op `PATH`; alleen de test voor `systemctl --user failed` een falende.
  - `test_wrap_builds_scope`: met nep-`systemctl`, `systemd-run` en `choom` op `PATH` en `Limits("2G","200%",None)`
    begint de argv met `systemd-run --user --scope --quiet -p MemoryMax=2G -p MemorySwapMax=0 -p CPUQuota=200%`,
    bevat hij `GOMAXPROCS=2`, `choom -n 800` en `nice -n 10`, en geen `--slice`.
  - `test_wrap_adds_slice_when_configured`: `Limits(..., slice="s.slice")` → `--slice=s.slice`.
  - `test_no_systemd_says_why`: `PATH` met de `systemctl`-stub maar zonder `systemd-run` → `run-bounded`
    draait het commando toch, en stderr bevat `secret-scan: no memory ceiling: no systemd-run`.
  - `test_max_rss_measured`: `run_bounded` op `python3 -c "b = bytearray(32 * 2**20)"` zonder
    scope → `max_rss_kb` ≥ 32 MB.
  - `test_ceiling_message_on_sigkill`: nep-`systemd-run` die `exit 137` doet → exit 2 en stderr bevat
    `secret-scan: ABORTED at memory ceiling (2G)`.
  - `test_other_rc_is_could_not_check`: commando met rc 5 → exit 2, `COULD NOT CHECK — gitleaks exited 5`.
  - `test_real_ceiling`, skip zonder werkende `systemctl --user show-environment`: echte `systemd-run`,
    `host.toml` met `scan_memory_max = "8M"`, commando `python3 -c "b = bytearray(64 * 2**20)"` → plafondmelding.
- [ ] **Stap 2:** `uv run pytest -q tests/test_checkpoint_scan.py`, verwacht FAIL (module ontbreekt).
- [ ] **Stap 3:** implementeer. Neem de `XDG_RUNTIME_DIR`-reparatie en de ladder van redenen over van
  `pack/core/hooks/commit-test-gate.sh:421-457` (geen systemctl / geen systemd-run / geen choom /
  `systemctl --user show-environment` faalt; de scan vraagt geen slice, dus de slicechecks vallen weg).
  Wacht op het kind met `os.wait4`, niet met `Popen.wait`, voor `max_rss_kb`. Plafond = rc 137 of -9, **of** `oom_kill` > 0 in `memory.events` van de
  scope. Meet met `test_real_ceiling` welk signaal betrouwbaar is na het einde van de scope en
  noteer dat in een commentaar; hou alleen wat werkt.
- [ ] **Stap 4:** tests PASS.
- [ ] **Stap 5:** commit `feat(hooks): gitleaks begrensd in een systemd-scope (#198)`.

### Taak 4: blobmodus, ledger en batches — `scan`

**Files:**
- Modify: `tools/hooks/checkpoint_scan.py`
- Test: `tests/test_checkpoint_scan.py`

**Interfaces:**
- Consumes: `Limits`, `run_bounded` (Taak 3).
- Produces:
  - `@dataclass class Blob: size: int; places: set[tuple[str, str]]` — (ref, pad)
  - `missing_blobs(repo: Path, refs: dict[str, str], remote: str) -> dict[str, Blob]` — `refs` is
    ref → sha; per ref de blobs uit `git rev-list --objects <sha> --not --remotes=<remote>`, met
    grootte via één `git cat-file --batch-check`
  - `ledger_path(repo: Path, config: Path) -> Path` —
    `<git-common-dir>/lighthouse/secret-scan/clean-<sha256(config-bytes + gitleaks version)>.txt`
  - `load_ledger(path: Path) -> set[str]`, `add_to_ledger(path: Path, blobs: Iterable[str]) -> None` (atomair, gesorteerd)
  - `batches(blobs: dict[str, Blob], limit: int = BATCH_BYTES) -> list[list[str]]` — op grootte, een blob groter dan `limit` alleen
  - `@dataclass class Finding: blob: str; rule: str; match: str`
  - `@dataclass class ScanResult: findings: list[Finding]; overloaded: list[str]; aborted: str | None`
    (`overloaded`: regels boven `FINDINGS_CAP` in één batch)
  - `scan_blobs(repo: Path, blobs: dict[str, Blob], config: Path, limits: Limits, ledger: Path | None, stop_on_cap: bool) -> ScanResult`
  - CLI: `checkpoint_scan.py scan --remote <naam> [--full] [--keep-going] <ref>=<sha>…`
    (`--keep-going`: `stop_on_cap=False`, voor de meting): exit 0 schoon, 1 findings
    (per finding `ref`, `pad`, `regel`, geredigeerde match), 2 kon niet scannen. Boven
    `HOOK_VOLUME_LIMIT` ongescand en zonder `--full`: exit 2 met de blokkeermelding uit de spec.

Testfixture: `_bulk_checkpoints(repo, start, n) -> dict[str, str]` met één `git fast-import`-stroom:
per checkpoint `i` een root-commit op `refs/entire/checkpoints/<i%100:02d>/ck<i:04d>` met
`0/full.jsonl` (sessie `i // 10`, regels 0..`i % 10`, zodat het bestand per sessie groeit en blobs
gedeeld worden) en `0/metadata.json`. Nep-`gitleaks`: `version` → `fake 1`; `dir <map> … -r <rapport>`
logt de bestandsnamen naar `$FAKE_GITLEAKS_LOG` en schrijft een JSON-rapport met een finding
(`RuleID: "fake-rule"`, `File`, `Match: "REDACTED"`) per bestand dat `FAKESECRET` bevat; rc 1 bij
findings, anders 0. `FAKE_GITLEAKS_KILL_AT=<n>`: de n-de aanroep doet `kill -9 $$`.

- [ ] **Stap 1: falende tests**:
  - `test_second_scan_touches_only_new_blobs` (AC 1): 500 refs, `scan` → log bevat alle unieke blobs;
    5 refs erbij, `scan` → log bevat precies de nieuwe blobs van die 5 (vergelijk met
    `git ls-tree -r` van de nieuwe tips minus de oude).
  - `test_shared_blob_scanned_once`: twee refs met dezelfde `full.jsonl` → die blob één keer in het log.
  - `test_config_change_rescans_everything`, `test_kill_mid_run_keeps_earlier_batches`
    (`limit` klein genoeg voor 3 batches), `test_missing_report_is_not_clean` (Review Focus 1–3).
  - `test_findings_cap_blocks_scan`: 201 blobs met `FAKESECRET` in één batch → exit 1 en stderr bevat
    `201 findings for rule fake-rule in one batch; an allowlist is probably missing for this pattern`.
  - `test_volume_limit_refuses`: met `HOOK_VOLUME_LIMIT` via env `LIGHTHOUSE_SCAN_VOLUME_LIMIT=1000`
    (alleen voor tests) → exit 2, stderr bevat `are not yet scanned` en `checkpoint_scan.py push`.
  - `test_dirty_blob_not_in_ledger`: één vuile blob → exit 1, de schone blobs uit die batch in de ledger, de vuile niet.
  - `test_real_gitleaks_finds_pat`, `@needs_gitleaks`: één checkpoint met `FAKE_PAT` → exit 1 en de ref in de uitvoer.
- [ ] **Stap 2:** tests FAIL.
- [ ] **Stap 3:** implementeer. Per batch één `tempfile.TemporaryDirectory()` als `<root>`, met de
  blobs in `<root>/blobs/` (via één `git cat-file --batch`, de blob-id als bestandsnaam) en het rapport
  op `<root>/report.json`, dat vóór de gitleaks-aanroep niet bestaat. Nooit een rapportpad buiten
  `<root>`: een gedeeld pad zoals `/tmp/report.json` laat een andere batch of push zijn rapport lezen.
  gitleaks via `run_bounded` met
  `dir <root>/blobs --config <config> --no-banner --redact -f json -r <root>/report.json --exit-code 1`.
  Tussenstand per batch naar stderr: `secret-scan: batch <i>/<n>, <mb> MB, <k> findings`. Met
  `--keep-going` (de meting) daarnaast per batch `secret-scan: batch <i>/<n> peak RSS <mb> MB`
  uit `Run.max_rss_kb` (`unknown` bij `None`).
- [ ] **Stap 4:** tests PASS; `test_second_scan_touches_only_new_blobs` duurt < 20 s.
- [ ] **Stap 5:** commit `feat(hooks): secret-scan van checkpoints per unieke blob met ledger (#198)`.

### Taak 5: de pre-push-hook gebruikt de module

**Files:**
- Modify: `tools/hooks/pre-push-secret-scan.sh` (het verzamelen `:139-164`, de vroege exit en de
  gitleaks-check `:166-176`, die nu doorlopen als de entire-lijst of `tips` niet leeg is, en de scan
  `:182-196`; de header-comment bijwerken)
- Test: `tests/test_pre_push_secret_scan.py`

**Interfaces:**
- Consumes: `checkpoint_scan.py scan` en `run-bounded` (Taak 3, 4).

- [ ] **Stap 1: falende tests**:
  - `test_entire_refs_go_through_blob_scan`: push van 3 checkpoints naar `ckpt` met een nep-gitleaks die logt →
    de aanroep is `gitleaks dir`, niet `gitleaks git`.
  - `test_shadow_branch_uses_commit_range` (Review Focus 5): een `refs/heads/entire/shadow` op `work` naar
    `ckpt` → `gitleaks git` met `--log-opts`, geen `dir`.
  - `test_ceiling_blocks_push`: nep-`gitleaks` plus slagende `systemctl`-stub, `choom`-stub en
    nep-`systemd-run` met rc 137 op `PATH` (zonder nep-gitleaks laat de hook een push naar een lokale
    remote ongescand door, `:168-174`) → push geweigerd, stderr bevat `ABORTED at memory ceiling`,
    niet `found a secret`.
  - `test_bulk_push_refused_with_pointer`: met `LIGHTHOUSE_SCAN_VOLUME_LIMIT=1000` → geweigerd, stderr wijst naar `checkpoint_scan.py push`.
- [ ] **Stap 2:** tests FAIL.
- [ ] **Stap 3:** splits in de lus de ref-regels: `refs/entire/*` (lokaal of remote) naar een lijst `ref=sha`,
  de rest naar `tips`/`excludes` als nu. Entire-lijst niet leeg → `python3 -I "$SCRIPT_DIR/checkpoint_scan.py" scan --remote "$remote" …`.
  Gewone tips → `python3 -I … run-bounded -- gitleaks git …` met de bestaande `log_opts`. Exit 1 →
  de bestaande BLOCKED-melding; exit 2 → `pre-push: BLOCKED — the scan did not complete:` plus de uitvoer van de module.
  Vul de `tool_bin`-lijsten in de bestaande tests aan met wat de hook nu vóór de gitleaks-check aanroept.
  Elke test die de hook naar een fixture-repo kopieert, kopieert nu ook `tools/hooks/checkpoint_scan.py`
  en `.lighthouse/lib/` (de module zoekt `config.py` relatief aan zichzelf), in het bijzonder
  `test_entire_checkpoints_leave_only_through_the_scan` (`tests/test_pre_push_secret_scan.py:336-340`).
- [ ] **Stap 4:** alle tests in `tests/test_pre_push_secret_scan.py` en `tests/test_checkpoint_scan.py` PASS,
  met `LIGHTHOUSE_REQUIRE_GITLEAKS=1`.
- [ ] **Stap 5:** commit `feat(hooks): pre-push scant checkpoints per blob en begrensd (#198)`.

### Taak 6: `push` en `held.txt`

**Files:**
- Modify: `tools/hooks/checkpoint_scan.py`
- Test: `tests/test_checkpoint_scan.py`

**Interfaces:**
- Consumes: `missing_blobs`, `scan_blobs(stop_on_cap=False)`, de checkpoint-remote-regel.
- Produces: CLI `checkpoint_scan.py push <remote-naam-of-url> [--full]`; `held.txt` in
  `<git-common-dir>/lighthouse/secret-scan/`, per vuile ref een blok `ref`, daaronder per finding
  `  <pad>  <regel>  <match>`, bovenaan de `allowlist is probably missing`-regels.

- [ ] **Stap 1: falende tests** (echte pushes naar de bare `ckpt` uit `fx`, met de hook geïnstalleerd):
  - `test_push_sends_clean_and_holds_dirty`, `@needs_gitleaks`: 3 checkpoints, één met `FAKE_PAT` → in `ckpt`
    staan er 2, `held.txt` noemt de derde, stdout eindigt op `2 pushed, 1 held — see <pad>`, exit 1.
  - `test_shared_dirty_blob_holds_every_ref` (Review Focus 4).
  - `test_push_refuses_non_checkpoint_remote`: `push origin` → exit 2, niets gepusht.
  - `test_push_chunks_refspecs`: 1200 schone refs (fixture uit Taak 4, nep-gitleaks) → alle 1200 in `ckpt`,
    in ≥ 3 `git push`-aanroepen (tel via `GIT_TRACE` of een wrapper), en de hook scant in die pushes niets meer (log leeg).
  - `test_push_full_writes_ledger`: `push --full` na een eerdere scan → log bevat alles opnieuw, daarna is de ledger gevuld.
  - `test_cap_in_push_holds_and_continues`: 201 vuile blobs met één regel → de rest gepusht, overzicht begint met de cap-melding.
  - `test_push_aborted_pushes_nothing`: `FAKE_GITLEAKS_KILL_AT=2` met 3 batches → exit 2, niets in `ckpt`,
    batch 1 wel in de ledger.
- [ ] **Stap 2:** tests FAIL.
- [ ] **Stap 3:** implementeer. Welke refs de remote mist: `git ls-remote <remote> 'refs/entire/*'`; de
  checkpoint-remote-regel hergebruikt de logica van de hook (lees `.entire/settings*.json` zoals
  `read_entire_settings`; trek die in de module en laat de hook hem aanroepen als dat de regel op één plek houdt).
  Pushen in brokken van ≤ 500 refspecs `<ref>:<ref>`, gewone `git push`, zonder `--no-verify`.
  Een afgebroken batch (plafond) of `COULD NOT CHECK` in de scan: geen enkele push, exit 2 met de
  melding; een volgende `push` gaat verder dankzij de ledger. Een brok-`git push` die faalt: stop,
  exit 2, meld hoeveel refs al gepusht zijn.
- [ ] **Stap 4:** tests PASS.
- [ ] **Stap 5:** commit `feat(hooks): checkpoint_scan.py push laat schone checkpoints door (#198)`.

### Taak 7: CI, docs en changelog

**Files:**
- Modify: `.github/workflows/ci.yml:82` (voeg `tests/test_checkpoint_scan.py` toe aan de pytest-regel van `secret-scan`)
- Create: `docs/secret-scan.md` (Nederlands: wat de hook doet, blobmodus en ledger, `push`, `held.txt`,
  de `host.toml`-sleutels, hoe je een vuile checkpoint vrijgeeft, de begrensde volledige scan; de meetsectie volgt in Taak 8)
- Create: `changelog.d/issue-198-gitleaks-begrensd.md` (`type: internal`, één alinea)
- Modify: header-comment van `tools/hooks/pre-push-secret-scan.sh`: verwijs naar `docs/secret-scan.md`

- [ ] **Stap 1:** schrijf de bestanden; links relatief.
- [ ] **Stap 2:** `_ci_scan_command()` in de tests vindt nog precies één `run: gitleaks git`-regel:
  `uv run pytest -q tests/test_pre_push_secret_scan.py` PASS. `md-link-check` meldt niets.
- [ ] **Stap 3:** commit `docs(security): secret-scan van checkpoints, CI en changelog (#198)`.
- [ ] **Stap 4:** pas met `/lighthouse-write-issue` de AC-tekst van #198 aan, zoals de spec
  (*Afwijkingen van de AC*) vastlegt: AC 4 meldt het ongescande volume bij de eerste push (pre-push-hook
  en `checkpoint_scan.py push`), niet bij `entire import`; AC 2 citeert de Engelse melding
  `ABORTED at memory ceiling`. Verwijs in de bewerking naar de spec. Geen commit.

### Taak 8: meting op de Beelink (AC 3)

**Files:**
- Modify: `docs/secret-scan.md` (sectie *Kosten van een volledige scan*)

- [ ] **Stap 1:** vanuit deze worktree, met de module van deze branch:
  `/usr/bin/time -v python3 -I tools/hooks/checkpoint_scan.py scan --full --keep-going --remote ckpt-probe $(git for-each-ref --format='%(refname)=%(objectname)' refs/entire/)`
  met `ckpt-probe` een niet-bestaande remote-naam (sluit niets uit). Draai het in de achtergrond en volg
  `free -m` en de tussenstand; breek af bij swapgebruik > 1 GB.
- [ ] **Stap 2:** noteer datum, refs, unieke blobs en MB, aantal batches, totale duur, piek-RSS per batch
  (de regels `peak RSS` uit `--keep-going`; `time -v` geeft alleen het maximum over de hele run) en
  findings per regel.
- [ ] **Stap 3:** schrijf de cijfers en de opdracht in `docs/secret-scan.md`. Verschillen de cijfers sterk
  van 64 MB/256 MB/200, zeg dat in het rapport aan de gebruiker; verplaats geen constante naar config.
- [ ] **Stap 4:** commit `docs(security): gemeten kosten van een volledige checkpoint-scan (#198)`.

### Taak 9: na de merge — de echte eerste push

**Niet in de SDD-uitvoering.** SDD stopt na Taak 8 en de PR. Taak 9 draait de bouwer pas na de merge
van #194 en deze PR naar `main` (keuze van de gebruiker), als eigen sessie: de overdracht na de merge
noemt deze taak als eerste stap. Dan verwijst de hook naar de kopie in de hoofdcheckout.

- [ ] **Stap 1:** `gh repo view CorneeldH/lighthouse-checkpoints --json nameWithOwner,visibility`: verwacht
  `nameWithOwner` = `CorneeldH/lighthouse-checkpoints` en `visibility` = `PRIVATE`.
  Bestaat hij niet of is hij niet privé: stop en meld het.
- [ ] **Stap 2:** in de hoofdcheckout: `bash tools/hooks/install.sh`; controleer dat `.git/hooks/pre-push` het blok bovenaan heeft.
- [ ] **Stap 3:** in de hoofdcheckout: `python3 -I tools/hooks/checkpoint_scan.py push https://github.com/CorneeldH/lighthouse-checkpoints.git`
  (er is geen git-remote voor de checkpoints; de URL past op de checkpoint-remote-regel van de hook,
  `.entire/settings.json` → `checkpoint_remote.repo`). Meld `<n> pushed, <m> held` en het pad van
  `held.txt` aan de gebruiker.
