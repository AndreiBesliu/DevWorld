package recorder

import (
	"os"
	"os/exec"
	"syscall"
)

// The Linux parts: the MediaMTX child dies with the hub, the directory sync
// and the rename.

// childProc is a started MediaMTX child.
type childProc struct{ cmd *exec.Cmd }

// startChild starts cmd in a process group of its own, so that a signal to
// the hub's group does not reach it, and with SIGKILL as its parent-death
// signal, so that it dies with the hub (C07): a hub that is killed stops
// recording, as a crash of hubd does (§4.2). The signal fires when the
// thread that started the child exits, so the caller keeps that goroutine
// locked to its thread for the child's lifetime.
func startChild(cmd *exec.Cmd) (*childProc, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &childProc{cmd: cmd}, nil
}

// interrupt asks MediaMTX to stop: it closes its segments, which renames
// them and runs their hooks, and exits.
func (c *childProc) interrupt() error { return c.cmd.Process.Signal(syscall.SIGTERM) }

// release frees what startChild holds once the child has exited.
func (c *childProc) release() {}

// platformSyncDir makes the entries of dir durable.
func platformSyncDir(dir string) error {
	f, err := os.Open(dir) //nolint:gosec // a directory of the recordings
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// renameReplace renames from to to, replacing to.
func renameReplace(from, to string) error { return os.Rename(from, to) }
