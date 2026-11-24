[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$SysVadRoot
)
$ErrorActionPreference = "Stop"
$python = Get-Command python.exe -ErrorAction SilentlyContinue
if (-not $python) { $python = Get-Command python -ErrorAction SilentlyContinue }
if (-not $python) { throw "Python 3 was not found. Install Python 3 and add it to PATH." }
$patcher = Join-Path $PSScriptRoot "patch_sysvad.py"
& $python.Source $patcher $SysVadRoot
if ($LASTEXITCODE -ne 0) { throw "PhoneVR SysVAD patch failed with exit code $LASTEXITCODE" }
