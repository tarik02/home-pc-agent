$ErrorActionPreference = 'Stop'

$Script:PhysicalDisplayName = 'PG32UCDM'
$Script:VirtualDisplayName = 'VDD by MTT'
$Script:VirtualPnPNames = @('IddSampleDriver Device HDR', 'Virtual Display Driver')

function Get-DisplayPropertyValue
{
    param(
        $Display,
        [string[]]$Names
    )

    foreach ($name in $Names)
    {
        if ($null -ne $Display -and $Display.PSObject.Properties.Match($name).Count -gt 0)
        {
            $value = $Display.$name
            if ($null -ne $value -and "$value" -ne '')
            {
                return $value
            }
        }
    }
    return $null
}

function Format-DisplayRefreshRate
{
    param([double]$Rate)

    if ([math]::Abs($Rate - 240.016) -lt 0.01)
    {
        return '240.016'
    }
    if ([math]::Abs($Rate - [math]::Round($Rate)) -lt 0.01)
    {
        return [string][int][math]::Round($Rate)
    }
    return [string]$Rate
}

function ConvertTo-RefreshRateSetterParam
{
    param([double]$Rate)

    if ([math]::Abs($Rate - 240.016) -lt 0.01)
    {
        return '240_016'
    }
    return [string][int][math]::Round($Rate)
}

function Initialize-NativeDisplayApi
{
    if ("NativeDisplay" -as [type])
    {
        return
    }

    Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class NativeDisplay {
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Ansi)]
    public struct DEVMODE {
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)]
        public string dmDeviceName;
        public short dmSpecVersion;
        public short dmDriverVersion;
        public short dmSize;
        public short dmDriverExtra;
        public int dmFields;
        public int dmPositionX;
        public int dmPositionY;
        public int dmDisplayOrientation;
        public int dmDisplayFixedOutput;
        public short dmColor;
        public short dmDuplex;
        public short dmYResolution;
        public short dmTTOption;
        public short dmCollate;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)]
        public string dmFormName;
        public short dmLogPixels;
        public int dmBitsPerPel;
        public int dmPelsWidth;
        public int dmPelsHeight;
        public int dmDisplayFlags;
        public int dmDisplayFrequency;
        public int dmICMMethod;
        public int dmICMIntent;
        public int dmMediaType;
        public int dmDitherType;
        public int dmReserved1;
        public int dmReserved2;
        public int dmPanningWidth;
        public int dmPanningHeight;
    }
    [DllImport("user32.dll", CharSet = CharSet.Ansi)]
    public static extern bool EnumDisplaySettings(string lpszDeviceName, int iModeNum, ref DEVMODE lpDevMode);
}
"@
}

function Get-DisplayAspectRatio
{
    param(
        [int]$Width,
        [int]$Height
    )

    if ($Height -eq 0) { return 0.0 }
    return [double]$Width / [double]$Height
}

function Test-DisplayAspectRatioMatch
{
    param(
        [double]$Left,
        [double]$Right,
        [double]$Tolerance = 0.03
    )

    if ($Left -eq 0 -or $Right -eq 0) { return $false }
    return [math]::Abs($Left - $Right) -le $Tolerance
}

function Get-DisplayEnumModeMap
{
    param([Parameter(Mandatory = $true)]$Display)

    Initialize-NativeDisplayApi
    $device = $Display.GdiDeviceName
    if ([string]::IsNullOrWhiteSpace($device))
    {
        throw "Display '$($Display.DisplayName)' has no GdiDeviceName."
    }

    $map = @{}
    $index = 0
    $devMode = New-Object NativeDisplay+DEVMODE
    while ([NativeDisplay]::EnumDisplaySettings($device, $index, [ref]$devMode))
    {
        $width = [int]$devMode.dmPelsWidth
        $height = [int]$devMode.dmPelsHeight
        if ($width -ge 640 -and $height -ge 480)
        {
            $key = '{0}x{1}' -f $width, $height
            if (-not $map.ContainsKey($key))
            {
                $map[$key] = @{}
            }
            $rate = [int]$devMode.dmDisplayFrequency
            $map[$key][$rate] = $true
        }
        $index++
    }
    return $map
}

