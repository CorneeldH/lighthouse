## Lighthouse: shell

- Every edited `.sh` or `.bash` file is shellchecked; a finding blocks. `shell-lint-check: COULD NOT CHECK` means nothing was checked, not that the file is clean.
- Before writing shell that guards something (a hook, a gate, its tests), read the skill `lighthouse-shell-standard`.
- The floor codes (SC2164, SC2046, SC2115, SC2155, SC2034) stay on whatever a `.shellcheckrc` or `SHELLCHECK_OPTS` says; a block tags the code. Existing debt goes behind `# shellcheck disable=SCxxxx <reason>`, not behind a check that is off.
