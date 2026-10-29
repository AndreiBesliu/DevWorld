#!/usr/bin/env bash
# Prints THIRD_PARTY_NOTICES.txt for a spike release: the license texts of
# what its programs and the spike app redistribute.
#   - Go: the standard library and runtime, and every module that go-licenses
#     (pinned in tools/go.mod) finds in the release's Go binaries, for each of
#     their targets: Windows (amd64) and Linux (arm64, arm with GOARM=7).
#     - video (the default): spikehub.
#     - recording: ipcam-hub, recspike and reccontroller, and for ARMv6
#       reccontroller and recspike-probe; and mediamtx, the MediaMTX fork in
#       hub/third_party/mediamtx, which is outside the workspace and is read
#       with GOWORK=off. The fork's own MIT license (MediaMTX), hls.js's
#       Apache-2.0 for the hls.min.js that mediamtx embeds, and keywrap's
#       BSD-2-Clause, which is in the header of its source because its
#       module has no license file, are named explicitly (G35, C1-R5): the
#       report skips the first as the repository's own, the second is no Go
#       module, and go-licenses cannot classify the third.
#   - npm: every package that the spike app depends on at run time (its
#     production dependencies, as pnpm lists them), which includes every
#     package bundled into it, and the NOTICE files of this repository's own
#     packages among them (the player's carries video-rtc.js's MIT notice).
# The Android libraries of the APK and the Rust crates of ipcam-desktop are
# not listed; the release notes say so.
#
#   tools/scripts/third-party-notices.sh [--release video|recording] > THIRD_PARTY_NOTICES.txt
#
# Run it after `pnpm install` and with the Go modules downloadable. Exits 1
# when a module or package has no license file, or a tool fails; 2 on a usage
# error.
set -euo pipefail

usage() {
  echo "usage: $0 [--release video|recording] > THIRD_PARTY_NOTICES.txt" >&2
  exit 2
}
release=video
if [[ $# -gt 0 ]]; then
  [[ $# -eq 2 && "$1" == --release && ("$2" == video || "$2" == recording) ]] || usage
  release="$2"
fi

ipcam_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ipcam_dir}"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

own_module_prefix="github.com/AndreiBesliu/DevWorld/ipcam"
rule="$(printf '=%.0s' {1..80})"
thin="$(printf -- '-%.0s' {1..80})"
problems=()
targets=(windows/amd64/ linux/arm64/ linux/arm/7)
# The MediaMTX fork, and keywrap, which the fork reaches through
# datarhei/gosrt (SRT encryption) and which go-licenses cannot classify
# (check-licenses.sh's read_by_hand).
fork=hub/third_party/mediamtx
keywrap_module=github.com/benburkert/openpgp
keywrap_package="${keywrap_module}/aes/keywrap"

# go-licenses runs on this machine and reads the packages of each target, so
# it is built for this machine first (`go tool` would build it for GOOS). It
# tells the standard library apart by the GOROOT it was built with, so the
# fork, whose go.mod names an older Go, is read with the same toolchain.
go build -o "${tmp}/go-licenses" github.com/google/go-licenses/v2
toolchain="$(go env GOVERSION)"
# One line per module and license: name, version, license name, license file.
printf '{{range .}}{{.Name}}\t{{.Version}}\t{{.LicenseName}}\t{{.LicensePath}}\n{{end}}' > "${tmp}/report.tpl"
# report <target> <package>...: go-licenses' lines for the packages built for
# the target (GOOS/GOARCH/GOARM), in the folder where it runs.
report() {
  local goos goarch goarm
  IFS=/ read -r goos goarch goarm <<< "$1"
  shift
  GOOS="${goos}" GOARCH="${goarch}" GOARM="${goarm}" CGO_ENABLED=0 \
    "${tmp}/go-licenses" report --template "${tmp}/report.tpl" --ignore "${own_module_prefix}" --ignore "${keywrap_package}" "$@"
}
if [[ "${release}" == video ]]; then
  for target in "${targets[@]}"; do
    report "${target}" ./tools/spikes/video/cmd/spikehub >> "${tmp}/go.tsv"
  done
else
  for target in "${targets[@]}"; do
    report "${target}" ./hub/cmd/ipcam-hub ./tools/spikes/recording/cmd/recspike ./tools/spikes/recording/cmd/reccontroller >> "${tmp}/go.tsv"
  done
  report linux/arm/6 ./tools/spikes/recording/cmd/reccontroller ./tools/spikes/recording/cmd/recspike-probe >> "${tmp}/go.tsv"
  for target in "${targets[@]}"; do
    (cd "${fork}" && GOWORK=off GOTOOLCHAIN="${toolchain}" report "${target}" .) >> "${tmp}/go.tsv"
  done
  keywrap="$(cd "${fork}" && GOWORK=off GOTOOLCHAIN="${toolchain}" go mod download -json "${keywrap_module}" | jq -r '[.Version, .Dir] | @tsv')"
  IFS=$'\t' read -r keywrap_version keywrap_dir <<< "${keywrap}"
  # Its license is the comment above keywrap.go's package clause.
  awk '/^\/\// { sub(/^\/\/ ?/, ""); print; next } { exit }' "${keywrap_dir}/aes/keywrap/keywrap.go" > "${tmp}/keywrap-license"
fi
# One line per module: name, version, license names (sorted, comma-separated), license file.
go_modules="$(awk -F'\t' 'NF >= 4 {
    key = $1 "\t" $2 "\t" $4
    if (!(key in names)) { order[++n] = key; names[key] = "" }
    if (index("," names[key] ",", "," $3 ",") == 0) names[key] = names[key] (names[key] == "" ? "" : ",") $3
  }
  END { for (i = 1; i <= n; i++) print order[i] "\t" names[order[i]] }' "${tmp}/go.tsv" |
  LC_ALL=C sort -t$'\t' -k1,1 -k2,2)"

# The npm packages: name, version, folder; a version "link:…" is a package of this repository.
npm_packages="$(pnpm --filter spike-video-app list --prod --depth Infinity --json |
  jq -r 'def deps: (.dependencies // {}) | to_entries[] | (.value + {name: .key}) | (., deps);
    [.[] | deps] | unique_by([.name, .version]) | .[] | [.name, .version, .path] | @tsv' |
  LC_ALL=C sort -t$'\t' -k1,1 -k2,2)"

