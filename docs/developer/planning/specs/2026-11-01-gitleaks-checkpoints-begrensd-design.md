# Secret-scan van entire-checkpoints: per unieke blob, begrensd en incrementeel

Issue: #198 · Datum: 2026-11-01 · Area: meta

**Status:** Ontwerp. De keuzes van de gebruiker komen uit de brainstorm van 2026-11-01.
**Bouwt voort op:** de branch `issue-194-entire-checkpoints` (nog niet gemerged): de pre-push-hook
`tools/hooks/pre-push-secret-scan.sh`, `tools/hooks/install.sh`, `.gitleaks.toml` en de
`secret-scan`-job in `.github/workflows/ci.yml`. Deze branch stapelt daarop.

## Probleem

Op 2026-11-01 legde één gitleaks-run over `refs/entire/*` de Beelink plat: 7–10 GB RSS, swap
vol, de OOM-killer killde andere sessies. De pre-push-hook van #194 doet bij de eerste push van
de checkpoints precies dezelfde run: alle tips in één `gitleaks git --log-opts=…`.

Twee oorzaken, allebei gemeten:

- **De invoer wordt vijftien keer gelezen.** Elke checkpoint is een root-commit, dus voor
  `gitleaks git` is de diff de hele boom. Over de 2671 tips zijn dat 16.026 blob-vermeldingen,
  samen **9,18 GB**. Uniek zijn er 8593 blobs, samen **0,61 GB**. `full.jsonl` groeit per
  checkpoint en staat in elke boom opnieuw.
- **Het geheugen groeit met de bevindingen.** gitleaks houdt alle findings vast tot het rapport.
  Van de 252.858 findings waren er 245.774 `generic-api-key` op de Anthropic beta-headernaam
  `"oauth-2025-04-20","interleaved-thinking-2025-05-14"`: vals-positief. Die allowlist staat nog
  niet in `.gitleaks.toml`.

De checkpoints moeten wel naar de private remote, ook de 2671 uit `entire import claude-code`.
Een eenmalige zware scan vermijden door ze lokaal te houden, is dus geen optie.

## Doelen

- De eerste push van alle checkpoints kost één scan van de unieke inhoud (≈ 0,61 GB), in
  batches, met een plafond op geheugen en CPU. De host blijft bereikbaar.
- Elke volgende scan raakt alleen blobs die nog niet schoon bevonden zijn.
- Een hit in een paar checkpoints houdt de rest niet tegen: schone checkpoints gaan de deur uit,
  vuile blijven lokaal met een lijst van wat er mis is.
- Een scan die op het plafond sneuvelt of te veel findings oplevert, zegt dat met een eigen
  melding. Hij is nooit groen of leeg.

## Niet-doelen

- Geen nachtelijke of geplande scan. Met 0,61 GB in batches is dat niet nodig.
- Geen wijziging aan de entire-regels van #194 (push_sessions, de checkpoint-remote, de plek
  van het hookblok).
- De gewone branches blijven zoals #194 ze scant: `gitleaks git` over het commitbereik, met
  dezelfde config als CI.
- Geen beoordeling van de 78× `aws-access-token`, 39× `GITHUB_TOKEN=…` en 6928×
  `turnAttributionKey` in dit issue. Dat is mensenwerk op de lijst die de push-opdracht maakt.

## Anti-doelen

- Een scan die een checkpoint als schoon markeert zonder dat gitleaks hem gezien heeft
  (lege batch, gekilde run, verkeerde config).
- Een allowlist op pad (`paths`) voor checkpoint-inhoud. Allowlists blijven op de match.
- Een begrenzing die op een host zonder systemd stil wegvalt.

## Aanpakken

Overwogen in de brainstorm:

| | Aanpak | Afweging |
|---|---|---|
| **A** | Per unieke blob, met een ledger van schone blobs; `gitleaks dir` per batch | Eerste scan 0,61 GB, daarna alleen nieuwe blobs. Findings wijzen naar een blob, dus er is een eigen opzoektabel naar ref en pad nodig. |
| B | Ledger per ref, `gitleaks git` per batch van refs | Dicht bij de huidige hook, findings houden commitcontext. Eerste scan blijft 9,18 GB; alleen incrementeel per ref. |
| C | Alleen begrenzen en batchen, geen ledger | Kleinste wijziging, maar elke push herscant alles wat de remote mist. Haalt AC 1 niet. |

**Gekozen: A** (gebruiker). Het is de enige aanpak waarbij de omvang van de scan niet meegroeit
met het aantal checkpoints.

Voor een root-commit is A precies gelijkwaardig aan `gitleaks git`: de diff van een root-commit
is de volledige inhoud van elke blob. Daarom geldt de blobmodus alleen voor `refs/entire/*`. Voor
een gewone commit zou een volledige blob meer zien dan de toegevoegde regels, en daarmee afwijken
van CI.

