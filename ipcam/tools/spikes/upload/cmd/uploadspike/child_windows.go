// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// startWithUs starts cmd inside a Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE whose only handle this process keeps
// until it exits: however uploadspike ends, TerminateProcess included,
// Windows closes the handle and ends reccontroller, so a killed mi5 leaves
// nothing on the cameras' ports (a closed console window ends both
// anyway). The child starts suspended and runs only once it is in the job,
// so nothing it starts escapes the job. A copy of the hub's recorder
// (startChild, C07): tools import nothing of the hub. It returns a channel
// closed once the child ended.
func startWithUs(cmd *exec.Cmd) (<-chan struct{}, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create a Job Object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil { //nolint:gosec // G103: the limits' address and size, as SetInformationJobObject takes them
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("set up the Job Object: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	pid := uint32(cmd.Process.Pid) //nolint:gosec // a PID
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err == nil {
		err = resumeProcess(pid)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("put the child in the Job Object: %w", err)
	}
	// The job's handle stays open until uploadspike exits.
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return done, nil
}

// resumeProcess resumes the threads of process pid, which started
// suspended: its main thread, the only one it has.
func resumeProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
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
			return terr
		}
		resumed++
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	if resumed == 0 {
		return errors.New("the child has no thread to resume")
	}
	return nil
}
