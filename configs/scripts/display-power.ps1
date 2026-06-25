[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('PG32UCDM', 'VDD by MTT', IgnoreCase = $true)]
    [string]$DisplayName,

    [Parameter(Mandatory = $true)]
    [ValidateSet('on', 'off', 'query', IgnoreCase = $true)]
    [string]$State
)

$commonPath = Join-Path $PSScriptRoot 'Display.Common.ps1'
. $commonPath

switch ($DisplayName.ToUpper())
{
    'PG32UCDM'
    {
        switch ($State.ToLower())
        {
            'on' { Set-PhysicalDisplayEnabled -Enabled $true }
            'off' { Set-PhysicalDisplayEnabled -Enabled $false }
            'query'
            {
                if (Test-DisplayActive -DisplayName $Script:PhysicalDisplayName) { Write-Output 'true' } else { Write-Output 'false' }
            }
        }
    }
    'VDD BY MTT'
    {
        switch ($State.ToLower())
        {
            'on' { Set-VirtualDisplayEnabled -Enabled $true }
            'off' { Set-VirtualDisplayEnabled -Enabled $false }
            'query'
            {
                if (Test-DisplayActive -DisplayName $Script:VirtualDisplayName) { Write-Output 'true' } else { Write-Output 'false' }
            }
        }
    }
}
