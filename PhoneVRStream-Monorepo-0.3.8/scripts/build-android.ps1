[CmdletBinding()]
param(
    [ValidateSet("Debug", "Release", "Both")]
    [string]$Configuration = "Both",
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$repoRoot = Get-PhoneVRRepositoryRoot
$androidRoot = Join-Path $repoRoot "android"
$wrapper = Join-Path $androidRoot "gradlew.bat"
$outputRoot = Join-Path $repoRoot "dist\android"

if (-not (Test-Path $wrapper)) {
    throw "Android Gradle Wrapper not found: $wrapper"
}

Write-BuildStep "Checking Android build environment"
Resolve-PhoneVRJava | Out-Null
Resolve-PhoneVRAndroidSdk | Out-Null

$tasks = @()
if (-not $SkipTests) {
    $tasks += ":app:testDebugUnitTest"
}
if ($Configuration -eq "Debug" -or $Configuration -eq "Both") {
    $tasks += ":app:assembleDebug"
}
if ($Configuration -eq "Release" -or $Configuration -eq "Both") {
    $tasks += ":app:assembleRelease"
}

Write-BuildStep "Running Android Gradle tasks: $($tasks -join ', ')"
$gradleArgs = @("--no-daemon", "--stacktrace") + $tasks
Invoke-PhoneVRCommand -FilePath $wrapper -ArgumentList $gradleArgs -WorkingDirectory $androidRoot -FailureMessage "Android tests or APK build failed"

New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
$artifacts = @()

if ($Configuration -eq "Debug" -or $Configuration -eq "Both") {
    $debugApk = Join-Path $androidRoot "app\build\outputs\apk\debug\app-debug.apk"
    if (-not (Test-Path $debugApk)) {
        throw "Debug APK was not produced: $debugApk"
    }
    $destination = Join-Path $outputRoot "PhoneVRStream-Android-debug.apk"
    Copy-Item $debugApk $destination -Force
    $artifacts += $destination
}

if ($Configuration -eq "Release" -or $Configuration -eq "Both") {
    $releaseDir = Join-Path $androidRoot "app\build\outputs\apk\release"
    $releaseApk = Get-ChildItem -Path $releaseDir -Filter "*.apk" -File -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $releaseApk) {
        throw "Release APK was not produced in: $releaseDir"
    }
    $destination = Join-Path $outputRoot "PhoneVRStream-Android-release-unsigned.apk"
    Copy-Item $releaseApk.FullName $destination -Force
    $artifacts += $destination
}

Write-BuildStep "Android build completed"
foreach ($artifact in $artifacts) {
    $file = Get-Item $artifact
    $hash = (Get-FileHash $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
    Write-Host ("{0} ({1:N2} MB)" -f $file.FullName, ($file.Length / 1MB)) -ForegroundColor Green
    Write-Host "SHA256: $hash" -ForegroundColor DarkGray
}
