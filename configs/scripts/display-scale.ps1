[CmdletBinding()]
param(
    [Parameter(ParameterSetName = 'Set', Mandatory = $true)]
    [ValidateSet('100', '125', '150', '175', '200', '225', '250', IgnoreCase = $true)]
    [string]$Scale,

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
        Write-Output (Get-DisplayScaleQuery -DisplayName $DisplayName)
        exit 0
    }
    'List'
    {
        Write-Output (Get-DisplayScaleOptionsJson -DisplayName $DisplayName)
        exit 0
    }
}

Set-DisplayScaleOn -Scale ([int]$Scale) -DisplayName $DisplayName
