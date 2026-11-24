#!/usr/bin/env pwsh
# psscriptanalyzer-repo.ps1 -Path <file> -Rules <comma list> [-Version <module version>]
#
# Runs PSScriptAnalyzer with exactly the given rules on one file, for powershell-lint-check.
# Writes one line per finding to stdout: `RuleName<TAB>Line<TAB>Message`.
#
# Exit codes:
#   0   clean
#   10  findings (a code `pwsh -File` never gives itself: an uncaught error is rc 1)
#   3   the PSScriptAnalyzer module (or the pinned -Version of it) cannot be loaded
#   4   any other failure; nothing was checked
param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$Rules,
    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'

try {
    $import = @{ Name = 'PSScriptAnalyzer'; ErrorAction = 'Stop' }
    if ($Version -ne '') {
        $import['RequiredVersion'] = $Version
    }
    Import-Module @import
}
catch {
    $wanted = if ($Version -ne '') { " version $Version" } else { '' }
    [Console]::Error.WriteLine("psscriptanalyzer-repo.ps1: the PSScriptAnalyzer module$wanted cannot be loaded: $($_.Exception.Message)")
    exit 3
}

try {
    $ruleList = @($Rules.Split(',') | Where-Object { $_ -ne '' })
    if ($ruleList.Count -eq 0) {
        throw 'no rules given'
    }
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "no such file: $Path"
    }
    # -Path takes wildcards: escape them, so `x [1].ps1` is that file and not a pattern.
    $target = [Management.Automation.WildcardPattern]::Escape($Path)
    $findings = @(Invoke-ScriptAnalyzer -Path $target -IncludeRule $ruleList)
    foreach ($finding in $findings) {
        $message = ($finding.Message -replace '[\t\r\n]+', ' ').Trim()
        [Console]::Out.WriteLine("$($finding.RuleName)`t$($finding.Line)`t$message")
    }
    if ($findings.Count -gt 0) {
        exit 10
    }
    exit 0
}
catch {
    [Console]::Error.WriteLine("psscriptanalyzer-repo.ps1: $($_.Exception.Message)")
    exit 4
}
