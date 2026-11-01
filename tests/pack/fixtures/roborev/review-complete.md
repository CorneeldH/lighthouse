## roborev: Review Complete (`bf1eb24`)

**Verdict:** Changes require fixes for 2 findings.

### Medium

- scripts/forge_drager.py:93: The Lighthouse carrier contract requires mode 0600, but validation only checks that the path is a regular file. A group/world-readable or writable carrier is accepted even though it controls the Claude session and arguments. Validate the permission bits from lstat with stat.S_IMODE(mode) == 0o600 and reject/delete carriers with any other mode; add a behavioral test covering permissive modes.

- scripts/forge_agent.sh:612: Carrier discovery treats a failed lookup caused by an unreadable or unsearchable Lighthouse state/carriers directory exactly like an absent carrier. The wrapper can therefore fall back to starting an ordinary, untracked agent when it cannot actually prove that no Lighthouse carrier exists. Before interpreting a missing carrier as absence, verify that every existing state/carriers directory is readable and searchable; fail with exit 4 when absence cannot be measured.

