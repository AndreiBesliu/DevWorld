// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package runstate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive flock on f without waiting; false when another
// open file holds it.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // a file descriptor
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// lockWait takes an exclusive flock on f, waiting for it.
func lockWait(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX) //nolint:gosec // a file descriptor
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// unlock releases the flock on f.
func unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) } //nolint:gosec // a file descriptor

// BootID is this boot's id: the kernel's boot_id, which each boot draws
// anew.
func BootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", errors.New("runstate: the boot id is empty")
	}
	return id, nil
}

// SameBoot reports whether two boot ids name one boot.
func SameBoot(a, b string) bool { return a == b }

// Mono is the host's monotonic clock since its boot, which stops while it
// sleeps: CLOCK_MONOTONIC.
func Mono() (time.Duration, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}
	return time.Duration(ts.Nano()), nil
}

// clockTicks is the kernel's USER_HZ, the unit of /proc/<pid>/stat's start
// time, 100 on every Linux that the spike runs on.
const clockTicks = 100

// ProcStartMs is when the process pid started, in ms since the boot: field
// 22 of /proc/<pid>/stat. A process that has exited, a zombie included,
// has none.
func ProcStartMs(pid int) (int64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The command name, field 2, is in parentheses and may hold spaces.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, errors.New("runstate: a malformed /proc stat")
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 {
		return 0, errors.New("runstate: a short /proc stat")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return 0, fmt.Errorf("runstate: process %d has exited", pid) // a zombie waits for its reaping only
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, err
	}
	return ticks * 1000 / clockTicks, nil
}

// replaceFile renames from over to: Linux replaces a file that another
// process has open.
func replaceFile(from, to string) error { return os.Rename(from, to) }

// readFile reads the run state.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // uploadspike's own run state
}
