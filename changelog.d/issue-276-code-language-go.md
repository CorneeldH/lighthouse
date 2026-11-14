---
type: changed
---
`code-language-check` runs in the `lighthouse` binary (`lighthouse hook code-language-check`) instead of bash and Python; the verdicts are unchanged. Python that tokenizes but does not parse (`x = = 1`) is now scanned instead of reported as `COULD NOT CHECK`. Update the `lighthouse` binary on your PATH and run `lighthouse init --upgrade`.
