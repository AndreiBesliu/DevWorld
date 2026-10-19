# WIN-2 smoke test of the spike shell (plan 2026-10-18-phase0b-video-spike,
# Tasks 11 and 11b), run by ipcam-windows.yml on windows-latest. CI-only: it
# stops unless GITHUB_ACTIONS is true, because it opens a DevTools port by a
# machine policy (below), which must never stay on a PC. It starts spikehub with
# 4 simulated cameras, then starts the app from a new process several times:
# each start runs scenario S1 by autorun (--autorun S1 --hub
# http://127.0.0.1:7080, R5), and the hub must store its S1 result with 20
# samples within 5 min. Every start must pass: the script goes on after a start
# that fails, to count how many do, and fails if any did. Nothing is retried.
#
# A start passes when, after its launch, the hub stores an S1 result that is
# not interrupted, comes from platform windows-tauri, has 20 first-frame
# samples and names the CPU (the model comes from the shell's spike_metrics
# command), and when that command, asked through WebView2's DevTools port,
# counts at least one WebView2 process.
#
# The DevTools port is opened by the machine policy AdditionalBrowserArguments
# for the app's executable, which the script writes. The runner's processes are
# elevated, and WebView2 Runtime 150 and later ignores the variable
# WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS and per-user policies in an elevated
# process (WebView2Feedback issue 5645). The script creates the policy's keys
# only where they are missing, so an existing key keeps its values, and removes
# its own value when it ends, also after an error.
#
# On failure it prints the cause as check annotations, which can be read where
# the job log cannot.
#
#   pwsh apps/desktop/ci/windows-smoke.ps1 -App <exe> -Hub <spikehub> -Media <folder> -Work <folder> [-Starts 3] [-RunTimeoutS 300]
param(
  [Parameter(Mandatory)] [string] $App,
  [Parameter(Mandatory)] [string] $Hub,
  [Parameter(Mandatory)] [string] $Media,
  # Takes the hub's results folder and the logs.
  [Parameter(Mandatory)] [string] $Work,
  [int] $Starts = 3,
  # How long one start may take; S1 takes about a minute.
  [int] $RunTimeoutS = 300
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true') {
  throw 'windows-smoke.ps1 runs in GitHub Actions only: it opens a DevTools port for the app by a machine policy.'
}

# 127.0.0.1, not localhost: spikehub listens on IPv4 only (R38), and Windows
# tries ::1 first for localhost, gets refused and retries before it falls back,
# which made the 30 s readiness probe fail.
$hubUrl = 'http://127.0.0.1:7080'
$debugPort = 9222
$argumentsPolicy = 'HKLM:\SOFTWARE\Policies\Microsoft\Edge\WebView2\AdditionalBrowserArguments'
$pageState = Join-Path $PSScriptRoot 'page-state.ts'
# Start-Process takes the programs and the hub its folders by full path.
$App = (Resolve-Path -LiteralPath $App).Path
$Hub = (Resolve-Path -LiteralPath $Hub).Path
$Media = (Resolve-Path -LiteralPath $Media).Path
$Work = (New-Item -ItemType Directory -Force -Path $Work).FullName
$results = (New-Item -ItemType Directory -Force -Path (Join-Path $Work 'results')).FullName
$hubOut = Join-Path $Work 'spikehub.out.log'
$hubLog = Join-Path $Work 'spikehub.log'

# Prints $Text as one annotation of level $Level (error, notice) titled $Title.
# GitHub shows about 4000 characters of an annotation: keep the first 3300.
function Write-Annotation([string] $Level, [string] $Title, [string] $Text) {
  if (-not $Text) { $Text = '(nothing)' }
  if ($Text.Length -gt 3300) { $Text = $Text.Substring(0, 3300) }
  Write-Host "::${Level} title=${Title}::$($Text.Replace('%', '%25').Replace("`r", '%0D').Replace("`n", '%0A'))"
}

# The last $Lines lines of a log, each cut to 200 characters.
function Get-Tail([string] $Path, [int] $Lines) {
  if (-not (Test-Path -LiteralPath $Path)) { return '(no log)' }
  $tail = Get-Content -LiteralPath $Path -Tail $Lines | ForEach-Object { if ($_.Length -gt 200) { $_.Substring(0, 200) } else { $_ } }
  if ($tail) { $tail -join "`n" } else { '(empty)' }
}

# A property of a parsed JSON object, or $null when it has none.
function Get-Field($Object, [string] $Name) {
  if ($null -eq $Object) { return $null }
  $property = $Object.PSObject.Properties[$Name]
  if ($property) { $property.Value } else { $null }
}

# The page's state and the raw spike_metrics answer, as JSON text, or why they cannot be read.
function Read-Page {
  $ErrorActionPreference = 'Continue'
  $text = (& node $pageState $debugPort 2>&1 | Out-String).Trim()
  if ($LASTEXITCODE -ne 0) { return "the page cannot be read through the DevTools port: $text" }
  $text
}

