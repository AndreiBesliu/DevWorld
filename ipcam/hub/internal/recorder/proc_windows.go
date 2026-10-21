package recorder

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
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
// the child (C07), as §4.7 does for the launcher. The child starts
// suspended and runs only once it is in the job, so neither it nor a
// process it starts can escape the job. It gets a process group of its own,
// which interrupt's Ctrl+Break reaches alone, and which a Ctrl+C in the
// hub's console does not reach.
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
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	pid := uint32(cmd.Process.Pid) //nolint:gosec // a PID
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
		if err != nil {
			err = fmt.Errorf("recorder: put MediaMTX in the Job Object: %w", err)
		}
	}
	if err == nil {
		err = resumeProcess(pid)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &childProc{cmd: cmd, job: job}, nil
}

// resumeProcess resumes the threads of process pid, which started
// suspended: its main thread, the only one it has.
func resumeProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("recorder: list the threads to resume MediaMTX: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := 0
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, terr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if terr == nil {
			_, terr = windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
		}
		if terr != nil {
			return fmt.Errorf("recorder: resume MediaMTX: %w", terr)
		}
		resumed++
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("recorder: list the threads to resume MediaMTX: %w", err)
	}
	if resumed == 0 {
		return errors.New("recorder: MediaMTX has no thread to resume")
	}
	return nil
}

// interrupt asks MediaMTX to stop with a Ctrl+Break to its process group:
// Go delivers it as os.Interrupt, on which MediaMTX closes its segments,
// which renames them and runs their hooks, and exits
// (internal/core/core.go). It fails when the hub has no console that the
// child shares.
func (c *childProc) interrupt() error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(c.cmd.Process.Pid)) //nolint:gosec // a PID
}

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
