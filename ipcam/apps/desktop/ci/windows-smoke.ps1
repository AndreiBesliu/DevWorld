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
# counts at least one WebView2 process, reports the app's memory and the PC's,
# and names the OS and the WebView2 Runtime.
#
# Then it checks that the app is single-instance (R52): it starts the app,
# minimizes its window and starts it again. After 5 s the second start must
# have ended with exit code 0, the first must be the only app process, and its
# window must no longer be minimized (the first instance restored it).
#
# Last, the hub panel (SPIKE-ONLY, plan 2026-11-24-spike-hub-panel): in a
# spikehub folder like the owner's (spikehub.exe, media, results), the app,
# started without arguments, must start spikehub when the page's «Pornește
# hub-ul» is pressed: the hub must answer on port 7080 within 30 s, run with no
# window, and the panel must show its line «spikehub listens». «Oprește hub-ul»
# must end it; started again, closing the app must end it too. It runs last
# because the page then remembers the folder, and starts a hub at each launch.
#
# The DevTools port is opened by the machine policy AdditionalBrowserArguments
# for the app's executable, which the script writes. The runner's processes are
# elevated, and WebView2 Runtime 150 and later ignores the variable
# WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS and per-user policies in an elevated
# process (WebView2Feedback issue 5645). The script creates the policy's keys
# only where they are missing, so an existing key keeps its values, and removes
# its own value when it ends, also after an error, and warns when the value is
# still there afterwards.
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
$pageHub = Join-Path $PSScriptRoot 'page-hub.ts'
# Start-Process takes the programs and the hub its folders by full path.
$App = (Resolve-Path -LiteralPath $App).Path
$Hub = (Resolve-Path -LiteralPath $Hub).Path
$Media = (Resolve-Path -LiteralPath $Media).Path
$Work = (New-Item -ItemType Directory -Force -Path $Work).FullName
$results = (New-Item -ItemType Directory -Force -Path (Join-Path $Work 'results')).FullName
$hubOut = Join-Path $Work 'spikehub.out.log'
$hubLog = Join-Path $Work 'spikehub.log'
# The app's process name and the title of its window (main.rs).
$appName = [IO.Path]::GetFileNameWithoutExtension($App)
$windowTitle = 'Proba ipcam'

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

# What is wrong with the shell's metrics in the page state, or $null. A field
# that is missing or null (n/a) fails like a 0 or an empty name.
function Get-MetricsProblem([string] $Page) {
  try { $metrics = Get-Field ($Page | ConvertFrom-Json) 'metrics' } catch { return "no page state: $Page" }
  if (Get-Field $metrics 'error') { return "spike_metrics failed: $(Get-Field $metrics 'error')" }
  $json = $metrics | ConvertTo-Json -Compress
  if ((Get-Field $metrics 'webview2_procs') -lt 1) { return "spike_metrics counts no WebView2 process: $json" }
  if ((Get-Field $metrics 'rss_mb') -le 0 -or (Get-Field $metrics 'private_mb') -le 0) { return "spike_metrics reports no memory: $json" }
  if ((Get-Field $metrics 'ram_mb') -le 0) { return "spike_metrics reports no physical memory of the PC: $json" }
  if (-not (Get-Field $metrics 'os')) { return "spike_metrics names no OS: $json" }
  if (-not (Get-Field $metrics 'webview')) { return "spike_metrics names no WebView2 Runtime: $json" }
  $null
}

