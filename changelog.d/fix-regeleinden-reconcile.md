---
type: fix
---
`lighthouse init --upgrade` ziet een bestand dat alleen CRLF-regeleinden heeft als ongewijzigd en schrijft het terug met LF, zonder `--force`. Een bestaand CRLF-bestand zonder manifest-regel wordt bij `init` ook hersteld.
