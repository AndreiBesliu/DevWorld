#!/usr/bin/env bash
# Checks that the Go workspace leaves the video spike's build list as it is
# (ruling R04). Go picks one version of each module for the whole workspace,
# the highest that any workspace module requires, so a module that joins
# ipcam/go.work with a newer shared dependency would change what the video
# spike builds and tests. This compares the modules (and versions) of every
# package that the video spike's packages and tests depend on, in the
# workspace (ipcam/go.work, whatever GOWORK the caller set) and with
# GOWORK=off.
#
#   tools/scripts/check-video-build-list.sh
#
# Prints "video build list unchanged (<n> modules)" and exits 0, or names each
# module whose version differs and exits 1. Exits 2 on a usage error.
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "usage: $0" >&2
  exit 2
fi

ipcam_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ipcam_dir}/tools/spikes/video"

# One line per package: the module of a dependency, or nothing for the
# standard library and for the main modules.
format='{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}'
workspace="$(GOWORK="${ipcam_dir}/go.work" go list -deps -test -f "${format}" ./... | awk 'NF' | LC_ALL=C sort -u)"
alone="$(GOWORK=off go list -deps -test -f "${format}" ./... | awk 'NF' | LC_ALL=C sort -u)"

if [[ "${workspace}" == "${alone}" ]]; then
  echo "video build list unchanged ($(awk 'NF { n++ } END { print n + 0 }' <<< "${alone}") modules)"
  exit 0
fi

echo "video build list changed by the workspace (ipcam/go.work):"
# Each module whose version differs, or that only one of the two lists has.
awk 'FNR == 1 { list++ }
  NF == 0 { next }
  list == 1 { alone[$1] = $2; paths[$1] = 1 }
  list == 2 { workspace[$1] = $2; paths[$1] = 1 }
  END {
    for (path in paths) {
      a = (path in alone) ? alone[path] : "none"
      w = (path in workspace) ? workspace[path] : "none"
      if (a != w) print "  " path ": " a " with GOWORK=off, " w " in the workspace"
    }
  }' <(printf '%s\n' "${alone}") <(printf '%s\n' "${workspace}") | LC_ALL=C sort
exit 1
