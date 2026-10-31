# SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md
#
# Runs one program in an S4U logon of the current user, the logon that
# uploadspike's autostart task runs in (P49), and reports what it printed and
# how it ended. It registers a temporary Task Scheduler task for the current
# user (New-ScheduledTaskPrincipal -LogonType S4U -RunLevel Highest), starts it
# once and waits for it. The program's stdout, then its stderr, go to -Out.
# Then it unregisters the task and exits with the program's exit code, or 124
# when the program did not end within -TimeoutSec (it is then killed with its
# children), or 125 when this script failed (the reason is on stderr, and in
# -Out when the task got as far as starting the program). An S4U task with the
# highest run level needs an administrator, as the CI runner is.
#
#   pwsh -File ci/s4u-run.ps1 -Exe <path> [-Arguments <string>] -Out <file> [-TimeoutSec 300]
#
# -Arguments reaches the program unchanged, as the rest of its command line.
# The windows job of ipcam-spike-upload.yml runs whoami.exe this way; Task 9
# unseals a token in the S4U logon through it.
#
# In the task, this script runs again with -Job <file>: it runs the program
# with its output redirected, writes -Out, then the exit code to the job's code
# file, which the waiting script reads.
#Requires -Version 7
[CmdletBinding(DefaultParameterSetName = 'Run')]
param(
  [Parameter(Mandatory, ParameterSetName = 'Run')] [string] $Exe,
  [Parameter(ParameterSetName = 'Run')] [string] $Arguments = '',
  [Parameter(Mandatory, ParameterSetName = 'Run')] [string] $Out,
  [Parameter(ParameterSetName = 'Run')] [ValidateRange(1, 86400)] [int] $TimeoutSec = 300,
  # The task's own call: the job file that the waiting script wrote.
  [Parameter(Mandatory, ParameterSetName = 'Job')] [string] $Job
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Task Scheduler's LastTaskResult while a task runs, and before its first run.
$taskRunning = 0x41301
$taskHasNotRun = 0x41303

# Runs the job's program and returns its exit code, or 124 after killing it
# and its children at the timeout. Its stdout, then its stderr, go to $Output.
function Invoke-Program($Spec, [IO.Stream] $Output) {
  $start = [Diagnostics.ProcessStartInfo]::new([string] $Spec.exe, [string] $Spec.arguments)
  $start.UseShellExecute = $false
  $start.RedirectStandardOutput = $true
  $start.RedirectStandardError = $true
  $process = [Diagnostics.Process]::Start($start)
  $stdout = [IO.MemoryStream]::new()
  $stderr = [IO.MemoryStream]::new()
  # Both pipes are read at once, so that a full one never blocks the program.
  $copies = [Threading.Tasks.Task[]] @(
    $process.StandardOutput.BaseStream.CopyToAsync($stdout),
    $process.StandardError.BaseStream.CopyToAsync($stderr)
  )
  $code = 124
  if ($process.WaitForExit([int] $Spec.timeoutSec * 1000)) {
    $code = $process.ExitCode
  } else {
    $process.Kill($true)
    $process.WaitForExit()
  }
  # The pipes close once the program and every child that holds them ended.
  $null = [Threading.Tasks.Task]::WaitAll($copies, 10000)
  $stdout.WriteTo($Output)
  $stderr.WriteTo($Output)
  $code
}

# The task's side: runs the job's program, writes its output to the job's
# -Out, then its exit code to the job's code file, renamed into place so that
# the waiting script never reads half of it. A program that cannot be started
# gets 125, and the reason goes to -Out.
function Invoke-Job([string] $JobFile) {
  $spec = Get-Content -Raw -LiteralPath $JobFile | ConvertFrom-Json
  $output = [IO.MemoryStream]::new()
  $code = 125
  try {
    $code = Invoke-Program $spec $output
  } catch {
    $reason = [Text.Encoding]::UTF8.GetBytes("s4u-run.ps1: $($_.Exception.Message)`n")
    $output.Write($reason, 0, $reason.Length)
  }
  [IO.File]::WriteAllBytes([string] $spec.out, $output.ToArray())
  [IO.File]::WriteAllText("$($spec.code).tmp", "$code")
  Move-Item -Force -LiteralPath "$($spec.code).tmp" -Destination ([string] $spec.code)
  $code
}

# The waiting side: registers the task, starts it once, waits for the code
# file and returns the program's exit code. The task is unregistered and its
# job folder removed however this ends.
function Invoke-InS4uTask {
  $exePath = (Resolve-Path -LiteralPath $Exe).Path
  $outPath = [IO.Path]::GetFullPath($Out)
  $name = "ipcam-s4u-run-$([guid]::NewGuid().ToString('N'))"
  $work = (New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) $name)).FullName
  $jobFile = Join-Path $work 'job.json'
  $codeFile = Join-Path $work 'code'
  try {
    # An -Out left by an earlier run must not pass for this run's output.
    Remove-Item -LiteralPath $outPath -Force -ErrorAction SilentlyContinue
    @{ exe = $exePath; arguments = $Arguments; out = $outPath; code = $codeFile; timeoutSec = $TimeoutSec } |
      ConvertTo-Json | Set-Content -LiteralPath $jobFile -Encoding utf8NoBOM
    $user = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $action = New-ScheduledTaskAction -Execute ([Environment]::ProcessPath) `
      -Argument "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$PSCommandPath`" -Job `"$jobFile`""
    $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType S4U -RunLevel Highest
    # Task Scheduler ends the task by itself if the script in it hangs.
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
      -ExecutionTimeLimit (New-TimeSpan -Seconds ($TimeoutSec + 120))
    $null = Register-ScheduledTask -TaskName $name -Action $action -Principal $principal -Settings $settings
    $null = Start-ScheduledTask -TaskName $name
    # The task gets 60 s more than the program, to start the script in it.
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSec + 60)
    while (-not (Test-Path -LiteralPath $codeFile)) {
      $state = (Get-ScheduledTask -TaskName $name).State
      $result = (Get-ScheduledTaskInfo -TaskName $name).LastTaskResult
      if (($state -notin 'Queued', 'Running') -and ($result -notin $taskRunning, $taskHasNotRun)) {
        # The script in the task writes the code file just before it ends.
        if (Test-Path -LiteralPath $codeFile) { break }
        throw ('the S4U task ended with result 0x{0:X8} and wrote no exit code' -f $result)
      }
      if ([DateTime]::UtcNow -gt $deadline) {
        [Console]::Error.WriteLine("s4u-run.ps1: the S4U task did not end within $($TimeoutSec + 60) s")
        return 124
      }
      Start-Sleep -Milliseconds 500
    }
    $code = [int] (Get-Content -Raw -LiteralPath $codeFile).Trim()
    # The script in the task exits right after it writes the code file.
    $ends = [DateTime]::UtcNow.AddSeconds(30)
    while ((Get-ScheduledTask -TaskName $name).State -eq 'Running' -and [DateTime]::UtcNow -lt $ends) {
      Start-Sleep -Milliseconds 200
    }
    return $code
  } finally {
    $task = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
    if ($task -and $task.State -eq 'Running') {
      $null = Stop-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
    }
    if ($task) {
      $null = Unregister-ScheduledTask -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
  }
}

$code = 125
try {
  $code = if ($PSCmdlet.ParameterSetName -eq 'Job') { Invoke-Job $Job } else { Invoke-InS4uTask }
} catch {
  [Console]::Error.WriteLine("s4u-run.ps1: $($_.Exception.Message)")
}
exit $code
