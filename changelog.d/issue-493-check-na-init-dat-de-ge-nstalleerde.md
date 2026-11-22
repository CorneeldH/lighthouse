---
type: added
---
`lighthouse check-hooks` checks that the hooks registered in `.claude/settings.json` can run: the shell (`sh`, or Git Bash on Windows), the scripts, the line endings and a smoketest per hook. `lighthouse init` runs it after writing and prints the result, and sets `.gitattributes` rules so a checkout does not turn the hooks into CRLF.
