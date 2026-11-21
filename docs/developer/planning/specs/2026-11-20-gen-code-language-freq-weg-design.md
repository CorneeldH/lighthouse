# `tools/gen_code_language_freq` weg — ontwerp

Issue: #363. Milestone: *De repo zonder Python*, stap 4 van
[`2026-11-16-repo-zonder-python-design.md`](2026-11-16-repo-zonder-python-design.md).

## Doel en waarom

De repo moet zonder Python kunnen, op `pack/python` na. `tools/gen_code_language_freq.py` maakt
`internal/codelang/data/freq.tsv.gz` uit wordfreq 3.1.1, een Python-bibliotheek waarvan de data
niet meer wordt bijgewerkt. Geen workflow roept het script aan; het draaide één keer, voor
c49fd274 (#331).

Besluit van de gebruiker: de tabel hoeft niet meer opnieuw gegenereerd te worden. Script en test
gaan weg; de documentatie wijst naar de commit waarin het script zijn laatste vorm had.

## Aanpakken

- **a) Weg** — gekozen. Minste werk; de bron staat stil, dus opnieuw genereren levert dezelfde
  tabel. Nadeel: een toekomstige denylist-treffer in de bytes van `freq.tsv.gz` (CI scant
  `internal/` met `ci/literal-denylist-go.txt`) vraagt het script terug uit de geschiedenis.
- **b) Go-port** — afgewezen. Go kan de tabel niet uit de repo alleen maken: de bron is de
  wordfreq-data in het Python-pakket. Veel werk voor data die stilstaat.
- **c) Weg plus een Go-test op de vorm van de tabel** — afgewezen. De loader in
  `internal/codelang/score.go` faalt al op een kapotte tabel; een denylist-treffer vangt
  `ci/literal-grep.sh` al.

## Wijzigingen

1. `tools/gen_code_language_freq.py` en `tests/test_gen_code_language_freq.py` weg.
2. `internal/codelang/data/README.md`: de link naar het script wordt een verwijzing naar
   c49fd274, met hoe je het script terughaalt en draait:
   `git show c49fd274:tools/gen_code_language_freq.py > /tmp/gen.py` en
   `uv run --with wordfreq==3.1.1 python /tmp/gen.py` (vanuit de repo-root).
3. `internal/codelang/data/NOTICE.txt`: "The file is produced by tools/gen_code_language_freq.py."
   wordt "The file was produced by tools/gen_code_language_freq.py, removed after commit
   c49fd274." De lijst met wijzigingen aan de data blijft ongewijzigd: die beschrijft de tabel,
   en de tabel verandert niet.
4. `ci/domains.py:33`: het commentaar noemt alleen nog `ci/literal-grep.sh` als lezer van de
   literal-lijsten.
5. `changelog.d/issue-363-gen-code-language-freq-weg.md`, type `internal`.
6. Een comment op #363 met het besluit (acceptatiecriterium).

## Niet

- `freq.tsv.gz`, `jargon.txt` en de scorer blijven ongewijzigd.
- Geen nieuwe Go-test.
- De spec, de mockup en oude plannen die het script noemen, blijven zoals ze zijn (milestone-no-go
  voor plannen; de spec beschrijft de keuze zelf).

## Verificatie

- `git ls-files 'tools/*.py' 'tests/test_gen_code_language_freq.py'` geeft alleen
  `tools/check_cla.py`.
- `git grep -n gen_code_language_freq -- ':!docs/developer/planning'` geeft alleen de verwijzingen
  in `internal/codelang/data/`.
- `go test ./internal/codelang/...` groen; `pytest tests/test_domains.py` groen.
- `.claude/hooks/lighthouse/run.sh md-link-check` slaagt op de gewijzigde README.
