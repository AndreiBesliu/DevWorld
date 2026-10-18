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

# Prints standard input as one annotation of level $1 (error, notice) titled $2.
annotate() {
  local body
  body="$(sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "" } { print (NR > 1 ? "%0A" : "") $0 }')"
  echo "::$1 title=$2::${body:-(nothing)}"
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
  fi | annotate error "emulator page"
  adb logcat -d -v brief | grep -E '^[VDIWEF]/(Capacitor[^ (]*|chromium|cr_[A-Za-z]+|WebViewFactory|AndroidRuntime) *\(' |
    tail -n 60 | annotate error "emulator logcat"
  tail -n 40 "${hub_log}" | annotate error "spikehub log"
}

# The emulator's network comes up a little after boot: start S1 only when
# spikehub answers (toybox nc: -w connect timeout, -q quit after stdin ends).
booted=${SECONDS}
until adb shell 'nc -w 2 -q 1 10.0.2.2 7080 </dev/null' >/dev/null 2>&1; do
  if ((SECONDS - booted >= 120)); then
    echo "the emulator could not reach spikehub at 10.0.2.2:7080 for 2 min" | annotate error "emulator network"
    break
  fi
  sleep 2
done
echo "spikehub reachable check took $((SECONDS - booted)) s" | annotate notice "emulator network"

adb install -r "${apk}"
adb shell am start -W -n "${app}/.MainActivity" --es autorun S1 --es hub http://10.0.2.2:7080

deadline=$((SECONDS + 300))
while ((SECONDS < deadline)); do
  for file in "${results}"/*.json; do
    if [[ ! -f "${file}" ]] || ! jq -e '.scenario == "S1"' "${file}" >/dev/null; then
      continue
    fi
    samples="$(jq '.samples.firstFrameMs // [] | length' "${file}")"
    if ((samples == 20)); then
      # Nearest rank: p50 is the 10th and p90 the 18th of the 20 sorted samples.
      jq -r '(.samples.firstFrameMs | sort) as $ms
        | "S1 stored with 20 samples: first frame p50 \($ms[9]) ms, p90 \($ms[17]) ms, max \($ms[19]) ms;"
          + " \(.device.model) \(.device.os) \(.device.webview); notes: \(.notes | join("; "))"' "${file}" |
        annotate notice "emulator S1"
      exit 0
    fi
    jq -r '"S1 stored with \(.samples.firstFrameMs // [] | length) of 20 samples; notes: \(.notes | join("; "))"' "${file}" |
      annotate error "emulator S1"
    diagnose
    exit 1
  done
  sleep 5
done

echo "no S1 result within 5 min" | annotate error "emulator S1"
diagnose
exit 1
