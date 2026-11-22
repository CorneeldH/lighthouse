---
type: added
---
Een run kan zijn eigen `CODEX_HOME` krijgen: is die gezet, dan meten `lighthouse codex-usage`, de roborev-gate en `lighthouse usage` alleen die home, en haalt `review-run` zijn `auth.json` daar vandaan. Zonder `CODEX_HOME` blijft alles bij `~/.codex` en `~/.codex-specreview`.
