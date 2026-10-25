// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package hubproc

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ownGroup starts cmd in a process group of its own, so that the owner's
// Ctrl+C in recspike's console reaches recspike alone, which then stops
// ipcam-hub through its stdin. ipcam-hub binds its MediaMTX child to its own
// lifetime (a Job Object, C07), so a kill stops recording.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// kill ends p at once, as a crash would: os.Process.Kill is
// TerminateProcess on Windows.
func kill(p *os.Process) error { return p.Kill() }
