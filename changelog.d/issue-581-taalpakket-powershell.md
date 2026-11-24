---
type: added
---
New language pack `powershell`: `lighthouse init` detects a repository with `.ps1` or `.psm1` files and reports a missing `pwsh` or PSScriptAnalyzer, and the `powershell-lint-check` hook blocks after an Edit, Write or Bash write on five fixed PSScriptAnalyzer rules, with canaries on Linux and a measurement on Windows (#581).
