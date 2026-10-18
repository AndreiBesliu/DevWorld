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
# Two more starts check what must not end or repeat a run:
# - the probe changes the system's font scale 5 s after the launch. The Activity
#   must not be created again for that (nor for the other changes an emulator or
#   a phone makes at run time, such as the SIM's mcc and mnc): a new Activity gets
#   no launch extras, and the scenario would be lost;
# - the process-death start sends the finished app to the background, kills its
#   process and starts it again from the launcher Intent. The system restores the
#   Activity from the launch Intent it kept, extras included, and the scenario
#   must not run again.
#
# On failure it prints the cause as check annotations, which can be read where
# the job log cannot.
#
#   apps/mobile/ci/emulator-smoke.sh <apk> <spikehub results folder> <spikehub log> [starts]
set -euo pipefail
shopt -s nullglob

apk="$1"
results="$2"
hub_log="$3"
starts="${4:-3}"
app=ro.ipcam.spike
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
run_timeout=150 # seconds one start may take; S1 takes about a minute

# Prints standard input as one annotation of level $1 (error, notice) titled $2.
# GitHub shows about 4000 characters of an annotation: keep the first 3300.
annotate() {
  local text body
  text="$(cat)"
  body="$(printf '%s' "${text:0:3300}" | sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "" } { print (NR > 1 ? "%0A" : "") $0 }')"
  echo "::$1 title=$2::${body:-(nothing)}"
}

# The app's own log lines (tag SpikeMetrics): each Activity and page it went through.
app_log() {
  adb logcat -d -v time -s SpikeMetrics:V | sed -E 's/ [VDIWEF]\/SpikeMetrics\( *[0-9]+\): / /' | cut -c1-300 | tail -n "$1"
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
    # The system's view of the app: Activities created and destroyed, processes started and died.
    adb logcat -b events -d -v threadtime | grep -E "am_(create_activity|destroy_activity|relaunch_activity|proc_start|proc_died|kill|crash|anr).*${app}" |
      cut -c1-200 | tail -n 8
  } | annotate error "emulator app log"
  {
    adb logcat -d -v brief | grep -E '^[VDIWEF]/(Capacitor[^ (]*|chromium|cr_WebViewFactory|AndroidRuntime) *\(' |
      grep -v -E 'Handling local request|runtime_features|Cronet' | cut -c1-200 | tail -n 10
    echo "spikehub log:"
    tail -n 4 "${hub_log}" | cut -c1-200
  } | annotate error "emulator logcat"
)

# The emulator's network comes up a little after boot: start S1 only when
# spikehub answers (toybox nc: -w connect timeout, -q quit after stdin ends).
# If it stays unreachable for 2 min, S1 cannot run: report why and stop.
booted=${SECONDS}
until adb shell 'nc -w 2 -q 1 10.0.2.2 7080 </dev/null' >/dev/null 2>&1; do
  if ((SECONDS - booted >= 120)); then
    echo "the emulator could not reach spikehub at 10.0.2.2:7080 for 2 min" | annotate error "emulator network"
    diagnose
    exit 1
  fi
  sleep 2
done
echo "spikehub reachable check took $((SECONDS - booted)) s" | annotate notice "emulator network"

adb install -r "${apk}"

# Starts the app cold with the launch extras (the process is gone, so the app has
# nothing but them) and waits for the S1 result. $1: the label of the start,
# $2: "probe" to change the font scale 5 s after the launch. Returns 0 with a
# notice when the result holds 20 samples, else 1 with an error and the diagnosis.
cold_start() {
  local label="$1" probe="${2:-}" launch stored="" problem="" deadline file samples
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
    diagnose
    return 1
  fi
  {
    # Nearest rank: p50 is the 10th and p90 the 18th of the 20 sorted samples.
    jq -r '(.samples.firstFrameMs | sort | map(round)) as $ms
      | "S1 stored with 20 samples: first frame p50 \($ms[9]) ms, p90 \($ms[17]) ms, max \($ms[19]) ms;"
        + " \(.device.model) \(.device.os) \(.device.webview); notes: \(.notes | join("; "))"' "${stored}"
    echo "launch: ${launch}"
    app_log 12
  } | annotate notice "emulator S1 ${label}"
}

# After a start with its result: the app goes to the background (its Activity
# stops and saves its state), the system kills its process, and the launcher
# Intent starts it again. Returns 0 when the app is back in a new process and
# did not load the page with the launch extras again, else 1 with the diagnosis.
survives_process_death() {
  local label="process death" before after log
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
  elif ! grep -q 'page loaded' <<<"${log}"; then
    echo "the restored app loaded no page within 30 s:"$'\n'"${log}" | annotate error "emulator S1 ${label}"
  else
    echo "new process ${after} after ${before}; no autorun again:"$'\n'"${log}" | annotate notice "emulator S1 ${label}"
    return 0
  fi
  diagnose
  return 1
}

failures=0
for ((start = 1; start <= starts; start++)); do
  cold_start "start ${start} of ${starts}" || failures=$((failures + 1))
done
cold_start "probe (font scale change)" probe || failures=$((failures + 1))
if cold_start "start before process death"; then
  survives_process_death || failures=$((failures + 1))
else
  failures=$((failures + 1))
fi

if ((failures > 0)); then
  echo "${failures} of $((starts + 3)) checks failed" | annotate error "emulator S1"
  exit 1
fi
