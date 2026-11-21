---
type: added
---
Een nieuwe poort controleert dat elke directe import in een manifest is gedeclareerd: `lighthouse imports check` draait `go mod tidy -diff` (Go), knip (node) en deptry (Python), per Stop-hook (`go-imports-gate`, `python-imports-gate`, `node-imports-gate`) en in `commit-test-gate`, ook met `[test_gate] command = ""`. Uitzetten kan alleen met `[imports] check = false`. Kan de commit-gate de check niet draaien, dan blokkeert zij met `COULD NOT CHECK`; de Stop-hooks melden dat zonder te blokkeren. Bekende beperking: bij `git commit -a` in één Bash-aanroep is er bij `pre` nog niets gestaged, dus slaat de commit-gate de check over; stage in een aparte aanroep. In een Expo-project zonder `main` of entry vindt knip niets. De CI-job `canary` houdt de checkers zelf in de gaten.
