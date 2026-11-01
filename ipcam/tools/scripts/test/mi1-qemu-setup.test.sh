#!/usr/bin/env bash
# Tests of tools/scripts/mi1-qemu-setup.sh with a fake sudo, apt-get, udevadm
# and qemu-system-x86_64 that log what they were asked, and a file of the
# test's own standing in for /dev/kvm (KVM_DEVICE).
#
#   tools/scripts/test/mi1-qemu-setup.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../mi1-qemu-setup.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

# The fakes log one line per call to FAKE_LOG. sudo runs the command it is
# given, except tee, whose input it keeps in FAKE_LOG.tee instead of /etc.
mkdir -p "${tmp}/bin"
cat > "${tmp}/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "sudo $*" >> "${FAKE_LOG}"
if [[ "$1" == tee ]]; then
  cat > "${FAKE_LOG}.tee"
  exit 0
fi
exec "$@"
EOF
cat > "${tmp}/bin/apt-get" <<'EOF'
#!/usr/bin/env bash
echo "apt-get $*" >> "${FAKE_LOG}"
if [[ "$1" == install && -n "${FAKE_APT_FAILS:-}" ]]; then
  echo "E: Unable to locate package" >&2
  exit 100
fi
EOF
cat > "${tmp}/bin/udevadm" <<'EOF'
#!/usr/bin/env bash
echo "udevadm $*" >> "${FAKE_LOG}"
EOF
cat > "${tmp}/bin/qemu-system-x86_64" <<'EOF'
#!/usr/bin/env bash
echo "qemu-system-x86_64 $*" >> "${FAKE_LOG}"
printf 'QEMU emulator version 8.2.2\nCopyright (c) 2003-2023\n'
EOF
chmod +x "${tmp}/bin/"*

# setup <case> <kvm device> <argument>...: runs the script with the fakes and
# a log of its own, then prints the log after a "--- calls" line.
setup() {
  local log="${tmp}/$1.log" code=0
  shift
  local kvm="$1"
  shift
  : > "${log}"
  KVM_DEVICE="${kvm}" FAKE_LOG="${log}" PATH="${tmp}/bin:${PATH}" "${script}" "$@" || code=$?
  echo "--- calls"
  cat "${log}"
  [[ ! -f "${log}.tee" ]] || { echo "--- udev rule"; cat "${log}.tee"; }
  return "${code}"
}

touch "${tmp}/kvm"
chmod 666 "${tmp}/kvm"

check 'a runner without KVM fails before it installs anything' 1 \
  --has "KVM is not available on this runner (${tmp}/none is missing)" --has 'TST-09' --lacks 'apt-get' -- \
  setup no-kvm "${tmp}/none"
check 'QEMU, cloud-localds and the extra packages are installed, and the runner gets KVM' 0 \
  --has 'sudo apt-get update' \
  --has 'sudo apt-get install --yes --no-install-recommends qemu-system-x86 qemu-utils cloud-image-utils ffmpeg' \
  --has 'sudo tee /etc/udev/rules.d/99-kvm4all.rules' \
  --has 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' \
  --has $'udevadm control --reload-rules\nsudo udevadm trigger --name-match=kvm\nudevadm trigger --name-match=kvm\nsudo udevadm settle' \
  --has $'QEMU emulator version 8.2.2\n--- calls' --lacks 'Copyright' -- \
  setup ok "${tmp}/kvm" ffmpeg
check 'no extra package installs QEMU and cloud-localds alone' 0 \
  --has $'install --yes --no-install-recommends qemu-system-x86 qemu-utils cloud-image-utils\n' -- \
  setup no-extra "${tmp}/kvm"
FAKE_APT_FAILS=1 check 'a failed install stops the setup' 100 --has 'Unable to locate package' --lacks 'udev' -- \
  setup apt-fails "${tmp}/kvm"
# root reads and writes every file, whatever its mode: this case needs a
# user of its own, as on GitHub's runners.
if [[ $(id -u) -ne 0 ]]; then
  touch "${tmp}/kvm-unusable"
  chmod 000 "${tmp}/kvm-unusable"
  check 'a KVM device the runner cannot use fails' 1 --has 'KVM is not usable by this runner' -- \
    setup unusable "${tmp}/kvm-unusable"
fi

finish mi1-qemu-setup.test.sh
