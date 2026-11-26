---
type: added
---
`lighthouse init` seeds a `.github/dependabot.yml` with one block per ecosystem it finds (`github-actions`, `gomod`, `npm`, `uv`, `docker`) when the file does not exist, never overwrites it afterwards, and warns about a workflow that runs on a self-hosted runner without a `dependabot[bot]` guard (#617).
