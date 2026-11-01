## roborev: Review Complete (`803c1a6`)

## Summary

De wijziging voegt een lokale reviewgate met regeldrempel, Codex-budgetcontrole en persistente status toe, maar bevat twee uitvoeringsproblemen.

**Agent assessment:** Fail

## Findings

### 1. Medium

**Location:** pack/core/scripts/roborev_gate.py:184

**Problem:** Bij de eerste uitkomst `run` bestaat de map voor `raw` nog niet: alleen `write_state()` maakt die aan, en dat gebeurt uitsluitend bij overslaan. De geplande reviewaanroep met `> <raw>` faalt daardoor bij de shellredirectie voordat roborev start.

**Fix:** Maak de uitvoermap aan voordat `run` wordt teruggegeven. Test dat de geretourneerde raw-locatie in een verse worktree direct beschrijfbaar is.

### 2. Medium

**Location:** pack/core/scripts/roborev_gate.py:169

**Problem:** De parser behandelt de paden uit `git diff --numstat` als ongequote tekst. Git quote onder standaardinstellingen onder andere namen met niet-ASCII-tekens. Daardoor wordt bijvoorbeeld `docs/café.md` niet herkend als documentatie en tellen de regels ten onrechte mee, wat een onnodige Codex-review kan starten.

**Fix:** Gebruik `git diff --numstat -z` en parse de NUL-gescheiden records. Voeg een gedragstest toe met documentatiebestanden waarvan Git de namen normaal quote.

