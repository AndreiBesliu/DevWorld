#!/usr/bin/env bash
# Runs one MI1 lab shard on this runner (R03, Task 7): reccontroller -local
# serving the media on loopback, and recspike's shard against it. On a
# qemu-reset shard, recspike boots the guest that runs the shard and resets
# it at each cut (mi1.QEMU). The lab workflow and the mi1-smoke job both run
# it, so that the smoke goes the lab's way.
#
#   tools/scripts/mi1-lab.sh <bin dir> <media dir> <kind> <shard> [--smoke]
#
# <bin dir> holds recspike, reccontroller, ipcam-hub and mediamtx (.exe on
# Windows); reccontroller writes the envelopes to <bin dir>/results/MI1/<set>/
# and its screen to <bin dir>/reccontroller.out. recspike's output reaches
# stdout as it is, so that each "mi1-diag: " line starts a line. Exits with
# recspike's status.
set -euo pipefail

if [[ $# -lt 4 || $# -gt 5 ]]; then
  echo "usage: $0 <bin dir> <media dir> <kind> <shard> [--smoke]" >&2
  exit 2
fi
bin="$1" media="$2" kind="$3" shard="$4"
shift 4
exe=""
if [[ "${OS:-}" == Windows_NT ]]; then
  exe=.exe
fi

screen="${bin}/reccontroller.out"
"${bin}/reccontroller${exe}" -local -media "${media}" > "${screen}" 2>&1 < /dev/null &
controller=$!
# shellcheck disable=SC2329 # the EXIT trap calls it
stop() {
  if [[ -n "${exe}" ]]; then
    taskkill //F //IM "reccontroller.exe" > /dev/null 2>&1 || true
  else
    kill "${controller}" 2> /dev/null || true
  fi
  wait "${controller}" 2> /dev/null || true
}
trap stop EXIT

for _ in $(seq 60); do
  if grep -q '^reccontroller rulează' "${screen}" || ! kill -0 "${controller}" 2> /dev/null; then
    break
  fi
  sleep 1
done
if ! grep -q '^reccontroller rulează' "${screen}"; then
  echo "reccontroller did not start:"
  cat "${screen}"
  exit 1
fi

status=0
"${bin}/recspike${exe}" mi1 --lab --kind "${kind}" --shard "${shard}" "$@" || status=$?
exit "${status}"