function Get-DisplayResolutionOptionsJson
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Get-DisplayByName -DisplayName $DisplayName
    if ($null -eq $display -or -not $display.DisplayId)
    {
        return '[]'
    }
    $display = Assert-DisplayPresent -Display $display -DisplayName $DisplayName
    $modeMap = Get-DisplayEnumModeMap -Display $display
    $nativeWidth = [int]$display.Mode.Width
    $nativeHeight = [int]$display.Mode.Height
    $targetAspect = Get-DisplayAspectRatio -Width $nativeWidth -Height $nativeHeight
    $minWidth = [math]::Max(1280, [int]([math]::Round($nativeWidth * 0.5)))

    $resolutions = foreach ($key in $modeMap.Keys)
    {
        if ($key -notmatch '^(\d+)x(\d+)$') { continue }
        $width = [int]$matches[1]
        $height = [int]$matches[2]
        if ($width -lt $minWidth) { continue }
        $aspect = Get-DisplayAspectRatio -Width $width -Height $height
        if (-not (Test-DisplayAspectRatioMatch -Left $aspect -Right $targetAspect)) { continue }
        [pscustomobject]@{
            Width = $width
            Height = $height
            Pixels = $width * $height
        }
    }

    $options = @()
    foreach ($resolution in ($resolutions | Sort-Object Pixels -Descending -Unique))
    {
        $name = '{0}x{1}' -f $resolution.Width, $resolution.Height
        $options += [ordered]@{
            name = $name
            parameters = [ordered]@{
                DisplayName = $DisplayName
                Width = $resolution.Width
                Height = $resolution.Height
            }
        }
    }

    if ($options.Count -eq 0)
    {
        $name = Get-DisplayResolutionQuery -DisplayName $DisplayName
        if ($name -match '^(\d+)x(\d+)$')
        {
            $options = @([ordered]@{
                name = $name
                parameters = [ordered]@{
                    DisplayName = $DisplayName
                    Width = [int]$matches[1]
                    Height = [int]$matches[2]
                }
            })
        }
    }
    return (ConvertTo-DisplayOptionsJson -Options $options)
}

function Get-DisplayRefreshOptionsJson
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Get-DisplayByName -DisplayName $DisplayName
    if ($null -eq $display -or -not $display.DisplayId)
    {
        return '[]'
    }
    $display = Assert-DisplayPresent -Display $display -DisplayName $DisplayName
    $modeMap = Get-DisplayEnumModeMap -Display $display
    $current = $display.Mode
    $resolutionKey = '{0}x{1}' -f [int]$current.Width, [int]$current.Height
    $rates = @()
    if ($modeMap.ContainsKey($resolutionKey))
    {
        $rates = $modeMap[$resolutionKey].Keys | ForEach-Object { [int]$_ } | Where-Object { $_ -ge 60 } | Sort-Object -Descending
    }

    $seen = @{}
    $options = @()
    foreach ($rate in $rates)
    {
        $bucket = [int][math]::Round($rate)
        if ($seen.ContainsKey($bucket)) { continue }
        $seen[$bucket] = $true
        $label = '{0} Hz' -f (Format-DisplayRefreshRate -Rate ([double]$bucket))
        $options += [ordered]@{
            name = $label
            parameters = [ordered]@{
                DisplayName = $DisplayName
                RefreshRate = (ConvertTo-RefreshRateSetterParam -Rate ([double]$bucket))
            }
        }
    }

    if ($options.Count -eq 0)
    {
        $rate = Get-DisplayRefreshQuery -DisplayName $DisplayName
        $options = @([ordered]@{
            name = '{0} Hz' -f $rate
            parameters = [ordered]@{
                DisplayName = $DisplayName
                RefreshRate = (ConvertTo-RefreshRateSetterParam -Rate ([double]$rate))
            }
        })
    }
    return (ConvertTo-DisplayOptionsJson -Options $options)
}

function ConvertTo-DisplayOptionsJson
{
    param([array]$Options)

    if ($null -eq $Options -or $Options.Count -eq 0)
    {
        return '[]'
    }
    if ($Options.Count -eq 1)
    {
        return ('[{0}]' -f ($Options[0] | ConvertTo-Json -Compress -Depth 4))
    }
    return ($Options | ConvertTo-Json -Compress -Depth 4)
}

function Get-DisplayTargetModeEntries
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    $modeMap = Get-DisplayEnumModeMap -Display $display
    $modes = @()
    foreach ($key in $modeMap.Keys)
    {
        if ($key -notmatch '^(\d+)x(\d+)$') { continue }
        $width = [int]$matches[1]
        $height = [int]$matches[2]
        foreach ($rate in $modeMap[$key].Keys)
        {
            $modes += [pscustomobject]@{
                Width = $width
                Height = $height
                RefreshRate = [double]$rate
            }
        }
    }
    return ,$modes
}

function Get-DisplayScaleOptionsJson
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Get-DisplayByName -DisplayName $DisplayName
    if ($null -eq $display -or -not $display.DisplayId)
    {
        return '[]'
    }
    $display = Assert-DisplayPresent -Display $display -DisplayName $DisplayName
    $scaleInfo = Get-DisplayScale -DisplayId $display.DisplayId
    if ($null -eq $scaleInfo -or $null -eq $scaleInfo.MaxScale)
    {
        throw "Scale is not available for display '$DisplayName'."
    }
    $options = @()
    for ($scale = 100; $scale -le [int]$scaleInfo.MaxScale; $scale += 25)
    {
        $options += [ordered]@{
            name = '{0}%' -f $scale
            parameters = [ordered]@{
                DisplayName = $DisplayName
                Scale = [string]$scale
            }
        }
    }
    return (ConvertTo-DisplayOptionsJson -Options $options)
}

