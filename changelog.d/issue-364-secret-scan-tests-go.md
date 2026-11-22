---
type: internal
---
De black-box-tests van de pre-push-secretscan en `tools/hooks/install.sh` draaien in Go (`e2e/prepushscan_test.go`) in plaats van pytest; de `secret-scan`-job heeft geen `uv` meer nodig.
