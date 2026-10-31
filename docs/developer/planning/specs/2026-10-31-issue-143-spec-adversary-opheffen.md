# Spec: `lighthouse-spec-adversary` opheffen (#143)

Kort design-doc: een opruiming zonder gedragswijziging.

## Waarom

De agent `lighthouse-spec-adversary` heeft nog één taak: de retry van ronde 1 als Codex én Opus
allebei faalden. Hij draait dan `review.sh` en geeft een jq-tabel terug. Op de ketenkaart (#99)
leek hij op `spec-adversarial-review` zelf, en de eigenaar vroeg wat het verschil is. Een
onderdeel dat niets toevoegt maakt de keten moeilijker uit te leggen.

## Meting (AC 1)

Bron: de Claude Code-sessielogs in `~/.claude/projects/*/…jsonl`, bewaartermijn 30 dagen, periode
2026-09-28 t/m 2026-10-31. Geteld: unieke `tool_use`-aanroepen van `Agent`/`Task` met een
`subagent_type` die `adversary` bevat.

- 56 aanroepen, allemaal als `spec-adversary` in `personal-ai-agent` (paia) en zijn worktrees,
  van vóór de gepaarde ronde 1.
- De laatste op 2026-10-16 ("Plan review round 1 rerun").
- `lighthouse-spec-adversary`: 0 keer sinds de agent op 2026-10-18 in lighthouse kwam (`52c8723`).

De reviewverslagen van `plan_review_record.py` kunnen dit niet meten: ze leggen niet vast of de
agent de retry deed. `paired: false` na een retry ziet er hetzelfde uit als in de andere
ongepaarde gevallen.

## Besluit

De agent verdwijnt. De skills doen de retry zelf, met de bouwstenen die ze al hebben: Codex als
achtergrond-Bash met redirect (zoals in ronde 1) en de jq-tabel uit *Opus dispatch* (zoals bij
elke verificatieronde). De ruwe review komt zo ook zonder agent niet in de context van de sessie.
Omdat de agent verdwijnt, vervalt het hernoemen volgens #142.

## Aanpakken

| | Retry na dubbel gefaald paar | Afweging |
|---|---|---|
| **A (gekozen)** | Codex nog één keer, inline | Gedrag blijft gelijk; `REVIEW_SH`-allowlist en `--review-mode codex` blijven. Faalt Codex om een blijvende reden, dan `--outcome aborted`, zoals nu. |
| B | Opus alleen, inline | Mechaniek bestaat al, maar ronde 1 verliest het oordeel van een andere modelfamilie. Een beleidskeuze, geen opruiming. |
| C | Het paar opnieuw | Twee reviewers en het blinde protocol opnieuw, voor een geval dat in 30 dagen niet voorkwam. |

De eigenaar koos A: het issue gaat over het onderdeel, niet over het retrybeleid.

## Wat verandert

- `pack/core/agents/spec-adversary.md` weg; ook uit `pack/core/requires.toml` en
  `pack/SOURCES.toml`.
- Beide reviewskills, `claude`- en `agents`-variant: de retry beschrijft de inline Codex-run
  (achtergrond-Bash, `run_in_background: true`, `dangerouslyDisableSandbox: true`, output naar
  `<ATTEMPT>/round-1.json`, log via redirect, alleen de exitcode lezen) gevolgd door de jq-tabel
  uit *Opus dispatch*. De retry is niet blind: er is één reviewer, de tabel mag dus direct.
- De regel "No `lighthouse-spec-adversary`" in de planskill (ronde 1, Codex-kant) blijft inhoudelijk
  staan: de Codex-kant van het paar is een Bash-run zonder tabel, anders is beslissen niet meer
  blind. Alleen de verwijzing naar de agent gaat eruit.
- De zin in de `agents`-varianten dat de agent alleen in Claude Code bestaat, gaat eruit.
- Kaartdata: de node `spec-adversary` en zijn verwijzingen uit `docs/site/data/chain.json`, en de
  vermelding in `docs/site/data/packs.json`.
- Tests: de tests die het agentbestand lezen (`tests/pack/test_issue_impact_scout.py`,
  `tests/pack/test_adversarial_review_skills.py`) en de lijst in `internal/packs/core_test.go`.
  Een test die de oude agent-contracten controleert, gaat over op een controle dat de skills de
  retry inline beschrijven en de agent nergens meer noemen.
- `go run ./cmd/lighthouse init --upgrade` verwijdert de geïnstalleerde kopie
  (`.claude/agents/lighthouse-spec-adversary.md`, manifestregel) en werkt de skillkopieën bij;
  daarna geeft het `No changes.`
- Een fragment in `changelog.d/`.

## Niet

- Geen wijziging aan `review.sh`, `verify.sh`, schema's of prompts.
- Geen nieuw veld in `plan_review_record.py`.
- Geen wijziging aan het retrybeleid (aanpak B blijft liggen).
- Historische plannen en specs onder `docs/developer/planning/` blijven zoals ze zijn.

## Aan de bouwer

- De exacte formulering van de retry in beide skills; de spec-skill verwijst naar de planskill,
  zoals nu.
- Welke testasserties vervallen en welke nieuwe nodig zijn, zolang een test faalt als een skill de
  agent weer noemt of de retry-instructie kwijtraakt.
