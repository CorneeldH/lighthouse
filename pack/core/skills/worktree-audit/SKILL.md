---
name: paia-worktree-audit
description: Audit worktrees, lokale en remote branches, PR-status en sessies van CorneeldH/personal_ai_agent in één tabel met een verdict per regel. Gebruik bij "welke worktrees kunnen weg", "ruim de worktrees op", "welke branches op origin zijn gemerged", "was er nog een sessie open op een worktree", "waar was ik gebleven op X", "hoeveel worktrees staan er", of /paia-worktree-audit. Rapporteert en levert commando's; muteert zelf niets. LET OP — gaat het over milestones, labels of de board-status, gebruik dan paia-board-sweep; gaat het over het aanmaken van een nieuwe worktree in plaats van het opruimen ervan, gebruik dan de skill using-git-worktrees.
metadata:
  domain: factory
---

# Worktree Audit Skill

Eén join over `git worktree list`, `git branch` (lokaal + remote), de PR-status per headbranch
en de sessies op schijf, met per regel een verdict. Vijf views op dezelfde rijenset.

```bash
uv run python scripts/worktree_audit.py            # --cleanup (default)
uv run python scripts/worktree_audit.py --sessions
uv run python scripts/worktree_audit.py --origin
uv run python scripts/worktree_audit.py --kpi
uv run python scripts/worktree_audit.py --only <branch>
```

