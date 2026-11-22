---
type: internal
---
CI draait één `pack-python`-job in plaats van drie pytest-shards, en die job draait alleen `tests/pack/python/`. De repo bevat geen Python meer buiten `pack/python/` en `tests/pack/python/`: de laatste root-tests zijn naar Go of vervallen.
