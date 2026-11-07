# Een software-transcript met de hand lezen — plan (#243)

> **Voor agents:** voer dit plan uit met superpowers:subagent-driven-development of
> superpowers:executing-plans, taak voor taak.

**Doel:** `docs/workplace.md` zegt in *Transcripten* hoe je het transcript van een `software`-run met
de hand leest.

**Context:** proefissue A voor #237. De host kan het volume `claude-state` van de devcontainer niet
lezen (eigenaar een subuid, mode 700); alleen `podman exec` in de container kan dat. De sectie
*Transcripten* legt uit hoe de adapter dat doet, maar niet hoe een mens het doet.

**Bestanden:**

- Wijzigen: `docs/workplace.md` (sectie *Transcripten*, de bullet over profiel `software`)
- Nieuw: `changelog.d/issue-243-transcript-met-de-hand.md`

## Taak 1: de zin in `docs/workplace.md`

- [ ] **Stap 1:** Voeg aan het eind van de bullet `Profiel software` in *Transcripten* één zin toe:

  ```markdown
  Met de hand lees je het met `podman exec <container> cat /home/vscode/.claude/projects/<map>/<sessie>.jsonl`,
  waarbij `<map>` de projectmap uit `workplace.ProjectDir` is.
  ```

  Houd de regelbreedte van het bestand aan (ongeveer 100 tekens).

- [ ] **Stap 2:** Controleer dat de sectie nog leest: `sed -n '/^## Transcripten/,/^## Stop/p' docs/workplace.md`.

- [ ] **Stap 3:** Commit `docs(workplace): een software-transcript met de hand lezen (#243)`.

## Taak 2: changelog-fragment

- [ ] **Stap 1:** Schrijf `changelog.d/issue-243-transcript-met-de-hand.md` in de vorm van de andere
  fragmenten (zie `docs/releasing.md`): één regel onder *Documentatie* die de zin noemt.

- [ ] **Stap 2:** Commit `docs(changelog): fragment voor #243`.

## Afronding

- [ ] `/lighthouse-write-pr 243`, dan de PR openen. Docs-only: de go-checks slaan over.
