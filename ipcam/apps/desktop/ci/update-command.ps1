# SPIKE-ONLY (owner's request of 2026-11-23): runs the shell's update command
# (capabilities/spike-update.json) as the shell plugin does, with the fixed
# arguments from the capability, against a published installer and a wrong
# SHA-256. The command must download it, refuse it ("SHA-256 mismatch"), delete
# it and start nothing. This checks the PowerShell script itself, which the
# unit tests (native-tauri.test.ts) only read.
#
#   ./apps/desktop/ci/update-command.ps1 [-Url <installer url>]
param(
  [string]$Url = 'https://github.com/AndreiBesliu/DevWorld/releases/download/spike-video-v0.1.0/ipcam-desktop-setup.exe'
)
$ErrorActionPreference = 'Stop'

$capability = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot '../src-tauri/capabilities/spike-update.json') | ConvertFrom-Json
$command = $capability.permissions[0].allow | Where-Object { $_.name -eq 'spike-update' }
if (-not $command) { throw 'the capability has no spike-update command' }
$fixed = @($command.args | Where-Object { $_ -is [string] })
$validators = @($command.args | Where-Object { $_ -isnot [string] } | ForEach-Object { $_.validator })
$wrong = '0' * 64
if ($Url -notmatch "^$($validators[0])$") { throw "the capability refuses $Url" }
if ($wrong -notmatch "^$($validators[1])$") { throw 'the capability refuses the SHA-256' }

$file = Join-Path $env:TEMP 'ipcam-update/ipcam-desktop-setup.exe'
if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
$before = @(Get-Process -Name 'ipcam-desktop-setup' -ErrorAction SilentlyContinue).Count

$out = & $command.cmd @fixed $Url $wrong 2>&1 | Out-String
$code = $LASTEXITCODE
Write-Host "exit code $code"
Write-Host $out
if ($code -eq 0) { throw 'the update command accepted a wrong SHA-256' }
if ($out -notmatch 'SHA-256 mismatch') { throw 'the update command failed, but not on the SHA-256: see its output above' }
if (Test-Path -LiteralPath $file) { throw "the refused download is still there: $file" }
if (@(Get-Process -Name 'ipcam-desktop-setup' -ErrorAction SilentlyContinue).Count -ne $before) { throw 'an installer started' }
Write-Host 'the update command downloads the installer, checks its SHA-256 and refuses a wrong one'
