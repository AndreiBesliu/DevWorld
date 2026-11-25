// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

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

// setProcAttr starts ipcam-hub in a process group of its own, as c1's
// ownGroup, so that a Ctrl+C in the owner's window reaches uploadspike
// alone, which then stops the hub through its stdin. No Job Object binds
// the hub to uploadspike: a kill on the job's close would end the hub
// before it acts on its stdin's end and tear every open segment; the next
// holder's WaitGone is the backstop.
func setProcAttr(cmd *exec.Cmd, _ *fileOwner) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// setDiagAttr leaves a diag command as it is: Windows runs it as
// uploadspike.
func setDiagAttr(*exec.Cmd, *fileOwner) {}

// startChild starts cmd and waits for it; done runs once it has exited.
func startChild(cmd *exec.Cmd, done func()) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
		done()
	}()
	return nil
}

// exitWait is how long kill waits for a process that refused
// TerminateProcess to show that it has ended.
const exitWait = 5 * time.Second

// kill ends p at once, as a crash would: os.Process.Kill is
// TerminateProcess on Windows.
func kill(p *os.Process) error {
	return killResult(p.Kill(), func(d time.Duration) (bool, error) { return processEnded(p.Pid, d) })
}

// killResult is c1's: os.Process.Kill's answer err as kill gives it.
// os.ErrProcessDone stands for a child that has ended already: a process
// that cmd.Wait has released gives EINVAL, and TerminateProcess answers
// ERROR_ACCESS_DENIED for a process that is exiting or has exited. That
// error counts as ended only once ended, the wait on the process handle,
// says so within exitWait: otherwise a second ipcam-hub could start on the
// same data folder while this one still runs, so it stays the error.
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
// its handle is signalled.
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
