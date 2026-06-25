#Requires -Version 5.1
<#
.SYNOPSIS
Build, deploy, and restart the home-pc-agent Windows service.

.DESCRIPTION
1. Builds home-pc-agent.exe
2. Stops any running service/process
3. Copies exe to ~/.bin/home-pc-agent.exe
4. Copies config + runner scripts to C:\ProgramData\home-pc-agent
5. Installs (if needed) and starts the Windows service

Run from an elevated shell (required for ProgramData deploy and Windows service).
#>
[CmdletBinding()]
param(
    [string]$ConfigSource,
    [string]$ServiceName = 'home-pc-agent'
)

$ErrorActionPreference = 'Stop'

$ScriptRoot = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
$RepoRoot = (Resolve-Path (Join-Path $ScriptRoot '..')).Path
if (-not $ConfigSource)
{
    $ConfigCandidates = @(
        (Join-Path $RepoRoot 'configs\home-pc-agent.local.toml'),
        (Join-Path $env:APPDATA 'home-pc-agent\home-pc-agent.toml'),
        (Join-Path $RepoRoot 'configs\home-pc-agent.windows.example.toml')
    )
    $ConfigSource = $ConfigCandidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
}

$BinDir = Join-Path $env:USERPROFILE '.bin'
$DataDir = 'C:\ProgramData\home-pc-agent'
$ScriptsDir = Join-Path $DataDir 'scripts'
$ConfigDest = Join-Path $DataDir 'home-pc-agent.toml'
$ScriptsSource = Join-Path $RepoRoot 'configs\scripts'
$ExeDest = Join-Path $BinDir 'home-pc-agent.exe'
$BuildOut = Join-Path $RepoRoot 'dist\home-pc-agent.exe'

function Write-Step([string]$Message)
{
    Write-Host ""
    Write-Host "==> $Message"
}

function Stop-HomepcRuntime
{
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($service -and $service.Status -ne 'Stopped')
    {
        Write-Step "Stopping Windows service '$ServiceName'"
        if (Test-Path -LiteralPath $ExeDest)
        {
            & $ExeDest service stop 2>$null
        }
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
        $service.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, (New-TimeSpan -Seconds 30))
    }

    Get-Process -Name 'home-pc-agent' -ErrorAction SilentlyContinue | ForEach-Object {
        Write-Step "Stopping home-pc-agent process $($_.Id)"
        Stop-Process -Id $_.Id -Force
    }
}

function Wait-HomepcServiceRunning
{
    param(
        [string]$Name,
        [int]$TimeoutSeconds = 30
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline)
    {
        $service = Get-Service -Name $Name -ErrorAction SilentlyContinue
        if ($service -and $service.Status -eq 'Running')
        {
            return $service
        }
        Start-Sleep -Milliseconds 500
    }
    throw "Service '$Name' did not reach Running within ${TimeoutSeconds}s (status: $($service.Status))."
}

function Test-Admin
{
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

Write-Step "Building home-pc-agent"
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $BuildOut) | Out-Null
Push-Location $RepoRoot
try
{
    go build -o $BuildOut ./cmd/home-pc-agent
}
finally
{
    Pop-Location
}

Stop-HomepcRuntime

Write-Step "Installing exe to $ExeDest"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Copy-Item -LiteralPath $BuildOut -Destination $ExeDest -Force

Write-Step "Deploying config and scripts"
if (-not (Test-Admin))
{
    throw "Deploying to C:\ProgramData\home-pc-agent requires an elevated shell."
}
if (-not (Test-Path -LiteralPath $ConfigSource))
{
    throw "Config source not found: $ConfigSource"
}
if (-not (Test-Path -LiteralPath $ScriptsSource))
{
    throw "Scripts source not found: $ScriptsSource"
}

New-Item -ItemType Directory -Force -Path $DataDir, $ScriptsDir | Out-Null
Copy-Item -LiteralPath $ConfigSource -Destination $ConfigDest -Force
Copy-Item -Path (Join-Path $ScriptsSource '*.ps1') -Destination $ScriptsDir -Force

Write-Step "Validating deployed config"
& $ExeDest config validate --config $ConfigDest

$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $service)
{
    if (-not (Test-Admin))
    {
        throw "Service '$ServiceName' is not installed. Re-run this script from an elevated shell."
    }

    Write-Step "Installing Windows service '$ServiceName'"
    & $ExeDest service install --config $ConfigDest
}
else
{
    Write-Host "Service '$ServiceName' already installed."
}

if (-not (Test-Admin))
{
    Write-Warning "Not running elevated; skipping service start. Run: $ExeDest service start"
    exit 0
}

Write-Step "Starting Windows service '$ServiceName'"
& $ExeDest service start
if ($LASTEXITCODE -ne 0) {
    throw "home-pc-agent service start failed with exit code $LASTEXITCODE"
}
$service = Wait-HomepcServiceRunning -Name $ServiceName
Write-Host ""
Write-Host "Deploy complete."
Write-Host "  exe:    $ExeDest"
Write-Host "  config: $ConfigDest"
Write-Host "  scripts:$ScriptsDir"
Write-Host "  service:$ServiceName ($($service.Status))"
