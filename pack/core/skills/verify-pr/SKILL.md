---
name: paia-verify-pr
description: 'Use before every `gh pr create` in CorneeldH/personal_ai_agent — "maak een PR", the push + PR choice in finishing-a-development-branch, or when pr-create-gate.sh blocks. Verifies each acceptance criterion of the issue on the branch and writes the PR body itself — `Closes #N` with an Evidence table (one row per AC), `Refs #N — <reden>`, or `No issue: <reden>` — without asking a question, interactive and headless alike. LET OP — een issue sluiten of na de merge nacontroleren is paia-close-issue; een rode PR repareren is paia-fix-pr.'
metadata:
  binding: hard
  origin: own
  domain: factory
---

# PR-body met bewijs

Een PR zegt zelf of hij zijn issue afmaakt: `Closes #N` met een bewijsrij per acceptatiecriterium,
of `Refs #N` met de reden waarom nog niet. Deze skill verifieert en schrijft die body; hij sluit
nooit zelf een issue (dat doet de merge van een `Closes`-PR, of de gebruiker). Hij stelt geen
vraag: interactief en headless (controller-run) is de uitkomst dezelfde.

Ontwerp: [spec sectie 2](../../../docs/developer/planning/specs/2026-10-03-pr-verifieert-issue-design.md).

## Wanneer

- Vóór elke `gh pr create`, ook "maak een PR" en de keuze push + PR in
  `finishing-a-development-branch`.
- Als `pr-create-gate.sh` een `gh pr create` blokkeert.
- `/paia-verify-pr <nr>`. Zonder argument komt het nummer uit de branch `issue-<nr>-…`
  (`git branch --show-current`). Geen issue: de body krijgt `No issue: <reden>` (≥10 tekens) en
  geen milestone; sla V1–V3 over.

## V1. Lees het issue

```bash
R=$(uv run python -m src.controller.identity repo) || exit 2
gh api "repos/$R/issues/<nr>" \
  --jq '{title, milestone: .milestone.title, body}'
```

Elke checkbox onder `### Acceptatiecriteria` is één AC; de hook telt ze en eist evenveel
Evidence-rijen. Noteer ook de DoD en de milestone-titel. `gh` draait buiten de sandbox
(`dangerouslyDisableSandbox: true`).

## V2. Bewijs per AC

Zoek per AC bewijs op de branch: `git diff origin/main...HEAD`, commando's draaien, bestanden
openen. Wat als bewijs telt en wat níét: B2–B4 van
[`paia-close-issue`](../paia-close-issue/SKILL.md). Geen bewijs = `niet`, ook als het
waarschijnlijk af is.

## Verdicts

De drie van B3 in `paia-close-issue` (`aantoonbaar af`, `deels`, `niet`), plus twee:

| Verdict | Betekenis |
|---|---|
| `na merge` | het AC hangt af van iets dat pas na de merge kan: een deploy, een meting over dagen, een check op de Beelink |
| `na PR` | het AC meet een eigenschap van de PR zelf en kan dus pas bestaan zodra die er is: zijn koppeling aan het issue (`closedByPullRequestsReferences`), de live run van de hook op zijn body. Niets anders valt hieronder. |

## V3. Kies de vorm

- Elk AC `aantoonbaar af` of `na PR` → `Closes #N`, met een kop `### Evidence #N` en één
  tabelrij per AC. Een `na PR`-rij heeft als Result `na PR: <verwacht fragment>`; ingang C van
  `paia-close-issue` meet hem na de merge. Het PR-nummer bestaat pas na `gh pr create`, dus meet
  een `na PR`-rij via het issue: `gh api graphql` op `closedByPullRequestsReferences` van
  `issue(number:<nr>)` (zie *Vorm van het bewijsblok*). `gh pr view` zonder nummer weigert de
  allowlist: na de merge meet ingang C in een detached worktree, zonder branch om op terug te vallen.
- Anders → `Refs #N — <reden>` op één regel (reden ≥10 tekens), met daaronder per open AC het
  verdict en wat er nog moet.

Mengen mag: `Closes #5` met Evidence #5 naast `Refs #6 — <reden>`.

## Evidence-regels

Ingang C draait elke rij na de merge opnieuw op `origin/main`, dus elke rij moet herhaalbaar zijn:

- geen branch-relatief commando (`origin/main...HEAD`, `HEAD~`, `@{u}`);
- de Command-cel is één enkel commando tussen backticks, zonder pipe, `;`, `&&`, `$(…)` of
  redirect, en staat op de Evidence-allowlist hieronder. Moet een AC daarbuiten bewezen worden,
  dan is het geen Evidence-rij: zoek een commando op de lijst, of het wordt `na merge` (dus `Refs`);
