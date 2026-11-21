param(
    [Parameter(Mandatory = $true)]
    [string]$DriverDirectory,
    [switch]$Remove
)
$ErrorActionPreference = 'Stop'
$driverPath = (Resolve-Path $DriverDirectory).Path
$manifest = Join-Path $driverPath 'driver.vrdrivermanifest'
if (-not (Test-Path $manifest)) { throw "Missing driver manifest: $manifest" }

$steamPath = (Get-ItemProperty -Path 'HKCU:\Software\Valve\Steam' -ErrorAction Stop).SteamPath
$vrPathReg = Join-Path $steamPath 'steamapps/common/SteamVR/bin/win64/vrpathreg.exe'
if (-not (Test-Path $vrPathReg)) { throw "SteamVR vrpathreg.exe not found: $vrPathReg" }
if ($Remove) {
    & $vrPathReg removedriver $driverPath
} else {
    & $vrPathReg adddriver $driverPath
}
if ($LASTEXITCODE -ne 0) { throw "vrpathreg failed with exit code $LASTEXITCODE" }
