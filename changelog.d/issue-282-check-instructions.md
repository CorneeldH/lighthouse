---
type: changed
---
`lighthouse check-instructions` replaces `.lighthouse/scripts/check-instructions.sh` and no longer needs `python3` or `sha256sum`. A repository that calls `.lighthouse/scripts/check-instructions.sh` from its own hooks or CI must switch to `lighthouse check-instructions`, because `init --upgrade` removes the script.
