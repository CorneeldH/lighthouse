---
type: fixed
---
`lighthouse pins check` meet een upstream met meer dan 1000 releases (openai/codex) weer: de releases worden newest-first gelezen en de meting stopt na de pagina met de release van de pin, zodat GitHub geen 422 ("Only the first 1000 results are available") meer geeft en een meting een paar pagina's kost in plaats van ongeveer 35.
