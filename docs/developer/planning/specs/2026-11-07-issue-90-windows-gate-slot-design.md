# Test gate op native Windows: het gate-slot in deadline.py (#90)

Issue: [#90](https://github.com/CorneeldH/lighthouse/issues/90), deelwerk na #266. Milestone:
Product: eerste versie voor anderen. Branch `issue-90-windows-gate-slot`, PR met `Refs #90`.

## Waarom

Sinds #266 laden de hooks op native Windows (Claude Code voor Windows, hooks in Git Bash). In
`windows-measure` slagen 10 van de 11 stappen. Alleen een commit met een `[test_gate] command`
faalt (stap 11, run
[42374756923](https://github.com/CorneeldH/lighthouse/actions/runs/42374756923)):

```text
commit-test-gate: COULD NOT CHECK — cannot take a gate slot: lock dir /tmp/lighthouse-gate-slots-197108 cannot be created (mkdir: cannot change permissions of …
```

`commit-test-gate` blokkeert daarmee elke commit met een test gate. Zonder test gate is lighthouse
op Windows maar half bruikbaar. De tweede gebruiker van M3 heeft een Windows-laptop (comment op
#90, M3-spec §7).

Het slot zelf werkt op Windows niet, om drie redenen:

1. `mkdir -p -m 700` faalt in Git Bash (`pack/core/hooks/commit-test-gate.sh:400`).
2. Op POSIX hangt een `flock` aan de open file description. Het slot blijft dus staan nadat
   `gate_slot.py` stopt, zolang de hook en `deadline.py` het fd open houden. Een
   `msvcrt.locking`-slot hoort bij het proces en verdwijnt zodra `gate_slot.py` stopt. Daarom
   weigert `gate_slot.py` op Windows (`pack/core/lib/gate_slot.py:62-66`,
   `filelock.HELD_PAST_EXIT`).
3. Niet gemeten: of een native `python3.exe` de fd's 3-9 erft die Git Bash opende. Git Bash geeft
   aan Windows-programma's waarschijnlijk alleen stdin, stdout en stderr door. Dan werkt het
   fd-ontwerp op Windows hoe dan ook niet.

## Doelen

1. Een commit met een `[test_gate] command` wordt op native Windows getest. Rood blokkeert, groen
   gaat door, net als op Linux.
2. De grens op het aantal gelijktijdige suites (`[test_gate] slots`, `[host] gate_slots`) geldt
   ook op Windows.
3. Linux en macOS gedragen zich precies als nu.

## Niet-doelen

- De geheugenrem en `systemd-run` op Windows. Ze lezen `/proc` en blijven Linux-only.
- De SQLite-URI in `internal/store` en de andere Go-tests die op Windows falen (controller,
  `serve`; buiten deze milestone).
- De quoting van hookcommando's in e2e `TestFreshRepoBlocksCommitOnMain` op Windows.
- Docs over de Windows-route (startgids, agentgids, site): dat volgt na de meting op een echte
  machine (AC 3 van #90).

## Anti-doelen

- Een slot dat vrij lijkt terwijl de suite draait. Geen slot is beter dan een vals slot (de regel
  achter de huidige weigering).
- Een commit die door een fout in de slotlogica ongetest doorgaat. Elke fout blijft `COULD NOT
  CHECK` met exit 2.

## Aanpakken

| Aanpak | Wat | Afweging |
|---|---|---|
| **A. `deadline.py` houdt het slot** (gekozen) | Op Windows opent `deadline.py` het lockbestand zelf per pad en houdt het slot vast zolang de suite draait. | Het slot leeft precies zo lang als de suite, en er gaan geen fd's van bash naar Python. Wel een tweede tak in de hook en in `deadline.py`. |
| B. Geen slot op Windows | De gate slaat de slots op Windows over, met een melding. | Klein, maar suites van meerdere sessies op één laptop lopen dan tegelijk. Dat haalt doel 2 niet. |
| C. Lockbestand met PID | Het slot is een bestand met het PID van de eigenaar; een slot van een dood proces telt als vrij. | Werkt zonder vasthoudend proces, maar het opsporen van verlopen sloten is foutgevoelig: Windows- tegenover MSYS-PID's, en hergebruikte PID's. |

De gebruiker koos A.

## Ontwerp

### Twee takken in de hook

`commit-test-gate.sh` kiest de tak vóór het slotblok (nu regels 390-448):

- **fd-tak** (Linux, macOS; ongewijzigd): `mkdir -p -m 700`, fd's 3-9, `gate_slot.py`, het slot
  vóór de geheugenrem en over de hele lus van roots.
- **procestak** (Windows): bash opent geen fd's en roept `gate_slot.py` niet aan. Het slot neemt
  `deadline.py` per aanroep.

De procestak geldt als `uname -s` begint met `MINGW`, `MSYS` of `CYGWIN`, of als
`LIGHTHOUSE_GATE_SLOT_MODE=process` gezet is. Die variabele is alleen voor tests, net als
`LIGHTHOUSE_GATE_MEMINFO`. Python kiest zelf het soort slot (`flock` of `msvcrt`, via
`filelock.py`). Ziet de hook een Cygwin-Python met `fcntl` aan voor Windows, dan neemt
`deadline.py` een `flock` per pad, en dat is ook een echt slot.

### De lockmap in de procestak

- `SLOT_DIR` wordt bepaald zoals nu (`LIGHTHOUSE_GATE_SLOT_DIR`, `/run/user/$UID/…`,
  `/tmp/lighthouse-gate-slots-$UID`).
- `mkdir -p` zonder `-m 700`. Op NTFS via MSYS zeggen modes niets; de map staat onder het profiel
  van de gebruiker.
- Het pad gaat als **argument** naar `deadline.py`, niet als omgevingsvariabele. Git Bash zet
  argumenten die op een POSIX-pad lijken om naar een Windows-pad voor een native programma;
  omgevingsvariabelen niet.

### `deadline.py` neemt het slot

Nieuwe opties vóór de `--`, alleen samen geldig:

```text
deadline.py <epoch> [--slot-dir D --slots N --slot-wait W --slot-report F] -- <cmd> [args...]
```

Gedrag met deze opties:

1. Open `D/slot-1` … `D/slot-N` (append, aanmaken als ze ontbreken) en probeer ze op volgorde met
   `filelock.try_lock`. Elke seconde een ronde, tot `W` seconden voorbij zijn; de logica van
   `gate_slot.acquire`.
2. Wordt de deadline eerder bereikt dan `W`, dan telt de deadline: de wachttijd telt mee voor de
   deadline, zoals nu.
3. **Slot gepakt:** schrijf `<n>\n` naar `F`, sluit de fd's van de andere slots, en draai het
   commando zoals nu (`run_until`). Het fd van het slot blijft open tot `deadline.py` eindigt.
   Daarna gaat het slot vrij, op POSIX en op Windows.
4. **Alles bezet na `W`:** schrijf `busy\n` naar `F`, start het commando niet en eindig met exit 75.
5. **Fout** (map of bestand niet te openen, `OSError` bij het slot): schrijf
   `error: <oorzaak>\n` naar `F`, start het commando niet en eindig met exit 1.

`F` is de bron van waarheid, niet de exitcode: een suite kan zelf met 75 of 1 eindigen.

Het slot hangt in de procestak aan het proces `deadline.py`. Wordt de hook gestopt (timeout van
Claude Code), dan leeft `deadline.py` door tot zijn deadline plus grace, en houdt het slot zo
lang vast. Dat is hetzelfde gedrag als de fd-tak nu, waar `deadline.py` het fd erft.

### De hook leest `F`

Per root, na de aanroep van `deadline.py`, in de procestak:

| Inhoud van `F` | Hook |
|---|---|
| een getal `1..N` | het slot is gepakt; de uitslag van de suite telt zoals nu |
| `busy` | dezelfde melding als nu bij exit 75 van `gate_slot.py` (`all gate slots busy after … s`), exit 2 |
| `error: …` | `slot_failed "<oorzaak>"`, exit 2 |
| leeg, ontbreekt of iets anders | `slot_failed "deadline.py gave no slot report"`, exit 2 |

`F` staat in `GATE_TMP`, per root een nieuw bestand.

### Slot per root

In de procestak neemt elke aanroep van `deadline.py` zijn eigen slot. Bij een commit die meerdere
roots raakt, gaat het slot tussen twee roots even vrij. Een andere gate kan het dan pakken, en de
volgende root wacht. De gebruiker vond dat acceptabel; de meeste commits raken één root. De fd-tak
houdt het slot over de hele lus, zoals nu.

### `gate_slot.py` en `filelock.py`

- `gate_slot.py` blijft de fd-tak bedienen. `acquire` verhuist niet, maar `deadline.py` gebruikt
  dezelfde functie (import uit `lighthouse_lib.gate_slot`) op fd's die het zelf opende. Zo bestaat
  de wachtlogica één keer.
- De weigering op Windows in `gate_slot.main` blijft: in de fd-tak blijft een `msvcrt`-slot vals.
  De melding wijst naar de procestak.
- `filelock.py` verandert niet.

## Bewijs

| Wat | Waar | Bewijst |
|---|---|---|
| procestak op Linux, echte `flock` per pad | `tests/pack/test_commit_test_gate.py` met `LIGHTHOUSE_GATE_SLOT_MODE=process`: groen, rood, `busy` (alle slots vast door een ander proces), map niet te maken, geen rapport | de hook leest `F` goed en blokkeert bij elke fout |
| slot vast tot `deadline.py` eindigt | `tests/pack/test_deadline.py`: een tweede proces krijgt het slot niet zolang het commando draait, wel daarna | het slot leeft zo lang als de suite |
| `msvcrt`-kant | `tests/pack/test_deadline.py` met de `FakeMsvcrt` uit `tests/pack/test_filelock.py` | de opties werken zonder `fcntl` |
| fd-tak ongewijzigd | de bestaande tests in `test_commit_test_gate.py` en `test_gate_slot.py` blijven groen zonder aanpassing | doel 3 |
| echt Windows | `windows-measure` stap 11 (`git --version` als `[test_gate] command`) `pass`; e2e `TestRedTestBlocksCommit`, `TestRedCommitComesBackAfter`, `TestDefaultCommandWithoutPytest` op `windows-latest` | doel 1 op de echte host |

## Aan de bouwer

- Hoe de hook de procestak in de lus van roots inpast, zolang de fd-tak er hetzelfde uitziet.
- De precieze tekst van de meldingen, zolang `busy` en `slot_failed` dezelfde zinnen geven als nu.
- Of `deadline.main` de opties met `argparse` of met de hand leest; `USAGE` wordt bijgewerkt.
- Dogfood: `go run ./cmd/lighthouse init --upgrade` na de wijziging in `pack/`, plus een fragment
  in `changelog.d/`. `docs/test-gate.md` krijgt een alinea over de procestak.
