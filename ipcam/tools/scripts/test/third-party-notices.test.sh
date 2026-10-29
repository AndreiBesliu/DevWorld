#!/usr/bin/env bash
# Tests of tools/scripts/third-party-notices.sh with a fake `go` and a fake
# `pnpm` that answer from testdata/notices: go-licenses' report of the
# workspace's binaries (spikehub; ipcam-hub, recspike, reccontroller and
# recspike-probe) and of the MediaMTX fork's for Windows and for Linux, the
# fork's keywrap module, `pnpm list` of the spike app's production packages,
# and the license files of each. The fork's own LICENSE and versions are the
# repository's.
#
#   tools/scripts/test/third-party-notices.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../third-party-notices.sh"
data="${here}/testdata/notices"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

# The fakes read their answers from FAKE_DATA, with @DATA@ standing for it.
# The fake go builds a fake go-licenses, which answers for the GOOS it runs with.
mkdir -p "${tmp}/bin"
cat > "${tmp}/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'env GOROOT') echo "${FAKE_DATA}/goroot" ;;
  'env GOVERSION') echo go1.27.2 ;;
  'build -o '*' github.com/google/go-licenses/v2')
    # The fake go-licenses: its report for the GOOS it runs with, of the
    # fork when it runs in the fork's folder, which needs GOWORK=off; each
    # call goes to FAKE_CALLS as "<folder> <GOOS>/<GOARCH>/<GOARM> <GOWORK> <packages>".
    cat > "$3" <<'TOOL'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == report ]] || { echo "fake go-licenses: unexpected arguments: $*" >&2; exit 2; }
