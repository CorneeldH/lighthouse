# Secret-scan van checkpoints

Entire legt agentsessies vast als checkpoints onder `refs/entire/*`. Een checkpoint is een
letterlijke kopie van het transcript, dus alles wat een agent las staat erin. De pre-push-hook
(`tools/hooks/pre-push-secret-scan.sh`, geïnstalleerd door `tools/hooks/install.sh`) scant elke
push met gitleaks voordat die de machine verlaat; de checkpoints mogen alleen naar de private
checkpoint-remote. Deze pagina beschrijft de scan van de checkpoints (#198). De gewone branches
gaan nog steeds door `gitleaks git` over het commitbereik, met dezelfde `.gitleaks.toml` als CI.

Wat de remote al heeft, scant de hook niet opnieuw: in het commitbereik de shas die git per ref aan
de hook geeft, in beide modi de remote-tracking refs (`--not --remotes=<naam>`). Die laatste volgen
de fetch-URL.
Daarom tellen ze alleen voor een geconfigureerde remote met precies één URL, waarnaar ook de push
gaat (`git remote get-url --push --all <naam>` geeft hetzelfde als `git remote get-url --all
<naam>`). Met een aparte `pushurl`, een tweede URL, of een push naar een kale URL sluit `--remotes`
niets uit: de push-repo kan missen wat de fetch-repo heeft. De regel staat één keer in de module
(`remote_exclusion`, voor de hook via `checkpoint_scan.py remote-exclusion <naam>`).

## Waarom per blob, en begrensd

Op 2026-11-01 legde één `gitleaks git` over alle checkpoints de host plat: 7–10 GB RSS, swap vol.
Elke checkpoint is een root-commit, dus voor `gitleaks git` is de diff de hele boom, en bestanden
als `full.jsonl` staan in elke boom opnieuw. Over 2671 tips waren dat 16.026 blob-vermeldingen
(9,18 GB) tegen 8593 unieke blobs (0,61 GB). Daarom scant `tools/hooks/checkpoint_scan.py` elke
unieke blob één keer, in batches, elke gitleaks-run in een systemd-scope met een plafond.

## Blobmodus en ledger

`checkpoint_scan.py scan --remote <naam> <ref>=<sha>…` (de hook roept dit aan voor `refs/entire/*`):

1. Bepaalt de blobs die de remote mist (`git rev-list --objects`, met `--not --remotes=<naam>` alleen
   als de push naar de enige fetch-URL gaat; zie boven).
2. Haalt de blobs weg die de ledger al schoon kent.
3. Zet de rest op grootte in batches van ca. 64 MB, schrijft elke blob als bestand naar een
   tijdelijke map, met een newline vóór de inhoud, en draait `gitleaks dir .` in die map met
   `.gitleaks.toml`, begrensd.
4. Controleert dat gitleaks alle geschreven bytes scande (zie hieronder).
5. Schrijft na elke batch de blobs zonder finding in de ledger (atomair), en meldt per batch
   `secret-scan: batch 3/12, 64 MB, 0 findings`.

Wat de blobmodus garandeert: elke unieke blob die de remote mist, scant gitleaks volledig, of de
batch telt als niet te scannen (`COULD NOT CHECK`, niets ervan in de ledger). Een checkpoint is een
root-commit, dus `gitleaks git` zou dezelfde inhoud zien. Toch is de blobmodus daar niet vanzelf
gelijk aan; de verschillen en wat de module ertegen doet:

- `gitleaks dir` (8.30.1) slaat een bestand over waarvan de eerste bytes het magic number van een
  binair type zijn (zip, gzip, ELF, PDF, RTF, SQLite, OLE en meer), ook als git het als tekst ziet
  en `gitleaks git` het wel scant. De newline vooraan haalt dat weg: geen type begint daarmee.
  Matches blijven gelijk (gemeten, ook voor een match aan het begin van de blob); alleen
  regelnummers schuiven één op, en de module meldt die niet.
- gitleaks draait in de tijdelijke map op `.`, dus het pad waarop zijn pad-allowlists werken is het
  kale blob-id. Op het absolute pad maakte een `TMPDIR` onder bijvoorbeeld `node_modules/` (in de
  standaard-allowlist) elke blob schoon. Zo doet ook een `.gitleaksignore` van de repo niet mee.
- gitleaks meldt `scanned ~<n> bytes`. Is dat minder dan de batch op schijf (een binair type op een
  vaste offset voorbij de eerste byte, zoals `ustar` op 257 bij tar; een pad-allowlist; een
  groottegrens), of ontbreekt de melding, dan geeft de batch `secret-scan: COULD NOT CHECK —
  gitleaks scanned <n> of <m> bytes in batch <i>; it skipped <blob-id>`.
- Een blob die git binair vindt (een NUL in de eerste 8000 bytes) slaat `gitleaks git` over; de
  blobmodus scant hem wel.

Wat overblijft:

- Een blob die gitleaks ondanks de newline overslaat (de bytetelling klopt niet) maakt de hele batch
  `COULD NOT CHECK`. Daarmee stopt de scan bij die batch: latere batches komen niet in de ledger.
  `push` pusht dan niets (het haalt een oude `held.txt` weg) en de hook blokkeert elke push waarvan
  de ongescande set die blob bevat. Verder komen: drop de checkpoint, of scan hem met de hand.
- De bytetelling leest een logregel van gitleaks. Verandert die in een nieuwe versie, dan is elke
  batch `COULD NOT CHECK`: dicht, niet open.

`refs/heads/entire/*` (shadow branches, `entire/checkpoints/v1`) staan op de
werkbranch en gaan, net als gewone refs, door `gitleaks git` over het commitbereik; ze mogen wel
alleen naar de checkpoint-remote.

De ledger staat in `<git-common-dir>/lighthouse/secret-scan/clean-<sleutel>.txt`, gedeeld door alle
worktrees van een clone en nooit in een commit. De sleutel is de sha256 van de bytes van
`.gitleaks.toml`, de uitvoer van `gitleaks version` en de scanmethode (`SCAN_METHOD` in de module).
Verandert een van de drie, dan begint een nieuwe ledger en volgt een volledige herscan; zo telt een
ledger van vóór de newline en de bytetelling niet meer. Alleen blobs zonder finding uit een batch
die afrondde (rc 0, of rc 1 met een gelezen rapport) en volledig gescand is, komen erin.

Uitslag van `scan`: exit 0 schoon, 1 bevindingen (per ref het pad, het regel-id van gitleaks en de
geredigeerde match), 2 niet te scannen. Is er meer dan 256 MB ongescand, dan scant de hook niet maar blokkeert
hij met `pre-push: BLOCKED — <n> checkpoint blobs (<mb> MB) are not yet scanned`; draai dan `push`.
Eén vuile ref blokkeert de hele hook-push, want een hook kan niet half doorlaten.

## De begrensde scan

Elke gitleaks-run loopt via `checkpoint_scan.py run-bounded` in:

```
systemd-run --user --scope --quiet -p MemoryMax=<max> -p MemorySwapMax=0 -p CPUQuota=<quota> --collect \
  [--slice=<slice>] -- env GOMAXPROCS=<n> choom -n 800 -- nice -n 10 gitleaks …
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
<regel-id> in one batch; an allowlist is probably missing for this pattern (sample: <match>)`,
met `<regel-id>` het id van de gitleaks-regel. Dat blokkeert in de
hook; `push` gaat door (zie hieronder).

## De eerste push: `push`

```
python3 -I tools/hooks/checkpoint_scan.py push https://github.com/CorneeldH/lighthouse-checkpoints.git
```

Er is geen git-remote voor de checkpoints; de URL komt uit `checkpoint_remote` in
`.entire/settings.json`. Een push gaat naar de checkpoint-remote als de remote de naam uit
`checkpoint_push_remote` heeft, of als de URL precies de host van de forge heeft (`github.com`,
`gitlab.com`; als `https://`, `http://`, `ssh://`, `git://` of `[user@]host:`) en precies
`<owner>/<repo>` als pad, eventueel met `.git` of een `/` erachter. Een andere host, ook
`notgithub.com` of `github.com.evil.com`, of de forge in het pad of de query van een andere host,
telt niet. `push` weigert als dit niet de checkpoint-remote is, of zolang entire zelf
mag pushen: er is een `.entire/settings*.json` gelezen, `enabled` is daarin niet `false` en
`strategy_options.push_sessions` is niet `false`. Het:

1. Scant alle `refs/entire/*` die de remote mist (zelfde batches en begrenzing, ledger bijgewerkt).
2. Schrijft de vuile refs naar `held.txt`, naast de ledger.
3. Pusht de schone refs als de commits die het scande (`<sha>:<ref>`, niet wat de ref bij de push
   houdt) met `git push --atomic`, in brokken van maximaal 500 refs, dus door de pre-push-hook;
   die vindt alles in de ledger en is snel. Faalt een brok, dan landt niets van dat brok en
   stopt `push`; `<k> of <n> clean refs pushed` klopt dus.
4. Eindigt met `<n> pushed, <m> held — see <pad>`. Exit 0 als niets vuil is, 1 bij vuile refs, 2
   als er niets (of niet alles) gepusht is. Een afgebroken scan pusht niets en verwijdert
   `held.txt` van een eerdere run; de volgende run gaat verder vanaf de ledger en schrijft
   `held.txt` opnieuw.

`push` stopt niet bij het findings-plafond: de blobs met een finding blijven uit de ledger, hun
refs gaan naar `held.txt`, de melding staat bovenaan het overzicht, en de schone refs gaan uit de
deur.

Let op: `.git/hooks/pre-push` draait eerst het scanscript van het pushende checkout (`install.sh`).
Een worktree op een branch van vóór #198 draait dus nog de oude hook, die `refs/entire/*`
onbegrensd met `gitleaks git` scant. Push checkpoints alleen vanuit een checkout met deze module
(`main` na de merge).

## `held.txt`

`<git-common-dir>/lighthouse/secret-scan/held.txt` wordt bij elke `push` herschreven, en weggehaald
als de scan afbreekt. Eerst de
meldingen van het findings-plafond, dan één blok per vuile ref: de ref, daarna per finding
`  <pad>  <regel-id>  <geredigeerde match>`.

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

`<naam>` is een remote die de checkpoint-remote is of ernaar verwijst; zijn remote-tracking refs
sluiten alleen iets uit als hij naar zijn enige fetch-URL pusht. `scan` neemt altijd
`<ref>=<sha>`-argumenten. `--keep-going` stopt niet bij het findings-plafond. Draai dit nooit met
een kale `gitleaks git` over `refs/entire/*`.

## Kosten van een volledige scan

Gemeten op 2026-11-01 op de always-on host, gitleaks 8.30.1, `.gitleaks.toml` met de allowlist van
de beta-headers, standaardlimieten (`scan_memory_max = "2G"`, `scan_cpu_quota = "200%"`). Het zijn
de cijfers van de tweede meting, na de reviewfixes (newline vooraan, scan in de blobmap, bytetelling).
Een tweede meting na die fixes zag één vuile blob meer dan de eerste en niets minder.

| Maat | Waarde |
|---|---|
| Refs onder `refs/entire/*` | 2671 |
| Unieke blobs | 8593, 582 MiB (ca. 0,61 GB) |
| Batches | 10 (negen van 57-64 MB, de laatste 19 MB) |
| Totale duur | 28,0 s (exit 1: bevindingen) |
| Piek-RSS per batch (gitleaks) | 106, 102, 94, 93, 89, 85, 84, 75, 73, 72 MB |
| Piek-RSS over de hele run | 108 MB |
| Swap | vlak, geen plafond geraakt |
| Findings | 470 over de 10 batches (11, 89, 72, 81, 90, 30, 51, 31, 15, 0); geen batch boven het plafond van 200 |

Na de run kende de ledger 8447 schone blobs; 146 blobs hebben dus een finding. Het rapport telde
1622 regels over 1496 verschillende refs: `generic-api-key` 1596, `aws-access-token` 26. Per ref:

- 1409 refs hadden alleen `turnAttributionKey` (een UUID van entire);
- 43 refs hadden alleen de beta-header;
- 31 refs hadden beide;
- 13 refs uit één sessie hadden de testwaarden uit de `.gitleaks.toml` van een andere repo.

De beta-header-matches waren geen andere header. gitleaks knipt een match af op een chunkgrens, aan
het begin of aan het eind. De allowlist vroeg de hele header en miste die stukken. Na beoordeling
door de gebruiker dekken de allowlists elk afgeknipt stuk van de header en van `turnAttributionKey`,
plus de exacte testwaarden. Een derde volledige scan op 2026-11-02 vond toen 0 findings in alle 10
batches (29,6 s, piek-RSS 107 MB, swap vlak).

Tegen de constanten: een batch van 64 MB kost ca. 100 MB RSS, ver onder het plafond van 2G. De
hele set (582 MiB) ligt boven de 256 MB-grens van de hook, dus de eerste push na deze import loopt
via `checkpoint_scan.py push`, zoals bedoeld. Het findings-plafond (200) is niet bereikt. De
constanten 64 MB, 256 MB en 200 houden stand.

De opdracht, met een `--remote` die niet bestaat zodat niets wordt uitgesloten:

```
/usr/bin/time -v python3 -I tools/hooks/checkpoint_scan.py scan --full --keep-going --remote ckpt-probe \
  $(git for-each-ref --format='%(refname)=%(objectname)' refs/entire/)
```
