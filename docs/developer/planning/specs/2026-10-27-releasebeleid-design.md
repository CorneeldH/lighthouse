# Releasebeleid, releasebinaries en changelog (#86)

## Doel en waarom

Lighthouse is nu alleen te installeren met Go (`go install …@main` of een clone). Een developer
zonder Go kan niets. Milestone 6 ("Product: eerste versie voor anderen") wil dat een ander
lighthouse downloadt en gebruikt. Daarvoor zijn nodig: een vast releasebeleid, binaries per
platform aan elke GitHub-release, een Engelse changelog, en een eerste release volgens dat beleid.

De repo is publiek (`gh api repos/CorneeldH/lighthouse` gaf op 2026-10-27 `public`); het
besluit en de scan op geheimen horen bij #93. Dit ontwerp gebruikt daarom gewone GitHub-releases:
geen aparte releaserepo, geen Pages.

## Doelen

- Releases gaan automatisch, zonder handwerk per week:
  - een merge met een bugfix levert direct een patchrelease;
  - een wekelijkse run levert een minor als er sinds de laatste tag iets anders veranderd is.
- Release notes en `CHANGELOG.md` zijn Engels en ontstaan automatisch.
- Elke release draagt binaries voor `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`
  en `windows/amd64`, met checksums.
- `lighthouse version` van een releasebinary toont de tag.
- Het beleid staat in de repo, voor gebruikers leesbaar.

## Non-goals