- de Result-cel is een stabiel fragment van de uitvoer: geen regelnummer en geen testaantal dat
  meeschuift met main. Het oordeel zit in de exitcode; Result is een letterlijke substring van de
  uitvoer.

Een `|` binnen een cel schrijf je als `\|` (markdown); binnen quotes wordt dat een gewone `|`,
kaal blijft het een pipe en is de rij ongeldig.

## Evidence-allowlist

Op argv-niveau, volledig; dezelfde lijst als `.claude/hooks/lib/evidence.py` (de bron) en ingang C.
Altijd: `argv[0]` is de kale naam (geen `/usr/bin/grep`, geen `FOO=1 grep`), een optie die niet in
de lijst staat is geweigerd, een optie met `=` en een lege waarde ook.

| Commando | Toegestaan | Niet toegestaan |
|---|---|---|
| `grep` | vaste-tekst-, regex- en uitvoeropties (`-F`, `-E`, `-i`, `-n`, `-r`, `-l`, `-c`, `-v`, `-w`, `-x`, `-o`, `-q`, …), `-e`, `--include`, `--exclude`, `--`, paden | — |
| `git grep` | de vlaggen van `grep` zonder `-r`/`-R`/`-s`; als waarde-optie alleen `-e`; `--`, refs en paden | `-O`, `--open-files-in-pager`, `--include`, `--exclude`, `--regexp` (schrijf `-e`), branch-relatieve refs |
| `test`, `ls`, `wc`, `cat` | `test`: elke expressie; `ls`: `-l`, `-a`, `-A`, `-1`, `-d`, `-R`, `-h`; `wc`: `-l`, `-w`, `-c`, `-m`; `cat`: `-n`; paden | elke andere optie |
| `git log`, `git show`, `git ls-files` | alleen `--oneline`, `--stat`, `--name-only`, `-n <getal>`, `--format=`/`--pretty=` (alleen met `=`), `--grep=`, `--`, refs en paden | elke andere optie (onder meer `--output`, dat schrijft); `%G` in `--format`/`--pretty` (start gpg); branch-relatieve refs (`..`, `@{`, `~`, `^`); globale git-opties vóór het subcommando (`git -c`, `git -C`, `--git-dir`) |
| `uv run pytest` | uitsluitend paden onder `tests/unit/` (met `::<test>`), plus `-q`/`-v` | elke andere optie (`-m`, `-p`, `-o`, `-k`, `--`), elk pad buiten `tests/unit/`, opties tussen `run` en `pytest` |
| `gh api <pad>` | GET met `--jq`/`-q`; een pad, geen URL | `-X`/`--method`, `-f`/`-F`/`--field`/`--raw-field`, `--input`, `-H`/`--header` |
| `gh api graphql` | precies één `-f query=…` zonder `mutation`, plus `--jq` | elke andere veldvlag |
| `gh issue view`, `gh pr view` | precies één nummer of github.com-URL, `--json`, `--jq`/`-q`, `--repo`/`-R`, `--comments`/`-c` | `--web`/`-w` (start een browser); geen nummer (gh valt dan terug op de branch van de cwd) |

Een `--jq`-expressie mag `env`, `$ENV` of `input`/`inputs` niet bevatten. De lijst uitbreiden is
een wijziging van deze skill én van `evidence.py`, nooit iets wat een PR-body kan.

Toets elke rij zodra je hem schrijft:

```bash
python3 .claude/hooks/lib/evidence.py check '<Command-cel>'
```

Exit 0 (stil) = toegestaan; exit 1 + reden = kies een ander commando.

Draai vóór de PR alle rijen samen, buiten de sandbox (`dangerouslyDisableSandbox: true`, anders
is elke `gh`-rij `niet meetbaar`):

```bash
python3 .claude/hooks/lib/evidence.py run --issue <nr> --body-file <pad> --cwd . --voor-pr
```

Uitvoer (stderr staat erin): een tabel `| AC | Uitkomst | Detail |`. Elke rij moet `klopt` of
`na PR, niet gemeten` zijn; een andere rij gaat eruit, en dan heeft dat AC een ander commando
nodig of is het niet bewezen (`Refs`).

