#!/usr/bin/env bash
# AND-3 smoke test of the spike APK (plan 2026-10-18-phase0b-video-spike,
# Task 10), run by ipcam-mobile.yml inside a booted emulator. It installs the
# APK, starts scenario S1 through the launch extras (R5) against spikehub on the
# runner (10.0.2.2 from the emulator) and waits up to 5 min for spikehub to
# store an S1 result with 20 samples. On failure it prints the cause as check
# annotations, which can be read where the job log cannot.
#
#   apps/mobile/ci/emulator-smoke.sh <apk> <spikehub results folder> <spikehub log>
set -euo pipefail

apk="$1"
results="$2"
hub_log="$3"
app=ro.ipcam.spike
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Prints standard input as one error annotation titled $1.
annotate() {
  local body
  body="$(sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "" } { print (NR > 1 ? "%0A" : "") $0 }')"
  echo "::error title=$1::${body:-(nothing)}"
}

# The page's state, the app's log lines and spikehub's log, as annotations.
diagnose() {
  set +e # the run has failed already: report whatever can be read
  local pid
  pid="$(adb shell pidof "${app}" | tr -d '\r')"
  if [[ -z "${pid}" ]]; then
    echo "the app is not running"
  elif ! adb forward tcp:9222 "localabstract:webview_devtools_remote_${pid}" >/dev/null; then
    echo "the WebView's DevTools socket cannot be reached"
  else
    node "${here}/read-page.ts" 9222 2>&1 | head -c 6000
  fi | annotate "emulator page"
  adb logcat -d -v brief | grep -E 'Capacitor|chromium|cr_|WebView|AndroidRuntime|ro\.ipcam' | tail -n 60 | annotate "emulator logcat"
  tail -n 40 "${hub_log}" | annotate "spikehub log"
}

adb install -r "${apk}"
adb shell am start -W -n "${app}/.MainActivity" --es autorun S1 --es hub http://10.0.2.2:7080

deadline=$((SECONDS + 300))
while ((SECONDS < deadline)); do
  for file in "${results}"/*.json; do
    if [[ ! -f "${file}" ]] || ! jq -e '.scenario == "S1"' "${file}" >/dev/null; then
      continue
    fi
    samples="$(jq '.samples.firstFrameMs // [] | length' "${file}")"
    jq -c '{scenario, device, durationS, interrupted, notes, firstFrameMs: .samples.firstFrameMs}' "${file}"
    if ((samples == 20)); then
      echo "S1 stored with 20 samples: $(basename "${file}")"
      exit 0
    fi
    jq -r '"S1 stored with \(.samples.firstFrameMs // [] | length) of 20 samples; notes: \(.notes | join("; "))"' "${file}" | annotate "emulator S1"
    diagnose
    exit 1
  done
  sleep 5
done

echo "no S1 result within 5 min" | annotate "emulator S1"
diagnose
exit 1
