// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package hubproc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// ownGroup starts cmd in a process group of its own, so that the owner's
// Ctrl+C in recspike's console reaches recspike alone, which then stops
// ipcam-hub through its stdin. ipcam-hub binds its MediaMTX child to its own
// lifetime (a Job Object, C07), so a kill stops recording.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// exitWait is how long kill waits for a process that refused
// TerminateProcess to show that it has ended.
const exitWait = 5 * time.Second

// kill ends p at once, as a crash would: os.Process.Kill is
// TerminateProcess on Windows.
func kill(p *os.Process) error {
	return killResult(p.Kill(), func(d time.Duration) (bool, error) { return processEnded(p.Pid, d) })
}

// killResult is os.Process.Kill's answer err as kill gives it.
// os.ErrProcessDone, as Linux says it, stands for a child that has ended
// already: a process that cmd.Wait has released gives EINVAL, and
// TerminateProcess answers ERROR_ACCESS_DENIED for a process that is
// exiting or has exited. That error counts as ended only once ended, the
// wait on the process handle, says so within exitWait: otherwise the
// driver would start a second ipcam-hub on the same data folder while this
// one still runs, so it stays the error.
func killResult(err error, ended func(time.Duration) (bool, error)) error {
	switch {
	case errors.Is(err, syscall.EINVAL):
		return os.ErrProcessDone
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		done, werr := ended(exitWait)
		if werr != nil {
			return fmt.Errorf("%w (and the wait for its exit failed: %v)", err, werr)
		}
		if done {
			return os.ErrProcessDone
		}
		return err
	}
	return err
}

// processEnded reports whether the process pid has ended within timeout:
// its handle is signalled. The caller keeps the process's own handle open
// (os.Process holds it until cmd.Wait releases it), so pid still names that
// process.
func processEnded(pid int, timeout time.Duration) (bool, error) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid)) //nolint:gosec // a Windows pid fits in a DWORD
	if err != nil {
		return false, fmt.Errorf("OpenProcess: %w", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	ms := timeout.Milliseconds()
	if ms < 0 || ms >= windows.INFINITE {
		return false, fmt.Errorf("hubproc: wait of %v out of range", timeout)
	}
	ev, err := windows.WaitForSingleObject(h, uint32(ms))
	if err != nil {
		return false, fmt.Errorf("WaitForSingleObject: %w", err)
	}
	return ev == windows.WAIT_OBJECT_0, nil
}
