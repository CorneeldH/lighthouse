---
type: added
---
`[test_gate] on` kiest wanneer de test-gate draait: `commit` (standaard, zoals voorheen), `pr` (vóór een `gh pr create`, op de branch-diff) of `both`. Bij `pr` en `both` blokkeert een commando dat een commit en een PR-create combineert, met het verzoek ze te splitsen. Een onleesbare `lighthouse.toml` blokkeert nu ook een `gh pr create`, en bij een onleesbare config zonder `on` noemt het blok bij de commit nu `on` in plaats van `forge.default_branch`; de oorzaaktekst blijft gelijk.
