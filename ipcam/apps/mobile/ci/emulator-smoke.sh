#!/usr/bin/env bash
# AND-3 smoke test of the spike APK (plan 2026-10-18-phase0b-video-spike,
# Task 10), run by ipcam-mobile.yml inside a booted emulator. It installs the
# APK, starts scenario S1 through the launch extras (R5) against spikehub on the
# runner (10.0.2.2 from the emulator) and waits for spikehub to store an S1
# result with 20 samples: up to 2.5 min for each of two cold starts. On failure
# it prints the cause as check annotations, which can be read where the job log
# cannot.
#
#   apps/mobile/ci/emulator-smoke.sh <apk> <spikehub results folder> <spikehub log>
set -euo pipefail

apk="$1"
results="$2"
hub_log="$3"
app=ro.ipcam.spike
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Prints standard input as one annotation of level $1 (error, warning, notice) titled $2.
annotate() {
  local body
  body="$(sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "" } { print (NR > 1 ? "%0A" : "") $0 }')"
  echo "::$1 title=$2::${body:-(nothing)}"
}

# The page's state, the app's log lines and spikehub's log, as annotations of
# level $1 (error by default). A subshell, so that its `set +e` stays inside.
diagnose() (
  set +e # the run has failed already: report whatever can be read
  level="${1:-error}"
  pid="$(adb shell pidof "${app}" | tr -d '\r')"
  if [[ -z "${pid}" ]]; then
    echo "the app is not running"
  elif ! adb forward tcp:9222 "localabstract:webview_devtools_remote_${pid}" >/dev/null; then
    echo "the WebView's DevTools socket cannot be reached"
  else
    node "${here}/read-page.ts" 9222 2>&1 | head -c 6000
  fi | annotate "${level}" "emulator page"
  # Every Bridge the app created: more than one means the Activity was created again.
  adb logcat -d -v brief | grep -E '^D/Capacitor *\(.*(Starting BridgeActivity|Loading app at)' |
    annotate "${level}" "emulator bridges"
  adb logcat -d -v brief | grep -E '^[VDIWEF]/(Capacitor[^ (]*|chromium|cr_[A-Za-z]+|WebViewFactory|AndroidRuntime) *\(' |
    tail -n 60 | annotate "${level}" "emulator logcat"
  tail -n 40 "${hub_log}" | annotate "${level}" "spikehub log"
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

# Waits up to $1 s for the S1 result. Exits 0 when it holds 20 samples and 1,
# with the diagnosis, when it holds fewer; returns when none was stored in time.
wait_for_s1() {
  local deadline=$((SECONDS + $1)) file samples
  while ((SECONDS < deadline)); do
    for file in "${results}"/*.json; do
      if [[ ! -f "${file}" ]] || ! jq -e '.scenario == "S1"' "${file}" >/dev/null; then
        continue
      fi
      samples="$(jq '.samples.firstFrameMs // [] | length' "${file}")"
      if ((samples == 20)); then
        # Nearest rank: p50 is the 10th and p90 the 18th of the 20 sorted samples.
        jq -r '(.samples.firstFrameMs | sort | map(round)) as $ms
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
}

adb install -r "${apk}"

# The plugin hands the launch extras to the page once, on a cold start. If the
# Activity is created again in the first moments (the emulator may change its
# configuration right after boot), the page stays on its start page and no S1
# comes: the second start is a new cold start.
for attempt in 1 2; do
  adb shell am force-stop "${app}"
  adb shell am start -W -n "${app}/.MainActivity" --es autorun S1 --es hub http://10.0.2.2:7080
  wait_for_s1 150
  if ((attempt == 1)); then
    echo "no S1 result within 2.5 min: starting the app again" | annotate warning "emulator S1"
    diagnose warning
  fi
done

echo "no S1 result after two starts" | annotate error "emulator S1"
diagnose
exit 1
