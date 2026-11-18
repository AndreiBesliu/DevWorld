#!/usr/bin/env bash
# Fails when a dependency of the given packages has a license that is not in
# tools/licenses/allowlist.txt. Run it from anywhere; the default packages are
# the video spike's.
#
#   tools/scripts/check-licenses.sh [package...]
set -euo pipefail

ipcam_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
allowlist="${ipcam_dir}/tools/licenses/allowlist.txt"

# grep exits 1 when nothing is left after filtering; the check below reports it.
allowed="$({ grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "${allowlist}" || true; } | paste -sd, -)"
if [[ -z "${allowed}" ]]; then
  echo "check-licenses: ${allowlist} lists no licenses" >&2
  exit 1
fi

if [[ $# -eq 0 ]]; then
  set -- ./tools/spikes/video/...
fi

# Our own packages have no license file to classify; their dependencies are
# still checked.
own_module_prefix="github.com/AndreiBesliu/DevWorld/ipcam"

cd "${ipcam_dir}"
go tool go-licenses check \
  --allowed_licenses="${allowed}" \
  --ignore "${own_module_prefix}" \
  "$@"