# The WebView2 browser process's arguments and the TCP ports that WebView2
# listens on: whether the DevTools port was asked for and opened.
function Get-WebView2State {
  $ErrorActionPreference = 'Continue'
  $procs = @(Get-CimInstance Win32_Process -Filter "Name = 'msedgewebview2.exe'" -ErrorAction SilentlyContinue)
  $browser = @($procs | Where-Object { $_.CommandLine -notmatch '--type=' } | ForEach-Object {
      $line = "$($_.ProcessId) (parent $($_.ParentProcessId)): $($_.CommandLine)"
      if ($line.Length -gt 700) { $line.Substring(0, 700) } else { $line }
    })
  $ids = @($procs | ForEach-Object ProcessId)
  $ports = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
      Where-Object { $ids -contains $_.OwningProcess } | ForEach-Object { "$($_.LocalAddress):$($_.LocalPort)" })
  $values = Get-ItemProperty -LiteralPath $argumentsPolicy -ErrorAction SilentlyContinue
  $policy = if ($values) { @($values.PSObject.Properties | Where-Object Name -notlike 'PS*' | ForEach-Object { "$($_.Name)=$($_.Value)" }) -join ', ' }
  @(
    "policy AdditionalBrowserArguments: $(if ($policy) { $policy } else { '(none)' })",
    "WebView2 browser processes: $(if ($browser) { $browser -join ' | ' } else { '(none)' })",
    "WebView2 listens on: $(if ($ports) { $ports -join ', ' } else { '(nothing)' })"
  ) -join "`n"
}

# What is wrong with an S1 result, or $null.
function Get-ResultProblem($Result) {
  $samples = @(Get-Field (Get-Field $Result 'samples') 'firstFrameMs').Where({ $null -ne $_ })
  $device = Get-Field $Result 'device'
  $notes = @(Get-Field $Result 'notes') -join '; '
  if (Get-Field $Result 'interrupted') { return "S1 was interrupted (the page was hidden); notes: $notes" }
  if ((Get-Field $device 'platform') -ne 'windows-tauri') { return "S1 came from platform $(Get-Field $device 'platform'), not windows-tauri" }
  if ($samples.Count -ne 20) { return "S1 stored with $($samples.Count) of 20 samples; notes: $notes" }
  if (-not (Get-Field $device 'model')) { return "S1 names no CPU model: the shell's metrics were not read; notes: $notes" }
  $null
}

# What is wrong with the shell's metrics in the page state, or $null.
function Get-MetricsProblem([string] $Page) {
  try { $metrics = Get-Field ($Page | ConvertFrom-Json) 'metrics' } catch { return "no page state: $Page" }
  if (Get-Field $metrics 'error') { return "spike_metrics failed: $(Get-Field $metrics 'error')" }
  if ((Get-Field $metrics 'webview2_procs') -lt 1) { return "spike_metrics counts no WebView2 process: $($metrics | ConvertTo-Json -Compress)" }
  if ((Get-Field $metrics 'rss_mb') -le 0 -or (Get-Field $metrics 'private_mb') -le 0) {
    return "spike_metrics reports no memory: $($metrics | ConvertTo-Json -Compress)"
  }
  $null
}

# Closes the app's window, so that WebView2 writes its profile (the results
# queue in localStorage) to disk, and kills the process tree if it does not
# end. Then waits for the WebView2 processes, which end with their app.
function Stop-App($Process) {
  if (-not $Process.HasExited) {
    $closing = $Process.CloseMainWindow()
    if (-not ($closing -and $Process.WaitForExit(15000))) {
      $Process.Kill($true)
      $null = $Process.WaitForExit(10000)
    }
  }
  Get-Process -Name msedgewebview2 -ErrorAction SilentlyContinue | Wait-Process -Timeout 20 -ErrorAction SilentlyContinue
}

