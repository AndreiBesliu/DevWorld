#!/usr/bin/env bash
# AND-3 smoke test of the spike APK (plan 2026-10-18-phase0b-video-spike,
# Task 10), run by ipcam-mobile.yml inside a booted emulator. It installs the
# APK, then starts the app from a cold process several times: each start runs
# scenario S1 through the launch extras (R5) against spikehub on the runner
# (10.0.2.2 from the emulator) and waits up to 2.5 min for spikehub to store an
# S1 result with 20 samples. Several starts in one job show whether the launch
# is reliable, which one start per job cannot: every start must give its result,
# and the script goes on after a start that fails, to count how many do.
#
# The first launch after the emulator booted is a warm-up, not counted: the
# emulator is still finishing its boot then (see warm_up).
#
# Two more starts check what must not end or repeat a run:
# - the probe changes the system's font scale 5 s after the launch. The Activity
#   must not be created again for that (nor for the other changes an emulator or
#   a phone makes at run time, such as the SIM's mcc and mnc): a new Activity
#   would end the run in progress with its WebView;
# - the process-death start sends the finished app to the background, kills its
#   process and starts it again from the launcher Intent. The system restores the
#   Activity from the launch Intent it kept, extras included, and the scenario
#   must not run again.
#
# After the first start that gives its result, one more check reads the device
# facts that AND-1's report needs from the metrics plugin (spec §16.3, R48): the
# stored S1 result must carry the phone's memory (device.ramMb > 0), and the
# plugin's answer, read in the page, a charging state that is a boolean and
# matches the system's (the emulator reports AC power, so it is true there).
# Without it, a misspelt key or a lost EXTRA_PLUGGED read would pass every test.
#
# In quick mode only the cold starts and the device facts run (ipcam-mobile runs
# 3 starts on a push); the full mode adds the two checks above (on a pull
# request, with 5 starts).
#
# The age mode runs, after the S1 starts have passed in the same emulator, the
# recorder spike's AGE scenario once from a cold start (C1-R25): agerange's
# speed in an Android WebView. The emulator's speed is not judged; its result
# must arrive with mbps > 0, which shows that the scenario runs in the WebView.
#
# On failure it prints the cause as check annotations, which can be read where
# the job log cannot. Each check and the outcome also go to the job's step
# summary ($GITHUB_STEP_SUMMARY), which GitHub keeps whole, while it shows only
# about ten annotations of a level per step.
#
#   apps/mobile/ci/emulator-smoke.sh <apk> <spikehub results folder> <spikehub log> [starts] [full|quick]
#   apps/mobile/ci/emulator-smoke.sh <apk> <spikehub results folder> <spikehub log> age
set -euo pipefail
shopt -s nullglob

