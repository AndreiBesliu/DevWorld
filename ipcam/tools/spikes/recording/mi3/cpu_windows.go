// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GetSystemTimes and K32GetProcessMemoryInfo, which x/sys/windows lacks.
var (
	getSystemTimes          = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemTimes")
	getProcessMemoryInfoK32 = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
)

// filetime is a FILETIME span: 100-ns units.
func filetime(ft windows.Filetime) time.Duration {
	return time.Duration(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) * 100 //nolint:gosec // CPU time, far below 2^63 ns
}

// machineTimes is the machine's CPU time since boot, summed over its
// logical processors, from GetSystemTimes: busy, everything but idle, and
// total; the kernel time includes the idle time.
func machineTimes() (busy, total time.Duration, err error) {
	if err := getSystemTimes.Find(); err != nil {
		return 0, 0, err
	}
	var idle, kernel, user windows.Filetime
	ok, _, callErr := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))) //nolint:gosec // GetSystemTimes's arguments
	if ok == 0 {
		return 0, 0, callErr
	}
	total = filetime(kernel) + filetime(user)
	return total - filetime(idle), total, nil
}

// openProcess opens pid to read its times and memory.
func openProcess(pid int) (windows.Handle, error) {
	return windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // a process id
}

// processTimes is the CPU time of pid, user and kernel.
func processTimes(pid int) (time.Duration, error) {
	h, err := openProcess(pid)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return filetime(kernel) + filetime(user), nil
}

// processMemoryCounters is PROCESS_MEMORY_COUNTERS (psapi.h).
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// processRSS is pid's working set, Windows' resident memory, in bytes.
func processRSS(pid int) (uint64, error) {
	if err := getProcessMemoryInfoK32.Find(); err != nil {
		return 0, err
	}
	h, err := openProcess(pid)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))                                                                        //nolint:gosec // a small struct
	ok, _, callErr := getProcessMemoryInfoK32.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)) //nolint:gosec // the call's arguments
	if ok == 0 {
		return 0, callErr
	}
	return uint64(c.workingSetSize), nil
}

// childrenOf lists the processes whose parent is pid, from a Toolhelp
// snapshot.
func childrenOf(pid int) ([]int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e)) //nolint:gosec // a small struct
	var out []int
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if int(e.ParentProcessID) == pid && e.ProcessID != 0 {
			out = append(out, int(e.ProcessID))
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return out, nil
}
