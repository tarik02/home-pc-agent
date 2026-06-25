<#
.SYNOPSIS
home-pc-agent runner entrypoint for desktop mode presets.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('default', 'gaming-chair', 'gaming-stream', 'remote', 'remote-mbp', 'remote-fhd', IgnoreCase = $true)]
    [string]$Mode
)

$commonPath = Join-Path $PSScriptRoot 'Display.Common.ps1'
. $commonPath

switch ($Mode.ToLower())
{
    'default'
    {
        Set-Display-Default -Scale 100
        Set-DefaultAudioDeviceByName -Name 'Speakers (PRO)'
        & 'C:\Program Files (x86)\Steam\steam.exe' -start steam://stopstreaming
        & 'C:\Program Files (x86)\Steam\steam.exe' -start steam://close/bigpicture
        Start-Sleep -Seconds 1
        nircmd.exe win min title Steam
    }

    'gaming-chair'
    {
        Set-Display-Default -Scale 175
        Set-DefaultAudioDeviceByName -Name 'PG32UCDM (NVIDIA High Definition Audio)'
        & 'C:\Program Files (x86)\Steam\steam.exe' -start steam://open/bigpicture -fulldesktopres
    }

    'gaming-stream'
    {
        Set-Display-Virtual
        Set-DefaultAudioDeviceByName -Name 'Speakers (Steam Streaming Speakers)'
    }

    'remote'
    {
        Set-Display-Virtual -Width 3840 -Height 2160 -RefreshRate 240 -Scale 100
    }

    'remote-mbp'
    {
        Set-Display-Virtual -Width 2560 -Height 1600 -RefreshRate 60 -Scale 200
    }

    'remote-fhd'
    {
        Set-Display-Virtual -Width 1920 -Height 1080 -RefreshRate 240 -Scale 100
    }
}