function Get-DisplayModeText
{
    param($Display)

    return Get-DisplayPropertyValue -Display $Display -Names @('Mode', 'CurrentMode')
}

function Get-DisplayScaleQuery
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    $scaleInfo = Get-DisplayScale -DisplayId $display.DisplayId
    if ($null -eq $scaleInfo -or $null -eq $scaleInfo.CurrentScale)
    {
        throw "Scale is not available for display '$DisplayName'."
    }
    return [string][int]$scaleInfo.CurrentScale
}

function Get-DisplayResolutionQuery
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    $mode = Get-DisplayModeText -Display $display
    if ($null -ne $mode -and $mode -match '^(\d+)x(\d+)')
    {
        return ('{0}x{1}' -f [int]$matches[1], [int]$matches[2])
    }

    $width = Get-DisplayPropertyValue -Display $display -Names @('Width', 'HorizontalResolution', 'CurrentHorizontalResolution')
    $height = Get-DisplayPropertyValue -Display $display -Names @('Height', 'VerticalResolution', 'CurrentVerticalResolution')
    if ($null -eq $width -or $null -eq $height)
    {
        throw "Resolution is not available for display '$DisplayName'."
    }
    return ('{0}x{1}' -f [int]$width, [int]$height)
}

function Get-DisplayRefreshQuery
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    $mode = Get-DisplayModeText -Display $display
    if ($null -ne $mode -and $mode -match '@([\d,]+)\s*Hz')
    {
        $rate = [double]($matches[1].Replace(',', '.'))
        return Format-DisplayRefreshRate -Rate $rate
    }

    $refresh = Get-DisplayPropertyValue -Display $display -Names @('RefreshRate', 'CurrentRefreshRate', 'Frequency')
    if ($null -eq $refresh)
    {
        throw "Refresh rate is not available for display '$DisplayName'."
    }
    return Format-DisplayRefreshRate -Rate ([double]$refresh)
}

function Get-DisplayByName
{
    param(
        [Parameter(Mandatory = $true)]
        [string]$DisplayName
    )

    Get-DisplayInfo | Where-Object { $_.DisplayName -eq $DisplayName } | Select-Object -First 1
}

function Wait-DisplayByName
{
    param(
        [Parameter(Mandatory = $true)]
        [string]$DisplayName,

        [switch]$RequireActive,

        [int]$TimeoutSeconds = 5
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do
    {
        $display = Get-DisplayByName -DisplayName $DisplayName
        if ($null -ne $display -and $display.DisplayId -and (-not $RequireActive -or $display.Active))
        {
            return $display
        }
        Start-Sleep -Milliseconds 200
    } while ((Get-Date) -lt $deadline)

    if ($RequireActive)
    {
        throw "Display '$DisplayName' was not active after $TimeoutSeconds seconds."
    }
    throw "Display '$DisplayName' was not found after $TimeoutSeconds seconds."
}

function Disable-DisplayIfPresent
{
    param(
        $Display,
        [Parameter(Mandatory = $true)]
        [string]$DisplayName
    )

    if ($null -eq $Display -or -not $Display.DisplayId -or -not $Display.Active)
    {
        return
    }
    Disable-Display -DisplayId $Display.DisplayId
}

function Assert-DisplayPresent
{
    param(
        $Display,
        [Parameter(Mandatory = $true)]
        [string]$DisplayName
    )

    if ($null -eq $Display -or -not $Display.DisplayId)
    {
        throw "Display '$DisplayName' was not found or has no DisplayId."
    }
    $Display
}

function Get-VirtualDisplayPnPDevice
{
    Get-PnpDevice -Class Display -ErrorAction SilentlyContinue |
        Where-Object { $Script:VirtualPnPNames -contains $_.FriendlyName }
}

function Set-VirtualDisplayDriverEnabled
{
    param([bool]$Enabled)

    $device = Get-VirtualDisplayPnPDevice
    if ($null -eq $device)
    {
        throw 'Virtual display PnP device was not found.'
    }
    if ($Enabled)
    {
        $device | Enable-PnpDevice -Confirm:$false
    }
    else
    {
        $device | Disable-PnpDevice -Confirm:$false
    }
}

function Test-DisplayActive
{
    param([Parameter(Mandatory = $true)][string]$DisplayName)

    $display = Get-DisplayByName -DisplayName $DisplayName
    return [bool]($null -ne $display -and $display.DisplayId -and $display.Active)
}

function Set-PhysicalDisplayEnabled
{
    param([bool]$Enabled)

    $display = Get-DisplayByName -DisplayName $Script:PhysicalDisplayName
    $display = Assert-DisplayPresent -Display $display -DisplayName $Script:PhysicalDisplayName
    if ($Enabled)
    {
        Enable-Display -DisplayId $display.DisplayId
        nircmd.exe monitor on
    }
    else
    {
        Disable-Display -DisplayId $display.DisplayId
    }
}

function Set-VirtualDisplayEnabled
{
    param([bool]$Enabled)

    if ($Enabled)
    {
        Set-VirtualDisplayDriverEnabled -Enabled $true
        $display = Assert-DisplayPresent -Display (Wait-DisplayByName -DisplayName $Script:VirtualDisplayName) -DisplayName $Script:VirtualDisplayName
        Enable-Display -DisplayId $display.DisplayId
        Wait-DisplayByName -DisplayName $Script:VirtualDisplayName -RequireActive | Out-Null
    }
    else
    {
        $display = Get-DisplayByName -DisplayName $Script:VirtualDisplayName
        Disable-DisplayIfPresent -Display $display -DisplayName $Script:VirtualDisplayName
        Set-VirtualDisplayDriverEnabled -Enabled $false
    }
}

function Set-Display-Default
{
    param(
        [ValidateRange(100, [int]::MaxValue)]
        [int]$Scale = 100
    )

    Set-VirtualDisplayDriverEnabled -Enabled $false
    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $Script:PhysicalDisplayName) -DisplayName $Script:PhysicalDisplayName
    Enable-Display -DisplayId $display.DisplayId
    Set-DisplayRefreshRate -DisplayId $display.DisplayId -RefreshRate 240.016 -AllowChanges
    Set-DisplayResolution -DisplayId $display.DisplayId -Width 3840 -Height 2160
    Set-DisplayScale -DisplayId $display.DisplayId -Scale $Scale
    nircmd.exe monitor on
}

