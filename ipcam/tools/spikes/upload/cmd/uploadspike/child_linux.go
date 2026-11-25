// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"os/exec"
	"runtime"
	"syscall"
)

// startWithUs starts cmd so that it dies with uploadspike, a SIGKILL
// included (PR_SET_PDEATHSIG): a killed mi5 leaves no reccontroller on the
// cameras' ports. It returns a channel closed once the child ended. Linux
// sends the signal when the thread that created the child ends, not the
// process (go.dev/issue/27505), and Go ends a thread whenever a goroutine
// exits locked to it, as hubproc's own does at each hub's end; so the
// child is started and waited for on a goroutine locked to its thread for
// the child's whole life, as hubproc's startChild does. A thread unlocked
// after the start could serve such a goroutine later: the lab run on
// 6415e55 lost its cameras when the login's hub stopped.
func startWithUs(cmd *exec.Cmd) (<-chan struct{}, error) {
	started := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread() // never unlocked: the thread ends with the child
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		_ = cmd.Wait()
		close(done)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return done, nil
}