usage() {
  echo "usage: $0 <apk> <spikehub results folder> <spikehub log> [starts] [full|quick]" >&2
  echo "       $0 <apk> <spikehub results folder> <spikehub log> age" >&2
  exit 2
}
[[ $# -ge 3 ]] || usage
apk="$1"
results="$2"
hub_log="$3"
if [[ "${4:-}" == age ]]; then
  [[ $# -eq 4 ]] || usage
  starts=0
  mode=age
else
  starts="${4:-3}"
  mode="${5:-full}"
  [[ "${starts}" =~ ^[1-9][0-9]*$ && "${mode}" =~ ^(full|quick)$ ]] || usage
fi
app=ro.ipcam.spike
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
run_timeout=150 # seconds one start may take; S1 takes about a minute
age_timeout=240 # seconds the AGE start may take; a phone needs a few seconds

# Prints standard input as one annotation of level $1 (error, notice) titled $2.
# GitHub shows about 4000 characters of an annotation: keep the first 3300.
annotate() {
  local text body
  text="$(cat)"
  body="$(printf '%s' "${text:0:3300}" | sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "" } { print (NR > 1 ? "%0A" : "") $0 }')"
  echo "::$1 title=$2::${body:-(nothing)}"
}

# Appends a line to the job's step summary, when there is one.
summarize() {
  if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    printf '%s\n' "$*" >> "${GITHUB_STEP_SUMMARY}"
  fi
}

# The app's own log lines (tag SpikeMetrics): each Activity and page it went through.
app_log() {
  adb logcat -d -v time -s SpikeMetrics:V | sed -E 's/ [VDIWEF]\/SpikeMetrics\( *[0-9]+\): / /' | cut -c1-300 | tail -n "$1"
}

# The system's view of the app, its last $1 events: Activities created, created
# again and destroyed (wm_ since Android 10), processes started and died.
system_events() {
  adb logcat -b events -d -v threadtime |
    grep -E "(am|wm)_(create_activity|destroy_activity|relaunch|proc_start|proc_died|kill|crash|anr).*${app}" |
    cut -c1-200 | tail -n "$1" || true
}

# The page's state, the app's log lines and the system's view of the app, as
# annotations (few: a step shows about ten of a level). A subshell, so that its
# `set +e` stays inside.
diagnose() (
  set +e # the run has failed already: report whatever can be read
  local pid
  pid="$(adb shell pidof "${app}" | tr -d '\r')"
  {
    if [[ -z "${pid}" ]]; then
      echo "the app is not running"
    elif ! adb forward tcp:9222 "localabstract:webview_devtools_remote_${pid}" >/dev/null; then
      echo "the WebView's DevTools socket cannot be reached"
    else
      node "${here}/read-page.ts" 9222 2>&1 | head -c 1800
    fi
    echo "pid: ${pid:-none}"
    adb shell dumpsys window | grep -E 'mCurrentFocus|mFocusedApp'
    adb shell dumpsys power | grep -E 'mWakefulness='
  } 2>&1 | cut -c1-1800 | annotate error "emulator page"
  {
    app_log 14
    system_events 8
  } | annotate error "emulator app log"
  {
    adb logcat -d -v brief | grep -E '^[VDIWEF]/(Capacitor[^ (]*|chromium|cr_WebViewFactory|AndroidRuntime) *\(' |
      grep -v -E 'Handling local request|runtime_features|Cronet' | cut -c1-200 | tail -n 10
    echo "spikehub log:"
    tail -n 4 "${hub_log}" | cut -c1-200
  } | annotate error "emulator logcat"
)

# The emulator's 1-minute load average.
load_average() {
  adb shell cat /proc/loadavg | tr -d '\r' | cut -d' ' -f1
}

# The emulator's load average, and how long it has been up.
emulator_load() {
  printf 'load average %s, up %s s' "$(load_average)" "$(adb shell cat /proc/uptime | tr -d '\r' | cut -d. -f1)"
}

# The emulator's network comes up a little after boot: start S1 only when
# spikehub answers (toybox nc: -w connect timeout, -q quit after stdin ends).
# If it stays unreachable for 2 min, S1 cannot run: report why and stop. The
# age mode runs after the S1 starts, in the same emulator, with the APK installed.
if [[ "${mode}" != age ]]; then
  booted=${SECONDS}
  until adb shell 'nc -w 2 -q 1 10.0.2.2 7080 </dev/null' >/dev/null 2>&1; do
    if ((SECONDS - booted >= 120)); then
      echo "the emulator could not reach spikehub at 10.0.2.2:7080 for 2 min" | annotate error "emulator network"
      diagnose
      exit 1
    fi
    sleep 2
  done
  echo "spikehub reachable check took $((SECONDS - booted)) s; $(emulator_load)" | annotate notice "emulator network"

  adb install -r "${apk}"
fi

# The S1 result that the last successful cold_start stored.
stored_s1=""

# Starts the app cold with the launch extras (the process is gone, so the app has
# nothing but them) and waits for the S1 result. $1: the label of the start,
# $2: "probe" to change the font scale 5 s after the launch. Returns 0 with a
# notice when the result holds 20 samples (and sets stored_s1), else 1 with an
# error and the diagnosis.
cold_start() {
  local label="$1" probe="${2:-}" launch stored="" problem="" deadline file samples known line
  adb shell am force-stop "${app}"
  adb logcat -b all -c
  # Only a result stored from now on belongs to this start, not a late one of an earlier start.
  known="$(printf '%s\n' "${results}"/*.json)"
  launch="$(adb shell am start -W -n "${app}/.MainActivity" --es autorun S1 --es hub http://10.0.2.2:7080 | tr -d '\r')"
  launch="${launch//$'\n'/ | }"
  echo "${label}: ${launch}"
  if [[ "${probe}" == probe ]]; then
    sleep 5
    adb shell settings put system font_scale 1.3
  fi

  deadline=$((SECONDS + run_timeout))
  while [[ -z "${stored}${problem}" ]] && ((SECONDS < deadline)); do
    for file in "${results}"/*.json; do
      if grep -qxF -- "${file}" <<<"${known}" || ! jq -e '.scenario == "S1"' "${file}" >/dev/null; then
        continue
      fi
      known+="${file}"$'\n'
      samples="$(jq '.samples.firstFrameMs // [] | length' "${file}")"
      if ((samples == 20)); then
        stored="${file}"
      else
        problem="$(jq -r '"S1 stored with \(.samples.firstFrameMs // [] | length) of 20 samples; notes: \(.notes | join("; "))"' "${file}")"
      fi
    done
    [[ -n "${stored}${problem}" ]] || sleep 3
  done
  if [[ "${probe}" == probe ]]; then
    adb shell settings put system font_scale 1.0
  fi

  if [[ -z "${stored}" ]]; then
    echo "${problem:-no S1 result within ${run_timeout} s}; ${launch}" | annotate error "emulator S1 ${label}"
    summarize "- ${label}: failed: ${problem:-no S1 result within ${run_timeout} s}"
    diagnose
    return 1
  fi
  # Nearest rank: p50 is the 10th and p90 the 18th of the 20 sorted samples.
  line="$(jq -r '(.samples.firstFrameMs | sort | map(round)) as $ms
    | "S1 stored with 20 samples: first frame p50 \($ms[9]) ms, p90 \($ms[17]) ms, max \($ms[19]) ms;"
      + " \(.device.model) \(.device.os) \(.device.webview); notes: \(.notes | join("; "))"' "${stored}")"
  summarize "- ${label}: ${line}"
  {
    echo "${line}"
    echo "launch: ${launch}"
    app_log 12
  } | annotate notice "emulator S1 ${label}"
  stored_s1="${stored}"
}

# The device facts of the metrics plugin, with the app still on the page of the
# start that stored $1: device.ramMb in the stored S1 result, and ramMb and
# charging in what window.spikeNative.metrics() answers now, with charging
# compared to the system's own power state. Returns 0 with a notice, else 1
# with an error.
device_facts() {
  local stored="$1" ram metrics="" pid powered system charging line problems=()
  ram="$(jq -c '.device.ramMb' "${stored}")"
  jq -e '(.device.ramMb | type == "number") and .device.ramMb > 0' "${stored}" >/dev/null ||
    problems+=("the stored S1 result has no device.ramMb > 0 (got ${ram})")
  pid="$(adb shell pidof "${app}" | tr -d '\r')"
  if [[ -z "${pid}" ]] || ! adb forward tcp:9222 "localabstract:webview_devtools_remote_${pid}" >/dev/null; then
    problems+=("the WebView's DevTools socket cannot be reached (pid ${pid:-none})")
  else
    metrics="$(node "${here}/read-page.ts" 9222 metrics 2>&1 | head -c 1500 || true)"
    jq -e '(.ramMb | type == "number") and .ramMb > 0' <<<"${metrics}" >/dev/null 2>&1 ||
      problems+=("the metrics plugin answered no ramMb > 0: ${metrics}")
    # Any power source counts, as in the plugin: AC, USB, wireless or dock.
    powered="$(adb shell dumpsys battery | tr -d '\r' | grep -E '^ *(AC|USB|Wireless|Dock) powered:' | sed -E 's/^ +//' | paste -sd ';' - || true)"
    system=false
    if [[ "${powered}" == *'powered: true'* ]]; then
      system=true
    fi
    if ! jq -e '.charging | type == "boolean"' <<<"${metrics}" >/dev/null 2>&1; then
      problems+=("the metrics plugin answered no boolean charging: ${metrics}")
    else
      charging="$(jq -r '.charging' <<<"${metrics}")"
      [[ "${charging}" == "${system}" ]] ||
        problems+=("the metrics plugin says charging ${charging}, but the system says${powered:+ ${powered}} (charging ${system})")
    fi
  fi
  if ((${#problems[@]} > 0)); then
    printf '%s\n' "${problems[@]}" | annotate error "emulator device facts"
    summarize "- device facts: failed: ${problems[*]}"
    return 1
  fi
  line="S1 device.ramMb ${ram}; metrics plugin: ramMb $(jq -c '.ramMb' <<<"${metrics}"), charging ${charging} (system:${powered:+ ${powered}})"
  summarize "- device facts: ${line}"
  echo "${line}" | annotate notice "emulator device facts"
}

# After a start with its result: the app goes to the background (its Activity
# stops and saves its state), the system kills its process, and the launcher
# Intent starts it again. Returns 0 when the app is back in a new process and
# did not load the page with the launch extras again, else 1 with the diagnosis.
survives_process_death() {
  local label="process death" before after log deadline
  before="$(adb shell pidof "${app}" | tr -d '\r')"
  adb shell input keyevent KEYCODE_HOME
  sleep 3
  adb shell am kill "${app}"
  sleep 2
  after="$(adb shell pidof "${app}" | tr -d '\r')"
  if [[ -n "${after}" ]]; then
    echo "the process ${after} of the app is still alive after am kill" | annotate error "emulator S1 ${label}"
    return 1
  fi
  adb logcat -b all -c
  adb shell am start -W -n "${app}/.MainActivity" >/dev/null
  deadline=$((SECONDS + 30))
  until [[ "$(app_log 60)" == *'page loaded'* ]] || ((SECONDS >= deadline)); do sleep 2; done
  after="$(adb shell pidof "${app}" | tr -d '\r')"
  log="$(app_log 14)"
  if [[ -z "${after}" || "${after}" == "${before}" ]]; then
    echo "the app did not start again in a new process (pid ${before} before, ${after:-none} after)" | annotate error "emulator S1 ${label}"
  elif grep -q 'loadUrl:' <<<"${log}"; then
    echo "the restored Activity ran the launch extras again:"$'\n'"${log}" | annotate error "emulator S1 ${label}"
  elif ! grep -q 'restored=true' <<<"${log}"; then
    echo "the app started again, but its Activity was not restored from a saved state:"$'\n'"${log}" | annotate error "emulator S1 ${label}"
  elif ! grep -q 'page loaded' <<<"${log}"; then
    echo "the restored app loaded no page within 30 s:"$'\n'"${log}" | annotate error "emulator S1 ${label}"
  else
    echo "new process ${after} after ${before}; no autorun again:"$'\n'"${log}" | annotate notice "emulator S1 ${label}"
    summarize "- ${label}: the app came back in a new process and did not run the scenario again"
    return 0
  fi
  summarize "- ${label}: failed (see the annotations)"
  diagnose
  return 1
}

# The first launch after the emulator booted, not counted. The emulator is still
# finishing its boot then, and every failure of this job up to 2026-10-30 with
# annotations came from that launch alone, never from a later one: the system
# created the Activity again 0.1 s after the launch (an app resource change after
# boot, which no configChanges entry stops; AutorunHandoff now keeps the scenario
# across it), the player's first frame came after its 4 s limit, or the metrics
# plugin's first answer after the page's 3 s. The warm-up takes the app's and the
# WebView's first launch, then waits at most 2 min for the emulator's load average
# to fall below its CPU count, so that the counted starts measure the app's cold
# start rather than the device's boot. Its notice says what it saw; it never fails.
warm_up() {
  local deadline cpus log events waited
  adb shell am force-stop "${app}"
  adb logcat -b all -c
  adb shell am start -W -n "${app}/.MainActivity" >/dev/null
  deadline=$((SECONDS + 60))
  until [[ "$(app_log 60)" == *'page loaded'* ]] || ((SECONDS >= deadline)); do sleep 2; done
  log="$(app_log 14)"
  events="$(system_events 8)"
  cpus="$(adb shell nproc | tr -d '\r' || true)"
  waited=${SECONDS}
  deadline=$((SECONDS + 120))
  until awk -v avg="$(load_average)" -v cpus="${cpus:-1}" 'BEGIN { exit !(avg + 0 < cpus + 0) }' ||
    ((SECONDS >= deadline)); do
    sleep 5
  done
  adb shell am force-stop "${app}"
  summarize "- warm-up launch (not counted): waited $((SECONDS - waited)) s for the emulator to settle; $(emulator_load), ${cpus} CPUs"
  {
    echo "waited $((SECONDS - waited)) s for the emulator to settle; $(emulator_load), ${cpus} CPUs"
    echo "${log}"
    echo "${events}"
  } | annotate notice "emulator warm-up"
}

# AGE once (age mode): the app starts cold with the launch extras autorun=AGE,
# like an S1 start, since a warm start ignores them. Returns 0 with a notice when
# spikehub stores an AGE result with mbps > 0 that was not interrupted, else 1
# with an error and the diagnosis. The notice and the error carry the app's log
# lines: the extras that the Activity read and the scenario that the page got.
age_start() {
  local launch stored="" deadline file known line problem=""
  adb shell am force-stop "${app}"
  adb logcat -b all -c
  known="$(printf '%s\n' "${results}"/*.json)"
  launch="$(adb shell am start -W -n "${app}/.MainActivity" --es autorun AGE --es hub http://10.0.2.2:7080 | tr -d '\r')"
  launch="${launch//$'\n'/ | }"
  echo "AGE: ${launch}"
  deadline=$((SECONDS + age_timeout))
  while [[ -z "${stored}" ]] && ((SECONDS < deadline)); do
    for file in "${results}"/*.json; do
      if ! grep -qxF -- "${file}" <<<"${known}" && jq -e '.scenario == "AGE"' "${file}" >/dev/null; then
        stored="${file}"
      fi
    done
    [[ -n "${stored}" ]] || sleep 3
  done
  if [[ -z "${stored}" ]]; then
    problem="no AGE result within ${age_timeout} s"
  elif ! jq -e '(.mbps | type == "number") and .mbps > 0 and .interrupted != true' "${stored}" >/dev/null; then
    problem="$(jq -r '"AGE stored without mbps > 0: mbps \(.mbps), interrupted \(.interrupted); notes: \(.notes | join("; "))"' "${stored}")"
  fi
  if [[ -n "${problem}" ]]; then
    {
      echo "${problem}; ${launch}"
      app_log 12
    } | annotate error "emulator AGE"
    summarize "- AGE: failed: ${problem}"
    diagnose
    return 1
  fi
  line="$(jq -r '"AGE stored: \(.mbps * 10 | round / 10) Mbps, \(.bytes) bytes in \(.ms | round) ms (the emulator'"'"'s speed is not judged);"
    + " \(.device.model) \(.device.os) \(.device.webview); notes: \(.notes | join("; "))"' "${stored}")"
  summarize "- ${line}"
  {
    echo "${line}"
    echo "launch: ${launch}"
    app_log 12
  } | annotate notice "emulator AGE"
}

if [[ "${mode}" == age ]]; then
  summarize "### AGE in the emulator's WebView (C1-R25)"
  age_start
  exit
fi

summarize "### Emulator smoke test (AND-3), ${mode} mode"
warm_up
failures=0
checks=$((starts + 1)) # the starts and the device facts
facts=""
for ((start = 1; start <= starts; start++)); do
  if ! cold_start "start ${start} of ${starts}"; then
    failures=$((failures + 1))
  elif [[ -z "${facts}" ]]; then
    facts=checked
    device_facts "${stored_s1}" || failures=$((failures + 1))
  fi
done
if [[ -z "${facts}" ]]; then
  # No start stored a result, so the device facts could not be read either.
  failures=$((failures + 1))
  summarize "- device facts: not checked, no start stored an S1 result"
fi
if [[ "${mode}" == full ]]; then
  checks=$((checks + 3))
  cold_start "probe (font scale change)" probe || failures=$((failures + 1))
  if cold_start "start before process death"; then
    survives_process_death || failures=$((failures + 1))
  else
    failures=$((failures + 1))
  fi
fi

if ((failures == 0)); then
  summarize "All ${checks} checks passed."
else
  summarize "${failures} of ${checks} checks failed."
fi
if ((failures > 0)); then
  echo "${failures} of ${checks} checks failed" | annotate error "emulator S1"
  exit 1
fi
