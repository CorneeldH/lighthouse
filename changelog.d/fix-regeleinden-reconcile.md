---
type: fixed
---
`lighthouse init --upgrade` treats a file that only has CRLF line endings as unchanged and writes it back with LF, without `--force`. An existing CRLF file without a manifest line is also repaired by `init`.
