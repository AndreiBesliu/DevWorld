#!/usr/bin/env bash
# Fails when a dependency of the given packages has a license that is not in
# tools/licenses/allowlist.txt. Run it from anywhere; the default packages are
# the video spike's.
#
#   tools/scripts/check-licenses.sh [package...]
#   tools/scripts/check-licenses.sh -C <dir> [package...]
#
# With -C, go-licenses (built from tools/go.mod) runs in <dir> with GOWORK=off,
# for a module outside ipcam/go.work such as the MediaMTX fork; the packages
# are read there and default to ./... . A relative <dir> is read from where
# the script runs. Exits 2 on a usage error.
set -euo pipefail

usage() {
  echo "usage: $0 [-C <dir>] [package...]" >&2
  exit 2
}

ipcam_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
allowlist="${ipcam_dir}/tools/licenses/allowlist.txt"

dir=""
if [[ "${1:-}" == -C ]]; then
  [[ $# -ge 2 ]] || usage
  [[ -d "$2" ]] || { echo "check-licenses: no directory $2" >&2; exit 1; }
  dir="$(cd "$2" && pwd)"
  shift 2
fi

# grep exits 1 when nothing is left after filtering; the check below reports it.
allowed="$({ grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "${allowlist}" || true; } | paste -sd, -)"
if [[ -z "${allowed}" ]]; then
  echo "check-licenses: ${allowlist} lists no licenses" >&2
  exit 1
fi

# Our own packages have no license file to classify; their dependencies are
# still checked.
own_module_prefix="github.com/AndreiBesliu/DevWorld/ipcam"
# Packages whose module has no license file, so go-licenses cannot classify
# them, with the license that a person read in their source. They are skipped
# too; the release notices quote that source instead.
#   github.com/benburkert/openpgp/aes/keywrap: BSD-2-Clause, the header of
#   keywrap.go ("Copyright 2014 Matthew Endsley"). The MediaMTX fork reaches
#   it through github.com/datarhei/gosrt/crypto (SRT encryption).
read_by_hand=(github.com/benburkert/openpgp/aes/keywrap)
ignore=(--ignore "${own_module_prefix}")
for package in "${read_by_hand[@]}"; do
  ignore+=(--ignore "${package}")
done

if [[ -n "${dir}" ]]; then
  [[ $# -gt 0 ]] || set -- ./...
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' EXIT
  # The tools module's Go builds go-licenses and loads <dir>'s packages:
  # go-licenses tells the standard library apart by the GOROOT it was built
  # with, and a go.mod in <dir> that names an older Go could switch toolchains.
  GOTOOLCHAIN="$(cd "${ipcam_dir}/tools" && GOWORK=off go env GOVERSION)"
  export GOTOOLCHAIN
  # `go tool` in <dir> would look for the tool in <dir>'s go.mod, so the
  # binary is built from the tools module, outside the workspace like <dir>.
  (cd "${ipcam_dir}/tools" && GOWORK=off go build -o "${tmp}/go-licenses" github.com/google/go-licenses/v2)
  cd "${dir}"
  GOWORK=off "${tmp}/go-licenses" check --allowed_licenses="${allowed}" "${ignore[@]}" "$@"
  exit 0
fi

if [[ $# -eq 0 ]]; then
  set -- ./tools/spikes/video/...
fi

cd "${ipcam_dir}"
go tool go-licenses check --allowed_licenses="${allowed}" "${ignore[@]}" "$@"
