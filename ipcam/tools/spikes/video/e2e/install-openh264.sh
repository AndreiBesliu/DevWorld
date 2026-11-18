#!/usr/bin/env bash
# Installs Mozilla's build of the OpenH264 media plugin (GMP) for the Firefox of
# the e2e tests and prints the plugin's folder: the value of SPIKE_OPENH264,
# which playwright.config.ts hands to Firefox as MOZ_GMP_PATH. SPIKE-ONLY
# (plan Task 12): used by the test browser alone, never shipped.
#
#   tools/spikes/video/e2e/install-openh264.sh <folder> [<zip>]
#
# <zip> is a copy of the plugin's zip from elsewhere, for a machine that cannot
# reach Cisco's server; it is held to the same size and SHA-512.
#
# Why: spec §14.7 runs Firefox with the OpenH264 plugin preinstalled. Firefox
# offers H.264 for WebRTC only when a media plugin can encode and decode it
# (DefaultCodecPreferences::SoftwareH264EnabledStatic), and Playwright's Firefox
# turns the plugin's download off (media.gmp-manager.updateEnabled = false), so
# without this it offers VP8, VP9 and AV1 only and the hub refuses its sessions.
#
# What: the zip that Firefox itself downloads, OpenH264 2.6.0 for
# Linux_x86_64-gcc3, with the URL, size and SHA-512 of Firefox's
# toolkit/content/gmp-sources/openh264.json (the same on Firefox's release,
# beta, esr140 and esr128 branches on 2026-10-19). Cisco covers the H.264 patent
# licence of the binaries it serves itself, so the zip is fetched from Cisco on
# each run, as Firefox does, and is never committed, cached or uploaded.
#
# Firefox loads a plugin from a folder <any>/gmp-<id>/<version> that holds
# <id>.info and lib<id>.so (GMPParent::Init), so that is the layout made here.
set -euo pipefail

readonly version='2.6.0'
readonly url='https://ciscobinary.openh264.org/openh264-linux64-652bdb7719f30b52b08e506645a7322ff1b2cc6f.zip'
readonly size=589006
readonly sha512='f5246bf14d038adf4ce0c4360262ab722bc3de4220f047c3d542b4c564074b4877dc8659e3125c5171c749e7ce93f20cc63777eb5e1539e960670cbc5f30ac85'

fail() {
  echo "install-openh264.sh: $*" >&2
  exit 1
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <folder> [<zip>]" >&2
  exit 2
fi
folder="$1"
plugin="${folder}/gmp-gmpopenh264/${version}"
zip="${folder}/openh264-${version}.zip"

mkdir -p "${folder}"
if [[ $# -eq 2 ]]; then
  cp "$2" "${zip}"
else
  curl --fail --silent --show-error --location --retry 3 --output "${zip}" "${url}"
fi

actual_size="$(stat -c %s "${zip}")"
[[ "${actual_size}" -eq "${size}" ]] || fail "the zip has ${actual_size} bytes, want ${size} (${url})"
actual_sha512="$(sha512sum "${zip}" | cut -d ' ' -f 1)"
[[ "${actual_sha512}" == "${sha512}" ]] || fail "the zip's SHA-512 is ${actual_sha512}, want ${sha512} (${url})"

rm -rf "${plugin}"
mkdir -p "${plugin}"
unzip -q "${zip}" -d "${plugin}"
rm "${zip}"
for file in gmpopenh264.info libgmpopenh264.so; do
  [[ -f "${plugin}/${file}" ]] || fail "the zip has no ${file}; it holds: $(cd "${plugin}" && find . -mindepth 1 | tr '\n' ' ')"
done

echo "OpenH264 ${version} installed in ${plugin}:" >&2
sed 's/^/  /' "${plugin}/gmpopenh264.info" >&2
echo "${plugin}"