| Exit | Betekenis | Doe |
|---|---|---|
| 0 | alles `klopt` of `na PR, niet gemeten` | door naar V4 |
| 1 | ≥1 `klopt niet` of `ongeldig` | rij herstellen of eruit |
| 2 | geen geldige `Evidence #N`-tabel (`Closes zonder bewijsblok`) | body herstellen |
| 3 | `KON NIET CONTROLEREN` (body onleesbaar, `--cwd` geen map) | blokkeert: geen PR, meld het |
| 4 | geen afwijking, maar ≥1 `niet meetbaar` | blokkeert: rij eruit, of opnieuw buiten de sandbox |
| elke andere | onbekend | blokkeert: geen PR op ongetoetst bewijs |

## DoD

Gaat naar `## Verification` en beslist niet over `Closes`/`Refs` ("code reviewed" kan vóór de
merge nooit waar zijn). Uitzondering: zijn de unit-tests niet groen, dan maakt de skill geen PR
en meldt hij dat.

Het bewijs daarvoor is in Claude Code de laatste uitkomst van de commit-gate
(`.claude/hooks/pre-commit-tests.sh` draait bij elke `git commit` dezelfde selectie op de
gecommitte boom): noem die in `## Verification` en draai de volle suite niet nog eens met de hand
(`.claude/rules/python.md`). Alleen buiten Claude Code (OpenCode, Codex; *Buiten Claude Code* in
`AGENTS.md`) draai je hem zelf:

```bash
uv run pytest tests/unit/ -m 'not hardware_audio and not slow' -n auto --maxprocesses 8 -q
```

## V4. Body-bestand en commando

Schrijf de body vanuit `.github/pull_request_template.md` naar de scratchpad of `$TMPDIR`, met
een aparte stap (de Write-tool), niet in dezelfde Bash-aanroep als `gh pr create`: de
PreToolUse-hook leest het bestand vóórdat het commando draait.

Open review-punten: geef de spec- en plandocumenten die de branch raakt
(`git diff --name-only origin/main...HEAD -- 'docs/developer/planning/**/*.md'`) aan
`uv run python scripts/plan_review_record.py pr-open-punten <pad>...`; de spec uit de
**Spec:**-regel van een plan volgt het script zelf, ook als die spec al op `main` staat. Is de
uitvoer niet leeg, zet hem ongewijzigd als eigen sectie `## Open review points` in de body; is
hij leeg, laat de sectie weg. Het is geen Evidence-rij en telt niet mee in de AC-telling van
`pr-create-gate.sh`. Noem de koppen hier alleen inline tussen backticks: een regel die met
`## ` begint, breekt de V4-sectie die `tests/unit/test_verify_pr_skill.py` uitsnijdt.

Geef dan het commando:

```bash
gh pr create --title '<titel>' --body-file <pad> --milestone "<milestone>"
```

`--title` altijd: zonder vraagt gh erom, en headless weigert hij. Enkele quotes, zodat de shell
niets in de titel uitvoert. `--milestone` alleen als het issue er een heeft; bij `No issue:` nooit. Nooit `--body "$(…)"`,
`--fill` of `--web`: de hook blokkeert ze.

## Vorm van het bewijsblok

```markdown
## Related issues

Closes #1091

### Evidence #1091

| AC | Command | Result |
|---|---|---|
| 1. Template en skill beschrijven beide vormen | `grep -F 'Evidence #' .github/pull_request_template.md` | `### Evidence #` |
| 3. Hook blokkeert body zonder vorm | `uv run pytest tests/unit/test_pr_create_gate.py -q` | `passed` |
| 6. Development-paneel toont de PR | `gh api graphql -f query=…closedByPullRequestsReferences…` | `na PR: "number":` |
```

## Tegenhanger

`.claude/hooks/pr-create-gate.sh` (PreToolUse op `Bash`) dwingt deze vorm af bij elke
`gh pr create`/`gh pr new`: geen geldige vorm, een Evidence-tabel met een ander aantal rijen dan
AC's, of een body die niet uit een letterlijke waarde of `--body-file` komt → blokkade. Kan hij de
AC's niet tellen (geen `gh`, geen netwerk), dan laat hij luid door met
`AC-telling niet meetbaar` — dat is geen geslaagd, noem het in `## Verification`.

## Buiten Claude Code

OpenCode en Codex draaien de hook niet. Toets de body dan zelf:

```bash
printf '%s' "gh pr create --body-file <pad>" | python3 .claude/hooks/lib/pr_body_gate.py "$PWD"
```

Exit 0 = in orde (lees de meldingen op stdout); 2 = geblokkeerd (reden op stderr); 3 = open
quote; elke andere exit = niet gecontroleerd, dus geen PR.