# Starts the app with autorun, waits for its S1 result and checks it and the
# shell's metrics, then closes the app. Returns $true with a notice when the
# start passes, else $false with the cause and the diagnosis as errors.
function Test-Start([string] $Label, [int] $Index) {
  $appOut = Join-Path $Work "app-$Index.out.log"
  $appErr = Join-Path $Work "app-$Index.err.log"
  # Only a result stored from now on can belong to this start.
  $known = @(Get-ChildItem -LiteralPath $results -Filter *.json | ForEach-Object FullName)
  $launched = [DateTimeOffset]::UtcNow
  $process = Start-Process -FilePath $App -ArgumentList '--autorun', 'S1', '--hub', $hubUrl `
    -RedirectStandardOutput $appOut -RedirectStandardError $appErr -PassThru
  $null = $process.Handle # keeps the exit code readable after the process ends
  $deadline = $launched.AddSeconds($RunTimeoutS)
  $stored = $null
  $problem = $null
  while (-not ($stored -or $problem)) {
    foreach ($file in Get-ChildItem -LiteralPath $results -Filter *.json) {
      if ($known -contains $file.FullName) { continue }
      try { $result = Get-Content -Raw -LiteralPath $file.FullName | ConvertFrom-Json } catch { continue } # still being written
      $known += $file.FullName
      # A result that the app kept from an earlier start and uploads now (Review Focus 5) is not this start's.
      if ((Get-Field $result 'scenario') -ne 'S1' -or [DateTimeOffset] (Get-Field $result 'startedAt') -lt $launched) { continue }
      $problem = Get-ResultProblem $result
      if (-not $problem) { $stored = $result }
    }
    if ($stored -or $problem) { break }
    if ($process.HasExited) { $problem = "the app ended with exit code $($process.ExitCode) before S1 stored a result" }
    elseif ([DateTimeOffset]::UtcNow -ge $deadline) { $problem = "no S1 result within $RunTimeoutS s" }
    else { Start-Sleep -Seconds 3 }
  }
  $page = if ($process.HasExited) { 'the app is not running' } else { Read-Page }
  if (-not $problem) { $problem = Get-MetricsProblem $page }

  if ($problem) {
    Write-Annotation error "windows S1 $Label" $problem
    Write-Annotation error "windows S1 $Label diagnosis" (@(
        "page: $page",
        "app running: $(-not $process.HasExited) (pid $($process.Id)); WebView2 processes: $(@(Get-Process -Name msedgewebview2 -ErrorAction SilentlyContinue).Count)",
        (Get-WebView2State),
        'app stderr:', (Get-Tail $appErr 12),
        'app stdout:', (Get-Tail $appOut 4),
        'spikehub log:', (Get-Tail $hubLog 8)
      ) -join "`n")
    Stop-App $process
    return $false
  }
  $metrics = Get-Field ($page | ConvertFrom-Json) 'metrics'
  # Nearest rank: p50 is the 10th and p90 the 18th of the 20 sorted samples.
  $ms = @($stored.samples.firstFrameMs | Sort-Object | ForEach-Object { [math]::Round($_) })
  $device = $stored.device
  Write-Annotation notice "windows S1 $Label" (@(
      "S1 stored with 20 samples: first frame p50 $($ms[9]) ms, p90 $($ms[17]) ms, max $($ms[19]) ms;",
      "$(Get-Field $device 'model') $(Get-Field $device 'os') $(Get-Field $device 'webview'); notes: $(@($stored.notes) -join '; ')",
      "spike_metrics: $($metrics | ConvertTo-Json -Compress)"
    ) -join "`n")
  Stop-App $process
  $true
}

$hubProcess = Start-Process -FilePath $Hub -ArgumentList '-sims', '4', '-media', $Media, '-results', $results `
  -RedirectStandardOutput $hubOut -RedirectStandardError $hubLog -PassThru
$hubDeadline = (Get-Date).AddSeconds(30)
while ($true) {
  try {
    $null = Invoke-WebRequest -Uri "$hubUrl/spike/v1/cameras" -TimeoutSec 2
    break
  } catch {
    if ($hubProcess.HasExited -or (Get-Date) -ge $hubDeadline) {
      Write-Annotation error 'spikehub' "spikehub did not answer on port 7080 within 30 s`n$(Get-Tail $hubOut 10)`n$(Get-Tail $hubLog 20)"
      exit 1
    }
    Start-Sleep -Seconds 1
  }
}

# Every WebView2 the app creates opens its DevTools port, which Read-Page asks.
# The policy's value name is the executable's file name. Each missing key of
# the policy's path is created on its own, and an existing key is left as it is
# (New-Item -Force would replace it and drop its values).
$policyValue = Split-Path -Leaf $App
$key = 'HKLM:'
foreach ($name in $argumentsPolicy.Substring($key.Length + 1).Split('\')) {
  $key = "$key\$name"
  if (-not (Test-Path -LiteralPath $key)) { $null = New-Item -Path $key }
}
$failures = 0
try {
  $null = New-ItemProperty -LiteralPath $argumentsPolicy -Name $policyValue -Value "--remote-debugging-port=$debugPort" -PropertyType String -Force
  for ($start = 1; $start -le $Starts; $start++) {
    if (-not (Test-Start "start $start of $Starts" $start)) { $failures++ }
  }
} finally {
  Remove-ItemProperty -LiteralPath $argumentsPolicy -Name $policyValue -ErrorAction SilentlyContinue
  Stop-Process -Id $hubProcess.Id -ErrorAction SilentlyContinue
}

$summary = if ($failures) { "$failures of $Starts starts failed" } else { "all $Starts starts stored S1 with 20 samples" }
if ($env:GITHUB_STEP_SUMMARY) { Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY -Value "Windows smoke test (WIN-2): $summary" }
if ($failures) {
  Write-Annotation error 'windows S1' $summary
  exit 1
}
Write-Annotation notice 'windows S1' $summary
