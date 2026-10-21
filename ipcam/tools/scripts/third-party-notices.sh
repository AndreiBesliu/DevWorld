#!/usr/bin/env bash
# Prints THIRD_PARTY_NOTICES.txt for the video spike release: the license
# texts of what spikehub and the spike app redistribute.
#   - Go: the standard library and runtime, and every module that go-licenses
#     (pinned in tools/go.mod) finds in spikehub's build for Windows (amd64)
#     and Linux (arm64, arm), the release's three targets.
#   - npm: every package that the spike app depends on at run time (its
#     production dependencies, as pnpm lists them), which includes every
#     package bundled into it, and the NOTICE files of this repository's own
#     packages among them (the player's carries video-rtc.js's MIT notice).
# The Android libraries of the APK and the Rust crates of ipcam-desktop are
# not listed; the release notes say so.
#
#   tools/scripts/third-party-notices.sh > THIRD_PARTY_NOTICES.txt
#
# Run it after `pnpm install` and with the Go modules downloadable. Exits 1
# when a module or package has no license file, or a tool fails; 2 on a usage
# error.
set -euo pipefail

usage() {
  echo "usage: $0 > THIRD_PARTY_NOTICES.txt" >&2
  exit 2
}
[[ $# -eq 0 ]] || usage

ipcam_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ipcam_dir}"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

own_module_prefix="github.com/AndreiBesliu/DevWorld/ipcam"
spikehub=./tools/spikes/video/cmd/spikehub
rule="$(printf '=%.0s' {1..80})"
thin="$(printf -- '-%.0s' {1..80})"
problems=()

# go-licenses runs on this machine and reads the packages of each target, so
# it is built for this machine first (`go tool` would build it for GOOS).
go build -o "${tmp}/go-licenses" github.com/google/go-licenses/v2
# One line per module and license: name, version, license name, license file.
printf '{{range .}}{{.Name}}\t{{.Version}}\t{{.LicenseName}}\t{{.LicensePath}}\n{{end}}' > "${tmp}/report.tpl"
for target in windows/amd64/ linux/arm64/ linux/arm/7; do
  IFS=/ read -r goos goarch goarm <<< "${target}"
  GOOS="${goos}" GOARCH="${goarch}" GOARM="${goarm}" CGO_ENABLED=0 \
    "${tmp}/go-licenses" report --template "${tmp}/report.tpl" --ignore "${own_module_prefix}" "${spikehub}" \
    >> "${tmp}/go.tsv"
done
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