`refs/heads/entire/*` (shadow branches, de legacy `entire/checkpoints/v1`) zijn geen
root-commits: ze staan op de werkbranch. In blobmodus zou `rev-list --objects` de hele
repo-geschiedenis meenemen, want de checkpoint-remote heeft niets van `main`. Die refs gaan dus,
net als gewone refs, door `gitleaks git` over het commitbereik. De regel dat ze alleen naar de
checkpoint-remote mogen, blijft. `push` behandelt alleen `refs/entire/*`.

## Ontwerp

### Onderdelen

1. **`tools/hooks/checkpoint_scan.py`**: Python, alleen stdlib, aangeroepen met `python3 -I`.
   Eén module voor de hook en de push-opdracht, met twee subcommando's:
   - `scan --remote <naam> <tip>…`: scant de blobs die de remote mist en nog niet in de ledger
     staan. Geeft de vuile refs terug.
   - `push <remote>`: scant alle `refs/entire/*` die de remote mist en pusht daarna alleen de
     schone refs met een gewone `git push` (dus door de pre-push-hook).
2. **`tools/hooks/pre-push-secret-scan.sh`**: splitst de ref-regels. Entire-refs gaan naar
   `checkpoint_scan.py scan`, gewone refs blijven bij de bestaande `gitleaks git`-aanroep, nu
   ook begrensd.
3. **Een gedeelde begrenzing** (`tools/hooks/bounded.sh`, of een functie in de Python-module; zie
   *Aan de bouwer*): wikkelt elke gitleaks-aanroep in de scope.
4. **`.gitleaks.toml`**: een allowlist op de beta-headernaam, op `match` en op
   `generic-api-key`, zoals de bestaande `row.TriggerKey`-regel.
5. **`host.toml`**: twee nieuwe optionele sleutels (hieronder).
6. **`docs/secret-scan.md`**: nieuwe Nederlandse topicpagina met wat de scan doet, de meting van
   AC 3 en de opdracht om hem begrensd en volledig te draaien.

### Blobs bepalen

De te scannen set voor een push naar `<remote>` met tips `T…`:

```
git rev-list --objects T… --not --remotes=<remote>  →  blobs (filter op type via cat-file --batch-check)
minus de blobs in de ledger
```

Bij `rev-list --objects` staat het pad naast de blob-id. Dat pad, met de ref die de blob bereikt,
gaat in de opzoektabel voor de melding. Een blob die in meerdere refs zit, wordt één keer gescand;
een hit erin maakt al die refs vuil.

Open voor de bouwer om te meten: of `--not --remotes=<remote>` voor `refs/entire/*` iets uitsluit.
Een push met refspec `refs/entire/…:refs/entire/…` maakt mogelijk geen remote-tracking ref.
Zonder uitsluiting vangt de ledger het op: wat al gepusht is, is eerder schoon bevonden.

### De ledger

- Plek: `$(git rev-parse --path-format=absolute --git-common-dir)/lighthouse/secret-scan/`.
  Gedeeld door alle worktrees, nooit in een commit, per clone.
- Bestand per sleutel: `clean-<sleutel>.txt`, gesorteerde blob-ids, één per regel. De sleutel is
  de sha256 van de inhoud van `.gitleaks.toml` plus de uitvoer van `gitleaks version`.
  Verandert een van beide, dan begint een nieuwe ledger: een volledige herscan van ≈ 0,61 GB.
- Alleen **schone** blobs gaan erin, en alleen nadat de batch met rc 0 of rc 1 (findings)
  eindigde. Bij rc 1 gaan de blobs zonder finding erin; de blobs met een finding niet.
- Schrijven: na elke batch, atomair (tijdelijk bestand plus `rename`). Een kill halverwege kost
  hooguit de lopende batch.
- `--full` op beide subcommando's leest de ledger niet, maar schrijft hem wel bij. Dat is de
  expliciete volledige herscan, ook voor de meting van AC 3. Na `push --full` vindt de hook
  daardoor alles in de ledger en blokkeert hij niet op de volumegrens. Er komt geen omweg van
  `push` langs de hook.

### Batches

- De blobs worden in volgorde van grootte in batches van ≈ 64 MB gezet. Een blob groter dan de
  batch krijgt een eigen batch; de grootste nu is 20 MB.
- Per batch: blobs met `git cat-file --batch` naar een tijdelijke map, één bestand per blob met
  de blob-id als naam. Daarna `gitleaks dir <map> --config .gitleaks.toml --no-banner --redact
  -f json -r <rapport> --exit-code 1`, in de begrenzing. Na de batch gaat de map weg.
