#!/usr/bin/env bash
# Tests of tools/scripts/lint-trees.sh with a fake `go`, which answers
# `go tool -n golangci-lint` with the path of a fake linter, and that fake
# linter, which logs each run (its GOOS, its directory and its arguments).
#
#   tools/scripts/test/lint-trees.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../lint-trees.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

# The fake go builds the linter only for this machine: with GOOS or GOARCH set
# it would build one that this machine may not run.
mkdir -p "${tmp}/bin" "${tmp}/tools"
cat > "${tmp}/bin/go" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'tool -n golangci-lint' ]] || { echo "fake go: unexpected arguments: $*" >&2; exit 2; }
[[ -z "${GOOS:-}${GOARCH:-}" ]] || { echo "fake go: a linter for GOOS=${GOOS:-} GOARCH=${GOARCH:-}" >&2; exit 2; }
[[ -z "${FAKE_GO_FAILS:-}" ]] || { echo 'fake go: build failed' >&2; exit 1; }
echo "${FAKE_TOOLS}/golangci-lint"
FAKE
cat > "${tmp}/tools/golangci-lint" <<'FAKE'
#!/usr/bin/env bash
printf '%s in %s: %s\n' "${GOOS:-}" "$(basename "${PWD}")" "$*" >> "${FAKE_RUNS}"
if [[ -n "${FAKE_FAIL:-}" && "$*" == *"${FAKE_FAIL}"* ]]; then
  echo "fake lint: issues under ${FAKE_FAIL}"
  exit 1
fi
FAKE
chmod +x "${tmp}/bin/go" "${tmp}/tools/golangci-lint"

# lints <TREES> [<NAME=value>...] -- [<argument>...]: runs the script with the
# fake tools from another directory, then prints the linter runs it made.
lints() {
  local trees="$1" vars=() code=0
  shift
  while [[ "$1" != -- ]]; do
    vars+=("$1")
    shift
  done
  shift
  : > "${tmp}/runs"
  (cd "${tmp}" && env PATH="${tmp}/bin:${PATH}" FAKE_TOOLS="${tmp}/tools" FAKE_RUNS="${tmp}/runs" TREES="${trees}" \
    ${vars[@]+"${vars[@]}"} "${script}" "$@") || code=$?
  cat "${tmp}/runs"
  return "${code}"
}

upload_run='run --config tools/spikes/upload/.golangci.yml'
hub_run='run --config hub/.golangci.yml'
both='./tools/spikes/upload/... ./hub ./hub/internal/storage/... ./hub/internal/outbox/...'
check 'both sides: one run each, with its config and GOOS, in ipcam' 0 --is \
  "windows in ipcam: ${upload_run} ./tools/spikes/upload/...
windows in ipcam: ${hub_run} ./hub ./hub/internal/storage/... ./hub/internal/outbox/..." -- \
  lints "${both}" -- windows
check 'the target GOOS is passed to the linter' 0 --has "linux in ipcam: ${hub_run} ./hub" -- \
  lints "${both}" -- linux
check 'only hub trees: no run of the upload config, so no ./...' 0 --is "linux in ipcam: ${hub_run} ./hub ./hub/internal/storage/..." -- \
  lints './hub ./hub/internal/storage/...' -- linux
check 'only upload trees: no run of the hub config' 0 --is "linux in ipcam: ${upload_run} ./tools/spikes/upload/..." -- \
  lints './tools/spikes/upload/...' -- linux
check 'extra spaces between the trees change nothing' 0 --is \
  "linux in ipcam: ${upload_run} ./tools/spikes/upload/...
linux in ipcam: ${hub_run} ./hub" -- \
  lints '  ./tools/spikes/upload/...   ./hub ' -- linux
# A YAML literal block (TREES: |) puts the trees on lines of their own, with a
# newline at the end.
check 'trees on lines of their own are all linted' 0 --is \
  "linux in ipcam: ${upload_run} ./tools/spikes/upload/...
linux in ipcam: ${hub_run} ./hub ./hub/internal/storage/... ./hub/internal/outbox/..." -- \
  lints $'./tools/spikes/upload/... ./hub\n./hub/internal/storage/...\n\t./hub/internal/outbox/...\n' -- linux
check 'a failing upload lint fails, and the hub lint still runs' 1 \
  --has 'fake lint: issues under ./tools/spikes/upload/...' --has "${hub_run} ./hub" -- \
  lints "${both}" FAKE_FAIL=./tools/spikes/upload/... -- windows
check 'a failing hub lint fails' 1 --has 'fake lint: issues under ./hub/internal/outbox/...' -- \
  lints "${both}" FAKE_FAIL=./hub/internal/outbox/... -- windows
check 'the linter is built for this machine even with GOOS and GOARCH exported' 0 \
  --has "windows in ipcam: ${hub_run} ./hub" -- \
  lints "${both}" GOOS=linux GOARCH=arm -- windows
check 'a linter that does not build fails, and nothing is linted' 1 --is 'fake go: build failed' -- \
  lints "${both}" FAKE_GO_FAILS=1 -- windows
check 'no GOOS is a usage error' 2 --has usage --lacks 'in ipcam' -- lints "${both}" --
check 'two arguments are a usage error' 2 --has usage -- lints "${both}" -- linux windows
check 'no TREES is a usage error' 2 --has usage --lacks 'in ipcam' -- lints '' -- linux
check 'TREES of spaces only is a usage error' 2 --has usage --lacks 'in ipcam' -- lints '   ' -- linux
check 'TREES of blank lines only is a usage error' 2 --has usage --lacks 'in ipcam' -- lints $'\n \n\t\n' -- linux

finish lint-trees.test.sh
