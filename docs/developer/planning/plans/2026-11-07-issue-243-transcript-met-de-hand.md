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

- [ ] **Stap 1:** Voeg aan het eind van de bullet `Profiel software` in *Transcripten* één zin toe
  (de bullet staat nu op regel 146-153; de zin komt na `(#237).`):

  ```markdown
  Met de hand: zoek de container met `podman ps --filter label=devcontainer.local_folder` en lees
  `podman exec <container> sh -c 'cat "$HOME/.claude/projects/<map>/<sessie>.jsonl"'`, waarbij
  `<map>` de projectmap uit `workplace.ProjectDir` is.
  ```

  `$HOME` staat tussen enkele aanhalingstekens, zodat de container hem uitbreidt en niet de host
  (een kale `~` of `$HOME` buiten `sh -c` wijst naar de host, waar het volume onleesbaar is).
  Houd de regelbreedte van het bestand aan (ongeveer 100 tekens).

- [ ] **Stap 2:** Controleer dat de sectie nog leest: `sed -n '/^## Transcripten/,/^## Stop/p' docs/workplace.md`.

- [ ] **Stap 3:** Commit `docs(workplace): een software-transcript met de hand lezen (#243)`.

## Taak 2: changelog-fragment

- [ ] **Stap 1:** Schrijf `changelog.d/issue-243-transcript-met-de-hand.md` volgens
  `docs/releasing.md` (*Changelog fragments*): een regel `type: internal` tussen twee `---`-regels,
  dan één Engelse zin, bijvoorbeeld `Document how to read a software-profile transcript by hand
  in docs/workplace.md.` Het type is `internal` (alleen documentatie voor ontwikkelaars): zo start
  het geen release. Geen kopje *Documentatie*; dat bestaat niet in dit formaat.

- [ ] **Stap 2:** Commit `docs(changelog): fragment voor #243`.

## Afronding

- [ ] `/lighthouse-write-pr 243`, dan de PR openen. Docs-only: de go-checks slaan over.
