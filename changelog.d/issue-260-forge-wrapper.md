---
type: added
---
`lighthouse forge` is het nieuwe oudercommando voor wat beide forges delen: issues, PR's en MR's, milestones, labels, blockers en een beperkte `api`, op GitHub en GitLab, zonder `merge`-commando en met de exitcodes 0, 1 en 2. De skill `lighthouse-forge` beschrijft per forge wat verschilt, en `lighthouse-start-issue`, `lighthouse-route-issue`, `lighthouse-write-issue` en `lighthouse-write-pr` gebruiken `lighthouse forge` in plaats van `gh`; `gh` blijft nodig voor `gh auth token` als terugval. De hooks (`pr-create-gate`) en `lighthouse evidence` werken op beide forges, en `board-start` draait zonder `gh`. De overige skills en scripts met `gh` volgen in vervolgissues.
