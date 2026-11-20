Set-StrictMode -Version 2.0

$script:PhoneVRRepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

function Write-BuildStep {
    param([Parameter(Mandatory = $true)][string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Invoke-PhoneVRCommand {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$ArgumentList,
        [Parameter(Mandatory = $true)][string]$WorkingDirectory,
        [string]$FailureMessage = "Command failed: $FilePath"
    )

    $exitCode = $null
    Push-Location $WorkingDirectory
    try {
        & $FilePath @ArgumentList
        $exitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }

    if ($null -eq $exitCode -or $exitCode -ne 0) {
        throw "$FailureMessage (exit code: $exitCode)"
    }
}

function Resolve-PhoneVRJava {
    $candidates = @()
    if ($env:JAVA_HOME) {
        $candidates += $env:JAVA_HOME
    }
    if (${env:ProgramFiles}) {
        $candidates += (Join-Path ${env:ProgramFiles} "Android\Android Studio\jbr")
    }
    if ($env:LOCALAPPDATA) {
        $candidates += (Join-Path $env:LOCALAPPDATA "Programs\Android Studio\jbr")
    }

    foreach ($candidate in ($candidates | Select-Object -Unique)) {
        $javaExe = Join-Path $candidate "bin\java.exe"
        if (Test-Path $javaExe) {
            $env:JAVA_HOME = $candidate
            $env:PATH = "$(Join-Path $candidate 'bin');$env:PATH"
            break
        }
    }

    $javaCommand = Get-Command java.exe -ErrorAction SilentlyContinue
    if (-not $javaCommand) {
        throw "Java was not found. Install Android Studio or set JAVA_HOME to JDK 17 or newer."
    }

    # java -version writes to stderr. Do not redirect native stderr into the
    # PowerShell error stream because ErrorActionPreference=Stop would treat
    # the normal version banner as a terminating error in Windows PowerShell 5.1.
    $javaStdOut = [System.IO.Path]::GetTempFileName()
    $javaStdErr = [System.IO.Path]::GetTempFileName()
    $javaProcess = $null
    try {
        $javaProcess = Start-Process -FilePath $javaCommand.Source `
            -ArgumentList '-version' `
            -NoNewWindow `
            -Wait `
            -PassThru `
            -RedirectStandardOutput $javaStdOut `
            -RedirectStandardError $javaStdErr

        $versionOutput = [System.IO.File]::ReadAllText($javaStdOut) + [Environment]::NewLine + [System.IO.File]::ReadAllText($javaStdErr)
        if ($javaProcess.ExitCode -ne 0) {
            throw "Unable to run Java (exit code: $($javaProcess.ExitCode)): $versionOutput"
        }
    }
    finally {
        if (Test-Path $javaStdOut) {
            Remove-Item $javaStdOut -Force -ErrorAction SilentlyContinue
        }
        if (Test-Path $javaStdErr) {
            Remove-Item $javaStdErr -Force -ErrorAction SilentlyContinue
        }
    }

    $versionMatch = [regex]::Match($versionOutput, 'version\s+"(?<version>[^"]+)"')
    if (-not $versionMatch.Success) {
        $versionMatch = [regex]::Match($versionOutput, '(?:openjdk|java)\s+(?<version>\d+(?:\.\d+)*)')
    }
    if (-not $versionMatch.Success) {
        throw "Could not parse Java version output: $versionOutput"
    }

    $versionText = $versionMatch.Groups["version"].Value
    $majorMatch = [regex]::Match($versionText, '^(?:1\.)?(?<major>\d+)')
    if (-not $majorMatch.Success) {
        throw "Could not determine Java major version from: $versionText"
    }
    $javaMajor = [int]$majorMatch.Groups["major"].Value
    if ($javaMajor -lt 17) {
        throw "Detected Java $versionText. PhoneVRStream requires JDK 17 or newer."
    }

    Write-Host "Java $versionText detected: $($javaCommand.Source)" -ForegroundColor DarkGray
}

function Resolve-PhoneVRAndroidSdk {
    $androidProject = Join-Path $script:PhoneVRRepoRoot "android"
    $localProperties = Join-Path $androidProject "local.properties"
    $candidates = @()

    if (Test-Path $localProperties) {
        $localPropertyLines = [System.IO.File]::ReadAllLines($localProperties, [System.Text.Encoding]::UTF8)
        $line = $localPropertyLines | Where-Object { $_ -match '^\s*sdk\.dir\s*=' } | Select-Object -First 1
        if ($line) {
            $value = ($line -replace '^\s*sdk\.dir\s*=\s*', '').Trim()
            $value = $value.Replace('\:', ':').Replace('\\', '\')
            $candidates += $value
        }
    }

    if ($env:ANDROID_HOME) {
        $candidates += $env:ANDROID_HOME
    }
    if ($env:ANDROID_SDK_ROOT) {
        $candidates += $env:ANDROID_SDK_ROOT
    }
    if ($env:LOCALAPPDATA) {
        $candidates += (Join-Path $env:LOCALAPPDATA "Android\Sdk")
    }

    $sdkPath = $null
    foreach ($candidate in ($candidates | Where-Object { $_ } | Select-Object -Unique)) {
        if (Test-Path (Join-Path $candidate "platforms\android-36\android.jar")) {
            $sdkPath = (Resolve-Path $candidate).Path
            break
        }
    }

    if (-not $sdkPath) {
        throw "Android SDK Platform 36 was not found. Install Android 16 (API 36) in Android Studio SDK Manager, or set ANDROID_HOME / ANDROID_SDK_ROOT / android\local.properties."
    }

    $buildTools = Join-Path $sdkPath "build-tools\36.0.0"
    if (-not (Test-Path $buildTools)) {
        throw "Android SDK Build-Tools 36.0.0 is missing. Install it in Android Studio SDK Manager. SDK: $sdkPath"
    }

    $sdkForProperties = $sdkPath -replace '\\', '/'
    $propertyLines = New-Object 'System.Collections.Generic.List[string]'
    $sdkPropertyWritten = $false

    if (Test-Path $localProperties) {
        foreach ($existingLine in [System.IO.File]::ReadAllLines($localProperties, [System.Text.Encoding]::UTF8)) {
            if ($existingLine -match '^\s*sdk\.dir\s*=') {
                if (-not $sdkPropertyWritten) {
                    $propertyLines.Add("sdk.dir=$sdkForProperties")
                    $sdkPropertyWritten = $true
                }
            }
            else {
                $propertyLines.Add($existingLine)
            }
        }
    }

    if (-not $sdkPropertyWritten) {
        $propertyLines.Add("sdk.dir=$sdkForProperties")
    }

    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($localProperties, (($propertyLines -join "`n") + "`n"), $utf8NoBom)

    $env:ANDROID_HOME = $sdkPath
    $env:ANDROID_SDK_ROOT = $sdkPath
    Write-Host "Android SDK: $sdkPath" -ForegroundColor DarkGray
    return $sdkPath
}

function Get-PhoneVRRepositoryRoot {
    return $script:PhoneVRRepoRoot
}