# Closes the app's window, so that WebView2 writes its profile (the results
# queue in localStorage) to disk, and kills the process tree if it does not
# end. Then waits for the WebView2 processes, which end with their app.
function Stop-App($Process) {
  if (-not $Process.HasExited) {
    $closing = $Process.CloseMainWindow()
    if (-not ($closing -and $Process.WaitForExit(15000))) {
      # The single-instance plugin keeps a hidden window of its own: name the window that was closed.
      $why = if (-not $closing) { 'has no main window to close' } else {
        $title = try { $Process.MainWindowTitle } catch { '?' }
        "did not end within 15 s of closing its main window «$title»"
      }
      Write-Annotation warning 'windows app close' "the app (pid $($Process.Id)) $why, so it was killed"
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

# R52: starts the app, minimizes its window, starts the app again and checks,
# 5 s later, that the second start ended with exit code 0, that the first is
# the only app process left, and that its window is no longer minimized.
# Returns $null, or what is wrong and a diagnosis.
function Test-SingleInstance {
  Add-Type -Namespace IpcamSmoke -Name User32 -MemberDefinition @'
[DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr FindWindow(string className, string windowName);
[DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
[DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr window, int command);
[DllImport("user32.dll")] public static extern bool IsIconic(IntPtr window);
'@
  $logs = @('first', 'second' | ForEach-Object { Join-Path $Work "app-single-$_.err.log" })
  $first = Start-Process -FilePath $App -RedirectStandardOutput (Join-Path $Work 'app-single-first.out.log') `
    -RedirectStandardError $logs[0] -PassThru
  $null = $first.Handle
  $second = $null
  try {
    # The first instance's window, found by its title and owned by its process.
    $deadline = (Get-Date).AddSeconds(60)
    $window = [IntPtr]::Zero
    while ($window -eq [IntPtr]::Zero) {
      if ($first.HasExited) { return "the first start ended with exit code $($first.ExitCode) before its window opened" }
      if ((Get-Date) -ge $deadline) { return "the first start opened no window «$windowTitle» within 60 s" }
      Start-Sleep -Seconds 1
      # [NullString]: PowerShell would pass $null to a string parameter as "".
      $found = [IpcamSmoke.User32]::FindWindow([NullString]::Value, $windowTitle)
      $owner = [uint32] 0
      $null = [IpcamSmoke.User32]::GetWindowThreadProcessId($found, [ref] $owner)
      if ($found -ne [IntPtr]::Zero -and $owner -eq $first.Id) { $window = $found }
    }
    $null = [IpcamSmoke.User32]::ShowWindow($window, 6) # SW_MINIMIZE
    if (-not [IpcamSmoke.User32]::IsIconic($window)) { return 'the window of the first start could not be minimized' }
    $second = Start-Process -FilePath $App -RedirectStandardOutput (Join-Path $Work 'app-single-second.out.log') `
      -RedirectStandardError $logs[1] -PassThru
    $null = $second.Handle
    Start-Sleep -Seconds 5
    $running = @(Get-Process -Name $appName -ErrorAction SilentlyContinue | ForEach-Object Id)
    $state = "app processes after 5 s: $(if ($running) { $running -join ', ' } else { 'none' }) (first $($first.Id), second $($second.Id))"
    $problem = if (-not $second.HasExited) { "the second start still runs; $state" }
    elseif ($second.ExitCode -ne 0) { "the second start ended with exit code $($second.ExitCode); $state" }
    elseif ($running.Count -ne 1 -or $running[0] -ne $first.Id) { "the first start is not the only app process; $state" }
    elseif ([IpcamSmoke.User32]::IsIconic($window)) { "the first start's window is still minimized: the second start did not restore it; $state" }
    if ($problem) { return "$problem`napp stderr, first start:`n$(Get-Tail $logs[0] 8)`napp stderr, second start:`n$(Get-Tail $logs[1] 8)" }
    Write-Annotation notice 'windows single instance' "the second start ended with exit code 0 and restored the first start's window; $state"
    $null
  } finally {
    # Stop-App also waits for the WebView2 processes, which a second start that ended does not have.
    if ($second -and -not $second.HasExited) { Stop-App $second }
    Stop-App $first
  }
}

# Opens the app's DevTools port by the machine policy (see the top). Each
# missing key of the policy's path is created on its own, and an existing key
# is left as it is (New-Item -Force would replace it and drop its values). The
# policy's value name is the executable's file name.
function Set-DevToolsPolicy {
  $key = 'HKLM:'
  foreach ($name in $argumentsPolicy.Substring($key.Length + 1).Split('\')) {
    $key = "$key\$name"
    if (-not (Test-Path -LiteralPath $key)) { $null = New-Item -Path $key }
  }
  $null = New-ItemProperty -LiteralPath $argumentsPolicy -Name $policyValue -Value "--remote-debugging-port=$debugPort" -PropertyType String -Force
}

# Removes the policy's value, and warns when it is still there.
function Remove-DevToolsPolicy {
  Remove-ItemProperty -LiteralPath $argumentsPolicy -Name $policyValue -ErrorAction SilentlyContinue
  # The value opens a DevTools port for the app: say so if it is still there.
  if ($null -ne (Get-ItemProperty -LiteralPath $argumentsPolicy -Name $policyValue -ErrorAction SilentlyContinue)) {
    Write-Annotation warning 'windows policy' "the policy value $policyValue is still set under $argumentsPolicy after Remove-ItemProperty"
  }
}

# The hub panel's state after an action of page-hub.ts (start <folder>, stop,
# read), parsed, or a string that says why it cannot be read.
function Invoke-HubPanel([string[]] $Action) {
  $ErrorActionPreference = 'Continue'
  $text = (& node $pageHub $debugPort @Action 2>&1 | Out-String).Trim()
  if ($LASTEXITCODE -ne 0) { return "the page cannot be read through the DevTools port: $text" }
  try { $state = $text | ConvertFrom-Json } catch { return "the page answered no JSON: $text" }
  # DevTools' own answer, when the page's expression returned nothing.
  if ($null -eq (Get-Field $state 'panel')) { return "the page answered no panel state: $text" }
  $state
}

# The spikehub processes started from $Exe.
function Get-HubProcesses([string] $Exe) {
  @(Get-Process -Name spikehub -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $Exe })
}

# Waits up to $Seconds for no spikehub of $Exe; returns whether none is left.
function Wait-HubGone([string] $Exe, [int] $Seconds) {
  $deadline = (Get-Date).AddSeconds($Seconds)
  while ((Get-HubProcesses $Exe).Count -gt 0) {
    if ((Get-Date) -ge $deadline) { return $false }
    Start-Sleep -Milliseconds 500
  }
  $true
}

# Waits up to $Seconds for the hub to answer on port 7080; returns whether it did.
function Wait-HubAnswers([int] $Seconds) {
  $deadline = (Get-Date).AddSeconds($Seconds)
  while ($true) {
    try {
      $null = Invoke-WebRequest -Uri "$hubUrl/spike/v1/cameras" -TimeoutSec 2
      return $true
    } catch {
      if ((Get-Date) -ge $deadline) { return $false }
      Start-Sleep -Seconds 1
    }
  }
}

# The hub panel (see the top). Returns $null, or what is wrong and a diagnosis.
function Test-HubPanel {
  # A spikehub folder as the owner has one after extracting spikehub-windows.zip.
  $folder = (New-Item -ItemType Directory -Force -Path (Join-Path $Work 'spikehub-windows')).FullName
  $exe = Join-Path $folder 'spikehub.exe'
  Copy-Item -LiteralPath $Hub -Destination $exe
  $null = New-Item -ItemType Junction -Path (Join-Path $folder 'media') -Target $Media
  $null = New-Item -ItemType Directory -Force -Path (Join-Path $folder 'results')
  $appErr = Join-Path $Work 'app-hub.err.log'
  $process = Start-Process -FilePath $App -RedirectStandardOutput (Join-Path $Work 'app-hub.out.log') `
    -RedirectStandardError $appErr -PassThru
  $null = $process.Handle
  $panel = $null
  $diagnosis = { "panel: $($panel | ConvertTo-Json -Compress)`napp stderr:`n$(Get-Tail $appErr 8)" }
  try {
    # The page's panel, once WebView2 has loaded it.
    $deadline = (Get-Date).AddSeconds(60)
    while ($true) {
      $panel = Invoke-HubPanel @('read')
      if ($panel -isnot [string] -and $panel.panel) { break }
      if ($process.HasExited) { return "the app ended with exit code $($process.ExitCode) before its page showed the hub panel" }
      if ((Get-Date) -ge $deadline) { return "the page showed no hub panel within 60 s`n$(& $diagnosis)" }
      Start-Sleep -Seconds 1
    }
    if ($panel.status -ne 'Hub-ul e oprit.') { return "the hub panel did not start as stopped`n$(& $diagnosis)" }

    $panel = Invoke-HubPanel @('start', $folder)
    if (-not (Wait-HubAnswers 30)) { return "the hub started from the panel did not answer on port 7080 within 30 s`n$(& $diagnosis)" }
    $hubs = Get-HubProcesses $exe
    if ($hubs.Count -ne 1) { return "$($hubs.Count) spikehub processes run from the folder, not 1`n$(& $diagnosis)" }
    # The app started it through cmd: spikehub's parent is cmd, whose parent is the app. A console
    # window, had one opened, would count as cmd's window: neither may have one.
    $cmd = Get-CimInstance Win32_Process -Filter "ProcessId = $((Get-CimInstance Win32_Process -Filter "ProcessId = $($hubs[0].Id)").ParentProcessId)"
    if (-not $cmd -or $cmd.Name -ne 'cmd.exe' -or $cmd.ParentProcessId -ne $process.Id) {
      return "spikehub's parent is $(if ($cmd) { "$($cmd.Name) (pid $($cmd.ProcessId), parent $($cmd.ParentProcessId))" } else { 'gone' }), not the app's cmd (app pid $($process.Id))"
    }
    $windowed = @(@($hubs[0]) + @(Get-Process -Id $cmd.ProcessId -ErrorAction SilentlyContinue) | Where-Object { $_.MainWindowHandle -ne [IntPtr]::Zero })
    if ($windowed) { return "the hub started from the panel has a window «$($windowed[0].MainWindowTitle)» ($($windowed[0].ProcessName))" }
    $deadline = (Get-Date).AddSeconds(10)
    while ($true) {
      $panel = Invoke-HubPanel @('read')
      if ($panel -isnot [string] -and "$($panel.log)".Contains('spikehub listens') -and $panel.status -eq 'Hub-ul rulează. Ce scrie el apare mai jos.') { break }
      if ((Get-Date) -ge $deadline) { return "the panel did not show the hub running with its line «spikehub listens» within 10 s`n$(& $diagnosis)" }
      Start-Sleep -Seconds 1
    }

    $panel = Invoke-HubPanel @('stop')
    if (-not (Wait-HubGone $exe 10)) { return "«Oprește hub-ul» left spikehub running after 10 s`n$(& $diagnosis)" }
    Start-Sleep -Seconds 1
    $panel = Invoke-HubPanel @('read')
    if ($panel -is [string] -or $panel.status -ne 'Hub-ul e oprit.') { return "after «Oprește hub-ul» the panel does not say the hub is stopped`n$(& $diagnosis)" }

    $panel = Invoke-HubPanel @('start', $folder)
    if (-not (Wait-HubAnswers 30)) { return "the hub started again from the panel did not answer within 30 s`n$(& $diagnosis)" }
    Stop-App $process
    if (-not (Wait-HubGone $exe 10)) { return 'closing the app left its spikehub running after 10 s' }
    Write-Annotation notice 'windows hub panel' 'the panel started spikehub without a window, showed «spikehub listens», stopped it, and closing the app ended it'
    $null
  } finally {
    Stop-App $process
    Get-HubProcesses $exe | Stop-Process -Force -ErrorAction SilentlyContinue
  }
}

# -browser=false: spikehub opens its page in the default browser on Windows, which a runner must not do.
$hubProcess = Start-Process -FilePath $Hub -ArgumentList '-sims', '4', '-media', $Media, '-results', $results, '-browser=false' `
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

# Every WebView2 the app creates opens its DevTools port, which Read-Page and
# Invoke-HubPanel ask.
$policyValue = Split-Path -Leaf $App
$failures = 0
try {
  Set-DevToolsPolicy
  for ($start = 1; $start -le $Starts; $start++) {
    if (-not (Test-Start "start $start of $Starts" $start)) { $failures++ }
  }
} finally {
  Remove-DevToolsPolicy
  Stop-Process -Id $hubProcess.Id -ErrorAction SilentlyContinue
}
try { $singleProblem = Test-SingleInstance } catch { $singleProblem = "the check could not run: $_" }
# The panel's hub needs port 7080, which the hub above held.
$null = $hubProcess.WaitForExit(10000)
try {
  Set-DevToolsPolicy
  try { $panelProblem = Test-HubPanel } catch { $panelProblem = "the check could not run: $_" }
} finally {
  Remove-DevToolsPolicy
}

$summary = if ($failures) { "$failures of $Starts starts failed" } else { "all $Starts starts stored S1 with 20 samples" }
$single = if ($singleProblem) { 'failed' } else { 'a second start restored the first window and ended' }
$panelSummary = if ($panelProblem) { 'failed' } else { 'the app started and ended spikehub' }
if ($env:GITHUB_STEP_SUMMARY) {
  Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY -Value "Windows smoke test (WIN-2): $summary; single instance (R52): $single; hub panel: $panelSummary"
}
if ($singleProblem) { Write-Annotation error 'windows single instance' $singleProblem }
if ($panelProblem) { Write-Annotation error 'windows hub panel' $panelProblem }
Write-Annotation $(if ($failures) { 'error' } else { 'notice' }) 'windows S1' $summary
if ($failures -or $singleProblem -or $panelProblem) { exit 1 }
