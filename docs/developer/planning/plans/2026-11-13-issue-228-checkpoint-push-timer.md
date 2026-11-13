# Checkpoint-push met een systemd user-timer — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** checkpoints bereiken `CorneeldH/lighthouse-checkpoints` vanzelf, altijd gescand, via een
user-timer rond `checkpoint_scan.py push` op de always-on host.

**Architectuur:** de unit en de timer staan niet in de repo; `docs/secret-scan.md` krijgt een
sectie *Host-deploy* met het voorbeeld, naar het model van `docs/triage.md` (*Host-deploy*). Eén
codewijziging: `push` krijgt `--report-rss`, zodat de journal van de timer de piek-RSS per batch
toont (criterium 4). De standaard van `push` blijft stil over RSS, zoals #198 het vastlegde
(`test_push_chunks_refspecs` eist dat).

**Tech stack:** Python stdlib (`tools/hooks/checkpoint_scan.py`), pytest, systemd user-units.

**Spec:** geen; issue #228 is de spec (acceptatiecriteria daar).

## Global Constraints

- `push_sessions` blijft `false` in de gecommitte `.entire/settings.json`; dat bestand wordt niet
  aangeraakt.
- Geen unit- of timerbestand in de repo: ze horen bij de host-deploy.
- Geen `lighthouse init`-integratie (issue: *Later, niet in dit issue*). Wel: de unit heeft precies
  twee repo-specifieke waarden (`WorkingDirectory` en de checkpoint-URL), zodat `init` ze later kan
  invullen.
- Docs in het Nederlands; changelog-fragment in het Engels, `type: added`.
- Relatieve links in `.md`.

## Besluiten (het issue liet ze open)

| Vraag | Besluit | Waarom |
|---|---|---|
| Piek-RSS in de journal | `push --report-rss`, opt-in; de unit zet hem aan | Criterium 4 eist de piek-RSS; #198 hield `push` zonder vlag stil. Een vlag houdt beide waar. |
| Zichtbaarheid exit 1 | Geen `SuccessExitStatus`: exit 1 en 2 zetten de unit op `failed` | Een vastgehouden ref vraagt een mens. `failed` staat in `systemctl --user --failed` en in de kop van `systemctl --user status`; `status=1/FAILURE` versus `status=2` onderscheidt held van COULD NOT CHECK. Zolang de ref vastzit, faalt elke run opnieuw: het signaal dooft niet uit. Er is geen meldkanaal (ntfy e.d.) op de host; `OnFailure=` noemen de docs als uitbreiding, niet als default. |
| Frequentie | `OnCalendar=hourly`, `Persistent=true`, `RandomizedDelaySec=5min` | De ledger maakt een run zonder nieuwe blobs goedkoop (alleen `ls-remote` en `for-each-ref`). Een uur begrenst hoe lang een checkpoint alleen lokaal staat; vaker levert weinig op, want een sessie duurt al tientallen minuten. `Persistent` haalt een gemiste run in na suspend; de vertraging spreidt de run weg van het hele uur, waar andere timers vallen. |
| Credentials | `EnvironmentFile` met `GH_TOKEN` (`chmod 600`), fine-grained token met alleen *Contents: write* op `lighthouse-checkpoints` | De credential-helper is `!/usr/bin/gh auth git-credential`; `gh` leest zijn token nu uit de keyring, en die is in een user-service zonder ingelogde desktop op slot. `gh auth git-credential` geeft `GH_TOKEN` voorrang. Een token met alleen die repo beperkt de schade als het lekt. |
| `PATH` | `%h/.local/bin:/usr/local/bin:/usr/bin:/bin` | `gitleaks` staat in `~/.local/bin`; `git`, `systemctl`, `systemd-run`, `choom` in `/usr/bin`. Zonder één van de laatste drie draait de scan zonder plafond (`no memory ceiling`). |
| Python | `ExecStart=/usr/bin/python3 -I …` | Absoluut pad: geen venv uit `PATH`; `-I` zoals de docs. Het script gebruikt alleen stdlib. |

## Review Focus

1. Keyring op slot: zonder `GH_TOKEN` in de env-file faalt `git push` met exit 2, niet met een
   hangende prompt — `GIT_TERMINAL_PROMPT=0` in de unit voorkomt dat git op een tty wacht.
2. Twee runs tegelijk (de timer en een handmatige `push`): de oneshot start niet dubbel, een
   handmatige run kan wel samenvallen; de docs zeggen: start handmatig met
   `systemctl --user start`, niet met het script.