- Tussenstand naar stderr per batch: `secret-scan: batch 3/12, 64 MB, 0 findings`.
- **Findings-plafond.** gitleaks heeft geen `--max-findings`. De batchgrootte begrenst de
  findings per run, en daarmee het geheugen. Na elke batch telt de module de findings per regel.
  Boven 200 findings van één regel in één batch meldt hij:
  `secret-scan: <n> findings for rule <regel> in one batch; an allowlist is probably missing
  for this pattern (sample: <redacted match>)`.
  - In `scan` (de hook) blokkeert dat, zoals elke finding.
  - In `push` stopt de scan niet. De blobs met een finding blijven uit de ledger, hun refs gaan
    naar `held.txt`, en de melding staat bovenaan het eindoverzicht. De schone blobs van die
    batch gaan wel in de ledger. Zo komt doel 3 niet in gevaar door een patroon dat nog
    beoordeeld moet worden, zoals de 6928× `turnAttributionKey`.
  - Het getal 200 is mijn keuze en een constante.

### Begrenzing

Hetzelfde patroon als de geheugenrem van `pack/core/hooks/commit-test-gate.sh`: dezelfde ladder
van redenen, dezelfde `XDG_RUNTIME_DIR`-reparatie voor VS Code, een eigen melding als hij niet
beschikbaar is.

```
systemd-run --user --scope --quiet [--slice=<host.gate_slice>] \
  -p MemoryMax=<host.scan_memory_max> -p MemorySwapMax=0 -p CPUQuota=<host.scan_cpu_quota> \
  -- env GOMAXPROCS=<n> choom -n 800 -- nice -n 10 gitleaks …
```

- `MemorySwapMax=0`: de scan wordt gekild in plaats van de host in swap te duwen. Dat was de
  schade op 2026-11-01.
- Nieuwe sleutels in `[host]` van `host.toml`, beide optioneel:
  - `scan_memory_max`, standaard `"2G"`;
  - `scan_cpu_quota`, standaard `"200%"`. `GOMAXPROCS` volgt daaruit (200 % → 2).
  Ze komen in `HOST_KEYS`, `_OPTIONAL_HOST_KEYS`, de `Host`-dataclass, de validator en de
  sleutelkaart van `config.py`, met tests. De bron staat onder `pack/core/`; de geïnstalleerde
  kopie in `.lighthouse/lib/` komt uit `go run ./cmd/lighthouse init --upgrade` (Dogfood) en
  wordt meegecommit.
- `--slice=<host.gate_slice>` alleen als die sleutel gezet is. De letterlijke slicenaam komt
  niet in code of docs (`ci/literal-denylist.txt`).
- **Zonder systemd** (geen `systemd-run`, geen user-bus, geen `choom`): de scan draait zonder
  scope maar wel in batches, met `GOMAXPROCS` en `nice`. De uitvoer zegt het:
  `secret-scan: no memory ceiling: <reden>`.
- **Plafond geraakt.** Een batch die eindigt op rc 137 of SIGKILL, of waarbij `oom_kill` in
  `memory.events` van de scope opliep, geeft `secret-scan: ABORTED at memory ceiling
  (<scan_memory_max>) in batch <i>; raise host.scan_memory_max or lower the batch size`. De hook
  blokkeert dan, en de batch gaat niet in de ledger. Welk van die signalen betrouwbaar is na het
  einde van een `--scope`, meet de bouwer (zie *Aan de bouwer*).
- Ook de `gitleaks git`-aanroep voor gewone refs krijgt deze begrenzing.

### Pre-push-hook

- Entire-refs: `checkpoint_scan.py scan`. Is het ongescande volume groter dan 256 MB, dan scant
  de hook niet en blokkeert hij:
  `pre-push: BLOCKED — <n> checkpoint blobs (<mb> MB) are not yet scanned. A first push after a
  bulk import ('entire import') is heavy; run: python3 tools/hooks/checkpoint_scan.py push <remote>`.
  Dat is het raakpunt van AC 4 (zie *Afwijkingen van de AC*).
- Onder die grens scant de hook zelf. Eén vuile ref blokkeert de hele push (een hook kan niet
  half doorlaten), met per hit de ref, het pad, de regel en de geredigeerde match.
- De exitcodes van gitleaks worden niet langer op één hoop gegooid: rc 1 = findings, plafond =
  eigen melding, elke andere rc = `COULD NOT CHECK — gitleaks exited <rc>`.

### Push-opdracht

`python3 tools/hooks/checkpoint_scan.py push <remote>`:

1. Controleert dat `<remote>` de checkpoint-remote is, met dezelfde regel als de hook.
2. Scant alle `refs/entire/*` die de remote mist, begrensd en in batches, en vult de ledger.
3. Schrijft de vuile refs naar `<git-common-dir>/lighthouse/secret-scan/held.txt`: per ref de
   paden, de regels en de geredigeerde matches.