- De echte SPA in de binary (#78). De binary draagt de placeholder uit `internal/web/dist`.
- De website met download (#89).
- De scan op geheimen en het vastleggen van de toegangskeuze (#93).
- De fragmentenpijplijn voor consumers. Workflow en releasescript staan alleen in lighthouse's
  eigen `.github/` en `ci/`; een consumer krijgt ze niet via `init`. Dat kan later een eigen
  issue worden.
- Ondertekenen of notariseren (macOS Gatekeeper, Windows SmartScreen), Homebrew, Scoop.
- Native Windows-ondersteuning voor de hooks (bash en Python). De Windows-binary bouwt; de docs
  zeggen dat de hooks WSL vragen.
- Een 1.0-release.

## Anti-goals

- Release notes in het Nederlands, of notes die een mens elke week moet schrijven.
- Een release waarvan de binary `lighthouse dev` meldt.
- Een releaseworkflow die een model of API-sleutel nodig heeft.
- Een Windows-build die pas bij de release breekt in plaats van in de PR.

## Randvoorwaarden

- Artefacten (issues, PR's, commits, deze spec) zijn Nederlands; release notes en
  `CHANGELOG.md` zijn Engels (issue #86). Gebruikersdocs onder `docs/` zijn al Engels
  (`docs/install.md`); het beleid volgt die lijn.
- De squash-titels van PR's zijn Nederlands en dragen geen Conventional-prefix. De workflow kan
  het type van een wijziging dus niet uit de commit-titel halen.
- Nothing lands on `main` directly, voor mensen en agents. Zie besluit 6 voor de bot.
- Een tag of push gezet met `GITHUB_TOKEN` start geen nieuwe workflowrun (GitHub-regel).
- Dogfood: een wijziging onder `pack/` loopt `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie; CI eist `No changes.`
- `tests/test_ci_go_test_pin.py` en `tests/test_golangci_pin.py` pinnen regels in
  `.github/workflows/ci.yml`; die regels blijven ongemoeid.

## Aanpakken

1. **Changelog-fragment per PR (gekozen).** `/lighthouse-verify-pr` schrijft bij elke PR een
   Engels fragment met een type. Een CI-check eist het fragment. De releaseworkflow is
   deterministisch: hij leest de typen, bepaalt de bump en voegt de fragmenten samen. Kost een
   wijziging aan verify-pr in `pack/core`.
2. **Een model in CI bij de release.** De workflow verzamelt PR's sinds de laatste tag en laat
   Claude Engelse notes schrijven en de PR's indelen. Afgewezen: een API-sleutel in een publieke
   repo, notes die per run anders zijn, een indeling achteraf gegokt uit Nederlandse tekst, en
   voor de directe patch toch een label.
3. **release-please met Conventional PR-titels.** Afgewezen: vraagt Engelse PR-titels, wat botst
   met artefacten in het Nederlands.

Corneel koos aanpak 1.

## Besluiten

### 1. Semver onder 0.x

| Bump | Wanneer |
|---|---|
| patch | Na een merge op `main`, als alle openstaande releasebare fragmenten `type: fixed` zijn (en er minstens één is). |
| minor | De wekelijkse run, als er minstens één openstaand fragment van een ander releasebaar type is. Ook direct na een merge met een `fixed`-fragment als er al andere releasebare fragmenten openstaan: een patch zou die features meenemen. |
| major | Niet automatisch. 1.0 is een eigen besluit, buiten dit issue. |

"Openstaand" = in `changelog.d/` op `main`, nog niet in een release opgenomen.
`type: internal` is niet releasebaar: het telt niet mee voor de bump en komt niet in de notes.

Wat telt als wijziging voor een gebruiker: alles wat de binary of wat `init` schrijft (pakketten,
hooks, skills, het blok in `AGENTS.md`) verandert. Een nieuwe skill of hook is `added` en gaat dus
met de wekelijkse minor mee. Een breaking change (hernoemde sleutel in `lighthouse.toml`, een
verwijderde hook) is `changed` of `removed`; onder 0.x is dat ook een minor.

De eerste automatische release na `v0.1.6` is `v0.2.0`.

### 2. Fragmenten

- Map `changelog.d/` in de repo-root, met een getrackt markeerbestand `changelog.d/.lighthouse`
  (een regel uitleg plus een verwijzing naar `docs/releasing.md`). De release verwijdert het
  nooit en de fragmentlezer slaat het over: zonder dat bestand verdwijnt de lege map na elke
  release uit git, en daarmee het opt-in. *(eigen keuze na spec-review, r1-f5, r1-f13)*
- Eén bestand per wijziging met één type: `changelog.d/<branch>.md`, of
  `changelog.d/<branch>-<n>.md` als een PR meer dan één wijziging draagt (een fix én een feature:
  twee fragmenten). Op de branch, want het PR-nummer bestaat nog niet als verify-pr het fragment
  schrijft. Een `/` in de branchnaam wordt `-`. Bestaat de naam al (een open fragment van een
  eerdere branch met dezelfde naam), dan neemt verify-pr de eerste vrije `-<n>`.
  *(eigen keuze na spec-review, r1-f10)* Een PR draagt minstens één fragment.
- Vorm:

  ```markdown
  ---
  type: fixed
  ---
  `lighthouse init --upgrade` no longer rewrites unchanged hook files.
  ```

- Typen: `added`, `changed`, `fixed`, `removed`, `internal` (categorieën van Keep a Changelog,
  plus `internal`).
- Tekst: Engels, één tot drie zinnen, voor een gebruiker geschreven. `internal` mag kort.

### 3. verify-pr schrijft het fragment, opt-in per repo

- Opt-in via het markeerbestand `changelog.d/.lighthouse` (besluit 2): alleen dan schrijft
  `/lighthouse-verify-pr` het fragment en commit het op de branch, vóór `gh pr create`. Een
  consumer zonder dat bestand merkt niets; ook een consumer die `changelog.d/` al voor scriv
  gebruikt niet. Geen sleutel in `lighthouse.toml`: een sleutel die `init` bij ontbreken
  toevoegt (`AppendMissing`), zou het opt-in breken, en zonder pijplijn bij de consumer heeft een
  sleutel geen tweede lezer.
- Lighthouse zelf heeft het markeerbestand.
- De nieuwe alinea in de skill draagt de guard-frase ("only if `changelog.d/.lighthouse` is
  present"): `tests/pack/test_skill_contracts.py` laat een core-alinea met het woord `release`
  zonder guard falen. *(eigen keuze na spec-review, r1-f12)*
- De skill kiest het type uit de inhoud van de PR en de AC van het issue, en schrijft de tekst
  in het Engels, ongeacht `[project] language`.
- Wijziging in `pack/core/skills/verify-pr/`; dogfood geldt.

### 4. CI-check op het fragment

Een nieuwe job (of stap) in `ci.yml` faalt als een PR geen nieuw fragment onder `changelog.d/`
toevoegt, of als een fragment niet aan het contract van besluit 2 voldoet: front matter tussen
twee `---`-regels, precies één `type` uit de lijst, en een niet-lege tekst. De check en het
releasescript gebruiken **één en dezelfde parser**, zodat wat CI doorlaat ook op `main` te
releasen is. Geen label-uitzondering: wie niets releasebaars levert, schrijft `type: internal`.

### 5. Releaseworkflow

`.github/workflows/release.yml`:

- Triggers: `push` naar `main`, `schedule` wekelijks (maandag 06:00 UTC), `workflow_dispatch`
  met invoer `bump: auto|patch|minor|major` (standaard `auto`). Een major gaat alleen via deze
  dispatch, nooit met een tag met de hand: dan blijven `CHANGELOG.md` en de fragmenten kloppen.
- `concurrency` per trigger (push, schedule, dispatch), zonder `cancel-in-progress`. Eén groep
  zou een wachtende cron-run annuleren zodra er een tweede push-run achter komt, en dan valt de
  minor van die week weg. Aparte groepen zijn veilig omdat stap 4 atomair is.
  *(eigen keuze na spec-review, r1-f11)*
- Checkout met alle tags (`fetch-depth: 0`); het script faalt luid als het geen `v*`-tag vindt.
  *(eigen keuze na spec-review, r1-f6)*
- Stappen:
  1. Lees de openstaande fragmenten en bepaal de bump volgens besluit 1. Geen bump: stop.
  2. Bepaal de nieuwe versie uit de laatste `v*`-tag.
  3. Zet een sectie `## [vX.Y.Z] - YYYY-MM-DD` bovenaan `CHANGELOG.md`, gegroepeerd per type
     (`Added`, `Changed`, `Fixed`, `Removed`), en verwijder de opgenomen fragmenten (ook de
     `internal`, nooit `.lighthouse`).
  4. Commit `chore(release): vX.Y.Z`, zet de tag `vX.Y.Z` lokaal, en push beide in één keer:
     `git push --atomic origin HEAD:main vX.Y.Z`. Weigert de remote (een mens merget net), dan
     landt niets: haal `main` opnieuw op en begin weer bij stap 1, hooguit drie keer; daarna faalt
     de run zonder dat er iets op de remote staat. Een volgende run pakt de fragmenten weer op.
  5. Roep de buildworkflow aan (`workflow_call`) met alleen de tag.
- De logica van stap 1–3 zit in een script met pytest-tests, niet inline in de YAML.
- Permissions: `contents: write` op twee jobs: de job die commit en tagt, en de job die
  `build.yml` aanroept (`uses:`; een aangeroepen workflow kan rechten alleen verlagen, zie
  besluit 7). Alle andere jobs read-only. De repo-standaard voor `GITHUB_TOKEN` is `read`.

### 6. De releasecommit gaat direct op `main`

De bot (`github-actions[bot]` met `GITHUB_TOKEN`) commit en tagt rechtstreeks op `main`. Dat is
de enige uitzondering op "nothing lands on `main` directly", en het beleid noemt hem. De push
start geen nieuwe workflowrun, dus geen lus. Komt er later een ruleset op `main`, dan krijgt de
bot een bypass.

### 7. Buildworkflow

`.github/workflows/build.yml` (naam: keuze van de bouwer):

- Triggers: `push` van een tag `v*`, `workflow_call` (vanuit de releaseworkflow) en
  `workflow_dispatch` met invoer `tag`. De tag-push-trigger is er voor AC 2 en voor een
  prerelease-tag (`v0.2.0-rc.1`) als test; een gewone release gaat nooit met een tag met de hand
  (besluit 5). *(eigen keuze na spec-review, r1-f8)*
- Permissions: de publicatiejob declareert zelf `contents: write`, op elke trigger; de buildjobs
  blijven read-only. Onder `workflow_call` kan een aangeroepen workflow rechten alleen verlagen,
  dus ook de aanroepende job in `release.yml` heeft `contents: write`.
- Notes: de publicatiejob leest de sectie van de tag uit `CHANGELOG.md` in de getagde checkout,
  naar een bestand, en geeft dat met `--notes-file` aan `gh`. Geen notes als workflow-invoer en
  nooit fragmenttekst via `${{ }}` in een `run:`-script. *(eigen keuze na spec-review, r1-f14)*
  Een prerelease-tag zonder eigen sectie krijgt een vaste regel als notes en `--prerelease`.
- Herstel na een mislukte build: tag en releasecommit staan dan al op `main`, de fragmenten zijn
  weg. Opnieuw proberen = `workflow_dispatch` van de buildworkflow met die tag; die bouwt opnieuw
  en vervangt de assets van de bestaande release (`gh release upload --clobber`) of maakt de
  release aan als hij nog niet bestaat. `docs/releasing.md` beschrijft dit.
- Een mislukte stap 4 van besluit 5 laat door `--atomic` niets achter: commit en tag landen
  samen of geen van beide. Herstel is een nieuwe run (dispatch `bump: auto`).
- Checkout van precies de tag. Matrix over de vijf doelen, `CGO_ENABLED=0`, `-trimpath`.
- **Uitvoer buiten de checkout.** Gemeten: een ongetrackt bestand in de tree maakt de versie
  `+dirty`, en `version.normalize` maakt daar `dev` van. Een schone, getagde checkout geeft
  zonder ldflags de tag (`lighthouse v0.9.9` in een proef).
- Bestanden: `lighthouse_<os>_<arch>`, met `.exe` voor Windows; losse binaries, geen archieven.
  Plus `SHA256SUMS`.
- Controle in de workflow: de linux/amd64-binary draait `version` en de uitvoer moet
  `lighthouse <tag>` zijn; anders faalt de release.
- Publiceren met `gh release create <tag> --notes-file …` en de assets.
- Bestaande conventies uit `ci.yml`: acties op SHA met versiecommentaar,
  `persist-credentials: false`, `go-version-file: go.mod`, `timeout-minutes`.

### 8. Windows bouwt

`internal/store/lock.go` gebruikt `syscall.Flock` zonder build tag; de cross-build naar
`windows/amd64` faalt nu (`undefined: syscall.Flock`). Splits zoals `internal/usage` al doet
(`lock_flock.go` en `lock_other.go`). Een extra stap in `ci.yml` bouwt `GOOS=windows` en
`GOOS=darwin`, zodat een breuk in de PR boven komt en niet pas bij de release.

### 9. Het beleid

- `docs/releasing.md`, Engels: de semver-tabel, de cadans, dat de workflow tagt (ook een major,
  via `workflow_dispatch` met `bump: major`; nooit een tag met de hand), de bot-uitzondering, de
  fragmenten, het herstel uit besluit 7, en de upgrade voor een gebruiker: nieuwe binary
  downloaden, `lighthouse init --upgrade` in elke repo, de geïnstalleerde kopie via een PR
  committen.
- De regel *Releases* in `AGENTS.md` vervalt. Er komt hooguit één regel voor in de plaats die naar
  `docs/releasing.md` wijst en zegt dat een PR een fragment draagt.
- `docs/install.md`: een sectie "Download a release binary" met `curl`, `SHA256SUMS` en
  `chmod +x`. "macOS is not supported" geldt alleen nog voor `lighthouse serve` (systemd); de CLI
  draait op macOS, de hooks zijn daar nog niet getest (bijvoorbeeld `date -d` in
  `usage-guard.sh` is GNU). Windows: binary bestaat, hooks vragen WSL. De tekst over een private
  repo en `GOPRIVATE` vervalt. *(eigen keuze na spec-review, r1-f15, r1-f16)*

### 10. `CHANGELOG.md` en de eerste release

- `CHANGELOG.md` in de repo-root, Engels, vorm van Keep a Changelog.
- Deze PR schrijft zelf de eerste fragmenten (de releaseworkflow, de binaries, de Windows-fix).
  Wat sinds `v0.1.6` op `main` kwam, komt eenmalig als één `changed`-fragment met een korte
  samenvatting. Oudere tags krijgen één regel: "v0.1.0–v0.1.6: internal pre-releases".
- Deze PR draagt een `fixed`-fragment (de Windows-build) naast `added`-fragmenten. Volgens
  besluit 1 levert de merge dus direct een minor: `v0.2.0`, zonder handwerk. Mislukt dat, dan
  volgt herstel volgens besluit 7.
- Bewijs voor AC 2 en 3 vóór de merge: een tag `v0.2.0-rc.1` op de head van de branch. Een
  tag-push-run gebruikt de workflow uit de getagde commit, dus `build.yml` draait al voor de
  merge. Hij levert een prerelease met vijf binaries, `SHA256SUMS` en een geslaagde
  versiecontrole (`lighthouse v0.2.0-rc.1`). Daarna gaan prerelease en tag weg.
  *(eigen keuze na spec-review, r1-f9)*
- Bewijs voor AC 5: de release `v0.2.0` met vijf binaries en `SHA256SUMS`, de geslaagde
  versiecontrole in de run, en `curl -fsSLO <asset-url>` zonder login met exit 0 (de repo is al
  publiek).

## Aan de bouwer

- Taal en plek van het releasescript (Python naast `ci/`, of Go onder `tools/`), mits getest.
- Of de fragmentcheck een eigen job is of een stap in de Python-job.
- Exacte cron-tijd, mits wekelijks.

## Keuzes van Claude (niet door Corneel besloten)

- Fragmenten per wijziging, meer dan één per PR toegestaan (besluit 2).
- Opt-in via het markeerbestand `changelog.d/.lighthouse`, geen config-sleutel (besluit 3).
- De pijplijn alleen voor lighthouse zelf, niet via `init` naar consumers (non-goals).
- Major alleen via `workflow_dispatch`; herstel via dispatch van de buildworkflow (besluit 5, 7).
- Losse binaries in plaats van archieven; maandag 06:00 UTC; `type: internal` in plaats van een
  label-uitzondering.
- De eerste release komt automatisch bij de merge van deze PR (besluit 10).
- Een prerelease-tag `v0.2.0-rc.1` vóór de merge als bewijs voor AC 2 en 3 (besluit 10).