[[ -z "${FAKE_GO_LICENSES_FAILS:-}" ]] || { echo 'go-licenses: cannot load packages' >&2; exit 1; }
folder="$(basename "${PWD}")"
packages=()
for arg in "$@"; do
  [[ "${arg}" == ./* || "${arg}" == . ]] && packages+=("${arg}")
done
echo "${folder} ${GOOS}/${GOARCH}/${GOARM:-} ${GOWORK:-on} ${packages[*]}" >> "${FAKE_CALLS:-/dev/null}"
report="go-licenses-${GOOS}.tsv"
if [[ "${folder}" == mediamtx ]]; then
  [[ "${GOWORK:-}" == off ]] || { echo 'fake go-licenses: the fork runs with GOWORK=off' >&2; exit 2; }
  report="go-licenses-fork-${GOOS}.tsv"
fi
sed "s|@DATA@|${FAKE_DATA}|g" "${FAKE_DATA}/${report}"
TOOL
    chmod +x "$3"
    ;;
  'mod download -json github.com/benburkert/openpgp')
    [[ "$(basename "${PWD}")" == mediamtx && "${GOWORK:-}" == off ]] ||
      { echo 'fake go: the fork module downloads in its folder with GOWORK=off' >&2; exit 2; }
    printf '{"Path":"github.com/benburkert/openpgp","Version":"v0.0.0-20160410205803-c2471f86866c","Dir":"%s/modules/openpgp"}\n' "${FAKE_DATA}"
    ;;
  *) echo "fake go: unexpected arguments: $*" >&2; exit 2 ;;
esac
EOF
cat > "${tmp}/bin/pnpm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == '--filter spike-video-app list --prod --depth Infinity --json' ]] ||
  { echo "fake pnpm: unexpected arguments: $*" >&2; exit 2; }
sed "s|@DATA@|${FAKE_DATA}|g" "${FAKE_LIST:-${FAKE_DATA}/pnpm-list.json}"
EOF
chmod +x "${tmp}/bin/go" "${tmp}/bin/pnpm"

# notices [<NAME=value>...]: runs the script with the fakes and those variables
# (check passes them, which shellcheck cannot see).
# shellcheck disable=SC2120
notices() { env PATH="${tmp}/bin:${PATH}" FAKE_DATA="${data}" "$@" "${script}"; }
# notices_of <release> [<NAME=value>...]: the script for that release.
notices_of() {
  local release="$1"
  shift
  env PATH="${tmp}/bin:${PATH}" FAKE_DATA="${data}" "$@" "${script}" --release "${release}"
}
# count <pattern> <text>: how many lines of the text start with the pattern.
count() { grep -c -- "^$1" <<< "$2"; }

check 'the notices hold Go, each Go module once, each npm package and the workspace notices' 0 \
  --has 'Go: the standard library and runtime (go1.27.2)' \
  --has 'The Go license text (fixture).' \
  --has $'Go module: github.com/pion/webrtc/v4 v4.1.6\nLicense: MIT' \
  --has 'MIT License'$'\n''Copyright (c) 2023 The Pion community (fixture).' \
  --has $'Go module: golang.org/x/sys v0.48.0\nLicense: BSD-3-Clause' \
  --has $'Go module: example.com/winonly v1.0.0\nLicense: Apache-2.0, MIT' \
  --has 'The license of a module that only the Windows build compiles (fixture).' \
  --has $'npm package: hls.js 1.7.3\nLicense: Apache-2.0' \
  --has 'Apache License 2.0 of hls.js (fixture).' \
  --has $'npm package: @tauri-apps/api 2.12.1\nLicense: Apache-2.0 OR MIT' \
  --has 'Apache License 2.0 of @tauri-apps/api (fixture).' \
  --has 'MIT License of @tauri-apps/api (fixture).' \
  --has 'Notices of @ipcam/player (part of this repository)' \
  --has 'video-rtc.js (MIT, fixture).' \
  --has 'npm package: @noble/ciphers 2.4.0' \
  --lacks 'nolicense' --lacks '@ipcam/capacitor-pip' --lacks 'MediaMTX' --lacks 'gosrt' --lacks 'benburkert' -- notices
out="$(notices 2>/dev/null)"
check 'the video release is the default' 0 --is "${out}" -- notices_of video
check 'a module that two builds compile is listed once' 0 --is 1 -- count 'Go module: github.com/pion/webrtc/v4 ' "${out}"
check 'a package that two packages depend on is listed once' 0 --is 1 -- count 'npm package: hls.js ' "${out}"

sed 's|path": "@DATA@/npm/hls.js"|path": "@DATA@/npm/nolicense"|' "${data}/pnpm-list.json" > "${tmp}/no-license-list.json"
check 'an npm package without a license file fails' 1 --has 'hls.js 1.7.3: no license file in' -- \
  notices FAKE_LIST="${tmp}/no-license-list.json"

mkdir -p "${tmp}/data"
cp -R "${data}/." "${tmp}/data/"
printf 'example.com/nolicense\tv0.1.0\tUnknown\tUnknown\n' >> "${tmp}/data/go-licenses-linux.tsv"
check 'a Go module without a license file fails' 1 --has 'example.com/nolicense v0.1.0: no license file' -- \
  env PATH="${tmp}/bin:${PATH}" FAKE_DATA="${tmp}/data" "${script}"

check 'a failing go-licenses fails the notices' 1 --has 'go-licenses: cannot load packages' -- \
  notices FAKE_GO_LICENSES_FAILS=1

check 'an argument is a usage error' 2 --has usage -- env PATH="${tmp}/bin:${PATH}" FAKE_DATA="${data}" "${script}" extra
check 'an unknown release is a usage error' 2 --has usage -- notices_of audio

# The recording release: the Go modules of ipcam-hub, recspike, reccontroller
# and recspike-probe, the fork's with GOWORK=off, MediaMTX's own MIT license,
# hls.js's Apache-2.0 for the hls.min.js embedded in mediamtx, keywrap's
# BSD-2-Clause header (C1-R5), and the app's npm packages, @ipcam/core's
# @noble/* among them (G35).
fork_version="$(cat "${here}/../../../hub/third_party/mediamtx/internal/core/VERSION")"
check 'the recording notices hold the modules of every binary and of the fork, MediaMTX, hls.js, keywrap and the app' 0 \
  --has 'Go: the standard library and runtime (go1.27.2)' \
  --has $'Go module: github.com/pion/webrtc/v4 v4.1.6\nLicense: MIT' \
  --has $'Go module: github.com/datarhei/gosrt v0.11.1\nLicense: MIT' \
  --has 'Copyright (c) 2020 FOSS GmbH (gosrt, fixture).' \
  --has "MediaMTX ${fork_version}, the fork that mediamtx is built from (hub/third_party/mediamtx)"$'\nLicense: MIT' \
  --has 'Copyright (c) 2019 aler9' \
  --has $'hls.js v1.7.3, embedded in mediamtx as hls.min.js\nLicense: Apache-2.0' \
  --has $'Go module: github.com/benburkert/openpgp v0.0.0-20160410205803-c2471f86866c, its package aes/keywrap\nLicense: BSD-2-Clause' \
  --has $'Copyright 2014 Matthew Endsley\nAll rights reserved' \
  --has 'The BSD-2-Clause conditions of keywrap.go (fixture).' \
  --lacks '// Copyright' --lacks 'package keywrap' \
  --has $'npm package: hls.js 1.7.3\nLicense: Apache-2.0' \
  --has $'npm package: @noble/ciphers 2.4.0\nLicense: MIT' \
  --has 'Notices of @ipcam/player (part of this repository)' \
  --lacks 'spikehub' -- notices_of recording FAKE_CALLS="${tmp}/calls"
bins='./hub/cmd/ipcam-hub ./tools/spikes/recording/cmd/recspike ./tools/spikes/recording/cmd/reccontroller'
armv6='./tools/spikes/recording/cmd/reccontroller ./tools/spikes/recording/cmd/recspike-probe'
check 'the recording notices read the binaries of each target, and the fork with GOWORK=off' 0 --is "ipcam windows/amd64/ on ${bins}
ipcam linux/arm64/ on ${bins}
ipcam linux/arm/7 on ${bins}
ipcam linux/arm/6 on ${armv6}
mediamtx windows/amd64/ off .
mediamtx linux/arm64/ off .
mediamtx linux/arm/7 off ." -- cat "${tmp}/calls"
finish third-party-notices.test.sh
