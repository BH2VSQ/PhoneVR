[CmdletBinding()]
param(
    [switch]$SkipTests,
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$repoRoot = Get-PhoneVRRepositoryRoot
if (-not $Version) {
    $versionFile = Join-Path $repoRoot "VERSION"
    if (Test-Path $versionFile) {
        $Version = ([System.IO.File]::ReadAllText($versionFile)).Trim()
    }
}
if (-not $Version -or $Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "A valid semantic version is required. Set VERSION or pass -Version."
}
$hostRoot = Join-Path $repoRoot "windows-host"
$outputRoot = Join-Path $repoRoot "dist\windows"

$go = Get-Command go.exe -ErrorAction SilentlyContinue
if (-not $go) {
    $go = Get-Command go -ErrorAction SilentlyContinue
}
if (-not $go) {
    throw "Go was not found. Install Go 1.22 or newer and add go.exe to PATH."
}

Write-BuildStep "Go environment"
& $go.Source version
if ($LASTEXITCODE -ne 0) {
    throw "Unable to run Go."
}

if (-not $SkipTests) {
    Write-BuildStep "Running Windows Host unit tests"
    Invoke-PhoneVRCommand -FilePath $go.Source -ArgumentList @("test", "-v", "./...") -WorkingDirectory $hostRoot -FailureMessage "Go unit tests failed"

    Write-BuildStep "Running go vet"
    Invoke-PhoneVRCommand -FilePath $go.Source -ArgumentList @("vet", "./...") -WorkingDirectory $hostRoot -FailureMessage "go vet failed"
}

New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
$oldGoos = $env:GOOS
$oldGoarch = $env:GOARCH
$oldCgo = $env:CGO_ENABLED

try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"

    Write-BuildStep "Building Windows x64 Host"
    $guiBuildArgs = @(
        "build", "-trimpath", "-ldflags=-s -w -H=windowsgui -X main.appVersion=$Version",
        "-o", (Join-Path $outputRoot "PhoneVRHost.exe"),
        "./cmd/phonevr-host"
    )
    Invoke-PhoneVRCommand -FilePath $go.Source -ArgumentList $guiBuildArgs -WorkingDirectory $hostRoot -FailureMessage "Windows GUI Host build failed"

    # A console build is retained for diagnostics and headless deployments.
    $consoleBuildArgs = @(
        "build", "-trimpath", "-ldflags=-s -w -X main.appVersion=$Version",
        "-o", (Join-Path $outputRoot "PhoneVRHost-console.exe"),
        "./cmd/phonevr-host"
    )
    Invoke-PhoneVRCommand -FilePath $go.Source -ArgumentList $consoleBuildArgs -WorkingDirectory $hostRoot -FailureMessage "Windows console Host build failed"
}
finally {
    $env:GOOS = $oldGoos
    $env:GOARCH = $oldGoarch
    $env:CGO_ENABLED = $oldCgo
}

$guiExePath = Join-Path $outputRoot "PhoneVRHost.exe"
$consoleExePath = Join-Path $outputRoot "PhoneVRHost-console.exe"
foreach ($artifact in @($guiExePath, $consoleExePath)) {
    if (-not (Test-Path $artifact)) {
        throw "Windows Host EXE was not produced: $artifact"
    }
}

$hashLines = @()
foreach ($artifact in @($guiExePath, $consoleExePath)) {
    $hash = (Get-FileHash $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
    $hashLines += "$hash  $(Split-Path $artifact -Leaf)"
}
$hashLines | Set-Content (Join-Path $outputRoot "SHA256SUMS.txt") -Encoding ASCII

Write-BuildStep "Windows Host build completed"
Write-Host $guiExePath -ForegroundColor Green
Write-Host $consoleExePath -ForegroundColor DarkGray
Write-Host "SHA256SUMS.txt updated." -ForegroundColor DarkGray
