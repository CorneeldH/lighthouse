# De test-gate op de host

De test-gate (`commit-test-gate`, via `run.sh commit-test-gate pre|post`) draait
`[test_gate] command` uit `lighthouse.toml` bij een commit. Hoeveel gates tegelijk draaien en
hoeveel geheugen ze krijgen, is een eigenschap van de machine, niet van het project. Daarom staan
die grenzen in `[host]` van `host.toml`, en het project vraagt alleen.

## Sleutels in `[host]`

Alle optioneel.

| Sleutel | Standaard | Betekenis |
|---|---|---|
| `gate_slice` | niet gezet | de systemd-slice met `MemoryHigh`/`MemoryMax` waarin de gate draait; zonder slice draait hij zonder geheugenrem, met een melding |
| `gate_slots` | `7` | het plafond op het aantal gates tegelijk op deze host, voor alle projecten samen; hoger dan 7 blokkeert |
| `gate_min_mem_available_gb` | niet gezet | minimale `MemAvailable` in GiB voordat de gate een run start |
| `gate_min_swap_free_gb` | niet gezet | minimale `SwapFree` in GiB voordat de gate een run start |

**Slots.** Een project vraagt met `[test_gate] slots` (standaard 2) om een aantal slots. De slots
zijn locks in één map per gebruiker (`/run/user/<uid>/lighthouse-gate-slots`), dus alle projecten
op de host delen ze. De gate neemt het minimum van `test_gate.slots` en `gate_slots`. Vraagt het
project meer dan de host toestaat, dan blokkeert de gate niet: hij meldt
`commit-test-gate: test_gate.slots = 3 above host gate_slots = 2, the gate uses 2` en gaat door.

**Geheugen.** De drempels zijn in GiB (1024³ bytes), hoewel de sleutels op `_gb` eindigen; zo
ook `dispatch.min_mem_available_gb`. De gate meet ze in `/proc/meminfo`, dus alleen op Linux. Zet
ze niet op een host zonder `/proc/meminfo` (macOS): daar blokkeert elke commit met
`host memory not measurable`. Elke drempel controleert alleen zijn eigen waarde. Op een host zonder swap
(`SwapTotal: 0 kB`) vervalt de swapdrempel met de melding
`commit-test-gate: no swap on this host, SwapFree not checked`.

## Volgorde en wachten

1. De gate neemt een slot (wachten tot `[test_gate] slot_wait_seconds`).
2. Met een geheugendrempel leest hij `/proc/meminfo` en wacht tot de drempels gehaald zijn. Hij
   houdt zijn slot vast terwijl hij wacht, zodat niet alle wachtende gates tegelijk door dezelfde
   opening glippen. Het wachtbudget is `slot_wait_seconds`, maar nooit voorbij de ene deadline
   (`[test_gate] deadline_seconds`) waar ook de slotwacht al van afging.
3. Daarna start de run.

## De importcheck

Vóór het testcommando draait de gate `lighthouse imports check --files …` op de bestanden van de
commit, in elke root met geïnstalleerde packs (een root zonder `.lighthouse/manifest.toml` slaat
hij over). Deze stap hangt niet af van `[test_gate] command`: ook met `command = ""` draait hij,
en de melding `no test command configured, nothing was tested` blijft er naast staan. Uit gaat hij
alleen met `[imports] check = false`. De gate draait de binary waarmee hij zelf draait
(`os.Executable()`), niet `lighthouse` van de `PATH`.

- Een bevinding (`<bestand>:<regel>: <ID> <import> — <reden>`) blokkeert met
  `imports not declared in a manifest`, een andere melding dan `tests failed`.
- Kan de check niet draaien (exit 3, exit 1 zonder bevindingsregel, een binary zonder `imports`),
  dan blokkeert de gate met `COULD NOT CHECK`.
- Bestanden zijn bij `pre` de gestagede (`git diff --cached`), bij `post` die van de commit.
  Verwijderingen tellen niet mee; een lege lijst slaat de stap over.

**Bekende beperking.** Draait `git commit -a` of `git add -A && git commit` in één Bash-aanroep,
dan is er bij `pre` nog niets gestaged: de lijst is leeg en de stap meldt
`commit-test-gate: imports not checked: nothing staged yet`. Er is geen workaround; stage vóór
de commit in een aparte aanroep om de check te krijgen.

## Meldingen

- `COULD NOT CHECK — host memory below threshold after <n> s (MemAvailable 0.7 GiB < 4 GiB; SwapFree 0.5 GiB < 2 GiB)`:
  na het wachtbudget nog te weinig geheugen; er is niets getest.
- `COULD NOT CHECK — host memory not measurable: <reden>`: een drempel is gezet, maar
  `/proc/meminfo` is onleesbaar of heeft geen `MemAvailable:`. Niet kunnen meten is nooit een pass.
- `commit-test-gate: no gate memory threshold in host.toml, host memory not checked`: geen van
  beide drempels gezet; de gate test zoals voorheen.

## Voorbeeld: een thuisserver

```toml
[host]
name = "thuisserver"            # verplicht in elke host.toml
controller = true               # optioneel; true: dit is de controller-host, het adres wordt niet gemeten
tailscale_ip = "100.64.0.1"     # optioneel; zonder controller en zonder deze sleutel is dit voor host-check een andere host
gate_slice = "gates.slice"
gate_slots = 2
gate_min_mem_available_gb = 6
gate_min_swap_free_gb = 4
```

Een host zonder Tailscale laat `tailscale_ip` weg en zet `controller = true` als hij de controller-host is; `controller = false` zegt het omgekeerde. Een oudere lezer negeert `controller` en valt terug op `tailscale_ip`. Zet eerst elke repo op de host op een release met
deze wijziging (zie [getting-started](getting-started.md#a-hosttoml-without-tailscale)).

`gates.slice` heeft `MemoryHigh=7G` en één gate piekt op 2,3 GB, dus passen er twee slots in; de
drempels laten de host ruimte voor de vaste bewoners naast de gates.

## Tijdelijke bestanden van pytest

De gate geeft elke run een eigen map in `LIGHTHOUSE_GATE_TMP` en ruimt die op als de hook eindigt,
ook na een deadline-kill. Pytest schrijft zijn `tmp_path` anders naar `/tmp/pytest-of-<gebruiker>`,
en dat blijft staan. Geef daarom `--basetemp` mee in `[test_gate] command`:

```toml
[test_gate]
command = 'uv run pytest -q --basetemp="$LIGHTHOUSE_GATE_TMP"'
```
