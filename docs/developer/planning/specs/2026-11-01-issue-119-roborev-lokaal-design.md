# Roborev lokaal in write-pr, en fix-pr vanuit de dispatcher (#119)

Issue: [#119](https://github.com/CorneeldH/lighthouse/issues/119). Milestone: M2 — Dispatcher.

## Waarom

Een roborev-review die pas in CI draait, komt te laat. Eerst draait CI, dan reviewt roborev, dan
verwerkt een tweede sessie de bevindingen met koude context, en die push start CI opnieuw. Het
voorstel uit CorneeldH/personal_ai_agent#1442: de sessie die het werk deed, reviewt zelf vóór
`gh pr create`. Ze verwerkt de bevindingen in één ronde en opent dan pas de PR. CI draait zo één
keer, op code waarin de review al verwerkt is.

Het tweede deel is het "later" uit de M2-spec
([2026-10-27-m2-dispatcher-design.md](2026-10-27-m2-dispatcher-design.md), *Niet in M2*): een rode
PR wordt nu Teruggegeven, en hoort voortaan een fix-pr-run te starten.

Het spoor van de review is net zo belangrijk als de review zelf. Wat roborev vond en wat ermee
gebeurde, moet per commit terug te vinden zijn, en een script moet het later kunnen tellen
(bijvoorbeeld: welk deel van de `high`-bevindingen was terecht, per maand).

## Doelen

1. `/lighthouse-write-pr` draait de roborev-review lokaal vóór `gh pr create`. Dat gebeurt boven
   de drempel en met Codex-budget, in één ronde.
2. Elke bevinding wordt gefixt of gemotiveerd afgewezen, en dat staat zichtbaar en parseerbaar op
   de PR.
3. Bij `local` gebeurt overslaan nooit stil: de reden staat in de PR-body.
4. De dispatcher start bij een rode PR `/lighthouse-fix-pr`, met een plafond van drie runs.

## Niet-doelen

- **Geen `github`-stand** (review op een GitHub-hosted runner). Een ChatGPT-abonnement als secret
  vergt het terugschrijven van `auth.json` na elke job, met een PAT. Een API-key wordt apart
  betaald, en `codex_usage.py budget` meet dat verbruik niet. Ook geen issue hiervoor, voor nu.
- **Geen self-hosted runner op lighthouse.** Lighthouse is public: een PR vanaf een fork zou code
  draaien op de machine met de Codex-login.
- **Geen roborev-workflow in de pack.** `pack/core/workflows/` krijgt geen `roborev.yml`.
- **Geen re-review**, geen `roborev fix` en geen `roborev refine`, zoals in fix-pr sectie 6.
- **Geen trigger op een onverwerkte roborev-comment in de dispatcher.** Met de lokale stand post
  write-pr die comment altijd zelf, dus een onverwerkte comment ontstaat niet meer. In paia kan
  het nog, bij een PR die niet via write-pr ging. Daar start de gebruiker fix-pr zelf. Dit wijkt
  af van AC 5 in het issue, zie *Afwijkingen van het issue*.

## Anti-doelen

- Een review die stil wegvalt: geen comment, geen reden, en een groene PR die doet alsof er
  gereviewd is.
- Een spoor dat alleen een mens kan lezen, zodat het tellen achteraf handwerk wordt.
- Een fix-pr-lus die het budget leegtrekt op een PR die niet groen wordt.

## Randvoorwaarden

- **Codex blijft alleen reviewer** (No-Go van M2). Roborev gebruikt `codex` als agent.
- **Lighthouse merget nooit.**
- **Skill- en hooktekst in het Engels.** Artefacten zijn Nederlands (comment, PR-body,
  commit-body).
- **Dogfood.** Een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade`, en de
  geïnstalleerde kopie gaat mee in de commit.
- **Het V4-deel van write-pr** mag geen regel bevatten die met `## ` begint
  (`pack/core/skills/write-pr/SKILL.md:189-190`, `tests/pack/test_write_pr_skill.py`).

## Aanpakken

Overwogen, in het gesprek:

| | Aanpak | Afweging |
|---|---|---|
| **A** | **In write-pr, vóór `gh pr create`.** | CI draait één keer, en de context is nog warm. Geen runner en geen fork-risico. Wel duurt elke PR-sessie een paar minuten langer, en de review ziet code die CI nog niet testte. |
| B | Getriggerd door de PR, op een self-hosted runner (zoals paia's Beelink) | Bestaat al. Op public lighthouse draait fork-code op je machine. De verwerking volgt in een tweede sessie, met een tweede CI-run. |
| C | Getriggerd door de PR, via `lighthouse tick` | Geen runner nodig. Wel een koude tweede sessie en twee CI-runs, en het werkt alleen waar de tick draait. |

**Gekozen: A**, omdat alleen A het doel haalt: de review is verwerkt vóórdat CI draait. Paia houdt
zijn Beelink-lane als vangnet voor PR's die niet via write-pr kwamen. Die lane slaat over zodra de
lokaal-marker in de PR-body staat.

## Ontwerp

### 1. Setting

`lighthouse.toml` krijgt een tabel `[review]` met één sleutel:

```toml
[review]
roborev = "local"   # "local" of "off"; zonder sleutel: "off"
```

`.roborev.toml` blijft de plek voor agent, model en review-typen, zoals in paia. De stand `local`
zonder `.roborev.toml` telt als overslaan, met de reden `geen .roborev.toml`. Bij `off` (of zonder
sleutel) slaat write-pr de stap stil over en schrijft geen regel in de body: elke repo met
`pack/core` staat standaard op `off`, en een skipregel in elke PR daar is ruis. *(eigen keuze na
spec-review, r1-f5)*

De waarde-check komt in `.lighthouse/lib/lighthouse_lib/config.py`, als enum-check zoals die van
`forge.kind`; SCHEMA alleen controleert het type. De sleutel krijgt een rij in
`docs/site/data/config.json`, anders wordt `tests/test_site_config.py` rood. *(eigen keuze na
spec-review, r1-f8)*

Lighthouse zelf krijgt `roborev = "local"` en een `.roborev.toml`. Die neemt agent, model en
`review_types` over van paia.

*Eigen keuze (builder-check mag bijsturen): de tabelnaam `[review]` en de sleutel `roborev`.
`"pr"` en `"github"` zijn bewust geen waarden; een onbekende waarde is een configfout.*

### 2. De review in write-pr

Een nieuwe stap in `pack/core/skills/write-pr/SKILL.md`, na V2 (bewijs per AC) en vóór V4 (body en
`gh pr create`). De stap draait alleen bij `roborev = "local"`.

De stap draait ook bij een PR met `No issue:`, waar write-pr V1–V3 overslaat. Doel 3 geldt voor
elke PR. *(eigen keuze na spec-review, r1-f10)*

0. **Al gereviewd?** Bestaat de reviewuitvoer van deze branch al (zie *Uitvoer en herstart*), dan
   draait er geen tweede review en gaat de stap meteen naar V4 en de comment. Write-pr draait
   opnieuw na een blokkade door `pr-create-gate` (`pack/core/skills/write-pr/SKILL.md:20,72-73`).
   Zonder deze stap zou een herstart een tweede Codex-review starten, op HEAD met de fixes erin.
1. **Drempel.** Tel de coderegels van de branch-diff vanaf `git merge-base origin/<default> HEAD`,
   na een `git fetch origin <default>`, zoals de rest van write-pr tegen `origin/<default>` dift.
   Gebruik de regel van paia's `Size`-stap: docs/, binaire bestanden en .md .rst .txt .toml .yaml
   .yml .json .lock .csv .ini .cfg .cff tellen niet; `.github/workflows/` telt wel. Onder de 100
   regels: overslaan.
2. **Budget.** `python3 .lighthouse/scripts/codex_usage.py budget`. Alleen bij besluit `codex` gaat
   de review door. `fallback`, `not_measurable` of een kapotte check: overslaan, met het besluit
   en de reden.
3. **Review.** `roborev ci review --ref <merge-base>..HEAD`, zonder `--comment`. Zo draait de
   review lokaal, zonder daemon, in hetzelfde formaat als de CI-comment van paia. De ruwe output
   gaat naar het bestand uit *Uitvoer en herstart*.

   **De aanroep.** In de voorgrond: een controller-run mag geen achtergrondwerk starten
   (`internal/dispatch/instructions/run.md:56-58`). Buiten de sandbox, want Codex heeft netwerk
   nodig. Gewikkeld in `timeout 540`, met een Bash-timeout van 600000 ms, het maximum van één
   Bash-call. Loopt de review langer dan 9 minuten, dan slaat de stap over met
   `review faalde: timeout na 9 min`. *(eigen keuze na spec-review, r1-f2)*

   **De uitkomst lezen.** roborev v0.69.0 kent vijf koppen van de vorm
   `` ## roborev: <soort> (`<sha>`) ``:

   | Kop | Betekenis voor write-pr |
   |---|---|
   | `Review Complete`, `Combined Review` | review gedaan, met bevindingen: verwerken |
   | `Review Passed` | review gedaan, geen bevindingen: marker en comment met lege lijst |
   | `Review Failed`, `Review Skipped` | overslaan: `review faalde: <kop>` |
   | geen van deze | overslaan: `review faalde: <eerste foutregel>` |

   De exitcode alleen is geen faalsignaal. Paia zet `continue-on-error` op de stap "zodat een
   vondst … de PR niet blokkeert", dus een review met bevindingen kan een exitcode ≠ 0 geven. De
   builder meet de exitcodes van `roborev ci review` en pint deze indeling in de tests van het
   script. *(eigen keuze na spec-review, r1-f1)*
4. **Verwerken, één ronde.** Per bevinding `superpowers:receiving-code-review`: fixen of
   gemotiveerd afwijzen, met dezelfde severity-regels als fix-pr sectie 6 (high en medium serieus,
   een low alleen als die triviaal is). Elke fix is een eigen commit, en de body noemt de
   bevinding: `roborev: <path>:<line> (<severity>)`.
5. **Geen tweede review.**

**Uitvoer en herstart.** Alles komt in `$(git rev-parse --git-dir)/lighthouse/roborev/`: buiten
de werkboom, per worktree uniek, en het overleeft een herstart van write-pr. Twee bestanden per
branch:

- `<branch>.md`: de ruwe output van roborev;
- `<branch>.json`: de geclassificeerde uitkomst. Dat is de gereviewde range plus óf
  `{"uitkomst":"done","sha":"<sha uit de kop>"}` met de besluiten per bevinding (de velden van het
  log-blok), óf `{"uitkomst":"skip","reden":"<reden>"}`.

Write-pr schrijft `<branch>.json` pas als de uitkomst vaststaat: na de classificatie bij een skip,
na de laatste besluit-commit bij `done`. Het script leest alleen dit bestand. `done` geeft
`done: <sha>`, en V4 en de comment gebruiken de opgeslagen besluiten. `skip` geeft
`skip: <reden>`, zodat een herstart dezelfde reden in de body zet. Een gefaalde of afgebroken
review geldt dus nooit als gedaan, en een herstart reviewt niet opnieuw. Een
`<branch>.md` zonder `<branch>.json` (afgebroken tijdens het verwerken) telt als niet
geclassificeerd. Het script geeft dan `classify: <pad naar .md>`, een vierde uitkomst naast
`run`, `skip` en `done`. Stap 0 stuurt die naar classificatie en verwerken (stap 3, de tabel, en
stap 4), zonder nieuwe review. Stap 4 haalt eerst de besluiten terug die al genomen zijn: een
commit op de branch met `roborev: <path>:<line> (<severity>)` in de body telt als `fixed in
<die commit>`. Alleen de overige bevindingen worden opnieuw beoordeeld.
*(eigen keuze na spec-review, r1-f3, r2-f1, r3-f2)*

Drempel en budget komen in één script, zodat skill en tests hetzelfde meten:
`pack/core/scripts/roborev_gate.py` (naam is de keuze van de builder). Het geeft `run` of
`skip: <reden>` en de range, als JSON. Het script draait de review zelf niet; dat blijft één
zichtbaar commando in de skill.

Overslaan heeft vier redenen, elk letterlijk in de PR-body: `geen .roborev.toml`,
`geen roborev of codex op PATH`, `<n> coderegels (drempel 100)` en `Codex-budget: <besluit> -
<reden>`. Een review die faalt, volgens de tabel hierboven, telt ook als overslaan:
`review faalde: <kop, foutregel of timeout>`.

### 3. PR-body: de lokaal-marker

In V4 komt één regel in de body, buiten het Evidence-blok:

- na een review: `<!-- roborev-lokaal: <sha> -->`, met `<sha>` uit de kop van de review;
- bij overslaan: `Roborev overgeslagen: <reden>`.

Paia's lane slaat over zodra `roborev-lokaal:` in de body staat (zie §6).

### 4. PR-comment: tabel en log-blok

Direct na `gh pr create` post write-pr één comment, ook als elke bevinding is afgewezen en er niets
gefixt is. De opbouw:

```markdown
<!-- roborev-verwerkt: <sha> -->
<!-- roborev-log: {"v":1,"sha":"<sha>","bron":"lokaal","pr":<nr>,"bevindingen":[{"path":"internal/x.go","line":12,"severity":"high","uitkomst":"fixed","commit":"<sha>","reden":null}]} -->

| Bevinding | Severity | Uitkomst |
|---|---|---|
| `internal/x.go:12` | high | fixed in <commit> |

<details><summary>Ruwe roborev-output</summary>

…
</details>
```

- `uitkomst` is `fixed`, `refuted` of `left`. Bij `refuted` en `left` staat de reden in `reden`, en
  `commit` is `null`.
- Het log-blok staat op één regel, zodat een parser het met één regex vindt. De renderer
  escapet `<`, `>` en `-` in de JSON als `\u003c`, `\u003e` en `\u002d`. Zo sluit een reden met
  `-->` of een regeleinde het commentaar niet voortijdig af. Een bevinding zonder bestand of
  regel krijgt `path` of `line` als `null`. *(eigen keuze na spec-review, r1-f7)*
- De ruwe output wordt ingekort zodat de comment onder de limiet van GitHub blijft (65536
  tekens). Het log-blok en de tabel worden nooit ingekort. Faalt `gh pr comment`, dan meldt
  write-pr dat, en de bewaarde uitvoer maakt een tweede poging mogelijk. *(eigen keuze na
  spec-review, r1-f7)*
- Zonder bevindingen is de lijst leeg, en de tabel zegt "geen bevindingen".
- Het blok heeft een versieveld `v`, zodat een latere wijziging van de vorm herkenbaar blijft.

**Fix-pr sectie 6 gebruikt dezelfde vorm**, met `"bron":"ci"`. Zo levert elke route hetzelfde
spoor. Write-pr en fix-pr hoeven de vorm daarbij niet elk zelf op te bouwen. Een klein script dat
het blok uit een JSON-bestand rendert ligt voor de hand (keuze van de builder), want dan pinnen de
tests de vorm op één plek.

### 5. Dispatcher: fix-pr bij een rode PR

Nu: `prRed()` (`internal/dispatch/decide.go:466`) geeft een klare run met een open, rode PR terug,
met de reden `reason_pr_red`.

Voortaan:

- **Waar het besluit valt.** Alleen bij het afsluiten van een klare runrij in deze tick, in de
  regel die nu `prRed()` is (`settle` → `runCtx.outcome`, `decide.go:260-299,330-333`). Er komt
  geen losse regel in `issueCtx.next()`. Die zou ook zonder afgelopen run vuren: op een rode PR na
  een Teruggegeven om een andere reden, of na een push van de gebruiker, en dan bij elke tick
  opnieuw. *(eigen keuze na spec-review, r1-f4)*
- **Rood, onder het plafond, verse SHA.** `settle` start zelf niets. Het sluit de rij en geeft
  aan `decideIssue` door dat fix-pr aan de beurt is: PR, head-branch en trigger key. De start komt
  uit `decideIssue`, achter de bestaande poorten (`decide.go:158-195`): alle open rijen sluiten
  deze tick (`free`), geen orphan, het issue is `OPEN` en staat op `Controller`. Daar is het de
  eerste regel vóór `c.next()`. De blokkade-, park- en nachtregels van `next()` gelden net zo.
  - Poorten open: de rij sluit met uitkomst `handoff` en de start van `/lighthouse-fix-pr <pr>`
    volgt in dezelfde tick.
  - Een poort dicht (bijvoorbeeld On hold, of een tweede open rij): geen start. De rij sluit als
    `returned` met de bestaande reden "PR rood", zoals nu. Dat geldt ook voor elke andere uitkomst
    dan een start: parkeren bij een actieve blokkade (`decide.go:961-967`), wachten op de nacht of
    uitstellen. Het signaal leeft maar één tick. Een `handoff`-rij zonder start zou het issue stil
    op een rode PR laten staan. *(eigen keuze na spec-review, r3-f1)*

  Een fix-pr-start ontstaat dus alleen uit het afsluiten van een klare runrij in deze tick, en
  nooit naast een orphan of buiten `Controller`. *(eigen keuze na spec-review, r1-f4, r2-f2)*
- **Trigger** `fixpr`, met als **trigger key** `fixpr:<pr>:<head-sha>`. Een rode staat op dezelfde
  SHA start zo nooit twee keer (`spentKey`, `decide.go:1039`).
- **Plafond: drie fix-pr-runs per PR.** Tel de gesloten runrijen van het issue waarvan het
  commando `/lighthouse-fix-pr <pr>` is en de uitkomst niet `start_failed`. Een fix-pr-run die via
  een handoff startte, telt dus mee; een mislukte start niet. Is de teller drie en de PR nog rood,
  dan sluit de rij als `returned` met de nieuwe reden "PR rood na 3 fix-pr-runs" (`headings.toml`,
  nl en en). *(eigen keuze na spec-review, r1-f6)*
- **Rood op een al gebruikte SHA.** Een fix-pr-run die eindigt zonder push laat dezelfde head-SHA
  achter. Er start dan geen tweede run, en de rij sluit als `returned` met de bestaande reden
  "PR rood". Zijn zowel het plafond als de gebruikte SHA waar, dan wint de plafondreden.
- Een fixture in `internal/dispatch/testdata/decide/` per geval: eerste rode PR geeft een start
  van fix-pr; rood op een al gebruikte SHA geeft Teruggegeven; drie runs geeft Teruggegeven met de
  plafondreden. `settle-red-pr.json` past zich aan aan het nieuwe gedrag.

`run.md` (`internal/dispatch/instructions/run.md:48,65`) noemt nog `/lighthouse-verify-pr`; dat
wordt `/lighthouse-write-pr`. Regel 53-55 over de roborev-run op een groene PR vervalt.

### 6. Paia: de lane slaat over op de lokaal-marker

`personal_ai_agent/.github/workflows/roborev.yml` krijgt vóór `Size` een stap die de PR-body leest.
Staat `roborev-lokaal:` erin, dan slaat de job over met een notice. Dit is een wijziging in een
andere repo van dezelfde eigenaar. Ze wordt een follow-up-issue in paia, en dat issue noemt de
marker en verwijst naar #119. Het hoort niet in de PR van lighthouse. #119 sluit zonder die
wijziging (besluit van de gebruiker). Het issue wordt aangemaakt zodra de markervorm in lighthouse
gemerged is, zodat het naar een vaste vorm verwijst.

### 7. M2-spec bijwerken

In de tabel *Niet in M2* verwijst de rij "Automatische fix-pr en roborev" naar #119. De rij "PR
rood en de sessie is klaar" in de run-uitkomsttabel wordt "start fix-pr, na 3 runs `returned`".

## Afwijkingen van het issue

- **AC 5** vraagt een fix-pr-start bij "een rode PR of een onverwerkte roborev-comment". Dit
  ontwerp doet alleen de rode PR. Met de lokale stand ontstaat geen onverwerkte comment meer; zie
  *Niet-doelen*. De PR-body vermeldt deze afwijking.
- **AC 4** ("de CI-review slaat over op de lokaal-marker") gaat over de lane in paia: lighthouse
  heeft geen CI-review. Dit AC verhuist naar het follow-up-issue in paia van §6. In de PR van #119
  staat het als `Refs`, met een link naar dat issue.

## Bewijs (AC 6)

De PR van #119 zelf ligt boven de 100 coderegels. Na `init --upgrade` draait de nieuwe write-pr
op die PR. Bewijs: de ruwe output en het log-blok in de comment, en precies één CI-run op de
head-SHA, gemeten met `gh run list --commit <sha> --workflow ci.yml`. `cla.yml` en
`chain-roadmap.yml` draaien ook op `pull_request` en tellen niet mee. *(eigen keuze na
spec-review, r1-f9)*

## Aan de builder

- Waar de stap precies in write-pr komt, en hoe hij zich verhoudt tot V2/V3.
- Naam en interface van het drempel/budgetscript en van een eventuele renderer voor het
  comment-blok.
- Hoe de dispatcher de fix-pr-runs per PR telt in `runs.Row`, en hoe de start de head-branch
  vindt.
- De KPI-telling in `measure_fix_pr_kpi.py` als die runs met trigger `fixpr` moet kennen.

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, Ontwerp §5, Rood, onder het plafond, verse SHA): The fix says the blocker, park and night rules of next() apply to the fix-pr start, but it defines only two outcomes: gates open (row closes handoff, start in the same tick) or a gate closed (row closes returned, PR rood). When a next()-style rule parks or defers instead of starting, the row closes as handoff while the start is withheld. The fix-pr signal exists only while settle closes the row in this tick, so no later tick can start it.
- **r3-f2** (MAJOR, Ontwerp §2 stap 0 / Uitvoer en herstart): The fix adds a third state, `<branch>.md` without `<branch>.json`, but neither the script contract nor step 0 routes it. The script reads only `.json` and returns only run, skip or done. Step 0, unchanged, still sends any existing review output straight to V4. The fix-commit decisions made before the abort are not persisted anywhere, yet the resumed run must re-process the findings.
