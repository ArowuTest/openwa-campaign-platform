#Requires -Version 5.1
<#
.SYNOPSIS
  Operator-run production database login-role and Railway DATABASE_URL wiring.

.DESCRIPTION
  This script is intentionally not executed by ChatGPT tooling. It is a local
  operator script for the person who controls the Railway account and laptop.
  It generates service-specific passwords only in local process memory, creates
  PostgreSQL LOGIN roles over Railway SSH, sets service DATABASE_URL variables
  through Railway's stdin variable setter with deploys skipped, prints no
  passwords and writes no secret values to disk.

  Default mode is dry-run. Pass -Apply to perform changes.

.SECURITY
  - Does not print generated passwords or DSNs.
  - Does not write generated passwords or DSNs to files.
  - Sends role DDL to psql over stdin.
  - Sends DATABASE_URL to Railway over stdin.
  - Uses --skip-deploys so no app container is redeployed by this step.
  - Writes only a non-secret summary JSON if -SummaryPath is supplied.
#>
[CmdletBinding()]
param(
  [switch]$Apply,
  [string]$ProjectId = '8633a0b9-3b15-4c8d-b6f2-0306b284f4dd',
  [string]$Environment = 'production',
  [string]$PostgresService = 'Postgres',
  [string]$DatabaseName = 'railway',
  [string]$DatabaseHost = 'postgres.railway.internal',
  [int]$DatabasePort = 5432,
  [string]$SshIdentityFile = "$env:USERPROFILE\.ssh\id_ed25519_railway_openwa_verify",
  [string]$SummaryPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$services = @(
  [pscustomobject]@{ Name='control-api'; ServiceId='751fd9e6-e26f-411d-94bb-d00bff06abda'; StableRole='campaign_control_api'; LoginPrefix='prod_control_api' },
  [pscustomobject]@{ Name='audience-worker'; ServiceId='dede9d43-2cb9-413f-bacd-4f1f750b04ca'; StableRole='campaign_audience_worker'; LoginPrefix='prod_audience_worker' },
  [pscustomobject]@{ Name='campaign-worker'; ServiceId='5830baa1-c94e-465c-afae-b63a4cd47620'; StableRole='campaign_campaign_worker'; LoginPrefix='prod_campaign_worker' },
  [pscustomobject]@{ Name='export-worker'; ServiceId='b002f23f-c186-414c-8e2f-2221b39ab762'; StableRole='campaign_export_worker'; LoginPrefix='prod_export_worker' },
  [pscustomobject]@{ Name='inbound-governance-worker'; ServiceId='8d04dff3-8d7f-4d64-b427-14cd12c66142'; StableRole='campaign_inbound_governance_worker'; LoginPrefix='prod_inbound_governance_worker' },
  [pscustomobject]@{ Name='metrics-worker'; ServiceId='412f2fa2-288c-4898-9bf2-276f88890270'; StableRole='campaign_metrics_worker'; LoginPrefix='prod_metrics_worker' },
  [pscustomobject]@{ Name='platform-governance-worker'; ServiceId='9d9fe33b-be49-4881-99c8-4a0f3cebab5e'; StableRole='campaign_platform_governance_worker'; LoginPrefix='prod_platform_governance_worker' }
)

function Assert-Tool([string]$Name) {
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "Required command '$Name' was not found on PATH."
  }
}

function New-SecretToken {
  $bytes = New-Object byte[] 48
  $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
  try {
    $rng.GetBytes($bytes)
  } finally {
    $rng.Dispose()
  }
  return ([Convert]::ToBase64String($bytes)).TrimEnd('=').Replace('+','-').Replace('/','_')
}

function Quote-PgIdent([string]$Value) {
  return '"' + $Value.Replace('"','""') + '"'
}

function Quote-PgLiteral([string]$Value) {
  return "'" + $Value.Replace("'", "''") + "'"
}

function Invoke-RailwayPsql([string]$Sql, [switch]$ShowOutput) {
  $args = @('ssh','--project',$ProjectId,'--environment',$Environment,'--service',$PostgresService)
  if ($SshIdentityFile -and (Test-Path $SshIdentityFile)) {
    $args += @('--identity-file', $SshIdentityFile)
  }
  $args += @('--','psql','-v','ON_ERROR_STOP=1','-q','-f','-')
  if ($ShowOutput) {
    $Sql | & railway @args
  } else {
    $Sql | & railway @args | Out-Null
  }
  if ($LASTEXITCODE -ne 0) {
    throw "railway ssh psql failed with exit code $LASTEXITCODE"
  }
}

function Set-RailwaySecretVariable([string]$ServiceId, [string]$Key, [string]$Value) {
  $args = @('variable','set',$Key,'--stdin','--project',$ProjectId,'--environment',$Environment,'--service',$ServiceId,'--skip-deploys','--json')
  $Value | & railway @args | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "railway variable set failed for service $ServiceId key $Key with exit code $LASTEXITCODE"
  }
}

