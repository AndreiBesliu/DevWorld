// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// procDir is the proc file system; tests point it at a folder of theirs.
var procDir = "/proc"

// userHZ is the unit of /proc's CPU times: USER_HZ, 100 on every
// architecture that the spike runs on (amd64, arm64, arm).
const userHZ = 100

// tick is one USER_HZ tick.
const tick = time.Second / userHZ

// machineTimes is the machine's CPU time since boot, summed over its
// logical CPUs, from /proc/stat's "cpu" line: busy, everything but idle and
// iowait, and total.
func machineTimes() (busy, total time.Duration, err error) {
	data, err := os.ReadFile(filepath.Join(procDir, "stat"))
	if err != nil {
		return 0, 0, err
	}
	line, _, _ := bytes.Cut(data, []byte("\n"))
	fields := strings.Fields(string(line))
	if len(fields) < 9 || fields[0] != "cpu" {
		return 0, 0, errors.New("mi3: /proc/stat has no cpu line")
	}
	// user nice system idle iowait irq softirq steal; guest time is in user.
	var sum, idle int64
	for i, f := range fields[1:9] {
		v, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("mi3: /proc/stat: %w", err)
		}
		sum += v
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return time.Duration(sum-idle) * tick, time.Duration(sum) * tick, nil
}

// procStat is the fields of /proc/<pid>/stat after the command's name:
// the state is [0], the parent [1], utime [11] and stime [12].
func procStat(pid int) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil, err
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return nil, fmt.Errorf("mi3: /proc/%d/stat has no command name", pid)
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 13 {
		return nil, fmt.Errorf("mi3: /proc/%d/stat is short", pid)
	}
	return fields, nil
}

// processTimes is the CPU time of pid, user and system.
func processTimes(pid int) (time.Duration, error) {
	fields, err := procStat(pid)
	if err != nil {
		return 0, err
	}
	user, err1 := strconv.ParseInt(fields[11], 10, 64)
	system, err2 := strconv.ParseInt(fields[12], 10, 64)
	if err := errors.Join(err1, err2); err != nil {
		return 0, fmt.Errorf("mi3: /proc/%d/stat: %w", pid, err)
	}
	return time.Duration(user+system) * tick, nil
}

// processRSS is pid's resident memory, in bytes, from /proc/<pid>/statm.
func processRSS(pid int) (uint64, error) {
	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "statm"))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, fmt.Errorf("mi3: /proc/%d/statm is short", pid)
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("mi3: /proc/%d/statm: %w", pid, err)
	}
	return pages * uint64(os.Getpagesize()), nil //nolint:gosec // the page size is positive
}

// childrenOf lists the processes whose parent is pid.
func childrenOf(pid int) ([]int, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return nil, err
	}
	var out []int
	parent := strconv.Itoa(pid)
	for _, e := range entries {
		child, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		if fields, err := procStat(child); err == nil && fields[1] == parent {
			out = append(out, child)
		}
	}
	return out, nil
}
