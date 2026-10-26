// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package hubproc

import (
	"errors"
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
// TerminateProcess on Windows. A child that has ended already, as Linux's
// os.ErrProcessDone says: TerminateProcess answers a process that has
// exited with ERROR_ACCESS_DENIED, and a process that cmd.Wait has
// released gives EINVAL.
func kill(p *os.Process) error {
	err := p.Kill()
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, syscall.EINVAL) {
		return os.ErrProcessDone
	}
	return err
}
