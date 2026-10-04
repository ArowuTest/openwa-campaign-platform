$ErrorActionPreference = 'Stop'

$global:openwaRailwayCalls = @()

function railway {
    $chunks = @($input | ForEach-Object { [string]$_ })
    $global:openwaRailwayCalls += [pscustomobject]@{
        Arguments = @($args)
        Input = ($chunks -join ([char]10))
    }
    $global:LASTEXITCODE = 0
}

Describe 'Railway DB login wiring script' {
    It 'generates safe role comments before writing all seven variables' {
        $global:openwaRailwayCalls = @()
        $scriptPath = Join-Path $PSScriptRoot '../../scripts/wire-railway-production-db-logins.ps1'
        $summaryPath = Join-Path $TestDrive 'summary.json'

        & $scriptPath -Apply -SummaryPath $summaryPath | Out-Null

        $sqlCall = $global:openwaRailwayCalls |
            Where-Object { $_.Arguments -contains '-f' } |
            Select-Object -First 1

        $sqlCall | Should Not BeNullOrEmpty
        $sqlCall.Input | Should Match 'COMMENT ON ROLE'
        $sqlCall.Input | Should Not Match "member of 'campaign_"
        $sqlCall.Input | Should Match 'member of campaign_control_api'

        ($global:openwaRailwayCalls |
            Where-Object { $_.Arguments -contains 'variable' }).Count |
            Should Be 7
    }
}
