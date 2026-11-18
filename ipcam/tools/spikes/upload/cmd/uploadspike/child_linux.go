// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"os/exec"
	"runtime"
	"syscall"
)

// startWithUs starts cmd so that it dies with uploadspike, a SIGKILL
// included (PR_SET_PDEATHSIG): a killed mi5 leaves no reccontroller on
// the cameras' ports. The thread that forks stays locked meanwhile, since
// the signal follows that thread's end; Go keeps its threads after an
// unlock.
func startWithUs(cmd *exec.Cmd) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd.Start()
}
