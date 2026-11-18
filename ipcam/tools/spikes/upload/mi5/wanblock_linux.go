// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

// HostFirewall is this host's WAN block: nft (iptables as the fallback),
// for the hub's uid, with the cleanup's binaries in binDir.
func HostFirewall(binDir string) WanBlock { return newLinuxFirewall(binDir, runCommand) }
