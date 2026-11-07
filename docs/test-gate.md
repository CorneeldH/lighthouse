# De test-gate op de host

De test-gate (`commit-test-gate.sh`) draait `[test_gate] command` uit `lighthouse.toml` bij een
commit. Hoeveel gates tegelijk draaien en hoeveel geheugen ze krijgen, is een eigenschap van de
machine, niet van het project. Daarom staan die grenzen in `[host]` van `host.toml`, en het project
vraagt alleen.

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
name = "thuisserver"                # verplicht in elke host.toml
tailscale_ip = "100.64.0.1"     # verplicht in elke host.toml
gate_slice = "gates.slice"
gate_slots = 2
gate_min_mem_available_gb = 6
gate_min_swap_free_gb = 4
```

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
