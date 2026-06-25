[CmdletBinding()]
param(
    [Parameter(ParameterSetName = 'Set', Mandatory = $true)]
    [ValidateRange(640, 10000)]
    [int]$Width,

    [Parameter(ParameterSetName = 'Set', Mandatory = $true)]
    [ValidateRange(480, 10000)]
    [int]$Height,

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
        Write-Output (Get-DisplayResolutionQuery -DisplayName $DisplayName)
        exit 0
    }
    'List'
    {
        Write-Output (Get-DisplayResolutionOptionsJson -DisplayName $DisplayName)
        exit 0
    }
}

Set-DisplayResolutionOn -DisplayName $DisplayName -Width $Width -Height $Height
