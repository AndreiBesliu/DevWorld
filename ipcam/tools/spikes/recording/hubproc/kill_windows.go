// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package hubproc

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ownGroup starts cmd in a process group of its own, so that a console
// control event reaches ipcam-hub alone. ipcam-hub binds its MediaMTX child
// to its own lifetime (a Job Object, C07), so a kill stops recording.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// kill ends p at once, as a crash would: os.Process.Kill is
// TerminateProcess on Windows.
func kill(p *os.Process) error { return p.Kill() }

// interrupt presses Ctrl+Break for p: CTRL_BREAK_EVENT to its process
// group, which ipcam-hub receives as os.Interrupt. The child shares
// recspike's console.
func interrupt(p *os.Process) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.Pid)) //nolint:gosec // a PID
}