3. `XDG_RUNTIME_DIR`/user-bus in de service: `systemd-run --user --scope` moet werken; de
   journal mag geen `no memory ceiling` tonen (Task 3, stap 3).
4. De main-checkout op een branch van vóór #198: `push` draait het script van `WorkingDirectory`;
   de docs zeggen dat die op `main` moet staan.
5. `--report-rss` zonder de vlag: `push` blijft stil over RSS (bestaande
   `test_push_chunks_refspecs`, ongewijzigd groen).

---

### Task 1: `push --report-rss`

**Files:**
- Modify: `tools/hooks/checkpoint_scan.py` (`_push_cli` ~l.880, parser `push` ~l.958-960, module-docstring l.29 `push <remote> [--full] [--report-rss]`)
- Test: `tests/test_checkpoint_scan.py` (na `test_push_chunks_refspecs`, ~l.1215)
- Modify: `docs/secret-scan.md` (sectie *De eerste push: `push`*, ~l.116-150)

**Interfaces:**
- Produces: CLI `checkpoint_scan.py push <remote> [--full] [--report-rss]`; met de vlag per batch
  op stderr `secret-scan: batch <i>/<n> peak RSS <MB|unknown> MB` (de regel van `scan_blobs`).

- [ ] **Stap 1: falende test**

```python
def test_push_report_rss_reports_peak_rss_per_batch(px):
    refs = _three_batches(px)

    result = px.push("ckpt", "--report-rss")

    assert result.returncode == 0, result.stderr
    assert px.pushed() == set(refs)
    for i in (1, 2, 3):
        assert re.search(rf"secret-scan: batch {i}/3 peak RSS (\d+|unknown) MB", result.stderr)
```

`_three_batches(sx)` neemt een `ScanFx` (`PushFx` is er een subklasse van) en zet zelf
`LIGHTHOUSE_SCAN_BATCH_BYTES=1500` in `px.env`: drie blobs van 1000 bytes, drie batches.

- [ ] **Stap 2: rood draaien**

Run: `uv run pytest -q tests/test_checkpoint_scan.py::test_push_report_rss_reports_peak_rss_per_batch`
Verwacht: FAIL, `unrecognized arguments: --report-rss` (exit 2).

- [ ] **Stap 3: implementeren**

`push.add_argument("--report-rss", action="store_true", help="report gitleaks' peak RSS per batch (for a timer's journal)")`
en in `_push_cli` `scan_blobs(..., stop_on_cap=False, report_rss=ns.report_rss)`.

- [ ] **Stap 4: groen draaien, plus de stilte-test**

Run: `uv run pytest -q tests/test_checkpoint_scan.py -k "push"`
Verwacht: alle groen, ook `test_push_chunks_refspecs` (zonder vlag geen `peak RSS`).

- [ ] **Stap 5: mutatiecheck** (`/lighthouse-test-standard`)

Zet tijdelijk `report_rss=False` hard in `_push_cli`; de nieuwe test moet rood worden. Terugzetten.

- [ ] **Stap 6: docs-regel**

In *De eerste push: `push`*, na stap 4 van de lijst: één alinea dat `--report-rss` per batch
`batch <i>/<n> peak RSS <m> MB` op stderr zet, bedoeld voor de journal van de timer
(zie *Host-deploy*).

- [ ] **Stap 7: commit**

```bash
git add tools/hooks/checkpoint_scan.py tests/test_checkpoint_scan.py docs/secret-scan.md
git commit -m "feat(secret-scan): push --report-rss meldt piek-RSS per batch (#228)"
```

### Task 2: sectie *Host-deploy* in `docs/secret-scan.md` + changelog

**Files:**
- Modify: `docs/secret-scan.md` (nieuwe `## Host-deploy` na `## Een vuile checkpoint vrijgeven`,
  vóór `## De begrensde volledige scan`)
- Modify: `docs/host-assumptions.md` (één rij: `secret-scan.md#Host-deploy`, systemd user-service en -timer
  voor checkpoint-push, Linux-only, *Example; use the scheduler of the host*, —; naar het model van de
  triage-rij)
- Create: `changelog.d/issue-228-checkpoint-push-timer.md`

**Interfaces:**
- Consumes: `push --report-rss` uit Task 1.
- Produces: de unitnamen `lighthouse-checkpoint-push.service` / `.timer` en de env-file
  `%h/.config/lighthouse/checkpoint-push.env`, die Task 3 gebruikt.

