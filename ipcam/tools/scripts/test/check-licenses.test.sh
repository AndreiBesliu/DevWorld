#!/usr/bin/env bash
# Tests of tools/scripts/check-licenses.sh with a fake `go`: `go tool
# go-licenses` and the go-licenses binary that `go build` writes both print
# where they ran, with which GOWORK, GOTOOLCHAIN and arguments, instead of
# checking.
#
#   tools/scripts/test/check-licenses.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../check-licenses.sh"
ipcam_dir="$(cd "${here}/../../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

mkdir -p "${tmp}/bin" "${tmp}/fork/sub"
cat > "${tmp}/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'env GOVERSION') echo go1.27.2 ;;
  'tool go-licenses check '*)
    shift 2
    echo "go-licenses ran in ${PWD} with GOWORK=${GOWORK-unset} GOTOOLCHAIN=${GOTOOLCHAIN-unset}: $*"
    [[ -z "${FAKE_GO_LICENSES_FAILS:-}" ]] || { echo 'Forbidden license type MPL-1.1' >&2; exit 3; }
    ;;
  'build -o '*' github.com/google/go-licenses/v2')
    # The fake binary also reports where and how it was built.
    cat > "$3" <<TOOL
#!/usr/bin/env bash
echo "go-licenses built in ${PWD} with GOWORK=${GOWORK-unset} GOTOOLCHAIN=${GOTOOLCHAIN-unset}"
echo "go-licenses ran in \${PWD} with GOWORK=\${GOWORK-unset} GOTOOLCHAIN=\${GOTOOLCHAIN-unset}: \$*"
[[ -z "\${FAKE_GO_LICENSES_FAILS:-}" ]] || { echo 'Forbidden license type MPL-1.1' >&2; exit 3; }
TOOL
    chmod +x "$3"
    ;;
  *) echo "fake go: unexpected arguments: $*" >&2; exit 2 ;;
esac
EOF
chmod +x "${tmp}/bin/go"

# licenses [<NAME=value>...] -- <argument>...: runs the script from ${tmp}/fork with the fake go.
licenses() {
  local vars=()
  while [[ "$1" != -- ]]; do
    vars+=("$1")
    shift
  done
  shift
  (cd "${tmp}/fork" && env -u GOWORK -u GOTOOLCHAIN PATH="${tmp}/bin:${PATH}" ${vars[@]+"${vars[@]}"} "${script}" "$@")
}

allowed='--allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,ISC,MPL-2.0'
ignore='--ignore github.com/AndreiBesliu/DevWorld/ipcam --ignore github.com/benburkert/openpgp/aes/keywrap'

check 'without -C: the video spike, in the ipcam directory, in the workspace' 0 \
  --is "go-licenses ran in ${ipcam_dir} with GOWORK=unset GOTOOLCHAIN=unset: check ${allowed} ${ignore} ./tools/spikes/video/..." -- \
  licenses --
check 'without -C: the given packages' 0 \
  --is "go-licenses ran in ${ipcam_dir} with GOWORK=unset GOTOOLCHAIN=unset: check ${allowed} ${ignore} ./hub/... ./tools/spikes/recording/..." -- \
  licenses -- ./hub/... ./tools/spikes/recording/...
check 'with -C: a binary built from tools/go.mod with its Go runs in the directory with GOWORK=off' 0 \
  --is "go-licenses built in ${ipcam_dir}/tools with GOWORK=off GOTOOLCHAIN=go1.27.2
go-licenses ran in ${tmp}/fork/sub with GOWORK=off GOTOOLCHAIN=go1.27.2: check ${allowed} ${ignore} ./..." -- \
  licenses -- -C "${tmp}/fork/sub" ./...
check 'with -C: a relative directory is read from where the script runs' 0 \
  --has "go-licenses ran in ${tmp}/fork/sub with GOWORK=off GOTOOLCHAIN=go1.27.2: check ${allowed} ${ignore} ./..." -- \
  licenses -- -C sub ./...
check 'with -C and no packages: all packages of the directory' 0 \
  --has "go-licenses ran in ${tmp}/fork/sub with GOWORK=off GOTOOLCHAIN=go1.27.2: check ${allowed} ${ignore} ./..." -- \
  licenses -- -C sub
check 'with -C: a forbidden license fails the check' 3 --has 'Forbidden license type MPL-1.1' -- \
  licenses FAKE_GO_LICENSES_FAILS=1 -- -C sub ./...
check 'without -C: a forbidden license fails the check' 3 --has 'Forbidden license type MPL-1.1' -- \
  licenses FAKE_GO_LICENSES_FAILS=1 --
check 'a missing directory fails' 1 --has 'check-licenses: no directory missing' -- licenses -- -C missing ./...
check '-C without a directory is a usage error' 2 --has usage -- licenses -- -C

finish check-licenses.test.sh
