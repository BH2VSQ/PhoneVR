[CmdletBinding()]
param(
    [switch]$Remove,
    [string]$ProgramPath = ""
)

$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this script from an elevated PowerShell window (Run as administrator).'
}

$rules = @(
    @{ Name = 'PhoneVR USB Tether - Discovery UDP 48100'; Protocol = 'UDP'; Port = '48100' },
    @{ Name = 'PhoneVR USB Tether - Pose UDP 48102'; Protocol = 'UDP'; Port = '48102' },
    @{ Name = 'PhoneVR USB Tether - Control TCP 48101'; Protocol = 'TCP'; Port = '48101' }
)

if ($Remove) {
    foreach ($rule in $rules) {
        Get-NetFirewallRule -DisplayName $rule.Name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    }
    Write-Host 'PhoneVR USB tethering firewall rules removed.' -ForegroundColor Green
    exit 0
}

if (-not $ProgramPath) {
    $candidate = Join-Path (Split-Path $PSScriptRoot -Parent) 'dist\windows\PhoneVRHost.exe'
    if (Test-Path $candidate) { $ProgramPath = $candidate }
}
if ($ProgramPath -and -not (Test-Path $ProgramPath)) {
    throw "PhoneVR Host executable was not found: $ProgramPath"
}

foreach ($rule in $rules) {
    Get-NetFirewallRule -DisplayName $rule.Name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    $arguments = @{
        DisplayName = $rule.Name
        Direction = 'Inbound'
        Action = 'Allow'
        Protocol = $rule.Protocol
        LocalPort = $rule.Port
        Profile = 'Any'
        RemoteAddress = 'LocalSubnet'
        Description = 'Allows PhoneVR discovery, 3DoF pose and control over a directly tethered USB/RNDIS network.'
    }
    if ($ProgramPath) { $arguments.Program = (Resolve-Path $ProgramPath).Path }
    New-NetFirewallRule @arguments | Out-Null
}
Write-Host 'PhoneVR USB tethering firewall rules installed for LocalSubnet on all Windows network profiles.' -ForegroundColor Green
