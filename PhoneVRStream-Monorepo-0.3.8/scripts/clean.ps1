[CmdletBinding(SupportsShouldProcess = $true)]
param()

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$targets = @(
    (Join-Path $repoRoot "dist"),
    (Join-Path $repoRoot "android\build"),
    (Join-Path $repoRoot "android\app\build"),
    (Join-Path $repoRoot "windows-host\dist")
)

foreach ($target in $targets) {
    if (Test-Path $target) {
        if ($PSCmdlet.ShouldProcess($target, "Remove generated build output")) {
            Remove-Item $target -Recurse -Force
            Write-Host "Removed: $target" -ForegroundColor Yellow
        }
    }
}

Write-Host "Clean completed. Android SDK, local.properties and Gradle caches were preserved." -ForegroundColor Green
