#!/usr/bin/env bash
# Prepares a Linux runner for MI1's qemu-reset runs (TST-09): checks that it
# has KVM, installs QEMU and cloud-localds, with the extra apt packages given,
# and lets the runner's user open /dev/kvm through a udev rule. The lab
# workflow's qemu-reset shards and the mi1-smoke job both run it.
#
#   tools/scripts/mi1-qemu-setup.sh [<extra apt package>...]
#
# Prints QEMU's version, and exits 1 when the runner has no KVM or its user
# cannot use it. KVM_DEVICE replaces /dev/kvm in the tests.
set -euo pipefail

kvm="${KVM_DEVICE:-/dev/kvm}"
if [[ ! -e "${kvm}" ]]; then
  echo "KVM is not available on this runner (${kvm} is missing): MI1's qemu-reset runs need it (TST-09)."
  exit 1
fi
sudo apt-get update
sudo apt-get install --yes --no-install-recommends qemu-system-x86 qemu-utils cloud-image-utils "$@"
echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' |
  sudo tee /etc/udev/rules.d/99-kvm4all.rules > /dev/null
sudo udevadm control --reload-rules
sudo udevadm trigger --name-match=kvm
sudo udevadm settle
qemu-system-x86_64 --version | sed -n 1p
if [[ ! -r "${kvm}" || ! -w "${kvm}" ]]; then
  echo "KVM is not usable by this runner's user: $(ls -l "${kvm}")"
  exit 1
fi
