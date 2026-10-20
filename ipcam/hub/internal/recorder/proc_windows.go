package recorder

import (
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows parts: the MediaMTX child dies with the hub, the directory
// sync and the rename.

// childProc is a started MediaMTX child and the Job Object that holds it.
type childProc struct {
	cmd *exec.Cmd
	job windows.Handle
}

// startChild starts cmd inside a Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, whose only handle the hub holds:
// when the hub exits, however it exits, Windows closes the handle and kills
// the child (C07), as §4.7 does for the launcher.
func startChild(cmd *exec.Cmd) (*childProc, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("recorder: create a Job Object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("recorder: set up the Job Object: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid)) //nolint:gosec // a PID
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("recorder: put MediaMTX in the Job Object: %w", err)
	}
	return &childProc{cmd: cmd, job: job}, nil
}

// interrupt stops MediaMTX. Windows has no signal that a console child
// would handle here, so it is killed; reconcile repairs its .part files at
// the next start.
func (c *childProc) interrupt() error { return c.cmd.Process.Kill() }

// release closes the Job Object once the child has exited.
func (c *childProc) release() { _ = windows.CloseHandle(c.job) }

// platformSyncDir does nothing: Windows has no directory fsync, and NTFS
// journals directory entries (MTX-1 does the same).
func platformSyncDir(string) error { return nil }

// renameReplace renames from to to, replacing to, with
// MOVEFILE_WRITE_THROUGH so that the rename is on disk when it returns, as
// MTX-1 renames segments.
func renameReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
