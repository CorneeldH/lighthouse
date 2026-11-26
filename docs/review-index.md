# review-index

`lighthouse review-index` koppelt de bevindingen van de adversariële plan- en spec-reviews, en de
uitspraken van de taakreviews van een SDD-run, aan het traject (repo en issue) waartoe ze horen. Het
schrijft rijen naar `findings` en, per afgerond reviewrecord, één rij naar `review_runs` in `lighthouse.db`; de inhoud van een record of ledger wordt nooit
gekopieerd (alleen pad, hash, component, sleutel, ernst en besluit).

```
lighthouse review-index [--ledger <path>]... [--issue <n>]
```

Uitvoer: `indexed: new=<n> updated=<n> unchanged=<n> removed=<n>`, daarna één
`skipped: <path>: <reason>` per item dat niet is geïndexeerd. Exit 0 (ook met overgeslagen items),
1 bij een database- of leesfout, 2 bij verkeerd gebruik. Een ontbrekende
`<data>/{plan,spec}-review` telt als leeg. Een keten of record die niet te lezen of ongeldig is
(bijvoorbeeld een onleesbare map of een `written_at` van `0001-01-01T00:00:00Z`) wordt een
`skipped`-regel; de andere ketens worden gewoon geïndexeerd.

## Reviewrecords

`plan-review-record write` draait dezelfde indexering over de hele datamap nadat het zijn record
heeft geschreven. Een fout daarbij is één regel `REVIEW-INDEX FAILED: <error>` op stderr, een
overgeslagen record één regel `REVIEW-INDEX SKIPPED: <path>: <reason>`; beide veranderen de
exitcode nooit, want het record is het product. `review-index` is de inhaalrun en herkent een
`record.json` dat achteraf is gewijzigd (zelfde pad, andere hash: de rijen worden bijgewerkt, niet
verdubbeld).

- **Traject.** Het record (schema 6) draagt `trajectory`: `null`, of `{repo, issue}`. Het issue is
  `--issue` van `write` (het issue staat dan voor de spec), anders het nummer in de huidige branch
  (`issue-<n>`); de repo is `forge.repo` uit `lighthouse.toml`. Een record met `trajectory: null`,
  of van schema 5 of lager, krijgt geen bevindingen, maar wel een `review_runs`-rij zonder traject
  (`trajectory_id` leeg, reden `no_trajectory`), en verschijnt als `skipped` met de reden.
- **Traject herleiden.** `review-index` herleidt een record zonder `trajectory` (en elke indexering
  van de datamap, ook die na `plan-review-record write`): het issue is het `issue-<n>` in de
  `chain_key` (komen er twee verschillende voor, dan geen traject), anders `issue_spec.issue`; de repo
  is de enige bediende repo (`[dispatch] repos` in de hostconfig, met `forge.repo` uit zijn
  `lighthouse.toml`) waarvan de kloon `reviewed_sha` bevat. Is het issue of de repo niet eenduidig
  (de sha staat in twee repo's of in geen), dan blijft het record zoals hierboven: `no_trajectory`,
  geen bevindingen. Hij faalt dicht: zonder leesbare hostconfig, of als één van de bediende repo's niet laadt (fout in zijn `lighthouse.toml`, dubbele slug), herleidt hij niets, zodat een sha die ook in de weggelaten kloon staat niet naar de verkeerde repo linkt. `issue-<n>` telt alleen op een woordgrens: `reissue-5` en `issue-5bar` zijn geen verwijzing.
- **Reviewer per bevinding.** `findings` draagt `reviewer_source` (`codex`, `fallback` of `both`) en de ernst per reviewer (`severity_codex`, `severity_fallback`), uit `source` en `severity_by_source` van de bevinding; een bevinding zonder `source` (verificatieronde) laat ze leeg. `findings.source` blijft de herkomst `plan-review-record`.
- **Reviewruns.** Per afgerond record één rij in `review_runs`: `source = plan-review-record`,
  `source_id = <kind>/<chain>/<attempt>/record.json`, component (`plan_review` of `spec_review`),
  pad en hash van het record, `ran_at` (`written_at`), `outcome` (`converged`, `round_limit` of
  `aborted`) en `finding_count` (ook 0), `review_mode`, `reviewers` (JSON-array, bijvoorbeeld `["codex","opus"]`) en de indeling (`assignment`, standaard `standard`, en `assignment_applied`, leeg zonder veld in het record). Een nieuwere poging vervangt de rij van de oudere, en
  dezelfde hash verandert niets. Per keten gebeurt het verwijderen van oude rijen, het schrijven
  van de run en het schrijven van de bevindingen in één transactie: bij een fout blijven de rijen
  van de vorige poging staan. Een onleesbaar record of een foute `written_at` blijft een
  `skipped`-regel zonder rij. Het ledger (`--ledger`) schrijft geen `review_runs`. Het tabblad
  Telemetry leest deze rijen voor de vergelijking met en zonder review: zie
  [`telemetry-ui.md`](telemetry-ui.md).
- **Nieuwste poging per keten.** Per keten telt de poging met de grootste naam. Heeft die alleen
  `attempt.json` (nog `.<attempt>.tmp/`, of een mislukte `write`), dan geldt de keten als niet
  geslaagd: de bevindingen van oudere pogingen verdwijnen en er komen er geen bij.
- **Besluit.** `accept`, `spec`, `minor_yes` worden `adopted`; `reject`, `minor_no` worden
  `rejected`; `accepted_open`, `defer` worden `left_open`; geen besluit (alleen `aborted`) blijft
  leeg.

## SDD-ledger

`--ledger <path>` leest de regels `Task <n>: ... Ruling: ...` van een SDD-ledger als
`task_review`-bevindingen (ernst leeg). `Task <n>: parked ...` is `rejected` (de code blijft), elke
andere uitspraak is `left_open`; een `Ruling:`-regel zonder `Task <n>:`-voorvoegsel wordt
overgeslagen. Het ledger hoort bij het issue van `--issue`, anders bij het `issue-<n>` in het
planpad op de eerste regel (`# SDD ledger — plan: <path>`). Zonder een van beide wordt het met een
reden overgeslagen. Een ledger zonder die planregel wordt ook overgeslagen, omdat de bevindingen
dan geen stabiel id kunnen krijgen. Het ledgerpad wordt absoluut vastgelegd.

Het ledger heeft in deze repo geen vast formaat (het komt uit de externe superpowers-skill
`subagent-driven-development`) en wordt aan het eind van de SDD-run verwijderd: draai
`review-index --ledger` vóór die opruiming. Het automatisch aanroepen vanuit de skill hoort niet bij
deze wijziging.
