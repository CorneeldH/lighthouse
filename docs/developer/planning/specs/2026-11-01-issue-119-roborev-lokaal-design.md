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
3. Overslaan gebeurt nooit stil: de reden staat in de PR-body.
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
zonder `.roborev.toml` telt als overslaan, met de reden `geen .roborev.toml`.

Lighthouse zelf krijgt `roborev = "local"` en een `.roborev.toml`. Die neemt agent, model en
`review_types` over van paia.

*Eigen keuze (builder-check mag bijsturen): de tabelnaam `[review]` en de sleutel `roborev`.
`"pr"` en `"github"` zijn bewust geen waarden; een onbekende waarde is een configfout.*

### 2. De review in write-pr

Een nieuwe stap in `pack/core/skills/write-pr/SKILL.md`, na V2 (bewijs per AC) en vóór V4 (body en
`gh pr create`). De stap draait alleen bij `roborev = "local"`.

1. **Drempel.** Tel de coderegels van de branch-diff vanaf de merge-base met de default branch.
   Gebruik de regel van paia's `Size`-stap: docs/, binaire bestanden en .md .rst .txt .toml .yaml
   .yml .json .lock .csv .ini .cfg .cff tellen niet; `.github/workflows/` telt wel. Onder de 100
   regels: overslaan.
2. **Budget.** `python3 .lighthouse/scripts/codex_usage.py budget`. Alleen bij besluit `codex` gaat
   de review door. `fallback`, `not_measurable` of een kapotte check: overslaan, met het besluit
   en de reden.
3. **Review.** `roborev ci review --ref <merge-base>..HEAD`, zonder `--comment`. Zo draait de
   review lokaal, zonder daemon, in hetzelfde formaat als de CI-comment van paia: de kop
   `` ## roborev: Review Complete (`<sha>`) `` en dezelfde bevindingen. De ruwe output gaat naar
   een bestand.
4. **Verwerken, één ronde.** Per bevinding `superpowers:receiving-code-review`: fixen of
   gemotiveerd afwijzen, met dezelfde severity-regels als fix-pr sectie 6 (high en medium serieus,
   een low alleen als die triviaal is). Elke fix is een eigen commit, en de body noemt de
   bevinding: `roborev: <path>:<line> (<severity>)`.
5. **Geen tweede review.**

Drempel en budget komen in één script, zodat skill en tests hetzelfde meten:
`pack/core/scripts/roborev_gate.py` (naam is de keuze van de builder). Het geeft `run` of
`skip: <reden>` en de range, als JSON. Het script draait de review zelf niet; dat blijft één
zichtbaar commando in de skill.

Overslaan heeft vijf redenen, elk letterlijk in de PR-body: `roborev uit`, `geen .roborev.toml`,
`geen roborev of codex op PATH`, `<n> coderegels (drempel 100)`, `Codex-budget: <besluit> -
<reden>`. Een review die faalt (exit ≠ 0, geen leesbare kop) telt ook als overslaan:
`review faalde: <eerste foutregel>`.

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
- Het log-blok staat op één regel, zodat een parser het met één regex vindt.
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

- Een klare run met een open PR waarvan de checks `FAILURE` of `ERROR` zijn, sluit af, en de
  dispatcher start een nieuwe run met `/lighthouse-fix-pr <pr>` op de head-branch van de PR.
- **Trigger** `fixpr`, met als **trigger key** `fixpr:<pr>:<head-sha>`. Een rode staat op dezelfde
  SHA start zo nooit twee keer (`spentKey`, `decide.go:1039`).
- **Plafond: drie fix-pr-runs per PR.** Tel de runrijen met trigger `fixpr` voor die PR. Staat de
  teller op drie en is de PR nog rood, dan volgt een Teruggegeven met een nieuwe reden: "PR rood na
  3 fix-pr-runs" (`headings.toml`, nl en en).
- Een fix-pr-run die eindigt zonder push, met dezelfde head-SHA, telt mee in het plafond. Er start
  geen tweede run op die SHA, dus de rode PR wordt Teruggegeven met de bestaande reden "PR rood".
- Een fixture in `internal/dispatch/testdata/decide/` per geval: eerste rode PR geeft een start
  van fix-pr; rood op een al gebruikte SHA geeft Teruggegeven; drie runs geeft Teruggegeven met de
  plafondreden. `settle-red-pr.json` past zich aan aan het nieuwe gedrag.

`run.md` (`internal/dispatch/instructions/run.md:48,65`) noemt nog `/lighthouse-verify-pr`; dat
wordt `/lighthouse-write-pr`. Regel 53-55 over de roborev-run op een groene PR vervalt.

### 6. Paia: de lane slaat over op de lokaal-marker

`personal_ai_agent/.github/workflows/roborev.yml` krijgt vóór `Size` een stap die de PR-body leest.
Staat `roborev-lokaal:` erin, dan slaat de job over met een notice. Dit is een wijziging in een
andere repo van dezelfde eigenaar. Ze komt als eigen PR in paia (`Refs` naar #119) en hoort niet
in de PR van lighthouse.

### 7. M2-spec bijwerken

In de tabel *Niet in M2* verwijst de rij "Automatische fix-pr en roborev" naar #119. De rij "PR
rood en de sessie is klaar" in de run-uitkomsttabel wordt "start fix-pr, na 3 runs `returned`".

## Afwijkingen van het issue

- **AC 5** vraagt een fix-pr-start bij "een rode PR of een onverwerkte roborev-comment". Dit
  ontwerp doet alleen de rode PR. Met de lokale stand ontstaat geen onverwerkte comment meer; zie
  *Niet-doelen*. De PR-body vermeldt deze afwijking.
- **AC 4** ("de CI-review slaat over op de lokaal-marker") gaat over de lane in paia: lighthouse
  heeft geen CI-review. Bewijs komt uit de paia-PR van §6.

## Bewijs (AC 6)

De PR van #119 zelf ligt boven de 100 coderegels. Na `init --upgrade` draait de nieuwe write-pr
op die PR. Bewijs: de ruwe output en het log-blok in de comment, en precies één CI-run op de
head-SHA, gemeten met `gh run list --commit <sha>`.

## Aan de builder

- Waar de stap precies in write-pr komt, en hoe hij zich verhoudt tot V2/V3.
- Naam en interface van het drempel/budgetscript en van een eventuele renderer voor het
  comment-blok.
- Hoe de dispatcher de fix-pr-runs per PR telt in `runs.Row`, en hoe de start de head-branch
  vindt.
- De KPI-telling in `measure_fix_pr_kpi.py` als die runs met trigger `fixpr` moet kennen.
