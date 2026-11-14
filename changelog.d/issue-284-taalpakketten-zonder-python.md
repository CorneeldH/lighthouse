---
type: changed
---
De pakketten `go` en `node` leveren `gate_go.py` en `gate_node.py` niet meer mee: `[test_gate] command` roept `lighthouse gate go` en `lighthouse gate node` aan, met dezelfde uitvoer en exitcodes. `lighthouse init --upgrade` zet het oude commando om en meldt andere verwijzingen naar de scripts. De `lighthouse`-binary op het PATH moet deze release zijn, anders geeft de test-gate `unknown command "gate"`. `python3` staat niet meer in `requires.toml` van `go`, `node`, `shell` en `expo`.