`--only` is de view voor `paia-close-issue` (#1422): één rij plus een regel
`opruimen: ja|nee — <reden>`, exit 0 alleen bij `ja`. Die skill voert de geleverde regels uit
na bevestiging; deze skill en het script muteren nog steeds niets.

## When to Use

- De worktrees zijn weer opgelopen ("welke kunnen weg?").
- Na een reeks gemergede PR's, om origin op te ruimen.
- "Was er nog een sessie open op een worktree?" / "waar was ik gebleven?" → `--sessions`.
- Periodiek, als opruimronde; rij 2 van de KPI-tabel is de trigger.

Niet gebruiken om een worktree te *maken* — dat is `superpowers:using-git-worktrees`.

## Harde regel

**Deze skill muteert niets.** Geen `git push origin --delete`, geen `git branch -D`, geen
`git worktree remove`, geen `unlock`, geen `kill`, geen schrijven naar `.port-registry`.
Het script drukt kopieerbare commando's af; de mens plakt. De menselijke poort staat één keer,
vlak vóór het onomkeerbare — `git push origin --delete` is vanuit een script niet terug te draaien.

`run_git` in het script weigert elke niet-lezende git-verb, dus die norm zit in het uitvoerpad
en niet alleen in deze tekst.

## Vier dingen die `git worktree prune` niet afvangt

1. **Squash-merge maakt `git branch --merged` een leugen.** Alles gaat squash-gemerged naar
   `main`, dus de originele commits zitten niet in main's historie. De PR-lookup op de
   headbranch is de enige betrouwbare test.
2. **Ongepushte commits.** Werk dat nergens anders bestaat. Het script telt met
   `git rev-list --count <b> --not --remotes` — "bestaat nergens anders", niet "niet gemerged".
   **Die telling is bij een squash-merge per constructie de hele branchhistorie**: GitHub gooit
   de losse commits weg en zet één nieuwe commit op `main`, dus de originelen bereiken nooit een
   remote. Daarom kijkt rang 4 ook naar het merge-moment; zie de ladder hieronder.
3. **Fixture-branches uit een mislukte testrun.** `tests/integration/test_pre_push_checkpoint_scan.sh`
   maakt zijn fixtures met `git checkout --orphan` in een `mktemp -d`-repo. Faalt die `mktemp`
   (een gesandboxte shell), dan landen ze in de **echte** repo — zo ontstonden `branch-dirty` en
   `branch-allowlisted`. Ze lezen als gewoon ongepusht werk; de ladder benoemt ze apart.
4. **Gelockte worktrees.** Een lock kan achterblijven ná de sessie die hem zette. Melden,
   niet forceren. **De permission-classifier weigert in auto-mode `git worktree unlock`
   en `git worktree remove` op een gelockte worktree.** Ruim dan niet op; meld het. Een
   lock met een dode pid zakt door de ladder met de aantekening "verweesde lock";
   ontgrendelen doet de skill nooit.

## De verdict-ladder — eerste treffer wint

| # | Voorwaarde | Verdict |
|---|---|---|
| 1 | naam of map op de protect-lijst | 🔒 nooit aanraken |
| 2 | worktree `locked` én lock-pid leeft in `/proc` (of pid onbekend), óf een lopende sessie heeft de worktree als cwd | ⛔ actieve sessie / lopende sessie |
| 3 | vuile tree, of tree-status onleesbaar | ⛔ eerst wegschrijven |
| 4a | ongepushte commits, tip zonder ouder, lokaal, geen remote, geen PR | ⚠️ wees-commit zonder ouder — vermoedelijk testlekkage |
| 4b | ongepushte commits terwijl de PR gemerged is én de tip vóór de merge ligt | *geen treffer* — valt door naar rang 6 met modifier `[squash-merge]` |
| 4c | ongepushte commits (`--not --remotes` > 0), of aantal onbekend | ⚠️ controleren |
| 5 | open PR | ⏸️ PR open |
| 6 | merged PR | ✅ veilig weg |
| 7 | geen open/merged PR, en PR-status onbekend óf inhoudstoets mislukt óf unieke bestanden t.o.v. `main` | ⚠️ PR-status onbekend / inhoudstoets mislukt / uniek werk — bekijken |
| 8 | geen PR, inhoud zit in `main` — of er is geen inhoud (`main..<branch>` telt 0) | ✅ veilig weg / ✅ leeg — geen eigen commits |
| 9 | map onder `.claude/worktrees/` niet in `git worktree list` | ❓ wees — handmatig |

Rang 9 ontstaat typisch in de devcontainer: de `.git` van zo'n map wijst naar
`/workspaces/...`, dus git kent hem niet en `git worktree prune` raakt hem nooit. Alleen
`git diff --name-only main...<branch>` plus `git cat-file -e main:<f>` per bestand vindt wat
er echt alleen daar staat.

Rang 8 kent twee teksten, want "niets uniek" heeft twee oorzaken. Of het werk landde in `main`,
of er was nooit werk: een branch die is aangemaakt en nooit gecommit. Die tweede heet
**leeg — geen eigen commits**. Zo las issue-941 eerst als "inhoud in `main`", terwijl er niets
van in `main` stond. Beide zijn veilig weg: de tree is schoon (rang 3), dus er gaat niets
verloren. Maar een lege worktree is vaak een sessie die nog moet beginnen. Kijk bij een verse
*laatste sessie* even of iemand er net aan begint.

**Worktrees van de sessie-server** (`claude-remote-control.service`, #963) krijgen nooit een
PR. Zolang een sessie ze als cwd heeft: rang 2. Daarna: rang 8 zonder eigen commits, rang 4
met. Pad en branch volgen het vaste patroon `.claude/worktrees/bridge-cse_…` /
`worktree-bridge-cse_…`; de volle vorm (pad, branch, lock) staat in
`deploy/beelink/remote-surfaces-verification.md`, sectie R1. Hun lock (`claude agent bridge-…`)
draagt de pid van de server, niet van de sessie; de audit telt hem daarom niet als
sessiesignaal (modifier `server-lock`). Zo'n ✅-rij is nog gelockt, dus hij staat niet in de
opruimcommando's maar in een eigen sectie: de gebruiker draait met de hand `git worktree unlock`
en ruimt daarna op, of laat hem staan tot de volgende serverstart. Een serverstop laat de lock
staan (dode pid, R1-einde); de volgende start van de server heft de locks van eerdere
serverprocessen op (gemeten 2026-09-25, sectie R1).

Rang 3 en 4 staan **boven** rang 6: een gemergede branch met ongepushte commits krijgt nooit plat
"veilig weg". Dat is het #544-geval.

**4b is de uitzondering die #544 juist overeind houdt.** De vraag van rang 4 is "draagt deze branch
werk dat nergens anders bestaat". Bij een squash-merge beantwoordt `--not --remotes` die vraag
verkeerd: hij telt dan altijd de hele historie, ook als elke regel netjes in `main` staat. Op deze
repo maakte dat 9 van de 14 ⚠️-rijen vals, en omdat rang 4 bóven de inhoudstoets van rang 7/8 staat
werden die branches nooit op inhoud gemeten. De vervangende maat is smal: **is er ná het
merge-moment nog doorgecommit?** Zo nee, dan landde alles in de squash-commit en zakt de rij naar
rang 6 met de modifier `[squash-merge]` erbij, zodat zichtbaar blijft waaróm. Zo ja — of ontbreekt
een van beide tijdstempels — dan blijft rang 4 staan. Dat laatste is precies #544: doorgecommit ná
de merge is echt werk.

Beide tijdstempels zijn **epoch-seconden**, en dat is geen implementatiedetail. `merged_at` komt van
GitHub in UTC ("Z"), de commitdatum van git met een lokale offset. Als tekst vergeleken leest
`08:24+02:00` als *later* dan `08:08Z` terwijl het twee uur eerder is — dat gaf een volle tabel valse
"commit na merge"-regels voordat deze maat bestond. Daarom `git rev-list -1 --timestamp` (epoch, geen
offset) en niet `log --format=%cI`.

**4a benoemt, het geeft geen toestemming.** Het verdict blijft ⚠️ en dus nooit "veilig weg" — een
heuristiek mag in deze skill niet zelf tot verwijderen besluiten. De toets is bewust smal: één commit
die zijn eigen root is. Een pre-rewrite branch als `v2.1-mac-thin-client` heeft óók een losse
historie t.o.v. `main`, maar wél ouders, en valt er daarom buiten.

## De protect-lijst

```python
PROTECTED_REFS  = ("main", "entire/*")     # 78 lokale checkpointrefs (2026-08-24)
PROTECTED_PATHS = (".port-registry",)      # + elke map die er één bevat
```

`entire/*` is de checkpointstate van de entire CLI; `.port-registry` is de poortregistratie die
`scripts/opencode-lane/worktree-run.sh` gebruikt om per worktree-naam een reproduceerbaar
poortblok uit te delen. Beide hebben geen PR, zijn niet gemerged, en worden niet gemist tot iets
stukgaat. Het is een constante met een test eromheen, geen instelling: een instelling die je kunt
vergeten in te vullen is geen bescherming.

`tests/unit/test_worktree_audit.py::TestProtectListBites` bewaakt hem.

## Non-negotiables

- **Niets muteren**, ook niet "even dit ene voor de hand liggende geval".
- **`gh api`, nooit `gh pr list --json`** — dat breekt onder de rtk-proxy, en een onbekend
  filter wordt daar stil genegeerd. In Claude Code draait `gh` met
  `dangerouslyDisableSandbox: true`.
- **Nooit `git branch --merged`** als merge-test.
- **Een lock nooit forceren of ontgrendelen.** Rapporteer de pid erbij.
- **Poortblokken worden gemeld, nooit gekild.** De skill toont welk blok op welke naam
  geregistreerd staat; welk *proces* die poort vasthoudt zoekt hij **niet** op — daarvoor
  zou hij de poorten moeten scannen. Lees een ontbrekende pid dus niet als "er draait
  niets". Dat stuk, en het killen van dev-servers die aan een verwijderd pad hangen, is
  **#438**; deze skill is daar de detectiehelft van, niet de guard.
- **Ongecommit werk eerst wegschrijven** vóór iemand een `worktree remove --force` plakt.

## KPI's

Alle vier komen uit dezelfde aanroep: `uv run python scripts/worktree_audit.py --kpi`.

| Metric | Veld | 2026-08-27 | 2026-08-28 | Target |
|---|---|---:|---:|---:|
| Worktrees onder `.claude/worktrees/` | `worktrees=` | 12 | 7 | ≤ 4 |
| Rijen met verdict ✅ die blijven staan | `safe=` | 86 | 8 | 0 |
| Remote branches op origin | `remote_branches=` | 95 | 8 | ≤ 15 |
| Rijen met ⚠️ (uniek werk of lokale commits) | `warn=` | 11 | 6 | 0 |

Elke kolom komt uit één `--kpi`-aanroep, ná `git fetch --prune`; het zijn metingen, geen handtellingen. Ze bewegen ook tussen twee aanroepen door als een andere sessie intussen commit — dat is de drift die deze skill zichtbaar maakt, geen meetfout.

De kolom 2026-08-28 is ná een opruimronde **en** ná rang 4b. Die twee zijn niet te scheiden in `warn=`: van de 14 ⚠️-rijen van die ochtend waren er 9 het squash-artefact, en 8 daarvan lezen nu als ✅ `[squash-merge]`. Wie de daling van 11 → 6 leest als "er is opgeruimd" heeft dus maar de helft van het verhaal; `safe=` telt sindsdien rijen die eerder onzichtbaar waren.

Rij 2 is de eigenlijke maat. Hij zakt naar nul zodra het commandoblok geplakt is en loopt daarna
vanzelf weer op; dat oplopen ís het signaal om deze skill te draaien.

## Output-format

`--cleanup` opent met de tabel uit issue #665 en sluit af met kopieerbare blokken:

```
| worktree | branch | PR-state | ongepushte commits | vuile tree | laatste sessie | live? | verdict |
```

Daarna, alleen gevuld met rijen die verdict ✅ dragen, in deze volgorde — nooit omgekeerd:

```bash
git worktree remove <pad>
git branch -D <branch> <branch> ...
git push origin --delete <branch> <branch> ...
```

De volgorde is geen smaak maar plakbaarheid: een branch die in een worktree is uitgecheckt kan
git niet verwijderen ("Cannot delete branch … checked out at …"), dus `worktree remove` moet vóór
`branch -D` staan — de omgekeerde volgorde breekt een top-naar-onder paste halverwege.

Rapporteer aan de gebruiker de rijen die **niet** ✅ zijn met hun reden — dat is de signaal-helft.
Een lijst van 84 correcte ✅-regels uitschrijven is ruis; noem het aantal en plak het blok.

## Overlap

- **agentsview** (`docs/developer/planning/specs/2026-08-16-agentsview-sessie-inventaris-design.md`)
  beantwoordt "waar deed ik X ook alweer" uit geïndexeerde transcript-inhoud. Deze skill
  beantwoordt "wat kan weg" uit git-state. Het enige raakvlak is de kolom *laatste sessie*, en die
  kost hier één `stat` — geen API-koppeling waard.
- **#438** dekt het killen van dev-servers aan een verwijderd worktree-pad. Deze skill detecteert,
  killt niet.