- [ ] **Stap 1: sectie schrijven**

Opbouw als `docs/triage.md` *Host-deploy*: één zin dat unit en timer bij de host-deploy horen en
niet in de repo staan, dan twee ```ini-blokken, dan per regel een bullet met het waarom.

```ini
# ~/.config/systemd/user/lighthouse-checkpoint-push.service
[Unit]
Description=Lighthouse: checkpoints gescand pushen

[Service]
Type=oneshot
WorkingDirectory=%h/repositories/lighthouse
EnvironmentFile=%h/.config/lighthouse/checkpoint-push.env
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Environment=GIT_TERMINAL_PROMPT=0
ExecStart=/usr/bin/python3 -I tools/hooks/checkpoint_scan.py push --report-rss https://github.com/CorneeldH/lighthouse-checkpoints.git
TimeoutStartSec=30min
```

```ini
# ~/.config/systemd/user/lighthouse-checkpoint-push.timer
[Unit]
Description=Lighthouse: checkpoints elk uur gescand pushen

[Timer]
OnCalendar=hourly
RandomizedDelaySec=5min
Persistent=true

[Install]
WantedBy=timers.target
```

Bullets (één per regel, inhoud uit de tabel *Besluiten*): `--user`; `WorkingDirectory` (de
hoofdcheckout, op `main`: `push` draait het script en de pre-push-hook van dat checkout, zie de
let-op in *De eerste push*); `EnvironmentFile` (`GH_TOKEN`, `chmod 600`, fine-grained, alleen
*Contents: write* op `lighthouse-checkpoints`; waarom niet de keyring); `PATH` (welke binaries
waar, en dat zonder `systemctl`/`systemd-run`/`choom` de scan zonder plafond draait);
`GIT_TERMINAL_PROMPT=0` (geen wachtende prompt bij een ontbrekend token: exit 2);
`ExecStart` (`--report-rss`: de piek-RSS per batch in de journal); geen `SuccessExitStatus`
(exitcodes 0/1/2 en waarom 1 de unit op `failed` zet; `status=1/FAILURE` versus `status=2`;
het signaal herhaalt zich elk uur tot de ref is vrijgegeven, zie *Een vuile checkpoint
vrijgeven*); `TimeoutStartSec` (een eerste run zonder ledger scant alles: 30 min ruim boven de
gemeten duur in *De begrensde volledige scan*); `OnCalendar`/`RandomizedDelaySec`/`Persistent`
(frequentie).

Daarna een kort blok **Controleren**:

```
systemctl --user enable --now lighthouse-checkpoint-push.timer
systemctl --user start lighthouse-checkpoint-push.service   # een run nu
journalctl --user -u lighthouse-checkpoint-push.service -n 50
systemctl --user --failed
```

Met de zin: start een handmatige run met `systemctl --user start`, niet met het script, zodat
nooit twee pushes tegelijk lopen. En één zin: wil je een melding, hang dan een eigen unit aan
`OnFailure=`; dat is host-keuze en niet nodig voor het signaal.

Eén zin vooruit: de unit heeft twee repo-specifieke waarden, `WorkingDirectory` en de URL; de
rest is per host gelijk.

- [ ] **Stap 2: changelog-fragment**

```markdown
---
type: added
---
`checkpoint_scan.py push --report-rss` reports gitleaks' peak RSS per batch. `docs/secret-scan.md` gets a *Host-deploy* section with an example systemd user service and hourly timer that push checkpoints scanned; a held ref leaves the unit `failed`.
```

- [ ] **Stap 3: verifiëren**

Run: `.claude/hooks/lighthouse/run.sh md-link-check` gaat vanzelf bij de Write/Edit; daarnaast
`grep -n '^## ' docs/secret-scan.md` toont `## Host-deploy` tussen *Een vuile checkpoint
vrijgeven* en *De begrensde volledige scan*. `git diff --stat .entire/` is leeg.

- [ ] **Stap 4: commit**

```bash
git add docs/secret-scan.md docs/host-assumptions.md changelog.d/issue-228-checkpoint-push-timer.md
git commit -m "docs(secret-scan): host-deploy met user-timer voor checkpoint-push (#228)"
```

### Task 3: deploy en bewijs op de always-on host (handen)

