# `tools/gen_code_language_freq` weg — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** het generatorscript van `freq.tsv.gz` en zijn test verdwijnen; de documentatie bij de data wijst naar c49fd274.

**Architecture:** alleen verwijderen en documentatie bijwerken. Geen Go-code, de tabel verandert niet.

**Tech Stack:** git, Markdown, één Python-commentaar.

**Spec:** [`2026-11-20-gen-code-language-freq-weg-design.md`](../specs/2026-11-20-gen-code-language-freq-weg-design.md)

## Global Constraints

- `internal/**` en `ci/**` zijn Engels (`code_language_paths`); `changelog.d/` en de issue-comment zijn Nederlands.
- De commit waarin het script zijn laatste vorm had: `c49fd274`.
- Links in `.md` zijn relatief en wijzen naar een bestaand doel (`md-link-check`).
- `freq.tsv.gz`, `jargon.txt` en de scorer blijven byte-gelijk.

## Review Focus

- Een dode link in `internal/codelang/data/README.md` naar het verwijderde script: bewerk de README met Edit, zodat de `md-link-check`-hook hem controleert; geen link naar `tools/gen_code_language_freq.py` meer, alleen code-spans.
- `NOTICE.txt` blijft de CC BY-SA-naamsvermelding: alleen de regel over het script verandert.
- Een pytest-run die nog `tests/test_gen_code_language_freq.py` verwacht: na het weghalen slaagt `uv run pytest -q tests/test_domains.py`.

---

### Task 1: Script en test weg, documentatie bijgewerkt

**Files:**
- Delete: `tools/gen_code_language_freq.py`, `tests/test_gen_code_language_freq.py`
- Modify: `internal/codelang/data/README.md:5-6`, `internal/codelang/data/NOTICE.txt:15`, `ci/domains.py:33`
- Create: `changelog.d/issue-363-gen-code-language-freq-weg.md`

- [ ] **Step 1: Verwijder script en test**

```bash
git rm tools/gen_code_language_freq.py tests/test_gen_code_language_freq.py
```

- [ ] **Step 2: README.** Vervang in het `freq.tsv.gz`-item de link naar het script door: generated from wordfreq 3.1.1 by `tools/gen_code_language_freq.py`, removed after commit `c49fd274` (the data is no longer updated upstream). Voeg toe hoe je het terughaalt, vanuit de repo-root:

```bash
git checkout c49fd274 -- tools/gen_code_language_freq.py
uv run --with wordfreq==3.1.1 python tools/gen_code_language_freq.py
```

Het script moet terug op `tools/`: het vindt de denylist en de uitvoer via `Path(__file__).resolve().parents[1]`. Zeg dat in één zin.

- [ ] **Step 3: NOTICE.** Regel 15 wordt: `The file was produced by tools/gen_code_language_freq.py, removed after commit c49fd274.` Verder niets.

- [ ] **Step 4: `ci/domains.py:33`.** `the literal lists are read only by ci/literal-grep.sh and tools/gen_code_language_freq.py.` wordt `the literal lists are read only by ci/literal-grep.sh.`

- [ ] **Step 5: Changelog-fragment** `changelog.d/issue-363-gen-code-language-freq-weg.md`:

```markdown
---
type: internal
---
`tools/gen_code_language_freq.py` en zijn test zijn weg; `internal/codelang/data/README.md` zegt hoe je het script uit commit c49fd274 terughaalt.
```

- [ ] **Step 6: Verifieer**

```bash
git ls-files 'tools/*.py' 'tests/test_gen_code_language_freq.py'   # alleen tools/check_cla.py
git grep -n gen_code_language_freq -- ':!docs/developer/planning'  # alleen internal/codelang/data/ en changelog.d/
git diff --stat origin/main -- internal/codelang/data/freq.tsv.gz internal/codelang/data/jargon.txt  # leeg
go test ./internal/codelang/...                                     # ok
uv run pytest -q tests/test_domains.py                              # passed
ci/literal-grep.sh; LITERAL_DENYLIST=ci/literal-denylist-go.txt LITERAL_SKIP_TESTS=1 ci/literal-grep.sh internal cmd  # rc 0
```

- [ ] **Step 7: Commit**

```bash
git add -A tools tests internal/codelang/data ci/domains.py changelog.d
git commit -m "chore(codelang): tools/gen_code_language_freq weg (#363)"
```

### Task 2: Besluit op het issue

- [ ] **Step 1:** Comment op #363 (Nederlands): besluit *weg*, want wordfreq 3.1.1 wordt niet meer bijgewerkt en een Go-port kan de tabel niet zonder die Python-data maken; de README wijst naar c49fd274. Met `lighthouse forge issue comment 363 --body-file <bestand>` (sandbox uit).
