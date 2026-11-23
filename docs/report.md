# lighthouse report

`lighthouse report` maakt het verslag van één repo over één dag, week of maand. Wie met agents
werkt, verliest het overzicht: sessies en worktrees lopen door elkaar, PR's worden geopend en
gemerged, issues gaan dicht. Het verslag zet dat uit de forge op een rij, elk item met link, en
beantwoordt twee vragen: wat is er gedaan, en wat is er gaande. Het ontwerp staat in
[de spec](developer/planning/specs/2026-11-16-issue-44-verslag-design.md).

## Aanzetten

Het verslag staat standaard uit. Een repo zet het aan in zijn `lighthouse.toml`:

```toml
[report]
enabled = true
sink = "file"
dir = "docs/verslagen"
```

De eigen configuratie van lighthouse staat in `lighthouse.toml` en schrijft naar [het procesverslag](developer/verslag/README.md).

| Sleutel | Type | Default | Betekenis |
|---|---|---|---|
| `report.enabled` | bool | `false` | Zet het verslag aan. |
| `report.sink` | str | `"stdout"` | `stdout` of `file`. |
| `report.dir` | str | geen | Map voor `file`, relatief aan de repo-root en binnen de repo. Verplicht bij `sink = "file"`. |

Staat `enabled` niet op `true`, dan zegt het commando op stderr in één regel dat het verslag uit
staat en hoe je het aanzet, en stopt met exit 0. Er volgt geen forge-call, en `[forge]` hoeft dan
niet te bestaan.

Een fout in `[report]` (een verkeerd type, een onbekende `sink`, `file` zonder `dir`, een `dir` die
absoluut is of buiten de repo valt) laat de repo voor `tick`, `serve`, `run` en `triage` gewoon
doorlopen. Alleen `lighthouse report` stopt erop, met de sleutelnaam in de melding en exit 1.

## Aanroep

```text
lighthouse report day|week|month [--date JJJJ-MM-DD] [--repo <pad>]
```

- Het eerste argument is verplicht en kiest de periode.
- `--date` kiest de periode waar die datum in valt. Zonder `--date` geldt de lopende periode:
  vandaag, deze week, deze maand. Wie maandag de vorige week wil, geeft een datum in die week mee.
- `--repo` is het pad van de repo. Zonder `--repo` geldt de repo-root: eerst
  `LIGHTHOUSE_REPO_ROOT`, anders `git rev-parse --show-toplevel`. In een worktree is dat de
  worktree. De `lighthouse.toml` en de map van `sink = "file"` komen van daar.
- Het token komt uit `GH_TOKEN` of `GITHUB_TOKEN`, met als terugval `gh auth token`.
- Exit 0: het verslag is geschreven, of het staat uit. Exit 1: een fout in de config of van de
  forge. Exit 2: verkeerd gebruik, zoals een onbekende periode of een datum die geen
  `JJJJ-MM-DD` is.

## Periodes en labels

Alles in de lokale tijdzone van de host, met halfopen intervallen `[van, tot)`.

| Periode | Loopt van | Label |
|---|---|---|
| `day` | 00:00 van de dag tot 00:00 van de volgende dag | `2026-11-16` |
| `week` | ISO-week, maandag 00:00 tot de maandag erna | `2026-W47` |
| `month` | de 1e om 00:00 tot de 1e van de volgende maand | `2026-11` |

Het weeklabel gebruikt het ISO-jaar: 30 december 2024 valt in `2025-W01`. Bij een wisseling van
zomer- en wintertijd duurt een dag 23 of 25 uur; de grenzen komen uit de kalender, niet uit een
optelling van 24 uur.

## Bestemming

- **`stdout`** (de default) schrijft het verslag naar de standaarduitvoer.
- **`file`** schrijft naar `<dir>/<periode>/<label>.md`, bijvoorbeeld
  `docs/verslagen/week/2026-W47.md`. `<periode>` is letterlijk het argument (`day`, `week`,
  `month`), niet vertaald. Een bestaand bestand wordt overschreven: hetzelfde verslag later
  opnieuw maken geeft de nieuwere stand. Het schrijven gaat via een tijdelijk bestand, dus een
  lezer ziet nooit een half verslag.

Het commando commit niets. Wie het bestand in de repo wil, doet dat zelf. Faalt een bron van de
forge, dan komt er een fout en exit 1, en raakt het commando de bestemming niet aan: er is geen half
verslag en geen stille lege lijst.

## Inhoud

Het verslag staat in `[project] language`: `nl` en `en` zijn er, elke andere taal valt terug op
`en`. Een lege sectie blijft staan met één regel (`Geen.` in `nl`, `None.` in `en`).

**Gedaan**

- *Gemergde PR's*: de PR's die in de periode zijn gemerged, op tijdstip van mergen.
- *Gesloten issues*: de issues die in de periode zijn gesloten, op tijdstip van sluiten. Alleen
  een sluiting als `completed` (of zonder reden) telt als gedaan; elke andere reden (`not_planned`,
  `duplicate`) valt weg.

**Gaande**

- *Open PR's*: alle PR's die nu open staan, op nummer, elk met een status. Dit is een
  momentopname en niet aan de periode gebonden; de kop noemt daarom de stand met tijdstip, ook bij
  een `--date` in het verleden. De status is de eerste die past: `concept` (draft), `conflict`,
  `rood` (checks `FAILURE` of `ERROR`), `bezig` (`PENDING` of `EXPECTED`), `groen`
  (`SUCCESS`), `geen checks`.
- *Nieuw op de roadmap*: de issues die in de periode aan een milestone zijn gehangen, per
  milestone (op titel), met de issues op nummer. Alleen issues die nu nog open zijn en nog in die
  milestone zitten tellen, zodat een issue nooit tegelijk onder Gedaan en Gaande staat.

Bewust weggelaten: lokale worktrees, issues met een `needs-*`-label en wat er misging. Hook-uitkomsten
worden nergens gelogd.

## Grenzen

- **Een hernoemde milestone matcht niet.** Het event van de forge draagt alleen de titel van de
  milestone. Veranderde die in de periode van naam, dan hoort het issue niet onder *Nieuw op de
  roadmap*. Dat is een bekende rand, geen fout.
- **Search heeft een plafond.** De search-API geeft hoogstens 1000 resultaten per vraag. Is er
  meer, of meldt de forge een onvolledig antwoord, dan faalt het commando in plaats van af te
  kappen.
- **Alleen GitHub** is geïmplementeerd. Het commando leest uitsluitend via de `Forge`-interface,
  dus een andere forge kan dezelfde vier methodes later invullen.
