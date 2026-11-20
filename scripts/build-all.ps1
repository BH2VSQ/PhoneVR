[CmdletBinding()]
param(
    [ValidateSet("Debug", "Release", "Both")]
    [string]$AndroidConfiguration = "Both",
    [switch]$SkipTests,
    [switch]$SkipAndroid,
    [switch]$SkipWindows
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$repoRoot = Get-PhoneVRRepositoryRoot
if ($SkipAndroid -and $SkipWindows) {
    throw "At least one build target must remain enabled."
}

$startedAt = Get-Date
$failures = @()

Write-Host "==============================================" -ForegroundColor Cyan
Write-Host " PhoneVRStream - Full Local Build" -ForegroundColor Cyan
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "Repository: $repoRoot"
Write-Host "Android APK: $AndroidConfiguration"
Write-Host "Run tests: $(-not $SkipTests)"

if (-not $SkipAndroid) {
    try {
        & (Join-Path $PSScriptRoot "build-android.ps1") -Configuration $AndroidConfiguration -SkipTests:$SkipTests
    }
    catch {
        $failures += "Android: $($_.Exception.Message)"
        Write-Host "Android build failed: $($_.Exception.Message)" -ForegroundColor Red
    }
}
else {
    Write-Host "Skipping Android build." -ForegroundColor Yellow
}

if (-not $SkipWindows) {
    try {
        & (Join-Path $PSScriptRoot "build-windows-host.ps1") -SkipTests:$SkipTests
    }
    catch {
        $failures += "Windows Host: $($_.Exception.Message)"
        Write-Host "Windows Host build failed: $($_.Exception.Message)" -ForegroundColor Red
    }
}
else {
    Write-Host "Skipping Windows Host build." -ForegroundColor Yellow
}

$elapsed = (Get-Date) - $startedAt
$elapsedText = "{0:hh\:mm\:ss}" -f $elapsed
Write-Host ""
Write-Host "==============================================" -ForegroundColor Cyan

if ($failures.Count -eq 0) {
    Write-Host "All requested targets built successfully in $elapsedText." -ForegroundColor Green
    Write-Host "Artifacts: $(Join-Path $repoRoot 'dist')" -ForegroundColor Green
    exit 0
}

Write-Host "Build completed with errors:" -ForegroundColor Red
foreach ($failure in $failures) {
    Write-Host " - $failure" -ForegroundColor Red
}

$failureSummary = $failures -join "; "
throw "Build failed: $failureSummary"
