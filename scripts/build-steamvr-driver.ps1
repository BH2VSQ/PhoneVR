$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$sourceDir = Join-Path $repoRoot 'steamvr-driver'
$buildDir = Join-Path $repoRoot 'build/steamvr-driver'
$installDir = Join-Path $repoRoot 'dist/steamvr-driver'

if (-not (Get-Command cmake -ErrorAction SilentlyContinue)) {
    throw 'CMake 3.24 or newer is required to build the SteamVR driver.'
}
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    throw 'Git is required by CMake FetchContent to retrieve the pinned OpenVR SDK.'
}
if (-not $env:VisualStudioVersion -and -not (Get-Command cl.exe -ErrorAction SilentlyContinue)) {
    Write-Host 'CMake will use the installed Visual Studio x64 generator/toolchain.'
}

if (Test-Path $installDir) { Remove-Item -LiteralPath $installDir -Recurse -Force }
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
cmake -S $sourceDir -B $buildDir -A x64
if ($LASTEXITCODE -ne 0) { throw 'SteamVR driver CMake configure failed.' }
cmake --build $buildDir --config Release --parallel
if ($LASTEXITCODE -ne 0) { throw 'SteamVR driver build failed.' }
cmake --install $buildDir --config Release --prefix $installDir
if ($LASTEXITCODE -ne 0) { throw 'SteamVR driver install failed.' }

$manifest = Join-Path $installDir 'driver.vrdrivermanifest'
$dll = Join-Path $installDir 'bin/win64/driver_phonevr.dll'
if (-not (Test-Path $manifest) -or -not (Test-Path $dll)) {
    throw 'SteamVR driver package is incomplete.'
}
$hash = (Get-FileHash -Algorithm SHA256 $dll).Hash.ToLowerInvariant()
Set-Content -Path (Join-Path $installDir 'SHA256SUMS.txt') -Value "$hash  bin/win64/driver_phonevr.dll" -Encoding ascii
Write-Host "SteamVR driver package: $installDir"