# entry <heading> <license names> <file>...: one notice, each file under its name when there are several.
entry() {
  local heading="$1" names="$2" file
  shift 2
  printf '\n%s\n%s\n' "${rule}" "${heading}"
  [[ -z "${names}" ]] || printf 'License: %s\n' "${names}"
  printf '%s\n' "${thin}"
  for file in "$@"; do
    [[ $# -eq 1 ]] || printf '[%s]\n' "$(basename "${file}")"
    cat "${file}"
    [[ -z "$(tail -c1 "${file}")" ]] || echo
  done
}

{
  if [[ "${release}" == video ]]; then
    cat <<'EOF'
Licențele componentelor terțe din spikehub și din aplicația de test a probei video (Faza 0 b).
Fișierul le păstrează pentru conformitate; nu trebuie să faceți nimic cu el.

Third-party notices for spikehub (its Windows and Linux builds) and for the spike
app that spikehub serves and that ipcam-spike.apk and ipcam-desktop-setup.exe
embed: the Go standard library and the Go modules compiled into spikehub, and the
npm packages that the app depends on at run time (its production dependencies,
which include every package bundled into it), each with its license text. The
Android libraries of the APK and the Rust crates of ipcam-desktop are not listed.
EOF
  else
    cat <<'EOF'
Licențele componentelor terțe din programele probei de înregistrare (Faza 0 c1) și din aplicația de test.
Fișierul le păstrează pentru conformitate; nu trebuie să faceți nimic cu el.

Third-party notices for the recording spike's programs, in their Windows and
Linux builds: recspike, reccontroller, ipcam-hub, recspike-probe and mediamtx,
the MediaMTX fork; and for the spike app that ipcam-spike.apk embeds. They hold
the Go standard library and the Go modules compiled into each program, MediaMTX
itself, the hls.js that mediamtx embeds, and the npm packages that the app
depends on at run time (its production dependencies, which include every
package bundled into it), each with its license text. The Android libraries of
the APK are not listed.
EOF
  fi

  goroot="$(go env GOROOT)"
  entry "Go: the standard library and runtime ($(go env GOVERSION))" BSD-3-Clause "${goroot}/LICENSE"

  while IFS=$'\t' read -r name version path names; do
    [[ -n "${name}" ]] || continue
    if [[ "${path}" == Unknown || ! -f "${path}" ]]; then
      problems+=("${name} ${version}: no license file (go-licenses found none)")
      continue
    fi
    entry "Go module: ${name} ${version}" "${names//,/, }" "${path}"
  done <<< "${go_modules}"

  if [[ "${release}" == recording ]]; then
    if [[ -s "${tmp}/keywrap-license" ]]; then
      entry "Go module: ${keywrap_module} ${keywrap_version}, its package aes/keywrap" BSD-2-Clause "${tmp}/keywrap-license"
    else
      problems+=("${keywrap_package} ${keywrap_version}: no license in the header of keywrap.go")
    fi
    entry "MediaMTX $(cat "${fork}/internal/core/VERSION"), the fork that mediamtx is built from (${fork})" MIT "${fork}/LICENSE"
    # The license text of the hls.js that mediamtx embeds is the npm
    # package's of the same version, which the app bundles too.
    hlsjs_version="$(tr -d '[:space:]' < "${fork}/internal/servers/hls/hlsjsdownloader/VERSION")"
    hlsjs_path="$(awk -F'\t' -v version="${hlsjs_version#v}" '$1 == "hls.js" && $2 == version { print $3; exit }' <<< "${npm_packages}")"
    if [[ -n "${hlsjs_path}" && -f "${hlsjs_path}/LICENSE" ]]; then
      entry "hls.js ${hlsjs_version}, embedded in mediamtx as hls.min.js" Apache-2.0 "${hlsjs_path}/LICENSE"
    else
      problems+=("hls.js ${hlsjs_version}: no npm package of this version whose license to quote for the hls.min.js in mediamtx")
    fi
  fi

  while IFS=$'\t' read -r name version path; do
    [[ -n "${name}" ]] || continue
    mapfile -t files < <(find "${path}" -maxdepth 1 -type f \
      \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' \) | LC_ALL=C sort)
    if [[ "${version}" == link:* ]]; then
      mapfile -t notices < <(find "${path}" -maxdepth 1 -type f -iname 'notice*' | LC_ALL=C sort)
      [[ ${#notices[@]} -eq 0 ]] || entry "Notices of ${name} (part of this repository)" "" "${notices[@]}"
      continue
    fi
    if [[ ${#files[@]} -eq 0 ]]; then
      problems+=("${name} ${version}: no license file in ${path}")
      continue
    fi
    license="$(jq -r '.license // ([.licenses[]?.type] | join(", ")) // ""' "${path}/package.json" 2>/dev/null || true)"
    entry "npm package: ${name} ${version}" "${license}" "${files[@]}"
  done <<< "${npm_packages}"

  if [[ ${#problems[@]} -gt 0 ]]; then
    printf '%s\n' "${problems[@]}" >&2
    echo "third-party-notices: the modules and packages above have no license file to quote" >&2
    exit 1
  fi
}
