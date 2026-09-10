#Requires -Version 5.1
<#
.SYNOPSIS
  YoYoPaste Windows uninstaller.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Continue"

$taskName = "YoYoPaste"
Write-Host "Stopping and removing scheduled task $taskName ..."
schtasks /end /tn $taskName 2>$null | Out-Null
schtasks /delete /tn $taskName /f 2>&1 | Out-Null
# Also clean up the old test rig task if still present
schtasks /end /tn YoYoPasteTest 2>$null | Out-Null
schtasks /delete /tn YoYoPasteTest /f 2>&1 | Out-Null

$destDir = "C:\ProgramData\YoYoPaste"
if (Test-Path $destDir) {
  Write-Host "Removing $destDir ..."
  Remove-Item -Recurse -Force $destDir
}

# Firewall rule is intentionally kept (YYP-062) — the installer needs it, and
# removing it would break a re-install on the same host.
# To remove it manually: Remove-NetFirewallRule -DisplayName "YoYoPaste peer 8383"

Write-Host "Uninstall complete. Firewall rule kept."

# Clean up stray per-user copies left by manual testing
$legacy = "C:\Users\lmin\yoyopasted.exe"
if (Test-Path $legacy) {
  Write-Host "Removing $legacy ..."
  Remove-Item -Force $legacy
}
$legacy2 = "$env:USERPROFILE\yoyopasted.exe"
if ((Test-Path $legacy2) -and ($legacy2 -ne $legacy)) {
  Write-Host "Removing $legacy2 ..."
  Remove-Item -Force $legacy2
}
