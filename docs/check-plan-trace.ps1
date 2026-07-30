[CmdletBinding()]
param(
    [string]$RepositoryRoot = (Split-Path -Parent $PSScriptRoot)
)

$ErrorActionPreference = 'Stop'
$planPath = Join-Path $RepositoryRoot 'docs/plan.md'
$todoPath = Join-Path $RepositoryRoot 'TODOS.md'
$errors = [System.Collections.Generic.List[string]]::new()

foreach ($path in @($planPath, $todoPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        $errors.Add("Required document is missing: $path")
    }
}

if ($errors.Count -eq 0) {
    $plan = Get-Content -LiteralPath $planPath -Raw
    $todos = Get-Content -LiteralPath $todoPath -Raw

    for ($layer = 0; $layer -le 20; $layer++) {
        $matches = [regex]::Matches(
            $plan,
            "(?m)^### Layer $layer(?:\s|:)"
        ).Count
        if ($matches -ne 1) {
            $errors.Add("Layer $layer heading count is $matches; expected 1")
        }
    }

    for ($milestone = 0; $milestone -le 24; $milestone++) {
        $id = 'M{0:D2}' -f $milestone
        $todoMatches = [regex]::Matches(
            $todos,
            "(?m)^# Milestone {0:D2}(?:\s|:)" -f $milestone
        ).Count
        if ($todoMatches -ne 1) {
            $errors.Add("$id TODO heading count is $todoMatches; expected 1")
        }

        $mapMatches = [regex]::Matches(
            $plan,
            "(?m)^\| $id \| Layer (?:[0-9]|1[0-9]|20) \|$"
        ).Count
        if ($mapMatches -ne 1) {
            $errors.Add("$id layer mapping count is $mapMatches; expected 1")
        }
    }

    $sectionZeroStart = $plan.IndexOf('# 0. Linear Concept and Build Order')
    $sectionOneStart = $plan.IndexOf('# 1. Product Constraints')
    if ($sectionZeroStart -lt 0 -or $sectionOneStart -le $sectionZeroStart) {
        $errors.Add('Could not isolate canonical Section 0')
    }
    else {
        $sectionZero = $plan.Substring(
            $sectionZeroStart,
            $sectionOneStart - $sectionZeroStart
        )
        $referencedSections = [System.Collections.Generic.HashSet[string]]::new()
        foreach ($line in $sectionZero -split "`r?`n") {
            if ($line -notmatch '^\* ') {
                continue
            }
            foreach ($match in [regex]::Matches($line, '(?<![0-9])([0-9]{1,2}[A-D]?)(?![0-9])')) {
                [void]$referencedSections.Add($match.Groups[1].Value)
            }
        }

        foreach ($section in $referencedSections) {
            $headingMatches = [regex]::Matches(
                $plan,
                "(?m)^# $([regex]::Escape($section))\."
            ).Count
            if ($headingMatches -ne 1) {
                $errors.Add(
                    "Section 0 references section $section, but heading count is $headingMatches"
                )
            }
        }
    }

    foreach ($requiredPhrase in @(
        'smallest prerequisite concepts',
        'owning product layer and milestone',
        'typed inputs and their authority/revision scope',
        'observable outputs and durable events',
        'forbidden forward dependencies',
        'stop, narrow, or removal condition'
    )) {
        if (-not $plan.Contains($requiredPhrase)) {
            $errors.Add("Major subsystem declaration rule is missing: $requiredPhrase")
        }
    }
}

if ($errors.Count -gt 0) {
    foreach ($message in $errors) {
        Write-Error $message
    }
    exit 1
}

Write-Output 'Plan trace check passed: M00-M24 map exactly once to Layers 0-20, and canonical references resolve.'
