#Requires -Version 5.1
<#
.SYNOPSIS
  YoYoPaste Windows installer — one command, no questions.

.DESCRIPTION
  Implements the proven-working sequence from YYP-061, verified on vista:

  1. Detects the user who owns the Tailscale GUI (tailscale-ipn process) — D14:
     the daemon must run as that user, otherwise LocalAPI returns
     "Tailscale already in use" and SelfIP never binds. A SYSTEM service cannot work.
  2. Copies yoyopasted.exe to C:\ProgramData\YoYoPaste\ (readable by that user).
  3. Unblock-File (Mark-of-the-Web).
  4. Creates inbound firewall rule for TCP 8383 (blocked by default; over DERP indistinguishable from "not running").
  5. Registers a scheduled task as that user with interactive token (/ru "<user>" /it, no stored password) and /logon trigger, then starts it.

  Fails loudly if Tailscale is not running.

  Usage: powershell -ExecutionPolicy Bypass -File scripts/install-windows.ps1 [-BinaryPath .\yoyopasted.exe]
#>
param(
  [string]$BinaryPath = ".\yoyopasted.exe"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Fail($msg) { Write-Error $msg; exit 1 }

# 1. Tailscale must be running — otherwise SelfIP will never work
try { $null = Get-Process -Name "tailscale-ipn" -ErrorAction Stop } catch { Fail "Tailscale is not running (tailscale-ipn process not found). Start Tailscale and log in, then re-run." }
try { $null = Get-Process -Name "tailscaled" -ErrorAction SilentlyContinue } catch {}

# Detect owner of tailscale-ipn
$owner = $null
try {
  $proc = Get-CimInstance Win32_Process -Filter "Name='tailscale-ipn.exe'" | Select-Object -First 1
  if ($proc) {
    $ownerInfo = Invoke-CimMethod -InputObject $proc -MethodName GetOwner
    if ($ownerInfo.Domain -and $ownerInfo.User) { $owner = "$($ownerInfo.Domain)\$($ownerInfo.User)" }
    elseif ($ownerInfo.User) { $owner = $ownerInfo.User }
  }
} catch {}
if (-not $owner) {
  # Fallback: whoami for the interactive user if GetOwner failed
  try { $owner = (whoami) } catch {}
}
if (-not $owner) { Fail "Could not determine Tailscale GUI user (owner of tailscale-ipn). Is Tailscale running as a GUI app?" }
Write-Host "Tailscale GUI user: $owner"

if (-not (Test-Path $BinaryPath)) { Fail "Binary not found at $BinaryPath. Pass -BinaryPath <path>." }

$destDir = "C:\ProgramData\YoYoPaste"
$destBin = Join-Path $destDir "yoyopasted.exe"
Write-Host "Installing to $destBin ..."
New-Item -ItemType Directory -Force -Path $destDir | Out-Null
Copy-Item -Force $BinaryPath $destBin
Unblock-File -Path $destBin
Write-Host "Unblocked $destBin"

# 3. Firewall — keep existing rule if present
$ruleName = "YoYoPaste peer 8383"
if (-not (Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8383 | Out-Null
  Write-Host "Firewall rule '$ruleName' created."
} else {
  Write-Host "Firewall rule '$ruleName' already exists."
}

# 4. Scheduled task with interactive token, logon trigger
$taskName = "YoYoPaste"
# Remove old test task if present
try { schtasks /delete /tn YoYoPasteTest /f 2>$null | Out-Null } catch {}
try { schtasks /delete /tn $taskName /f 2>$null | Out-Null } catch {}

$cmd = "schtasks /create /tn $taskName /tr `"'$destBin' -v`" /sc onlogon /ru `"$owner`" /it /f"
Write-Host "Creating scheduled task: $cmd"
$out = cmd /c $cmd 2>&1
if ($LASTEXITCODE -ne 0) { Fail "schtasks create failed: $out" }
Write-Host $out

# Start now (also runs on next logon)
$out = schtasks /run /tn $taskName 2>&1
Write-Host $out
Start-Sleep -Seconds 2
$state = (schtasks /query /tn $taskName /v /fo LIST 2>&1 | Select-String "Status")
Write-Host "Task $taskName $state"
Write-Host "Done. yoyopasted should be listening on 100.x:8383 (tailscale ip) and http://127.0.0.1:8384"
Write-Host "Check: curl http://127.0.0.1:8384/api/state  and  curl http://(tailscale ip):8383/v0/hello"
