# Issue #44 — Verslag per dag, week of maand als subcommando van lighthouse

Issue: [#44](https://github.com/CorneeldH/lighthouse/issues/44). Wacht op dit issue:
[#128](https://github.com/CorneeldH/lighthouse/issues/128) (het verslag van lighthouse zelf). Bouwt
op [Lighthouse zonder Python](2026-11-07-zonder-python-design.md): alles wat lighthouse doet, zit
in de binary.

## Waarom

Wie met agents werkt, verliest in de loop van een dag het overzicht. Er lopen sessies en
worktrees tegelijk, agents openen en mergen PR's, issues gaan dicht. Aan het eind van de dag, week
of maand is niet meer te zien wat er gedaan is en wat er gaande is. Lighthouse maakt dat overzicht
uit de forge: een verslag per periode, optioneel per repo.

## Doelen

- `lighthouse report day|week|month [--date JJJJ-MM-DD]` maakt een verslag van één repo over één
  periode.
- Het verslag beantwoordt twee vragen, elk item met link:
  - **Gedaan:** gemergde PR's en gesloten issues in de periode.
  - **Gaande:** de open PR's met hun status, en de issues die in de periode aan een milestone zijn
    gehangen (nieuw op de roadmap), per milestone.
- Een repo zet het verslag aan met `[report] enabled = true` in `lighthouse.toml`. Zonder die
  sleutel doet het commando niets.
- Het verslag staat in `[project] language`.
- Geen repo-naam, labelnaam of pad als literal in de code: alles komt uit `lighthouse.toml`.
- De generator is vanaf het begin forge-neutraal: hij leest alleen via de `Forge`-interface.

## Niet-doelen

Dit zijn latere stappen, elk een eigen vervolgissue:

- **Andere bestemmingen.** Een wiki-pagina (forge-neutraal via `git push` naar `<repo>.wiki.git`)
  en een GitHub Discussion (alleen GitHub). Deze spec levert `stdout` en `file`.
- **Over repo's heen.** Eén verslag over alle repo's uit `host.toml`.
- **Een timer** die het verslag aan het eind van de periode maakt.
- **Een pagina in de UI** van `lighthouse serve`.
- **GitLab of Forgejo.** De interface is er klaar voor; alleen GitHub wordt geïmplementeerd.

Bewust weggelaten uit de inhoud, op verzoek van de gebruiker: lokale worktrees, issues met een
`needs-*`-label en "wat ging er mis" (hook-uitkomsten, rode test-gate-runs). Hook-uitkomsten worden
nergens gelogd, en lokale worktrees zeggen te weinig.

## Anti-doelen

- Geen nieuw `.py`-bestand. Het commando is Go, in de binary.
- Geen `gh`-aanroep naast de `Forge`-interface: dat breekt de forge-neutraliteit.
- Geen dagelijkse commit of PR op de default branch voor het verslag zelf.
- Geen stille lege lijst als de forge faalt: dan een fout en exit ≠ 0.

## Aanpakken

- **A. Getypte periode-queries op `Forge` (gekozen).** Drie nieuwe methodes met een periode en één
  voor de open PR's met status. Elke vraag is apart te testen met een fake, en een tweede forge
  implementeert later dezelfde methodes.
- **B. Eén generieke `Activity(since, until)`-stroom van events** die het verslag zelf in secties
  sorteert. Eén methode minder, maar de betekenis zit dan in een event-mapping per forge, en die
  verschilt per forge sterk. Lastiger te testen. Afgevallen.
- **C. `gh` direct aanroepen** buiten de interface. Afgevallen: niet forge-neutraal.

De gebruiker koos A.

## Ontwerp

### 1. Configuratie

Nieuwe tabel `[report]` in `lighthouse.toml`:

| Sleutel | Type | Default | Betekenis |
|---|---|---|---|
| `report.enabled` | bool | `false` | Zet het verslag aan. |
| `report.sink` | str | `"stdout"` | `stdout` of `file`. |
| `report.dir` | str | geen | Map voor `file`, relatief aan de repo-root. Verplicht bij `sink = "file"`. |

Bij `sink = "file"` schrijft het commando naar `<dir>/<periode>/<label>.md`. `<periode>` is
letterlijk het argument (`day`, `week`, `month`), niet vertaald: het is een pad, geen tekst. Als label
`2026-11-16` (dag), `2026-W47` (week) of `2026-11` (maand). Een bestaand bestand wordt
overschreven: een verslag van dezelfde periode later opnieuw maken geeft de nieuwere stand.
Het commando commit niets.

De sleutels komen op elke plek waar een sleutel nu staat, zodat de pariteitstests groen blijven:

- `schema` en `defaults` in `internal/config/config.go`;
- de bestaande mirror `pack/core/lib/config.py`, alleen een wijziging en geen nieuw bestand;
- `docs/site/data/config.json`, als nieuwe tabel `report`;
- de fixtures onder `tests/pack/fixtures/config/` en `tests/pack/fixtures/config-schema.json`.

Validatie op twee plekken, elk met één regel:

- **De waarde van `report.sink`** (`stdout` of `file`) controleren `internal/config` en de mirror
  in `config.py`, net als `review.roborev`. Er komt een parity-fixture bij voor een foute waarde.
  Zo weigert `lighthouse config` dezelfde file die `lighthouse report` weigert.
- **`file` zonder `dir`** is een regel over twee sleutels. Die controleert alleen
  `internal/repoconfig`.

`internal/repoconfig` krijgt in `Repo` een veld `Report` (`Enabled`, `Sink`, `Dir`) en een veld
`ReportErr`, beide gevuld in `Load`. Net als `TriageErr` laat een fout in `[report]` `Load` niet
falen: `tick`, `serve`, `run` en `triage` delen `Load`, en een tikfout in `report.sink` mag een
repo daar niet uit halen. Alleen `lighthouse report` stopt op `ReportErr`, met de sleutelnaam in
de melding.

Omdat `pack/core/lib/config.py` onder `pack/` staat, vraagt dat de dogfood-stap:
`go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie committen.

### 2. Het commando

```text
lighthouse report day|week|month [--date JJJJ-MM-DD] [--repo <pad>]
```

- Het eerste argument is verplicht en kiest de periode.
- `--date` kiest de periode waar die datum in valt. Zonder `--date` geldt de lopende periode
  (vandaag, deze week, deze maand).
- `--repo` is het pad van de repo. Zonder `--repo` geldt de top van de werkmap
  (`config.RepoRoot()`, `git rev-parse --show-toplevel`). In een worktree is dat dus de worktree,
  niet de main-checkout. De `lighthouse.toml` en de map van `sink = "file"` komen van daar.
- Staat `[report] enabled` niet op `true`, dan meldt het commando één regel op stderr (in het
  Engels, het is een hook- of toolmelding) dat het verslag uit staat en hoe je het aanzet. Exit 0.
- Een fout van de forge of de config: melding op stderr en exit ≠ 0, volgens het
  `exitError`-patroon in `internal/cli`.

Registratie in `internal/cli/root.go`, met een eigen `internal/cli/report.go` naar het model van
`internal/cli/worktreeaudit.go`: een injecteerbare omgeving met de forge-opener, de klok en de
uitvoer, zodat de test zonder netwerk en zonder echte tijd draait. De forge komt uit
`forgeOpener.forRepo` (`internal/cli/forges.go`).

### 3. Periodes

Alles in de lokale tijdzone van de host (`time.Local`), met halfopen intervallen `[since, until)`:

- **day:** van 00:00 van die dag tot 00:00 van de volgende dag.
- **week:** ISO-week, van maandag 00:00 tot de maandag erna. Label `JJJJ-Www` met het ISO-jaar
  (30 december 2024 valt in `2025-W01`).
- **month:** van de 1e om 00:00 tot de 1e van de volgende maand.

Bij een wisseling van zomer- naar wintertijd duurt een dag 23 of 25 uur. De grenzen komen uit
`time.Date` in `time.Local`, niet uit een optelling van 24 uur.

### 4. Forge-uitbreiding

Nieuwe types en methodes in `internal/forge`:

```go
// PRSummary is a pull request as a report lists it.
type PRSummary struct {
	Number    int
	Title     string
	URL       string
	Draft     bool
	Mergeable string    // MERGEABLE, CONFLICTING, UNKNOWN; "" for a merged PR
	Checks    string    // statusCheckRollup.state, "" without checks
	MergedAt  time.Time // zero while not merged
}

// MilestonedIssue is an issue that was put in a milestone inside a period.
type MilestonedIssue struct {
	Issue     Issue
	Milestone Milestone
	At        time.Time
}

// in Forge:
MergedPRs(ctx context.Context, since, until time.Time) ([]PRSummary, error)
ClosedIssues(ctx context.Context, since, until time.Time) ([]Issue, error)
MilestonedIssues(ctx context.Context, since, until time.Time) ([]MilestonedIssue, error)
OpenPRsWithStatus(ctx context.Context) ([]PRSummary, error)
```

Het bestaande `PR`-type en `OpenPRs` blijven ongewijzigd. `PR` heeft geen titel en hoort bij de
snapshot van het board, en `OpenPRs` heeft andere afnemers.

Invulling voor GitHub, het startpunt voor de bouwer:

- **`MergedPRs`:** de search-API, `repo:<slug> is:pr is:merged merged:<since>..<until>`, met
  tijdstippen in ISO 8601 met offset. Alle pagina's volgen. De search-API geeft maximaal 1000
  resultaten. Komt `total_count` daarboven, dan is dat een fout, geen stille afkapping.
- **`ClosedIssues`:** dezelfde route, `is:issue is:closed closed:<since>..<until>`. Alleen issues
  met `state_reason` `completed` (of zonder `state_reason`) tellen als gedaan. `not_planned` valt
  weg.
- **`MilestonedIssues`:** de repo-brede issue-events (`GET /repos/{slug}/issues/events`),
  nieuwste eerst. Bladeren stopt zodra een event ouder is dan `since`. Een issue telt als het
  event `milestoned` in `[since, until)` ligt, het issue nu nog in díe milestone zit, en het issue
  nu open is (het `issue`-object in het event, of één extra issue-read). Een issue dat intussen
  dicht is, staat daardoor nooit onder "Gaande". Was het in de periode gesloten, dan staat het al
  onder "Gedaan". Eén regel per issue en milestone, en pull requests vallen weg.
- **`OpenPRsWithStatus`:** GraphQL `pullRequests(states: OPEN)` met titel, url, draft, mergeable
  en `statusCheckRollup.state`, met alle pagina's. Dat lijkt op `prFields` in
  `internal/forge/board.go`.

Elke implementatie en fake van `Forge` krijgt de vier methodes. Fakes die `forge.Forge`
embedden, hoeven niets.

### 5. Inhoud en vorm

Het verslag is markdown. De vaste koppen en zinnen staan in Go per taal (`nl`, `en`), volgens het
patroon van `openText` in `internal/review/openpoints.go`. Het zijn geen contract-headings, dus ze
staan niet in `headings.toml`. Een andere taal dan `nl` of `en` valt terug op `en`.

Schets in `nl`:

```markdown
# Verslag week 2026-W47 · CorneeldH/lighthouse

16–22 november 2026. Open PR's: stand op 2026-11-20 17:04.

## Gedaan

### Gemergde PR's (2)

- [#352](https://github.com/…/pull/352) fix(gitleaks): proza-allowlist …
- [#351](https://github.com/…/pull/351) fix(gate): resolve volgt symlinks …

### Gesloten issues (1)

- [#228](https://github.com/…/issues/228) Gitleaks vangt checkpoints …

## Gaande

### Open PR's (2)

- [#353](…) feat(report): … — groen
- [#354](…) docs: … — conflict

### Nieuw op de roadmap (1)

#### Lighthouse zonder Python

- [#285](…) …
```

Regels:

- Een lege sectie krijgt één regel "Geen." en verdwijnt niet.
- **Volgorde:** gemergde PR's op `MergedAt`, gesloten issues op `ClosedAt` (beide oplopend). Open
  PR's op nummer. Milestones op titel, issues erin op nummer.
- **Status van een open PR, eerste treffer wint:** `concept` (draft), `conflict`
  (`CONFLICTING`), `rood` (checks `FAILURE` of `ERROR`), `bezig` (`PENDING` of `EXPECTED`),
  `groen` (`SUCCESS`), `geen checks` (leeg).
- **Open PR's zijn een momentopname.** Ze zijn niet aan de periode gebonden. Daarom staat de
  stand met tijdstip in de kop, ook bij een `--date` in het verleden.

### 6. Pakketindeling

Een nieuw pakket `internal/report` met:

- de periode-berekening;
- `Build(ctx, src, period) (Report, error)`, waarbij `src` een smalle interface is met alleen de
  vier methodes, zodat de test een kleine fake heeft;
- `Render(Report, language) string`.

De sink (stdout of bestand) zit in `internal/cli/report.go`.

### 7. Tests

Volgens `/lighthouse-test-standard` en `/lighthouse-go-standard`:

- **Periodes:** de jaargrens van een ISO-week, een schrikkeljaar in februari, en een
  zomertijd-wissel in `Europe/Amsterdam` (een dag van 23 en van 25 uur).
- **Build met een fake bron:** elke sectie gevuld en leeg, een fout van één bron wordt een fout
  van `Build` en geen half verslag, en de statusvolgorde van open PR's.
- **Render:** `nl` en `en` als golden, de val terug op `en`, en "Geen." bij een lege sectie.
- **Contract van de forge.** De vier methodes komen in de forge-neutrale suite
  `internal/forge/forgesuite`, met nieuwe `Fixture`-methodes om een gemergde PR, een gesloten
  issue en een `milestoned`-event te zaaien. Ook krijgt de nep-GitHub `internal/forge/forgetest`
  routes voor search, `issues/events` en GraphQL `pullRequests`. Zo draait een tweede forge later
  dezelfde gevallen.
- **GitHub-specifieke gevallen tegen een `httptest`-server** met opgenomen antwoorden:
  - de query-string van de search-API (repo, datumbereik met offset);
  - bladeren, en de fout bij `total_count` > 1000;
  - het stoppen bij een event ouder dan `since`;
  - de filter op `not_planned`;
  - een issue dat intussen uit de milestone is gehaald.
- **CLI met de injecteerbare omgeving:** het verslag dat uit staat (exit 0, melding), `file` zonder
  `dir` (fout), het pad en label per periode, en `--date`.
- **Config:** de pariteitstest Go en Python, en de test van de site-config, met de nieuwe sleutels.

### 8. Bijwerken

- De body van #44 volgt deze spec: AC 2 noemt nu "issues die op de gebruiker wachten" en gaat over
  naar "nieuw op de roadmap", met week en maand erbij.
- Een fragment in `changelog.d/`.
- Documentatie van het commando en de sleutels, op de plek waar andere subcommando's beschreven
  staan.
- Vervolgissues voor de niet-doelen: de sinks `wiki` en `discussion`, over repo's heen, een timer,
  en de UI.

## Mijn keuzes (te controleren)

- **Namen:** `[report]` en `lighthouse report`, Engels zoals de gebruiker vroeg. `day|week|month`
  is een argument en geen vlag.
- **Default `sink = "stdout"`, en `file` alleen met `dir`.** Er is geen default-map: de plek is per
  repo een keuze (#128 kiest die van lighthouse).
- **Zonder `--date` de lopende periode**, niet de vorige. Wie maandag de vorige week wil, geeft een
  datum in die week mee.
- **ISO-week** van maandag tot en met zondag.
- **"Gesloten issues" alleen `completed`:** "not planned" is niet gedaan.
- **"Nieuw op de roadmap"** telt alleen issues die nu nog open zijn en nog in die milestone zitten.
  Zo staat een issue nooit tegelijk onder "Gedaan" en "Gaande".
- **Een fout in `[report]` laat `Load` niet falen** (`ReportErr`, zoals `TriageErr`). Alleen
  `lighthouse report` stopt erop.
- **Zonder `--repo` de top van de werkmap.** In een worktree is dat de worktree.
- **Padsegment `day|week|month`** letterlijk, niet vertaald.
- **De vier methodes gaan in `forgesuite`.** Dat maakt het plan groter, maar is het bestaande
  mechanisme voor forge-neutraliteit.
- **De melding bij een uitgezet verslag** is Engels: het is een toolmelding, geen artefact.
- **De mirror in `config.py` wordt gewijzigd**, niet vervangen. Zolang de milestone zonder Python
  hem niet heeft opgeruimd, eisen de pariteitstests dat.

## Aan de bouwer

- De naam van het pakket en de types, en of `PRSummary` een eigen bestand krijgt.
- Of `OpenPRsWithStatus` GraphQL of REST plus de status-API gebruikt. Het verschil is het aantal
  calls bij veel open PR's.
- **Verifiëren:** hoe ver `GET /repos/{slug}/issues/events` teruggaat. Is dat beperkt, dan geeft
  een `--date` voorbij die grens geen stil leeg "Nieuw op de roadmap", maar een fout of een regel
  in het verslag dat de bron niet zo ver teruggaat.
- Waar de documentatie van het commando komt, naast die van `worktree-audit` of een eigen pagina.
