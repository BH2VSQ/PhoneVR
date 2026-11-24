[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$WindowsDriverSamplesRoot,
    [ValidateSet("Debug", "Release")]
    [string]$Configuration = "Debug",
    [switch]$Install,
    [string]$DevconPath = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$samplesRoot = (Resolve-Path -LiteralPath $WindowsDriverSamplesRoot).Path
$sysvadRoot = Join-Path $samplesRoot "audio\sysvad"
$project = Join-Path $sysvadRoot "TabletAudioSample\TabletAudioSample.vcxproj"
$solution = Join-Path $sysvadRoot "sysvad.sln"
if (-not (Test-Path $project) -or -not (Test-Path $solution)) {
    throw "SysVAD TabletAudioSample project or sysvad.sln was not found under: $sysvadRoot"
}
$requiredCommit = "d49bb7d72f3651952b2b6e4ad28d14fe2247eb69"
$git = Get-Command git.exe -ErrorAction SilentlyContinue
if (-not $git) { $git = Get-Command git -ErrorAction SilentlyContinue }
if (-not $git) { throw "Git is required to verify the pinned Windows-driver-samples source revision." }
if (-not (Test-Path (Join-Path $samplesRoot ".git"))) {
    throw "WindowsDriverSamplesRoot must be a Git checkout, not a source archive."
}
$sourceCommit = (& $git.Source -C $samplesRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $sourceCommit -ne $requiredCommit) {
    throw "Windows-driver-samples must be checked out at $requiredCommit. Current revision: $sourceCommit"
}

$python = Get-Command python.exe -ErrorAction SilentlyContinue
if (-not $python) { $python = Get-Command python -ErrorAction SilentlyContinue }
if (-not $python) { throw "Python 3 is required to patch the SysVAD capture miniport." }
$msbuild = Get-Command msbuild.exe -ErrorAction SilentlyContinue
if (-not $msbuild) { $msbuild = Get-Command msbuild -ErrorAction SilentlyContinue }
if (-not $msbuild) {
    $vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio\Installer\vswhere.exe"
    if (Test-Path $vswhere) {
        $vs = & $vswhere -latest -products * -requires Microsoft.Component.MSBuild -property installationPath
        if ($vs) {
            $candidate = Join-Path $vs "MSBuild\Current\Bin\MSBuild.exe"
            if (Test-Path $candidate) { $msbuild = [pscustomobject]@{ Source = $candidate } }
        }
    }
}
if (-not $msbuild) { throw "MSBuild was not found. Install Visual Studio with Desktop C++ and Windows Driver Kit components." }

Write-Host "Applying PhoneVR WaveRT microphone changes to SysVAD..." -ForegroundColor Cyan
& $python.Source (Join-Path $PSScriptRoot "patch_sysvad.py") $sysvadRoot
if ($LASTEXITCODE -ne 0) { throw "SysVAD source patch failed with exit code $LASTEXITCODE" }

Write-Host "Building the PhoneVR Microphone endpoint package ($Configuration|x64)..." -ForegroundColor Cyan
# Build through the solution so its explicit TabletAudioSample -> EndpointsCommon
# project dependency is honored and the patched WaveRT miniport library is rebuilt.
& $msbuild.Source $solution /t:TabletAudioSample /m /nologo /v:minimal "/p:Configuration=$Configuration" "/p:Platform=x64" "/p:SignMode=TestSign"
if ($LASTEXITCODE -ne 0) { throw "TabletAudioSample/EndpointsCommon WDK solution build failed with exit code $LASTEXITCODE" }

$infCandidates = Get-ChildItem -LiteralPath (Join-Path $sysvadRoot "TabletAudioSample") -Recurse -Filter ComponentizedAudioSample.inf -File |
    Where-Object { (Get-Content -LiteralPath $_.FullName -Raw) -match "PhoneVR Microphone" } |
    Sort-Object LastWriteTime -Descending
if (-not $infCandidates) {
    throw "Build completed, but no ComponentizedAudioSample.inf containing 'PhoneVR Microphone' was found. Inspect the TabletAudioSample build output and WDK packaging log."
}
$packageInf = $infCandidates[0]
$rootHardwareId = "Root\sysvad_ComponentizedAudioSample"
$infText = Get-Content -LiteralPath $packageInf.FullName -Raw
if ($infText -notmatch [regex]::Escape($rootHardwareId)) {
    throw "The packaged INF does not declare the expected Root hardware ID '$rootHardwareId': $($packageInf.FullName)"
}
Write-Host "PhoneVR Microphone driver package: $($packageInf.FullName)" -ForegroundColor Green

if ($Install) {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Run this script from an elevated PowerShell window to install the test driver."
    }
    $bootConfig = & bcdedit.exe /enum "{current}"
    if (($bootConfig -join "`n") -notmatch "(?im)^\s*testsigning\s+Yes\s*$") {
        throw "Windows test-signing mode is not enabled. Enable it with: bcdedit /set testsigning on ; then reboot before installing this development driver."
    }
    $devcon = $null
    if ($DevconPath) {
        if (-not (Test-Path -LiteralPath $DevconPath -PathType Leaf)) {
            throw "DevCon executable was not found: $DevconPath"
        }
        $devcon = [pscustomobject]@{ Source = (Resolve-Path -LiteralPath $DevconPath).Path }
    }
    else {
        $devcon = Get-Command devcon.exe -ErrorAction SilentlyContinue
        if (-not $devcon) { $devcon = Get-Command devcon -ErrorAction SilentlyContinue }
        if (-not $devcon) {
            $toolRoots = @()
            if ($env:WindowsSdkDir) { $toolRoots += (Join-Path $env:WindowsSdkDir "bin") }
            if (${env:ProgramFiles(x86)}) { $toolRoots += (Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\Tools") }
            if ($env:ProgramFiles) { $toolRoots += (Join-Path $env:ProgramFiles "Windows Kits\11\Tools") }
            $devconCandidate = $null
            foreach ($toolRoot in $toolRoots) {
                if (-not (Test-Path -LiteralPath $toolRoot)) { continue }
                $devconCandidate = Get-ChildItem -LiteralPath $toolRoot -Recurse -Filter devcon.exe -File -ErrorAction SilentlyContinue |
                    Where-Object { $_.FullName -match "(?i)[\\/]x64[\\/]devcon\.exe$" } |
                    Sort-Object FullName -Descending | Select-Object -First 1
                if ($devconCandidate) { break }
            }
            if ($devconCandidate) {
                $devcon = [pscustomobject]@{ Source = $devconCandidate.FullName }
            }
        }
    }
    if (-not $devcon) {
        throw "WDK DevCon was not found. Install the WDK tools or pass -DevconPath 'C:\path\to\devcon.exe'."
    }

    # pnputil /add-driver only stages/updates the package. SysVAD is a root-
    # enumerated virtual adapter, so DevCon must create the Root hardware node.
    Write-Host "Creating the PhoneVR Microphone root-enumerated audio device..." -ForegroundColor Cyan
    & $devcon.Source install $packageInf.FullName $rootHardwareId
    if ($LASTEXITCODE -ne 0) { throw "DevCon failed to create '$rootHardwareId' (exit code $LASTEXITCODE)" }
    Write-Host "PhoneVR Microphone device created. Verify it in Device Manager and Windows Sound > Input." -ForegroundColor Green
}
