#!/usr/bin/env bash
# Tests of tools/scripts/third-party-notices.sh with a fake `go` and a fake
# `pnpm` that answer from testdata/notices: go-licenses' report of spikehub's
# modules for Windows and for Linux, `pnpm list` of the spike app's production
# packages, and the license files of each.
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
    # The fake go-licenses: its report for the GOOS it runs with.
    cat > "$3" <<'TOOL'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == report ]] || { echo "fake go-licenses: unexpected arguments: $*" >&2; exit 2; }
[[ -z "${FAKE_GO_LICENSES_FAILS:-}" ]] || { echo 'go-licenses: cannot load packages' >&2; exit 1; }
sed "s|@DATA@|${FAKE_DATA}|g" "${FAKE_DATA}/go-licenses-${GOOS}.tsv"
TOOL
    chmod +x "$3"
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
  --lacks 'nolicense' --lacks '@ipcam/capacitor-pip' -- notices
out="$(notices 2>/dev/null)"
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
finish third-party-notices.test.sh
