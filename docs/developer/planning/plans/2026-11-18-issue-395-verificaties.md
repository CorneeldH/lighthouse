# Verificaties en nulmeting van de REST-forge (#395)

Status: **vastgesteld**. De rollup-regel is gekozen (Step 6): de aangepaste regel, met per
(app, naam) alleen de nieuwste check-run. De eerste meting (op #421, 2026-11-18) verwierp de
kandidaatregel; de tweede meting (op #423, 2026-11-19) en het bewijs op `9941f44a` bevestigen de
aangepaste regel. Gemeten met het token van de controller (`EnvironmentFile` van
`lighthouse-tick.service`: `~/.config/paia-controller/env`, klassieke PAT, scopes `project,
read:discussion, read:org, repo`).

De PR voor de meting en het werk: **#423** (`issue-395-forge-rest`, gewone PR, `Refs #395`). Hij
vervangt #421, die met de spec, het plan en het concept van dit bestand gemerged is.

## Step 1: nulmeting GraphQL

Kosten per query (`rateLimit { cost }`, echte variabelen):

| Query | Kosten | Opmerking |
|---|---|---|
| `BoardQuery`, één pagina | 14 | 100 items, `hasNextPage: true`; het board heeft 204 items, dus 3 pagina's per volle lezing (≈ 42 punten). Het commentaar in `board.go` noemt 53 per pagina; dat klopt nu niet meer. |
| `BranchPRsQuery` | 1 | branch `issue-395-forge-rest` |
| `IssueItemsQuery` | 1 | issue 395 |

Verbruik: `gh api rate_limit --jq .resources.graphql.used` elke minuut, van 15:45:01 tot 15:54:05
(10 samples, **9 minuten in plaats van 15**: de achtergrondlus stopte na 15:54).

| tijd | used |
|---|---|
| 15:45:01 | 463 |
| 15:46:02 | 496 |
| 15:47:02 | 553 |
| 15:48:03 | 568 |
| 15:49:03 | 621 |
| 15:50:03 | 665 |
| 15:51:04 | 860 |
| 15:52:04 | 882 |
| 15:53:05 | 898 |
| 15:54:05 | 926 |

- +463 in 9 min 4 s. Eigen GraphQL-meetverkeer in dat venster ≈ 21 punten (de drie queries hierboven
  een paar keer). Rest ≈ 442 punten, ≈ **730 per kwartier** op dit token.
- Ticks in het venster: 15:45:01 (klaar 15:46:00), 15:48:04 (faalde: `forge POST
  https://api.github.com/graphql: status 504` op `personal_ai_agent`), 15:51:04 (klaar 15:51:50),
  15:54:04 (klaar 15:54:52, na het laatste sample). Drie volle ticks plus één mislukte; de timer
  loopt elke 3 minuten en een tick dekt twee repos (`lighthouse` en `personal_ai_agent`).
- Het verbruik is niet alleen van de controller: er liepen drie open runs (#121, #285,
  `personal_ai_agent`#1779) op hetzelfde account. De sprongen per minuut (33, 57, 15, 53, 44, 195, 22,
  16, 28) vallen niet op de tickmomenten.

**Twee GraphQL-tellers.** Met hetzelfde token geeft `rateLimit` in een GraphQL-antwoord wisselend
een van twee tellers: teller A (`used` 463→926, reset 16:34:09 CET, dezelfde als `/rate_limit`) en
teller B (`used` 1748 om 15:45:58 → 3190 om 15:54:06, reset 16:31:10 CET). Teller B liep +1442 in
8 min (≈ 2660 per kwartier). Welke query op welke teller valt, wisselt per request (`BranchPRs` en
`IssueItems` zagen beide tellers). `/rate_limit` toont alleen teller A. Een query met alleen
`rateLimit` kost niets.

## Step 2: gearchiveerde items

Item `PVTI_lAHOAGmU3s4BnmdyzhBnXhM` (id 275209747, issue #3, gesloten, Done). Gearchiveerd met
`gh project item-archive`, daarna:

- gewone lijst (`items?per_page=100`, gepagineerd): item **niet** aanwezig; 203 items.
- `items?per_page=100&q=is:archived`: item **niet** aanwezig; 203 items, dezelfde set als de gewone
  lijst. `q` werkt wel (`status:Done` 144, `is:open` 60), `is:archived` wordt genegeerd.
- `GET users/CorneeldH/projectsV2/2/items/275209747`: geeft het item, met
  `archived_at: "2026-11-18T16:40:37Z"`.

Teruggezet met `--undo`; daarna `archived_at: null`, weer in de gewone lijst (204 items).

**Pad B**: geen van beide lijsten geeft een gearchiveerd item. Wel geeft `GET …/items/{id}` het
item met `archived_at`, als de numerieke id bekend is.

## Step 3: read-after-write

Item 275209734 (issue #1, Done, pagina 1). Geen item op pagina 1 had een Priority; de beginwaarde
was `null`.

| lezing | status | nieuwe waarde (P2) zichtbaar |
|---|---|---|
| lijst vóór de PATCH (ETag `W/"3728…"`) | 200 | n.v.t. (`null`) |
| `PATCH …/items/275209734` met `{"fields":[{"id":428493778,"value":"321d79fc"}]}` | 200 | — |
| lijst direct daarna met `If-None-Match: <oude etag>` | **200**, nieuwe ETag | **ja** |
| `GET …/items/275209734?fields=428493778` | 200 | **ja** |

Teruggezet met `PATCH` en `"value": null` (200, `value: null`); daarna geverifieerd met een `GET`.
Controle: dezelfde lijst twee keer met de verse ETag geeft 304; drie 304's na elkaar verhoogden
`X-RateLimit-Used` (core) niet.

## Step 4: blokkades

`issue_dependencies_summary.total_blocked_by > blocked_by` bij 54 issues. `GET
repos/CorneeldH/lighthouse/issues/<n>/dependencies/blocked_by`:

- #380 (`blocked_by: 2`, `total_blocked_by: 3`): 3 blokkades, waarvan #325 `closed`.
- #285 (`blocked_by: 0`, `total_blocked_by: 10`): 10 blokkades (#275–#284), alle `closed`.

Gesloten blokkades **staan erin**.

## Step 5: check-rollup

### Eerste meting: PR #421 (kandidaatregel, verworpen)

Head `9941f44a70a663647dfa7c90adf1a928aedd1cff` (lege commit), gepusht om 17:42:49. Elke ~12 s
gesampled tot alle checks klaar waren. Regel = de kandidaatregel uit het plan, met de hand
toegepast; `statuses[]` was in elk sample leeg (`total_count: 0`).

| tijd | statusCheckRollup.state | regel | runs (klaar/totaal) | geannuleerd | statuses (total_count) | suites wachtend / totaal | opmerking |
|---|---|---|---|---|---|---|---|
| 17:42:49 | "" (null) | SUCCESS | 0/0 | 0 | 0 | 0 / 1 (claude:queued) | regel SUCCESS, GraphQL niet |
| 17:43:01 | PENDING | PENDING | 0/5 | 0 | 0 | 3 / 4 (claude:queued, github-actions:in_progress ×3) |  |
| 17:43:14 | PENDING | PENDING | 9/14 | 0 | 0 | 1 / 4 (claude:queued, github-actions:in_progress) |  |
| 17:43:26 | PENDING | PENDING | 9/14 | 0 | 0 | 1 / 4 (claude:queued, github-actions:in_progress) |  |
| 17:43:38 | PENDING | PENDING | 9/14 | 0 | 0 | 1 / 4 (claude:queued, github-actions:in_progress) |  |
| 17:43:50 | PENDING | FAILURE | 12/16 | 1 | 0 | 2 / 7 (claude:queued, github-actions:in_progress, github-actions:pending) | afwijking |
| 17:44:02 | PENDING | FAILURE | 16/19 | 5 | 0 | 1 / 7 (claude:queued, github-actions:in_progress) | afwijking |
| 17:44:15 | PENDING | FAILURE | 23/28 | 5 | 0 | 1 / 7 (claude:queued, github-actions:in_progress) | afwijking |
| 17:44:28 | PENDING | FAILURE | 23/28 | 5 | 0 | 1 / 7 (claude:queued, github-actions:in_progress) | afwijking |
| 17:44:40 | PENDING | FAILURE | 24/28 | 5 | 0 | 1 / 7 (claude:queued, github-actions:in_progress) | afwijking |
| 17:44:52 | PENDING | FAILURE | 24/28 | 5 | 0 | 1 / 7 (claude:queued, github-actions:in_progress) | afwijking |
| 17:45:04 | PENDING | FAILURE | 25/28 | 5 | 0 | 1 / 7 (claude:queued, github-actions:in_progress) | afwijking |
| 17:45:17 | SUCCESS | FAILURE | 28/28 | 5 | 0 | 0 / 7 (claude:queued) | afwijking |
| 17:45:29 | SUCCESS | FAILURE | 28/28 | 5 | 0 | 0 / 7 (claude:queued) | afwijking |
| 17:45:45 | SUCCESS | FAILURE | 28/28 | 5 | 0 | 0 / 7 (claude:queued) | afwijking |

Oorzaak van de afwijking: op dezelfde head liepen twee `pull_request`-runs van `ci` (16:42:54 UTC,
van het openen van de PR, en 16:43:41 UTC, van de push vlak daarna). `concurrency:
cancel-in-progress` annuleerde de eerste: suite 120055494192, `conclusion: cancelled`, 12 runs,
waarvan 5 `cancelled`. `check-runs` (ook met de standaard `filter=latest`) geeft beide sets (28
runs, 14 namen dubbel). `statusCheckRollup { state }` negeert de vervangen suite en geeft `SUCCESS`.

Twee bijvangsten:

- Vraagt de query ook `contexts`, dan geeft dezelfde `statusCheckRollup.state` `FAILURE`
  (reproduceerbaar, 28 contexts, 5 `CANCELLED`). Alleen `state` geeft `SUCCESS`; dat is wat
  `prFields` vraagt.
- Het eerste sample: direct na de push bestaat alleen de suite van de app `claude` (`queued`, 0
  runs), nog geen Actions-suite. De regel geeft dan `SUCCESS` (niets faalt, niets wacht), GraphQL
  `null`.

Op de laatste samples geeft de regel per naam de nieuwste run (hoogste `id`) wel `SUCCESS`:
14 namen, alle `success`/`skipped`. Dat was toen niet over de tussenliggende samples bewezen; dat
doet de tweede meting.

### Tweede meting: PR #423 (aangepaste regel)

PR **#423** geopend om 18:47:16 (2026-11-19, CET) met head `72a9ef72` (het changelog-fragment).
Sampler gestart om 18:47:21, elke 10 s, in de voorgrond. Lege commit 1 (`6f6f3f60`) gepusht van
18:48:11 tot 18:48:14; lege commit 2 (`432b4247`) van 18:49:01 tot 18:49:04. Zo komt het patroon
openen-dan-pushen twee keer voor. De sampler stopte om 18:51:12: de laatste head had alle runs
klaar, geen wachtende suite, en daarna nog twee samples.

Per sample: de `head.sha` van de PR (REST), GraphQL `commits(last:1){…{oid statusCheckRollup{state}}}`
(alleen `oid` en `state`), en voor die head `check-runs` (alle pagina's), `statuses[]` van
`…/status` en `check-suites`. Beide regels door een script toegepast. `statuses[]` was in elk
sample leeg (`total_count: 0`). Kolom *runs* telt de gehouden runs (na ontdubbelen); in deze meting
had geen head dubbele namen, dus gehouden = alle runs.

| tijd | head | GraphQL `state` | aangepaste regel | oude regel | runs klaar/totaal | suites wachtend/totaal | opmerking |
|---|---|---|---|---|---|---|---|
| 18:47:22 | `72a9ef7` | PENDING | PENDING | PENDING | 0/5 | 3/4 |  |
| 18:47:32 | `72a9ef7` | PENDING | PENDING | PENDING | 0/5 | 3/4 |  |
| 18:47:42 | `72a9ef7` | PENDING | PENDING | PENDING | 9/14 | 1/4 |  |
| 18:47:52 | `72a9ef7` | PENDING | PENDING | PENDING | 9/14 | 1/4 |  |
| 18:48:02 | `72a9ef7` | PENDING | PENDING | PENDING | 9/14 | 1/4 |  |
| 18:48:15 | `72a9ef7` | PENDING | PENDING | PENDING | 10/14 | 1/4 |  |
| 18:48:22 | `6f6f3f6` | "" (null) | PENDING | PENDING | 0/1 | 3/4 | afwijking, fail-closed: 1 run `queued`, Actions-suites `queued` |
| 18:48:32 | `6f6f3f6` | PENDING | PENDING | PENDING | 0/2 | 2/4 |  |
| 18:48:42 | `6f6f3f6` | PENDING | PENDING | PENDING | 2/5 | 1/4 |  |
| 18:48:52 | `6f6f3f6` | PENDING | PENDING | PENDING | 8/14 | 1/4 |  |
| 18:49:04 | `6f6f3f6` | "" (null) | PENDING | PENDING | 8/14 | 1/4 | GraphQL ziet al `432b424`: telt niet mee |
| 18:49:12 | `432b424` | PENDING | PENDING | PENDING | 0/2 | 3/4 |  |
| 18:49:22 | `432b424` | PENDING | PENDING | PENDING | 1/2 | 2/4 |  |
| 18:49:32 | `432b424` | PENDING | PENDING | PENDING | 2/5 | 1/4 |  |
| 18:49:42 | `432b424` | PENDING | PENDING | PENDING | 9/14 | 1/4 |  |
| 18:49:52 | `432b424` | PENDING | PENDING | PENDING | 9/14 | 1/4 |  |
| 18:50:02 | `432b424` | PENDING | PENDING | PENDING | 9/14 | 1/4 |  |
| 18:50:12 | `432b424` | PENDING | PENDING | PENDING | 10/14 | 1/4 |  |
| 18:50:22 | `432b424` | PENDING | PENDING | PENDING | 10/14 | 1/4 |  |
| 18:50:33 | `432b424` | PENDING | PENDING | PENDING | 11/14 | 1/4 |  |
| 18:50:42 | `432b424` | PENDING | PENDING | PENDING | 13/14 | 1/4 |  |
| 18:50:52 | `432b424` | SUCCESS | SUCCESS | SUCCESS | 14/14 | 0/4 |  |
| 18:51:02 | `432b424` | SUCCESS | SUCCESS | SUCCESS | 14/14 | 0/4 |  |
| 18:51:12 | `432b424` | SUCCESS | SUCCESS | SUCCESS | 14/14 | 0/4 |  |

Uitkomst:

- Geen sample waarin de aangepaste regel `SUCCESS` geeft terwijl GraphQL iets anders geeft, of
  terwijl een check nog loopt of moet starten. De samples met `changes`, `changelog` en
  `secret-scan` klaar en de jobs met `needs: changes` nog zonder run (9/14) geven `PENDING`, via
  de Actions-suite die nog loopt.
- De laatste samples (alle checks klaar): regel en GraphQL beide `SUCCESS`.
- Eén afwijking, fail-closed: om 18:48:22 geeft GraphQL `null` voor de nieuwe head, de regel
  `PENDING` (er staat al een run `queued`). Beide betekenen: niet mergen.
- Eén sample telt niet: om 18:49:04 gaf REST nog head `6f6f3f6`, GraphQL al `432b424`.
- In deze meting kreeg elke head één `ci`-suite; de dubbele suite van #421 kwam niet terug. De
  oude regel geeft hier dus overal hetzelfde als de aangepaste. Het onderscheid bewijst het
  sample op `9941f44a` hieronder.
- De vervangen heads, na afloop gelezen: `72a9ef7` (4 runs `cancelled`) en `6f6f3f6` (5 runs
  `cancelled`) geven bij beide regels en bij GraphQL `FAILURE`.

### Bewijs op `9941f44a` (dubbele suite)

Op 2026-11-19 om 18:46 opnieuw gelezen: head `9941f44a70a663647dfa7c90adf1a928aedd1cff` van #421,
GraphQL `repository{object(oid:…){… on Commit{statusCheckRollup{state}}}}`.

| bron | uitkomst |
|---|---|
| GraphQL `statusCheckRollup.state` | `SUCCESS` |
| aangepaste regel | `SUCCESS` (14 gehouden runs van 28, alle `success`/`skipped`; 0 van 7 suites wachtend) |
| oude regel | `FAILURE` (5 runs `cancelled` in de vervangen suite 120055494192) |

Alle 14 namen (ook `check` en `cla`) staan dubbel; per naam is de run met het hoogste `id` ook die
met de laatste `started_at`.

## Step 6: beslissing

**Gekozen: de aangepaste regel.** Invoer voor één head-sha: `check-runs` (alle pagina's), de
`statuses[]` van `…/commits/<sha>/status` (nooit de bovenste `state`) en `check-suites`.

1. Ontdubbel de check-runs: per (`app.slug`, `name`) alleen de nieuwste run, met het hoogste `id`
   (`started_at` als controle).
2. Geen runs over en `statuses[]` leeg: `""`.
3. Anders `FAILURE` als een gehouden run de conclusie `failure`, `timed_out`, `cancelled`,
   `action_required` of `startup_failure` heeft, of een status `failure` is.
4. Anders `ERROR` als een status `error` is.
5. Anders `SUCCESS` als elke gehouden run `completed` is met conclusie `success`, `neutral` of
   `skipped`, elke status `success` is, en geen suite *wachtend* is (wachtend = `status !=
   "completed"` en (`latest_check_runs_count > 0` of `app.slug == "github-actions"`)).
6. Anders `PENDING` (ook bij elke onbekende waarde, zoals `stale`).

De kandidaatregel van het plan valt af: zonder ontdubbelen geeft hij `FAILURE` op een head met
een vervangen suite (eerste meting, `9941f44a`), en `SUCCESS` zonder enige run.

**Gearchiveerde items: pad B** (Step 2). Geen van beide lijsten geeft een gearchiveerd item;
`IssueItem` blijft op GraphQL. Wel geeft `GET …/items/{id}` het item met `archived_at`, als de
numerieke id bekend is.
