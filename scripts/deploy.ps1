#Requires -Version 5.1
<#
.SYNOPSIS
Build, validate, deploy, and restart the home-pc-agent Windows service.

.DESCRIPTION
Stages the executable, configuration, and runner scripts under
C:\ProgramData\home-pc-agent. The current deployment is backed up and restored
if installation or startup fails.

Run from an elevated shell.
#>
[CmdletBinding()]
param(
    [string]$ConfigSource,
    [string]$ServiceName = 'home-pc-agent'
)

$ErrorActionPreference = 'Stop'

function Write-Step([string]$Message)
{
    Write-Host ""
    Write-Host "==> $Message"
}

function Test-Admin
{
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Wait-ServiceStatus
{
    param(
        [string]$Name,
        [System.ServiceProcess.ServiceControllerStatus]$Status,
        [int]$TimeoutSeconds = 30
    )

    $service = Get-Service -Name $Name -ErrorAction Stop
    $service.WaitForStatus($Status, (New-TimeSpan -Seconds $TimeoutSeconds))
    $service.Refresh()
    if ($service.Status -ne $Status)
    {
        throw "Service '$Name' did not reach $Status within ${TimeoutSeconds}s."
    }
}

if (-not (Test-Admin))
{
    throw 'Deployment requires an elevated shell.'
}

$ScriptRoot = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
$RepoRoot = (Resolve-Path (Join-Path $ScriptRoot '..')).Path
$DataDir = 'C:\ProgramData\home-pc-agent'
$ExeDest = Join-Path $DataDir 'home-pc-agent.exe'
$ConfigDest = Join-Path $DataDir 'home-pc-agent.toml'
$ScriptsDest = Join-Path $DataDir 'scripts'
$ScriptsSource = Join-Path $RepoRoot 'configs\scripts'

if (-not $ConfigSource)
{
    $LocalConfig = Join-Path $RepoRoot 'configs\home-pc-agent.local.toml'
    if (Test-Path -LiteralPath $LocalConfig -PathType Leaf)
    {
        $ConfigSource = $LocalConfig
    }
    elseif (Test-Path -LiteralPath $ConfigDest -PathType Leaf)
    {
        $ConfigSource = $ConfigDest
    }
    else
    {
        throw 'No deployment config found. Pass -ConfigSource or create configs\home-pc-agent.local.toml.'
    }
}

if (-not (Test-Path -LiteralPath $ConfigSource -PathType Leaf))
{
    throw "Config source not found: $ConfigSource"
}
if (-not (Test-Path -LiteralPath $ScriptsSource -PathType Container))
{
    throw "Scripts source not found: $ScriptsSource"
}
if (-not (Get-Command mise -ErrorAction SilentlyContinue))
{
    throw 'mise is required to build with the repository toolchain.'
}

$ConfigSource = (Resolve-Path -LiteralPath $ConfigSource).Path
$DeployID = Get-Date -Format 'yyyyMMdd-HHmmss'
$CandidateDir = Join-Path $DataDir ".deploy-$DeployID"
$BackupDir = Join-Path $DataDir "backups\$DeployID"
$CandidateExe = Join-Path $CandidateDir 'home-pc-agent.exe'
$CandidateConfig = Join-Path $CandidateDir 'home-pc-agent.toml'
$CandidateScripts = Join-Path $CandidateDir 'scripts'
$OriginalService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
$ServiceExisted = $null -ne $OriginalService
$ServiceWasRunning = $ServiceExisted -and $OriginalService.Status -ne 'Stopped'
$DeploymentStarted = $false

New-Item -ItemType Directory -Force -Path $CandidateDir, $CandidateScripts | Out-Null
try
{
    Write-Step 'Building candidate executable'
    Push-Location $RepoRoot
    try
    {
        & mise exec -- go build -o $CandidateExe ./cmd/home-pc-agent
        if ($LASTEXITCODE -ne 0)
        {
            throw "Build failed with exit code $LASTEXITCODE."
        }
    }
    finally
    {
        Pop-Location
    }

    Copy-Item -LiteralPath $ConfigSource -Destination $CandidateConfig
    Copy-Item -Path (Join-Path $ScriptsSource '*.ps1') -Destination $CandidateScripts

    Write-Step 'Validating candidate config'
    & $CandidateExe config validate --config $CandidateConfig
    if ($LASTEXITCODE -ne 0)
    {
        throw "Config validation failed with exit code $LASTEXITCODE."
    }

    New-Item -ItemType Directory -Force -Path $BackupDir | Out-Null
    if (Test-Path -LiteralPath $ExeDest -PathType Leaf)
    {
        Copy-Item -LiteralPath $ExeDest -Destination (Join-Path $BackupDir 'home-pc-agent.exe')
    }
    if (Test-Path -LiteralPath $ConfigDest -PathType Leaf)
    {
        Copy-Item -LiteralPath $ConfigDest -Destination (Join-Path $BackupDir 'home-pc-agent.toml')
    }
    if (Test-Path -LiteralPath $ScriptsDest -PathType Container)
    {
        Copy-Item -LiteralPath $ScriptsDest -Destination $BackupDir -Recurse
    }

    $DeploymentStarted = $true
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($service -and $service.Status -ne 'Stopped')
    {
        Write-Step "Stopping Windows service '$ServiceName'"
        Stop-Service -Name $ServiceName -Force
        Wait-ServiceStatus -Name $ServiceName -Status Stopped
    }

    Write-Step "Installing candidate under $DataDir"
    New-Item -ItemType Directory -Force -Path $DataDir, $ScriptsDest | Out-Null
    Copy-Item -LiteralPath $CandidateExe -Destination $ExeDest -Force
    Copy-Item -LiteralPath $CandidateConfig -Destination $ConfigDest -Force
    Copy-Item -Path (Join-Path $CandidateScripts '*.ps1') -Destination $ScriptsDest -Force

    if (-not $ServiceExisted)
    {
        Write-Step "Installing Windows service '$ServiceName'"
        & $ExeDest service install --name $ServiceName --config $ConfigDest
        if ($LASTEXITCODE -ne 0)
        {
            throw "Service installation failed with exit code $LASTEXITCODE."
        }
    }

    Write-Step "Starting Windows service '$ServiceName'"
    Start-Service -Name $ServiceName
    Wait-ServiceStatus -Name $ServiceName -Status Running

    Write-Host ""
    Write-Host 'Deploy complete.'
    Write-Host "  exe:     $ExeDest"
    Write-Host "  config:  $ConfigDest"
    Write-Host "  scripts: $ScriptsDest"
    Write-Host "  backup:  $BackupDir"
}
catch
{
    if (-not $DeploymentStarted)
    {
        throw
    }
    Write-Warning "Deployment failed. Restoring backup from $BackupDir."
    Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue

    if (-not $ServiceExisted -and (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue))
    {
        & $ExeDest service uninstall --name $ServiceName 2>$null
    }

    foreach ($Name in @('home-pc-agent.exe', 'home-pc-agent.toml'))
    {
        $BackupPath = Join-Path $BackupDir $Name
        $Destination = Join-Path $DataDir $Name
        if (Test-Path -LiteralPath $BackupPath -PathType Leaf)
        {
            Copy-Item -LiteralPath $BackupPath -Destination $Destination -Force
        }
        elseif (Test-Path -LiteralPath $Destination -PathType Leaf)
        {
            Remove-Item -LiteralPath $Destination -Force
        }
    }

    $BackupScripts = Join-Path $BackupDir 'scripts'
    if (Test-Path -LiteralPath $ScriptsDest -PathType Container)
    {
        Remove-Item -LiteralPath $ScriptsDest -Recurse -Force
    }
    if (Test-Path -LiteralPath $BackupScripts -PathType Container)
    {
        Copy-Item -LiteralPath $BackupScripts -Destination $DataDir -Recurse
    }

    if ($ServiceWasRunning)
    {
        Start-Service -Name $ServiceName -ErrorAction SilentlyContinue
    }
    throw
}
finally
{
    if (Test-Path -LiteralPath $CandidateDir -PathType Container)
    {
        Remove-Item -LiteralPath $CandidateDir -Recurse -Force
    }
}
