// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

// HostFirewall is this host's WAN block: Windows Firewall rules for
// binDir's ipcam-hub.exe and mediamtx.exe.
func HostFirewall(binDir string) WanBlock { return newWindowsFirewall(binDir, runCommand) }
