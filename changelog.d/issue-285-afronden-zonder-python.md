---
type: changed
---
De Python-lib van `pack/core` (`.lighthouse/lib/`) en `create_labels.py` zijn weg: geen script van `pack/core` is nog Python. `lighthouse init --upgrade` ruimt ze op in een bestaande repo; een lokaal gewijzigd bestand onder `.lighthouse/lib/` blijft staan en wordt gemeld. Een host zonder `python3` werkt nu volledig, behalve voor het pakket `python` (`ty-gate`, `gate_python`, `python-standard`). De installatiegids vraagt Python niet meer als vereiste. Werk de binary op je PATH bij vóór `lighthouse init --upgrade`.
