// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package hubproc

import (
	"os"
	"os/exec"
	"syscall"
)

// ownGroup leaves cmd as it is: a signal reaches only the process that it
// is sent to. ipcam-hub binds its MediaMTX child to its own lifetime
// (Pdeathsig, C07), so a kill stops recording.
func ownGroup(*exec.Cmd) {}

// kill ends p at once, as a crash would: SIGKILL.
func kill(p *os.Process) error { return p.Signal(syscall.SIGKILL) }

// interrupt asks p to stop cleanly, as systemd does: SIGTERM.
func interrupt(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
