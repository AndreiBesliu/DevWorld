// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package hubproc

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// setProcAttr starts ipcam-hub in a session of its own (Setsid), so that a
// terminal hangup or Ctrl-C reaches only uploadspike, with Pdeathsig
// SIGTERM as a backstop for a hub that would not stop at its stdin's end,
// and as owner when set. Go sets Pdeathsig after the switch of identity,
// which would otherwise clear it (syscall/exec_linux.go).
func setProcAttr(cmd *exec.Cmd, owner *fileOwner) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGTERM}
	if owner != nil && owner.switchID {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(owner.uid), Gid: uint32(owner.gid), NoSetGroups: true} //nolint:gosec // a uid and a gid
	}
}

// setDiagAttr runs a diag command as owner when set.
func setDiagAttr(cmd *exec.Cmd, owner *fileOwner) {
	if owner != nil && owner.switchID {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(owner.uid), Gid: uint32(owner.gid), NoSetGroups: true}, //nolint:gosec // a uid and a gid
		}
	}
}

// startChild starts cmd and waits for it on a goroutine locked to its OS
// thread for the child's whole life: Linux sends Pdeathsig when the thread
// that created the child ends, not the process (go.dev/issue/27505). done
// runs once the child has exited.
func startChild(cmd *exec.Cmd, done func()) error {
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread ends with the child
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		_ = cmd.Wait()
		done()
	}()
	return <-started
}

// kill ends p at once, as a crash would: SIGKILL.
func kill(p *os.Process) error { return p.Signal(syscall.SIGKILL) }