**Niet voor de onbewaakte uitvoerder.** Geen repo-wijziging; vereist een mens (token) en de
always-on host. Een uitvoerder zonder mens stopt na Task 2, draait hier geen `systemctl` en
schrijft de PR-body als `Refs #228 — host-deploy en bewijs voor criteria 2–4 volgen op de
always-on host`, niet `Closes #228`; die criteria worden later bewezen via
`/lighthouse-close-issue` (entry B). Levert het bewijs voor criteria 2–4. Draait op de always-on
host, na merge van Task 1 op `main` (de unit draait het script van de hoofdcheckout). Vóór de
merge kan het tegen deze worktree: zet dan tijdelijk `WorkingDirectory` op het worktree-pad en
zeg dat in het bewijs.

- [ ] **Stap 1: token en units neerzetten**

Eerst de omgeving van de unit controleren: `systemctl --user show-environment`,
`loginctl show-user $USER -p Linger` (moet `yes`; anders `loginctl enable-linger`) en
`command -v entire gitleaks python3` onder het PATH van de unit (`entire` moet erop staan: de
pre-push-hook draait zijn regel na het scanblok).

Mens: fine-grained token aanmaken (alleen `CorneeldH/lighthouse-checkpoints`, *Contents: read
and write*), in `~/.config/lighthouse/checkpoint-push.env` als `GH_TOKEN=…`, `chmod 600`.
Units uit Task 2 naar `~/.config/systemd/user/`, dan
`systemctl --user daemon-reload && systemctl --user enable --now lighthouse-checkpoint-push.timer`.

- [ ] **Stap 2: schone run (criterium 2)**

Na een nieuwe sessie (nieuwe ref onder `refs/entire/`):
`systemctl --user start lighthouse-checkpoint-push.service`, dan
`journalctl --user -u lighthouse-checkpoint-push.service -n 50 --no-pager` toont
`<n> pushed, 0 held`, en
`git ls-remote https://github.com/CorneeldH/lighthouse-checkpoints.git 'refs/entire/*' | grep <nieuwe ref>`
toont de ref. `systemctl --user is-failed lighthouse-checkpoint-push.service` geeft `inactive`.

- [ ] **Stap 3: begrensd (criterium 4)**

Dezelfde journal toont per batch `peak RSS <m> MB` en geen regel met `no memory ceiling`:
`journalctl --user -u lighthouse-checkpoint-push.service -n 200 --no-pager | grep -c 'no memory ceiling'`
geeft `0`. Draaide de run zonder nieuwe blobs (geen batch), maak dan eerst een nieuwe sessie.

- [ ] **Stap 4: vastgehouden ref (criterium 3)**

Een lokale testref met een nep-PAT, die nooit de remote haalt omdat hij vastgehouden wordt:

```bash
cd ~/repositories/lighthouse
blob=$(printf '{"text": "GITHUB_PAT=ghp_%s"}\n' "$(tr -dc A-Za-z0-9 </dev/urandom | head -c 36)" | git hash-object -w --stdin)
tree=$(printf '100644 blob %s\tfull.jsonl\n' "$blob" | git mktree)
commit=$(git commit-tree "$tree" -m "test #228 held")
git update-ref refs/entire/checkpoints/zz/test-228-held "$commit"
# Eerst bewijzen dat de blob gevlagd wordt (verwacht exit 1), anders is de test leeg:
python3 -I tools/hooks/checkpoint_scan.py scan --remote https://github.com/CorneeldH/lighthouse-checkpoints.git "refs/entire/checkpoints/zz/test-228-held=$commit"; echo "scan rc=$?"
systemctl --user start lighthouse-checkpoint-push.service; echo "rc=$?"
systemctl --user status lighthouse-checkpoint-push.service --no-pager | head -5
```

Verwacht: `systemctl start` faalt, status `failed` met `status=1/FAILURE`, journal toont
`held  refs/entire/checkpoints/zz/test-228-held` en `<n> pushed, 1 held`;
`systemctl --user --failed` noemt de unit. `git ls-remote … | grep test-228-held` is leeg.

Opruimen: `git update-ref -d refs/entire/checkpoints/zz/test-228-held`,
`systemctl --user reset-failed lighthouse-checkpoint-push.service`, en een run die weer
`0 held` toont.

- [ ] **Stap 5: criterium 5**

`git show origin/main:.entire/settings.json | grep '"push_sessions": false'` geeft één regel.

- [ ] **Stap 6: bewijs vastleggen**

De journalregels (gecensureerd waar nodig) en de `ls-remote`-regel gaan naar de Evidence-tabel van
de PR (`/lighthouse-write-pr`). Valt deze taak na de merge, dan in het sluitcommentaar
(`/lighthouse-close-issue`, entry B).