Assert-Tool 'railway'

$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddHHmmss')
$summary = [ordered]@{
  schema_version = 1
  evidence_type = 'operator-db-login-role-wiring-summary'
  generated_at_utc = (Get-Date).ToUniversalTime().ToString('o')
  project_id = $ProjectId
  environment = $Environment
  database = $DatabaseName
  apply = [bool]$Apply
  secrets_redacted = $true
  deploys_skipped = $true
  services = @()
}

Write-Host 'Production DB login-role/Railway DATABASE_URL wiring'
Write-Host "Mode: $(if ($Apply) { 'APPLY' } else { 'DRY-RUN' })"
Write-Host 'Secrets will not be printed or written to disk.'
Write-Host ''

$generated = @()
foreach ($svc in $services) {
  $loginRole = ($svc.LoginPrefix + '_' + $stamp).ToLowerInvariant()
  if ($Apply) {
    $password = New-SecretToken
    $dsn = "postgres://$([uri]::EscapeDataString($loginRole)):$([uri]::EscapeDataString($password))@$DatabaseHost`:$DatabasePort/$DatabaseName`?sslmode=require"
  } else {
    $password = ''
    $dsn = ''
  }
  $generated += [pscustomobject]@{ Service=$svc; LoginRole=$loginRole; Password=$password; DatabaseUrl=$dsn }
  $summary.services += [ordered]@{
    name = $svc.Name
    service_id = $svc.ServiceId
    stable_role = $svc.StableRole
    login_role = $loginRole
    database_url_variable = 'DATABASE_URL'
    database_url_secret_set = [bool]$Apply
  }
}

if (-not $Apply) {
  foreach ($item in $generated) {
    Write-Host ("DRY-RUN would create LOGIN role {0} as member of {1}, then set DATABASE_URL for Railway service {2}." -f $item.LoginRole, $item.Service.StableRole, $item.Service.Name)
  }
  Write-Host ''
  Write-Host 'No roles created and no Railway variables changed. Re-run with -Apply to perform the operation.'
} else {
  $sql = New-Object System.Text.StringBuilder
  [void]$sql.AppendLine('BEGIN;')
  foreach ($item in $generated) {
    $loginIdent = Quote-PgIdent $item.LoginRole
    $stableIdent = Quote-PgIdent $item.Service.StableRole
    $loginLiteral = Quote-PgLiteral $item.LoginRole
    $passwordLiteral = Quote-PgLiteral $item.Password
    $stableLiteral = Quote-PgLiteral $item.Service.StableRole
    [void]$sql.AppendLine("DO `$`$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $loginLiteral) THEN RAISE EXCEPTION 'role already exists: %', $loginLiteral; END IF; END `$`$;")
    [void]$sql.AppendLine("CREATE ROLE $loginIdent LOGIN PASSWORD $passwordLiteral NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT;")
    [void]$sql.AppendLine("GRANT $stableIdent TO $loginIdent;")
    [void]$sql.AppendLine("COMMENT ON ROLE $loginIdent IS 'campaign-platform production rotatable login for $($item.Service.Name); member of $stableLiteral; created by operator script $stamp';")
  }
  [void]$sql.AppendLine('COMMIT;')

  Invoke-RailwayPsql -Sql $sql.ToString()

  foreach ($item in $generated) {
    Set-RailwaySecretVariable -ServiceId $item.Service.ServiceId -Key 'DATABASE_URL' -Value $item.DatabaseUrl
    Write-Host ("Configured DATABASE_URL for {0}; login role {1}; secret redacted." -f $item.Service.Name, $item.LoginRole)
  }

  $verifySql = @"
SELECT child.rolname AS login_role,
       parent.rolname AS stable_role,
       child.rolsuper,
       child.rolcreatedb,
       child.rolcreaterole,
       child.rolcanlogin
FROM pg_auth_members membership
JOIN pg_roles child ON child.oid = membership.member
JOIN pg_roles parent ON parent.oid = membership.roleid
WHERE child.rolname LIKE 'prod\_%\_$stamp' ESCAPE '\'
ORDER BY child.rolname;
"@
  Write-Host ''
  Write-Host 'Non-secret PostgreSQL role verification:'
  Invoke-RailwayPsql -Sql $verifySql -ShowOutput
}

if ($SummaryPath) {
  $dir = Split-Path -Parent $SummaryPath
  if ($dir) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
  $summaryJson = $summary | ConvertTo-Json -Depth 8
  $summaryFullPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($SummaryPath)
  [System.IO.File]::WriteAllText($summaryFullPath, $summaryJson, (New-Object System.Text.UTF8Encoding($false)))
  Write-Host "Non-secret summary written to $SummaryPath"
}


