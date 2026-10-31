---
type: added
---
A new package `node` adds `gate_node.py`, the test-gate selector for a JavaScript project: when the commit touches the package directory it runs the npm scripts you name (lint, typecheck, test) after any drift check (`--drift gen=src/api`). Add it with `lighthouse init --upgrade --pack node` and put it in `[test_gate] command` yourself.
