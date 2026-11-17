// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package runstate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive LockFileEx lock on the file's first byte
// without waiting; false when another handle holds it.
func tryLock(f *os.File) (bool, error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

// lockWait takes an exclusive LockFileEx lock on f, waiting for it.
func lockWait(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

// unlock releases the lock on f.
func unlock(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}

var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procGetTickCount64             = kernel32.NewProc("GetTickCount64")
	procQueryUnbiasedInterruptTime = kernel32.NewProc("QueryUnbiasedInterruptTime")
)

// bootPrefix starts a Windows boot id: Windows has no boot id, so it is the
// boot's wall time, the clock minus GetTickCount64, which counts sleep too.
const bootPrefix = "windows-boot-ms:"

// bootTolerance is how far two readings of one boot's time may lie apart:
// the tick's resolution and the clock's corrections in between.
const bootTolerance = 2 * time.Minute

// BootID is this boot's id: the boot's wall time, in Unix ms.
func BootID() (string, error) {
	if err := procGetTickCount64.Find(); err != nil {
		return "", err
	}
	ms, _, _ := procGetTickCount64.Call()
	boot := time.Now().Add(-time.Duration(ms) * time.Millisecond) //nolint:gosec // ms since boot, far below 2^63
	return bootPrefix + strconv.FormatInt(boot.UnixMilli(), 10), nil
}

// SameBoot reports whether two boot ids name one boot: their boot times lie
// within bootTolerance.
func SameBoot(a, b string) bool {
	ta, okA := bootMs(a)
	tb, okB := bootMs(b)
	if !okA || !okB {
		return a == b
	}
	d := ta - tb
	return d < bootTolerance.Milliseconds() && -d < bootTolerance.Milliseconds()
}

// bootMs is a boot id's time.
func bootMs(id string) (int64, bool) {
	rest, ok := strings.CutPrefix(id, bootPrefix)
	if !ok {
		return 0, false
	}
	ms, err := strconv.ParseInt(rest, 10, 64)
	return ms, err == nil
}

// Mono is the host's monotonic clock since its boot, which stops while it
// sleeps: QueryUnbiasedInterruptTime.
func Mono() (time.Duration, error) {
	if err := procQueryUnbiasedInterruptTime.Find(); err != nil {
		return 0, err
	}
	// t is in 100-ns units.
	var t uint64
	p := unsafe.Pointer(&t) //nolint:gosec // the call's out parameter
	if r, _, err := procQueryUnbiasedInterruptTime.Call(uintptr(p)); r == 0 {
		return 0, err
	}
	return time.Duration(t) * 100, nil //nolint:gosec // 100-ns units since boot, far below 2^63 ns
}

// ProcStartMs is when the process pid started, in Unix ms: its creation
// time.
func ProcStartMs(pid int) (int64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // a Windows pid fits in a DWORD
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return 0, err
	}
	if code != 259 { // STILL_ACTIVE
		return 0, fmt.Errorf("runstate: process %d has exited", pid)
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return creation.Nanoseconds() / int64(time.Millisecond), nil
}
