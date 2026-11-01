package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// inOwnGroup starts cmd in a process group of its own, so that a console
// control event can reach it alone.
func inOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// withoutConsole starts cmd with no console at all (DETACHED_PROCESS), as a
// parent without one would leave it, in a process group of its own.
func withoutConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
}

// stopHub presses Ctrl+Break for the hub: CTRL_BREAK_EVENT to its process
// group, which Go delivers as os.Interrupt, as it does Ctrl+C.
func stopHub(h *hub) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(h.cmd.Process.Pid)) //nolint:gosec // a PID
}
