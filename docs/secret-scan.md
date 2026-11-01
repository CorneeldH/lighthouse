# Secret-scan van checkpoints

Entire legt agentsessies vast als checkpoints onder `refs/entire/*`. Een checkpoint is een
letterlijke kopie van het transcript, dus alles wat een agent las staat erin. De pre-push-hook
(`tools/hooks/pre-push-secret-scan.sh`, geïnstalleerd door `tools/hooks/install.sh`) scant elke
push met gitleaks voordat die de machine verlaat; de checkpoints mogen alleen naar de private
checkpoint-remote. Deze pagina beschrijft de scan van de checkpoints (#198). De gewone branches
gaan nog steeds door `gitleaks git` over het commitbereik, met dezelfde `.gitleaks.toml` als CI.

## Waarom per blob, en begrensd

Op 2026-11-01 legde één `gitleaks git` over alle checkpoints de host plat: 7–10 GB RSS, swap vol.
Elke checkpoint is een root-commit, dus voor `gitleaks git` is de diff de hele boom, en bestanden
als `full.jsonl` staan in elke boom opnieuw. Over 2671 tips waren dat 16.026 blob-vermeldingen
(9,18 GB) tegen 8593 unieke blobs (0,61 GB). Daarom scant `tools/hooks/checkpoint_scan.py` elke
unieke blob één keer, in batches, elke gitleaks-run in een systemd-scope met een plafond.

## Blobmodus en ledger

`checkpoint_scan.py scan --remote <naam> <ref>=<sha>…` (de hook roept dit aan voor `refs/entire/*`):

1. Bepaalt de blobs die de remote mist (`git rev-list --objects` met `--not --remotes=<naam>`).
2. Haalt de blobs weg die de ledger al schoon kent.
3. Zet de rest op grootte in batches van ca. 64 MB, schrijft elke blob als bestand naar een
   tijdelijke map en draait `gitleaks dir` met `.gitleaks.toml`, begrensd.
4. Schrijft na elke batch zonder finding de blobs in de ledger (atomair), en meldt per batch
   `secret-scan: batch 3/12, 64 MB, 0 findings`.

De blobmodus is voor `refs/entire/*` gelijkwaardig aan `gitleaks git`, omdat een checkpoint een
root-commit is. `refs/heads/entire/*` (shadow branches, `entire/checkpoints/v1`) staan op de
werkbranch en gaan, net als gewone refs, door `gitleaks git` over het commitbereik; ze mogen wel
alleen naar de checkpoint-remote.

De ledger staat in `<git-common-dir>/lighthouse/secret-scan/clean-<sleutel>.txt`, gedeeld door alle
worktrees van een clone en nooit in een commit. De sleutel is de sha256 van de bytes van
`.gitleaks.toml` plus de uitvoer van `gitleaks version`. Verandert een van beide, dan begint een
nieuwe ledger en volgt een volledige herscan. Alleen blobs zonder finding uit een batch die
afrondde (rc 0, of rc 1 met een gelezen rapport) komen erin.

Uitslag van `scan`: exit 0 schoon, 1 bevindingen (per ref het pad, de regel en de geredigeerde
match), 2 niet te scannen. Is er meer dan 256 MB ongescand, dan scant de hook niet maar blokkeert
hij met `pre-push: BLOCKED — <n> checkpoint blobs (<mb> MB) are not yet scanned`; draai dan `push`.
Eén vuile ref blokkeert de hele hook-push, want een hook kan niet half doorlaten.

## De begrensde scan

Elke gitleaks-run loopt via `checkpoint_scan.py run-bounded` in:

```
systemd-run --user --scope --quiet -p MemoryMax=<max> -p MemorySwapMax=0 -p CPUQuota=<quota> \
  -- env GOMAXPROCS=<n> choom -n 800 -- nice -n 10 gitleaks …
```

Sleutels in `[host]` van `host.toml`, alle optioneel:

| Sleutel | Standaard | Betekenis |
|---|---|---|
| `scan_memory_max` | `2G` | `MemoryMax` van de scope; zonder swap |
| `scan_cpu_quota` | `200%` | `CPUQuota`; `GOMAXPROCS` volgt eruit |
| `gate_slice` | niet gezet | alleen als gezet gaat de scope in deze slice (zet hier de eigen slicenaam van de host) |

Een batch die op het plafond sneuvelt (rc 137 of SIGKILL; `memory.events` bleek na het einde van
de scope onbetrouwbaar) geeft `secret-scan: ABORTED at memory ceiling (<max>) in batch <i>; raise
host.scan_memory_max or lower the batch size`. De push blokkeert en de batch komt niet in de
ledger. Een andere rc geeft `secret-scan: COULD NOT CHECK — gitleaks exited <rc> in batch <i>`.

Zonder systemd (geen `systemd-run`, geen user-bus of geen `choom`) draait de scan zonder plafond
maar wel in batches, met `GOMAXPROCS` en `nice`, en meldt `secret-scan: no memory ceiling: <reden>`.

Meer dan 200 findings van één regel in één batch geeft `secret-scan: <n> findings for rule
<regel> in one batch; an allowlist is probably missing for this pattern`. Dat blokkeert in de
hook; `push` gaat door (zie hieronder).

## De eerste push: `push`

```
python3 -I tools/hooks/checkpoint_scan.py push https://github.com/CorneeldH/lighthouse-checkpoints.git
```

Er is geen git-remote voor de checkpoints; de URL komt uit `checkpoint_remote` in
`.entire/settings.json`. `push` weigert als dit niet de checkpoint-remote is, of zolang entire zelf
mag pushen (`push_sessions` niet `false`). Het:

1. Scant alle `refs/entire/*` die de remote mist (zelfde batches en begrenzing, ledger bijgewerkt).
2. Schrijft de vuile refs naar `held.txt`, naast de ledger.
3. Pusht de schone refs met een gewone `git push`, in brokken van maximaal 500 refs, dus door de
   pre-push-hook; die vindt alles in de ledger en is snel.
4. Eindigt met `<n> pushed, <m> held — see <pad>`. Exit 0 als niets vuil is, 1 bij vuile refs, 2
   als er niets (of niet alles) gepusht is. Een afgebroken scan gaat bij de volgende run verder
   vanaf de ledger.

`push` stopt niet bij het findings-plafond: de blobs met een finding blijven uit de ledger, hun
refs gaan naar `held.txt`, de melding staat bovenaan het overzicht, en de schone refs gaan uit de
deur.

Let op: `.git/hooks/pre-push` draait eerst het scanscript van het pushende checkout (`install.sh`).
Een worktree op een branch van vóór #198 draait dus nog de oude hook, die `refs/entire/*`
onbegrensd met `gitleaks git` scant. Push checkpoints alleen vanuit een checkout met deze module
(`main` na de merge).

## `held.txt`

`<git-common-dir>/lighthouse/secret-scan/held.txt` wordt bij elke `push` herschreven. Eerst de
meldingen van het findings-plafond, dan één blok per vuile ref: de ref, daarna per finding
`  <pad>  <regel>  <geredigeerde match>`.

## Een vuile checkpoint vrijgeven

- **Vals-positief:** zet een allowlist op de `match` in `.gitleaks.toml`, nooit op `paths`. De
  sleutel van de ledger verandert dan, dus de volgende run herscant alles; draai `push` opnieuw.
- **Echt secret:** roteer het secret en drop de checkpoint.

## De begrensde volledige scan

Een expliciete herscan zonder ledger (die wel wordt bijgewerkt), met piek-RSS per batch:

```
python3 -I tools/hooks/checkpoint_scan.py scan --full --keep-going --remote <naam> \
  $(git for-each-ref --format='%(refname)=%(objectname)' refs/entire/)
```

`<naam>` is een remote die de checkpoint-remote is of ernaar verwijst. `scan` neemt altijd
`<ref>=<sha>`-argumenten. `--keep-going` stopt niet bij het findings-plafond. Draai dit nooit met
een kale `gitleaks git` over `refs/entire/*`.

## Kosten van een volledige scan

Volgt met de meting (#198).