4. Pusht de schone refs met `git push <remote> <ref>:<ref>…`, in brokken van ≤ 500 refs per
   aanroep (lengte van de opdrachtregel). Elke push gaat door de pre-push-hook. Die vindt alles
   in de ledger en is dus snel.
5. Eindigt met: `<n> pushed, <m> held — see <pad naar held.txt>`. Exit 0 als er niets vuil is,
   anders 1.

Een vuile checkpoint vrijgeven gaat zoals nu: de match is vals, dan komt hij op `match` in
`.gitleaks.toml`; de match is echt, dan wordt het secret geroteerd en de checkpoint gedropt.

### Taal van de meldingen

Hookmeldingen zijn Engels (AGENTS.md: instructies en hookmeldingen in het Engels). De
Nederlandse zinnen uit het issue worden dus `ABORTED at memory ceiling` en `an allowlist is
probably missing for this pattern`. `docs/secret-scan.md` is Nederlands.

## Afwijkingen van de AC

- **AC 4.** `entire import` is een binary van een ander. Hij schrijft refs grotendeels via
  go-git, dus een git-hook als `reference-transaction` vuurt niet betrouwbaar. De melding komt
  daarom op het eerste raakpunt na de import: de pre-push-hook en de push-opdracht. Die melden
  het ongescande volume en wijzen naar de begrensde variant. De AC-tekst wordt daarop aangepast.
- **AC 2.** De melding is Engels (zie hierboven).

## Tests

- **AC 1.** Een fixture met ≥ 500 checkpoint-refs, gemaakt met één `git fast-import` (niet 500
  keer `commit-tree`), met een groeiend `full.jsonl` zoals entire het maakt. Een nep-`gitleaks`
  op `PATH` logt welke bestanden hij per batch kreeg. De eerste scan raakt alle unieke blobs. Na
  5 nieuwe refs raakt een tweede scan alleen hun nieuwe blobs. Een wijziging aan `.gitleaks.toml`
  geeft weer een volledige scan.
- **Echte gitleaks**, klein: een nep-`ghp_`-token in één van drie checkpoints. `push` pusht er
  twee naar een lokale bare remote en zet de derde in `held.txt`.
- **Plafond.** Een nep-`systemd-run` die rc 137 teruggeeft, plus een proef met de echte
  `systemd-run` en `scan_memory_max = "8M"` (overgeslagen zonder user-bus). Beide geven de
  plafondmelding, de hook blokkeert, de ledger blijft leeg.
- **Findings-plafond.** Een nep-`gitleaks` die 201 findings op één regel schrijft.
- **Zonder systemd.** Een `PATH` zonder `systemd-run` geeft de melding `no memory ceiling` en
  scant toch.
- **Bestaande tests** in `tests/test_pre_push_secret_scan.py` blijven groen. Nieuwe tools die de
  hook vóór de gitleaks-check aanroept, komen in de `tool_bin`-lijsten.
- `config.py`: tests voor de twee nieuwe sleutels (gezet, ontbrekend, ongeldig).

## AC 3: de meting

Op de Beelink, na de allowlist en met de nieuwe module:

```
/usr/bin/time -v python3 tools/hooks/checkpoint_scan.py scan --full --remote <checkpoint-remote> $(git for-each-ref --format='%(objectname)' refs/entire/)
```

Piek-RSS per batch (uit `memory.peak` van de scope, of `time -v`), totale duur en aantal
findings gaan in `docs/secret-scan.md`, met de datum en het aantal refs en blobs.

De bouwer draait de meting zelf op de Beelink, begrensd, en daarna de echte eerste
`push <checkpoint-remote>` naar `CorneeldH/lighthouse-checkpoints` (gebruiker, 2026-11-01). De
vuile checkpoints blijven lokaal in `held.txt`, ter beoordeling door de gebruiker. AC 3 sluit zo
in de PR.

## Aan de bouwer

- Of het plafond betrouwbaar herkend wordt via rc 137, via `memory.events` van de scope-cgroup,
  of via beide. Ruimt systemd de scope-cgroup na afloop op voordat je hem kunt lezen? Meten,
  dan kiezen.
- Of de begrenzing een bash-helper wordt die hook en module delen, of in Python zit en de hook
  hem aanroept. Voorkeur: één plek.
- Hoe `held.txt` precies opgemaakt is. Leesbaar voor een mens, één blok per ref.
- De batchgrootte (64 MB), de volumegrens van de hook (256 MB) en het findings-plafond (200)
  zijn constanten. Verplaats ze naar config alleen als de meting van AC 3 daar reden voor geeft.
- `changelog.d/issue-198-…md`, `type: internal`.
