# Starts trackz at logon for the current user (HKCU Run key).
# Usage: powershell -ExecutionPolicy Bypass -File install-autostart.ps1 [-Exe C:\path\to\trackz.exe] [-Remove]
param(
    [string]$Exe = (Join-Path $env:LOCALAPPDATA "Programs\trackz\trackz.exe"),
    [switch]$Remove
)
$key = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
if ($Remove) {
    Remove-ItemProperty -Path $key -Name "Trackz" -ErrorAction SilentlyContinue
    Write-Host "Trackz autostart removed."
    exit 0
}
if (-not (Test-Path $Exe)) { Write-Error "Not found: $Exe"; exit 1 }
Set-ItemProperty -Path $key -Name "Trackz" -Value "`"$Exe`" run"
Write-Host "Trackz will start at logon: $Exe"
