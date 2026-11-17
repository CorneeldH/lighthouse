# Forge-wrapper voor skills en hooks — ontwerp (#260)

## Waarom

Deel 4 van de milestone GitLab-forge. De forge-interface (#258) en de GitLab-client (#259) staan,
maar de skills en hooks praten nog rechtstreeks met `gh`. De milestone-DoD vraagt dat een repo op
GitLab één echt issue tot een MR brengt via de handmatige keten, met de hooks actief, terwijl de
GitHub-keten blijft werken. Dit ontwerp maakt de keten tot en met de MR forge-onafhankelijk.

## Wat sinds het issue veranderd is

- De GitLab-client in [`internal/forge`](../../../../internal/forge/gitlab.go) praat met de
  GitLab-API zelf. De open milestone-vraag "glab of de API" is daarmee beantwoord: de API.
- `forge.host` bestaat in de config (alleen bij `kind = "gitlab"`). Self-hosted is dus geen apart
  doel: gitlab.com is de standaard, `forge.host` wijst een eigen server aan.
- De hooks `pr-create-gate`, `pr_body_gate` en `evidence` zijn nu Go
  ([`internal/hooks`](../../../../internal/hooks/prcreategate.go),
  [`internal/prbody`](../../../../internal/prbody/find.go),
  [`internal/evidence`](../../../../internal/evidence/allowlist.go)), niet meer shell en Python.
- `board-start` is Go, maar roept nog `gh` aan
  ([`boardstart.go:111`](../../../../internal/boardstart/boardstart.go)).

## Doelen

- `lighthouse forge …` dekt de operaties die de keten `start-issue` → `route-issue` →
  `write-issue` → `write-pr` op beide forges gelijk doet, met één JSON-vorm.
- Wat echt per forge verschilt, staat in een referentie per forge die de skills aanwijzen.
- De skills van die keten en `board-start` roepen geen `gh` meer rechtstreeks aan.
- `pr-create-gate`, `pr_body_gate` en `evidence` werken op beide forges.
- Op GitHub gedraagt de keten zich als nu.

## Niet-doelen

- `/lighthouse-close-issue`. Die krijgt een eigen issue (keuze van de gebruiker): het leunt op
  GitHub-mechaniek (workflow `unblock-dependents`, releases, `closedByPullRequestsReferences`) die
  op GitLab een eigen ontwerp vraagt.
- De overige units met `gh` (`fix-pr`, `write-handoff`, `review-plan`, `upgrade`,
  `audit-worktrees`, `post-artefact`, `unblock-dependents`, `create_labels.py`, de workflows). Die
  komen in één issue met wat ze op GitLab nodig hebben (AC 5).
- De dispatcher op GitLab.
- Een echte run op GitLab (één issue tot een MR, milestone-DoD). Dat wordt een eigen
  verificatie-issue in de milestone, voor wie als eerste een GitLab-project heeft (gebruiker):
  #358.
- `glab` als afhankelijkheid.

## Anti-doelen

- Een wrapper die GitHub-begrippen nabootst op GitLab (Projects v2, `blocked_by`, sub-issues). Wat
  niet gelijk is, krijgt geen gemeenschappelijk commando.
- Twee skills per forge, of een GitHub- en GitLab-pad naast elkaar in één `SKILL.md`.
- Een GitHub-keten die stiller, trager of minder streng wordt dan nu.

## Aanpak

Drie overwogen:

| | Aanpak | Afweging |
|---|---|---|
| A | Alleen de wrapper: elke forge-operatie via `lighthouse forge` | Skills forge-blind, maar de wrapper moet ook verbergen wat maar op één forge bestaat: precies het risico "abstractie op maat van GitHub" uit de milestone. Veel Go. |
| B | Alleen referenties: per forge het rauwe commando (`gh`, `glab`/curl) | Weinig Go, maar twee commandosets, hooks en evidence moeten `glab` kennen, en op GitLab komt er een tool bij. |
| **C** | **Hybride: wrapper voor wat gelijk is, gedeelde referentie per forge voor wat verschilt** | **Gekozen.** Het gemeenschappelijke deel blijft testbaar in Go, met de bestaande contract-suite voor beide forges; de verschillen blijven zichtbaar in plaats van verstopt. |

Binnen C liggen de referenties op één gedeelde plek waar de skills naar verwijzen (skill-chaining),
niet per skill een eigen kopie (*eigen keuze*: één plek om bij te houden, en de gebruiker maakte
geen bezwaar).

## Werking

### 1. `lighthouse forge`

Een oudercommando met subcommando's, naar het model van `lighthouse evidence`
([`internal/cli/evidence.go`](../../../../internal/cli/evidence.go)). Het opent de forge van
`forge.repo` via `forgeOpener.forRepo`
([`internal/cli/forges.go`](../../../../internal/cli/forges.go)); tokens zoals nu
(`GH_TOKEN`/`GITHUB_TOKEN` of `gh auth token`, `GITLAB_TOKEN`).

| Subcommando | Wat | Nodig door |
|---|---|---|
| `issue view <nr> [--comments]` | issue, met comments op verzoek | start-issue, route-issue, write-pr, write-issue |
| `issue list [--state] [--label] [--milestone]` | issues, alle pagina's | write-issue (overzicht, duplicaten) |
| `issue search <tekst>` | issues op titel/tekst | write-issue (duplicaatcheck, herstel na mislukt create) |
| `issue create --title --body-file [--label]… [--milestone]` | nieuw issue; drukt nummer en URL | write-issue |
| `issue edit <nr> [--title] [--body-file] [--add-label]… [--remove-label]… [--milestone]` | wijzigt | write-issue, route-issue |
| `issue comment <nr> --body-file` | comment | write-issue |
| `pr create --title --body-file [--milestone] [--head] [--base]` | PR of MR; drukt nummer en URL | write-pr |
| `pr view <nr>` | PR of MR | write-pr (evidence) |
| `pr comment <nr> --body-file` | comment op PR of MR | write-pr |
| `milestone list [--state]`, `milestone view <nr>` | milestones met beschrijving | start-issue, write-issue, write-pr |
| `milestone create --title --description-file`, `milestone edit <nr> [--title] [--description-file]` | schrijven | write-issue |
| `label list` | labels van de repo | write-issue |
| `issue blockers <nr>` | open en gesloten blockers (`BlockedBy`) | route-issue, write-issue |
| `issue block <nr> --by <blocker>` | blocker-relatie zetten (`AddBlocker`, nieuwe interfacemethode) | write-issue |
| `issue status <nr> <status>` | status op het board (`IssueItem`, `SetStatus`) | write-issue |
| `api <pad> [-X <methode>] [-f <veld>=<tekst>]… [-F <veld>=<waarde>]…` | rauwe aanroep op de API van de geconfigureerde forge, met diens token en host | de referenties (§2) |

Afspraken:

- **Eén JSON-vorm** op stdout voor beide forges, met namen van lighthouse zelf: `number`, `title`,
  `body`, `state` (`open`/`closed`), `labels` (lijst van namen), `milestone` (`number`, `title`,
  `description`, of `null`), `url`, `created_at`, `author`, en bij `--comments` een lijst `comments` (`author`, `body`,
  `created_at`). `created_at` en `author` zijn nodig voor het herstel na een mislukt `issue create`
  (zelfde titel en body, aangemaakt na het genoteerde starttijdstip) *(eigen keuze na spec-review, r1-f3)*. Een PR en een MR hebben dezelfde vorm; `state` is voor een PR
  `open`, `closed` of `merged` *(eigen keuze na spec-review, r1-f12)*. `issue blockers` drukt een
  lijst af met per blocker `number`, `state` (`open`/`closed`), `repo` en `merged_pr` (de vorm van
  `forge.Blocker`, [`board.go:41-45`](../../../../internal/forge/board.go)) *(eigen keuze na spec-review, r1-f12)*. De skills passen hun `jq` hierop aan.
- **`--field <pad>`** op elk leescommando: een pad met punten (`milestone.title`, `labels`) dat
  alleen die waarde afdrukt, een scalar als tekst en de rest als JSON. Zo pint een Evidence-rij een
  exacte waarde zonder `jq` (*eigen keuze*: geen nieuwe dependency zoals gojq). `--field` is op
  `api` geen kiezer: `api` kent alleen `-f` en `-F`, en de lange vorm `--field` bestaat daar niet,
  zodat de verbodslijst van write-pr (`-f`/`-F`/`--field`/`--raw-field`) ondubbelzinnig blijft
  *(eigen keuze na spec-review, r1-f13)*.
- **Milestones op GitLab**: `milestone list` en `milestone view` geven project- én groepsmilestones
  (`include_ancestors`); `milestone create` maakt er één in het project. Het `number` van een milestone in de JSON is op
  GitLab het globale id (de iid herhaalt zich tussen project en groep, zoals `glIssue.issue` al
  vastlegt). `milestone view <nr>` zoekt dat id in `include_ancestors`, `milestone edit <nr>` weigert
  een groepsmilestone met een heldere melding (exit 1; die wijzig je met `api`), en een titel die
  op twee milestones past is een gebruiksfout (exit 2) die beide noemt
  *(eigen keuze na spec-review, r1-f5)*; `--milestone` op
  `issue create`, `issue edit` en `pr create` neemt een titel, op beide forges (*eigen keuze*: veel
  GitLab-projecten houden milestones op groepsniveau, en de titel is wat de skills nu al gebruiken).
- **`api` is de uitwijkweg, geen abstractie.** Het pad en de JSON zijn die van de forge zelf; de
  wrapper voegt alleen token, host en paginering toe. Veldsemantiek als bij `gh api`: `-f` is een
  tekstveld, `-F` een getypeerd veld (getal, `true`/`false`, `null`, of `@bestand` voor de inhoud
  van een bestand), en samen vormen ze de JSON-body. GraphQL loopt door hetzelfde commando met het
  eigen pad van de forge (`graphql` op GitHub, `api/graphql` op GitLab) en `-F`/`-f query=…`; de
  evidence-allowlist laat dat niet toe omdat het geen GET is *(eigen keuze na spec-review, r1-f4)*. Zo hoeven de referenties geen `glab` of `curl`
  met een token in de shell te gebruiken (gebruiker, keuze a).
- **`pr` op beide forges.** Op GitLab maakt `pr create` een MR; de tekst van de uitvoer zegt "MR".
  Geen apart `mr`-commando (*eigen keuze*: één woord in de skills).
- **Exitcodes**, eigen reeks (niet die van `evidence`, dat 3 voor een onleesbare config gebruikt):
  0 goed, 1 forge-fout (met de API-melding), 2 gebruiksfout of ontbrekende config
  *(eigen keuze na spec-review, r1-f8)*.
- **Geen merge.** `lighthouse forge` krijgt geen merge-commando (milestone-no-go).
- **Nieuwe interfacemethodes.** De `Forge`-interface krijgt wat nu ontbreekt (create/edit issue,
  comment, create PR/MR, milestones, labels, search, `AddBlocker`). `AddBlocker` zet op GitHub
  `dependencies/blocked_by` met het gehele database-`id` van de blocker (zoals write-issue nu doet,
  `SKILL.md:210-211`), op GitLab `POST …/links` met `link_type=is_blocked_by`; een weigering van de
  API (bijvoorbeeld op Free) komt als forge-fout met de API-melding terug (exit 1)
  *(eigen keuze na spec-review, r1-f2, r1-f7)*, voor GitHub en GitLab, met gevallen in
  [`internal/forge/forgesuite`](../../../../internal/forge/forgesuite/suite.go) zodat beide
  implementaties dezelfde contracttests draaien.
- **`board-start`** gebruikt de forge-interface (`IssueItem`, `SetStatus`) in plaats van `gh`, en
  behoudt zijn huidige contract *(eigen keuze na spec-review, r1-f6)*: de token valt terug op
  `gh auth token` (de `ghToken`-terugval van `forgeOpener`, nu alleen bij `ask`, wordt ook hier
  ingehangen); een token- of forgefout blijft exit 0 met een `board: …`-regel; een status die niet
  in `status_options` staat (bijvoorbeeld "On hold") beslist board-start op de ruwe optienaam: `BoardItem`
  krijgt een extra veld `StatusName`, gevuld op GitHub door `name` aan `fieldValuesSelection` toe te
  voegen en op GitLab uit `status.name`; `Status` en de `BoardItems`-query van de dispatcher blijven
  gelijk in betekenis (het veld is additief) *(eigen keuze na spec-review, r2-f1)*. De 37 gevallen in
  `tests/pack/fixtures/scripts/board-start/cases.json` die `gh`-argv stubben, worden vervangen door
  tests op een fake `forge.Forge`; de uitkomsten per geval blijven gelijk.

### 2. De gedeelde referentie

Een nieuwe skill `pack/core/skills/forge/`, geïnstalleerd als `lighthouse-forge`:

```text
pack/core/skills/forge/
  SKILL.md    — wanneer welke referentie; niet om zelf mee te starten
  github.md   — §board, §blockers, §sub-issues, §sluitwoorden, §proxy
  gitlab.md   — dezelfde secties
```

Board-status en blockers zitten al in de forge-interface en lopen daarom via de wrapper
(`issue status`, `issue blockers`, `issue block`). De referentie zegt bij §board en §blockers alleen
wat er per forge anders aan is (statusvelden, tiergrenzen). Wat geen gemeenschappelijk commando
heeft, beschrijft de referentie met `lighthouse forge api`.

- De andere skills verwijzen met een relatief pad vanaf hun eigen map:
  `../lighthouse-forge/<kind>.md`, met `<kind>` uit `lighthouse config forge.kind`. Zo werkt één
  `SKILL.md` in `.claude/skills/` en `.agents/skills/` gelijk.
- Elke referentie heeft dezelfde secties in dezelfde volgorde, zodat een skill één sectienaam
  noemt (bijvoorbeeld "§blockers") en de forge alleen het bestand kiest.
- De `SKILL.md` van `lighthouse-forge` heeft een beschrijving die zegt dat de skill alleen via een
  andere skill gelezen wordt, zodat hij niet uit zichzelf triggert.

Wat er in de secties staat:

| Sectie | GitHub | GitLab |
|---|---|---|
| §board | Projects v2; statussen uit `[forge.board]`; token met scope `project` | ingebouwde work-item-status (zoals de client van #259, `gitlab_board.go`); namen uit `[forge.board]` `status_options`, gematcht tegen de lifecycle van de topgroep; geen label *(eigen keuze na spec-review, r1-f1)* |
| §blockers | `dependencies/blocked_by` | issue links `is_blocked_by`; alleen in Premium/Ultimate. Op Free: zie *Aan de bouwer*. `issue block` geeft daar de weigering van de API; `issue blockers` geeft op Free een lege lijst zonder fout, dus de referentie zegt dat route-issue op GitLab ook het label `blocked_issue` en de blocker in de body leest *(eigen keuze na spec-review, r1-f7)* |
| §sub-issues | `lighthouse forge api` op `repos/<repo>/issues/<nr>/sub_issues` | taken (work item type Task) onder een issue via `lighthouse forge api`; epics vragen Premium |
| §sluitwoorden | `close[sd]`, `fix(e[sd])`, `resolve[sd]` | daarnaast `closing`, `fixing`, `resolving`, `implement(s/ed/ing)` |
| §proxy | `gh issue list --json` breekt onder de rtk-proxy; gebruik `lighthouse forge` of `gh api` | n.v.t. |

### 3. De skills

- `start-issue`, `route-issue`, `write-issue`, `write-pr`: elke `gh`-aanroep die in de tabel van §1
  valt, wordt `lighthouse forge …`. Wat in een sectie van §2 valt, wordt een verwijzing naar die
  sectie. De regel over `gh issue list --json` verhuist naar §proxy.
- Beide tweelingen (`claude/` en `agents/`) gaan mee; `route-issue` en `write-pr` hebben er geen
  (één bestand). De contracttests in `tests/pack/test_skill_contracts.py` die de `gh`-vorm vastleggen
  (de #224-test voor `write-pr`, rond regel 404) en de eis dat een skill die `gh` noemt een
  `requires.toml`-regel heeft, gaan mee *(eigen keuze na spec-review, r1-f11)*.
- `pack/core/requires.toml`: `gh` blijft bij de ketenskills staan, omdat `github.md` het noemt
  (`gh auth token` als token, `gh project` voor wie het board met de hand bijwerkt). Op een
  GitLab-repo geeft dat een onterechte waarschuwing; een voorwaarde per forge in `requires.toml`
  hoort bij het issue voor de overige units (*eigen keuze*).
- Dogfood: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie mee in de commit.

### 4. Hooks

**`pr-create-gate` en `pr_body_gate`** herkennen drie vormen:

- `lighthouse forge pr create` (het gewone pad op beide forges);
- `gh pr create`/`gh pr new` (zoals nu);
- `glab mr create` (rauw, voor wie buiten de skill om gaat; de body via `-d`/`--description`). De
  `--repo`-regel geldt voor `glab` met `-R`/`--repo` zoals voor `gh`, en de tweestaps-regel voor een
  onparseerbaar commando kijkt ook naar `glab`/`mr` (nu laat `prcreategate.go:85` een commando zonder
  `gh` en `pr` door) *(eigen keuze na spec-review, r1-f10)*.

De twee plekken die nu op `gh pr create` matchen
([`find.go:233`](../../../../internal/prbody/find.go),
[`prcreategate.go:85`](../../../../internal/hooks/prcreategate.go)) veranderen samen. De
`--repo`-regel ([`scope.go:242-253`](../../../../internal/prbody/scope.go)) geldt voor `gh`; de
wrapper leest `forge.repo` zelf en heeft hem niet nodig.

**Sluitwoorden en doelen** ([`form.go`](../../../../internal/prbody/form.go)): bij
`forge.kind = "gitlab"` de GitLab-woorden uit §2 erbij, en als doel ook
`<groep>/<project>#N` met subgroepen en `https://<host>/<pad>/-/issues/N`.

**AC-telling** ([`gate.go:62-72`](../../../../internal/prbody/gate.go)): leest het issue via de
forge-interface in plaats van `gh api`. Op GitHub valt het token terug op `gh auth token` (de terugval wordt in de bouw van `prbody.Env`
aan de forge-opener gehangen, met de huidige `Env.GH`-naad als testnaad; r1-f9), dus de
uitkomst (doorlaten of blokkeren) blijft gelijk. Een meldingstekst mag veranderen (een ontbrekend
token in plaats van een ontbrekende `gh`); de gate wordt nergens minder streng, en een fout blijft
een blokkade (*eigen keuze*).

**`evidence`** ([`allowlist.go`](../../../../internal/evidence/allowlist.go)): staat de leesvormen
van `lighthouse forge` toe (`issue view`, `issue list`, `issue search`, `issue blockers`,
`pr view`, `milestone list`, `milestone view`, `label list`, en `api` alleen als GET zonder
`-f`), met `--field`, naast de bestaande `gh`-vormen. Dezelfde verboden als nu gelden (geen `env`,
geen invoerbestand). Dat zijn de GitLab-varianten
van AC 4 (*eigen keuze*: geen `glab` in de allowlist, want `glab` is geen afhankelijkheid). De
bereikbaarheidsprobe (`gh api rate_limit`) wordt een probe op de forge van `forge.kind`; ook rijen
met `lighthouse forge` tellen mee voor die probe en worden *onmeetbaar* als de forge onbereikbaar
is, niet *faalt* (nu geldt dat alleen voor `argv[0] == "gh"`, `run.go:402`)
*(eigen keuze na spec-review, r1-f14)*.

## Vastgelegd

- Aanpak C, met de referenties gedeeld in `lighthouse-forge` (gebruiker).
- `/lighthouse-close-issue` gaat naar een eigen issue (gebruiker).
- De wrapper praat met de API via `internal/forge`, niet met `glab` (volgt uit #259).
- gitlab.com is de standaard; `forge.host` voor self-hosted (bestaande config).
- Blockers en board-status via de wrapper; de referenties gebruiken `lighthouse forge api` voor de
  rest (gebruiker, keuze a).
- Geen echte GitLab-run in dit issue; daar komt een eigen issue voor (gebruiker).

## Aan de bouwer

- De precieze vlaggen van de subcommando's, de volgorde van de velden in de JSON, en hoe
  `glab mr create` in de hooks geparsed wordt.
- Of `issue search` en `issue list` één subcommando worden.
- Hoe `§blockers` op GitLab Free werkt: de API weigert `is_blocked_by` daar. Voorstel: de
  referentie zegt dat, `route-issue` valt terug op het label `labels.blocked_issue` plus het nummer
  van de blocker in de body, en `lighthouse forge` meldt de weigering helder in plaats van stil.
- De twee issues die uit dit ontwerp volgen (close-issue; de overige units) schrijven via
  `/lighthouse-write-issue`, in dezelfde milestone.

## Bijgewerkte acceptatiecriteria van #260

- [ ] `lighthouse forge` dekt wat de keten `/lighthouse-start-issue` tot `/lighthouse-write-pr` van
  de forge vraagt, voor GitHub en GitLab, met contracttests voor beide in `forgesuite`.
- [ ] Die skills, hun tweelingen en `board-start` roepen `lighthouse forge` of de forge-interface
  aan in plaats van `gh`; wat per forge verschilt staat in `lighthouse-forge/<kind>.md`. Op GitHub
  gedragen ze zich als nu.
- [ ] `pr-create-gate` en `pr_body_gate` herkennen `lighthouse forge pr create`, `gh pr create` en
  `glab mr create`, en de sluitwoorden en doelen van GitLab.
- [ ] `lighthouse evidence` laat de leesvormen van `lighthouse forge` toe.
- [ ] `/lighthouse-close-issue` en de overige units met `gh` staan in issues, met wat ze op GitLab
  nodig hebben.
