[CmdletBinding()]
param(
    [Parameter(ParameterSetName = 'Set', Mandatory = $true)]
    [ValidateRange(30, 500)]
    [double]$RefreshRate,

    [Parameter(ParameterSetName = 'Set', Mandatory = $true)]
    [Parameter(ParameterSetName = 'Query', Mandatory = $true)]
    [Parameter(ParameterSetName = 'List', Mandatory = $true)]
    [ValidateSet('PG32UCDM', 'VDD by MTT', IgnoreCase = $true)]
    [string]$DisplayName,

    [Parameter(ParameterSetName = 'Query', Mandatory = $true)]
    [switch]$Query,

    [Parameter(ParameterSetName = 'List', Mandatory = $true)]
    [switch]$List
)

$commonPath = Join-Path $PSScriptRoot 'Display.Common.ps1'
. $commonPath

switch ($PSCmdlet.ParameterSetName)
{
    'Query'
    {
        Write-Output (Get-DisplayRefreshQuery -DisplayName $DisplayName)
        exit 0
    }
    'List'
    {
        Write-Output (Get-DisplayRefreshOptionsJson -DisplayName $DisplayName)
        exit 0
    }
}

$rate = [double]([string]$RefreshRate -replace '_', '.')
if ($rate -ge 239.9 -and $rate -le 240.1) { $rate = 240.016 }
Set-DisplayRefreshOn -RefreshRate $rate -DisplayName $DisplayName
