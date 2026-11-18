#!/usr/bin/env bash
# Tests of tools/scripts/check-video-build-list.sh with a fake `go` that prints
# the video spike's dependencies as `go list` would: one line per package, in
# the workspace (FAKE_WORKSPACE) or with GOWORK=off (FAKE_ALONE).
#
#   tools/scripts/test/check-video-build-list.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../check-video-build-list.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

# The fake go answers only the one command the script runs, only in the video
# spike's directory, and only with GOWORK naming ipcam/go.work or off.
mkdir -p "${tmp}/bin"
cat > "${tmp}/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
want=(list -deps -test -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' ./...)
[[ "$*" == "${want[*]}" ]] || { echo "fake go: unexpected arguments: $*" >&2; exit 2; }
[[ "${PWD}" == */ipcam/tools/spikes/video ]] || { echo "fake go: run in ${PWD}" >&2; exit 2; }
[[ -z "${FAKE_GO_FAILS:-}" ]] || { echo 'go: updates to go.mod needed' >&2; exit 1; }
case "${GOWORK:-}" in
  off) printf '%b' "${FAKE_ALONE}" ;;
  */ipcam/go.work) printf '%b' "${FAKE_WORKSPACE}" ;;
  *) echo "fake go: GOWORK=${GOWORK:-}" >&2; exit 2 ;;
esac
EOF
chmod +x "${tmp}/bin/go"

# lists <workspace list> <list with GOWORK=off> [<NAME=value>...]: runs the script with the fake go.
lists() {
  local workspace="$1" alone="$2"
  shift 2
  env PATH="${tmp}/bin:${PATH}" FAKE_WORKSPACE="${workspace}" FAKE_ALONE="${alone}" "$@" "${script}"
}

# go list prints a module once per package, and nothing for a package of the standard library.
same='github.com/pion/rtp v1.10.5\ngithub.com/pion/rtp v1.10.5\n\ngolang.org/x/sys v0.48.0\n'
check 'equal lists: unchanged, each module counted once' 0 --is 'video build list unchanged (2 modules)' -- \
  lists "${same}" "${same}"
check 'the order of the packages does not matter' 0 --is 'video build list unchanged (2 modules)' -- \
  lists 'golang.org/x/sys v0.48.0\ngithub.com/pion/rtp v1.10.5\n' 'github.com/pion/rtp v1.10.5\ngolang.org/x/sys v0.48.0\n'
check 'a newer version in the workspace fails and names the module' 1 \
  --has 'github.com/pion/rtp: v1.10.5 with GOWORK=off, v1.10.6 in the workspace' --lacks 'golang.org/x/sys' -- \
  lists 'github.com/pion/rtp v1.10.6\ngolang.org/x/sys v0.48.0\n' "${same}"
check 'a module only in the workspace fails and names it' 1 \
  --has 'golang.org/x/time: none with GOWORK=off, v0.14.0 in the workspace' -- \
  lists "${same}golang.org/x/time v0.14.0\n" "${same}"
check 'a module only with GOWORK=off fails and names it' 1 \
  --has 'golang.org/x/time: v0.14.0 with GOWORK=off, none in the workspace' -- \
  lists "${same}" "${same}golang.org/x/time v0.14.0\n"
check 'a failing go list fails the check' 1 --has 'go: updates to go.mod needed' -- \
  lists "${same}" "${same}" FAKE_GO_FAILS=1
check 'GOWORK=off in the environment still compares with the workspace' 1 \
  --has 'golang.org/x/time: none with GOWORK=off, v0.14.0 in the workspace' -- \
  lists "${same}golang.org/x/time v0.14.0\n" "${same}" GOWORK=off
check 'an argument is a usage error' 2 --has usage -- \
  env PATH="${tmp}/bin:${PATH}" FAKE_WORKSPACE="${same}" FAKE_ALONE="${same}" "${script}" extra

finish check-video-build-list.test.sh
