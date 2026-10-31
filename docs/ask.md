# Ask

`lighthouse ask` is hoe een controller-run zijn vraag zelf op het issue zet. De controller leest de
uitvoer van de run niet meer: de run post één 🤖 Vraag en stopt, en de controller hervat de sessie
met het antwoord. Het ontwerp staat in
[de dispatcher-spec](developer/planning/specs/2026-10-27-m2-dispatcher-design.md). De run-instructies
(`internal/dispatch/instructions/run.md`) verwijzen naar dit document.

## Wat ask doet

1. Leest de sessie uit `LIGHTHOUSE_CONTROLLER_RUN` (een UUID). Ontbreekt die of is het geen UUID,
   dan staat er niets op het issue.
2. Leest de repo (`[forge] repo`) en de taal (`[project] language`) uit `lighthouse.toml` in de
   huidige map. De huidige map is ook de worktree in de vraag.
3. Leest het issue voor titel en link, en rendert de vraag in de projecttaal.
4. Leest de gerenderde vraag terug zoals de controller dat doet. Komt er niet uit wat erin ging
   (bijvoorbeeld een veldkop in de tekst), dan volgt een fout en geen post.
5. Post de vraag als comment, met de sessie in de voet, en schrijft de comment-URL op stdout.

`ask` opent nooit de database. Het token komt uit `GH_TOKEN`, dan `GITHUB_TOKEN`, dan
`gh auth token`.

## Vlaggen

```text
lighthouse ask --issue N --phase T [--task T] --where T --what T
  (--question T (--option T){2,6} --recommended <a-f>){1,4}
  [--command T]... [--dry-run]
```

- `--issue`: het issuenummer `<N>` uit de prompt `(issue #<N>, controller-run)`. Verplicht.
- `--phase`, `--where`, `--what`: verplicht en niet leeg. `--task` is optioneel.
- `--question`: opent een vraag. De `--option`s en de `--recommended` daarna horen erbij, in de
  volgorde van de command line. Een bundel heeft 1 tot 4 vragen.
- `--option`: 2 tot 6 per vraag, niet leeg.
- `--recommended`: de letter (`a` tot `f`) van de aanbevolen optie. Precies één keer per vraag, en de
  optie moet er zijn.
- `--command`: een commando dat de gebruiker zelf moet draaien. Mag vaker. Het komt ongewijzigd in
  de vraag.
- `--dry-run`: print de vraag en post niets.

## Voorbeeld

```bash
lighthouse ask --issue 42 --phase execution --task "Task 2" \
  --where "Task 2 done, tests green" \
  --what "Two choices I cannot make myself" \
  --question "Which approach do we take?" \
  --option "approach A" --option "approach B" --recommended b \
  --question "When do we remove the old route?" \
  --option "now" --option "in a follow-up issue" --recommended b \
  --command "sudo dnf install ripgrep"
```

## Exitcodes

- `0`: gepost (de comment-URL staat op stdout) of met `--dry-run` gerenderd.
- `1`: alles anders. Een foute vorm eindigt op `; nothing posted`: een `--option` of `--recommended`
  vóór de eerste `--question`, geen of meer dan vier vragen, een vraag met minder dan twee of meer
  dan zes opties, een lege optie, een vraag zonder of met twee `--recommended`, een `--recommended`
  naar een optie die er niet is, een leeg veld, een ontbrekende of ongeldige sessie, geen
  `lighthouse.toml` met `[forge] repo`, of geen token. Een mislukte lees van het issue of een mislukte
  post geeft `lighthouse ask: post failed: <fout>`; dan staat er niets op het issue en mag de run
  dezelfde aanroep één keer herhalen.