function Set-Display-Virtual
{
    param(
        [int]$Width = 2420,
        [int]$Height = 1668,
        [int]$RefreshRate = 120,
        [int]$Scale = 175
    )

    Set-VirtualDisplayDriverEnabled -Enabled $true
    $physical = Get-DisplayByName -DisplayName $Script:PhysicalDisplayName
    $virtual = Assert-DisplayPresent -Display (Wait-DisplayByName -DisplayName $Script:VirtualDisplayName) -DisplayName $Script:VirtualDisplayName
    Enable-Display -DisplayId $virtual.DisplayId
    $virtual = Wait-DisplayByName -DisplayName $Script:VirtualDisplayName -RequireActive
    Set-DisplayResolution -DisplayId $virtual.DisplayId -Width $Width -Height $Height
    Set-DisplayRefreshRate -DisplayId $virtual.DisplayId -RefreshRate $RefreshRate -AllowChanges
    Set-DisplayScale -DisplayId $virtual.DisplayId -Scale $Scale
    Disable-DisplayIfPresent -Display $physical -DisplayName $Script:PhysicalDisplayName
}

function Set-DisplayScaleOn
{
    param(
        [Parameter(Mandatory = $true)]
        [ValidateRange(100, 500)]
        [int]$Scale,

        [Parameter(Mandatory = $true)]
        [ValidateSet('PG32UCDM', 'VDD by MTT')]
        [string]$DisplayName
    )

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    Set-DisplayScale -DisplayId $display.DisplayId -Scale $Scale
}

function Set-DisplayResolutionOn
{
    param(
        [Parameter(Mandatory = $true)]
        [ValidateRange(640, 10000)]
        [int]$Width,

        [Parameter(Mandatory = $true)]
        [ValidateRange(480, 10000)]
        [int]$Height,

        [Parameter(Mandatory = $true)]
        [ValidateSet('PG32UCDM', 'VDD by MTT')]
        [string]$DisplayName
    )

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    Set-DisplayResolution -DisplayId $display.DisplayId -Width $Width -Height $Height
}

function Set-DisplayRefreshOn
{
    param(
        [Parameter(Mandatory = $true)]
        [double]$RefreshRate,

        [Parameter(Mandatory = $true)]
        [ValidateSet('PG32UCDM', 'VDD by MTT')]
        [string]$DisplayName
    )

    $display = Assert-DisplayPresent -Display (Get-DisplayByName -DisplayName $DisplayName) -DisplayName $DisplayName
    Set-DisplayRefreshRate -DisplayId $display.DisplayId -RefreshRate $RefreshRate -AllowChanges
}

function Set-DefaultAudioDeviceByName
{
    param([Parameter(Mandatory = $true)][string]$Name)

    $device = Get-AudioDevice -List | Where-Object { $_.Name -eq $Name } | Select-Object -First 1
    if ($null -eq $device)
    {
        throw "Audio device '$Name' was not found."
    }
    Set-AudioDevice -ID $device.ID | Out-Null
}
