#!/usr/bin/env bash
# Lints the Go trees of ipcam-spike-upload.yml's TREES for GOOS <goos>, each
# module's trees with that module's config: golangci-lint takes the config of
# the first path it analyses, so the trees under ./hub get a run of their own
# with hub/.golangci.yml, and the others one with the upload spike's. A side
# without trees is not run, since golangci-lint with no path lints ./..., every
# module of the workspace.
#
#   TREES='./tools/spikes/upload/... ./hub ./hub/internal/storage/...' tools/scripts/lint-trees.sh windows
#
# The linter is built for this machine (go tool -n builds it and prints its
# path) and run with GOOS=<goos>: `GOOS=windows go tool golangci-lint` would
# build a Windows executable, which Linux cannot run. Both runs go ahead even
# when the first one fails; exits 1 when one failed, 2 on a usage error.
set -euo pipefail

# The trees are split at every space, tab and newline, as the workflow's
# unquoted ${TREES} is, so a YAML literal block (TREES: |) lints every line.
# read -d '' reads to the end and then returns 1, which is no error here.
read -r -d '' -a trees <<< "${TREES:-}" || true
if [[ $# -ne 1 || -z "$1" || ${#trees[@]} -eq 0 ]]; then
  echo "usage: TREES='<tree>...' $0 <goos>" >&2
  exit 2
fi
goos="$1"

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

hub=() upload=()
for tree in "${trees[@]}"; do
  if [[ ${tree} == ./hub* ]]; then hub+=("${tree}"); else upload+=("${tree}"); fi
done

# Without GOOS and GOARCH, even when the caller exported them.
lint="$(env -u GOOS -u GOARCH go tool -n golangci-lint)"
status=0
if ((${#upload[@]})); then
  GOOS="${goos}" "${lint}" run --config tools/spikes/upload/.golangci.yml "${upload[@]}" || status=1
fi
if ((${#hub[@]})); then
  GOOS="${goos}" "${lint}" run --config hub/.golangci.yml "${hub[@]}" || status=1
fi
exit "${status}"
