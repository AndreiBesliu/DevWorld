#!/usr/bin/env bash
# Vendors a tagged MediaMTX release into hub/third_party/mediamtx as the nested
# module github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx, and
# applies patch MTX-0 (hub/third_party/PATCHES.md):
#   - downloads the module with `go mod download`, which checks it against the
#     Go checksum database;
#   - replaces the upstream tree there (main.go, internal/, LICENSE, go.mod,
#     go.sum) with the release's; SOURCE.md and the patch tests stay;
#   - removes the rpiCamera static source (R05);
#   - rewrites the module path and every import of it, and points the hub
#     module at ../.. for the patches that import hub/internal;
#   - writes the outputs of MediaMTX's go:generate steps: hls.min.js from the
#     hls.js release that hlsjsdownloader pins (checked against its HASH), and
#     internal/core/VERSION;
#   - prints the values that SOURCE.md records.
#
#   tools/scripts/vendor-mediamtx.sh <version>      e.g. v1.21.1
#
# Then run `GOWORK=off go mod tidy` in hub/third_party/mediamtx, redo the
# dependency changes that hub/third_party/PATCHES.md lists, update SOURCE.md
# and re-apply the patches MTX-1… (this script overwrites all of them).
# Exits 1 when a step fails, 2 on a usage error.
set -euo pipefail

usage() {
  echo "usage: $0 <version>   (a MediaMTX release tag, e.g. v1.21.1)" >&2
  exit 2
}
[[ $# -eq 1 && "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
version="$1"

upstream=github.com/bluenviron/mediamtx
fork=github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx
ipcam_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dest="${ipcam_dir}/hub/third_party/mediamtx"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# The workspace's Go, also outside it, rather than whatever toolchain the
# release's go.mod would switch to.
GOTOOLCHAIN="$(cd "${ipcam_dir}" && go env GOVERSION)"
export GOTOOLCHAIN

fail() {
  echo "vendor-mediamtx: $*" >&2
  exit 1
}

# 1. The module, as the Go proxy serves it and the checksum database knows it.
info="$(cd "${tmp}" && GOWORK=off GOFLAGS='' go mod download -json "${upstream}@${version}")"
src="$(jq -r .Dir <<< "${info}")"
zip="$(jq -r .Zip <<< "${info}")"
commit="$(jq -r '.Origin.Hash // empty' <<< "${info}")"
[[ "$(jq -r '.Origin.Ref // empty' <<< "${info}")" == "refs/tags/${version}" && -n "${commit}" ]] ||
  fail "the Go proxy names no tag ${version} with a commit for ${upstream}"
zip_sha256="$(sha256sum "${zip}" | cut -d' ' -f1)"

# 2. The tree. Upstream has no other Go file at its root than main.go; a new
# one would need a decision, so it stops the script.
mapfile -t root_go < <(cd "${src}" && find . -maxdepth 1 -name '*.go' ! -name main.go)
[[ ${#root_go[@]} -eq 0 ]] || fail "upstream has Go files at its root besides main.go: ${root_go[*]}"
mkdir -p "${dest}"
rm -rf "${dest:?}/internal" "${dest}/main.go" "${dest}/LICENSE" "${dest}/go.mod" "${dest}/go.sum"
cp -R "${src}/internal" "${src}/main.go" "${src}/LICENSE" "${src}/go.mod" "${src}/go.sum" "${dest}/"
chmod -R u+w "${dest}"
cd "${dest}"

# 3. MTX-0: no rpiCamera source. Its package goes; the static source handler
# loses its import and its case; the configuration rejects the source.
# cut_case <file> <case line> [<replacement>]: replaces the switch case that
# starts at the given line, up to the next case or default of that switch.
cut_case() {
  local file="$1" start="$2" replacement="${3:-}"
  [[ "$(grep -cxF -- "${start}" "${file}")" -eq 1 ]] || fail "${file}: expected one line '${start}'"
  awk -v start="${start}" -v replacement="${replacement}" '
    $0 == start { skipping = 1; if (replacement != "") print replacement; next }
    skipping && /^\t(case |default:)/ { skipping = 0 }
    !skipping { print }
    END { exit skipping }
  ' "${file}" > "${tmp}/cut" || fail "${file}: no case or default after '${start}'"
  cat "${tmp}/cut" > "${file}"
}
rm -r internal/staticsources/rpicamera
handler=internal/staticsources/handler.go
[[ "$(grep -c '^	ssrpicamera "' "${handler}")" -eq 1 ]] || fail "${handler}: expected one ssrpicamera import"
sed -i '/^	ssrpicamera "/d' "${handler}"
cut_case "${handler}" $'\tcase s.Conf.Source == "rpiCamera":'
cut_case internal/conf/path.go $'\tcase pconf.Source == "rpiCamera":' \
  $'\tcase pconf.Source == "rpiCamera":\n\t\treturn fmt.Errorf("source \'rpiCamera\' is not supported: this build has no Raspberry Pi camera source")\n'

# 4. MTX-0: the module path. Every quoted import path (and apidocsgen's
# package prefix) changes; URLs of the upstream project do not.
grep -rlZF --include='*.go' "\"${upstream}/" . | xargs -0 sed -i "s|\"${upstream}/|\"${fork}/|g"
GOWORK=off GOFLAGS='' go mod edit -module "${fork}" -replace "github.com/AndreiBesliu/DevWorld/ipcam/hub=../.."

# 5. MTX-0: the go:generate outputs, which the module zip does not contain.
hls=internal/servers/hls
hlsjs_version="$(tr -d '[:space:]' < "${hls}/hlsjsdownloader/VERSION")"
hlsjs_hash="$(tr -d '[:space:]' < "${hls}/hlsjsdownloader/HASH")"
curl -fsSL --retry 3 -o "${tmp}/hlsjs.zip" \
  "https://github.com/video-dev/hls.js/releases/download/${hlsjs_version}/release.zip"
echo "${hlsjs_hash}  ${tmp}/hlsjs.zip" | sha256sum --check --strict --quiet ||
  fail "hls.js ${hlsjs_version}: release.zip does not match ${hls}/hlsjsdownloader/HASH"
unzip -p "${tmp}/hlsjs.zip" dist/hls.min.js > "${hls}/hls.min.js"
[[ -s "${hls}/hls.min.js" ]] || fail "hls.js ${hlsjs_version}: release.zip holds no dist/hls.min.js"
# versiongetter writes `git describe --tags`, without a line break.
printf '%s' "${version}" > internal/core/VERSION

cat <<EOF
Vendored ${upstream} ${version} into ${dest}.
- Tag: ${version}
- Commit: ${commit}
- Module zip SHA-256: ${zip_sha256}
- hls.min.js SHA-256: $(sha256sum "${hls}/hls.min.js" | cut -d' ' -f1) (hls.js ${hlsjs_version}, release.zip SHA-256 ${hlsjs_hash})
- VERSION: $(cat internal/core/VERSION)
- Command: tools/scripts/vendor-mediamtx.sh ${version}
Next: GOWORK=off go mod tidy in ${dest}, the dependency changes of hub/third_party/PATCHES.md, then SOURCE.md.
EOF
